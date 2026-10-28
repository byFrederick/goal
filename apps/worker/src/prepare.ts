import { createHash } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { gitWithRetry } from './retry.js';
import { Bundle, type GitCredential } from './spec.js';

/**
 * One command prepare runs; the result is its exit code. Its output goes to stderr, never stdout.
 * With signal, the command is ended when it fires; with stderr, its stderr is also passed there.
 */
export type Exec = (
  command: string,
  args: string[],
  opts?: { cwd?: string; signal?: AbortSignal; stderr?: (chunk: string) => void },
) => Promise<number>;

/** One command whose stdout prepare reads; it throws when the command fails. Its stderr goes to stderr. */
export type Output = (command: string, args: string[], opts?: { cwd?: string }) => Promise<string>;

export interface PrepareDeps {
  fetch: typeof fetch;
  exec: Exec;
  output: Output;
  env: NodeJS.ProcessEnv;
  now: () => Date;
  /** Where the bundle's files go (/goal), the repository is cloned (/workspace) and Claude Code's config dir (claude). */
  dirs: { goal: string; workspace: string; claude: string };
  /** The wait between attempts of a network git step; setTimeout when absent. */
  sleep?: (ms: number) => Promise<void>;
  /** Where a retry is logged; stderr when absent. */
  log?: (line: string) => void;
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
}

type FreshBase = NonNullable<NonNullable<Bundle['repository']>['fresh_base']>;

/**
 * goal-agent prepare: fetches the run bundle from the gateway with the run's task token and
 * sets the sandbox up for `goal-agent run`. It writes /goal (spec.json and the agent's files),
 * configures a git credential helper per bundle credential, clones the repository into
 * /workspace on the item's branch (the remote one when it exists, otherwise new from the base: the
 * bundle's repository.base, created from the default branch when missing, or the default branch),
 * brings an existing branch up to date with that base (fresh_base: rebase or merge; a conflict is
 * aborted and reported, not failed), and installs tools with mise: the
 * repository's own (mise.toml, .tool-versions, …), then the definition's runtime.tools. The factory's rules
 * go to the user-level rules in the config dir, never into /workspace, so they cannot be committed.
 * In definition mode (the bundle's definition, ADR 0021) /workspace gets the factory's draft files
 * instead, /goal/definition.json their starting sha256, and no git command runs.
 * The clone is partial (--filter=blob:none: history without file contents, fetched when checked
 * out). The network git steps (the clone, and the base branch's push and fetch) run with a time limit
 * per attempt (GOAL_GIT_ATTEMPT_TIMEOUT seconds, 300 by default); an attempt cut off at it or failed
 * on the transport (a dropped connection, a 5xx) is retried up to 3 attempts, the clone from an
 * empty workspace. Any other failure throws.
 */
export async function prepare(deps: PrepareDeps): Promise<Prepared> {
  const started = deps.now();
  const gateway = required(deps.env, 'GOAL_GATEWAY_URL');
  const bundle = await fetchBundle(deps, new URL('/run', gateway), required(deps.env, 'GOAL_GATEWAY_TOKEN'));

  for (const [path, content] of Object.entries(bundle.files)) {
    await put(join(deps.dirs.goal, path), Buffer.from(content, 'base64'));
  }
  // Each rule is the complete file (frontmatter included); its name is a slug, so it stays in rules/.
  for (const rule of bundle.spec.rules ?? []) {
    await put(join(deps.dirs.claude, 'rules', `${rule.name}.md`), rule.content);
  }
  // Definition mode has no repository: nothing signs git in.
  for (const c of bundle.definition ? [] : bundle.git_credentials) {
    // --add: two connections may sign git in to one host; git asks each helper until one answers.
    await must(deps, 'git', [
      'config',
      '--global',
      '--add',
      `credential.https://${c.host}.helper`,
      credentialHelper(c),
    ]);
  }

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
  if (repo) {
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
    if (existed) {
      out.fresh_base = repo.fresh_base ?? 'rebase';
      const conflicts = await freshen(deps, ws, out.fresh_base, base);
      if (conflicts.length > 0) {
        out.conflicts = conflicts;
        baseBranch = base;
      }
    }
    // The repository's own tools; mise trusts configs under /workspace (MISE_TRUSTED_CONFIG_PATHS).
    await must(deps, 'mise', ['install', '--yes'], ws);
    out.branch = repo.branch;
    out.branch_existed = existed;
  }

  const spec = {
    ...bundle.spec,
    gateway_url: gateway,
    ...(out.conflicts ? { base_conflicts: out.conflicts } : {}),
    ...(baseBranch ? { base_branch: baseBranch } : {}),
  };
  await put(join(deps.dirs.goal, 'spec.json'), JSON.stringify(spec));

  if (bundle.tools.length > 0) {
    // In the global config, so they are on PATH (mise's shims) wherever the agent works.
    await must(deps, 'mise', ['use', '--global', '--yes', ...bundle.tools]);
  }

  const done = deps.now();
  out.at = done.toISOString();
  out.seconds = Math.round((done.getTime() - started.getTime()) / 100) / 10;
  return out;
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

/**
 * A git credential helper that answers "get" with c's user name and the password in c's
 * variable, read when git asks: the password is in the environment of the command that runs
 * git, never on disk. An unset or empty variable answers nothing, so git fails without a prompt.
 */
export function credentialHelper(c: GitCredential): string {
  const v = c.password_env;
  return `!f() { if [ "$1" = get ] && [ -n "$${v}" ]; then printf 'username=%s\\npassword=%s\\n' '${c.username}' "$${v}"; fi; }; f`;
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

async function must(deps: PrepareDeps, command: string, args: string[], cwd?: string): Promise<void> {
  const code = await deps.exec(command, args, cwd === undefined ? {} : { cwd });
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
