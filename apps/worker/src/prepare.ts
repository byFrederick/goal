import { createHash } from 'node:crypto';
import { createWriteStream } from 'node:fs';
import { chmod, mkdir, readdir, rm, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { Readable } from 'node:stream';
import { pipeline } from 'node:stream/promises';
import type { ReadableStream } from 'node:stream/web';
import { fileURLToPath } from 'node:url';
import { cpuStartFile, saveCpuStart } from './cgroup.js';
import { gitWithRetry } from './retry.js';
import { Bundle, type ClonedRepository, type GitCredential, type Knowledge, paths } from './spec.js';

/**
 * One command prepare runs; the result is its exit code. Its output goes to stderr, never stdout.
 * With signal, the command is ended when it fires; with stderr, its stderr is also passed there;
 * with env, those variables are added to the command's environment, and to no other command's.
 */
export type Exec = (
  command: string,
  args: string[],
  opts?: { cwd?: string; env?: Record<string, string>; signal?: AbortSignal; stderr?: (chunk: string) => void },
) => Promise<number>;

/** One command whose stdout prepare reads; it throws when the command fails. Its stderr goes to stderr. */
export type Output = (command: string, args: string[], opts?: { cwd?: string }) => Promise<string>;

export interface PrepareDeps {
  fetch: typeof fetch;
  exec: Exec;
  output: Output;
  env: NodeJS.ProcessEnv;
  now: () => Date;
  /**
   * Where the bundle's files go (/goal), the repository is cloned (/workspace), the item's other
   * repositories are (/repos), Claude Code's config dir (claude) the box's home, where git's
   * credential store and the helper's files go, and the run's knowledge is unpacked (/knowledge).
   */
  dirs: { goal: string; workspace: string; repos: string; claude: string; home: string; knowledge: string };
  /**
   * How git runs the credential helper (src/credential.ts): node and the script. The worker's own
   * node and dist/credential.js beside this file when absent.
   */
  credentialHelper?: { node: string; script: string };
  /** The wait between attempts of a network git step; setTimeout when absent. */
  sleep?: (ms: number) => Promise<void>;
  /** Where a retry, or a cgroup counter that can't be read, is logged; stderr when absent. */
  log?: (line: string) => void;
  /**
   * The box's cgroup v2 root (src/cgroup.ts), whose cpu.stat usage_usec prepare saves to
   * dirs.goal/cpu-start-usec as it starts, for the run's cpu_seconds. Absent, nothing is saved.
   */
  cgroup?: string;
}

/** Each network git attempt's default time limit, in seconds (GOAL_GIT_ATTEMPT_TIMEOUT). */
const defaultAttemptTimeout = 300;

/** prepare's one stdout line: what it set up, for goald's run events. */
export interface Prepared {
  type: 'prepared';
  at: string;
  seconds: number;
  /** The item's branch checked out in the workspace; absent without a repository. */
  branch?: string;
  /** Whether that branch already existed on the remote: earlier runs of the item pushed it. */
  branch_existed?: boolean;
  /** How the existing branch was brought up to date with its base; absent for a new one. */
  fresh_base?: FreshBase;
  /** The files that conflicted doing so, sorted; the rebase or merge was aborted. */
  conflicts?: string[];
  /**
   * How many of the bundle's tools were installed: runtime.tools, then the connections' CLI tools.
   * The repository's own, which mise installs from its config, are not counted.
   */
  tools: number;
  /** Definition mode: how many of the draft's files were written into the workspace. */
  definition_files?: number;
  /** How many knowledge snapshots were unpacked under /knowledge; absent for none. */
  knowledge?: number;
  /**
   * Each repository prepare cloned (ADR 0041 decision 5): its URL, the base's commit and the
   * branch's head right after checkout, before it was brought up to date; absent without a repository.
   */
  repositories?: { repo: string; base_commit?: string; branch_head?: string }[];
}

/** The transport of the address pushes to a read-only prefix go to (bin/git-remote-goal-refuse). */
export const refusingTransport = 'goal-refuse';

type FreshBase = NonNullable<NonNullable<Bundle['repository']>['fresh_base']>;

/**
 * goal-agent prepare: fetches the run bundle from the gateway with the run's task token and
 * sets the sandbox up for `goal-agent run`. It writes /goal (spec.json and the agent's files),
 * configures a git credential helper per bundle credential (a stored one in git's credential store
 * under the box's home: storeCredentials), clones the repository into
 * /workspace on the item's branch (the remote one when it exists, otherwise new from the base: the
 * bundle's repository.base, created from the default branch when missing, or the default branch),
 * brings an existing branch up to date with that base (fresh_base: rebase or merge; a conflict is
 * aborted and reported, not failed), and installs tools with mise: the
 * repository's own (mise.toml, .tool-versions, …), then the definition's runtime.tools. The factory's rules
 * go to the user-level rules in the config dir, never into /workspace, so they cannot be committed.
 * In definition mode (the bundle's definition, ADR 0021) /workspace gets the factory's draft files
 * instead, /goal/definition.json their starting sha256, and no git command runs.
 * While the repository clones, prepare downloads the run's knowledge snapshots (spec.knowledge,
 * ADR 0038) in parallel and unpacks each into /knowledge/<source>/, files 0444 and folders 0555,
 * replacing an earlier unpack; a snapshot that does not download or unpack fails the run.
 * A station run's bundle with repositories (ADR 0041 decision 5) clones each instead, by path: the
 * first at /workspace as above, base and fresh_base from repository, and each other under /repos
 * (cloneOther). Its git signs in with the per-path credential helper and refuses pushes where the
 * run may not write (configureCode), and the stored login is not kept. An empty list clones nothing.
 * The clone is partial (--filter=blob:none: history without file contents, fetched when checked
 * out). The network git steps (the clone, and the base branch's push and fetch) run with a time limit
 * per attempt (GOAL_GIT_ATTEMPT_TIMEOUT seconds, 300 by default); an attempt cut off at it or failed
 * on the transport (a dropped connection, a 5xx) is retried up to 3 attempts, the clone from an
 * empty workspace. Any other failure throws.
 * With deps.cgroup, prepare first saves the box's CPU counter beside spec.json (saveCpuStart); a
 * counter that can't be read saves nothing and never fails prepare.
 */
export async function prepare(deps: PrepareDeps): Promise<Prepared> {
  const started = deps.now();
  if (deps.cgroup !== undefined) await saveCpuStart(deps.cgroup, join(deps.dirs.goal, cpuStartFile), deps.log);
  const gateway = required(deps.env, 'GOAL_GATEWAY_URL');
  const token = required(deps.env, 'GOAL_GATEWAY_TOKEN');
  const bundle = await fetchBundle(deps, new URL('/run', gateway), token);
  // It runs beside the clone; the handler keeps a failure from going unhandled until it is awaited.
  const knowledge = unpackKnowledge(deps, gateway, token, bundle.spec.knowledge?.snapshots ?? []);
  knowledge.catch(() => undefined);

  for (const [path, content] of Object.entries(bundle.files)) {
    await put(join(deps.dirs.goal, path), Buffer.from(content, 'base64'));
  }
  // Each rule is the complete file (frontmatter included); its name is a slug, so it stays in rules/.
  for (const rule of bundle.spec.rules ?? []) {
    await put(join(deps.dirs.claude, 'rules', `${rule.name}.md`), rule.content);
  }
  // Definition mode has no repository: nothing signs git in.
  const credentials = bundle.definition ? [] : bundle.git_credentials;
  // A station run's code (ADR 0041): its repositories by path, a try and older bundles excepted.
  const code = !bundle.definition && !bundle.repository?.commit ? bundle.repositories : undefined;
  if (code) await configureCode(deps, bundle);
  const store = code
    ? ''
    : await storeCredentials(
        deps,
        credentials.filter((c) => c.store),
      );
  // The per-path helper replaces the stored login; connection helpers stay, after it.
  for (const c of code ? credentials.filter((c) => !c.store) : credentials) {
    // --add: two connections may sign git in to one host; git asks each helper until one answers.
    await must(deps, 'git', [
      'config',
      '--global',
      '--add',
      `credential.https://${c.host}.helper`,
      c.store ? `store --file '${store}'` : credentialHelper(c),
    ]);
  }

  const mise = miseEnv(deps.env, credentials, bundle.repository_logins ?? []);
  const out: Prepared = { type: 'prepared', at: '', seconds: 0, tools: bundle.tools.length };
  if (bundle.definition) {
    const baseline: Record<string, string> = {};
    for (const [path, content] of Object.entries(bundle.definition.files)) {
      const bytes = Buffer.from(content, 'base64');
      await put(join(deps.dirs.workspace, path), bytes);
      baseline[path] = createHash('sha256').update(bytes).digest('hex');
    }
    await put(join(deps.dirs.goal, 'definition.json'), JSON.stringify(baseline));
    out.definition_files = Object.keys(baseline).length;
  }
  const repo = bundle.repository;
  let baseBranch: string | undefined;
  const cloned: ClonedRepository[] = [];
  if (code) {
    for (const c of code) {
      const entry: ClonedRepository = { ...c };
      if (c.path === paths.workspace) {
        const first = await cloneWorkspace(deps, { ...c, base: repo?.base, fresh_base: repo?.fresh_base }, out, mise);
        baseBranch = first.conflictsWith;
        if (first.base_head) entry.base_commit = first.base_head;
        if (first.branch_head) entry.branch_head = first.branch_head;
      } else {
        Object.assign(entry, await cloneOther(deps, c));
      }
      cloned.push(entry);
    }
  } else if (repo?.commit) {
    // A try's repository is read-only: its agent gets no git login (ADR 0039 decision 9).
    cloned.push(await checkoutCommit(deps, repo, out, mise));
  } else if (repo) {
    const first = await cloneWorkspace(deps, repo, out, mise);
    baseBranch = first.conflictsWith;
    cloned.push({
      url: repo.url,
      path: paths.workspace,
      branch: repo.branch,
      write: true,
      ...(first.base_head ? { base_commit: first.base_head } : {}),
      ...(first.branch_head ? { branch_head: first.branch_head } : {}),
    });
  }
  if (code || cloned.length > 0) {
    out.repositories = cloned.map((e) => ({
      repo: e.url,
      ...(e.base_commit ? { base_commit: e.base_commit } : {}),
      ...(e.branch_head ? { branch_head: e.branch_head } : {}),
    }));
  }
  // A try's agent gets no git login (ADR 0039 decision 9): the clone was the logins' only use.
  if (repo?.commit) await dropLogins(deps, credentials);
  const unpacked = await knowledge;
  if (unpacked > 0) out.knowledge = unpacked;

  const spec = {
    ...bundle.spec,
    gateway_url: gateway,
    ...(out.conflicts ? { base_conflicts: out.conflicts } : {}),
    ...(baseBranch ? { base_branch: baseBranch } : {}),
    ...(out.branch ? { branch: out.branch } : {}),
    ...(code || cloned.length > 0 ? { repositories: cloned } : {}),
  };
  await put(join(deps.dirs.goal, 'spec.json'), JSON.stringify(spec));

  if (bundle.tools.length > 0) {
    // In the global config, so they are on PATH (mise's shims) wherever the agent works.
    await must(deps, 'mise', ['use', '--global', '--yes', ...bundle.tools], undefined, mise);
  }

  const done = deps.now();
  out.at = done.toISOString();
  out.seconds = Math.round((done.getTime() - started.getTime()) / 100) / 10;
  return out;
}

/**
 * Clones the first repository into /workspace on the item's branch: the remote one when it exists,
 * otherwise new from its base (created from the default branch when missing), brings an existing
 * branch up to date with that base and installs the repository's own tools. It sets out's branch,
 * fresh_base and conflicts, and returns the base the conflicts are with and the heads before the
 * rebase or merge, which fill the repository's entry.
 */
async function cloneWorkspace(
  deps: PrepareDeps,
  repo: { url: string; branch: string; base?: string | undefined; fresh_base?: FreshBase | undefined },
  out: Prepared,
  mise: Record<string, string>,
): Promise<{ conflictsWith: string | undefined; base_head?: string; branch_head?: string }> {
  const ws = deps.dirs.workspace;
  // An empty base is the default branch, as an absent one.
  const base = repo.base || undefined;
  await must(deps, 'git', ['check-ref-format', '--branch', repo.branch]);
  if (base) await must(deps, 'git', ['check-ref-format', '--branch', base]);
  const clone = await network(deps, ['clone', '--quiet', '--filter=blob:none', '--', repo.url, ws], { clean: ws });
  if (clone !== 0) throw new Error(`git clone exited ${clone}`);
  const existed = await hasRemote(deps, ws, repo.branch);
  if (base) await ensureBase(deps, ws, base);
  const start = base ? [`origin/${base}`] : [];
  const branch = existed ? ['-B', repo.branch, `origin/${repo.branch}`] : ['-b', repo.branch, ...start];
  await must(deps, 'git', ['checkout', '--quiet', ...branch], ws);
  // The heads before the rebase or merge, for a later try to start where this run did (ADR 0039).
  const branchHead = await commitOf(deps, ws, 'HEAD');
  const baseHead = await commitOf(deps, ws, start[0] ?? 'origin/HEAD');
  let conflictsWith: string | undefined;
  if (existed) {
    out.fresh_base = repo.fresh_base ?? 'rebase';
    const conflicts = await freshen(deps, ws, out.fresh_base, base);
    if (conflicts.length > 0) {
      out.conflicts = conflicts;
      conflictsWith = base;
    }
  }
  // The repository's own tools; mise trusts configs under /workspace (MISE_TRUSTED_CONFIG_PATHS).
  await must(deps, 'mise', ['install', '--yes'], ws, mise);
  out.branch = repo.branch;
  out.branch_existed = existed;
  return {
    conflictsWith,
    ...(baseHead ? { base_head: baseHead } : {}),
    ...(branchHead ? { branch_head: branchHead } : {}),
  };
}

/**
 * Clones one of the item's other repositories, partially, at its path under /repos: on the item's
 * branch where the remote has it (checked out with -B from origin/<branch>), otherwise on the
 * default branch. It returns the default branch's commit and the head checked out.
 */
async function cloneOther(
  deps: PrepareDeps,
  c: { url: string; path: string; branch: string },
): Promise<{ base_commit?: string; branch_head?: string }> {
  const dir = join(deps.dirs.repos, c.path.slice(paths.repos.length + 1));
  await must(deps, 'git', ['check-ref-format', '--branch', c.branch]);
  const clone = await network(deps, ['clone', '--quiet', '--filter=blob:none', '--', c.url, dir], { clean: dir });
  if (clone !== 0) throw new Error(`git clone of ${c.url} exited ${clone}`);
  if (await hasRemote(deps, dir, c.branch)) {
    await must(deps, 'git', ['checkout', '--quiet', '-B', c.branch, `origin/${c.branch}`], dir);
  }
  const base = await commitOf(deps, dir, 'origin/HEAD');
  const head = await commitOf(deps, dir, 'HEAD');
  return { ...(base ? { base_commit: base } : {}), ...(head ? { branch_head: head } : {}) };
}

/**
 * Sets git up for a station run's code (ADR 0041 decision 5). credential.useHttpPath on, so git
 * asks for each repository's path; the logins, with the tokens read from their variables (one
 * whose variable is unset is left out), in a 0600 file under the box's home; and the credential
 * helper (src/credential.ts) first for each login's host. For each prefix the run may not write,
 * pushes go to goal-refuse::<prefix>, which refuses them (bin/git-remote-goal-refuse); a write
 * login under such a prefix gets an identity rewrite, since git takes the longest pushInsteadOf.
 * Fetches are never rewritten, so they keep the token.
 */
async function configureCode(deps: PrepareDeps, bundle: Bundle): Promise<void> {
  const logins = bundle.repository_logins ?? [];
  const readOnly = bundle.read_only ?? [];
  const dir = join(deps.dirs.home, '.goal');
  const loginsFile = join(dir, 'repository-logins.json');
  const recordFile = join(dir, 'repositories');
  await mkdir(dir, { recursive: true, mode: 0o700 });
  const answers = logins.flatMap((l) => {
    const password = deps.env[l.password_env];
    return password ? [{ prefix: l.prefix, username: l.username, password }] : [];
  });
  await writeFile(loginsFile, JSON.stringify(answers), { mode: 0o600 });
  await chmod(loginsFile, 0o600);

  const helper = deps.credentialHelper ?? {
    node: process.execPath,
    script: fileURLToPath(new URL('./credential.js', import.meta.url)),
  };
  await must(deps, 'git', ['config', '--global', 'credential.useHttpPath', 'true']);
  const command = `!${[helper.node, helper.script, loginsFile, recordFile].map(shellQuote).join(' ')}`;
  for (const host of new Set(logins.map((l) => new URL(l.prefix).host))) {
    await must(deps, 'git', ['config', '--global', '--add', `credential.https://${host}.helper`, command]);
  }

  if (readOnly.length > 0) {
    await must(deps, 'git', ['config', '--global', `protocol.${refusingTransport}.allow`, 'always']);
  }
  for (const prefix of readOnly) {
    await must(deps, 'git', [
      'config',
      '--global',
      '--add',
      `url.${refusingTransport}::${prefix}.pushInsteadOf`,
      prefix,
    ]);
  }
  for (const l of logins) {
    if (l.write && readOnly.some((p) => l.prefix.startsWith(p) && l.prefix !== p)) {
      await must(deps, 'git', ['config', '--global', '--add', `url.${l.prefix}.pushInsteadOf`, l.prefix]);
    }
  }
}

/** s as one sh word. */
function shellQuote(s: string): string {
  return `'${s.replaceAll("'", `'\\''`)}'`;
}

/**
 * Downloads snapshots in parallel and unpacks each into dirs.knowledge/<source>/; it returns how
 * many. It fails with the first failure once every one has settled, so none still writes after.
 */
async function unpackKnowledge(
  deps: PrepareDeps,
  gateway: string,
  token: string,
  snapshots: Knowledge['snapshots'],
): Promise<number> {
  const results = await Promise.allSettled(snapshots.map((s) => unpackSnapshot(deps, gateway, token, s)));
  const failed = results.find((r) => r.status === 'rejected');
  if (failed) throw failed.reason;
  return snapshots.length;
}

/**
 * Downloads one snapshot's zip to a file beside its folder, replaces the folder with the zip's
 * files and makes them read-only. Its URL resolves against the gateway's, and only a request to the
 * gateway carries the task token.
 */
async function unpackSnapshot(
  deps: PrepareDeps,
  gateway: string,
  token: string,
  s: Knowledge['snapshots'][number],
): Promise<void> {
  const url = new URL(s.url, gateway);
  const headers: Record<string, string> =
    url.origin === new URL(gateway).origin ? { Authorization: `Bearer ${token}` } : {};
  const res = await deps.fetch(url, { headers });
  if (!res.ok || !res.body) {
    const why = (await res.text()).trim().slice(0, 200);
    throw new Error(`knowledge ${s.source}: GET ${url.pathname}: ${res.status}${why ? ` ${why}` : ''}`);
  }
  const dest = join(deps.dirs.knowledge, s.source);
  const zip = join(deps.dirs.knowledge, `.${s.source}.zip`);
  await mkdir(deps.dirs.knowledge, { recursive: true });
  try {
    await pipeline(Readable.fromWeb(res.body as ReadableStream<Uint8Array>), createWriteStream(zip));
    await removeTree(dest);
    await mkdir(dest);
    const code = await deps.exec('unzip', ['-q', '-o', zip, '-d', dest]);
    if (code !== 0) throw new Error(`knowledge ${s.source}: unzip exited ${code}`);
  } finally {
    await rm(zip, { force: true });
  }
  await readOnly(dest);
}

/** Makes the files under dir 0444 and its folders, dir included, 0555; a link is left as it is. */
async function readOnly(dir: string): Promise<void> {
  for (const e of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, e.name);
    if (e.isDirectory()) await readOnly(path);
    else if (e.isFile()) await chmod(path, 0o444);
  }
  await chmod(dir, 0o555);
}

/** Removes dir, an earlier read-only unpack included: its folders are made writable first. */
async function removeTree(dir: string): Promise<void> {
  const writable = async (d: string): Promise<void> => {
    await chmod(d, 0o755);
    for (const e of await readdir(d, { withFileTypes: true })) {
      if (e.isDirectory()) await writable(join(d, e.name));
    }
  };
  try {
    await writable(dir);
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code !== 'ENOENT') throw err;
  }
  await rm(dir, { recursive: true, force: true });
}

/** The commit ref names in ws, or undefined when git cannot name one (an empty repository). */
async function commitOf(deps: PrepareDeps, ws: string, ref: string): Promise<string | undefined> {
  try {
    const sha = (await deps.output('git', ['rev-parse', '--verify', '--quiet', `${ref}^{commit}`], { cwd: ws })).trim();
    return isCommitId(sha) ? sha : undefined;
  } catch {
    return undefined;
  }
}

/** Whether s is a full git commit id: 40 (SHA-1) or 64 (SHA-256) lowercase hex digits. */
export function isCommitId(s: string): boolean {
  return /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/.test(s);
}

async function hasRemote(deps: PrepareDeps, ws: string, branch: string): Promise<boolean> {
  const ref = `refs/remotes/origin/${branch}`;
  return (await deps.exec('git', ['rev-parse', '--verify', '--quiet', ref], { cwd: ws })) === 0;
}

/**
 * Makes sure the remote has the base branch. A missing one is created from the default branch
 * (origin/HEAD) and pushed plainly, never forced. When that push fails because another run pushed
 * the base first, it fetches theirs and uses it; any other failure throws.
 */
async function ensureBase(deps: PrepareDeps, ws: string, base: string): Promise<void> {
  if (await hasRemote(deps, ws, base)) return;
  await must(deps, 'git', ['branch', '--no-track', base, 'origin/HEAD'], ws);
  const code = await network(deps, ['push', '--quiet', 'origin', base], { cwd: ws });
  if (code === 0) return;
  const tracking = `+refs/heads/${base}:refs/remotes/origin/${base}`;
  if ((await network(deps, ['fetch', '--quiet', 'origin', tracking], { cwd: ws })) !== 0) {
    throw new Error(`git push exited ${code}`);
  }
  // The local base follows the one that won, so nothing in the workspace points at the lost one.
  await must(deps, 'git', ['branch', '--force', '--no-track', base, `origin/${base}`], ws);
}

/**
 * Rebases the checked-out branch on its base (origin/<base>, or the default branch: origin/HEAD,
 * which clone set) or merges the base into it. On a conflict it aborts, so the branch is as pushed,
 * and returns the conflicting files; any other failure throws.
 */
async function freshen(deps: PrepareDeps, ws: string, mode: FreshBase, onto?: string): Promise<string[]> {
  const base = onto
    ? `origin/${onto}`
    : (await deps.output('git', ['rev-parse', '--abbrev-ref', 'origin/HEAD'], { cwd: ws })).trim();
  if (!base.startsWith('origin/')) throw new Error(`origin/HEAD is ${base || 'not set'}`);
  return bringUpToDate(deps, ws, mode, base);
}

/** Rebases the checked-out branch on base or merges base into it, as freshen does. */
async function bringUpToDate(deps: PrepareDeps, ws: string, mode: FreshBase, base: string): Promise<string[]> {
  const args = mode === 'merge' ? ['merge', '--quiet', '--no-edit', base] : ['rebase', '--quiet', base];
  const code = await deps.exec('git', args, { cwd: ws });
  if (code === 0) return [];
  const unmerged = await deps.output('git', ['diff', '--name-only', '--diff-filter=U', '-z'], { cwd: ws });
  const conflicts = unmerged
    .split('\0')
    .filter((f) => f !== '')
    .sort();
  if (conflicts.length === 0) throw new Error(`git ${mode} exited ${code}`);
  await must(deps, 'git', [mode, '--abort'], ws);
  return conflicts;
}

/** The ref a try's change is read against once its agent ended (domain.PastTryStartRef in goald). */
export const tryStartRef = 'refs/goal/try-start';

/**
 * A try of a past item's workspace (ADR 0039 decision 9): a full clone, the recorded commit and
 * its base's fetched by SHA, the branch checked out at the commit and brought up to date with the
 * base commit as the original run's prepare did (a conflict is aborted and reported), then
 * tryStartRef at the result and the repository's own tools. A commit the remote no longer has
 * fails prepare, which goald records as the try's error.
 */
async function checkoutCommit(
  deps: PrepareDeps,
  repo: NonNullable<Bundle['repository']>,
  out: Prepared,
  mise: Record<string, string>,
): Promise<ClonedRepository> {
  const ws = deps.dirs.workspace;
  const commit = repo.commit as string;
  await must(deps, 'git', ['check-ref-format', '--branch', repo.branch]);
  const clone = await network(deps, ['clone', '--quiet', '--no-checkout', '--', repo.url, ws], { clean: ws });
  if (clone !== 0) throw new Error(`git clone exited ${clone}`);
  const wanted = repo.base_commit && repo.base_commit !== commit ? [commit, repo.base_commit] : [commit];
  const fetched = await network(deps, ['fetch', '--quiet', 'origin', ...wanted], { cwd: ws });
  if (fetched !== 0)
    throw new Error(`commit ${wanted.join(' or ')} is gone from the remote: git fetch exited ${fetched}`);
  await must(deps, 'git', ['checkout', '--quiet', '-B', repo.branch, commit], ws);
  if (repo.base_commit) {
    const behind =
      (await deps.exec('git', ['merge-base', '--is-ancestor', repo.base_commit, 'HEAD'], { cwd: ws })) !== 0;
    if (behind) {
      out.fresh_base = repo.fresh_base ?? 'rebase';
      const conflicts = await bringUpToDate(deps, ws, out.fresh_base, repo.base_commit);
      if (conflicts.length > 0) out.conflicts = conflicts;
    }
  }
  await must(deps, 'git', ['update-ref', tryStartRef, 'HEAD'], ws);
  await must(deps, 'mise', ['install', '--yes'], ws, mise);
  out.branch = repo.branch;
  out.branch_existed = true;
  return {
    url: repo.url,
    path: paths.workspace,
    branch: repo.branch,
    write: false,
    ...(repo.base_commit ? { base_commit: repo.base_commit } : {}),
    branch_head: commit,
  };
}

/**
 * Drops every git login prepare configured: the credential store under the box's home and each
 * host's credential helpers, so nothing the agent runs signs git in.
 */
async function dropLogins(deps: PrepareDeps, credentials: GitCredential[]): Promise<void> {
  await rm(join(deps.dirs.home, '.git-credentials'), { force: true });
  for (const host of new Set(credentials.map((c) => c.host))) {
    await must(deps, 'git', ['config', '--global', '--unset-all', `credential.https://${host}.helper`]);
  }
}

/**
 * A git credential helper that answers "get" with c's user name and the password in c's
 * variable, read when git asks: the password is in the environment of the command that runs
 * git, never on disk. An unset or empty variable answers nothing, so git fails without a prompt.
 */
export function credentialHelper(c: GitCredential): string {
  const v = c.password_env;
  return `!f() { if [ "$1" = get ] && [ -n "$${v}" ]; then printf 'username=%s\\npassword=%s\\n' '${c.username}' "$${v}"; fi; }; f`;
}

/**
 * Saves the stored logins (the repository token's, ADR 0037 decision 7) in git's credential store,
 * <home>/.git-credentials with mode 0600, outside /workspace and /goal, so the clone, the base's push
 * and the agent's own push sign in after prepare, when the agent's env has no password variable.
 * A login whose variable is unset or empty is left out, so git fails without a prompt. The box is
 * thrown away after its run, and the file with it. Returns the file's path.
 */
async function storeCredentials(deps: PrepareDeps, stored: GitCredential[]): Promise<string> {
  const file = join(deps.dirs.home, '.git-credentials');
  const lines = stored.flatMap((c) => {
    const password = deps.env[c.password_env];
    if (!password) return [];
    return [`https://${encodeURIComponent(c.username)}:${encodeURIComponent(password)}@${c.host}\n`];
  });
  if (lines.length === 0) return file;
  await mkdir(deps.dirs.home, { recursive: true });
  await writeFile(file, lines.join(''), { mode: 0o600 });
  await chmod(file, 0o600);
  return file;
}

async function fetchBundle(deps: PrepareDeps, url: URL, token: string): Promise<Bundle> {
  const res = await deps.fetch(url, { headers: { Authorization: `Bearer ${token}`, Accept: 'application/json' } });
  if (!res.ok) {
    const why = (await res.text()).trim().slice(0, 200);
    throw new Error(`GET ${url.pathname}: ${res.status}${why ? ` ${why}` : ''}`);
  }
  return Bundle.parse(await res.json());
}

/** A network git step through gitWithRetry: retried on the transport, with a time limit per attempt. */
function network(deps: PrepareDeps, args: string[], opts: { cwd?: string; clean?: string }): Promise<number> {
  return gitWithRetry(
    {
      exec: deps.exec,
      sleep: deps.sleep ?? ((ms) => new Promise((resolve) => setTimeout(resolve, ms))),
      log: deps.log ?? ((line) => process.stderr.write(`${line}\n`)),
      timeoutMs: attemptTimeout(deps.env) * 1000,
    },
    args,
    opts,
  );
}

function attemptTimeout(env: NodeJS.ProcessEnv): number {
  const v = env.GOAL_GIT_ATTEMPT_TIMEOUT;
  if (!v) return defaultAttemptTimeout;
  const n = Number(v);
  if (!Number.isFinite(n) || n <= 0)
    throw new Error(`GOAL_GIT_ATTEMPT_TIMEOUT is not a positive number of seconds: ${v}`);
  return n;
}

/**
 * The environment of every mise process prepare runs: quiet, so a failure's stderr opens with
 * mise's ERROR lines and not progress frames; and, when the run has a github.com login whose
 * variable is set, MISE_GITHUB_TOKEN (mise reads it before GITHUB_API_TOKEN and GITHUB_TOKEN), so
 * its tool downloads are signed in. Only mise gets it: it is passed per call, never put in process.env.
 */
function miseEnv(
  env: NodeJS.ProcessEnv,
  credentials: GitCredential[],
  logins: { prefix: string; password_env: string }[],
): Record<string, string> {
  const names = [
    ...credentials.filter((c) => c.host === 'github.com').map((c) => c.password_env),
    ...logins.filter((l) => new URL(l.prefix).hostname === 'github.com').map((l) => l.password_env),
  ];
  const token = names.map((n) => env[n]).find((v) => v);
  return token ? { MISE_QUIET: '1', MISE_GITHUB_TOKEN: token } : { MISE_QUIET: '1' };
}

async function must(
  deps: PrepareDeps,
  command: string,
  args: string[],
  cwd?: string,
  env?: Record<string, string>,
): Promise<void> {
  const code = await deps.exec(command, args, {
    ...(cwd === undefined ? {} : { cwd }),
    ...(env === undefined ? {} : { env }),
  });
  if (code !== 0) throw new Error(`${command} ${args[0]} exited ${code}`);
}

async function put(path: string, content: string | Uint8Array): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, content);
}

function required(env: NodeJS.ProcessEnv, name: string): string {
  const v = env[name];
  if (!v) throw new Error(`${name} is not set`);
  return v;
}
