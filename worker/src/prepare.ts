import { mkdir, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { Bundle, type GitCredential } from './spec.js';

/** One command prepare runs; the result is its exit code. Its output goes to stderr, never stdout. */
export type Exec = (command: string, args: string[], opts?: { cwd?: string }) => Promise<number>;

export interface PrepareDeps {
  fetch: typeof fetch;
  exec: Exec;
  env: NodeJS.ProcessEnv;
  now: () => Date;
  /** Where the bundle's files go (/goal) and the repository is cloned (/workspace). */
  dirs: { goal: string; workspace: string };
}

/** prepare's one stdout line: what it set up, for goald's run events. */
export interface Prepared {
  type: 'prepared';
  at: string;
  seconds: number;
  /** The item's branch checked out in the workspace; absent without a repository. */
  branch?: string;
  /** Whether that branch already existed on the remote: earlier runs of the item pushed it. */
  branch_existed?: boolean;
  /** How many of the definition's runtime.tools were installed. */
  tools: number;
}

/**
 * goal-agent prepare: fetches the run bundle from the gateway with the run's task token and
 * sets the sandbox up for `goal-agent run`. It writes /goal (spec.json and the agent's files),
 * configures a git credential helper per bundle credential, clones the repository into
 * /workspace on the item's branch (the remote one when it exists, otherwise new from the default
 * branch), and installs tools with mise: the repository's own (mise.toml, .tool-versions, …),
 * then the definition's runtime.tools. Any failure throws; nothing is retried.
 */
export async function prepare(deps: PrepareDeps): Promise<Prepared> {
  const started = deps.now();
  const gateway = required(deps.env, 'GOAL_GATEWAY_URL');
  const bundle = await fetchBundle(deps, new URL('/run', gateway), required(deps.env, 'GOAL_GATEWAY_TOKEN'));

  for (const [path, content] of Object.entries(bundle.files)) {
    await put(join(deps.dirs.goal, path), Buffer.from(content, 'base64'));
  }
  await put(join(deps.dirs.goal, 'spec.json'), JSON.stringify({ ...bundle.spec, gateway_url: gateway }));

  for (const c of bundle.git_credentials) {
    await must(deps, 'git', ['config', '--global', `credential.https://${c.host}.helper`, credentialHelper(c)]);
  }

  const out: Prepared = { type: 'prepared', at: '', seconds: 0, tools: bundle.tools.length };
  const repo = bundle.repository;
  if (repo) {
    const ws = deps.dirs.workspace;
    await must(deps, 'git', ['check-ref-format', '--branch', repo.branch]);
    await must(deps, 'git', ['clone', '--quiet', '--', repo.url, ws]);
    const remote = `refs/remotes/origin/${repo.branch}`;
    const existed = (await deps.exec('git', ['rev-parse', '--verify', '--quiet', remote], { cwd: ws })) === 0;
    const branch = existed ? ['-B', repo.branch, `origin/${repo.branch}`] : ['-b', repo.branch];
    await must(deps, 'git', ['checkout', '--quiet', ...branch], ws);
    // The repository's own tools; mise trusts configs under /workspace (MISE_TRUSTED_CONFIG_PATHS).
    await must(deps, 'mise', ['install', '--yes'], ws);
    out.branch = repo.branch;
    out.branch_existed = existed;
  }

  if (bundle.tools.length > 0) {
    // In the global config, so they are on PATH (mise's shims) wherever the agent works.
    await must(deps, 'mise', ['use', '--global', '--yes', ...bundle.tools]);
  }

  const done = deps.now();
  out.at = done.toISOString();
  out.seconds = Math.round((done.getTime() - started.getTime()) / 100) / 10;
  return out;
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
