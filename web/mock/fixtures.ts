// Illustrative data for the mock API: a "goal" factory building itself and a small SRE factory
// (sample org acme). Times are relative to when the mock starts, so ages read like a live floor.
import type { ClassOfService, Factory, ItemEvent, ItemState, Metrics } from '@goal/api-client';

export interface MockItem {
  id: string;
  factory: string;
  number: number;
  type: string;
  lane: string | null;
  title: string;
  body: string;
  station: string;
  state: ItemState;
  class_of_service: ClassOfService;
  priority: number;
  rework_count: number;
  cost_usd: number;
  outputs: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  done_at?: string;
  agent: string | null;
  pr_url: string | null;
}

export interface MockStationConfig {
  agents: string[];
  wip_limit: number | null;
}

export interface MockState {
  factories: Factory[];
  stations: Record<string, Record<string, MockStationConfig>>;
  constraint: Record<string, string | null>;
  metrics: Record<string, Metrics>;
  items: MockItem[];
  events: ItemEvent[];
  nextEventId: number;
}

type Tool = [name: string, input: string, summary: string, isError?: boolean];

type Step =
  | { move: string; by?: string }
  | {
      run: string;
      tools: Tool[];
      message?: string;
      cost: number;
      turns: number;
      status?: 'succeeded' | 'failed' | 'running';
      error?: string;
    }
  | { check: string; exit: number; ms: number }
  | { gate: 'approve' | 'reject'; agent: string; reason?: string }
  | { human: 'approved' | 'rejected'; by: string; reason?: string }
  | { waiting: true; reason?: string }
  | { blocked: string }
  | { done: true };

interface Seed {
  number: number;
  title: string;
  type: string;
  lane?: string;
  body: string;
  station: string;
  state: ItemState;
  expedite?: boolean;
  agent?: string;
  rework?: number;
  pr?: number;
  createdMin: number;
  updatedMin: number;
  outputs?: Record<string, unknown>;
  steps: Step[];
}

const model = 'claude-sonnet-5-5';
const repo = 'https://github.com/acme/goal';

const goalFactory: Factory = {
  id: '6f0c2a7e-3d1b-4c55-9a7e-1f2b3c4d5e60',
  name: 'goal',
  title: 'Goal',
  description: 'Builds the Goal platform from this repository: engine, UI, infrastructure and its own factories.',
  work_types: ['feature', 'fix', 'refactor', 'infra', 'design'],
  stations: [
    { id: 'inbox', kind: 'queue' },
    { id: 'spec', kind: 'agent' },
    { id: 'approve-spec', kind: 'human' },
    { id: 'build', kind: 'agent' },
    { id: 'review', kind: 'agent' },
    { id: 'merge', kind: 'human', action: 'merge' },
    { id: 'done', kind: 'done' },
  ],
};

const sreFactory: Factory = {
  id: '0b7d9e21-58a4-4f0e-b6c3-2a9d8e7f6a51',
  name: 'sre',
  title: 'SRE',
  description: 'Triage, investigate and document production issues and SRE toil.',
  work_types: ['incident', 'change', 'toil'],
  stations: [
    { id: 'inbox', kind: 'queue' },
    { id: 'triage', kind: 'agent' },
    { id: 'investigate', kind: 'agent' },
    { id: 'approve', kind: 'human' },
    { id: 'done', kind: 'done' },
  ],
};

const specOutput = (summary: string, criteria: string[], lane: string) => ({
  summary,
  acceptance_criteria: criteria,
  lane,
});

const specRun = (tools: Tool[], cost: number, turns: number): Step => ({
  run: 'spec-writer',
  tools,
  message: 'Wrote the spec with acceptance criteria and chose the lane.',
  cost,
  turns,
});

const goalSeeds: Seed[] = [
  {
    number: 16,
    title: 'goald exits when the bucket has no factories',
    type: 'fix',
    body: 'Starting goald against an empty bucket ends the process instead of serving an empty floor.',
    station: 'spec',
    state: 'in_progress',
    expedite: true,
    agent: 'spec-writer',
    createdMin: 38,
    updatedMin: 1,
    steps: [
      { move: 'spec' },
      {
        run: 'spec-writer',
        status: 'running',
        cost: 0.31,
        turns: 9,
        tools: [
          ['Read', 'cmd/goald/main.go', 'Read 214 lines'],
          ['Grep', 'factories loaded', 'internal/definitions/definitions.go:88, cmd/goald/main.go:152'],
          ['Bash', 'go test ./internal/definitions/...', 'ok  internal/definitions 0.41s'],
        ],
      },
    ],
  },
  {
    number: 15,
    title: 'Metrics endpoint: lead time percentiles per work type',
    type: 'feature',
    body: 'The floor needs p50 and p85 lead time per work type, not only per factory.',
    station: 'inbox',
    state: 'queued',
    createdMin: 64,
    updatedMin: 64,
    steps: [],
  },
  {
    number: 14,
    title: 'Board: a Move menu on every card',
    type: 'feature',
    body: 'Every card gets a Move menu so a card can change station without dragging.',
    station: 'inbox',
    state: 'queued',
    createdMin: 190,
    updatedMin: 190,
    steps: [],
  },
  {
    number: 13,
    title: 'Spec the knowledge sync retry',
    type: 'feature',
    body: 'Knowledge sync gives up after one failure. Retry with backoff and surface the failure on the floor.',
    station: 'spec',
    state: 'in_progress',
    agent: 'spec-writer',
    createdMin: 95,
    updatedMin: 2,
    steps: [
      { move: 'spec' },
      {
        run: 'spec-writer',
        status: 'running',
        cost: 0.22,
        turns: 6,
        tools: [
          ['Read', 'docs/adr/0005-factory-definitions-in-object-storage.md', 'Read 61 lines'],
          ['Glob', 'internal/factorystore/*.go', '3 files'],
        ],
      },
    ],
  },
  {
    number: 12,
    title: 'goald: serve the embedded web UI',
    type: 'feature',
    lane: 'go',
    body: 'goald serves the built SPA from embedded files with a fallback to index.html for client routes.',
    station: 'approve-spec',
    state: 'waiting_human',
    createdMin: 170,
    updatedMin: 48,
    outputs: {
      spec: specOutput(
        'Embed web/dist into goald and serve it at /, falling back to index.html for client routes; /v1 and /mcp keep their handlers.',
        [
          'GET / returns index.html with no-cache',
          'Hashed assets are served with a one-year immutable cache',
          'go build works when web/dist has not been built',
        ],
        'go',
      ),
    },
    steps: [
      { move: 'spec' },
      specRun(
        [
          ['Read', 'docs/engineering/frontend.md', 'Read 118 lines'],
          ['Grep', 'mux.Handle', 'cmd/goald/main.go:168, cmd/goald/main.go:169'],
          ['Read', 'cmd/goald/main.go', 'Read 214 lines'],
          ['WebFetch', 'pkg.go.dev/embed', 'Directive syntax and the all: prefix'],
        ],
        0.41,
        12,
      ),
      { move: 'approve-spec' },
      { waiting: true },
    ],
  },
  {
    number: 11,
    title: 'Item trace: group tool calls by run',
    type: 'feature',
    lane: 'web',
    body: 'Show each agent run as one block with its tool calls, cost and turns.',
    station: 'build',
    state: 'ready',
    createdMin: 300,
    updatedMin: 118,
    outputs: {
      spec: specOutput('Group tool calls under their run in the item trace.', ['Runs show cost and turns'], 'web'),
    },
    steps: [
      { move: 'spec' },
      specRun([['Read', 'web/README.md', 'Read 40 lines']], 0.18, 5),
      { move: 'approve-spec' },
      { waiting: true },
      { human: 'approved', by: 'fcruz' },
      { move: 'build' },
    ],
  },
  {
    number: 10,
    title: 'Stream: resume from the last event after a reconnect',
    type: 'fix',
    lane: 'go',
    body: 'A browser that reconnects to /v1/stream misses the events sent while it was away.',
    station: 'build',
    state: 'ready',
    createdMin: 260,
    updatedMin: 96,
    outputs: { spec: specOutput('Honour Last-Event-ID on /v1/stream.', ['No event is lost across a reconnect'], 'go') },
    steps: [
      { move: 'spec' },
      specRun([['Grep', 'text/event-stream', 'internal/api/stream.go:31']], 0.21, 6),
      { move: 'approve-spec' },
      { waiting: true },
      { human: 'approved', by: 'fcruz' },
      { move: 'build' },
    ],
  },
  {
    number: 9,
    title: 'Gateway: cache tools/list per agent',
    type: 'refactor',
    lane: 'go',
    body: 'tools/list is fetched from every upstream on each task. Cache it per agent and scope.',
    station: 'build',
    state: 'in_progress',
    agent: 'builder-1',
    rework: 1,
    createdMin: 410,
    updatedMin: 3,
    outputs: {
      spec: specOutput('Cache tools/list per agent with a scope key.', ['Second task reuses the list'], 'go'),
    },
    steps: [
      { move: 'spec' },
      specRun([['Read', 'internal/gateway/upstream.go', 'Read 188 lines']], 0.27, 7),
      { move: 'approve-spec' },
      { waiting: true },
      { human: 'approved', by: 'fcruz' },
      { move: 'build' },
      {
        run: 'builder-1',
        cost: 1.84,
        turns: 31,
        tools: [
          ['Read', 'internal/gateway/upstream.go', 'Read 188 lines'],
          ['Edit', 'internal/gateway/upstream.go', 'Added toolsCache with a scope key'],
          ['Bash', 'go test ./internal/gateway/...', 'ok  internal/gateway 1.92s'],
        ],
        message: 'Added a per-agent cache for tools/list.',
      },
      { check: 'task lint', exit: 0, ms: 41_200 },
      { check: 'go test -race ./...', exit: 0, ms: 88_400 },
      { move: 'review' },
      {
        run: 'reviewer',
        cost: 0.62,
        turns: 11,
        tools: [['Bash', 'git diff main...HEAD --stat', '3 files changed, 96 insertions(+), 12 deletions(-)']],
      },
      {
        gate: 'reject',
        agent: 'reviewer',
        reason: 'The cache never expires; add a TTL and a test that proves a stale list is refreshed.',
      },
      { move: 'build' },
      {
        run: 'builder-1',
        status: 'running',
        cost: 0.58,
        turns: 9,
        tools: [
          ['Read', 'internal/gateway/upstream.go', 'Read 214 lines'],
          ['Edit', 'internal/gateway/upstream.go', 'Added a 5 minute TTL'],
          ['Bash', 'go test ./internal/gateway/ -run TestToolsCache', 'FAIL TestToolsCache_Refreshes (0.02s)', true],
        ],
      },
    ],
  },
  {
    number: 8,
    title: 'Worker: report usage per message',
    type: 'feature',
    lane: 'web',
    body: 'Emit a usage line per API message so goald can show tokens as they are spent.',
    station: 'build',
    state: 'in_progress',
    agent: 'builder-2',
    createdMin: 520,
    updatedMin: 1,
    outputs: { spec: specOutput('Emit usage lines from the worker.', ['goald records usage events'], 'web') },
    steps: [
      { move: 'spec' },
      specRun([['Read', 'worker/README.md', 'Read 96 lines']], 0.24, 6),
      { move: 'approve-spec' },
      { waiting: true },
      { human: 'approved', by: 'fcruz' },
      { move: 'build' },
      {
        run: 'builder-2',
        status: 'running',
        cost: 1.12,
        turns: 22,
        tools: [
          ['Read', 'worker/src/main.ts', 'Read 240 lines'],
          ['Edit', 'worker/src/protocol.ts', 'Added the usage line type'],
          ['Bash', 'pnpm --filter @goal/worker test', '14 passed'],
          ['Edit', 'worker/README.md', 'Documented usage fields'],
        ],
      },
    ],
  },
  {
    number: 7,
    title: 'Terraform: ECS service for goald',
    type: 'infra',
    lane: 'infra',
    body: 'Run goald on ECS behind an ALB with the database URL from Secrets Manager.',
    station: 'review',
    state: 'in_progress',
    agent: 'reviewer',
    pr: 41,
    createdMin: 700,
    updatedMin: 4,
    outputs: {
      spec: specOutput(
        'An ECS service module for goald.',
        ['terraform validate passes', 'No secret in plain text'],
        'infra',
      ),
      build: {
        pull_request: `${repo}/pull/41`,
        branch: 'goal/7',
        summary: 'Added terraform/modules/goald with the service, task role and ALB target group.',
      },
    },
    steps: [
      { move: 'spec' },
      specRun([['Read', 'terraform/README.md', 'Read 72 lines']], 0.33, 8),
      { move: 'approve-spec' },
      { waiting: true },
      { human: 'approved', by: 'fcruz' },
      { move: 'build' },
      {
        run: 'builder-2',
        cost: 2.41,
        turns: 38,
        tools: [
          ['Write', 'terraform/modules/goald/main.tf', 'Created 142 lines'],
          ['Bash', 'terraform validate', 'Success! The configuration is valid.'],
          ['Bash', 'gh pr create --fill', `${repo}/pull/41`],
        ],
      },
      { check: 'task tf-check', exit: 0, ms: 23_900 },
      { move: 'review' },
      {
        run: 'reviewer',
        status: 'running',
        cost: 0.4,
        turns: 7,
        tools: [['Bash', 'gh pr diff 41', '+142 -0 in terraform/modules/goald']],
      },
    ],
  },
  {
    number: 6,
    title: 'Floor: throughput bars per day',
    type: 'design',
    lane: 'web',
    body: 'Show items finished per day for the window as hatched bars, today solid.',
    station: 'merge',
    state: 'waiting_human',
    pr: 38,
    rework: 1,
    createdMin: 980,
    updatedMin: 35,
    outputs: {
      spec: specOutput('Daily throughput bars on the floor.', ['Bars are labelled', 'Today is solid teal'], 'web'),
      build: {
        pull_request: `${repo}/pull/38`,
        branch: 'goal/6',
        summary: 'Added DailyBars with an SVG hatch pattern and labels.',
      },
      review: { decision: 'approve', reason: 'Labels and contrast check out; axe passes at three widths.' },
    },
    steps: [
      { move: 'spec' },
      specRun([['Read', 'design-system/goal/MASTER.md', 'Read 230 lines']], 0.29, 7),
      { move: 'approve-spec' },
      { waiting: true },
      { human: 'approved', by: 'fcruz' },
      { move: 'build' },
      {
        run: 'builder-1',
        cost: 1.96,
        turns: 29,
        tools: [
          ['Write', 'web/src/ui/daily-bars.tsx', 'Created 74 lines'],
          ['Bash', 'pnpm --filter @goal/web test', '22 passed'],
          ['Bash', 'gh pr create --fill', `${repo}/pull/38`],
        ],
      },
      { check: 'task web:check', exit: 1, ms: 31_000 },
      { move: 'build' },
      {
        run: 'builder-1',
        cost: 0.71,
        turns: 10,
        tools: [
          ['Bash', 'pnpm biome check --write src', 'Fixed 2 files'],
          ['Bash', 'git push', 'goal/6 -> goal/6'],
        ],
      },
      { check: 'task web:check', exit: 0, ms: 29_400 },
      { move: 'review' },
      {
        run: 'reviewer',
        cost: 0.55,
        turns: 9,
        tools: [['Bash', 'gh pr diff 38', '+118 -6 in web/src']],
      },
      { gate: 'reject', agent: 'reviewer', reason: 'Bars rely on colour alone for today; add a text label.' },
      { move: 'build' },
      {
        run: 'builder-1',
        cost: 0.38,
        turns: 6,
        tools: [['Edit', 'web/src/ui/daily-bars.tsx', 'Labelled each bar']],
      },
      { check: 'task web:check', exit: 0, ms: 28_100 },
      { move: 'review' },
      { run: 'reviewer', cost: 0.31, turns: 5, tools: [['Bash', 'gh pr checks 38', 'All checks passed']] },
      { gate: 'approve', agent: 'reviewer' },
      { move: 'merge' },
      { waiting: true },
    ],
  },
  {
    number: 5,
    title: 'Retry blocked items from the CLI',
    type: 'fix',
    lane: 'go',
    body: 'goal retry <item> puts a blocked item back in its station queue.',
    station: 'build',
    state: 'blocked',
    rework: 2,
    createdMin: 1300,
    updatedMin: 140,
    outputs: { spec: specOutput('A goal retry command.', ['Blocked items return to the queue'], 'go') },
    steps: [
      { move: 'spec' },
      specRun([['Read', 'cmd/goal/work.go', 'Read 340 lines']], 0.26, 7),
      { move: 'approve-spec' },
      { waiting: true },
      { human: 'approved', by: 'fcruz' },
      { move: 'build' },
      {
        run: 'builder-2',
        cost: 1.31,
        turns: 24,
        tools: [
          ['Edit', 'cmd/goal/work.go', 'Added the retry subcommand'],
          ['Bash', 'go test ./cmd/goal/', 'FAIL TestRetry_UnknownItem', true],
        ],
      },
      { check: 'go test -race ./...', exit: 1, ms: 91_000 },
      {
        run: 'builder-2',
        cost: 0.92,
        turns: 15,
        tools: [['Edit', 'cmd/goal/work_test.go', 'Fixed the stub server']],
      },
      { check: 'go test -race ./...', exit: 1, ms: 90_300 },
      {
        run: 'builder-2',
        status: 'failed',
        cost: 0.66,
        turns: 40,
        error: 'error_max_turns: the agent used all 40 turns',
        tools: [['Bash', 'go test ./cmd/goal/ -run TestRetry', 'FAIL TestRetry_UnknownItem', true]],
      },
      { blocked: 'go test -race ./... failed 3 times; a human must look' },
    ],
  },
  {
    number: 4,
    title: 'Add goal version subcommand',
    type: 'feature',
    lane: 'go',
    body: 'goal version prints the build version and commit.',
    station: 'done',
    state: 'done',
    pr: 36,
    createdMin: 2900,
    updatedMin: 1500,
    outputs: {
      spec: specOutput('A version subcommand.', ['Prints version and commit'], 'go'),
      build: { pull_request: `${repo}/pull/36`, branch: 'goal/4', summary: 'Added goal version.' },
    },
    steps: [
      { move: 'spec' },
      specRun([['Read', 'cmd/goal/main.go', 'Read 120 lines']], 0.15, 4),
      { move: 'approve-spec' },
      { waiting: true },
      { human: 'approved', by: 'fcruz' },
      { move: 'build' },
      { run: 'builder-1', cost: 0.88, turns: 14, tools: [['Edit', 'cmd/goal/main.go', 'Added version']] },
      { check: 'go test -race ./...', exit: 0, ms: 80_000 },
      { move: 'review' },
      { run: 'reviewer', cost: 0.21, turns: 4, tools: [['Bash', 'gh pr diff 36', '+40 -2']] },
      { gate: 'approve', agent: 'reviewer' },
      { move: 'merge' },
      { waiting: true },
      { human: 'approved', by: 'fcruz' },
      { move: 'done' },
      { done: true },
    ],
  },
  {
    number: 3,
    title: 'Gateway: send the Mcp-Param headers upstream servers require',
    type: 'fix',
    lane: 'go',
    body: 'Some MCP servers reject calls without Mcp-Param headers.',
    station: 'done',
    state: 'done',
    pr: 33,
    createdMin: 4200,
    updatedMin: 2800,
    outputs: { build: { pull_request: `${repo}/pull/33`, branch: 'goal/3', summary: 'Forward Mcp-Param headers.' } },
    steps: [
      { move: 'spec' },
      { move: 'build' },
      { move: 'review' },
      { move: 'merge' },
      { move: 'done' },
      { done: true },
    ],
  },
];

const sreSeeds: Seed[] = [
  {
    number: 3,
    title: 'Disk at 85% on bastion-prod',
    type: 'toil',
    body: 'Alert from the disk usage monitor.',
    station: 'triage',
    state: 'in_progress',
    agent: 'triager',
    createdMin: 12,
    updatedMin: 1,
    steps: [
      { move: 'triage' },
      { run: 'triager', status: 'running', cost: 0.08, turns: 3, tools: [['Bash', 'df -h', '/ 85% used']] },
    ],
  },
  {
    number: 2,
    title: 'New runbook: ECS task out of memory',
    type: 'change',
    body: 'Write a runbook for ECS tasks killed for memory.',
    station: 'approve',
    state: 'waiting_human',
    createdMin: 240,
    updatedMin: 60,
    outputs: { investigate: { summary: 'Runbook drafted with three diagnosis steps.' } },
    steps: [
      { move: 'triage' },
      { run: 'triager', cost: 0.06, turns: 2, tools: [] },
      { move: 'investigate' },
      { run: 'investigator', cost: 0.92, turns: 18, tools: [['Read', 'runbooks/ecs.md', 'Read 80 lines']] },
      { move: 'approve' },
      { waiting: true },
    ],
  },
];

function uuid(prefix: number, n: number): string {
  const hex = (prefix * 1000 + n).toString(16).padStart(12, '0');
  return `00000000-0000-4000-8000-${hex}`;
}

function build(factory: Factory, factoryIndex: number, seeds: Seed[], now: number, state: MockState) {
  for (const seed of seeds) {
    const id = uuid(factoryIndex + 1, seed.number);
    const created = now - seed.createdMin * 60_000;
    const updated = now - seed.updatedMin * 60_000;
    const first = factory.stations[0]?.id ?? 'inbox';

    const item: MockItem = {
      id,
      factory: factory.name,
      number: seed.number,
      type: seed.type,
      lane: seed.lane ?? null,
      title: seed.title,
      body: seed.body,
      station: seed.station,
      state: seed.state,
      class_of_service: seed.expedite ? 'expedite' : 'standard',
      priority: 0,
      rework_count: seed.rework ?? 0,
      cost_usd: 0,
      outputs: seed.outputs ?? {},
      created_at: new Date(created).toISOString(),
      updated_at: new Date(updated).toISOString(),
      agent: seed.agent ?? null,
      pr_url: seed.pr ? `${repo}/pull/${seed.pr}` : null,
    };

    if (seed.state === 'done') item.done_at = item.updated_at;

    // Spread the history evenly over the item's lifetime.
    const ticks =
      1 + seed.steps.reduce((n, s) => n + ('run' in s ? 2 + s.tools.length * 2 + (s.message ? 1 : 0) : 1), 0);
    const dt = ticks > 1 ? (updated - created) / (ticks - 1) : 0;
    let at = created;
    let station = first;
    const push = (kind: string, actor: string, data: Record<string, unknown> = {}, run?: string) => {
      state.events.push({
        id: state.nextEventId++,
        item_id: id,
        ...(run ? { run_id: run } : {}),
        at: new Date(Math.round(at)).toISOString(),
        kind,
        station,
        actor,
        data,
      });
      at += dt;
    };

    push('created', 'engine');
    let runs = 0;
    for (const step of seed.steps) {
      if ('move' in step) {
        const from = station;
        station = step.move;
        push('moved', step.by ?? 'engine', { from, to: step.move });
      } else if ('run' in step) {
        const run = `${id.slice(-6)}-run-${++runs}`;
        push('claimed', 'engine', { slot: 0, agent: step.run });
        push('run_started', step.run, {}, run);
        push('agent_started', step.run, { model, session_id: run }, run);
        step.tools.forEach(([tool, input, summary, isError], i) => {
          push('tool_call', step.run, { id: `t${i}`, tool, input: JSON.stringify({ arg: input }) }, run);
          push('tool_result', step.run, { id: `t${i}`, is_error: Boolean(isError), summary }, run);
        });
        if (step.message) push('agent_message', step.run, { text: step.message }, run);
        item.cost_usd += step.cost;
        if (step.status !== 'running') {
          push(
            'run_finished',
            step.run,
            { status: step.status ?? 'succeeded', error: step.error ?? '', cost_usd: step.cost, turns: step.turns },
            run,
          );
        }
      } else if ('check' in step) {
        push(step.exit === 0 ? 'check_passed' : 'check_failed', 'engine', {
          command: step.check,
          exit_code: step.exit,
          duration_ms: step.ms,
        });
      } else if ('gate' in step) {
        push(
          step.gate === 'approve' ? 'gate_approved' : 'gate_rejected',
          step.agent,
          step.reason ? { reason: step.reason } : {},
        );
      } else if ('human' in step) {
        push(step.human, step.by, step.reason ? { reason: step.reason } : {});
      } else if ('waiting' in step) {
        push('waiting_human', 'engine', step.reason ? { reason: step.reason } : {});
      } else if ('blocked' in step) {
        push('blocked', 'engine', { reason: step.blocked });
      } else {
        push('done', 'engine');
      }
    }
    item.cost_usd = Math.round(item.cost_usd * 100) / 100;
    state.items.push(item);
  }
}

export function seed(now = Date.now()): MockState {
  const state: MockState = {
    factories: [goalFactory, sreFactory],
    stations: {
      goal: {
        inbox: { agents: [], wip_limit: null },
        spec: { agents: ['spec-writer'], wip_limit: 2 },
        'approve-spec': { agents: [], wip_limit: null },
        build: { agents: ['builder-1', 'builder-2'], wip_limit: 2 },
        review: { agents: ['reviewer'], wip_limit: 1 },
        merge: { agents: [], wip_limit: null },
        done: { agents: [], wip_limit: null },
      },
      sre: {
        inbox: { agents: [], wip_limit: null },
        triage: { agents: ['triager'], wip_limit: 4 },
        investigate: { agents: ['investigator'], wip_limit: 3 },
        approve: { agents: [], wip_limit: null },
        done: { agents: [], wip_limit: null },
      },
    },
    constraint: { goal: 'build', sre: null },
    metrics: {},
    items: [],
    events: [],
    nextEventId: 1,
  };

  build(goalFactory, 0, goalSeeds, now, state);
  build(sreFactory, 1, sreSeeds, now, state);

  const day = (offset: number) => new Date(now - offset * 86_400_000).toISOString().slice(0, 10);
  state.metrics.goal = {
    window: '7d',
    throughput: [2, 1, 3, 0, 2, 4, 1].map((done, i) => ({ day: day(6 - i), done })),
    lead_time_hours: { p50: 6.2, p85: 14.8 },
    wip: 9,
    rework_rate: 0.23,
    spend_usd: 18.42,
    items_done: 13,
  };
  state.metrics.sre = {
    window: '7d',
    throughput: [0, 1, 0, 0, 1, 0, 0].map((done, i) => ({ day: day(6 - i), done })),
    lead_time_hours: null,
    wip: 2,
    rework_rate: 0,
    spend_usd: 1.06,
    items_done: 2,
  };

  return state;
}
