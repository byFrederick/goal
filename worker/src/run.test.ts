import type { Options, query, SDKMessage } from '@anthropic-ai/claude-agent-sdk';
import { describe, expect, it } from 'vitest';
import type { WorkerEvent } from './events.js';
import { runTask } from './run.js';
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
  ANTHROPIC_API_KEY: 'sk-ant-must-not-pass',
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

/** A query that plays scripted messages and records the options it was given. */
function scripted(messages: unknown[], opts: { throws?: Error } = {}) {
  const calls: { prompt: string; options: Options }[] = [];
  const fake = ((params: { prompt: string; options: Options }) => {
    calls.push(params);
    return (async function* () {
      for (const m of messages) yield m as SDKMessage;
      if (opts.throws) throw opts.throws;
    })();
  }) as unknown as typeof query;
  return { fake, calls };
}

async function run(messages: unknown[], opts: { throws?: Error; spec?: TaskSpec; files?: AgentFiles } = {}) {
  const { fake, calls } = scripted(messages, opts);
  const events: WorkerEvent[] = [];
  const code = await runTask(opts.spec ?? spec, opts.files ?? files, {
    query: fake,
    emit: (e) => events.push(e),
    now: () => at,
    env,
    abortController: new AbortController(),
  });
  return { code, events, calls };
}

describe('runTask', () => {
  it('runs the agent with the task policy, one gateway and no API key', async () => {
    const { calls } = await run([success]);
    const options = calls[0]?.options;

    expect(options).toMatchObject({
      cwd: '/workspace',
      model: 'claude-sonnet-5-5',
      systemPrompt: { type: 'preset', preset: 'claude_code', append: 'You are the engineer.' },
      tools: ['Read', 'Edit', 'Bash', 'Skill'],
      allowedTools: ['Read', 'Edit', 'Bash(npm test*)', 'mcp__goal'],
      disallowedTools: ['Bash(git push*)'],
      permissionMode: 'dontAsk',
      skills: ['acme-conventions'],
      mcpServers: {
        goal: {
          type: 'http',
          url: 'http://host.docker.internal:8081/mcp',
          headers: { Authorization: 'Bearer gateway-token' },
        },
      },
      strictMcpConfig: true,
      maxTurns: 40,
      maxBudgetUsd: 6,
      outputFormat: { type: 'json_schema', schema: { type: 'object' } },
    });
    expect(Object.keys(options?.mcpServers ?? {})).toEqual(['goal']);
    expect(options?.env).toMatchObject({ CLAUDE_CODE_OAUTH_TOKEN: 'oauth-token', PATH: '/usr/bin' });
    expect(options?.env).not.toHaveProperty('ANTHROPIC_API_KEY');
    expect(options?.env).not.toHaveProperty('GOAL_GATEWAY_TOKEN');
    expect(calls[0]?.prompt).toContain('Add a health endpoint');
    expect(calls[0]?.prompt).toContain('"lane": "backend"');
    expect(calls[0]?.prompt).toContain('output contract "change"');
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

  it('streams progress events and ends with the result', async () => {
    const assistant = (content: unknown[]) => ({
      type: 'assistant',
      message: { id: 'msg_1', content, usage },
      parent_tool_use_id: null,
      session_id: 's1',
    });
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
      assistant([{ type: 'text', text: 'Looking at the router.' }]),
      assistant([{ type: 'tool_use', id: 'tu_1', name: 'Bash', input: { command: 'npm test' } }]),
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
    expect(events.map((e) => e.type)).toEqual(['started', 'message', 'usage', 'tool_use', 'tool_result', 'finished']);
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

describe('parseSpec', () => {
  it('rejects a spec without its run id', () => {
    expect(() => parseSpec(JSON.stringify({ ...spec, run_id: undefined }))).toThrow();
  });
});
