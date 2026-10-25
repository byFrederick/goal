import type { CanUseTool, Options } from '@anthropic-ai/claude-agent-sdk';
import { describe, expect, it } from 'vitest';
import { agentQuery } from './agent.js';
import { type Connection, parseSpec, type TaskSpec } from './spec.js';

const base = {
  factory_id: 'f1',
  item_id: 'i1',
  run_id: 'r1',
  station: 'build',
  agent: 'engineer',
  skills: [],
  tools_allow: [],
  tools_deny: [],
  brief: 'Add a health endpoint',
};

/** A remote server's header as the bundle carries it: the variable's name, for Claude Code to expand. */
// biome-ignore lint/suspicious/noTemplateCurlyInString: Claude Code's ${NAME} reference, not a template
const issuesAuth = 'Bearer ${ISSUES_TOKEN}';

/** The agent's connections as the run bundle names them: a remote MCP server, a stdio one and an API. */
const issues: Connection = {
  name: 'issues',
  kind: 'issue-tracker',
  title: 'Issue tracker',
  env: ['ISSUES_SITE', 'ISSUES_TOKEN'],
  mcp: { url: 'https://issues.example.com/mcp', headers: { Authorization: issuesAuth } },
  docs: 'File and read issues with the issues CLI or its MCP tools.',
};
const connections: Connection[] = [
  issues,
  {
    name: 'docs',
    kind: 'mcp-server',
    title: 'MCP server',
    env: [],
    mcp: { command: ['sh', '-c', 'npx -y @acme/docs-mcp@1.0.0'] },
  },
  { name: 'metrics', kind: 'http-api-key', title: 'HTTP API key', env: ['METRICS_KEY', 'METRICS_URL'] },
];

/** The run's env: the connections' values, which nothing the worker configures may carry. */
const env = { ISSUES_TOKEN: 'issues-s3cr3t', ISSUES_SITE: 'issues.example.com', GOAL_GATEWAY_TOKEN: 'gateway-token' };

function optionsOf(spec: Partial<TaskSpec>): Options {
  return agentQuery(
    parseSpec(JSON.stringify({ ...base, ...spec })),
    { prompt: 'You are the engineer.' },
    env,
    new AbortController(),
  ).options;
}

function systemPromptOf(spec: Partial<TaskSpec>): string {
  const system = optionsOf(spec).systemPrompt;
  if (typeof system !== 'object' || system === null || !('append' in system)) throw new Error('no appended prompt');
  return system.append ?? '';
}

/** Asks options' permission check about one call, as Claude Code does for a call nothing pre-approved. */
async function decide(options: Options, tool: string): Promise<'allow' | 'deny'> {
  const check = options.canUseTool as CanUseTool;
  const result = await check(
    tool,
    { q: 'x' },
    { signal: new AbortController().signal, toolUseID: 't1', requestId: 'q1' },
  );
  if (!result) throw new Error(`no decision on ${tool}`);
  return result.behavior;
}

describe('agentQuery', () => {
  it("puts the factory's purpose before the agent's prompt", () => {
    expect(systemPromptOf({ purpose: 'Builds and reviews changes to the acme shop.' })).toBe(
      '## This factory\nBuilds and reviews changes to the acme shop.\n\nYou are the engineer.',
    );
  });

  it("puts goald's feedback for this attempt (GOAL_FEEDBACK) right after the brief", () => {
    const spec = parseSpec(JSON.stringify(base));
    const feedback = 'Your previous attempt was rejected: `task check` exited 1.';
    const { prompt } = agentQuery(spec, { prompt: 'p' }, { GOAL_FEEDBACK: feedback }, new AbortController());
    expect(prompt.startsWith(`Add a health endpoint\n\n${feedback}\n\n## Finishing`)).toBe(true);
    expect(agentQuery(spec, { prompt: 'p' }, {}, new AbortController()).prompt).not.toContain('rejected');
  });

  it('appends only the prompt when the factory has no purpose', () => {
    expect(systemPromptOf({})).toBe('You are the engineer.');
    expect(systemPromptOf({ purpose: '' })).toBe('You are the engineer.');
  });

  it("starts each connection's MCP server from the box: remote with headers naming env variables, stdio by its command", () => {
    const options = optionsOf({ connections, gateway_url: 'http://goal-egress:3128/mcp' });

    expect(options.mcpServers).toEqual({
      goal: {
        type: 'http',
        url: 'http://goal-egress:3128/mcp',
        headers: { Authorization: 'Bearer gateway-token' },
        alwaysLoad: true,
      },
      issues: {
        type: 'http',
        url: 'https://issues.example.com/mcp',
        headers: { Authorization: issuesAuth },
        alwaysLoad: true,
      },
      docs: { type: 'stdio', command: 'sh', args: ['-c', 'npx -y @acme/docs-mcp@1.0.0'], alwaysLoad: true },
    });
    // Claude Code expands ${ISSUES_TOKEN} from the env it runs with; the config never holds the value.
    expect(JSON.stringify(options.mcpServers)).not.toContain('issues-s3cr3t');
    expect(options.env).toMatchObject({ ISSUES_TOKEN: 'issues-s3cr3t' });
  });

  it("applies the agent's tool rules to its connections' tools in the permission check, deny winning", async () => {
    const options = optionsOf({
      connections,
      tools_allow: ['Read', 'issues__get_*', 'issues__list_issues'],
      tools_deny: ['Bash(git push*)', 'issues__get_secret', 'docs__drop_*'],
    });

    expect(options.permissionMode).toBe('default');
    expect(options.allowedTools).toEqual(['Read']);
    expect(options.disallowedTools).toEqual(['Bash(git push*)']);

    const calls: [string, 'allow' | 'deny'][] = [
      ['mcp__issues__get_issue', 'allow'], // allowed
      ['mcp__issues__list_issues', 'allow'], // allowed
      ['mcp__issues__get_secret', 'deny'], // denied, though an allow rule matches
      ['mcp__issues__create_issue', 'deny'], // the connection's allow rules leave it out
      ['mcp__docs__search', 'allow'], // no allow rule for docs: all its tools but the denied
      ['mcp__docs__drop_index', 'deny'], // denied
      ['mcp__elsewhere__get_issue', 'deny'], // no such connection
      ['Bash', 'deny'], // not pre-approved: nobody is there to ask
      ['Write', 'deny'],
    ];
    for (const [tool, want] of calls) {
      expect([tool, await decide(options, tool)]).toEqual([tool, want]);
    }
  });

  it('treats a malformed deny pattern as denying every tool of its connection', async () => {
    const options = optionsOf({ connections, tools_deny: ['issues__[get'] });

    expect(await decide(options, 'mcp__issues__get_issue')).toBe('deny');
    expect(await decide(options, 'mcp__docs__search')).toBe('allow');
  });

  it('tells the agent its connections: names, kinds, env names, MCP servers and docs, never a value', () => {
    const prompt = systemPromptOf({ connections });

    expect(prompt).toBe(
      [
        'You are the engineer.',
        '## Connections',
        'Your environment holds these connections. Their variables carry credentials: use them, never print or share their values.',
        '### issues (Issue tracker)\n\nEnvironment: `ISSUES_SITE`, `ISSUES_TOKEN`. MCP server `issues`: its tools are `mcp__issues__*`.\n\nFile and read issues with the issues CLI or its MCP tools.',
        '### docs (MCP server)\n\nMCP server `docs`: its tools are `mcp__docs__*`.',
        '### metrics (HTTP API key)\n\nEnvironment: `METRICS_KEY`, `METRICS_URL`.',
      ].join('\n\n'),
    );
    expect(prompt).not.toContain('issues-s3cr3t');
  });

  it("refuses a connection named like goald's gateway", () => {
    expect(() => optionsOf({ connections: [{ ...issues, name: 'goal' }] })).toThrow(/goal/);
  });
});
