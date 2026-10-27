// Illustrative data for the mock API: two organizations, as the Org-Switcher board shows them.
// Acme has three factories (a "goal" factory building itself, a small SRE factory with three
// decisions waiting, as on the canvas, and an empty support factory whose line is stopped by an
// andon pull); Example Labs has one, empty. Times are relative to when the mock starts, so ages
// read like a live floor.
import type {
  Actor,
  AgentStats,
  AndonPull,
  AndonState,
  AuditEntry,
  ClassOfService,
  Factory,
  GateTally,
  ItemEvent,
  ItemSpan,
  ItemState,
  Org,
  Run,
  VersionRef,
} from '@goal/api-client';
import {
  type MockDefinition,
  type MockSkill,
  type MockTemplate,
  seedDefinitions,
  seedSkills,
  seedTemplates,
} from './definition-seed.ts';
import type { MockFault } from './faults.ts';
import { type MockDone, seedHistory } from './flow.ts';
import { type MockPeople, seedPeople } from './people.ts';
import type { MockReason } from './touches.ts';

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
  /** Its spans at stations, kept as goald's trigger keeps item_spans (trackSpan). */
  spans: ItemSpan[];
  /** The files it changes (its plan's touches), which a files wait compares. Not on Item. */
  paths?: string[];
  /** The open need it waits for while queued or ready (Item.waiting). Not on Item. */
  waitsOn?: MockWaitsOn;
}

/** The need a queued item waits for: an item of the factory by number, by its order (after) or its files (files). */
export interface MockWaitsOn {
  kind: 'after' | 'files';
  number: number;
}

/** An agent's measures (AgentStats without what the request and the line give). */
export type MockAgentStats = Omit<AgentStats, 'agent' | 'from' | 'to' | 'stations'>;

export interface MockStationConfig {
  agents: string[];
  wip_limit: number | null;
  /** How many copies of its agent the station runs (agents in the definition); 1 when absent. */
  copies?: number;
  /** Items kept ready in front of the station (its buffer, at the drum). */
  buffer?: number;
  /** The contract the station's output must satisfy, and what it checks on submit. */
  output?: string;
  checks?: string[];
}

/** A factory's flow targets and task budget (flow.targets, runtime.budget), in seconds and dollars. */
export interface MockFlow {
  throughput_per_day?: number | null;
  lead_time: number | null;
  expedite_lead_time: number | null;
  per_item_usd: number | null;
  max_turns_per_task: number | null;
  /** flow.release; push when absent. */
  release?: 'push' | 'drum-buffer-rope';
  /** flow.expedite.wip (null: no limit); absent when the factory has no expedite lane. */
  expedite_wip?: number | null;
}

/** Where an item stands, as a span records it. */
interface Position {
  station: string;
  state: ItemState;
  rework_count: number;
}

/**
 * Keeps an item's spans as goald's work_items trigger does: a change of station, state or rework
 * count closes the open span at `at` and opens the next one, none once the item is done or
 * cancelled; a new rework round is by the station the item left.
 */
export function trackSpan(spans: ItemSpan[], pos: Position, at: string) {
  const open = spans.findLast((s) => s.left_at === null);
  if (open && open.station === pos.station && open.state === pos.state && open.rework_round === pos.rework_count)
    return;
  let by: string | null = null;
  if (open) {
    open.left_at = at;
    by = pos.rework_count > open.rework_round ? open.station : open.rework_by;
  }
  if (pos.state === 'done' || pos.state === 'cancelled') return;
  spans.push({
    station: pos.station,
    state: pos.state,
    rework_round: pos.rework_count,
    rework_by: by,
    entered_at: at,
    left_at: null,
  });
}

/** A station's busy and send-back shares over the board window and its queue's trend. */
export interface MockStationStats {
  busy_ratio?: number;
  send_back_ratio?: number;
  queue_trend?: 'growing' | 'steady' | 'shrinking';
}

/** An organization's slot caps (null: no cap), by factory id for the factories. */
export interface MockSlots {
  max: number | null;
  factories: Record<string, number | null>;
}

export interface MockState {
  /** Organizations by name; factory_count is computed from factories. */
  orgs: Org[];
  /** Who exists and belongs where, and the invitations (people.ts). */
  people: MockPeople;
  slots: Record<string, MockSlots>;
  /** Every organization's audit log, oldest first. */
  audit: { org: string; entry: AuditEntry }[];
  nextAuditId: number;
  factories: Factory[];
  stations: Record<string, Record<string, MockStationConfig>>;
  constraint: Record<string, string | null>;
  /** Board statistics goald derives from history, by factory name and station. */
  stationStats: Record<string, Record<string, MockStationStats>>;
  /** Each factory's items finished before the mock started (flow.ts), by name. */
  history: Record<string, MockDone[]>;
  /**
   * Each factory's andon pulls, by name, oldest first; the last is open while its cleared_at is
   * null. Absent or empty: the line runs and was never stopped.
   */
  andon: Record<string, AndonPull[]>;
  nextAndonId: number;
  /** Each factory's board.flow.andon policy and its gates' tallies, by name; absent without gates. */
  gates: Record<string, { policy: AndonState['policy']; tallies: GateTally[] }>;
  /** Each factory's targets and task budget, by name; absent without any. */
  flow: Record<string, MockFlow>;
  /** Each factory's runtime.budget.weeklyUsd, by name; absent or null without one. */
  weeklyBudget: Record<string, number | null>;
  /** What goald measures of each agent over the last 7 days, by factory and agent; zeros when absent. */
  agentStats: Record<string, Record<string, MockAgentStats>>;
  items: MockItem[];
  events: ItemEvent[];
  nextEventId: number;
  /** The reasons people set on touches, by event id (goald's item_touch_labels, but defaults). */
  touchReasons: Record<number, MockReason>;
  /** Every agent run, in start order; goald's agent_runs. */
  runs: Run[];
  /** Each factory's versions and draft, by factory id. */
  definitions: Record<string, MockDefinition>;
  /** The template catalog (catalog/templates/). */
  templates: MockTemplate[];
  /** The skills.sh skills catalog (catalog/skills/). */
  skills: MockSkill[];
  /** Faults injected by POST /__mock/fail. */
  faults: MockFault[];
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
  paths?: string[];
  waitsOn?: MockWaitsOn;
  steps: Step[];
}

const model = 'claude-sonnet-5-5';

/** The published definition version each factory's runs ran on, by name. */
const versions: Record<string, { id: string; number: number }> = {
  goal: { id: '019a1c00-0000-7000-8000-00000000a009', number: 9 },
  sre: { id: '019a1c00-0000-7000-8000-00000000b004', number: 4 },
};

/** A step's run name is the agent copy, "builder-2": the agent and its slot from 0. */
function agentCopy(name: string): { agent: string; slot: number } {
  const m = /^(.+)-(\d+)$/.exec(name);
  return m ? { agent: m[1] ?? name, slot: Number(m[2]) - 1 } : { agent: name, slot: 0 };
}
const repo = 'https://github.com/acme/goal';

/** Who publishes in phase 2.2: the local user. */
const localUser: Actor = { kind: 'user', id: 'local', name: 'Local user' };

/** A published version as a factory names it. */
const published = (id: string, number: number, at: string): VersionRef => ({
  id,
  number,
  published_at: at,
  published_by: localUser,
});

/**
 * What the mock's caller may do (Org.can, Factory.can): every permission of contracts/api/permissions.yaml
 * at the level, sorted, as goald answers the local user and an owner.
 */
export const orgCan = [
  'api_key:manage',
  'api_key:read',
  'audit:read',
  'connection:manage',
  'connection:read',
  'factory:create',
  'invitation:read',
  'knowledge:manage',
  'knowledge:read',
  'member:manage',
  'member:read',
  'org:delete',
  'org:manage',
  'org:read',
  'org:transfer',
  'personal_token:manage',
  'personal_token:read',
];
export const factoryCan = [
  'andon:manage',
  'definition:edit',
  'definition:publish',
  'factory:manage',
  'factory:read',
  'item:create',
  'item:decide',
  'item:operate',
];

/** The organizations' ids. */
export const acmeOrg = '019a1c00-0000-7000-8000-0000000000aa';
export const labsOrg = '019a1c00-0000-7000-8000-0000000000bb';

const goalFactory: Factory = {
  id: '6f0c2a7e-3d1b-4c55-9a7e-1f2b3c4d5e60',
  org_id: acmeOrg,
  can: factoryCan,
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
  current: published('019a1c00-0000-7000-8000-0000000000d8', 8, '2026-10-20T14:32:00Z'),
  draft: {
    id: '019a1c00-0000-7000-8000-0000000000d9',
    revision: 42,
    updated_at: '2026-10-23T14:32:00Z',
    updated_by: localUser,
  },
  paused: false,
  created_at: '2026-09-01T09:00:00Z',
};

const sreFactory: Factory = {
  id: '0b7d9e21-58a4-4f0e-b6c3-2a9d8e7f6a51',
  org_id: acmeOrg,
  can: factoryCan,
  name: 'sre',
  title: 'SRE',
  description: 'Triage, investigate and document production issues and SRE toil.',
  work_types: ['incident', 'change', 'toil'],
  stations: [
    { id: 'inbox', kind: 'queue', description: 'Alerts and requests arrive here' },
    { id: 'triage', kind: 'agent', description: 'Sorts by severity and size' },
    { id: 'investigate', kind: 'agent', description: 'Finds the cause, with evidence' },
    { id: 'approve', kind: 'human', description: 'You' },
    { id: 'done', kind: 'done', description: 'Closed' },
  ],
  current: published('019a1c00-0000-7000-8000-0000000000e3', 3, '2026-10-12T10:00:00Z'),
  draft: null,
  paused: false,
  created_at: '2026-09-15T09:00:00Z',
};

const supportFactory: Factory = {
  id: '5c0d7a52-8b1e-4f43-9d3a-2e6b7c8f9a01',
  org_id: acmeOrg,
  // No item:create here, so the sidebar's New item picker leaves this factory out.
  can: factoryCan.filter((p) => p !== 'item:create'),
  name: 'support',
  title: 'Support',
  description: 'Answers questions from the acme shop, citing runbooks.',
  work_types: ['question'],
  stations: [
    { id: 'inbox', kind: 'queue' },
    { id: 'answer', kind: 'agent' },
    { id: 'approve', kind: 'human' },
    { id: 'done', kind: 'done' },
  ],
  current: published('019a1c00-0000-7000-8000-0000000000f1', 1, '2026-10-18T16:00:00Z'),
  draft: null,
  paused: false,
  created_at: '2026-10-18T15:00:00Z',
};

const researchFactory: Factory = {
  id: '9e8d7c6b-5a49-4382-9172-6a5b4c3d2e1f',
  org_id: labsOrg,
  can: factoryCan,
  name: 'research',
  title: 'Research',
  description: 'Reads papers and writes short notes for the Example Labs team.',
  work_types: ['question'],
  stations: [
    { id: 'inbox', kind: 'queue' },
    { id: 'read', kind: 'agent' },
    { id: 'done', kind: 'done' },
  ],
  current: published('019a1c00-0000-7000-8000-0000000000b1', 2, '2026-10-05T11:00:00Z'),
  draft: null,
  paused: false,
  created_at: '2026-10-01T09:00:00Z',
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
  // The sidebar search's "spend" (shell.jpg s2): items whose titles match it at the Build station.
  {
    number: 147,
    title: "Show this week's spend",
    type: 'feature',
    body: 'The sidebar shows the spend of the week against the budget, with the factories it is spent on.',
    station: 'build',
    state: 'queued',
    createdMin: 95,
    updatedMin: 12,
    steps: [],
  },
  {
    number: 145,
    title: 'Check the spend against the budget',
    type: 'refactor',
    body: 'Compare the week’s spend with each factory’s budget and say which ones are close.',
    station: 'build',
    state: 'queued',
    createdMin: 120,
    updatedMin: 30,
    steps: [],
  },
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
    paths: ['internal/knowledge/sync.go', 'internal/metrics/**'],
    waitsOn: { kind: 'files', number: 13 },
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
    waitsOn: { kind: 'after', number: 16 },
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
    paths: ['internal/knowledge/**', 'internal/knowledge/sync.go'],
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
        // The bars' tokens live in the design repository too: a second change, on GitLab.
        pull_requests: ['https://gitlab.example.com/acme/design-tokens/-/merge_requests/12'],
        branch: 'goal/6',
        summary: 'Added DailyBars with an SVG hatch pattern and labels.',
      },
      review: { decision: 'approve', reason: 'Labels and contrast check out; axe passes at three widths.' },
    },
    steps: [
      { move: 'spec' },
      specRun([['Read', 'docs/design/MASTER.md', 'Read 230 lines']], 0.29, 7),
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
      build: {
        pull_request: `${repo}/pull/36`,
        branch: 'goal/4',
        summary: [
          '## Added goal version',
          '',
          '**What changed**',
          '',
          '- Added `cmd/goal/version.go` with the `version` subcommand, which prints the build version and commit.',
          '- Wired `version` into the command table in `cmd/goal/main.go`, next to `help`.',
          '- Set the version and commit with `-ldflags` in the release build (`Taskfile.yml`, task `build`).',
          '- Kept the default output to one line so scripts can parse it.',
          '',
          '**Migration and port detail**',
          '',
          '1. No migration: the version comes from the build info, not from goald.',
          '2. No port: the command reads `runtime/debug` directly, as `goal up` already does.',
          '3. The commit is `unknown` for a build from a dirty tree, and the tests say so.',
          '4. `goal version --json` prints the same fields for the release checks.',
          '',
          '**Tests**',
          '',
          '- `TestVersionOutput` checks the one-line form and the JSON form.',
          '- `TestVersionUnknownCommit` checks the fallback when build info has no commit.',
          '- The release check reads the printed version and fails on an empty value.',
          '',
          '**Notes for the reviewer**',
          '',
          '> Nothing else changed: no flags were added to `goal up`, and goald did not change.',
          '',
          'The pull request is linked from the item; the spec output lists the one acceptance criterion it meets.',
        ].join('\n'),
      },
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
    number: 5,
    title: 'Rotate RDS certificates before Nov 30',
    type: 'change',
    body: 'The RDS CA certificates expire on Nov 30.',
    station: 'inbox',
    state: 'queued',
    createdMin: 120,
    updatedMin: 120,
    steps: [],
  },
  {
    number: 6,
    title: 'Daily health check, Oct 23',
    type: 'toil',
    body: 'The daily health check of the production accounts.',
    station: 'inbox',
    state: 'queued',
    createdMin: 60,
    updatedMin: 60,
    steps: [],
  },
  {
    number: 7,
    title: 'Alert noise on worker-queue-depth',
    type: 'toil',
    body: 'The worker-queue-depth alert fired 14 times this week without an incident.',
    station: 'inbox',
    state: 'queued',
    createdMin: 40,
    updatedMin: 40,
    steps: [],
  },
  {
    number: 8,
    title: 'billing-svc deploy rolled back twice',
    type: 'incident',
    body: 'The last two billing-svc deploys rolled back on failing health checks.',
    station: 'investigate',
    state: 'in_progress',
    agent: 'investigator',
    createdMin: 60,
    updatedMin: 1,
    steps: [
      { move: 'triage' },
      { run: 'triager', cost: 0.04, turns: 2, tools: [] },
      { move: 'investigate' },
      {
        run: 'investigator-1',
        status: 'running',
        cost: 0.62,
        turns: 22,
        tools: [
          ['Bash', 'kubectl rollout history deploy/billing-svc', '3 revisions'],
          ['Read', 'deploy/billing-svc/health.yaml', 'Read 30 lines'],
          ['Bash', 'kubectl logs deploy/billing-svc --previous', 'readiness probe failed: 503'],
        ],
      },
    ],
  },
  {
    number: 9,
    title: 'S3 spend up 22% month over month',
    type: 'toil',
    body: 'The S3 line of the AWS bill grew 22% this month.',
    station: 'investigate',
    state: 'in_progress',
    agent: 'investigator',
    createdMin: 180,
    updatedMin: 2,
    steps: [
      { move: 'triage' },
      { run: 'triager', cost: 0.03, turns: 2, tools: [] },
      { move: 'investigate' },
      {
        run: 'investigator-3',
        status: 'running',
        cost: 0.35,
        turns: 9,
        tools: [['Bash', 'aws ce get-cost-and-usage --group-by SERVICE', 'S3 $4,210 (+22%)']],
      },
    ],
  },
  {
    number: 10,
    title: 'Upgrade EKS add-ons in staging',
    type: 'change',
    body: 'CoreDNS and kube-proxy lag two minor versions behind in staging.',
    station: 'investigate',
    state: 'queued',
    createdMin: 50,
    updatedMin: 20,
    steps: [{ move: 'triage' }, { run: 'triager', cost: 0.03, turns: 2, tools: [] }, { move: 'investigate' }],
  },
  {
    number: 11,
    title: 'Stale IAM access keys report',
    type: 'toil',
    body: 'List IAM access keys unused for 90 days.',
    station: 'investigate',
    state: 'queued',
    createdMin: 45,
    updatedMin: 15,
    steps: [{ move: 'triage' }, { run: 'triager', cost: 0.02, turns: 1, tools: [] }, { move: 'investigate' }],
  },
  {
    number: 12,
    title: 'Checkout API p95 above 900 ms since 14:05',
    type: 'incident',
    lane: 'sev2',
    body: 'Checkout API latency p95 crossed 900 ms at 14:05 and stays there.',
    station: 'investigate',
    state: 'in_progress',
    expedite: true,
    agent: 'investigator',
    createdMin: 38,
    updatedMin: 1,
    steps: [
      { move: 'triage' },
      { run: 'triager', cost: 0.05, turns: 2, tools: [] },
      { move: 'investigate' },
      {
        run: 'investigator-2',
        status: 'running',
        cost: 1.07,
        turns: 14,
        tools: [
          ['Bash', 'kubectl top pods -l app=checkout', 'checkout-7d9 cpu 940m'],
          ['Read', 'dashboards/checkout.json', 'Read 120 lines'],
        ],
      },
    ],
  },
  {
    number: 13,
    title: 'Redis evictions on session-cache',
    type: 'incident',
    body: 'session-cache evicted keys for 20 minutes after the 09:00 traffic peak.',
    station: 'done',
    state: 'done',
    createdMin: 300,
    updatedMin: 5,
    outputs: { investigate: { summary: 'Postmortem filed' } },
    steps: [
      { move: 'triage' },
      { run: 'triager', cost: 0.04, turns: 2, tools: [] },
      { move: 'investigate' },
      { run: 'investigator-1', cost: 0.88, turns: 17, tools: [['Bash', 'redis-cli info stats', 'evicted_keys:48211']] },
      { move: 'approve' },
      { waiting: true },
      { human: 'approved', by: 'fcruz' },
      { move: 'done' },
      { done: true },
    ],
  },
  {
    number: 4,
    title: 'Raise ALB idle timeout for websockets',
    type: 'change',
    body: 'Websocket clients drop after 60 seconds behind the load balancer.',
    station: 'approve',
    state: 'waiting_human',
    createdMin: 300,
    updatedMin: 200,
    outputs: {
      investigate: {
        summary: 'Idle timeout 60 s; the clients ping every 90 s. Raise it to 120 s.',
        risk: 'Low',
        change: '1 line of Terraform',
      },
    },
    steps: [
      { move: 'triage' },
      { run: 'triager', cost: 0.05, turns: 2, tools: [] },
      { move: 'investigate' },
      { run: 'investigator', cost: 0.41, turns: 9, tools: [['Read', 'alb/listeners.tf', 'Read 40 lines']] },
      { move: 'approve' },
      { waiting: true },
    ],
  },
  {
    number: 1,
    title: 'Rotate the staging TLS certificate',
    type: 'toil',
    body: 'The staging certificate expires in nine days.',
    station: 'approve',
    state: 'waiting_human',
    createdMin: 1500,
    updatedMin: 180,
    outputs: { investigate: { summary: 'Renewal request drafted for staging.example.com.' } },
    steps: [
      { move: 'triage' },
      { run: 'triager', cost: 0.04, turns: 2, tools: [] },
      { move: 'investigate' },
      { run: 'investigator', cost: 0.22, turns: 5, tools: [['Bash', 'openssl x509 -enddate', 'notAfter in 9 days']] },
      { move: 'approve' },
      { waiting: true },
    ],
  },
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
      ...(seed.paths ? { paths: seed.paths } : {}),
      ...(seed.waitsOn ? { waitsOn: seed.waitsOn } : {}),
      spans: [],
    };

    if (seed.state === 'done') item.done_at = item.updated_at;

    // Spread the history evenly over the item's lifetime.
    const ticks =
      1 + seed.steps.reduce((n, s) => n + ('run' in s ? 2 + s.tools.length * 2 + (s.message ? 1 : 0) : 1), 0);
    const dt = ticks > 1 ? (updated - created) / (ticks - 1) : 0;
    let at = created;
    let station = first;
    const stationKinds = new Map(factory.stations.map((s) => [s.id, s.kind]));
    const place = (kind: string): ItemState | undefined => {
      if (kind === 'created' || kind === 'moved') {
        const k = stationKinds.get(station);
        return k === 'human' ? 'waiting_human' : k === 'done' ? 'done' : 'queued';
      }
      return ({ claimed: 'in_progress', waiting_human: 'waiting_human', blocked: 'blocked', done: 'done' } as const)[
        kind as 'claimed'
      ];
    };
    const push = (kind: string, actor: string, data: Record<string, unknown> = {}, run?: string) => {
      const placed = place(kind);
      if (placed)
        trackSpan(item.spans, { station, state: placed, rework_count: 0 }, new Date(Math.round(at)).toISOString());
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
        const { agent, slot } = agentCopy(step.run);
        push('claimed', 'engine', { slot, agent });
        const startedAt = new Date(Math.round(at)).toISOString();
        push('run_started', agent, {}, run);
        push('agent_started', agent, { model, session_id: run }, run);
        step.tools.forEach(([tool, input, summary, isError], i) => {
          push('tool_call', agent, { id: `t${i}`, tool, input: JSON.stringify({ arg: input }) }, run);
          push('tool_result', agent, { id: `t${i}`, is_error: Boolean(isError), summary }, run);
        });
        if (step.message) push('agent_message', agent, { text: step.message }, run);
        item.cost_usd += step.cost;
        const status = step.status ?? 'succeeded';
        const version = versions[factory.name] ?? null;
        state.runs.push({
          id: run,
          item_id: id,
          station,
          agent,
          model,
          status,
          turns: status === 'running' ? 0 : step.turns,
          input_tokens: step.turns * 4_000,
          output_tokens: step.turns * 600,
          cost_usd: status === 'running' ? 0 : step.cost,
          error: step.error ?? null,
          started_at: startedAt,
          ended_at: status === 'running' ? null : new Date(Math.round(at)).toISOString(),
          definition_version_id: version?.id ?? null,
          definition_version: version?.number ?? null,
        });
        if (status !== 'running') {
          push('run_finished', agent, { status, error: step.error ?? '', cost_usd: step.cost, turns: step.turns }, run);
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
    // The history is illustrative; the item's row is the truth for where it stands now.
    trackSpan(item.spans, item, item.updated_at);
    state.items.push(item);
  }
}

export function seed(now = Date.now()): MockState {
  const created = new Date(now - 90 * 86_400_000).toISOString();
  const state: MockState = {
    orgs: [
      { id: acmeOrg, slug: 'acme', name: 'Acme', mark: 'Ac', factory_count: 0, created_at: created, can: orgCan },
      {
        id: labsOrg,
        slug: 'example-labs',
        name: 'Example Labs',
        mark: 'EL',
        factory_count: 0,
        created_at: created,
        can: orgCan,
      },
    ],
    factories: [goalFactory, sreFactory, supportFactory, researchFactory].map((f) => structuredClone(f)),
    people: seedPeople(now, {
      acme: acmeOrg,
      labs: labsOrg,
      factories: { goal: goalFactory.id, sre: sreFactory.id, support: supportFactory.id },
    }),
    // Acme shares 6 slots, at most 3 for goal and 2 for sre (Org-Settings board); labs has no caps.
    slots: {
      [acmeOrg]: { max: 6, factories: { [goalFactory.id]: 3, [sreFactory.id]: 2 } },
      [labsOrg]: { max: null, factories: {} },
    },
    audit: [],
    nextAuditId: 1,
    stations: {
      goal: {
        inbox: { agents: [], wip_limit: null },
        spec: { agents: ['spec-writer'], wip_limit: 2, output: 'spec', checks: ['schema'] },
        'approve-spec': { agents: [], wip_limit: null },
        build: {
          agents: ['builder-1', 'builder-2'],
          wip_limit: 2,
          copies: 2,
          output: 'change',
          checks: ['schema', 'task check'],
        },
        review: { agents: ['reviewer'], wip_limit: 1 },
        merge: { agents: [], wip_limit: null },
        done: { agents: [], wip_limit: null },
      },
      sre: {
        inbox: { agents: [], wip_limit: null },
        triage: { agents: ['triager'], wip_limit: 4 },
        investigate: {
          agents: ['investigator'],
          wip_limit: 3,
          copies: 3,
          buffer: 2,
          output: 'diagnosis',
          checks: ['schema', '2+ evidence links'],
        },
        approve: { agents: [], wip_limit: 5 },
        done: { agents: [], wip_limit: null },
      },
      support: {
        inbox: { agents: [], wip_limit: null },
        answer: { agents: ['responder'], wip_limit: 2 },
        approve: { agents: [], wip_limit: null },
        done: { agents: [], wip_limit: null },
      },
      research: {
        inbox: { agents: [], wip_limit: null },
        read: { agents: ['reader'], wip_limit: 1 },
        done: { agents: [], wip_limit: null },
      },
    },
    constraint: { goal: 'build', sre: 'investigate', support: null, research: null },
    stationStats: {
      goal: {
        spec: { busy_ratio: 0.41, queue_trend: 'steady' },
        build: { busy_ratio: 0.88, queue_trend: 'growing' },
        review: { busy_ratio: 0.3, send_back_ratio: 0.23 },
      },
      sre: {
        triage: { busy_ratio: 0.38, queue_trend: 'steady' },
        investigate: { busy_ratio: 0.94, queue_trend: 'growing' },
        inbox: { queue_trend: 'growing' },
      },
    },
    history: {},
    andon: {
      support: [
        {
          id: 1,
          kind: 'manual',
          station: null,
          reason: 'Answers cite a retired runbook',
          pulled_by: 'user:local',
          pulled_at: new Date(now - 3 * 86_400_000).toISOString(),
          cleared_by: 'user:local',
          cleared_at: new Date(now - 3 * 86_400_000 + 40 * 60_000).toISOString(),
          note: 'Removed the runbook from the knowledge base',
          data: {},
        },
        {
          id: 2,
          kind: 'manual',
          station: null,
          reason: 'Answers sent back 4 of the last 10 times',
          pulled_by: 'user:local',
          pulled_at: new Date(now - 35 * 60_000).toISOString(),
          cleared_by: null,
          cleared_at: null,
          note: null,
          data: {},
        },
      ],
    },
    nextAndonId: 3,
    gates: {
      goal: {
        policy: { rework_rate: 0.4, window: 10 },
        tallies: [{ station: 'review', verdicts: 10, rejected: 3, top_reason: 'missing tests', warning: true }],
      },
    },
    flow: {
      goal: {
        throughput_per_day: 3,
        lead_time: 6 * 3600,
        expedite_lead_time: 3600,
        per_item_usd: 4,
        max_turns_per_task: 80,
        expedite_wip: null,
      },
      sre: {
        throughput_per_day: 8,
        lead_time: 6 * 3600,
        expedite_lead_time: 3600,
        per_item_usd: 2,
        max_turns_per_task: 40,
        release: 'drum-buffer-rope',
        expedite_wip: 1,
      },
    },
    // The sidebar's spend (shell.jpg): $512 of $1,400 this week, the budgets of goal and sre.
    weeklyBudget: { goal: 1000, sre: 400 },
    agentStats: {
      sre: {
        investigator: {
          model,
          tasks_finished: 31,
          sent_back: { count: 7, ratio: 0.22 },
          median_turns: 18,
          spend_per_task_usd: 0.94,
          task_time_p85_minutes: 24,
          spend_usd: 29.14,
          reasons: [
            { label: 'No CloudWatch evidence for a latency claim', count: 4 },
            { label: 'Did not rule out a recent deploy', count: 2 },
            { label: 'Risk marked low on a production database change', count: 1 },
          ],
          denied_tool_calls: { count: 2, tools: [{ tool: 'aws ssm put-parameter', count: 2 }] },
        },
      },
      goal: {
        builder: {
          model,
          tasks_finished: 12,
          sent_back: { count: 2, ratio: 0.17 },
          median_turns: 26,
          spend_per_task_usd: 1.42,
          task_time_p85_minutes: 95,
          spend_usd: 17.04,
          reasons: [
            { label: 'Bars rely on colour alone for today; add a text label.', count: 1 },
            { label: 'no test for the reconnect', count: 1 },
          ],
          denied_tool_calls: { count: 0, tools: [] },
        },
      },
    },
    items: [],
    events: [],
    nextEventId: 1,
    touchReasons: {},
    runs: [],
    definitions: seedDefinitions(),
    templates: seedTemplates(),
    skills: seedSkills(),
    faults: [],
  };

  const localUser = { kind: 'user', id: 'local', name: 'Local user' } as const;
  for (const org of state.orgs) {
    state.audit.push({
      org: org.id,
      entry: {
        id: state.nextAuditId++,
        at: created,
        actor: localUser,
        action: 'org.created',
        target: { kind: 'org', id: org.id },
        data: { slug: org.slug, name: org.name },
      },
    });
  }
  state.audit.push({
    org: acmeOrg,
    entry: {
      id: state.nextAuditId++,
      at: new Date(now - 30 * 86_400_000).toISOString(),
      actor: localUser,
      action: 'org.updated',
      target: { kind: 'org', id: acmeOrg },
      data: {
        max_slots: { from: null, to: 6 },
        factory_slots: { [goalFactory.id]: { from: null, to: 3 }, [sreFactory.id]: { from: null, to: 2 } },
      },
    },
  });

  build(goalFactory, 0, goalSeeds, now, state);
  build(sreFactory, 1, sreSeeds, now, state);

  for (const f of state.factories) state.history[f.name] = seedHistory(f, now, state.constraint[f.name] ?? null);
  fitWeeklySpend(state, ['goal', 'sre', 'support'], now, 512);

  return state;
}

/**
 * Acme's last seven days cost $512 (the sidebar's picture, shell.jpg). Scales the cost of the finished
 * work of its factories in that window, the seeded history and the finished items, so they add up to
 * exactly `target`, the last one taking the rounding. The overview reads the same window: from six days
 * before today (UTC) up to now.
 */
function fitWeeklySpend(state: MockState, names: string[], now: number, target: number) {
  const day = 86_400_000;
  const today = Math.floor(now / day) * day;
  const inWeek = (at: number) => at >= today - 6 * day && at < now;
  const week: { cost_usd: number }[] = [
    ...names.flatMap((name) => (state.history[name] ?? []).filter((d) => inWeek(d.done_at))),
    ...state.items.filter((it) => names.includes(it.factory) && it.done_at && inWeek(Date.parse(it.done_at))),
  ];
  const total = week.reduce((n, d) => n + d.cost_usd, 0);
  if (week.length === 0 || total === 0) return;
  const cents = (n: number) => Math.round(n * 100);
  let sum = 0;
  for (const d of week) {
    d.cost_usd = Math.round(((d.cost_usd * target) / total) * 100) / 100;
    sum += cents(d.cost_usd);
  }
  const last = week[week.length - 1];
  if (last) last.cost_usd = (cents(last.cost_usd) + cents(target) - sum) / 100;
}
