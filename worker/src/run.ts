import type { query as sdkQuery } from '@anthropic-ai/claude-agent-sdk';
import { agentQuery } from './agent.js';
import { EventMapper, type Finished, type WorkerEvent } from './events.js';
import type { AgentFiles, TaskSpec } from './spec.js';

export interface RunDeps {
  query: typeof sdkQuery;
  emit: (event: WorkerEvent) => void;
  now: () => Date;
  env: NodeJS.ProcessEnv;
  abortController: AbortController;
}

/**
 * Runs one task and emits its events, ending with exactly one finished event. It returns the
 * process exit code: 0 when the agent produced a result (succeeded or not), 1 when it crashed.
 */
export async function runTask(spec: TaskSpec, files: AgentFiles, deps: RunDeps): Promise<number> {
  const mapper = new EventMapper(deps.now);
  const { prompt, options } = agentQuery(spec, files, deps.env, deps.abortController);
  let result: Finished | undefined;

  try {
    for await (const msg of deps.query({ prompt, options })) {
      for (const event of mapper.map(msg)) {
        if (event.type === 'finished') result = event;
        else deps.emit(event);
      }
    }
  } catch (err) {
    deps.emit(failed(deps.now, err instanceof Error ? err.message : String(err)));
    return 1;
  }

  deps.emit(result ?? failed(deps.now, 'the agent ended without a result'));
  return result ? 0 : 1;
}

export function failed(now: () => Date, error: string): Finished {
  return {
    type: 'finished',
    at: now().toISOString(),
    status: 'failed',
    error,
    cost_usd: 0,
    turns: 0,
    input_tokens: 0,
    output_tokens: 0,
    session_id: '',
  };
}
