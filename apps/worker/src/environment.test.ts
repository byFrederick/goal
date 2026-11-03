import { mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { applyEnvironment, type Shell, setupTimeoutMs, Tail } from './environment.js';
import { Environment } from './spec.js';

const environmentOf = (e: Partial<Environment>) => Environment.parse({ name: 'default', ...e });

/** A shell that records each command and the files that existed when it ran, and answers with result. */
function recordingShell(
  watch: string[],
  result: (command: string, onOutput: (chunk: string) => void) => { code: number; timedOut: boolean } = () => ({
    code: 0,
    timedOut: false,
  }),
) {
  const calls: { command: string; cwd: string; timeoutMs: number; present: boolean[] }[] = [];
  const shell: Shell = async (command, opts) => {
    const present = await Promise.all(
      watch.map((p) =>
        stat(p).then(
          () => true,
          () => false,
        ),
      ),
    );
    calls.push({ command, cwd: opts.cwd, timeoutMs: opts.timeoutMs, present });
    return result(command, opts.onOutput);
  };
  return { shell, calls };
}

describe('applyEnvironment', () => {
  let home: string;
  let abs: string;

  beforeEach(async () => {
    home = await mkdtemp(join(tmpdir(), 'goal-env-home-'));
    abs = await mkdtemp(join(tmpdir(), 'goal-env-abs-'));
  });
  afterEach(async () => {
    await rm(home, { recursive: true, force: true });
    await rm(abs, { recursive: true, force: true });
  });

  it('writes each file from its variable with mode 0600, ~/ in the home, before the first setup command', async () => {
    const kube = join(home, '.kube/config');
    const conf = join(abs, 'etc/app.conf');
    const { shell, calls } = recordingShell([kube, conf]);
    const environment = environmentOf({
      files: [
        { path: `${abs}/etc/app.conf`, env: 'GOAL_FILE_1' },
        { path: '~/.kube/config', env: 'GOAL_FILE_2' },
      ],
      setup: ['pnpm install --frozen-lockfile', 'make tools'],
    });

    await applyEnvironment(environment, {
      env: { GOAL_FILE_1: 'level: info\n', GOAL_FILE_2: 'apiVersion: v1\n' },
      home,
      workspace: '/workspace',
      marker: join(home, 'setup.done'),
      shell,
    });

    expect(await readFile(kube, 'utf8')).toBe('apiVersion: v1\n');
    expect(await readFile(conf, 'utf8')).toBe('level: info\n');
    expect((await stat(kube)).mode & 0o777).toBe(0o600);
    expect((await stat(conf)).mode & 0o777).toBe(0o600);
    expect(calls).toEqual([
      {
        command: 'pnpm install --frozen-lockfile',
        cwd: '/workspace',
        timeoutMs: setupTimeoutMs,
        present: [true, true],
      },
      { command: 'make tools', cwd: '/workspace', timeoutMs: setupTimeoutMs, present: [true, true] },
    ]);
    expect(setupTimeoutMs).toBe(30 * 60 * 1000);
    expect(await readFile(join(home, 'setup.done'), 'utf8')).not.toContain('level: info');
  });

  it('runs setup once per box: a run that finds the marker writes the files again and runs no command', async () => {
    const npmrc = join(home, '.npmrc');
    const marker = join(home, 'goal/environment-setup.done');
    const environment = environmentOf({
      files: [{ path: '~/.npmrc', env: 'GOAL_FILE_1' }],
      setup: ['pnpm install', 'make tools'],
    });
    const markerAt: boolean[] = [];
    const { shell, calls } = recordingShell([], () => ({ code: 0, timedOut: false }));
    const watching: Shell = async (command, opts) => {
      markerAt.push(
        await stat(marker).then(
          () => true,
          () => false,
        ),
      );
      return shell(command, opts);
    };
    const deps = (value: string) => ({
      env: { GOAL_FILE_1: value },
      home,
      workspace: '/workspace',
      marker,
      shell: watching,
    });

    await applyEnvironment(environment, deps('first'));
    expect(calls.map((c) => c.command)).toEqual(['pnpm install', 'make tools']);
    expect(markerAt).toEqual([false, false]);
    expect(await readFile(marker, 'utf8')).not.toContain('first');

    await applyEnvironment(environment, deps('second'));
    expect(calls.map((c) => c.command)).toEqual(['pnpm install', 'make tools']);
    expect(await readFile(npmrc, 'utf8')).toBe('second');
  });

  it('writes no marker when setup fails, so the next run in the box runs setup again', async () => {
    const marker = join(home, 'setup.done');
    let code = 1;
    const { shell, calls } = recordingShell([], () => ({ code, timedOut: false }));
    const deps = { env: {}, home, workspace: '/workspace', marker, shell };

    await expect(applyEnvironment(environmentOf({ setup: ['make deps'] }), deps)).rejects.toThrow(
      'environment setup: `make deps` exited 1',
    );
    await expect(stat(marker)).rejects.toThrow();

    code = 0;
    await applyEnvironment(environmentOf({ setup: ['make deps'] }), deps);
    expect(calls.map((c) => c.command)).toEqual(['make deps', 'make deps']);
    await stat(marker);
  });

  it('makes an existing file 0600 too', async () => {
    const path = join(abs, 'token');
    await writeFile(path, 'old', { mode: 0o644 });
    const { shell } = recordingShell([]);

    await applyEnvironment(environmentOf({ files: [{ path, env: 'GOAL_FILE_1' }] }), {
      env: { GOAL_FILE_1: 'new' },
      home,
      workspace: '/workspace',
      marker: join(home, 'setup.done'),
      shell,
    });

    expect(await readFile(path, 'utf8')).toBe('new');
    expect((await stat(path)).mode & 0o777).toBe(0o600);
  });

  it('fails naming the file and its variable when the variable is not set, before any setup', async () => {
    const { shell, calls } = recordingShell([]);

    await expect(
      applyEnvironment(environmentOf({ files: [{ path: '~/.npmrc', env: 'GOAL_FILE_1' }], setup: ['true'] }), {
        env: {},
        home,
        workspace: '/workspace',
        marker: join(home, 'setup.done'),
        shell,
      }),
    ).rejects.toThrow('environment file ~/.npmrc: its variable GOAL_FILE_1 is not set');
    expect(calls).toEqual([]);
  });

  it('stops at a failing command with the command and its last 50 lines, and runs no later one', async () => {
    const lines = Array.from({ length: 60 }, (_, i) => `line ${i + 1}`);
    const { shell, calls } = recordingShell([], (command, onOutput) => {
      if (command !== 'pnpm install') return { code: 0, timedOut: false };
      // Chunks split mid-line, as a pipe delivers them.
      const text = `${lines.join('\n')}\n`;
      onOutput(text.slice(0, 37));
      onOutput(text.slice(37));
      return { code: 1, timedOut: false };
    });

    const err = await applyEnvironment(environmentOf({ setup: ['echo hi', 'pnpm install', 'make'] }), {
      env: {},
      home,
      workspace: '/workspace',
      marker: join(home, 'setup.done'),
      shell,
    }).catch((e: unknown) => e as Error);

    expect(err).toBeInstanceOf(Error);
    const message = (err as Error).message;
    expect(message).toBe(
      `environment setup: \`pnpm install\` exited 1; its last 50 lines:\n${lines.slice(10).join('\n')}`,
    );
    expect(message).toContain('line 11\n');
    expect(message).not.toContain('line 10\n');
    expect(calls.map((c) => c.command)).toEqual(['echo hi', 'pnpm install']);
  });

  it('fails a command that ran past its limit', async () => {
    const { shell } = recordingShell([], (_command, onOutput) => {
      onOutput('still installing');
      return { code: 137, timedOut: true };
    });

    await expect(
      applyEnvironment(environmentOf({ setup: ['sleep 9999'] }), {
        env: {},
        home,
        workspace: '/workspace',
        marker: join(home, 'setup.done'),
        shell,
      }),
    ).rejects.toThrow('environment setup: `sleep 9999` ran past 30 minutes; its last 1 lines:\nstill installing');
  });
});

describe('Tail', () => {
  it('keeps the last n lines across chunks, with the line still being written', () => {
    const tail = new Tail(2);
    tail.push('a\nb');
    tail.push('c\nd\ne');
    expect(tail.lines()).toEqual(['d', 'e']);
    tail.push('\n');
    expect(tail.lines()).toEqual(['d', 'e']);
  });
});
