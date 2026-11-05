import { mkdtemp, readFile, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { CanUseTool, Options, query, SDKMessage, SDKUserMessage } from '@anthropic-ai/claude-agent-sdk';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { untrustedDataSection } from './agent.js';
import type { Shell } from './environment.js';
import type { WorkerEvent } from './events.js';
import { lsRemoteHead, type RunDeps, runTask } from './run.js';
import { type AgentFiles, parseSpec, type TaskSpec } from './spec.js';

const spec: TaskSpec = parseSpec(
  JSON.stringify({
    factory_id: 'f1',
    item_id: 'i1',
    run_id: 'r1',
    station: 'build',
    agent: 'engineer',
    model: 'claude-sonnet-5-5',
    skills: ['acme-conventions'],
    tools_allow: ['Read', 'Edit', 'Bash(npm test*)'],
    tools_deny: ['Bash(git push*)'],
    gateway_url: 'http://host.docker.internal:8081/mcp',
    brief: 'Add a health endpoint',
    inputs: { spec: { lane: 'backend' } },
    output: 'change',
    max_turns: 40,
    max_usd: 6,
  }),
);
const files: AgentFiles = { prompt: 'You are the engineer.', outputSchema: { type: 'object' } };
const env = {
  PATH: '/usr/bin',
  CLAUDE_CODE_OAUTH_TOKEN: 'oauth-token',
  GOAL_GATEWAY_TOKEN: 'gateway-token',
};
const at = new Date('2026-10-23T10:00:00Z');

const usage = { input_tokens: 100, output_tokens: 20, cache_read_input_tokens: 5, cache_creation_input_tokens: 0 };
const success = {
  type: 'result',
  subtype: 'success',
  is_error: false,
  num_turns: 3,
  total_cost_usd: 0.42,
  usage,
  result: 'done',
  structured_output: { summary: 'added /healthz' },
  session_id: 's1',
};

/** One query call: the inputs the worker streamed to it, read to the end of the stream, and its options. */
interface Call {
  inputs: SDKUserMessage[];
  read: Promise<void>;
  options: Options;
}

/** A query that plays scripted messages, reads its input stream as the SDK does, and records what it was given. */
function scripted(messages: unknown[], opts: { throws?: Error } = {}) {
  const calls: Call[] = [];
  const fake = ((params: { prompt: AsyncIterable<SDKUserMessage>; options: Options }) => {
    const inputs: SDKUserMessage[] = [];
    const read = (async () => {
      for await (const m of params.prompt) inputs.push(m);
    })();
    calls.push({ inputs, read, options: params.options });
    return (async function* () {
      for (const m of messages) yield m as SDKMessage;
      if (opts.throws) throw opts.throws;
    })();
  }) as unknown as typeof query;
  return { fake, calls };
}

/** The prompt text of a call's first input, the task's prompt. */
const promptOf = (call?: Call) => String(call?.inputs[0]?.message.content ?? '');

/** goald's gateway as fetch: each request is recorded and answered by respond. */
function gateway(respond: (url: URL) => Response | Promise<Response> = () => Response.json({ notes: [] })) {
  const requests: { url: URL; init: RequestInit | undefined }[] = [];
  const fetch = (async (input: string | URL, init?: RequestInit) => {
    const url = new URL(String(input));
    requests.push({ url, init });
    return respond(url);
  }) as typeof globalThis.fetch;
  return { fetch, requests };
}

async function run(
  messages: unknown[],
  opts: {
    throws?: Error;
    spec?: TaskSpec;
    files?: AgentFiles;
    env?: NodeJS.ProcessEnv;
    fetch?: typeof globalThis.fetch;
    remoteHead?: RunDeps['remoteHead'];
    recorded?: RunDeps['recorded'];
    environment?: RunDeps['environment'];
  } = {},
) {
  const { fake, calls } = scripted(messages, opts);
  const events: WorkerEvent[] = [];
  const code = await runTask(opts.spec ?? spec, opts.files ?? files, {
    query: fake,
    emit: (e) => events.push(e),
    now: () => at,
    env: opts.env ?? env,
    abortController: new AbortController(),
    fetch: opts.fetch ?? gateway().fetch,
    notePollMs: 0,
    ...(opts.remoteHead ? { remoteHead: opts.remoteHead } : {}),
    ...(opts.recorded ? { recorded: opts.recorded } : {}),
    ...(opts.environment ? { environment: opts.environment } : {}),
  });
  for (const call of calls) await call.read;
  return { code, events, calls };
}

describe('runTask', () => {
  it("runs the agent with the task policy, the gateway and the model's access goald gave it", async () => {
    const { calls } = await run([success]);
    const options = calls[0]?.options;

    expect(options).toMatchObject({
      cwd: '/workspace',
      model: 'claude-sonnet-5-5',
      systemPrompt: {
        type: 'preset',
        preset: 'claude_code',
        append: `You are the engineer.\n\n${untrustedDataSection}`,
      },
      tools: ['Read', 'Edit', 'Bash', 'Skill'],
      allowedTools: ['Read', 'Edit', 'Bash(npm test*)', 'mcp__goal'],
      disallowedTools: ['Bash(git push*)'],
      permissionMode: 'default',
      skills: ['acme-conventions'],
      mcpServers: {
        goal: {
          type: 'http',
          url: 'http://host.docker.internal:8081/mcp',
          headers: { Authorization: 'Bearer gateway-token' },
          alwaysLoad: true,
        },
      },
      strictMcpConfig: true,
      includePartialMessages: true,
      maxTurns: 40,
      maxBudgetUsd: 6,
      outputFormat: { type: 'json_schema', schema: { type: 'object' } },
    });
    expect(Object.keys(options?.mcpServers ?? {})).toEqual(['goal']);
    expect(options?.env).toMatchObject({ CLAUDE_CODE_OAUTH_TOKEN: 'oauth-token', PATH: '/usr/bin' });
    expect(options?.env).not.toHaveProperty('GOAL_GATEWAY_TOKEN');
    expect(promptOf(calls[0])).toContain('Add a health endpoint');
    expect(promptOf(calls[0])).toContain('"lane": "backend"');
    expect(promptOf(calls[0])).toContain('output contract "change"');
  });

  it("gives the agent only its definition's skills: no bundled, synced or plugin skills, its own config dir", async () => {
    for (const skills of [['acme-conventions'], []]) {
      const { calls } = await run([success], { spec: { ...spec, skills } as TaskSpec });
      const options = calls[0]?.options;

      expect(options?.skills).toEqual(skills);
      expect(options?.plugins ?? []).toEqual([]);
      expect(options?.settingSources).toEqual(['user', 'project']);
      expect(options?.settings).toEqual({
        disableBundledSkills: true,
        syncClaudeAiSkills: false,
        syncClaudeAiPlugins: false,
        disableClaudeAiConnectors: true,
      });
      expect(options?.env).toMatchObject({ CLAUDE_CONFIG_DIR: '/home/node/.claude' });
    }
  });

  it("turns off Claude Code's nonessential traffic: the egress proxy lets agents reach the model API only", async () => {
    const { calls } = await run([success], { env: { ...env, CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '0' } });

    expect(calls[0]?.options.env).toMatchObject({ CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: '1' });
  });

  it('keeps a config dir from the environment from replacing its own', async () => {
    const { calls } = await run([success], { env: { ...env, CLAUDE_CONFIG_DIR: '/host/.claude' } });

    expect(calls[0]?.options.env).toMatchObject({ CLAUDE_CONFIG_DIR: '/home/node/.claude' });
  });

  it('reports only the skills the agent may use as started, not every skill Claude Code found', async () => {
    const { events } = await run([
      {
        type: 'system',
        subtype: 'init',
        session_id: 's1',
        model: 'claude-sonnet-5-5',
        tools: ['Read'],
        skills: ['acme-conventions', 'loop', 'checkout-skill', 'plugin-authoring'],
        mcp_servers: [],
      },
      success,
    ]);

    expect(events[0]).toMatchObject({ type: 'started', skills: ['acme-conventions'] });
  });

  it('leaves gateway tool rules to the gateway', async () => {
    const { calls } = await run([success], {
      spec: {
        ...spec,
        tools_allow: ['Read', 'github__get_*', 'github__push_files'],
        tools_deny: ['Bash(git push*)', 'github__merge_*'],
      } as TaskSpec,
    });

    expect(calls[0]?.options).toMatchObject({
      tools: ['Read', 'Skill'],
      allowedTools: ['Read', 'mcp__goal'],
      disallowedTools: ['Bash(git push*)'],
    });
  });

  it('allows only read-only tools when the agent allows none', async () => {
    const { calls } = await run([success], {
      spec: { ...spec, tools_allow: [], skills: [], gateway_url: undefined } as TaskSpec,
    });

    expect(calls[0]?.options).toMatchObject({
      tools: ['Read', 'Grep', 'Glob'],
      allowedTools: ['Read', 'Grep', 'Glob'],
      mcpServers: {},
    });
  });

  it('puts the path of each Read, Grep and Glob call in its tool_use event', async () => {
    const long = `/knowledge/runbooks/${'deep/'.repeat(120)}disk.md`;
    const { events } = await run([
      {
        type: 'assistant',
        message: {
          id: 'msg_1',
          content: [
            { type: 'tool_use', id: 'tu_1', name: 'Read', input: { file_path: long } },
            { type: 'tool_use', id: 'tu_2', name: 'Grep', input: { pattern: 'disk', path: '/knowledge/runbooks' } },
            { type: 'tool_use', id: 'tu_3', name: 'Glob', input: { pattern: '**/*.md', path: '/knowledge' } },
            { type: 'tool_use', id: 'tu_4', name: 'Grep', input: { pattern: 'disk' } },
            { type: 'tool_use', id: 'tu_5', name: 'Bash', input: { path: '/knowledge/runbooks' } },
          ],
          usage: { ...usage, output_tokens: 1 },
        },
        parent_tool_use_id: null,
        session_id: 's1',
      },
      success,
    ]);

    const paths = events.flatMap((e) => (e.type === 'tool_use' ? [[e.id, e.path]] : []));
    // The path is never clipped, unlike the input, so a long one still counts.
    expect(paths).toEqual([
      ['tu_1', long],
      ['tu_2', '/knowledge/runbooks'],
      ['tu_3', '/knowledge'],
      ['tu_4', undefined],
      ['tu_5', undefined],
    ]);
  });

  it('streams progress events and ends with the result', async () => {
    const assistant = (content: unknown[]) => ({
      type: 'assistant',
      message: { id: 'msg_1', content, usage: { ...usage, output_tokens: 1 } },
      parent_tool_use_id: null,
      session_id: 's1',
    });
    const stream = (event: unknown) => ({ type: 'stream_event', event, parent_tool_use_id: null, session_id: 's1' });
    const { code, events } = await run([
      {
        type: 'system',
        subtype: 'init',
        session_id: 's1',
        model: 'claude-sonnet-5-5',
        tools: ['Read', 'mcp__goal__kb_search'],
        skills: ['acme-conventions'],
        mcp_servers: [{ name: 'goal', status: 'connected' }],
      },
      stream({ type: 'message_start', message: { id: 'msg_1', usage: { ...usage, output_tokens: 1 } } }),
      assistant([{ type: 'text', text: 'Looking at the router.' }]),
      assistant([{ type: 'tool_use', id: 'tu_1', name: 'Bash', input: { command: 'npm test' } }]),
      stream({ type: 'message_delta', delta: { stop_reason: 'tool_use' }, usage: { output_tokens: 45 } }),
      stream({ type: 'message_stop' }),
      {
        type: 'user',
        message: {
          role: 'user',
          content: [{ type: 'tool_result', tool_use_id: 'tu_1', content: [{ type: 'text', text: 'ok' }] }],
        },
        parent_tool_use_id: null,
      },
      success,
    ]);

    expect(code).toBe(0);
    expect(events.map((e) => e.type)).toEqual(['started', 'message', 'tool_use', 'usage', 'tool_result', 'finished']);
    // One usage event per API call, with its final counts: the assistant messages only carry
    // the stream's opening placeholder (output_tokens 1).
    expect(events).toContainEqual({
      type: 'usage',
      at: at.toISOString(),
      input_tokens: 100,
      output_tokens: 45,
      cache_read_input_tokens: 5,
      cache_creation_input_tokens: 0,
    });
    expect(events).toContainEqual({
      type: 'tool_use',
      at: at.toISOString(),
      id: 'tu_1',
      tool: 'Bash',
      input: '{"command":"npm test"}',
    });
    expect(events).toContainEqual({
      type: 'tool_result',
      at: at.toISOString(),
      id: 'tu_1',
      is_error: false,
      summary: 'ok',
    });
    // Bash names no path: goald counts only Read, Grep and Glob.
    expect(events.find((e) => e.type === 'tool_use')).not.toHaveProperty('path');
    expect(events.at(-1)).toEqual({
      type: 'finished',
      at: at.toISOString(),
      status: 'succeeded',
      subtype: 'success',
      output: { summary: 'added /healthz' },
      cost_usd: 0.42,
      turns: 3,
      input_tokens: 100,
      output_tokens: 20,
      session_id: 's1',
    });
  });

  it('reports the tool calls its policy denied, by tool and call id, never their input', async () => {
    const { events } = await run([
      {
        ...success,
        permission_denials: [
          { tool_name: 'Bash', tool_use_id: 'tu_7', tool_input: { command: 'aws ssm put-parameter --value s3cret' } },
          { tool_name: 'mcp__goal__github__merge_pr', tool_use_id: 'tu_9', tool_input: {} },
        ],
      },
    ]);

    expect(events.at(-1)).toMatchObject({
      type: 'finished',
      status: 'succeeded',
      permission_denials: [
        { tool: 'Bash', id: 'tu_7' },
        { tool: 'mcp__goal__github__merge_pr', id: 'tu_9' },
      ],
    });
    expect(JSON.stringify(events)).not.toContain('s3cret');
  });

  it('reports a budget or turn limit as a failed result', async () => {
    const { code, events } = await run([
      { ...success, subtype: 'error_max_turns', errors: ['reached 40 turns'], structured_output: undefined },
    ]);

    expect(code).toBe(0);
    expect(events.at(-1)).toMatchObject({
      type: 'finished',
      status: 'failed',
      subtype: 'error_max_turns',
      error: 'reached 40 turns',
    });
  });

  it('reads JSON from the final reply when the station has no schema', async () => {
    const reply = (result: string) =>
      run([{ ...success, structured_output: undefined, result }], { files: { prompt: 'p' } });

    expect((await reply('Done.\n```json\n{"decision":"approve"}\n```')).events.at(-1)).toMatchObject({
      output: { decision: 'approve' },
    });
    expect((await reply('I could not decide.')).events.at(-1)).toMatchObject({
      status: 'succeeded',
      output: 'I could not decide.',
    });
  });

  it('fails with the reason when the agent crashes or ends without a result', async () => {
    const crashed = await run([], { throws: new Error('Claude Code process exited with code 1') });
    expect(crashed.code).toBe(1);
    expect(crashed.events.at(-1)).toMatchObject({ status: 'failed', error: 'Claude Code process exited with code 1' });

    const silent = await run([]);
    expect(silent.code).toBe(1);
    expect(silent.events).toEqual([expect.objectContaining({ type: 'finished', status: 'failed' })]);
  });
});

describe('runTask pushed head', () => {
  const head = 'c'.repeat(40);
  const onBranch = parseSpec(JSON.stringify({ ...spec, branch: 'goal/1-add-a-health-endpoint' }));

  it("reports the item branch's head on the remote when the agent ended", async () => {
    const asked: string[] = [];
    const remoteHead = async (branch: string) => {
      asked.push(branch);
      return head;
    };

    const { code, events } = await run([success], { spec: onBranch, remoteHead });

    expect(code).toBe(0);
    expect(asked).toEqual(['goal/1-add-a-health-endpoint']);
    expect(events.at(-1)).toMatchObject({ type: 'finished', status: 'succeeded', pushed_head: head });
  });

  it('reports it after a crash too, as the agent may have pushed before', async () => {
    const { code, events } = await run([], { spec: onBranch, remoteHead: async () => head, throws: new Error('boom') });

    expect(code).toBe(1);
    expect(events.at(-1)).toMatchObject({ status: 'failed', error: 'boom', pushed_head: head });
  });

  it('leaves it out when the lookup fails or finds no branch, and the run ends as it would', async () => {
    for (const remoteHead of [async () => undefined, () => Promise.reject(new Error('git ls-remote exited 128'))]) {
      const { code, events } = await run([success], { spec: onBranch, remoteHead });

      expect(code).toBe(0);
      expect(events.at(-1)).toMatchObject({ type: 'finished', status: 'succeeded' });
      expect(events.at(-1)).not.toHaveProperty('pushed_head');
    }
  });

  it('never looks without a repository', async () => {
    const asked: string[] = [];
    const { events } = await run([success], {
      remoteHead: async (b) => {
        asked.push(b);
        return head;
      },
    });

    expect(asked).toEqual([]);
    expect(events.at(-1)).not.toHaveProperty('pushed_head');
  });
});

describe('runTask repositories', () => {
  const branch = 'goal/1-add-a-health-endpoint';
  const [base, before, pushed, other] = ['a'.repeat(40), 'b'.repeat(40), 'c'.repeat(40), 'd'.repeat(40)] as const;
  const withRepos = parseSpec(
    JSON.stringify({
      ...spec,
      branch,
      repositories: [
        {
          url: 'https://github.com/acme/app',
          path: '/workspace',
          branch,
          write: true,
          base_commit: base,
          branch_head: before,
        },
        {
          url: 'https://github.com/acme/lib',
          path: '/repos/acme/lib',
          branch,
          write: false,
          base_commit: base,
          branch_head: base,
        },
      ],
    }),
  );
  type At = { cwd: string } | { url: string } | undefined;

  it('reports one entry per repository cloned or recorded, once, with its heads and whether it wrote', async () => {
    const asked: { branch: string; at: At }[] = [];
    const heads: Record<string, string> = {
      '/workspace': pushed,
      '/repos/acme/lib': base,
      'https://github.com/acme/web': other,
    };
    const remoteHead = async (b: string, at?: At) => {
      asked.push({ branch: b, at });
      return at ? heads['cwd' in at ? at.cwd : at.url] : undefined;
    };
    // The helper records the clones too, and a repository once per request.
    const recorded = async () => [
      'https://github.com/acme/app',
      'https://github.com/acme/web',
      'https://github.com/acme/web',
      'https://github.com/acme/docs',
    ];

    const { code, events } = await run([success], { spec: withRepos, remoteHead, recorded });

    expect(code).toBe(0);
    expect(asked).toEqual([
      { branch, at: { cwd: '/workspace' } },
      { branch, at: { cwd: '/repos/acme/lib' } },
      { branch, at: { url: 'https://github.com/acme/web' } },
      { branch, at: { url: 'https://github.com/acme/docs' } },
    ]);
    const finished = events.at(-1);
    expect(finished).toMatchObject({ type: 'finished', status: 'succeeded', pushed_head: pushed });
    expect(finished).toHaveProperty('repositories', [
      { repo: 'https://github.com/acme/app', base_commit: base, branch_head: before, pushed_head: pushed, wrote: true },
      // The remote's branch is the head prepare saw: nothing new was pushed.
      { repo: 'https://github.com/acme/lib', base_commit: base, branch_head: base, pushed_head: base, wrote: false },
      // Opened by the agent: no starting head, so a branch there counts as written.
      { repo: 'https://github.com/acme/web', pushed_head: other, wrote: true },
      { repo: 'https://github.com/acme/docs', wrote: false },
    ]);
  });

  it("leaves a failed lookup's pushed head out and ends the run as it would", async () => {
    const remoteHead = async (_b: string, at?: At) => {
      if (at && 'cwd' in at && at.cwd === '/workspace') throw new Error('git ls-remote exited 128');
      return base;
    };

    const { code, events } = await run([success], { spec: withRepos, remoteHead });

    expect(code).toBe(0);
    const finished = events.at(-1);
    expect(finished).toMatchObject({ type: 'finished', status: 'succeeded' });
    expect(finished).not.toHaveProperty('pushed_head');
    expect(finished).toHaveProperty('repositories', [
      { repo: 'https://github.com/acme/app', base_commit: base, branch_head: before, wrote: false },
      { repo: 'https://github.com/acme/lib', base_commit: base, branch_head: base, pushed_head: base, wrote: false },
    ]);
  });

  it('reports them after a crash too', async () => {
    const { code, events } = await run([], {
      spec: withRepos,
      remoteHead: async () => pushed,
      throws: new Error('boom'),
    });

    expect(code).toBe(1);
    expect(events.at(-1)).toMatchObject({ status: 'failed', error: 'boom', pushed_head: pushed });
    expect((events.at(-1) as { repositories: unknown[] }).repositories).toHaveLength(2);
  });
});

describe('lsRemoteHead', () => {
  const sha = 'd'.repeat(40);

  it("reads the branch's commit from git ls-remote's output", () => {
    expect(lsRemoteHead(`${sha}\trefs/heads/goal/1-x\n`, 'goal/1-x')).toBe(sha);
  });

  it('reads nothing for another ref, no output or something that is not a commit id', () => {
    expect(lsRemoteHead(`${sha}\trefs/heads/goal/1-xy\n`, 'goal/1-x')).toBeUndefined();
    expect(lsRemoteHead('', 'goal/1-x')).toBeUndefined();
    expect(lsRemoteHead('main\trefs/heads/goal/1-x\n', 'goal/1-x')).toBeUndefined();
  });
});

describe('parseSpec', () => {
  it('starts the prompt with a note to resolve conflicts with the default branch', async () => {
    const { calls } = await run([success], { spec: { ...spec, base_conflicts: ['go.mod', 'internal/api/server.go'] } });
    const prompt = promptOf(calls[0]);

    expect(prompt.startsWith('## First: conflicts with the default branch')).toBe(true);
    expect(prompt).toContain('- go.mod\n- internal/api/server.go');
    expect(prompt.indexOf('internal/api/server.go')).toBeLessThan(prompt.indexOf('Add a health endpoint'));
    expect(promptOf((await run([success])).calls[0]).startsWith('Add a health endpoint')).toBe(true);
  });

  it('rejects a spec without its run id', () => {
    expect(() => parseSpec(JSON.stringify({ ...spec, run_id: undefined }))).toThrow();
  });
});

describe('runTask live notes', () => {
  const init = {
    type: 'system',
    subtype: 'init',
    session_id: 's1',
    model: 'claude-sonnet-5-5',
    tools: ['Read', 'Bash'],
    skills: [],
    mcp_servers: [],
  };
  const assistant = { type: 'assistant', message: { id: 'msg_1', content: [] }, parent_tool_use_id: null };
  const toolResult = {
    type: 'user',
    message: { role: 'user', content: [{ type: 'tool_result', tool_use_id: 'tu_1', content: 'ok' }] },
    parent_tool_use_id: null,
  };
  const note = { id: 7, at: '2026-10-23T10:01:00Z', by: 'alice@example.com', text: 'Use the v2 endpoint' };
  const noteMessage = {
    type: 'user',
    message: { role: 'user', content: 'A note from alice@example.com arrived while you work: Use the v2 endpoint' },
    parent_tool_use_id: null,
    priority: 'next',
  };
  const noteDelivered = { type: 'note_delivered', at: at.toISOString(), note_id: 7 };
  const finishedEvents = (events: WorkerEvent[]) => events.filter((e) => e.type === 'finished');

  afterEach(() => vi.restoreAllMocks());

  it('hands a note served mid-run to the agent once, and reports it delivered once', async () => {
    // The gateway serves the same note on every poll, as if it did not know the note was delivered.
    const served = gateway(() => Response.json({ notes: [note] }));
    const { code, events, calls } = await run([init, assistant, toolResult, success], { fetch: served.fetch });

    expect(code).toBe(0);
    expect(calls[0]?.inputs).toHaveLength(2);
    expect(promptOf(calls[0])).toContain('Add a health endpoint');
    expect(calls[0]?.inputs[1]).toEqual(noteMessage);
    expect(events.filter((e) => e.type === 'note_delivered')).toEqual([noteDelivered]);
    // Polled after the assistant message and after the tool result, then the note's id is the cursor.
    expect(served.requests.map((r) => r.url.searchParams.get('notes_after'))).toEqual(['0', '7']);
    expect(served.requests[0]?.init?.headers).toEqual({ Authorization: 'Bearer gateway-token' });
    expect(served.requests[0]?.url.href).toBe('http://host.docker.internal:8081/run?notes_after=0');
  });

  it('ends the run with exactly one finished event, after the notes', async () => {
    const { events, calls } = await run([init, assistant, success], {
      fetch: gateway(() => Response.json({ notes: [note] })).fetch,
    });

    expect(finishedEvents(events)).toHaveLength(1);
    expect(events.at(-1)?.type).toBe('finished');
    expect(calls[0]?.inputs.at(-1)).toEqual(noteMessage);
  });

  it('polls between turns only: the result ends the run without a poll, so a later note waits for the next run', async () => {
    const served = gateway();
    const { events, calls } = await run([init, assistant, success], { fetch: served.fetch });

    expect(served.requests).toHaveLength(1);
    expect(calls[0]?.inputs).toHaveLength(1);
    expect(events.filter((e) => e.type === 'note_delivered')).toEqual([]);
  });

  it('does not fail the run when a poll rejects, and says so on stderr', async () => {
    const write = vi.spyOn(process.stderr, 'write').mockImplementation(() => true);
    const { code, events } = await run([init, assistant, toolResult, success], {
      fetch: (async () => {
        throw new Error('connect ECONNREFUSED');
      }) as typeof globalThis.fetch,
    });

    expect(code).toBe(0);
    expect(events.at(-1)).toMatchObject({ type: 'finished', status: 'succeeded' });
    expect(write).toHaveBeenCalledTimes(2);
    expect(write.mock.calls[0]?.[0]).toContain('connect ECONNREFUSED');
  });

  it('does not fail the run when the gateway answers an error', async () => {
    const write = vi.spyOn(process.stderr, 'write').mockImplementation(() => true);
    const { code, events } = await run([init, assistant, success], {
      fetch: gateway(() => new Response('gateway unavailable', { status: 503 })).fetch,
    });

    expect(code).toBe(0);
    expect(events.at(-1)).toMatchObject({ type: 'finished', status: 'succeeded' });
    expect(write.mock.calls[0]?.[0]).toContain('503');
  });

  it('never polls without a gateway URL or a gateway token', async () => {
    const served = gateway(() => Response.json({ notes: [note] }));
    const noUrl = await run([init, assistant, toolResult, success], {
      spec: { ...spec, gateway_url: undefined } as TaskSpec,
      fetch: served.fetch,
    });
    const noToken = await run([init, assistant, toolResult, success], {
      env: { PATH: '/usr/bin', CLAUDE_CODE_OAUTH_TOKEN: 'oauth-token' },
      fetch: served.fetch,
    });

    expect(served.requests).toEqual([]);
    expect(noUrl.calls[0]?.inputs).toHaveLength(1);
    expect(noToken.calls[0]?.inputs).toHaveLength(1);
    expect(finishedEvents(noUrl.events)).toHaveLength(1);
    expect(finishedEvents(noToken.events)).toHaveLength(1);
  });
});

describe('runTask Ask-first tool calls', () => {
  it('stops the agent at an Ask-first call, reports it, sends no tool result and ends with finished', async () => {
    const askSpec: TaskSpec = {
      ...spec,
      connections: [
        {
          name: 'issues',
          kind: 'issue-tracker',
          title: 'Issue tracker',
          env: [],
          mcp: { url: 'https://issues.example.com/mcp' },
          tool_modes: { create_comment: 'ask' },
        },
      ],
    };
    let verdict: Awaited<ReturnType<CanUseTool>> | undefined;
    const fake = ((params: { prompt: AsyncIterable<SDKUserMessage>; options: Options }) => {
      const check = params.options.canUseTool as CanUseTool;
      return (async function* () {
        verdict = await check(
          'mcp__issues__create_comment',
          { body: 'test-input' },
          { signal: new AbortController().signal, toolUseID: 't1', requestId: 'q1' },
        );
        // The SDK ends the query after the interrupt: nothing more is yielded.
        yield* [];
      })();
    }) as unknown as typeof query;
    const gw = gateway((url) =>
      url.pathname === '/run/tool-approval' ? new Response(null, { status: 202 }) : Response.json({ notes: [] }),
    );
    const events: WorkerEvent[] = [];

    const code = await runTask(askSpec, files, {
      query: fake,
      emit: (e) => events.push(e),
      now: () => at,
      env,
      abortController: new AbortController(),
      fetch: gw.fetch,
      notePollMs: 0,
    });

    expect(verdict).toMatchObject({ behavior: 'deny', interrupt: true });
    const reports = gw.requests.filter((r) => r.url.pathname === '/run/tool-approval');
    expect(reports).toHaveLength(1);
    expect(JSON.parse(String(reports[0]?.init?.body))).toEqual({
      connection: 'issues',
      tool: 'create_comment',
      input: { body: 'test-input' },
    });
    expect(events.some((e) => e.type === 'tool_result')).toBe(false);
    expect(events.at(-1)).toMatchObject({ type: 'finished', status: 'failed' });
    expect(code).toBe(1);
  });
});

describe('runTask environment', () => {
  const withEnvironment = (setup: string[]) =>
    parseSpec(
      JSON.stringify({
        ...spec,
        environment: { name: 'default', files: [{ path: '~/.npmrc', env: 'GOAL_FILE_1' }], setup },
      }),
    );
  let home = '';

  afterEach(async () => {
    if (home) await rm(home, { recursive: true, force: true });
  });

  it('writes the files and runs setup before it queries the agent', async () => {
    home = await mkdtemp(join(tmpdir(), 'goal-run-home-'));
    const ran: string[] = [];
    const shell: Shell = async (command) => {
      ran.push(`${command}:${await readFile(join(home, '.npmrc'), 'utf8')}`);
      return { code: 0, timedOut: false };
    };

    const { code, events, calls } = await run([success], {
      spec: withEnvironment(['pnpm install']),
      env: { ...env, GOAL_FILE_1: 'registry=https://npm.example.com/' },
      environment: { home, workspace: '/workspace', marker: join(home, 'goal/environment-setup.done'), shell },
    });

    expect(code).toBe(0);
    expect(ran).toEqual(['pnpm install:registry=https://npm.example.com/']);
    expect((await stat(join(home, '.npmrc'))).mode & 0o777).toBe(0o600);
    expect(calls).toHaveLength(1);
    expect(events.at(-1)).toMatchObject({ type: 'finished', status: 'succeeded' });
  });

  it("gives a try's env to setup alone: its variables are gone before the agent starts", async () => {
    home = await mkdtemp(join(tmpdir(), 'goal-run-home-'));
    const runEnv: NodeJS.ProcessEnv = { ...env, NPM_TOKEN: 'acme-npm-token' };
    const seen: (string | undefined)[] = [];
    const shell: Shell = async () => {
      seen.push(runEnv.NPM_TOKEN);
      return { code: 0, timedOut: false };
    };
    const trySpec = parseSpec(
      JSON.stringify({
        ...spec,
        environment: { name: 'default', setup: ['pnpm install'], env: ['NPM_TOKEN'], setup_only: true },
      }),
    );

    const { code, calls } = await run([success], {
      spec: trySpec,
      env: runEnv,
      environment: { home, workspace: '/workspace', marker: join(home, 'goal/environment-setup.done'), shell },
    });

    expect(code).toBe(0);
    expect(seen).toEqual(['acme-npm-token']);
    expect(calls[0]?.options.env).not.toHaveProperty('NPM_TOKEN');
    expect(JSON.stringify(calls[0]?.options)).not.toContain('acme-npm-token');
  });

  it('fails the run with the command and its last lines when setup fails, and never queries the agent', async () => {
    home = await mkdtemp(join(tmpdir(), 'goal-run-home-'));
    const shell: Shell = async (_command, opts) => {
      opts.onOutput(`${Array.from({ length: 60 }, (_, i) => `line ${i + 1}`).join('\n')}\n`);
      return { code: 2, timedOut: false };
    };

    const { code, events, calls } = await run([success], {
      spec: withEnvironment(['make deps']),
      env: { ...env, GOAL_FILE_1: 'x' },
      environment: { home, workspace: '/workspace', marker: join(home, 'goal/environment-setup.done'), shell },
    });

    expect(code).toBe(1);
    expect(calls).toEqual([]);
    expect(events).toHaveLength(1);
    const finished = events[0];
    expect(finished).toMatchObject({ type: 'finished', status: 'failed' });
    const error = finished?.type === 'finished' ? (finished.error ?? '') : '';
    expect(error).toMatch(/^environment setup: `make deps` exited 2; its last 50 lines:\nline 11\n/);
    expect(error).toMatch(/line 60$/);
    expect(error).not.toContain('line 10\n');
  });

  it('writes the files again but runs no setup on a second run in the same box', async () => {
    home = await mkdtemp(join(tmpdir(), 'goal-run-home-'));
    const ran: string[] = [];
    const shell: Shell = async (command) => {
      ran.push(command);
      return { code: 0, timedOut: false };
    };
    const marker = join(home, 'goal/environment-setup.done');
    const box = (value: string) => ({
      spec: withEnvironment(['pnpm install', 'make tools']),
      env: { ...env, GOAL_FILE_1: value },
      environment: { home, workspace: '/workspace', marker, shell },
    });

    const first = await run([success], box('registry=https://npm.example.com/'));
    expect(first.code).toBe(0);
    expect(ran).toEqual(['pnpm install', 'make tools']);
    expect(await readFile(marker, 'utf8')).not.toContain('npm.example.com');

    const second = await run([success], box('registry=https://npm2.example.com/'));
    expect(second.code).toBe(0);
    expect(ran).toEqual(['pnpm install', 'make tools']);
    expect(await readFile(join(home, '.npmrc'), 'utf8')).toBe('registry=https://npm2.example.com/');
    expect(second.calls).toHaveLength(1);
    expect(second.events.at(-1)).toMatchObject({ type: 'finished', status: 'succeeded' });
  });

  it('runs setup again on the next run in the box after a failed setup', async () => {
    home = await mkdtemp(join(tmpdir(), 'goal-run-home-'));
    let code = 2;
    const ran: string[] = [];
    const shell: Shell = async (command) => {
      ran.push(command);
      return { code, timedOut: false };
    };
    const box = {
      spec: withEnvironment(['make deps']),
      env: { ...env, GOAL_FILE_1: 'x' },
      environment: { home, workspace: '/workspace', marker: join(home, 'goal/environment-setup.done'), shell },
    };

    const first = await run([success], box);
    expect(first.code).toBe(1);
    expect(first.calls).toEqual([]);
    const finished = first.events.at(-1);
    expect(finished?.type === 'finished' ? finished.error : '').toBe('environment setup: `make deps` exited 2');

    code = 0;
    const second = await run([success], box);
    expect(second.code).toBe(0);
    expect(ran).toEqual(['make deps', 'make deps']);
    expect(second.calls).toHaveLength(1);
  });

  it('runs a spec without an environment as before, with no shell', async () => {
    const { code, calls } = await run([success]);
    expect(code).toBe(0);
    expect(calls).toHaveLength(1);
  });
});
