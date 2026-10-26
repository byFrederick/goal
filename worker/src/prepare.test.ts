import { execFileSync, spawn } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { credentialHelper, type Exec, type Output, type PrepareDeps, prepare } from './prepare.js';

const gateway = 'http://goal-egress:3128/mcp';
const token = 'task-token-123';
const at = new Date('2026-10-24T10:00:00Z');
const later = new Date('2026-10-24T10:00:12.34Z');

const spec = {
  factory_id: 'f1',
  item_id: 'i1',
  run_id: 'r1',
  station: 'build',
  agent: 'engineer',
  skills: ['acme-conventions'],
  tools_allow: ['Read'],
  tools_deny: [],
  brief: 'Add a health endpoint',
};

const b64 = (s: string) => Buffer.from(s).toString('base64');

let root: string;
let home: string;
let dirs: PrepareDeps['dirs'];

beforeEach(async () => {
  root = await mkdtemp(join(tmpdir(), 'goal-prepare-'));
  home = join(root, 'home');
  await mkdir(home);
  dirs = { goal: join(root, 'goal'), workspace: join(root, 'workspace') };
});

afterEach(async () => {
  await rm(root, { recursive: true, force: true });
});

/** git with the test's own home and no system config, so the developer's config is never read or written. */
function gitEnv(): NodeJS.ProcessEnv {
  // The box's /etc/gitconfig gives git its identity; here the environment does.
  return {
    PATH: process.env.PATH,
    HOME: home,
    GIT_CONFIG_NOSYSTEM: '1',
    GIT_TERMINAL_PROMPT: '0',
    LC_ALL: 'C',
    GIT_AUTHOR_NAME: 'Goal agent',
    GIT_AUTHOR_EMAIL: 'goal-agent@example.com',
    GIT_COMMITTER_NAME: 'Goal agent',
    GIT_COMMITTER_EMAIL: 'goal-agent@example.com',
  };
}

function git(cwd: string, ...args: string[]): string {
  return execFileSync('git', ['-c', 'user.name=Test', '-c', 'user.email=test@example.com', ...args], {
    cwd,
    env: gitEnv(),
    encoding: 'utf8',
  }).trim();
}

/**
 * A bare repository with a commit on main and, when given, one on branch writing changed (path to content).
 * moved, when given, is committed to main after the branch was pushed: path to content.
 */
async function origin(
  branch?: string,
  opts: { changed?: Record<string, string>; moved?: Record<string, string> } = {},
): Promise<string> {
  const bare = join(root, 'origin.git');
  const work = join(root, 'seed');
  execFileSync('git', ['init', '--quiet', '--bare', '--initial-branch=main', bare], { env: gitEnv() });
  execFileSync('git', ['init', '--quiet', '--initial-branch=main', work], { env: gitEnv() });
  git(work, 'remote', 'add', 'origin', bare);
  git(work, 'commit', '--quiet', '--allow-empty', '-m', 'first');
  git(work, 'push', '--quiet', 'origin', 'HEAD:main');
  if (branch) {
    git(work, 'checkout', '--quiet', '-b', branch);
    const changed = opts.changed ?? { 'branch.txt': 'from the branch\n' };
    for (const [path, content] of Object.entries(changed)) await writeFile(join(work, path), content);
    git(work, 'add', '--all');
    git(work, 'commit', '--quiet', '-m', 'earlier run');
    git(work, 'push', '--quiet', 'origin', branch);
  }
  if (opts.moved) {
    git(work, 'checkout', '--quiet', 'main');
    for (const [path, content] of Object.entries(opts.moved)) await writeFile(join(work, path), content);
    git(work, 'add', '--all');
    git(work, 'commit', '--quiet', '-m', 'main moved on');
    git(work, 'push', '--quiet', 'origin', 'main');
  }
  return `file://${bare}`;
}

/** Runs git for real (isolated), records every command and fakes mise with exit code `mise`. */
function recorder(mise = 0) {
  const calls: { command: string; args: string[]; cwd?: string }[] = [];
  const exec: Exec = (command, args, opts = {}) => {
    calls.push(opts.cwd === undefined ? { command, args } : { command, args, cwd: opts.cwd });
    if (command !== 'git') return Promise.resolve(mise);
    return new Promise((resolve) => {
      const child = spawn(command, args, { cwd: opts.cwd, env: gitEnv(), stdio: 'ignore' });
      child.once('close', (code) => resolve(code ?? 1));
    });
  };
  return { exec, calls };
}

/** Runs git for real (isolated) and returns its stdout. */
const output: Output = async (command, args, opts = {}) =>
  execFileSync(command, args, { cwd: opts.cwd, env: gitEnv(), encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] });

/** A gateway that answers GET /run with body (or status) and records the requests. */
function gatewayServing(body: unknown, status = 200) {
  const requests: { url: string; auth: string | null }[] = [];
  const fake = (async (input: string | URL | Request, init?: RequestInit) => {
    requests.push({ url: String(input), auth: new Headers(init?.headers).get('Authorization') });
    return new Response(typeof body === 'string' ? body : JSON.stringify(body), { status });
  }) as typeof fetch;
  return { fake, requests };
}

function depsWith(fake: typeof fetch, exec: Exec, env: NodeJS.ProcessEnv = {}): PrepareDeps {
  const times = [at, later];
  return {
    fetch: fake,
    exec,
    output,
    env: { GOAL_GATEWAY_URL: gateway, GOAL_GATEWAY_TOKEN: token, ...env },
    now: () => times.shift() ?? later,
    dirs,
  };
}

describe('prepare', () => {
  it("sets the sandbox up from the bundle goald's gateway serves (internal/gateway/testdata)", async () => {
    const served = JSON.parse(
      await readFile(new URL('../../internal/gateway/testdata/run-bundle.json', import.meta.url), 'utf8'),
    );
    const { fake } = gatewayServing(served);
    const calls: { command: string; args: string[] }[] = [];
    const exec: Exec = async (command, args) => {
      calls.push({ command, args });
      return command === 'git' && args[0] === 'rev-parse' ? 1 : 0;
    };

    const prepared = await prepare(depsWith(fake, exec));

    expect(JSON.parse(await readFile(join(dirs.goal, 'spec.json'), 'utf8'))).toEqual({
      ...served.spec,
      gateway_url: gateway,
    });
    expect(await readFile(join(dirs.goal, 'agent/prompt.md'), 'utf8')).toBe('# engineer\n');
    expect(await readFile(join(dirs.goal, 'agent/output.schema.json'), 'utf8')).toBe('{"type":"object"}');
    expect(calls.map((c) => `${c.command} ${c.args.join(' ')}`)).toEqual([
      `git config --global --add credential.https://github.com.helper ${credentialHelper(served.git_credentials[0])}`,
      'git check-ref-format --branch goal/1-add-a-health-endpoint',
      `git clone --quiet -- https://github.com/acme/api.git ${dirs.workspace}`,
      'git rev-parse --verify --quiet refs/remotes/origin/goal/1-add-a-health-endpoint',
      'git checkout --quiet -b goal/1-add-a-health-endpoint',
      'mise install --yes',
      'mise use --global --yes task@3.46.1 npm:@acme/issues-cli@2.0.0',
    ]);
    expect(prepared).toMatchObject({ branch: 'goal/1-add-a-health-endpoint', branch_existed: false, tools: 2 });
  });

  it('signs git in to one host with whichever connection the exec has: each helper answers only for its own variable', async () => {
    const repository = { host: 'github.com', username: 'x-access-token', password_env: 'CODE_TOKEN' };
    const agents = { host: 'github.com', username: 'x-access-token', password_env: 'GH_TOKEN' };
    const { fake } = gatewayServing({ spec, files: {}, git_credentials: [repository, agents] });
    const { exec } = recorder();

    await prepare(depsWith(fake, exec));

    const fill = (env: NodeJS.ProcessEnv) =>
      execFileSync('git', ['credential', 'fill'], {
        env: { ...gitEnv(), ...env },
        input: 'protocol=https\nhost=github.com\n\n',
        encoding: 'utf8',
      });
    expect(fill({ CODE_TOKEN: 'clone-s3cr3t' })).toContain('password=clone-s3cr3t');
    expect(fill({ GH_TOKEN: 'push-s3cr3t' })).toContain('password=push-s3cr3t');
  });

  it("writes the run bundle to /goal with the gateway's URL in spec.json", async () => {
    const files = {
      'agent/prompt.md': b64('You are the engineer.\n'),
      'agent/skills/acme-conventions/SKILL.md': b64('# acme\n'),
    };
    const { fake, requests } = gatewayServing({ spec, files, git_credentials: [] });
    const { exec, calls } = recorder();

    const prepared = await prepare(depsWith(fake, exec));

    expect(requests).toEqual([{ url: 'http://goal-egress:3128/run', auth: `Bearer ${token}` }]);
    expect(await readFile(join(dirs.goal, 'agent/prompt.md'), 'utf8')).toBe('You are the engineer.\n');
    expect(await readFile(join(dirs.goal, 'agent/skills/acme-conventions/SKILL.md'), 'utf8')).toBe('# acme\n');
    expect(JSON.parse(await readFile(join(dirs.goal, 'spec.json'), 'utf8'))).toEqual({
      ...spec,
      connections: [],
      gateway_url: gateway,
    });
    expect(prepared).toEqual({ type: 'prepared', at: later.toISOString(), seconds: 12.3, tools: 0 });
    // No repository: nothing cloned, no repository tools.
    expect(calls).toEqual([]);
  });

  it("clones the repository on the item's branch when earlier runs pushed it", async () => {
    const url = await origin('goal/7-add-a-health-endpoint');
    const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch: 'goal/7-add-a-health-endpoint' } });
    const { exec } = recorder();

    const prepared = await prepare(depsWith(fake, exec));

    expect(prepared).toMatchObject({ branch: 'goal/7-add-a-health-endpoint', branch_existed: true });
    expect(git(dirs.workspace, 'branch', '--show-current')).toBe('goal/7-add-a-health-endpoint');
    expect(git(dirs.workspace, 'log', '-1', '--format=%s')).toBe('earlier run');
  });

  it('starts the branch from the default branch when the remote has none', async () => {
    const url = await origin();
    const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch: 'goal/8-new-work' } });
    const { exec } = recorder();

    const prepared = await prepare(depsWith(fake, exec));

    expect(prepared).toMatchObject({ branch: 'goal/8-new-work', branch_existed: false });
    expect(prepared).not.toHaveProperty('fresh_base');
    expect(git(dirs.workspace, 'branch', '--show-current')).toBe('goal/8-new-work');
    expect(git(dirs.workspace, 'log', '-1', '--format=%s')).toBe('first');
  });

  it('rebases an existing branch on the default branch', async () => {
    const branch = 'goal/10-rebase';
    const url = await origin(branch, { moved: { 'main.txt': 'from main\n' } });
    const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch } });
    const { exec, calls } = recorder();

    const prepared = await prepare(depsWith(fake, exec));

    expect(prepared).toMatchObject({ branch, branch_existed: true, fresh_base: 'rebase' });
    expect(prepared).not.toHaveProperty('conflicts');
    expect(calls).toContainEqual({ command: 'git', args: ['rebase', '--quiet', 'origin/main'], cwd: dirs.workspace });
    const ws = dirs.workspace;
    expect(git(ws, 'branch', '--show-current')).toBe(branch);
    expect(git(ws, 'merge-base', '--is-ancestor', 'origin/main', 'HEAD')).toBe('');
    expect(git(ws, 'log', '--format=%s', 'origin/main..HEAD')).toBe('earlier run');
    expect(git(ws, 'rev-list', '--parents', '-1', 'HEAD').split(' ')).toHaveLength(2);
    expect(JSON.parse(await readFile(join(dirs.goal, 'spec.json'), 'utf8'))).not.toHaveProperty('base_conflicts');
  });

  it('merges the default branch when fresh_base is merge', async () => {
    const branch = 'goal/11-merge';
    const url = await origin(branch, { moved: { 'main.txt': 'from main\n' } });
    const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch, fresh_base: 'merge' } });
    const { exec } = recorder();

    const prepared = await prepare(depsWith(fake, exec));

    expect(prepared).toMatchObject({ branch, branch_existed: true, fresh_base: 'merge' });
    const ws = dirs.workspace;
    const [, ...parents] = git(ws, 'rev-list', '--parents', '-1', 'HEAD').split(' ');
    expect(parents).toEqual([git(ws, 'rev-parse', `origin/${branch}`), git(ws, 'rev-parse', 'origin/main')]);
  });

  for (const fresh_base of ['rebase', 'merge'] as const) {
    it(`reports conflicting files and leaves the branch as pushed (${fresh_base})`, async () => {
      const branch = `goal/12-conflict-${fresh_base}`;
      const url = await origin(branch, {
        changed: { 'z.txt': 'from the branch\n', 'b.txt': 'from the branch\n' },
        moved: { 'z.txt': 'from main\n', 'b.txt': 'from main\n', 'a.txt': 'no conflict\n' },
      });
      const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch, fresh_base } });
      const { exec } = recorder();

      const prepared = await prepare(depsWith(fake, exec));

      expect(prepared).toMatchObject({ branch, branch_existed: true, fresh_base, conflicts: ['b.txt', 'z.txt'] });
      const ws = dirs.workspace;
      expect(git(ws, 'branch', '--show-current')).toBe(branch);
      expect(git(ws, 'rev-parse', 'HEAD')).toBe(git(ws, 'rev-parse', `origin/${branch}`));
      expect(git(ws, 'status', '--porcelain')).toBe('');
      expect(JSON.parse(await readFile(join(dirs.goal, 'spec.json'), 'utf8'))).toMatchObject({
        base_conflicts: ['b.txt', 'z.txt'],
      });
    });
  }

  it('fails when bringing the branch up to date fails for another reason than a conflict', async () => {
    const branch = 'goal/13-fails';
    const url = await origin(branch, { moved: { 'main.txt': 'from main\n' } });
    const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch } });
    const { exec: real } = recorder();
    const exec: Exec = (command, args, opts) =>
      args[0] === 'rebase' ? Promise.resolve(128) : real(command, args, opts);

    await expect(prepare(depsWith(fake, exec))).rejects.toThrow('git rebase exited 128');
  });

  it("installs the repository's tools with mise, then the definition's runtime.tools", async () => {
    const url = await origin();
    const tools = ['go@1.26.10', 'task@3.46.1'];
    const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch: 'goal/9-x' }, tools });
    const { exec, calls } = recorder();

    const prepared = await prepare(depsWith(fake, exec));

    expect(calls.filter((c) => c.command === 'mise')).toEqual([
      { command: 'mise', args: ['install', '--yes'], cwd: dirs.workspace },
      { command: 'mise', args: ['use', '--global', '--yes', ...tools] },
    ]);
    expect(prepared.tools).toBe(2);
  });

  it('installs runtime.tools without a repository too', async () => {
    const { fake } = gatewayServing({ spec, files: {}, tools: ['python@3.14.8'] });
    const { exec, calls } = recorder();

    await prepare(depsWith(fake, exec));

    expect(calls).toEqual([{ command: 'mise', args: ['use', '--global', '--yes', 'python@3.14.8'] }]);
  });

  it('configures a credential helper per host that reads the password from the environment', async () => {
    const credential = { host: 'github.com', username: 'x-access-token', password_env: 'GOAL_REPOSITORY_TOKEN' };
    const { fake } = gatewayServing({ spec, files: {}, git_credentials: [credential] });
    const { exec } = recorder();

    await prepare(depsWith(fake, exec));

    const helper = execFileSync('git', ['config', '--global', '--get', 'credential.https://github.com.helper'], {
      env: gitEnv(),
      encoding: 'utf8',
    }).trim();
    expect(helper).toBe(credentialHelper(credential));
    expect(helper).not.toContain('s3cr3t');

    // git runs a "!" helper as sh -c '<helper> "$@"' with the action as its argument.
    const ask = (action: string, env: NodeJS.ProcessEnv) =>
      execFileSync('sh', ['-c', `${helper.slice(1)} "$@"`, 'helper', action], { env, encoding: 'utf8' });
    expect(ask('get', { GOAL_REPOSITORY_TOKEN: 'acme-s3cr3t' })).toBe(
      'username=x-access-token\npassword=acme-s3cr3t\n',
    );
    expect(ask('get', {})).toBe('');
    expect(ask('store', { GOAL_REPOSITORY_TOKEN: 'acme-s3cr3t' })).toBe('');
  });

  it('fails when the gateway refuses the bundle', async () => {
    const { fake } = gatewayServing('forbidden\n', 403);
    await expect(prepare(depsWith(fake, recorder().exec))).rejects.toThrow('GET /run: 403 forbidden');
  });

  it('fails without the gateway URL or token', async () => {
    const { fake, requests } = gatewayServing({ spec, files: {} });
    await expect(prepare(depsWith(fake, recorder().exec, { GOAL_GATEWAY_TOKEN: '' }))).rejects.toThrow(
      'GOAL_GATEWAY_TOKEN is not set',
    );
    expect(requests).toEqual([]);
  });

  it('refuses a bundle that would write outside /goal or pass an option to git or mise', async () => {
    for (const bundle of [
      { spec, files: { '../etc/profile': b64('x') } },
      { spec, files: { 'agent/../../x': b64('x') } },
      { spec, files: { '/etc/profile': b64('x') } },
      { spec, files: {}, repository: { url: '--upload-pack=sh', branch: 'goal/1-x' } },
      { spec, files: {}, tools: ['--yes'] },
      { spec, files: {}, git_credentials: [{ host: 'github.com', username: "x'; rm -rf ~; '", password_env: 'T' }] },
    ]) {
      const { exec, calls } = recorder();
      await expect(prepare(depsWith(gatewayServing(bundle).fake, exec))).rejects.toThrow();
      expect(calls).toEqual([]);
    }
  });

  it('fails when the clone fails', async () => {
    const missing = `file://${join(root, 'no-such.git')}`;
    const { fake } = gatewayServing({ spec, files: {}, repository: { url: missing, branch: 'goal/1-x' } });
    await expect(prepare(depsWith(fake, recorder().exec))).rejects.toThrow('git clone exited 128');
  });
});
