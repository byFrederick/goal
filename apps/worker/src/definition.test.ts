import { createHash } from 'node:crypto';
import { mkdir, mkdtemp, rm, symlink, unlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import type { CanUseTool, Options, query, SDKMessage, SDKUserMessage } from '@anthropic-ai/claude-agent-sdk';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  definitionChanges,
  definitionSystemPrompt,
  definitionTools,
  isDefinitionPath,
  runDefinition,
} from './definition.js';
import type { WorkerEvent } from './events.js';
import { parseSpec } from './spec.js';

const sha = (s: string) => createHash('sha256').update(s).digest('hex');
const b64 = (s: string) => Buffer.from(s).toString('base64');

let root: string;
let workspace: string;
let goal: string;

beforeEach(async () => {
  root = await mkdtemp(join(tmpdir(), 'goal-definition-'));
  workspace = join(root, 'workspace');
  goal = join(root, 'goal');
  await mkdir(workspace);
  await mkdir(goal);
});

afterEach(async () => {
  await rm(root, { recursive: true, force: true });
});

/** Writes the draft's starting files into the workspace and returns their baseline. */
async function draft(files: Record<string, string>): Promise<Record<string, string>> {
  const baseline: Record<string, string> = {};
  for (const [path, content] of Object.entries(files)) {
    await put(path, content);
    baseline[path] = sha(content);
  }
  await writeFile(join(goal, 'definition.json'), JSON.stringify(baseline));
  return baseline;
}

async function put(path: string, content: string): Promise<void> {
  await mkdir(dirname(join(workspace, path)), { recursive: true });
  await writeFile(join(workspace, path), content);
}

const start = {
  'factory.yaml': 'kind: Factory\n',
  'agents/reviewer.md': '# Reviewer\n',
  'rules/secrets.md': '# Secrets\n',
  'skills/acme/SKILL.md': '# acme\n',
  'evals/review.yaml': 'cases: []\n',
  'contracts/change.schema.json': '{}\n',
};

describe('definition allowlist', () => {
  it('accepts factory.yaml and files under agents/, rules/ and skills/', () => {
    for (const p of [
      'factory.yaml',
      'agents/reviewer.md',
      'rules/secrets.md',
      'skills/acme/SKILL.md',
      'skills/acme/a/b.txt',
    ]) {
      expect(isDefinitionPath(p), p).toBe(true);
    }
  });

  it('refuses evals/, contracts/, other top-level files and unsafe paths', () => {
    for (const p of [
      'evals/review.yaml',
      'contracts/change.schema.json',
      'README.md',
      'factory.yml',
      'agents',
      'agents/',
      'agentsx/a.md',
      './factory.yaml',
      'agents/./a.md',
      'agents/../evals/x.yaml',
      '../factory.yaml',
      '/factory.yaml',
      '/workspace/agents/a.md',
      'agents//a.md',
      '',
    ]) {
      expect(isDefinitionPath(p), p).toBe(false);
    }
  });
});

describe('definition changes', () => {
  it('reports modified and new files as changed and missing ones as deleted; unchanged files appear nowhere', async () => {
    const baseline = await draft(start);
    await put('factory.yaml', 'kind: Factory\nreviewers: 4\n');
    await put('agents/writer.md', '# Writer\n');
    await unlink(join(workspace, 'rules/secrets.md'));
    await mkdir(join(workspace, 'skills/empty'), { recursive: true });

    const got = await definitionChanges(workspace, baseline);

    expect(got).toEqual({
      changed: [
        { path: 'agents/writer.md', sha256: sha('# Writer\n'), content: b64('# Writer\n') },
        {
          path: 'factory.yaml',
          sha256: sha('kind: Factory\nreviewers: 4\n'),
          content: b64('kind: Factory\nreviewers: 4\n'),
        },
      ],
      deleted: ['rules/secrets.md'],
      rejected: [],
    });
  });

  it('rejects a change outside the allowlist instead of reporting it changed or deleted', async () => {
    const baseline = await draft(start);
    await put('evals/review.yaml', 'cases: [1]\n');
    await put('evals/new.yaml', 'cases: []\n');
    await unlink(join(workspace, 'contracts/change.schema.json'));
    await put('notes.txt', 'scratch\n');

    expect(await definitionChanges(workspace, baseline)).toEqual({
      changed: [],
      deleted: [],
      rejected: ['contracts/change.schema.json', 'evals/new.yaml', 'evals/review.yaml', 'notes.txt'],
    });
  });

  it('rejects symlinks, and never follows one', async () => {
    const baseline = await draft(start);
    await writeFile(join(root, 'outside.md'), 'secret\n');
    await symlink(join(root, 'outside.md'), join(workspace, 'agents/linked.md'));
    await symlink(root, join(workspace, 'skills/out'));
    await unlink(join(workspace, 'agents/reviewer.md'));
    await symlink(join(root, 'outside.md'), join(workspace, 'agents/reviewer.md'));

    expect(await definitionChanges(workspace, baseline)).toEqual({
      changed: [],
      deleted: [],
      rejected: ['agents/linked.md', 'agents/reviewer.md', 'skills/out'],
    });
  });
});

/** A query that plays scripted messages after edit ran (the agent's work), and records its options and prompt. */
function scripted(messages: unknown[], edit: () => Promise<void> = async () => {}) {
  const calls: { options: Options; inputs: SDKUserMessage[] }[] = [];
  const fake = ((params: { prompt: AsyncIterable<SDKUserMessage>; options: Options }) => {
    const call = { options: params.options, inputs: [] as SDKUserMessage[] };
    calls.push(call);
    return (async function* () {
      for await (const m of params.prompt) {
        call.inputs.push(m);
        break;
      }
      await edit();
      for (const m of messages) yield m as SDKMessage;
    })();
  }) as unknown as typeof query;
  return { fake, calls };
}

const success = {
  type: 'result',
  subtype: 'success',
  is_error: false,
  num_turns: 4,
  total_cost_usd: 0.12,
  usage: { input_tokens: 10, output_tokens: 5, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 },
  result: '',
  structured_output: { summary: 'Review now has a 4th reviewer.' },
  session_id: 's1',
};

const env = { PATH: '/usr/bin', CLAUDE_CODE_OAUTH_TOKEN: 'oauth-s3cr3t', GOAL_GATEWAY_TOKEN: 'gateway-s3cr3t' };
const at = new Date('2026-10-27T10:00:00Z');

const spec = parseSpec(
  JSON.stringify({
    factory_id: 'f1',
    item_id: '',
    run_id: 'r1',
    station: '',
    agent: '',
    model: 'claude-sonnet-5-5',
    skills: ['acme'],
    tools_allow: ['Bash'],
    tools_deny: [],
    gateway_url: 'http://goal-egress:3128/mcp',
    brief: '',
    max_turns: 30,
    max_usd: 2,
    connections: [
      { name: 'github', kind: 'github', title: 'GitHub', env: ['GH_TOKEN'], mcp: { url: 'https://example.com/mcp' } },
    ],
    draft_request: { id: 'd1', text: 'Give review a 4th reviewer', agent: 'reviewer' },
  }),
);

function deps(fake: typeof query) {
  const lines: WorkerEvent[] = [];
  return {
    lines,
    deps: {
      query: fake,
      emit: (e: WorkerEvent) => lines.push(e),
      now: () => at,
      env,
      abortController: new AbortController(),
      dirs: { workspace, goal },
    },
  };
}

describe('runDefinition', () => {
  it('finishes with the summary, the changed and deleted files, cost and turns', async () => {
    await draft(start);
    const { fake } = scripted([success], async () => {
      await put('factory.yaml', 'kind: Factory\nreviewers: 4\n');
      await unlink(join(workspace, 'agents/reviewer.md'));
    });
    const { deps: d, lines } = deps(fake);

    expect(await runDefinition(spec, d)).toBe(0);

    const last = lines.at(-1);
    expect(last).toMatchObject({ type: 'finished', status: 'succeeded', cost_usd: 0.12, turns: 4 });
    expect(last?.type === 'finished' && last.output).toEqual({
      summary: 'Review now has a 4th reviewer.',
      changed: [
        {
          path: 'factory.yaml',
          sha256: sha('kind: Factory\nreviewers: 4\n'),
          content: b64('kind: Factory\nreviewers: 4\n'),
        },
      ],
      deleted: ['agents/reviewer.md'],
      rejected: [],
    });
  });

  it('fails a run that changed something outside the allowlist, naming the paths, and drops the change', async () => {
    await draft(start);
    const { fake } = scripted([success], async () => {
      await put('rules/secrets.md', '# Secrets, kept\n');
      await put('evals/review.yaml', 'cases: [1]\n');
    });
    const { deps: d, lines } = deps(fake);

    expect(await runDefinition(spec, d)).toBe(0);

    const last = lines.at(-1);
    expect(last).toMatchObject({ type: 'finished', status: 'failed', cost_usd: 0.12, turns: 4 });
    expect(last?.type === 'finished' && last.error).toContain('evals/review.yaml');
    const output =
      last?.type === 'finished' ? (last.output as { changed: { path: string }[]; rejected: string[] }) : undefined;
    expect(output?.changed.map((c) => c.path)).toEqual(['rules/secrets.md']);
    expect(output?.rejected).toEqual(['evals/review.yaml']);
  });

  it('gives the query the fixed prompt, the file tools only and no MCP server', async () => {
    await draft(start);
    const { fake, calls } = scripted([success]);
    const { deps: d } = deps(fake);

    await runDefinition(spec, d);

    const options = calls[0]?.options;
    expect(options?.systemPrompt).toEqual({ type: 'preset', preset: 'claude_code', append: definitionSystemPrompt });
    expect(definitionTools).toEqual(['Read', 'Write', 'Edit', 'MultiEdit', 'Glob', 'Grep']);
    expect(options?.tools).toEqual(definitionTools);
    expect(options?.allowedTools ?? []).toEqual([]);
    expect(options?.mcpServers).toEqual({});
    expect(options?.strictMcpConfig).toBe(true);
    expect(options?.skills).toEqual([]);
    expect(options?.settingSources).toEqual(['user', 'project']);
    expect(options?.cwd).toBe(workspace);
    expect(options).toMatchObject({ model: 'claude-sonnet-5-5', maxTurns: 30, maxBudgetUsd: 2 });
    expect(options?.outputFormat).toMatchObject({ type: 'json_schema', schema: { required: ['summary'] } });
    expect(options?.env?.GOAL_GATEWAY_TOKEN).toBeUndefined();

    const prompt = String(calls[0]?.inputs[0]?.message.content);
    expect(prompt).toContain('Give review a 4th reviewer');
    expect(prompt).toContain('reviewer');
    for (const p of ['evals/', 'contracts/', 'factory.yaml']) expect(definitionSystemPrompt).toContain(p);
  });

  it('lets the file tools reach only the workspace, and the factory schema for reading', async () => {
    await draft(start);
    const { fake, calls } = scripted([success]);
    await runDefinition(spec, deps(fake).deps);
    const can = calls[0]?.options.canUseTool as CanUseTool | undefined;
    if (!can) throw new Error('no canUseTool');
    const decide = async (tool: string, input: Record<string, unknown>) =>
      (await can(tool, input, { signal: new AbortController().signal, toolUseID: 't1', requestId: 'q1' }))?.behavior;

    expect(await decide('Write', { file_path: join(workspace, 'agents/new.md') })).toBe('allow');
    expect(await decide('Edit', { file_path: 'factory.yaml' })).toBe('allow');
    expect(await decide('Read', { file_path: join(goal, 'definition/factory.schema.json') })).toBe('allow');
    expect(await decide('Grep', { pattern: 'x' })).toBe('allow');
    expect(await decide('Write', { file_path: join(goal, 'definition.json') })).toBe('deny');
    expect(await decide('Write', { file_path: join(workspace, '../goal/definition.json') })).toBe('deny');
    expect(await decide('Read', { file_path: '/proc/self/environ' })).toBe('deny');
    expect(await decide('Glob', { pattern: '*', path: '/home' })).toBe('deny');
    expect(await decide('Bash', { command: 'env' })).toBe('deny');
    await symlink(root, join(workspace, 'agents/out'));
    expect(await decide('Write', { file_path: join(workspace, 'agents/out/goal/definition.json') })).toBe('deny');
  });

  it('puts no env value in any emitted line', async () => {
    await draft(start);
    const { fake } = scripted(
      [{ type: 'assistant', message: { content: [{ type: 'text', text: 'working' }] } }, success],
      async () => put('factory.yaml', 'kind: Factory\nreviewers: 4\n'),
    );
    const { deps: d, lines } = deps(fake);

    await runDefinition(spec, d);

    const out = lines.map((l) => JSON.stringify(l)).join('\n');
    for (const value of Object.values(env)) {
      if (value !== '/usr/bin') expect(out).not.toContain(value);
    }
  });

  it('fails without a crash when the agent ends without a result', async () => {
    await draft(start);
    const { deps: d, lines } = deps(scripted([]).fake);

    expect(await runDefinition(spec, d)).toBe(1);
    expect(lines.at(-1)).toMatchObject({
      type: 'finished',
      status: 'failed',
      error: 'the agent ended without a result',
    });
  });
});
