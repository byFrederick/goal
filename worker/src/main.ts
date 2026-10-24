import { cp, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { query } from '@anthropic-ai/claude-agent-sdk';
import type { WorkerEvent } from './events.js';
import { failed, runTask } from './run.js';
import { type AgentFiles, parseSpec, paths } from './spec.js';

// stdout carries only protocol lines; anything else goes to stderr.
const emit = (event: WorkerEvent) => process.stdout.write(`${JSON.stringify(event)}\n`);
const now = () => new Date();

async function readAgentFiles(): Promise<AgentFiles> {
  const prompt = await readFile(join(paths.agent, 'prompt.md'), 'utf8');
  const schema = await readFile(join(paths.agent, 'output.schema.json'), 'utf8').catch((err: NodeJS.ErrnoException) => {
    if (err.code === 'ENOENT') return undefined;
    throw err;
  });
  return schema === undefined ? { prompt } : { prompt, outputSchema: JSON.parse(schema) };
}

/** Claude Code finds personal skills in its config dir's skills/; home is a throwaway volume. */
async function installSkills(): Promise<void> {
  await cp(join(paths.agent, 'skills'), join(paths.claudeConfig, 'skills'), { recursive: true }).catch(
    (err: NodeJS.ErrnoException) => {
      if (err.code !== 'ENOENT') throw err;
    },
  );
}

async function main(): Promise<number> {
  const abortController = new AbortController();
  // docker stop sends SIGTERM: end the agent's session cleanly before the kill.
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

process.exitCode = await main();
