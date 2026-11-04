import type { SDKMessage, query as sdkQuery } from '@anthropic-ai/claude-agent-sdk';
import { agentQuery } from './agent.js';
import { applyEnvironment, type EnvironmentDeps } from './environment.js';
import { EventMapper, type Finished, type WorkerEvent } from './events.js';
import { NotePoller, notePollMs, PromptQueue } from './notes.js';
import { isCommitId } from './prepare.js';
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
  /**
   * The item branch's commit on the remote, read after the agent ends; undefined when the remote
   * has no such branch. Absent, the finished event names no pushed head.
   */
  remoteHead?: (branch: string) => Promise<string | undefined>;
  /** Where and how the spec's environment is applied before the agent starts; needed when the spec has one. */
  environment?: Omit<EnvironmentDeps, 'env'>;
}

/**
 * Runs one task and emits its events, ending with exactly one finished event. It returns the
 * process exit code: 0 when the agent produced a result (succeeded or not), 1 when it crashed.
 *
 * On a spec with an environment (ADR 0037) its files are written first and its setup runs, once per
 * box (applyEnvironment's marker); a failure there ends the run with a failed event before the
 * agent is queried. A try's environment (setup_only) gives its env to setup alone: its variables
 * are removed before the agent starts.
 *
 * The agent's input is a stream: the task's prompt first, then each note left on the item while
 * it works. The stream ends at the result, so the query ends with it.
 */
export async function runTask(spec: TaskSpec, files: AgentFiles, deps: RunDeps): Promise<number> {
  if (spec.environment) {
    try {
      if (!deps.environment) throw new Error('environment setup: the worker has no shell to apply it');
      await applyEnvironment(spec.environment, { ...deps.environment, env: deps.env });
    } catch (err) {
      deps.emit(failed(deps.now, err instanceof Error ? err.message : String(err)));
      return 1;
    }
    // A try's env is setup's alone (ADR 0039 decision 9): the agent starts without it.
    if (spec.environment.setup_only) {
      for (const name of spec.environment.env) delete deps.env[name];
    }
  }
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
    deps.emit(await withPushedHead(spec, deps, failed(deps.now, err instanceof Error ? err.message : String(err))));
    return 1;
  } finally {
    inbox.close();
  }

  deps.emit(await withPushedHead(spec, deps, result ?? failed(deps.now, 'the agent ended without a result')));
  return result ? 0 : 1;
}

/**
 * The finished event with the head the run pushed (ADR 0039, decision 8): the agent pushes with
 * its own git, so this is the item branch's commit on the remote once it ended. Without a
 * repository, or when the lookup finds nothing or fails, the event is as it was.
 */
async function withPushedHead(spec: TaskSpec, deps: RunDeps, f: Finished): Promise<Finished> {
  if (!spec.branch || !deps.remoteHead) return f;
  const head = await deps.remoteHead(spec.branch).catch((err: unknown) => {
    process.stderr.write(`goal-agent run: the pushed head: ${err instanceof Error ? err.message : String(err)}\n`);
    return undefined;
  });
  return head ? { ...f, pushed_head: head } : f;
}

/** The commit `git ls-remote origin refs/heads/<branch>` printed for branch, if it printed one. */
export function lsRemoteHead(out: string, branch: string): string | undefined {
  for (const line of out.split('\n')) {
    const [sha, ref] = line.trim().split('\t');
    if (ref === `refs/heads/${branch}` && sha && isCommitId(sha)) return sha;
  }
  return undefined;
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
