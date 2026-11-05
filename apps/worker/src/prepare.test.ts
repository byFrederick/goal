import { execFileSync, spawn, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { existsSync, readdirSync } from 'node:fs';
import { chmod, mkdir, mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { crc32 } from 'node:zlib';
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
  dirs = {
    goal: join(root, 'goal'),
    workspace: join(root, 'workspace'),
    repos: join(root, 'repos'),
    claude: join(home, '.claude'),
    home,
    knowledge: join(root, 'knowledge'),
  };
});

afterEach(async () => {
  // The unpacked knowledge is read-only: its owner makes it writable again to remove it.
  execFileSync('chmod', ['-R', 'u+w', root]);
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
 * base, when given, is a branch of its own from main's first commit, with one commit writing base.txt.
 */
async function origin(
  branch?: string,
  opts: { changed?: Record<string, string>; moved?: Record<string, string>; base?: string; name?: string } = {},
): Promise<string> {
  const name = opts.name ?? 'origin';
  const bare = join(root, `${name}.git`);
  const work = join(root, name === 'origin' ? 'seed' : `${name}-seed`);
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
  if (opts.base) {
    git(work, 'checkout', '--quiet', '-b', opts.base, git(work, 'rev-list', '--max-parents=0', 'main'));
    await writeFile(join(work, 'base.txt'), 'from the base\n');
    git(work, 'add', '--all');
    git(work, 'commit', '--quiet', '-m', 'on the base');
    git(work, 'push', '--quiet', 'origin', opts.base);
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

/**
 * What `git credential fill` answers for https://<host> in the test's git environment, which holds
 * no password variable; empty when nothing signs in (no prompt, since GIT_TERMINAL_PROMPT is 0).
 */
function fill(host: string, cwd?: string): string {
  try {
    return execFileSync('git', ['credential', 'fill'], {
      cwd,
      env: { ...gitEnv(), GIT_ASKPASS: '' },
      input: `protocol=https\nhost=${host}\n\n`,
      encoding: 'utf8',
      stdio: ['pipe', 'pipe', 'ignore'],
    });
  } catch {
    return '';
  }
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

/** A zip of files (path to content), stored without compression, as unzip reads it. */
function zipOf(files: Record<string, string>): Buffer {
  const locals: Buffer[] = [];
  const centrals: Buffer[] = [];
  let offset = 0;
  for (const [path, content] of Object.entries(files)) {
    const name = Buffer.from(path);
    const body = Buffer.from(content);
    const crc = crc32(body);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(body.length, 18);
    local.writeUInt32LE(body.length, 22);
    local.writeUInt16LE(name.length, 26);
    const central = Buffer.alloc(46);
    central.writeUInt32LE(0x02014b50, 0);
    central.writeUInt16LE(20, 4);
    central.writeUInt16LE(20, 6);
    central.writeUInt32LE(crc, 16);
    central.writeUInt32LE(body.length, 20);
    central.writeUInt32LE(body.length, 24);
    central.writeUInt16LE(name.length, 28);
    central.writeUInt32LE(offset, 42);
    locals.push(local, name, body);
    centrals.push(central, name);
    offset += local.length + name.length + body.length;
  }
  const directory = Buffer.concat(centrals);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(Object.keys(files).length, 8);
  end.writeUInt16LE(Object.keys(files).length, 10);
  end.writeUInt32LE(directory.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...locals, directory, end]);
}

/** Runs unzip for real, records every command and answers the others with 0 (git rev-parse: no such branch). */
function unzipping(before?: (command: string, args: string[]) => Promise<void>) {
  const calls: string[] = [];
  const exec: Exec = async (command, args) => {
    calls.push(`${command} ${args.join(' ')}`);
    await before?.(command, args);
    if (command === 'git' && args[0] === 'rev-parse') return 1;
    if (command !== 'unzip') return 0;
    return new Promise((resolve) => {
      const child = spawn(command, args, { stdio: 'ignore' });
      child.once('close', (c) => resolve(c ?? 1));
    });
  };
  return { exec, calls };
}

const handbookZip = zipOf({ 'README.md': '# Handbook', 'onboarding/first-week.md': '# First week' });
const runbooksZip = zipOf({ 'alerts/disk.md': '# Disk' });

const knowledge = {
  section: '## Knowledge',
  snapshots: [
    { source: 'handbook', url: '/run/knowledge/handbook' },
    { source: 'runbooks', url: '/run/knowledge/runbooks' },
  ],
};

/** A gateway that serves bundle at /run and each zip of zips at /run/knowledge/<source>, recording what was asked. */
function knowledgeGateway(bundle: unknown, zips: Record<string, Buffer | number>, onRequest?: () => void) {
  const requests: { url: string; auth: string | null }[] = [];
  const fake = (async (input: string | URL | Request, init?: RequestInit) => {
    const url = new URL(String(input));
    requests.push({ url: String(input), auth: new Headers(init?.headers).get('Authorization') });
    onRequest?.();
    if (url.pathname === '/run') return new Response(JSON.stringify(bundle));
    const zip = zips[url.pathname.replace('/run/knowledge/', '')];
    if (zip === undefined || typeof zip === 'number') return new Response('not found', { status: zip ?? 404 });
    return new Response(new Uint8Array(zip), { headers: { 'Content-Type': 'application/zip' } });
  }) as typeof fetch;
  return { fake, requests };
}

/** The permission bits of path. */
async function mode(path: string): Promise<number> {
  return (await stat(path)).mode & 0o777;
}

describe('prepare: knowledge', () => {
  it('downloads every snapshot while the repository clones and unpacks each read-only into /knowledge/<source>/', async () => {
    const bundle = {
      spec: { ...spec, knowledge },
      files: {},
      repository: { url: 'https://github.com/acme/api.git', branch: 'goal/1-x' },
    };
    // The clone ends only once both snapshots were asked for: a prepare that waited for it first never would.
    let asked: () => void = () => undefined;
    const bothAsked = new Promise<void>((resolve) => {
      asked = resolve;
    });
    const { fake, requests } = knowledgeGateway(bundle, { handbook: handbookZip, runbooks: runbooksZip }, () => {
      if (requests.filter((r) => r.url.includes('/run/knowledge/')).length === 2) asked();
    });
    const { exec } = unzipping(async (command, args) => {
      if (command === 'git' && args[0] === 'clone') await bothAsked;
    });

    // A box prepared again finds an earlier, read-only unpack; the new one replaces it.
    await mkdir(join(dirs.knowledge, 'handbook'), { recursive: true });
    await writeFile(join(dirs.knowledge, 'handbook', 'stale.md'), 'stale');
    await chmod(join(dirs.knowledge, 'handbook', 'stale.md'), 0o444);
    await chmod(join(dirs.knowledge, 'handbook'), 0o555);

    const prepared = await prepare(depsWith(fake, exec));

    expect(requests.filter((r) => r.url.includes('/run/knowledge/'))).toEqual([
      { url: 'http://goal-egress:3128/run/knowledge/handbook', auth: `Bearer ${token}` },
      { url: 'http://goal-egress:3128/run/knowledge/runbooks', auth: `Bearer ${token}` },
    ]);
    expect(await readFile(join(dirs.knowledge, 'handbook', 'onboarding', 'first-week.md'), 'utf8')).toBe(
      '# First week',
    );
    expect(await readFile(join(dirs.knowledge, 'runbooks', 'alerts', 'disk.md'), 'utf8')).toBe('# Disk');
    expect(existsSync(join(dirs.knowledge, 'handbook', 'stale.md'))).toBe(false);
    expect(existsSync(join(dirs.knowledge, '.handbook.zip'))).toBe(false);
    for (const file of ['handbook/README.md', 'handbook/onboarding/first-week.md', 'runbooks/alerts/disk.md']) {
      expect(await mode(join(dirs.knowledge, file)), file).toBe(0o444);
    }
    for (const dir of ['handbook', 'handbook/onboarding', 'runbooks', 'runbooks/alerts']) {
      expect(await mode(join(dirs.knowledge, dir)), dir).toBe(0o555);
    }
    expect(prepared.knowledge).toBe(2);
    expect(JSON.parse(await readFile(join(dirs.goal, 'spec.json'), 'utf8')).knowledge).toEqual(knowledge);
  });

  it('fails the run with the source and the status when a snapshot does not download', async () => {
    const bundle = { spec: { ...spec, knowledge }, files: {} };
    const { fake } = knowledgeGateway(bundle, { handbook: handbookZip, runbooks: 404 });
    await expect(prepare(depsWith(fake, unzipping().exec))).rejects.toThrow(
      'knowledge runbooks: GET /run/knowledge/runbooks: 404 not found',
    );
  });

  it('fails the run when a snapshot does not unpack', async () => {
    const bundle = { spec: { ...spec, knowledge: { ...knowledge, snapshots: [knowledge.snapshots[0]] } }, files: {} };
    const { fake } = knowledgeGateway(bundle, { handbook: Buffer.from('not a zip') });
    await expect(prepare(depsWith(fake, unzipping().exec))).rejects.toThrow(/knowledge handbook: unzip exited [1-9]/);
  });

  it('sends the task token only to the gateway', async () => {
    const elsewhere = { source: 'handbook', url: 'https://bucket.example.com/handbook.zip?signature=x' };
    const bundle = { spec: { ...spec, knowledge: { section: '', snapshots: [elsewhere] } }, files: {} };
    const requests: { url: string; auth: string | null }[] = [];
    const fake = (async (input: string | URL | Request, init?: RequestInit) => {
      requests.push({ url: String(input), auth: new Headers(init?.headers).get('Authorization') });
      return String(input).endsWith('/run')
        ? new Response(JSON.stringify(bundle))
        : new Response(new Uint8Array(handbookZip));
    }) as typeof fetch;

    await prepare(depsWith(fake, unzipping().exec));

    expect(requests[1]).toEqual({ url: elsewhere.url, auth: null });
  });

  it('downloads nothing for a bundle without snapshots, as a draft request has', async () => {
    const bundle = { spec: { ...spec, knowledge: { section: '## Knowledge', snapshots: [] } }, files: {} };
    const { fake, requests } = knowledgeGateway(bundle, {});
    const { exec, calls } = unzipping();

    const prepared = await prepare(depsWith(fake, exec));

    expect(requests).toHaveLength(1);
    expect(calls).toEqual([]);
    expect(prepared.knowledge).toBeUndefined();
  });
});

describe('prepare', () => {
  it("sets the sandbox up from the bundle goald's gateway serves (internal/gateway/testdata)", async () => {
    const served = JSON.parse(
      await readFile(new URL('../../../internal/gateway/testdata/run-bundle.json', import.meta.url), 'utf8'),
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
      branch: 'goal/1-add-a-health-endpoint',
      repositories: served.repositories,
    });
    expect(await readFile(join(dirs.goal, 'agent/prompt.md'), 'utf8')).toBe('# engineer\n');
    expect(await readFile(join(dirs.goal, 'agent/output.schema.json'), 'utf8')).toBe('{"type":"object"}');
    // The bundle's repositories: the first clones into /workspace with the repository's fresh_base.
    expect(calls.map((c) => `${c.command} ${c.args.join(' ')}`)).toEqual([
      'git config --global credential.useHttpPath true',
      `git config --global --add credential.https://github.com.helper ${credentialHelper(served.git_credentials[0])}`,
      'git check-ref-format --branch goal/1-add-a-health-endpoint',
      `git clone --quiet --filter=blob:none -- https://github.com/acme/api ${dirs.workspace}`,
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
    // No repository: nothing cloned, no repository tools, no heads.
    expect(calls).toEqual([]);
  });

  it("writes the factory's rules to the user-level rules, never into the workspace", async () => {
    const url = await origin();
    const goErrors = '---\npaths:\n  - "**/*.go"\n---\nWrap with %w.\n';
    const tone = 'Say what you did.\n';
    const rules = [
      { name: 'go-errors', sha256: 'a'.repeat(64), paths: ['**/*.go'], content: goErrors },
      { name: 'tone', sha256: 'b'.repeat(64), content: tone },
    ];
    const { fake } = gatewayServing({
      spec: { ...spec, rules },
      files: {},
      repository: { url, branch: 'goal/9-rules' },
    });

    await prepare(depsWith(fake, recorder().exec));

    expect(await readFile(join(dirs.claude, 'rules/go-errors.md'), 'utf8')).toBe(goErrors);
    expect(await readFile(join(dirs.claude, 'rules/tone.md'), 'utf8')).toBe(tone);
    // Nothing of the rules lands in the checkout, so nothing of them can be committed.
    expect(existsSync(join(dirs.workspace, '.claude'))).toBe(false);
    expect(git(dirs.workspace, 'status', '--porcelain')).toBe('');
  });

  it('writes no rules directory for a bundle without rules', async () => {
    const { fake } = gatewayServing({ spec, files: {} });

    await prepare(depsWith(fake, recorder().exec));

    expect(existsSync(join(dirs.claude, 'rules'))).toBe(false);
  });

  it.each(['../escape', 'nested/rule', 'Upper'])('refuses a rule named %s before anything is written', async (name) => {
    const rules = [{ name, sha256: 'a'.repeat(64), content: 'x\n' }];
    const { fake } = gatewayServing({ spec: { ...spec, rules }, files: {} });

    await expect(prepare(depsWith(fake, recorder().exec))).rejects.toThrow();

    expect(existsSync(join(dirs.claude, 'rules'))).toBe(false);
    expect(existsSync(join(root, 'escape.md'))).toBe(false);
    expect(existsSync(join(dirs.goal, 'spec.json'))).toBe(false);
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

  it("reports the branch's and the base's heads from before the rebase, and writes the branch into spec.json", async () => {
    const branch = 'goal/11-heads';
    const url = await origin(branch, { moved: { 'main.txt': 'from main\n' } });
    const bare = join(root, 'origin.git');
    const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch } });
    const { exec } = recorder();

    const prepared = await prepare(depsWith(fake, exec));

    const pushed = git(bare, 'rev-parse', `refs/heads/${branch}`);
    expect(prepared).toMatchObject({ branch_head: pushed, base_head: git(bare, 'rev-parse', 'refs/heads/main') });
    // The rebase moved the branch past the head it reports.
    expect(git(dirs.workspace, 'rev-parse', 'HEAD')).not.toBe(pushed);
    expect(JSON.parse(await readFile(join(dirs.goal, 'spec.json'), 'utf8'))).toMatchObject({ branch });
  });

  it("reports a new branch's head as the default branch's", async () => {
    const url = await origin();
    const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch: 'goal/12-new' } });
    const { exec } = recorder();

    const prepared = await prepare(depsWith(fake, exec));

    const main = git(join(root, 'origin.git'), 'rev-parse', 'refs/heads/main');
    expect(prepared).toMatchObject({ branch_head: main, base_head: main });
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

  describe('with a base branch', () => {
    const base = 'goal/assembly-330';
    const bare = () => join(root, 'origin.git');
    const spec_ = async () => JSON.parse(await readFile(join(dirs.goal, 'spec.json'), 'utf8'));

    it('starts the branch from the base branch when one is set', async () => {
      const url = await origin(undefined, { moved: { 'main.txt': 'from main\n' }, base });
      const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch: 'goal/20-part', base } });
      const { exec, calls } = recorder();

      const prepared = await prepare(depsWith(fake, exec));

      expect(prepared).toMatchObject({ branch: 'goal/20-part', branch_existed: false });
      expect(calls).toContainEqual({
        command: 'git',
        args: ['checkout', '--quiet', '-b', 'goal/20-part', `origin/${base}`],
        cwd: dirs.workspace,
      });
      expect(calls.some((c) => c.args[0] === 'push')).toBe(false);
      const ws = dirs.workspace;
      expect(git(ws, 'branch', '--show-current')).toBe('goal/20-part');
      expect(git(ws, 'merge-base', '--is-ancestor', `origin/${base}`, 'HEAD')).toBe('');
      expect(() => git(ws, 'merge-base', '--is-ancestor', 'origin/main', 'HEAD')).toThrow();
    });

    it("reports the base branch's head as base_head", async () => {
      const url = await origin(undefined, { moved: { 'main.txt': 'from main\n' }, base });
      const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch: 'goal/21-part', base } });
      const { exec } = recorder();

      const prepared = await prepare(depsWith(fake, exec));

      const head = git(bare(), 'rev-parse', `refs/heads/${base}`);
      expect(head).not.toBe(git(bare(), 'rev-parse', 'refs/heads/main'));
      expect(prepared).toMatchObject({ branch_head: head, base_head: head });
    });

    it('rebases an existing branch on the base branch', async () => {
      const branch = 'goal/21-rebase';
      const url = await origin(branch, { moved: { 'main.txt': 'from main\n' }, base });
      const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch, base } });
      const { exec, calls } = recorder();

      const prepared = await prepare(depsWith(fake, exec));

      expect(prepared).toMatchObject({ branch, branch_existed: true, fresh_base: 'rebase' });
      expect(calls).toContainEqual({
        command: 'git',
        args: ['rebase', '--quiet', `origin/${base}`],
        cwd: dirs.workspace,
      });
      const ws = dirs.workspace;
      expect(git(ws, 'merge-base', '--is-ancestor', `origin/${base}`, 'HEAD')).toBe('');
      expect(() => git(ws, 'merge-base', '--is-ancestor', 'origin/main', 'HEAD')).toThrow();
      expect(git(ws, 'log', '--format=%s', `origin/${base}..HEAD`)).toBe('earlier run');
      expect(await spec_()).not.toHaveProperty('base_branch');
    });

    it('merges the base branch into an existing branch when fresh_base is merge', async () => {
      const branch = 'goal/22-merge';
      const url = await origin(branch, { moved: { 'main.txt': 'from main\n' }, base });
      const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch, base, fresh_base: 'merge' } });
      const { exec } = recorder();

      await prepare(depsWith(fake, exec));

      const ws = dirs.workspace;
      const [, ...parents] = git(ws, 'rev-list', '--parents', '-1', 'HEAD').split(' ');
      expect(parents).toEqual([git(ws, 'rev-parse', `origin/${branch}`), git(ws, 'rev-parse', `origin/${base}`)]);
      expect(() => git(ws, 'merge-base', '--is-ancestor', 'origin/main', 'HEAD')).toThrow();
    });

    it('names the base branch next to the conflicting files', async () => {
      const branch = 'goal/23-conflict';
      const url = await origin(branch, { changed: { 'base.txt': 'from the branch\n' }, base });
      const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch, base } });

      const prepared = await prepare(depsWith(fake, recorder().exec));

      expect(prepared).toMatchObject({ conflicts: ['base.txt'] });
      expect(await spec_()).toMatchObject({ base_conflicts: ['base.txt'], base_branch: base });
    });

    it('creates a missing base branch from the default branch and pushes it', async () => {
      const url = await origin(undefined, { moved: { 'main.txt': 'from main\n' } });
      const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch: 'goal/24-first', base } });
      const { exec, calls } = recorder();

      const prepared = await prepare(depsWith(fake, exec));

      expect(prepared).toMatchObject({ branch: 'goal/24-first', branch_existed: false });
      const pushes = calls.filter((c) => c.args[0] === 'push');
      expect(pushes).toEqual([{ command: 'git', args: ['push', '--quiet', 'origin', base], cwd: dirs.workspace }]);
      const main = git(bare(), 'rev-parse', 'main');
      expect(git(bare(), 'rev-parse', base)).toBe(main);
      expect(git(dirs.workspace, 'rev-parse', 'HEAD')).toBe(main);
    });

    it("pushes a missing base with the repository token's stored login", async () => {
      const url = await origin();
      const login = {
        host: 'github.com',
        username: 'x-access-token',
        password_env: 'GOAL_REPOSITORY_TOKEN',
        store: true,
      };
      const { fake } = gatewayServing({
        spec,
        files: {},
        repository: { url, branch: 'goal/24-first', base },
        git_credentials: [login],
      });
      const { exec: real } = recorder();
      // What git would sign in with when the push asks for the host's login, in the push's own
      // environment: gitEnv, which has no GOAL_REPOSITORY_TOKEN.
      let answer = '';
      const exec: Exec = (command, args, opts) => {
        if (args[0] === 'push') answer = fill('github.com', opts?.cwd);
        return real(command, args, opts);
      };

      await prepare(depsWith(fake, exec, { GOAL_REPOSITORY_TOKEN: 'acme-s3cr3t' }));

      expect(answer).toContain('username=x-access-token\npassword=acme-s3cr3t\n');
    });

    it('uses the base branch another run pushed first', async () => {
      const url = await origin();
      const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch: 'goal/25-second', base } });
      const { exec: real, calls } = recorder();
      // Another run pushes the base between prepare's check and its push, so prepare's push is rejected.
      let theirs = '';
      const exec: Exec = (command, args, opts) => {
        if (args[0] === 'push' && !theirs) {
          const work = join(root, 'seed');
          git(work, 'checkout', '--quiet', '-b', base, 'main');
          git(work, 'commit', '--quiet', '--allow-empty', '-m', 'the other run');
          git(work, 'push', '--quiet', 'origin', base);
          theirs = git(work, 'rev-parse', 'HEAD');
        }
        return real(command, args, opts);
      };

      const prepared = await prepare(depsWith(fake, exec));

      expect(prepared).toMatchObject({ branch: 'goal/25-second', branch_existed: false });
      expect(calls.filter((c) => c.args[0] === 'push')).toHaveLength(1);
      expect(git(bare(), 'rev-parse', base)).toBe(theirs);
      expect(git(dirs.workspace, 'rev-parse', 'HEAD')).toBe(theirs);
      expect(git(dirs.workspace, 'rev-parse', base)).toBe(theirs);
    });

    it('fails when pushing a missing base fails and the remote still has none', async () => {
      const url = await origin();
      const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch: 'goal/26-x', base } });
      const { exec: real } = recorder();
      const exec: Exec = (command, args, opts) =>
        args[0] === 'push' ? Promise.resolve(128) : real(command, args, opts);

      await expect(prepare(depsWith(fake, exec))).rejects.toThrow('git push exited 128');
    });

    it('treats an empty base as the default branch', async () => {
      const url = await origin();
      const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch: 'goal/27-x', base: '' } });
      const { exec, calls } = recorder();

      await prepare(depsWith(fake, exec));

      expect(calls).toContainEqual({
        command: 'git',
        args: ['checkout', '--quiet', '-b', 'goal/27-x'],
        cwd: dirs.workspace,
      });
      expect(calls.filter((c) => c.args[0] === 'check-ref-format')).toHaveLength(1);
    });

    it('refuses a base that is not a branch name before cloning', async () => {
      const url = await origin();
      const { fake } = gatewayServing({
        spec,
        files: {},
        repository: { url, branch: 'goal/28-x', base: '--upload-pack=sh' },
      });
      const { exec, calls } = recorder();

      await expect(prepare(depsWith(fake, exec))).rejects.toThrow('git check-ref-format exited');
      expect(calls.some((c) => c.args[0] === 'clone')).toBe(false);
    });
  });

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

  it("keeps a stored login in git's credential store under the home, outside the workspace", async () => {
    const login = { host: 'gitlab.com', username: 'oauth2', password_env: 'GOAL_REPOSITORY_TOKEN', store: true };
    const { fake } = gatewayServing({ spec, files: {}, git_credentials: [login] });

    await prepare(depsWith(fake, recorder().exec, { GOAL_REPOSITORY_TOKEN: 'acme-s3cr3t/+' }));

    // A later git command, the agent's push, runs without the variable and still signs in.
    expect(fill('gitlab.com')).toContain('username=oauth2\npassword=acme-s3cr3t/+\n');
    const helper = execFileSync('git', ['config', '--global', '--get', 'credential.https://gitlab.com.helper'], {
      env: gitEnv(),
      encoding: 'utf8',
    });
    expect(helper).not.toContain('s3cr3t');
    const store = join(home, '.git-credentials');
    expect((await stat(store)).mode & 0o777).toBe(0o600);
    expect(existsSync(dirs.workspace) ? readdirSync(dirs.workspace) : []).toEqual([]);
  });

  it('stores nothing for a stored login whose variable is unset', async () => {
    const login = {
      host: 'github.com',
      username: 'x-access-token',
      password_env: 'GOAL_REPOSITORY_TOKEN',
      store: true,
    };
    const { fake } = gatewayServing({ spec, files: {}, git_credentials: [login] });

    await prepare(depsWith(fake, recorder().exec));

    expect(fill('github.com')).not.toContain('password=');
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

  describe('definition mode', () => {
    const draftSpec = {
      ...spec,
      item_id: '',
      station: '',
      draft_request: { id: 'd1', text: 'Give review a 4th reviewer' },
    };
    const sha = (s: string) => createHash('sha256').update(s).digest('hex');
    const factory = 'apiVersion: goal/v1\nkind: Factory\n';
    const prompt = '# Reviewer\n';

    it("writes the draft's files into the workspace and their sha256 into /goal/definition.json, with no git or mise", async () => {
      const bundle = {
        spec: draftSpec,
        files: { 'agent/prompt.md': b64('unused\n') },
        git_credentials: [{ host: 'github.com', username: 'x-access-token', password_env: 'GH_TOKEN' }],
        definition: { files: { 'factory.yaml': b64(factory), 'agents/reviewer.md': b64(prompt) } },
      };
      const { exec, calls } = recorder();

      const prepared = await prepare(depsWith(gatewayServing(bundle).fake, exec));

      expect(await readFile(join(dirs.workspace, 'factory.yaml'), 'utf8')).toBe(factory);
      expect(await readFile(join(dirs.workspace, 'agents/reviewer.md'), 'utf8')).toBe(prompt);
      expect(JSON.parse(await readFile(join(dirs.goal, 'definition.json'), 'utf8'))).toEqual({
        'factory.yaml': sha(factory),
        'agents/reviewer.md': sha(prompt),
      });
      expect(JSON.parse(await readFile(join(dirs.goal, 'spec.json'), 'utf8')).draft_request).toEqual(
        draftSpec.draft_request,
      );
      expect(prepared).toEqual({
        type: 'prepared',
        at: later.toISOString(),
        seconds: 12.3,
        tools: 0,
        definition_files: 2,
      });
      // No clone, no credential helper, no mise install for a repository.
      expect(calls).toEqual([]);
    });

    it('refuses a definition with a repository, without a draft request, or with a path that leaves the workspace', async () => {
      const files = { 'factory.yaml': b64(factory) };
      for (const bundle of [
        {
          spec: draftSpec,
          files: {},
          definition: { files },
          repository: { url: 'https://example.com/a.git', branch: 'goal/1-x' },
        },
        { spec, files: {}, definition: { files } },
        { spec: draftSpec, files: {} },
        { spec: draftSpec, files: {}, definition: { files: { '../x': b64('x') } } },
        { spec: draftSpec, files: {}, definition: { files: { 'agents/../../x': b64('x') } } },
        { spec: draftSpec, files: {}, definition: { files: { '/etc/profile': b64('x') } } },
        { spec: draftSpec, files: { 'definition.json': b64('{}') }, definition: { files } },
      ]) {
        const { exec, calls } = recorder();
        await expect(prepare(depsWith(gatewayServing(bundle).fake, exec))).rejects.toThrow();
        expect(calls).toEqual([]);
        expect(existsSync(join(dirs.workspace, 'factory.yaml'))).toBe(false);
      }
    });

    it('refuses a grade with a repository or a definition, and writes its block to spec.json', async () => {
      const grade = { reason: 'It returned 500.', result: { kind: 'try', output: 'o', diff: '' } };
      const gradeSpec = { ...spec, item_id: '', station: '', grade };
      for (const bundle of [
        { spec: gradeSpec, files: {}, repository: { url: 'https://example.com/a.git', branch: 'goal/1-x' } },
        { spec: gradeSpec, files: {}, definition: { files: { 'factory.yaml': b64(factory) } } },
      ]) {
        const { exec, calls } = recorder();
        await expect(prepare(depsWith(gatewayServing(bundle).fake, exec))).rejects.toThrow();
        expect(calls).toEqual([]);
      }

      const { exec } = recorder();
      await prepare(depsWith(gatewayServing({ spec: gradeSpec, files: {} }).fake, exec));
      expect(JSON.parse(await readFile(join(dirs.goal, 'spec.json'), 'utf8')).grade).toEqual(grade);
    });

    it('refuses a jonah review with a repository or a definition, and writes its box and spec', async () => {
      const jonah = { propose_after: 5, manual: false };
      const jonahSpec = { ...spec, item_id: '', station: '', jonah };
      for (const bundle of [
        { spec: jonahSpec, files: {}, repository: { url: 'https://example.com/a.git', branch: 'goal/1-x' } },
        { spec: jonahSpec, files: {}, definition: { files: { 'factory.yaml': b64(factory) } } },
      ]) {
        const { exec, calls } = recorder();
        await expect(prepare(depsWith(gatewayServing(bundle).fake, exec))).rejects.toThrow();
        expect(calls).toEqual([]);
      }

      const { exec } = recorder();
      const box = {
        'definition/factory.yaml': b64(factory),
        'evidence/README.md': b64('# Evidence\n'),
        'events.md': b64('# Sent-back events\n'),
      };
      await prepare(depsWith(gatewayServing({ spec: jonahSpec, files: box }).fake, exec));
      expect(JSON.parse(await readFile(join(dirs.goal, 'spec.json'), 'utf8')).jonah).toEqual(jonah);
      expect(await readFile(join(dirs.goal, 'definition', 'factory.yaml'), 'utf8')).toBe(factory);
      expect(await readFile(join(dirs.goal, 'evidence', 'README.md'), 'utf8')).toBe('# Evidence\n');
    });

    it('still needs an item outside definition mode', async () => {
      const { exec } = recorder();
      const bundle = { spec: { ...spec, item_id: '' }, files: {} };
      await expect(prepare(depsWith(gatewayServing(bundle).fake, exec))).rejects.toThrow('item_id');
    });
  });

  it('fails when the clone fails', async () => {
    const missing = `file://${join(root, 'no-such.git')}`;
    const { fake } = gatewayServing({ spec, files: {}, repository: { url: missing, branch: 'goal/1-x' } });
    await expect(prepare(depsWith(fake, recorder().exec))).rejects.toThrow('git clone exited 128');
  });
});

describe('prepare: a try of a past item', () => {
  it('fully clones, fetches the recorded commits by SHA, checks the branch out at the commit and brings it up to date with the base commit', async () => {
    const branch = 'goal/20-try';
    const url = await origin(branch, { moved: { 'main.txt': 'from main\n' } });
    const bare = join(root, 'origin.git');
    const commit = git(bare, 'rev-parse', `refs/heads/${branch}`);
    const base = git(bare, 'rev-parse', 'refs/heads/main');
    const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch, commit, base_commit: base } });
    const { exec, calls } = recorder();

    const prepared = await prepare(depsWith(fake, exec));

    expect(prepared).toMatchObject({ branch, branch_head: commit, base_head: base, fresh_base: 'rebase' });
    const clone = calls.find((c) => c.args[0] === 'clone');
    expect(clone?.args).not.toContain('--filter=blob:none');
    expect(calls).toContainEqual({
      command: 'git',
      args: ['fetch', '--quiet', 'origin', commit, base],
      cwd: dirs.workspace,
    });
    const ws = dirs.workspace;
    expect(git(ws, 'branch', '--show-current')).toBe(branch);
    expect(git(ws, 'merge-base', '--is-ancestor', base, 'HEAD')).toBe('');
    expect(git(ws, 'rev-parse', 'refs/goal/try-start')).toBe(git(ws, 'rev-parse', 'HEAD'));
  });

  it('drops the stored login and every credential helper before the agent runs', async () => {
    const branch = 'goal/21-try';
    const url = await origin(branch);
    const commit = git(join(root, 'origin.git'), 'rev-parse', `refs/heads/${branch}`);
    const login = {
      host: 'github.com',
      username: 'x-access-token',
      password_env: 'GOAL_REPOSITORY_TOKEN',
      store: true,
    };
    const other = { host: 'gitlab.com', username: 'oauth2', password_env: 'GITLAB_TOKEN' };
    const { fake } = gatewayServing({
      spec,
      files: {},
      repository: { url, branch, commit },
      git_credentials: [login, other],
    });

    await prepare(
      depsWith(fake, recorder().exec, { GOAL_REPOSITORY_TOKEN: 'acme-s3cr3t', GITLAB_TOKEN: 'acme-other' }),
    );

    expect(existsSync(join(home, '.git-credentials'))).toBe(false);
    expect(fill('github.com')).not.toContain('password=');
    expect(fill('gitlab.com')).not.toContain('password=');
    const config = execFileSync('git', ['config', '--global', '--list'], { env: gitEnv(), encoding: 'utf8' });
    expect(config).not.toContain('credential.');
  });

  it('fails, naming the commit, when the remote no longer has it', async () => {
    const branch = 'goal/22-try';
    const url = await origin(branch);
    const gone = 'f'.repeat(40);
    const { fake } = gatewayServing({ spec, files: {}, repository: { url, branch, commit: gone } });

    await expect(prepare({ ...depsWith(fake, recorder().exec), sleep: async () => undefined })).rejects.toThrow(
      `commit ${gone} is gone from the remote`,
    );
  });
});

describe("prepare: the item's repositories", () => {
  const branch = 'goal/acme-app/7-add-a-health-endpoint';
  const app = 'https://github.com/acme/app';
  const lib = 'https://github.com/acme/lib';
  const helper = { node: process.execPath, script: fileURLToPath(new URL('./credential.ts', import.meta.url)) };
  const bin = fileURLToPath(new URL('../bin', import.meta.url));

  /** Points app and lib's https URLs at local origins in the test's git config, as a host would serve them. */
  async function hosted(opts: { appBranch?: boolean; libBranch?: boolean } = {}) {
    const appOrigin = await origin(opts.appBranch ? branch : undefined, { name: 'app' });
    const libOrigin = await origin(opts.libBranch ? branch : undefined, {
      name: 'lib',
      changed: { 'lib.txt': 'lib\n' },
    });
    git(root, 'config', '--global', `url.${appOrigin}.insteadOf`, app);
    git(root, 'config', '--global', `url.${libOrigin}.insteadOf`, lib);
  }

  function codeBundle(code: Record<string, unknown>, extra: Record<string, unknown> = {}) {
    return {
      spec,
      files: {},
      repository: { url: `${app}.git`, branch, fresh_base: 'merge' },
      repositories: [
        { url: app, path: '/workspace', branch, write: true },
        { url: lib, path: '/repos/acme/lib', branch, write: false },
      ],
      repository_logins: [],
      read_only: [],
      ...code,
      ...extra,
    };
  }

  function deps(bundle: unknown, exec: Exec, env: NodeJS.ProcessEnv = {}): PrepareDeps {
    return { ...depsWith(gatewayServing(bundle).fake, exec, env), credentialHelper: helper };
  }

  const line = (c: { command: string; args: string[] }) => `${c.command} ${c.args.join(' ')}`;

  it('clones the first into /workspace and each other under /repos, on the item branch where the remote has it', async () => {
    await hosted({ appBranch: true, libBranch: true });
    const { exec, calls } = recorder();

    const prepared = await prepare(deps(codeBundle({}), exec));

    const libDir = join(dirs.repos, 'acme/lib');
    expect(calls.map(line)).toEqual(
      expect.arrayContaining([
        `git clone --quiet --filter=blob:none -- ${app} ${dirs.workspace}`,
        `git checkout --quiet -B ${branch} origin/${branch}`,
        // The first keeps today's flow: the repository's fresh_base and the repository's own tools.
        `git merge --quiet --no-edit origin/main`,
        'mise install --yes',
        `git clone --quiet --filter=blob:none -- ${lib} ${libDir}`,
      ]),
    );
    expect(calls.filter((c) => c.command === 'mise' && c.args[0] === 'install').map((c) => c.cwd)).toEqual([
      dirs.workspace,
    ]);
    expect(git(libDir, 'rev-parse', '--abbrev-ref', 'HEAD')).toBe(branch);
    expect(await readFile(join(libDir, 'lib.txt'), 'utf8')).toBe('lib\n');
    const libHead = git(libDir, 'rev-parse', 'HEAD');
    const libBase = git(libDir, 'rev-parse', 'origin/HEAD');
    expect(prepared).toMatchObject({ branch, branch_existed: true, fresh_base: 'merge' });
    expect(prepared.repositories).toEqual([
      { repo: app, base_commit: prepared.base_head, branch_head: prepared.branch_head },
      { repo: lib, base_commit: libBase, branch_head: libHead },
    ]);
    const written = JSON.parse(await readFile(join(dirs.goal, 'spec.json'), 'utf8'));
    expect(written.branch).toBe(branch);
    expect(written.repositories).toEqual([
      {
        url: app,
        path: '/workspace',
        branch,
        write: true,
        base_commit: prepared.base_head,
        branch_head: prepared.branch_head,
      },
      { url: lib, path: '/repos/acme/lib', branch, write: false, base_commit: libBase, branch_head: libHead },
    ]);
  });

  it('leaves another repository on its default branch when the remote has no item branch', async () => {
    await hosted();
    const { exec, calls } = recorder();

    const prepared = await prepare(deps(codeBundle({}), exec));

    const libDir = join(dirs.repos, 'acme/lib');
    expect(git(libDir, 'rev-parse', '--abbrev-ref', 'HEAD')).toBe('main');
    expect(calls.filter((c) => c.cwd === libDir && c.args[0] === 'checkout')).toEqual([]);
    expect(prepared.repositories?.[1]?.branch_head).toBe(git(libDir, 'rev-parse', 'origin/HEAD'));
  });

  it('clones nothing for an item with no repositories, and leaves /workspace empty', async () => {
    const { exec, calls } = recorder();

    const prepared = await prepare(deps(codeBundle({ repositories: [] }), exec));

    expect(calls.filter((c) => c.args[0] === 'clone' || c.args[0] === 'checkout')).toEqual([]);
    expect(existsSync(dirs.workspace) ? readdirSync(dirs.workspace) : []).toEqual([]);
    expect(prepared).not.toHaveProperty('branch');
    expect(prepared.repositories).toEqual([]);
  });

  it('signs git in per repository path with the token its entry picks, instead of the stored login', async () => {
    await hosted();
    const stored = {
      host: 'github.com',
      username: 'x-access-token',
      password_env: 'GOAL_REPOSITORY_TOKEN',
      store: true,
    };
    const connection = { host: 'github.com', username: 'x-access-token', password_env: 'GH_TOKEN' };
    const logins = [
      { prefix: app, username: 'x-access-token', password_env: 'GOAL_REPOSITORY_TOKEN_APP', write: true },
      {
        prefix: 'https://github.com/acme/',
        username: 'x-access-token',
        password_env: 'GOAL_REPOSITORY_TOKEN_ACME',
        write: false,
      },
      {
        prefix: 'https://gitlab.example.com/acme/',
        username: 'oauth2',
        password_env: 'GOAL_REPOSITORY_TOKEN_UNSET',
        write: false,
      },
    ];
    const env = {
      GOAL_REPOSITORY_TOKEN: 'stored-s3cr3t',
      GOAL_REPOSITORY_TOKEN_APP: 'app-s3cr3t',
      GOAL_REPOSITORY_TOKEN_ACME: 'acme-s3cr3t',
    };
    const bundle = codeBundle({ repository_logins: logins }, { git_credentials: [stored, connection] });
    const { exec, calls } = recorder();

    const prepared = await prepare(deps(bundle, exec, env));

    const helpers = git(root, 'config', '--global', '--get-all', 'credential.https://github.com.helper').split('\n');
    expect(helpers).toHaveLength(2);
    expect(helpers[0]).toContain(helper.script);
    expect(helpers[1]).toBe(credentialHelper(connection));
    expect(git(root, 'config', '--global', '--get', 'credential.useHttpPath')).toBe('true');
    expect(git(root, 'config', '--global', '--get-all', 'credential.https://gitlab.example.com.helper')).toContain(
      helper.script,
    );
    // The stored login is gone; the logins file is the helper's, 0600 under the home.
    expect(existsSync(join(home, '.git-credentials'))).toBe(false);
    const loginsFile = join(home, '.goal/repository-logins.json');
    expect((await stat(loginsFile)).mode & 0o777).toBe(0o600);
    expect(JSON.parse(await readFile(loginsFile, 'utf8'))).toEqual([
      { prefix: app, username: 'x-access-token', password: 'app-s3cr3t' },
      { prefix: 'https://github.com/acme/', username: 'x-access-token', password: 'acme-s3cr3t' },
    ]);
    for (const c of calls) expect(c.args.join(' ')).not.toContain('s3cr3t');
    expect(JSON.stringify(prepared)).not.toContain('s3cr3t');
    for (const dir of [dirs.workspace, dirs.goal, dirs.repos]) {
      // grep exits 1 when no file matches.
      expect(spawnSync('grep', ['-rl', 's3cr3t', dir], { encoding: 'utf8' })).toMatchObject({ status: 1, stdout: '' });
    }

    // A later git command, the agent's, runs without the variables and still signs in per path.
    const fillPath = (path: string) =>
      execFileSync('git', ['credential', 'fill'], {
        env: gitEnv(),
        input: `protocol=https\nhost=github.com\npath=${path}\n\n`,
        encoding: 'utf8',
        stdio: ['pipe', 'pipe', 'ignore'],
      });
    expect(fillPath('acme/app.git')).toContain('password=app-s3cr3t');
    expect(fillPath('acme/web.git')).toContain('password=acme-s3cr3t');
    expect(() => fillPath('other/app.git')).toThrow();
    expect((await readFile(join(home, '.goal/repositories'), 'utf8')).split('\n')).toEqual(
      expect.arrayContaining(['https://github.com/acme/app', 'https://github.com/acme/web']),
    );
  }, 20_000);

  it('refuses pushes under a read-only prefix and puts a write entry under it back, for pushes alone', async () => {
    await hosted();
    const logins = [
      { prefix: app, username: 'x-access-token', password_env: 'GOAL_REPOSITORY_TOKEN_APP', write: true },
      {
        prefix: 'https://github.com/acme/',
        username: 'x-access-token',
        password_env: 'GOAL_REPOSITORY_TOKEN_ACME',
        write: false,
      },
    ];
    const bundle = codeBundle({ repository_logins: logins, read_only: ['https://github.com/acme/'] });
    const { exec, calls } = recorder();

    await prepare(deps(bundle, exec));

    const config = calls.map(line).filter((l) => l.includes('InsteadOf') || l.includes('protocol.'));
    expect(config).toEqual([
      'git config --global protocol.goal-refuse.allow always',
      'git config --global --add url.goal-refuse::https://github.com/acme/.pushInsteadOf https://github.com/acme/',
      `git config --global --add url.${app}.pushInsteadOf ${app}`,
    ]);
    expect(calls.map(line).some((l) => /\.insteadOf/i.test(l))).toBe(false);

    // A push to the read-only repository reaches the refusing helper; a fetch still works.
    const libDir = join(dirs.repos, 'acme/lib');
    const pushEnv = { ...gitEnv(), PATH: `${bin}:${process.env.PATH}` };
    const push = spawn('git', ['push', 'origin', `HEAD:refs/heads/${branch}`], { cwd: libDir, env: pushEnv });
    let stderr = '';
    push.stderr.on('data', (c: Buffer) => {
      stderr += c.toString('utf8');
    });
    const code = await new Promise<number | null>((resolve) => push.once('close', resolve));
    expect(code).not.toBe(0);
    expect(stderr).toContain('this run may not push to https://github.com/acme/lib');
    expect(() => git(libDir, 'fetch', '--quiet', 'origin')).not.toThrow();
  }, 20_000);
});
