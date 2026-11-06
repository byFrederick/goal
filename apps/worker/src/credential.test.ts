import { execFileSync } from 'node:child_process';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { repositoryUrl } from './credential.js';

const script = fileURLToPath(new URL('./credential.ts', import.meta.url));

const logins = [
  { prefix: 'https://github.com/acme/', username: 'x-access-token', password: 'owner-token' },
  { prefix: 'https://github.com/acme/app', username: 'x-access-token', password: 'app-token' },
  { prefix: 'https://gitlab.example.com/acme/lib.git', username: 'oauth2', password: 'lib-token' },
];

let dir: string;
let loginsFile: string;
let record: string;

beforeEach(async () => {
  dir = await mkdtemp(join(tmpdir(), 'goal-credential-'));
  loginsFile = join(dir, 'logins.json');
  record = join(dir, 'record');
  await writeFile(loginsFile, JSON.stringify(logins), { mode: 0o600 });
});

afterEach(async () => {
  await rm(dir, { recursive: true, force: true });
});

/** Runs the helper as git does, with action and git's request on stdin; returns its stdout. */
function ask(input: string, action = 'get'): string {
  return execFileSync(process.execPath, [script, loginsFile, record, action], {
    input,
    encoding: 'utf8',
    env: { PATH: process.env.PATH, NODE_NO_WARNINGS: '1' },
  });
}

const request = (host: string, path?: string) =>
  `protocol=https\nhost=${host}\n${path === undefined ? '' : `path=${path}\n`}\n`;

async function recorded(): Promise<string[]> {
  return (await readFile(record, 'utf8').catch(() => '')).split('\n').filter((l) => l !== '');
}

describe('the credential helper', () => {
  it("answers a repository its own entry covers with that entry's token", async () => {
    expect(ask(request('gitlab.example.com', 'acme/lib.git'))).toBe('username=oauth2\npassword=lib-token\n');
    expect(await recorded()).toEqual(['https://gitlab.example.com/acme/lib']);
  });

  it("answers any repository of an owner pattern with the pattern's token", async () => {
    expect(ask(request('github.com', 'acme/web.git'))).toBe('username=x-access-token\npassword=owner-token\n');
  });

  it('prefers the more specific entry, in any case and with or without .git', async () => {
    expect(ask(request('github.com', 'acme/app.git'))).toContain('password=app-token');
    expect(ask(request('GitHub.com', 'Acme/App'))).toContain('password=app-token');
    // A sibling whose name starts like the entry's is the owner pattern's.
    expect(ask(request('github.com', 'acme/application.git'))).toContain('password=owner-token');
    expect(await recorded()).toEqual([
      'https://github.com/acme/app',
      'https://github.com/acme/app',
      'https://github.com/acme/application',
    ]);
  });

  it('gives a repository no entry covers no token and records nothing', async () => {
    expect(ask(request('github.com', 'other/app.git'))).toBe('');
    expect(ask(request('github.com', 'acme'))).toBe('');
    expect(await recorded()).toEqual([]);
  });

  it('answers nothing on another host, without a path or for another action', async () => {
    expect(ask(request('gitlab.example.com', 'acme/app.git'))).toBe('');
    expect(ask(request('github.com'))).toBe('');
    expect(ask(request('github.com', 'acme/app.git'), 'store')).toBe('');
    expect(ask('protocol=http\nhost=github.com\npath=acme/app.git\n\n')).toBe('');
    expect(await recorded()).toEqual([]);
  });

  it('answers nothing, and names no value, when the logins file is unreadable', async () => {
    await writeFile(loginsFile, '{"prefix": "https://github.com/acme/", "password": "s3cr3t"');
    const out = execFileSync(process.execPath, [script, loginsFile, record, 'get'], {
      input: request('github.com', 'acme/app.git'),
      encoding: 'utf8',
      env: { PATH: process.env.PATH, NODE_NO_WARNINGS: '1' },
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    expect(out).toBe('');
  });
});

describe('repositoryUrl', () => {
  it('compares repositories in lower case without .git or a trailing slash', () => {
    expect(repositoryUrl('https://GitHub.com/Acme/App.git/')).toBe('https://github.com/acme/app');
  });
});
