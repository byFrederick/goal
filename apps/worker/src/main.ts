import { spawn } from 'node:child_process';
import { cp, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { query } from '@anthropic-ai/claude-agent-sdk';
import type { WorkerEvent } from './events.js';
import { type Exec, type Output, type Prepared, prepare } from './prepare.js';
import { failed, runTask } from './run.js';
import { type AgentFiles, parseSpec, paths } from './spec.js';

// stdout carries only protocol lines; anything else goes to stderr.
const emit = (line: WorkerEvent | Prepared) => process.stdout.write(`${JSON.stringify(line)}\n`);
const now = () => new Date();

const usage = 'usage: goal-agent prepare | goal-agent run';

async function readAgentFiles(): Promise<AgentFiles> {
  const prompt = await readFile(join(paths.agent, 'prompt.md'), 'utf8');
  const schema = await readFile(join(paths.agent, 'output.schema.json'), 'utf8').catch((err: NodeJS.ErrnoException) => {
    if (err.code === 'ENOENT') return undefined;
    throw err;
  });
  return schema === undefined ? { prompt } : { prompt, outputSchema: JSON.parse(schema) };
}

/** Claude Code finds personal skills in its config dir's skills/; home is the sandbox's own. */
async function installSkills(): Promise<void> {
  await cp(join(paths.agent, 'skills'), join(paths.claudeConfig, 'skills'), { recursive: true }).catch(
    (err: NodeJS.ErrnoException) => {
      if (err.code !== 'ENOENT') throw err;
    },
  );
}

/** Runs a command with its output on stderr, so prepare's stdout stays the protocol. */
const exec: Exec = (command, args, opts = {}) =>
  new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd: opts.cwd, stdio: ['ignore', 2, 2] });
    child.once('error', reject);
    child.once('close', (code, signal) => resolve(code ?? (signal ? 128 : 1)));
  });

/** Runs a command and returns its stdout; its stderr goes to stderr. A failure rejects. */
const output: Output = (command, args, opts = {}) =>
  new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd: opts.cwd, stdio: ['ignore', 'pipe', 2] });
    const chunks: Buffer[] = [];
    child.stdout?.on('data', (c: Buffer) => chunks.push(c));
    child.once('error', reject);
    child.once('close', (code, signal) => {
      if (code === 0) resolve(Buffer.concat(chunks).toString('utf8'));
      else reject(new Error(`${command} ${args[0]} exited ${code ?? signal}`));
    });
  });

async function runPrepare(): Promise<number> {
  try {
    emit(
      await prepare({
        fetch,
        exec,
        output,
        env: process.env,
        now,
        dirs: { goal: paths.goal, workspace: paths.workspace, claude: paths.claudeConfig },
      }),
    );
    return 0;
  } catch (err) {
    process.stderr.write(`goal-agent prepare: ${err instanceof Error ? err.message : String(err)}\n`);
    return 1;
  }
}

async function runAgent(): Promise<number> {
  const abortController = new AbortController();
  // Ending the exec sends SIGTERM: end the agent's session cleanly before the kill.
  process.once('SIGTERM', () => abortController.abort());

  try {
    const spec = parseSpec(await readFile(paths.spec, 'utf8'));
    const files = await readAgentFiles();
    await installSkills();
    return await runTask(spec, files, { query, emit, now, env: process.env, abortController });
  } catch (err) {
    emit(failed(now, `worker setup: ${err instanceof Error ? err.message : String(err)}`));
    return 1;
  }
}

async function main(command: string | undefined): Promise<number> {
  switch (command) {
    case 'prepare':
      return runPrepare();
    case 'run':
      return runAgent();
    default:
      process.stderr.write(`${usage}\n`);
      return 2;
  }
}

process.exitCode = await main(process.argv[2]);
