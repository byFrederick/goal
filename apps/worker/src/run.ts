import type { SDKMessage, query as sdkQuery } from '@anthropic-ai/claude-agent-sdk';
import { agentQuery } from './agent.js';
import { readBuildCacheState, saveBuildCache } from './cache.js';
import { type MeterOptions, startMeter } from './cgroup.js';
import { repositoryUrl } from './credential.js';
import { applyEnvironment, type EnvironmentDeps } from './environment.js';
import { EventMapper, type Finished, type FinishedRepository, type WorkerEvent } from './events.js';
import { NotePoller, notePollMs, PromptQueue } from './notes.js';
import { type Exec, isCommitId } from './prepare.js';
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
   * The item branch's commit on the remote, read after the agent ends: origin's in the clone at
   * cwd (the workspace when absent), or the repository at url; undefined when the remote has no
   * such branch. Absent, the finished event names no pushed head.
   */
  remoteHead?: (branch: string, at?: { cwd: string } | { url: string }) => Promise<string | undefined>;
  /** The repositories the credential helper answered for, read after the agent ends; none when absent. */
  recorded?: () => Promise<string[]>;
  /** Where and how the spec's environment is applied before the agent starts; needed when the spec has one. */
  environment?: Omit<EnvironmentDeps, 'env'>;
  /**
   * The box's cgroup, /proc paths and prepare's starting CPU reading (src/cgroup.ts): every finished
   * line then carries cpu_seconds, peak_memory_bytes and memory_source where they can be read. Absent,
   * none of them.
   */
  resources?: MeterOptions;
  /**
   * The build cache prepare restored (src/cache.ts): its state file, where the archive is packed,
   * how tar runs and, for a test, the archive's limit. Every finished line then carries the state's
   * build_cache, and after a miss the cache is saved once the agent ended, before the finished line.
   * Absent, no build_cache.
   */
  buildCache?: { state: string; archive: string; exec: Exec; log?: (line: string) => void; maxBytes?: number };
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
 *
 * With deps.resources, the box is measured from the start (src/cgroup.ts), and the finished event
 * carries cpu_seconds, peak_memory_bytes and memory_source where they can be read; a counter that
 * can't be read leaves its field out and changes nothing else.
 *
 * With deps.buildCache, the finished event carries the build cache's outcome from prepare's state
 * file. After a miss, and once the agent ran, the cache is saved before that event (saveBuildCache);
 * a failed save is logged and changes neither the event nor the exit code.
 */
export async function runTask(spec: TaskSpec, files: AgentFiles, deps: RunDeps): Promise<number> {
  const meter = deps.resources ? startMeter(deps.resources) : undefined;
  const finish = async (f: Finished, ran = true) => {
    const build_cache = await settleBuildCache(spec, deps, ran);
    deps.emit({ ...f, ...(build_cache ? { build_cache } : {}), ...(await meter?.stop()) });
  };
  try {
    return await runAgent(spec, files, deps, finish);
  } finally {
    await meter?.stop();
  }
}

/** runTask's run, which ends with finish: the one finished event, given the box's usage. */
async function runAgent(
  spec: TaskSpec,
  files: AgentFiles,
  deps: RunDeps,
  finish: (f: Finished, ran?: boolean) => Promise<void>,
): Promise<number> {
  if (spec.environment) {
    try {
      if (!deps.environment) throw new Error('environment setup: the worker has no shell to apply it');
      await applyEnvironment(spec.environment, { ...deps.environment, env: deps.env });
    } catch (err) {
      await finish(failed(deps.now, err instanceof Error ? err.message : String(err)), false);
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
    await finish(await withPushes(spec, deps, failed(deps.now, err instanceof Error ? err.message : String(err))));
    return 1;
  } finally {
    inbox.close();
  }

  await finish(await withPushes(spec, deps, result ?? failed(deps.now, 'the agent ended without a result')));
  return result ? 0 : 1;
}

/**
 * The build cache's outcome from prepare's state file, saving the cache first after a miss when the
 * agent ran; undefined without deps.buildCache or a state.
 */
async function settleBuildCache(spec: TaskSpec, deps: RunDeps, ran: boolean): Promise<Finished['build_cache']> {
  const cache = deps.buildCache;
  if (!cache) return undefined;
  const state = await readBuildCacheState(cache.state);
  if (!state) return undefined;
  const token = deps.env.GOAL_GATEWAY_TOKEN;
  if (ran && state.outcome === 'miss' && spec.gateway_url && token) {
    await saveBuildCache(
      {
        fetch: deps.fetch,
        exec: cache.exec,
        gateway: spec.gateway_url,
        token,
        archive: cache.archive,
        log: cache.log ?? ((line) => process.stderr.write(`${line}\n`)),
        ...(cache.maxBytes === undefined ? {} : { maxBytes: cache.maxBytes }),
      },
      state,
    );
  }
  return state.outcome;
}

/**
 * The finished event with what the run pushed (ADR 0039 decision 8, ADR 0041 decision 5): the
 * agent pushes with its own git, so each head is the item branch's commit on the remote once it
 * ended. With spec.repositories, one entry per repository prepare cloned or the credential helper
 * answered for, by its URL, once; a repository the agent opened itself is looked up by URL and has
 * no starting head. Without them, no heads. A lookup that
 * finds nothing or fails leaves that head out.
 */
async function withPushes(spec: TaskSpec, deps: RunDeps, f: Finished): Promise<Finished> {
  const { remoteHead } = deps;
  if (!remoteHead) return f;
  const lookup = (branch: string, at?: { cwd: string } | { url: string }) =>
    remoteHead(branch, at).catch((err: unknown) => {
      process.stderr.write(`goal-agent run: the pushed head: ${err instanceof Error ? err.message : String(err)}\n`);
      return undefined;
    });
  if (!spec.repositories) return f;

  const repositories: FinishedRepository[] = [];
  const seen = new Set<string>();
  for (const c of spec.repositories) {
    const key = repositoryUrl(c.url);
    if (seen.has(key)) continue;
    seen.add(key);
    const pushed = await lookup(c.branch, { cwd: c.path });
    repositories.push({
      repo: c.url,
      ...(c.base_commit ? { base_commit: c.base_commit } : {}),
      ...(c.branch_head ? { branch_head: c.branch_head } : {}),
      ...(pushed ? { pushed_head: pushed } : {}),
      wrote: pushed !== undefined && pushed !== c.branch_head,
    });
  }
  const branch = spec.repositories[0]?.branch ?? spec.branch;
  const recorded = await (deps.recorded?.() ?? Promise.resolve([])).catch(() => [] as string[]);
  for (const url of recorded) {
    const key = repositoryUrl(url);
    if (key === '' || seen.has(key)) continue;
    seen.add(key);
    const pushed = branch ? await lookup(branch, { url }) : undefined;
    repositories.push({ repo: url, ...(pushed ? { pushed_head: pushed } : {}), wrote: pushed !== undefined });
  }
  return { ...f, repositories };
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
