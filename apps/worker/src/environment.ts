import { chmod, mkdir, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import type { Environment } from './spec.js';

/** How long one setup command may run (ADR 0037 decision 5). */
export const setupTimeoutMs = 30 * 60 * 1000;

/** How many of a failed setup command's last output lines its error holds. */
export const setupTailLines = 50;

/**
 * Runs command through a shell in cwd, passing each chunk of its stdout and stderr to onOutput, and
 * resolves with its exit code, or with timedOut once it ran past timeoutMs and was stopped.
 */
export type Shell = (
  command: string,
  opts: { cwd: string; timeoutMs: number; onOutput: (chunk: string) => void },
) => Promise<{ code: number; timedOut: boolean }>;

export interface EnvironmentDeps {
  /** goal-agent run's env, which holds each file's content in the variable the file names. */
  env: NodeJS.ProcessEnv;
  /** The agent's home directory, which a leading ~/ in a file's path names. */
  home: string;
  /** Where setup commands run. */
  workspace: string;
  shell: Shell;
}

/**
 * Applies the agent's environment in the box before the agent starts (ADR 0037 decision 5): each
 * file written from its variable at its path, ~/ expanded, with mode 0600, all of them before the
 * first setup command; then each setup command in order, each with a 30-minute limit. A failure
 * throws an error that names the command and holds its last 50 lines of output.
 */
export async function applyEnvironment(environment: Environment, deps: EnvironmentDeps): Promise<void> {
  for (const file of environment.files) {
    const content = deps.env[file.env];
    if (content === undefined) throw new Error(`environment file ${file.path}: its variable ${file.env} is not set`);
    const path = file.path.startsWith('~/') ? join(deps.home, file.path.slice(2)) : file.path;
    await mkdir(dirname(path), { recursive: true });
    await writeFile(path, content, { mode: 0o600 });
    // writeFile's mode applies only to a new file; an existing one keeps its own.
    await chmod(path, 0o600);
  }

  for (const command of environment.setup) {
    const tail = new Tail(setupTailLines);
    const { code, timedOut } = await deps.shell(command, {
      cwd: deps.workspace,
      timeoutMs: setupTimeoutMs,
      onOutput: (chunk) => tail.push(chunk),
    });
    if (timedOut || code !== 0) {
      const why = timedOut ? `ran past ${setupTimeoutMs / 60_000} minutes` : `exited ${code}`;
      const lines = tail.lines();
      const last = lines.length > 0 ? `; its last ${lines.length} lines:\n${lines.join('\n')}` : '';
      throw new Error(`environment setup: \`${command}\` ${why}${last}`);
    }
  }
}

/** The last n lines of a stream of chunks, holding no more than those lines and the one being written. */
export class Tail {
  private done: string[] = [];
  private open = '';

  constructor(private readonly n: number) {}

  push(chunk: string): void {
    const parts = (this.open + chunk).split('\n');
    this.open = parts.pop() ?? '';
    this.done.push(...parts);
    if (this.done.length > this.n) this.done.splice(0, this.done.length - this.n);
  }

  lines(): string[] {
    const all = this.open === '' ? this.done : [...this.done, this.open];
    return all.slice(-this.n);
  }
}
