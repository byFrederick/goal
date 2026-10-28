import type { SDKMessage, query as sdkQuery } from '@anthropic-ai/claude-agent-sdk';
import { agentQuery } from './agent.js';
import { EventMapper, type Finished, type WorkerEvent } from './events.js';
import { NotePoller, notePollMs, PromptQueue } from './notes.js';
import type { AgentFiles, TaskSpec } from './spec.js';

export interface RunDeps {
  query: typeof sdkQuery;
  emit: (event: WorkerEvent) => void;
  now: () => Date;
  env: NodeJS.ProcessEnv;
  abortController: AbortController;
  fetch: typeof globalThis.fetch;
  /** How often notes are polled between turns; 15 s when unset. */
  notePollMs?: number;
}

/**
 * Runs one task and emits its events, ending with exactly one finished event. It returns the
 * process exit code: 0 when the agent produced a result (succeeded or not), 1 when it crashed.
 *
 * The agent's input is a stream: the task's prompt first, then each note left on the item while
 * it works. The stream ends at the result, so the query ends with it.
 */
export async function runTask(spec: TaskSpec, files: AgentFiles, deps: RunDeps): Promise<number> {
  const mapper = new EventMapper(deps.now, spec.skills);
  const { prompt, options } = agentQuery(spec, files, deps.env, deps.abortController, deps.fetch);
  const inbox = new PromptQueue();
  inbox.push({ type: 'user', message: { role: 'user', content: prompt }, parent_tool_use_id: null });
  const token = deps.env.GOAL_GATEWAY_TOKEN;
  const poller =
    spec.gateway_url && token
      ? new NotePoller({
          fetch: deps.fetch,
          gatewayUrl: spec.gateway_url,
          token,
          inbox,
          emit: deps.emit,
          now: deps.now,
          intervalMs: deps.notePollMs ?? notePollMs,
        })
      : undefined;
  let result: Finished | undefined;

  try {
    for await (const msg of deps.query({ prompt: inbox, options })) {
      if (poller && isBetweenTurns(msg)) await poller.poll();
      for (const event of mapper.map(msg)) {
        if (event.type === 'finished') {
          result = event;
          inbox.close();
        } else deps.emit(event);
      }
    }
  } catch (err) {
    deps.emit(failed(deps.now, err instanceof Error ? err.message : String(err)));
    return 1;
  } finally {
    inbox.close();
  }

  deps.emit(result ?? failed(deps.now, 'the agent ended without a result'));
  return result ? 0 : 1;
}

/** Whether the agent is between turns: an assistant message or a tool result has just arrived. */
function isBetweenTurns(msg: SDKMessage): boolean {
  if (msg.type === 'assistant') return true;
  return (
    msg.type === 'user' &&
    Array.isArray(msg.message.content) &&
    msg.message.content.some((b) => b.type === 'tool_result')
  );
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
