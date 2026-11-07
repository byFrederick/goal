import { spawn } from 'node:child_process';
import { cp, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { query } from '@anthropic-ai/claude-agent-sdk';
import { cgroupRoot, cpuStartFile, meminfoFile, selfCgroupFile } from './cgroup.js';
import { runCheck } from './check.js';
import { runDefinition } from './definition.js';
import type { Shell } from './environment.js';
import type { WorkerEvent } from './events.js';
import { runGrade } from './grade.js';
import { runJonah } from './jonah.js';
import { type Exec, type Output, type Prepared, prepare } from './prepare.js';
import { failed, lsRemoteHead, runTask } from './run.js';
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

/**
 * Runs a command with its output on stderr, so prepare's stdout stays the protocol. With opts.stderr,
 * the command's stderr still goes to stderr and is also passed there; opts.signal kills the command.
 */
const exec: Exec = (command, args, opts = {}) =>
  new Promise((resolve, reject) => {
    const child = spawn(command, args, { cwd: opts.cwd, stdio: ['ignore', 2, opts.stderr ? 'pipe' : 2] });
    const { stderr } = opts;
    if (stderr) {
      child.stderr?.on('data', (c: Buffer) => {
        process.stderr.write(c);
        stderr(c.toString('utf8'));
      });
    }
    const kill = () => child.kill('SIGTERM');
    opts.signal?.addEventListener('abort', kill, { once: true });
    child.once('error', reject);
    child.once('close', (code, signal) => {
      opts.signal?.removeEventListener('abort', kill);
      resolve(code ?? (signal ? 128 : 1));
    });
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

/** How long reading the pushed head may take before the run ends without it. */
const remoteHeadTimeoutMs = 60_000;

/**
 * The item branch's commit on origin, from the workspace or another clone (at.cwd), or on the
 * repository at at.url, with the run's own git login. git never prompts (a missing login fails the
 * lookup) and is stopped after remoteHeadTimeoutMs.
 */
const remoteHead = (branch: string, at?: { cwd: string } | { url: string }): Promise<string | undefined> =>
  new Promise((resolve, reject) => {
    const remote = at && 'url' in at ? at.url : 'origin';
    const child = spawn('git', ['ls-remote', '--', remote, `refs/heads/${branch}`], {
      cwd: at && 'cwd' in at ? at.cwd : paths.workspace,
      env: { ...process.env, GIT_TERMINAL_PROMPT: '0' },
      stdio: ['ignore', 'pipe', 2],
      timeout: remoteHeadTimeoutMs,
    });
    const chunks: Buffer[] = [];
    child.stdout?.on('data', (c: Buffer) => chunks.push(c));
    child.once('error', reject);
    child.once('close', (code, signal) => {
      if (code === 0) resolve(lsRemoteHead(Buffer.concat(chunks).toString('utf8'), branch));
      else reject(new Error(`git ls-remote exited ${code ?? signal}`));
    });
  });

/**
 * Runs an environment's setup command with sh -c in its own process group, its output on stderr
 * (stdout is the event protocol) and passed to onOutput. Past the time limit, or when signal aborts,
 * the whole group is killed, so nothing the command started outlives it.
 */
const shellFor =
  (signal: AbortSignal): Shell =>
  (command, opts) =>
    new Promise((resolve, reject) => {
      const child = spawn('sh', ['-c', command], { cwd: opts.cwd, detached: true, stdio: ['ignore', 'pipe', 'pipe'] });
      let timedOut = false;
      const kill = () => {
        try {
          if (child.pid !== undefined) process.kill(-child.pid, 'SIGKILL');
        } catch {
          // the group has already exited
        }
      };
      const timer = setTimeout(() => {
        timedOut = true;
        kill();
      }, opts.timeoutMs);
      signal.addEventListener('abort', kill, { once: true });
      const pass = (c: Buffer) => {
        process.stderr.write(c);
        opts.onOutput(c.toString('utf8'));
      };
      child.stdout?.on('data', pass);
      child.stderr?.on('data', pass);
      child.once('error', (err) => {
        clearTimeout(timer);
        signal.removeEventListener('abort', kill);
        reject(err);
      });
      child.once('close', (code, sig) => {
        clearTimeout(timer);
        signal.removeEventListener('abort', kill);
        resolve({ code: code ?? (sig ? 128 : 1), timedOut });
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
        dirs: {
          goal: paths.goal,
          workspace: paths.workspace,
          repos: paths.repos,
          claude: paths.claudeConfig,
          home: paths.home,
          knowledge: paths.knowledge,
        },
        cgroup: cgroupRoot,
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
    // A draft request's run (ADR 0021) reads no agent files and installs no skills.
    // A grade run (ADR 0039 decision 10) reads no agent files and installs no skills either.
    if (spec.grade) return await runGrade(spec, { query, emit, now, env: process.env, abortController });
    if (spec.draft_request) return await runDefinition(spec, { query, emit, now, env: process.env, abortController });
    // A Jonah review (ADR 0034) reads no agent files and installs no skills either.
    if (spec.jonah) return await runJonah(spec, { query, emit, now, env: process.env, abortController });
    const files = await readAgentFiles();
    await installSkills();
    // A check (ADR 0040 decision 19) runs its agent as a task that only reads (src/check.ts).
    const run = spec.check ? runCheck : runTask;
    return await run(spec, files, {
      query,
      emit,
      now,
      env: process.env,
      abortController,
      fetch,
      remoteHead,
      resources: {
        root: cgroupRoot,
        selfCgroup: selfCgroupFile,
        meminfo: meminfoFile,
        startFile: join(paths.goal, cpuStartFile),
      },
      recorded: () =>
        readFile(paths.repositoryRecord, 'utf8').then(
          (s) => s.split('\n').filter((l) => l !== ''),
          (err: NodeJS.ErrnoException) => {
            if (err.code === 'ENOENT') return [];
            throw err;
          },
        ),
      environment: {
        home: paths.home,
        workspace: paths.workspace,
        marker: join(paths.goal, 'environment-setup.done'),
        shell: shellFor(abortController.signal),
      },
    });
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
