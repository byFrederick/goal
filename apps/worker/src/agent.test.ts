import { mkdir, mkdtemp, rm, symlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { CanUseTool, Options, PermissionResult } from '@anthropic-ai/claude-agent-sdk';
import { afterEach, beforeEach, describe, expect, it, type MockInstance, vi } from 'vitest';
import { agentQuery, connectionPermission, taskPrompt } from './agent.js';
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

/** A fetch no test expects: a request that reaches it is one the worker should not have made. */
const noFetch = (async () => {
  throw new Error('no request expected');
}) as typeof globalThis.fetch;

/** A fetch that answers every request with status and records each one it was asked for. */
function gatewayAnswering(status: number) {
  const requests: { url: string; init: RequestInit }[] = [];
  const fetch = (async (input: string | URL, init?: RequestInit) => {
    requests.push({ url: String(input), init: init ?? {} });
    return new Response(null, { status });
  }) as typeof globalThis.fetch;
  return { fetch, requests };
}

function optionsOf(spec: Partial<TaskSpec>): Options {
  return agentQuery(
    parseSpec(JSON.stringify({ ...base, ...spec })),
    { prompt: 'You are the engineer.' },
    env,
    new AbortController(),
    noFetch,
  ).options;
}

function systemPromptOf(spec: Partial<TaskSpec>): string {
  const system = optionsOf(spec).systemPrompt;
  if (typeof system !== 'object' || system === null || !('append' in system)) throw new Error('no appended prompt');
  return system.append ?? '';
}

/** Asks options' permission check about one call, as Claude Code does for a call nothing pre-approved. */
async function verdictOf(
  options: Pick<Options, 'canUseTool'>,
  tool: string,
  input: Record<string, unknown> = { q: 'x' },
): Promise<PermissionResult> {
  const check = options.canUseTool as CanUseTool;
  const result = await check(tool, input, { signal: new AbortController().signal, toolUseID: 't1', requestId: 'q1' });
  if (!result) throw new Error(`no decision on ${tool}`);
  return result;
}

async function decide(
  options: Pick<Options, 'canUseTool'>,
  tool: string,
  input: Record<string, unknown> = { q: 'x' },
): Promise<'allow' | 'deny'> {
  return (await verdictOf(options, tool, input)).behavior;
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
    const { prompt } = agentQuery(spec, { prompt: 'p' }, { GOAL_FEEDBACK: feedback }, new AbortController(), noFetch);
    expect(prompt.startsWith(`Add a health endpoint\n\n${feedback}\n\n## Finishing`)).toBe(true);
    expect(agentQuery(spec, { prompt: 'p' }, {}, new AbortController(), noFetch).prompt).not.toContain('rejected');
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

  it('tells the agent its factory rules and that they win on a conflict, when the run has rules', () => {
    const prompt = systemPromptOf({
      rules: [
        { name: 'go-errors', sha256: 'a'.repeat(64), paths: ['**/*.go'], content: 'Wrap with %w.\n' },
        { name: 'tone', sha256: 'b'.repeat(64), content: 'Say what you did.\n' },
      ],
    });

    expect(prompt).toContain('## Factory rules');
    expect(prompt).toContain('`go-errors`, `tone`');
    expect(prompt).toContain('$CLAUDE_CONFIG_DIR/rules');
    expect(prompt).toContain('the factory rule wins inside this factory');
    expect(prompt).toContain('say so in your output');
  });

  it('has no factory rules section without rules', () => {
    expect(systemPromptOf({})).not.toContain('Factory rules');
    expect(systemPromptOf({ rules: [] })).not.toContain('Factory rules');
  });

  it('names the knowledge tools and their sources after the prompt when the gateway serves them', () => {
    const append = systemPromptOf({
      gateway_url: 'http://gateway.internal/mcp',
      knowledge_sources: ['handbook', 'runbooks'],
    });
    expect(append).toBe(
      "You are the engineer.\n\n## Knowledge\nThe organization's knowledge sources handbook, runbooks are searchable with " +
        'mcp__goal__knowledge__search; mcp__goal__knowledge__read returns a whole document by the id a search gave.',
    );
    expect(systemPromptOf({ knowledge_sources: ['handbook'] })).not.toContain('Knowledge');
  });
});

describe('the permission check of off and Ask-first connection tools', () => {
  const gateway = 'http://gateway.internal/mcp';
  const reportUrl = 'http://gateway.internal/run/tool-approval';
  const create = 'mcp__issues__create_comment';
  /** The issues connection with these tool modes, and the run's gateway. */
  const permissionWith = (
    modes: Record<string, 'ask' | 'off'>,
    reach: { fetch: typeof globalThis.fetch; token?: string | undefined },
    extra: Partial<TaskSpec> = {},
  ): CanUseTool =>
    connectionPermission(
      {
        tools_allow: [],
        tools_deny: [],
        gateway_url: gateway,
        ...extra,
        connections: [{ ...issues, tool_modes: modes }],
      },
      { token: 'gateway-token', ...reach },
    );
  const body = { body: 'hello' };
  let stderr: MockInstance<typeof process.stderr.write>;

  beforeEach(() => {
    stderr = vi.spyOn(process.stderr, 'write').mockImplementation(() => true);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('denies an off tool with no request, and leaves the other tools of the connection to their rules', async () => {
    const gw = gatewayAnswering(202);
    const check = permissionWith({ create_comment: 'off' }, gw);

    expect(await decide({ canUseTool: check }, create, body)).toBe('deny');
    expect(await decide({ canUseTool: check }, 'mcp__issues__list_issues', body)).toBe('allow');
    expect(gw.requests).toEqual([]);
  });

  it('allows a tool with no mode, or a mode for another tool', async () => {
    const gw = gatewayAnswering(202);

    expect(await decide({ canUseTool: permissionWith({}, gw) }, create, body)).toBe('allow');
    expect(await decide({ canUseTool: permissionWith({ close_issue: 'ask' }, gw) }, create, body)).toBe('allow');
    expect(gw.requests).toEqual([]);
  });

  it('denies an Ask-first tool that tools_deny matches before it reads the mode or sends a request', async () => {
    const gw = gatewayAnswering(202);
    const check = permissionWith({ create_comment: 'ask' }, gw, { tools_deny: ['issues__create_*'] });

    expect(await verdictOf({ canUseTool: check }, create, body)).toEqual({
      behavior: 'deny',
      message: "The agent's tool rules do not let it call issues__create_comment.",
    });
    expect(gw.requests).toEqual([]);
  });

  it('reports an Ask-first call to the gateway with the task token and interrupts the agent on 202', async () => {
    const gw = gatewayAnswering(202);
    const verdict = await verdictOf({ canUseTool: permissionWith({ create_comment: 'ask' }, gw) }, create, body);

    expect(verdict).toMatchObject({ behavior: 'deny', interrupt: true });
    expect(verdict).toHaveProperty('message', 'The call issues__create_comment waits for a person to approve it.');
    expect(gw.requests).toHaveLength(1);
    const [request] = gw.requests;
    expect(request?.url).toBe(reportUrl);
    expect(request?.init.method).toBe('POST');
    expect(request?.init.headers).toEqual({
      Authorization: 'Bearer gateway-token',
      'Content-Type': 'application/json',
    });
    expect(JSON.parse(String(request?.init.body))).toEqual({
      connection: 'issues',
      tool: 'create_comment',
      input: body,
    });
  });

  it.each([409, 413, 500])('denies without interrupt and sends on when the gateway answers %i', async (status) => {
    const gw = gatewayAnswering(status);
    const verdict = await verdictOf({ canUseTool: permissionWith({ create_comment: 'ask' }, gw) }, create, body);

    expect(verdict).toEqual({
      behavior: 'deny',
      message: 'The call issues__create_comment could not be sent for approval, so it did not run.',
    });
    expect(gw.requests).toHaveLength(1);
    expect(stderr).toHaveBeenCalledWith(
      `goal-agent: tool approval for issues__create_comment not requested: the gateway answered ${status}\n`,
    );
  });

  it('denies without interrupt when the report is rejected', async () => {
    const offline = (async () => {
      throw new Error('fetch failed');
    }) as typeof globalThis.fetch;
    const verdict = await verdictOf(
      { canUseTool: permissionWith({ create_comment: 'ask' }, { fetch: offline }) },
      create,
      body,
    );

    expect(verdict).toMatchObject({ behavior: 'deny' });
    expect(verdict).not.toHaveProperty('interrupt');
    expect(stderr).toHaveBeenCalledWith(
      'goal-agent: tool approval for issues__create_comment not requested: fetch failed\n',
    );
  });

  it('denies without any request when the run has no gateway or no task token', async () => {
    const gw = gatewayAnswering(202);
    const noGateway = permissionWith({ create_comment: 'ask' }, gw, { gateway_url: undefined });
    const noToken = permissionWith({ create_comment: 'ask' }, { ...gw, token: undefined });

    for (const check of [noGateway, noToken]) {
      const verdict = await verdictOf({ canUseTool: check }, create, body);
      expect(verdict).toMatchObject({ behavior: 'deny' });
      expect(verdict).not.toHaveProperty('interrupt');
    }
    expect(gw.requests).toEqual([]);
  });

  it('runs an Ask-first call a grant matches, whatever the order of its keys, once', async () => {
    const gw = gatewayAnswering(202);
    const granted = [{ connection: 'issues', tool: 'create_comment', input: { a: 1, b: { d: [1, 'x'], c: true } } }];
    const check = permissionWith({ create_comment: 'ask' }, gw, { granted_calls: granted });

    expect(await decide({ canUseTool: check }, create, { b: { c: true, d: [1, 'x'] }, a: 1 })).toBe('allow');
    expect(gw.requests).toEqual([]);
  });

  it('asks again on a second use of a grant, and on the same call with another input', async () => {
    const gw = gatewayAnswering(202);
    const input = { body: 'hello' };
    const granted = [{ connection: 'issues', tool: 'create_comment', input }];
    const check = permissionWith({ create_comment: 'ask' }, gw, { granted_calls: granted });

    expect(await decide({ canUseTool: check }, create, input)).toBe('allow');
    expect(await verdictOf({ canUseTool: check }, create, input)).toMatchObject({ behavior: 'deny', interrupt: true });
    expect(await verdictOf({ canUseTool: check }, create, { body: 'bye' })).toMatchObject({ interrupt: true });
    expect(gw.requests).toHaveLength(2);
  });

  it('never writes a tool input to stderr', async () => {
    const gw = gatewayAnswering(409);
    const secret = { body: 'test-secret' };

    await verdictOf({ canUseTool: permissionWith({ create_comment: 'ask' }, gw) }, create, secret);

    const written = stderr.mock.calls.map((call: unknown[]) => String(call[0])).join('');
    expect(written).toContain('issues__create_comment');
    expect(written).not.toContain('test-secret');
  });
});

describe('the permission check of escalated built-in writes', () => {
  const write = (file_path: string) => ({ file_path, content: '# Rules' });
  const bash = (command: string) => ({ command });
  let workspace: string;
  const at = (rel: string) => join(workspace, rel);
  const checkOf = (spec: Partial<TaskSpec>, dir = workspace) => ({
    canUseTool: connectionPermission(
      { tools_allow: [], tools_deny: [], connections, ...spec },
      { fetch: noFetch, token: undefined },
      dir,
    ),
  });

  beforeEach(async () => {
    workspace = await mkdtemp(join(tmpdir(), 'goal-workspace-'));
  });
  afterEach(async () => {
    await rm(workspace, { recursive: true, force: true });
  });

  it('allows a granted Write, Edit or MultiEdit to the guidance files of the workspace', async () => {
    const guidance = [
      at('.claude/rules/x.md'),
      at('.claude/skills/s/SKILL.md'),
      at('.claude/agents/a.md'),
      at('.claude/commands/c.md'),
      at('CLAUDE.md'),
      at('AGENTS.md'),
      '.claude/rules/y.md', // relative to the workspace
    ];
    for (const tool of ['Write', 'Edit', 'MultiEdit']) {
      const check = checkOf({ tools_allow: ['Read', tool] });
      for (const path of guidance) {
        expect([tool, path, await decide(check, tool, write(path))]).toEqual([tool, path, 'allow']);
      }
    }
    const scoped = checkOf({ tools_allow: ['Write(/.claude/**)'] });
    expect(await decide(scoped, 'Write', write(at('.claude/rules/x.md')))).toBe('allow');
  });

  it('allows a Write of a new file in a new directory under .claude/rules', async () => {
    const check = checkOf({ tools_allow: ['Write'] });
    expect(await decide(check, 'Write', write(at('.claude/rules/new/dir/x.md')))).toBe('allow');
  });

  it('decides a workspace that does not exist yet without failing', async () => {
    const missing = join(workspace, 'missing');
    const check = checkOf({ tools_allow: ['Write'] }, missing);
    expect(await decide(check, 'Write', write(join(missing, '.claude/rules/x.md')))).toBe('allow');
    expect(await decide(check, 'Write', write(join(missing, '.claude/settings.json')))).toBe('deny');
  });

  it('denies a write the tool rules do not grant', async () => {
    const path = write(at('.claude/rules/x.md'));
    expect(await decide(checkOf({ tools_allow: ['Read', 'Edit'] }), 'Write', path)).toBe('deny');
    expect(await decide(checkOf({ tools_allow: ['Write(/docs/**)'] }), 'Write', path)).toBe('deny');
    expect(await decide(checkOf({ tools_allow: ['Bash(git *)'] }), 'Bash', bash('cat > .claude/rules/x.md'))).toBe(
      'deny',
    );
  });

  it('denies settings, hooks, .git, escapes and anything outside the workspace', async () => {
    const check = checkOf({ tools_allow: ['Write', 'Edit', 'MultiEdit'] });
    const denied = [
      at('.claude/settings.json'),
      at('.claude/settings.local.json'),
      at('.git/config'),
      `${workspace}/.claude/rules/../settings.json`,
      at('.claude/hooks/x.sh'),
      at('.claude/rules'),
      at('src/main.ts'),
      '/etc/x',
      '/home/node/.claude/rules/x.md',
      '../outside/CLAUDE.md',
    ];
    for (const path of denied) {
      for (const tool of ['Write', 'Edit', 'MultiEdit']) {
        expect([tool, path, await decide(check, tool, write(path))]).toEqual([tool, path, 'deny']);
      }
    }
    expect(await decide(check, 'Write', { content: 'x' })).toBe('deny'); // no file_path
    expect(await decide(check, 'NotebookEdit', write(at('.claude/rules/x.md')))).toBe('deny');
  });

  it('decides Bash by what the command mentions: a guidance path and nothing protected', async () => {
    const check = checkOf({ tools_allow: ['Bash'] });
    const calls: [string, 'allow' | 'deny'][] = [
      ['mkdir -p .claude/rules && cat > .claude/rules/go.md', 'allow'],
      [`cp /tmp/skill.md ${at('.claude/skills/s/SKILL.md')}`, 'allow'],
      ["printf '# Rules' >> AGENTS.md", 'allow'],
      ['echo {} > .claude/settings.json', 'deny'],
      ['cp x .claude/settings.local.json && echo CLAUDE.md', 'deny'],
      ['cp x .git/hooks/pre-push', 'deny'],
      ['cp .claude/rules/x.md .claude/hooks/x.sh', 'deny'],
      ['cp x ~/.claude/rules/x.md', 'deny'],
      ['cp x $HOME/.claude/rules/x.md', 'deny'],
      ['cp x .claude/rules/../../escape.md', 'deny'],
      ['rm -rf src', 'deny'], // no guidance path
    ];
    for (const [command, want] of calls) {
      expect([command, await decide(check, 'Bash', bash(command))]).toEqual([command, want]);
    }
    expect(await decide(check, 'Bash', {})).toBe('deny');
  });

  it('lets tools_deny win, in Claude Code rule syntax', async () => {
    const rules = at('.claude/rules/x.md');
    expect(await decide(checkOf({ tools_allow: ['Write'], tools_deny: ['Write'] }), 'Write', write(rules))).toBe(
      'deny',
    );
    expect(
      await decide(
        checkOf({ tools_allow: ['Write'], tools_deny: ['Write(/.claude/rules/**)'] }),
        'Write',
        write(rules),
      ),
    ).toBe('deny');
    const bashDenied = checkOf({ tools_allow: ['Bash'], tools_deny: ['Bash(cat*)'] });
    expect(await decide(bashDenied, 'Bash', bash('cat > .claude/rules/go.md'))).toBe('deny');
    expect(await decide(bashDenied, 'Bash', bash('tee .claude/rules/go.md'))).toBe('allow');
  });

  it('denies a write whose guidance path is a symlink out of it', async () => {
    await mkdir(at('.git/hooks'), { recursive: true });
    await mkdir(at('.claude/skills'), { recursive: true });
    await symlink('../.git', at('.claude/rules'));
    const check = checkOf({ tools_allow: ['Write'] });

    expect(await decide(check, 'Write', write(at('.claude/rules/hooks/pre-push')))).toBe('deny');
    expect(await decide(check, 'Write', write(at('.claude/skills/s/SKILL.md')))).toBe('allow');
  });
});

describe('taskPrompt', () => {
  it('puts the conflicts with the default branch before the brief and feedback, and nothing without them', () => {
    const spec = parseSpec(JSON.stringify({ ...base, base_conflicts: ['web/src/app.tsx'] }));
    const prompt = taskPrompt(spec, 'Rejected: the tests fail.');

    expect(prompt).toMatch(/^## First: conflicts with the default branch\n\n[^\n]*rebase or merge the default branch/);
    expect(prompt).toContain('- web/src/app.tsx\n\nAdd a health endpoint\n\nRejected: the tests fail.');
    expect(taskPrompt(parseSpec(JSON.stringify({ ...base, base_conflicts: [] })))).toMatch(/^Add a health endpoint/);
  });

  it('names the base branch in the conflicts heading when one is set', () => {
    const spec = parseSpec(
      JSON.stringify({ ...base, base_conflicts: ['go.mod'], base_branch: 'goal/assembly-330-integration' }),
    );
    const prompt = taskPrompt(spec);

    expect(prompt).toMatch(/^## First: conflicts with goal\/assembly-330-integration\n\n/);
    expect(prompt).toContain('(origin/goal/assembly-330-integration)');
    expect(prompt).toContain('rebase or merge origin/goal/assembly-330-integration');
    expect(prompt).not.toContain('default branch');
    expect(prompt).not.toContain('origin/HEAD');
    expect(prompt).toContain('- go.mod\n\nAdd a health endpoint');
  });
});
