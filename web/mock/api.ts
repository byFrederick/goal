// An in-memory stand-in for goald's /v1 API, typed with the contract's shapes from
// @goal/api-client, mounted into the Vite dev and preview servers when GOAL_API=mock
// (mock/plugin.ts). Like goald it serves /v1/orgs and every other route under
// /v1/orgs/{org} (slug or id; unknown is 404), among them the shell's attention, overview and
// inbox, keeps state per process and broadcasts each organization's item events on its
// /v1/orgs/{org}/stream. Decisions are the local user's: every decision waits on you. The
// definitions and builder routes are definitions.ts's. POST /__mock/reset restores the seed;
// POST /__mock/fail injects faults (faults.ts); an X-Mock-Latency-Ms header delays the answer; a
// reset's {now} fixes the mock's clock until the next reset.
import type { IncomingMessage, ServerResponse } from 'node:http';
import type {
  AgentStats,
  AndonPull,
  AndonState,
  ApprovalBrief,
  AuditPage,
  Board,
  BoardStation,
  ConstraintReport,
  Factory,
  FactoryAttention,
  FactoryEvent,
  FactoryOverview,
  FactoryOverviewList,
  Inbox,
  InboxEntry,
  Item,
  ItemCard,
  ItemEvent,
  ItemFlow,
  ItemPage,
  NoteReceipt,
  Org,
  OrgAttention,
  OrgSettings,
  RunList,
  StreamEvent,
} from '@goal/api-client';
import { createDefinitions } from './definitions.ts';
import { parseFault, sleep, takeFault } from './faults.ts';
import { type MockAgentStats, type MockItem, type MockSlots, type MockState, seed, trackSpan } from './fixtures.ts';
import { constraintOf, type DayRange, type FlowInput, leadTimeOf, metricsOf, rangeOf, weeklyOf } from './flow.ts';

type Next = (err?: unknown) => void;

interface Subscriber {
  org: string;
  factory: string;
  res: ServerResponse;
}

/** Who decides: goald's request actor in phase 2.2. */
const localUser = 'Local user';

/** How long goald keeps a deleted organization by default (GOAL_ORG_RETENTION). */
const retentionMs = 7 * 86_400_000;

/** The slug rule and reserved words of POST /v1/orgs. */
const slugRule = /^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$/;
const reserved = ['new', 'settings', 'api', 'v1', 'o', 'me', 'orgs', 'auth', 'invite', 'admin', 'default'];

/** The metrics window parameter: the last N UTC days, today included (1d to 90d). */
const windowRule = /^([1-9][0-9]?)d$/;
const dayMs = 86_400_000;

/** The UTC date `offset` days before `now`'s day. */
const utcDay = (offset: number, now: number) => new Date(now - offset * dayMs).toISOString().slice(0, 10);

const cents = (usd: number) => Math.round(usd * 100) / 100;

export interface MockApi {
  handle: (req: IncomingMessage, res: ServerResponse, next: Next) => void;
  close: () => void;
}

const json = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const problem = (res: ServerResponse, status: number, title: string, detail?: string, code?: string) =>
  json(res, status, { title, status, ...(detail ? { detail } : {}), ...(code ? { code } : {}) });

/** A slot cap of a request: null clears it; otherwise 1 to 1000. */
const isCap = (v: unknown): v is number | null =>
  v === null || (Number.isInteger(v) && Number(v) >= 1 && Number(v) <= 1000);

async function readBody(req: IncomingMessage): Promise<Record<string, unknown>> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) chunks.push(chunk as Buffer);
  if (chunks.length === 0) return {};
  try {
    return JSON.parse(Buffer.concat(chunks).toString('utf8')) as Record<string, unknown>;
  } catch {
    return {};
  }
}

export function createMockApi(options: { live?: boolean } = {}): MockApi {
  // The mock's clock: real time, or the instant a reset fixed (POST /__mock/reset {now}), so a test
  // and the page it drives count days from the same day even across UTC midnight.
  let fixed: number | undefined;
  const clock = () => fixed ?? Date.now();
  let state: MockState = seed(clock());
  const subscribers = new Set<Subscriber>();
  const definitions = createDefinitions({
    state: () => state,
    broadcast: (org, event, data) => {
      for (const sub of subscribers) {
        if (sub.org === org) sub.res.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
      }
    },
  });

  const ref = (it: MockItem) => `${it.factory}#${it.number}`;

  /**
   * The item's run in progress: its last run without a run_finished event while it is in
   * progress, with the slot of its last claim (0-based) and its turns (each tool call stands for
   * one; the mock's agents report no usage).
   */
  const liveRun = (it: MockItem) => {
    if (it.state !== 'in_progress' || !it.agent) return null;
    const events = state.events.filter((e) => e.item_id === it.id);
    const started = events.findLast((e) => e.kind === 'run_started' && e.run_id);
    const runId = started?.run_id;
    if (!started || !runId || events.some((e) => e.run_id === runId && e.kind === 'run_finished')) return null;
    const slot = events.findLast((e) => e.kind === 'claimed')?.data.slot;
    return {
      id: runId,
      started,
      events,
      slot: typeof slot === 'number' ? slot : 0,
      turn: events.filter((e) => e.run_id === runId && e.kind === 'tool_call').length,
    };
  };

  /** The summary of the item's latest station output that has one, in line order. */
  const summaryOf = (it: MockItem): string | null => {
    for (const s of [...stationsOf(it)].reverse()) {
      const out = it.outputs[s.id] as { summary?: unknown } | undefined;
      if (typeof out?.summary === 'string' && out.summary.trim()) return out.summary.trim();
    }
    return null;
  };

  const card = (it: MockItem): ItemCard => {
    const run = liveRun(it);
    const copies = state.stations[it.factory]?.[it.station]?.copies ?? 1;
    return {
      id: it.id,
      ref: ref(it),
      number: it.number,
      title: it.title,
      type: it.type,
      ...(it.lane ? { lane: it.lane } : {}),
      station: it.station,
      state: it.state,
      class_of_service: it.class_of_service,
      rework_count: it.rework_count,
      cost_usd: it.cost_usd,
      created_at: it.created_at,
      updated_at: it.updated_at,
      age_seconds: Math.round((clock() - Date.parse(it.created_at)) / 1000),
      agent: it.agent,
      pr_url: it.pr_url,
      slot: run && copies > 1 ? run.slot + 1 : null,
      turn: run ? run.turn : null,
      summary: summaryOf(it),
      in_buffer: (it.state === 'queued' || it.state === 'ready') && it.station === state.constraint[it.factory],
      rope_held: false,
    };
  };

  const full = (it: MockItem): Item => {
    const factory = state.factories.find((f) => f.name === it.factory);
    const { agent: _agent, factory: _factory, pr_url: _pr, spans: _spans, lane, ...rest } = it;
    return { ...rest, ...(lane ? { lane } : {}), ref: ref(it), factory_id: factory?.id ?? '' };
  };

  /** Item.flow, as GET of one item has it: spans, target, send-back station, budget, run in progress. */
  const flowOf = (it: MockItem): ItemFlow => {
    const stations = stationsOf(it);
    const index = stations.findIndex((s) => s.id === it.station);
    const config = state.stations[it.factory]?.[it.station];
    const flow = state.flow[it.factory];
    const open = it.state !== 'done' && it.state !== 'cancelled';
    const back = stations
      .slice(0, Math.max(0, index))
      .reverse()
      .find((s) => s.kind === 'agent');
    const target = it.class_of_service === 'expedite' ? flow?.expedite_lead_time : flow?.lead_time;

    // The run in progress, under its agent.
    let current: ItemFlow['current_run'] = null;
    const run = liveRun(it);
    if (run) {
      const model = run.events.find((e) => e.run_id === run.id && e.kind === 'agent_started')?.data.model;
      current = {
        id: run.id,
        agent: run.started.actor,
        slot: run.slot + 1,
        model: typeof model === 'string' ? model : 'claude-sonnet-5-5',
        turn: run.turn,
        started_at: run.started.at,
        output_contract: config?.output ?? null,
        checks: config?.checks ?? [],
      };
    }

    return {
      spans: it.spans.map((s) => ({ ...s })),
      target_seconds: target ?? null,
      rework_target: open ? (back?.id ?? null) : null,
      budget: { per_item_usd: flow?.per_item_usd ?? null, max_turns_per_task: flow?.max_turns_per_task ?? null },
      current_run: current,
    };
  };

  const findOrg = (key: string) => state.orgs.find((o) => o.slug === key || o.id === key);

  const withCount = (o: Org): Org => ({
    ...o,
    factory_count: state.factories.filter((f) => f.org_id === o.id).length,
  });

  /** A factory by id or name; within org when given. Names are unique across the mock's orgs. */
  const findFactory = (key: string, org?: string) =>
    state.factories.find((f) => (f.id === key || f.name === key) && (org === undefined || f.org_id === org));

  const findItem = (key: string, org: string) => {
    const [name, num] = key.split('#');
    if (num !== undefined) {
      const factory = findFactory(name ?? '', org);
      return state.items.find((it) => it.factory === factory?.name && it.number === Number(num));
    }
    return state.items.find((it) => it.id === key && findFactory(it.factory)?.org_id === org);
  };

  const settingsOf = (o: Org): OrgSettings => {
    const slots = state.slots[o.id] ?? { max: null, factories: {} };
    const factories = state.factories
      .filter((f) => f.org_id === o.id)
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((f) => ({ factory_id: f.id, name: f.name, max: slots.factories[f.id] ?? null }));
    return { name: o.name, slug: o.slug, mark: o.mark, slots: { max: slots.max, factories } };
  };

  const audit = (org: Org, action: string, data: Record<string, unknown>) => {
    state.audit.push({
      org: org.id,
      entry: {
        id: state.nextAuditId++,
        at: new Date(clock()).toISOString(),
        actor: { kind: 'user', id: 'local', name: localUser },
        action,
        target: { kind: 'org', id: org.id },
        data,
      },
    });
  };

  /** PATCH /settings as goald applies it: every rule checked first, then one org.updated. */
  const patchSettings = (org: Org, body: Record<string, unknown>, res: ServerResponse) => {
    const changed: Record<string, unknown> = {};
    const next = { ...org };
    for (const field of ['name', 'slug', 'mark'] as const) {
      const raw = body[field];
      if (raw === undefined) continue;
      const to = typeof raw === 'string' ? (field === 'name' ? raw.trim() : raw) : '';
      const ok =
        field === 'name'
          ? to.length >= 1 && to.length <= 80
          : field === 'mark'
            ? to.length >= 1 && to.length <= 2
            : slugRule.test(to) && !reserved.includes(to);
      if (!ok) return problem(res, 409, 'Conflict', `${field} "${to}" breaks the organization rules`);
      if (field === 'slug' && to !== org.slug && findOrg(to)) {
        return problem(res, 409, 'Conflict', `slug "${to}" is taken`, 'slug-taken');
      }
      if (to !== org[field]) {
        changed[field] = { from: org[field], to };
        next[field] = to;
      }
    }

    const current = state.slots[org.id] ?? { max: null, factories: {} };
    const slots: MockSlots = { max: current.max, factories: { ...current.factories } };
    const patch = body.slots as { max?: unknown; factories?: { factory_id?: unknown; max?: unknown }[] } | undefined;
    if (patch !== undefined) {
      const max = patch.max ?? null;
      if (!isCap(max)) return problem(res, 409, 'Conflict', 'a slot cap is 1 to 1000 agents');
      if (max !== current.max) changed.max_slots = { from: current.max, to: max };
      slots.max = max;
      const factories: Record<string, unknown> = {};
      for (const f of patch.factories ?? []) {
        const factory = findFactory(String(f.factory_id ?? ''), org.id);
        if (!factory) return problem(res, 404, 'Not Found', `factory ${String(f.factory_id)}`);
        const cap = f.max ?? null;
        if (!isCap(cap)) return problem(res, 409, 'Conflict', 'a slot cap is 1 to 1000 agents');
        const from = current.factories[factory.id] ?? null;
        if (cap !== from) factories[factory.id] = { from, to: cap };
        slots.factories[factory.id] = cap;
      }
      if (Object.keys(factories).length > 0) changed.factory_slots = factories;
    }

    if (Object.keys(changed).length > 0) {
      Object.assign(org, next);
      state.slots[org.id] = slots;
      audit(org, 'org.updated', changed);
    }
    return json(res, 200, settingsOf(org));
  };

  const emit = (it: MockItem, kind: string, actor: string, data: Record<string, unknown> = {}) => {
    const at = new Date(clock()).toISOString();
    const event: ItemEvent = { id: state.nextEventId++, item_id: it.id, at, kind, station: it.station, actor, data };
    state.events.push(event);
    it.updated_at = at;
    trackSpan(it.spans, it, at);

    const factory = findFactory(it.factory);
    const payload: StreamEvent = {
      org_id: factory?.org_id ?? '',
      item_id: it.id,
      ref: ref(it),
      factory_id: factory?.id ?? '',
      kind,
      station: it.station,
      state: it.state,
      at,
    };
    for (const sub of subscribers) {
      const sameOrg = sub.org === factory?.org_id;
      if (sameOrg && (sub.factory === '' || sub.factory === factory?.id || sub.factory === factory?.name)) {
        sub.res.write(`event: item\nid: ${event.id}\ndata: ${JSON.stringify(payload)}\n\n`);
      }
    }
  };

  const moveTo = (it: MockItem, to: string, actor: string, data: Record<string, unknown> = {}) => {
    const from = it.station;
    it.station = to;
    const kind = findFactory(it.factory)?.stations.find((s) => s.id === to)?.kind;
    it.state = kind === 'done' ? 'done' : kind === 'human' ? 'waiting_human' : 'queued';
    it.agent = null;
    if (it.state === 'done') it.done_at = new Date(clock()).toISOString();
    emit(it, 'moved', actor, { from, to, ...data });
    if (it.state === 'waiting_human') emit(it, 'waiting_human', 'engine');
    if (it.state === 'done') emit(it, 'done', 'engine');
  };

  const stationsOf = (it: MockItem) => findFactory(it.factory)?.stations ?? [];

  /** The organization's factories by name, as goald lists them. */
  const factoriesOf = (org: string) =>
    state.factories.filter((f) => f.org_id === org).sort((a, b) => a.name.localeCompare(b.name));

  const inState = (f: Factory, st: string) => state.items.filter((it) => it.factory === f.name && it.state === st);

  /** The factory's open andon pull, or null while its line runs. */
  const openAndon = (f: Factory): AndonPull | null => {
    const last = state.andon[f.name]?.at(-1);
    return last && last.cleared_at === null ? last : null;
  };

  /** A pull or a clear: its audit entry and the organization's `event: factory`, as goald's trigger sends it. */
  const andonChanged = (f: Factory, pull: AndonPull) => {
    const cleared = pull.cleared_at !== null;
    state.audit.push({
      org: f.org_id,
      entry: {
        id: state.nextAuditId++,
        at: pull.cleared_at ?? pull.pulled_at,
        actor: { kind: 'user', id: 'local', name: localUser },
        action: cleared ? 'andon.cleared' : 'andon.pulled',
        target: { kind: 'factory', id: f.id },
        factory_id: f.id,
        data: {
          pull: String(pull.id),
          kind: pull.kind,
          reason: pull.reason,
          ...(cleared ? { note: pull.note ?? '' } : {}),
        },
      },
    });
    const event: FactoryEvent = {
      org_id: f.org_id,
      factory_id: f.id,
      kind: cleared ? 'andon_cleared' : 'andon_pulled',
      andon_id: pull.id,
      at: pull.cleared_at ?? pull.pulled_at,
    };
    for (const sub of subscribers) {
      if (sub.org === f.org_id && (sub.factory === '' || sub.factory === f.id || sub.factory === f.name)) {
        sub.res.write(`event: factory\ndata: ${JSON.stringify(event)}\n\n`);
      }
    }
  };

  /** GET, POST …/andon and POST …/andon/clear, with goald's limits and refusals. */
  const andon = async (
    f: Factory,
    clear: boolean,
    method: string,
    url: URL,
    req: IncomingMessage,
    res: ServerResponse,
  ) => {
    const pulls = state.andon[f.name] ?? [];
    if (!clear && method === 'GET') {
      const limit = Number(url.searchParams.get('limit') ?? 10);
      if (!Number.isInteger(limit) || limit < 1 || limit > 50)
        return problem(res, 400, 'Bad Request', 'limit is 1 to 50');
      const gates = state.gates[f.name];
      const body: AndonState = {
        open: openAndon(f),
        recent: pulls.slice(-limit).reverse(),
        policy: gates?.policy ?? null,
        gates: gates?.tallies ?? [],
      };
      return json(res, 200, body);
    }
    if (method !== 'POST') return problem(res, 405, 'Method not allowed');

    const body = await readBody(req);
    const now = new Date(clock()).toISOString();
    const open = openAndon(f);
    if (clear) {
      const note = typeof body.note === 'string' ? body.note.trim() : '';
      if ([...note].length > 2000) return problem(res, 400, 'Bad Request', 'a note is at most 2000 characters');
      if (!open) return problem(res, 409, 'Conflict', 'the andon cord is not pulled', 'andon-not-pulled');
      Object.assign(open, { cleared_by: 'user:local', cleared_at: now, note: note || null });
      andonChanged(f, open);
      return json(res, 200, open);
    }

    const reason = typeof body.reason === 'string' ? body.reason.trim() : '';
    if (!reason || [...reason].length > 500)
      return problem(res, 400, 'Bad Request', 'the reason is 1 to 500 characters');
    if (open) return problem(res, 409, 'Conflict', 'the andon cord is already pulled', 'andon-pulled');
    const pull: AndonPull = {
      id: state.nextAndonId++,
      kind: 'manual',
      station: null,
      reason,
      pulled_by: 'user:local',
      pulled_at: now,
      cleared_by: null,
      cleared_at: null,
      note: null,
      data: {},
    };
    state.andon[f.name] = [...pulls, pull];
    andonChanged(f, pull);
    return json(res, 201, pull);
  };

  const attentionOf = (f: Factory): FactoryAttention => ({
    factory_id: f.id,
    waiting_on_you: inState(f, 'waiting_human').length,
    blocked_items: inState(f, 'blocked').length,
    andon: openAndon(f),
    connections_blocked: 0,
  });

  /** What a factory's flow metrics are computed from, for items of `type` ("" for all). */
  const flowInput = (f: Factory, type = ''): FlowInput => ({
    history: state.history[f.name] ?? [],
    items: state.items.filter((it) => it.factory === f.name),
    flow: state.flow[f.name],
    drum: state.constraint[f.name] ?? null,
    type,
    now: clock(),
  });

  const overviewOf = (f: Factory, days: number): FactoryOverview => {
    const today = Date.parse(`${utcDay(0, clock())}T00:00:00Z`);
    const metrics = metricsOf(flowInput(f), { from: today - (days - 1) * dayMs, to: today }, true);
    const waiting = inState(f, 'waiting_human');
    const types = [...new Set(waiting.map((it) => it.type))].sort();
    const andon = openAndon(f);
    const constraint = state.constraint[f.name] ?? null;
    const weekly = state.weeklyBudget[f.name] ?? null;
    return {
      factory: f,
      // Never published: a draft goald does not run (the wizard resumes it from the Factories home).
      health: f.current === null ? 'draft' : andon ? 'stopped' : 'running',
      andon,
      throughput: {
        per_day: metrics.throughput_per_day,
        previous_per_day: metrics.previous?.throughput_per_day ?? 0,
        days: metrics.throughput,
      },
      constraint: constraint
        ? { station: constraint, busy: state.stationStats[f.name]?.[constraint]?.busy_ratio ?? null }
        : null,
      waiting: {
        on_you: waiting.length,
        items_by_type: types.map((type) => ({ type, count: waiting.filter((it) => it.type === type).length })),
        tool_approvals: [],
      },
      spend_usd: metrics.spend_usd,
      budget_usd: weekly === null ? null : cents((weekly * days) / 7),
    };
  };

  /** An entry of kind about `what` of factory f, waiting since. */
  const entry = (f: Factory, kind: InboxEntry['kind'], what: string, since: string): InboxEntry => ({
    id: `${kind}:${what}`,
    kind,
    factory: { id: f.id, name: f.name, title: f.title ?? null },
    since,
    item: null,
    station: null,
    action: null,
    approval: null,
    blocked: null,
    andon: null,
    rework: null,
  });

  /** The brief of a decision, from the output of the latest station that has one (as goald builds it). */
  const briefOf = (it: MockItem, f: Factory): ApprovalBrief => {
    const out = f.stations
      .map((s) => it.outputs[s.id])
      .findLast((o): o is Record<string, unknown> => typeof o === 'object' && o !== null && !Array.isArray(o));
    const skipped = ['summary', 'pull_request', 'lane', 'decision', 'reason', 'category'];
    const facts = Object.keys(out ?? {})
      .sort()
      .filter((key) => !skipped.includes(key))
      .flatMap((key) => {
        const v = out?.[key];
        if (typeof v === 'string' && v.trim() !== '' && [...v.trim()].length <= 120) return [{ key, value: v.trim() }];
        if (typeof v === 'number') return [{ key, value: String(v) }];
        if (typeof v === 'boolean') return [{ key, value: v ? 'yes' : 'no' }];
        return [];
      })
      .slice(0, 4);
    const summary = typeof out?.summary === 'string' ? out.summary.trim() : '';
    return { summary: summary || null, facts, pull_request: it.pr_url, spend_usd: it.cost_usd };
  };

  /** Why a blocked item is blocked: its latest blocked or paused event. */
  const blockerOf = (it: MockItem): InboxEntry['blocked'] => {
    const last = state.events.findLast((e) => e.item_id === it.id && (e.kind === 'blocked' || e.kind === 'paused'));
    const reason = typeof last?.data.reason === 'string' && last.data.reason ? last.data.reason : null;
    return { reason, paused: last?.kind === 'paused' };
  };

  /** One factory's inbox entries, as goald's work source makes them (E §8.4). */
  const inboxOf = (f: Factory): InboxEntry[] => {
    const open = (it: MockItem) => it.state !== 'done' && it.state !== 'cancelled';
    const items = state.items
      .filter((it) => it.factory === f.name)
      .flatMap((it): InboxEntry[] => {
        const base = { ...entry(f, 'expedite', it.id, it.created_at), item: card(it), station: it.station };
        if (it.state === 'waiting_human') {
          return [
            {
              ...base,
              id: `decision:${it.id}`,
              kind: 'decision',
              since: it.updated_at,
              action: f.stations.find((s) => s.id === it.station)?.action ?? null,
              approval: briefOf(it, f),
            },
          ];
        }
        if (it.state === 'blocked') {
          return [{ ...base, id: `blocked:${it.id}`, kind: 'blocked', since: it.updated_at, blocked: blockerOf(it) }];
        }
        return it.class_of_service === 'expedite' && open(it) ? [base] : [];
      });

    const pull = openAndon(f);
    if (pull) return [...items, { ...entry(f, 'andon', String(pull.id), pull.pulled_at), andon: pull }];

    const gates = state.gates[f.name];
    const now = new Date(clock()).toISOString();
    const warnings = (gates?.tallies ?? []).flatMap((t): InboxEntry[] =>
      t.warning && gates?.policy
        ? [
            {
              ...entry(f, 'rework_warning', `${f.id}:${t.station}`, now),
              station: t.station,
              rework: {
                station: t.station,
                rejected: t.rejected,
                verdicts: t.verdicts,
                window: gates.policy.window,
                threshold: gates.policy.rework_rate,
                top_reason: t.top_reason,
              },
            },
          ]
        : [],
    );
    return [...items, ...warnings];
  };

  const kindOrder: InboxEntry['kind'][] = ['decision', 'andon', 'rework_warning', 'blocked', 'expedite'];

  /**
   * A board station's statistics: the seeded busy and send-back shares and queue trend, how long
   * the oldest waiting item has waited (its open span), the items done today (UTC).
   */
  const stationStats = (
    factory: string,
    s: { id: string; kind: BoardStation['kind'] },
    here: MockItem[],
  ): Pick<BoardStation, 'busy_ratio' | 'send_back_ratio' | 'queue_trend' | 'oldest_waiting_seconds' | 'done_today'> => {
    const seeded = state.stationStats[factory]?.[s.id];
    const waitingSince = here
      .filter((it) => it.state === 'queued' || it.state === 'ready')
      .map((it) => Date.parse(it.spans.findLast((sp) => sp.left_at === null)?.entered_at ?? it.updated_at));
    const today = new Date(clock()).toISOString().slice(0, 10);
    const queues = s.kind === 'queue' || s.kind === 'agent';
    return {
      busy_ratio: s.kind === 'agent' ? (seeded?.busy_ratio ?? null) : null,
      send_back_ratio: seeded?.send_back_ratio ?? null,
      queue_trend: queues ? (seeded?.queue_trend ?? 'steady') : null,
      oldest_waiting_seconds:
        queues && waitingSince.length > 0 ? Math.round((clock() - Math.min(...waitingSince)) / 1000) : null,
      done_today:
        s.kind === 'done' ? here.filter((it) => it.state === 'done' && it.done_at?.startsWith(today)).length : null,
    };
  };

  /** Detected when the seeded statistics measured the constraint over time, else the snapshot. */
  const constraintSourceOf = (f: Factory): Board['constraint_source'] => {
    const c = state.constraint[f.name];
    return c && state.stationStats[f.name]?.[c]?.busy_ratio !== undefined ? 'detected' : 'snapshot';
  };

  /** GET …/constraint: the agent stations ranked by their seeded busy shares (constraintOf). */
  const constraintReport = (f: Factory, range: DayRange): ConstraintReport =>
    constraintOf({
      factory: f,
      range,
      constraint: state.constraint[f.name] ?? null,
      source: constraintSourceOf(f),
      stats: state.stationStats[f.name] ?? {},
      agents: Object.fromEntries(Object.entries(state.stations[f.name] ?? {}).map(([id, c]) => [id, c.agents])),
      slots: Object.fromEntries(
        Object.entries(state.stations[f.name] ?? {}).map(([id, c]) => [
          id,
          c.wip_limit ?? Math.max(c.agents.length, 1),
        ]),
      ),
      waiting: Object.fromEntries(
        f.stations.map((s) => [
          s.id,
          state.items.filter(
            (it) => it.factory === f.name && it.station === s.id && (it.state === 'queued' || it.state === 'ready'),
          ).length,
        ]),
      ),
      now: clock(),
    });

  /**
   * One agent's statistics: the fixture's measures (zeros for an agent without any) over the
   * request's range, with the stations whose agents are its copies (builder-1 → builder). Null
   * for an agent the factory does not have; a string for a bad range.
   */
  const agentStatsOf = (factory: Factory, agent: string, params: URLSearchParams): AgentStats | string | null => {
    const config = state.stations[factory.name] ?? {};
    const base = (name: string) => name.replace(/-\d+$/, '');
    const stations = factory.stations
      .filter((s) => (config[s.id]?.agents ?? []).some((a) => base(a) === agent))
      .map((s) => ({
        id: s.id,
        slots: config[s.id]?.copies ?? 1,
        constraint: state.constraint[factory.name] === s.id,
      }));
    if (stations.length === 0) return null;
    const range = rangeOf(params, clock());
    if (typeof range === 'string') return range;
    const measured: MockAgentStats = state.agentStats[factory.name]?.[agent] ?? {
      model: null,
      tasks_finished: 0,
      sent_back: { count: 0, ratio: null },
      median_turns: null,
      spend_per_task_usd: null,
      task_time_p85_minutes: null,
      spend_usd: 0,
      reasons: [],
      denied_tool_calls: { count: 0, tools: [] },
    };
    const day = (ms: number) => new Date(ms).toISOString().slice(0, 10);
    return { ...measured, agent, from: day(range.from), to: day(range.to), stations };
  };

  const board = (name: string): Board | undefined => {
    const factory = findFactory(name);
    if (!factory) return undefined;
    const config = state.stations[factory.name] ?? {};
    const items = state.items.filter((it) => it.factory === factory.name);
    const stations: BoardStation[] = factory.stations.map((s) => {
      const here = items
        .filter((it) => it.station === s.id)
        .sort((a, b) =>
          a.class_of_service === b.class_of_service ? a.number - b.number : a.class_of_service === 'expedite' ? -1 : 1,
        );
      return {
        id: s.id,
        kind: s.kind,
        ...(s.action ? { action: s.action } : {}),
        agents: config[s.id]?.agents ?? [],
        wip_limit: config[s.id]?.wip_limit ?? null,
        waiting: here.filter((it) => it.state === 'queued' || it.state === 'ready').length,
        in_progress: here.filter((it) => it.state === 'in_progress').length,
        items: here.map(card),
        ...stationStats(factory.name, s, here),
      };
    });
    return {
      factory: { id: factory.id, name: factory.name, ...(factory.title ? { title: factory.title } : {}) },
      stations,
      constraint: state.constraint[factory.name] ?? null,
      constraint_source: constraintSourceOf(factory),
      line: lineOf(factory, stations, items),
    };
  };

  /**
   * The line as goald describes it: work in progress at the stations with a limit, the release
   * policy with the drum's buffer (shown, not enforced), the expedite lane and the weekly budget.
   */
  const lineOf = (factory: Factory, stations: BoardStation[], items: MockItem[]): Board['line'] => {
    const flow = state.flow[factory.name];
    const drum = state.constraint[factory.name] ?? null;
    const limited = stations.filter((s) => s.wip_limit !== null);
    const weekly = state.weeklyBudget[factory.name] ?? null;
    const today = Date.parse(`${utcDay(0, clock())}T00:00:00Z`);
    const spent = metricsOf(flowInput(factory), { from: today - 6 * dayMs, to: today }, false).spend_usd;
    const queues = new Set(stations.filter((s) => s.kind === 'queue').map((s) => s.id));
    const lane = flow?.expedite_wip !== undefined;
    const andon = openAndon(factory);
    return {
      status: andon ? 'stopped' : 'running',
      andon,
      wip: {
        in_progress: limited.reduce(
          (n, s) => n + s.in_progress + s.items.filter((it) => it.state === 'waiting_human').length,
          0,
        ),
        limit: limited.length > 0 ? limited.reduce((n, s) => n + (s.wip_limit ?? 0), 0) : null,
      },
      release: {
        policy: flow?.release ?? 'push',
        enforced: false,
        drum,
        buffer: drum ? (state.stations[factory.name]?.[drum]?.buffer ?? null) : null,
        in_buffer: stations.find((s) => s.id === drum)?.waiting ?? 0,
      },
      expedite: lane
        ? {
            limit: flow?.expedite_wip ?? null,
            in_use: items.filter(
              (it) =>
                it.class_of_service === 'expedite' &&
                it.state !== 'done' &&
                it.state !== 'cancelled' &&
                !queues.has(it.station),
            ).length,
          }
        : null,
      budget: weekly === null ? null : { weekly_usd: weekly, spent_usd: spent, exhausted: spent >= weekly },
    };
  };

  async function route(req: IncomingMessage, res: ServerResponse, next: Next) {
    const url = new URL(req.url ?? '/', 'http://mock');
    const path = url.pathname;
    const method = req.method ?? 'GET';

    if (path === '/__mock/reset' && method === 'POST') {
      const at = (await readBody(req)).now;
      fixed = typeof at === 'string' && !Number.isNaN(Date.parse(at)) ? Date.parse(at) : undefined;
      state = seed(clock());
      return json(res, 200, { ok: true });
    }
    if (path === '/__mock/fail' && method === 'POST') {
      const fault = parseFault(await readBody(req));
      if (!fault) return problem(res, 400, 'Bad Request', 'a fault names the path it applies to');
      state.faults.push(fault);
      return json(res, 200, { ok: true });
    }
    if (path === '/__mock/fail' && method === 'DELETE') {
      state.faults = [];
      return json(res, 200, { ok: true });
    }
    if (!path.startsWith('/v1/')) return next();

    // A latency every answer but the stream waits (X-Mock-Latency-Ms; one e2e project sets it), so
    // pending screens show and loads overlap as they do against a real goald.
    const latency = Number(req.headers['x-mock-latency-ms'] ?? 0);
    if (latency > 0 && !path.endsWith('/stream')) await sleep(latency);

    const fault = takeFault(state.faults, method, path);
    if (fault?.delay_ms) await sleep(fault.delay_ms);
    if (fault?.status) return json(res, fault.status, definitions.faultBody(path, fault));

    if (path === '/v1/health') return json(res, 200, { status: 'ok' });

    if (path === '/v1/orgs' && method === 'GET') {
      const orgs = [...state.orgs].sort((a, b) => a.name.localeCompare(b.name)).map(withCount);
      return json(res, 200, { orgs: orgs.map(({ created_at: _created, ...summary }) => summary) });
    }
    if (path === '/v1/orgs' && method === 'POST') {
      const body = await readBody(req);
      const name = typeof body.name === 'string' ? body.name.trim() : '';
      const slug = typeof body.slug === 'string' ? body.slug : '';
      if (findOrg(slug)) return problem(res, 409, 'Conflict', `slug "${slug}" is taken`, 'slug-taken');
      if (!name || !slugRule.test(slug) || reserved.includes(slug)) {
        return problem(res, 409, 'Conflict', `slug "${slug}" is reserved or malformed, or the name is empty`);
      }
      const words = name.split(/\s+/);
      const initials = words.length > 1 ? `${words[0]?.[0]}${words[1]?.[0]}`.toUpperCase() : name.slice(0, 2);
      const mark = typeof body.mark === 'string' && body.mark ? body.mark : initials;
      const org: Org = {
        id: crypto.randomUUID(),
        slug,
        name,
        mark,
        factory_count: 0,
        created_at: new Date(clock()).toISOString(),
      };
      state.orgs.push(org);
      return json(res, 201, org);
    }

    const scoped = path.match(/^\/v1\/orgs\/([^/]+)(\/.*)?$/);
    if (!scoped) return problem(res, 404, 'Not Found', path);
    const org = findOrg(decodeURIComponent(scoped[1] ?? ''));
    if (!org) return json(res, 404, { title: 'Organization not found', status: 404, code: 'org-not-found' });
    const rest = scoped[2] ?? '';
    if (await definitions.handle(org, rest, method, url, req, res)) return;

    if (rest === '' && method === 'GET') return json(res, 200, withCount(org));
    if (rest === '' && method === 'DELETE') {
      const body = await readBody(req);
      if (body.confirm !== org.name) {
        return problem(
          res,
          409,
          'Conflict',
          "the confirmation is not the organization's name",
          'confirmation-mismatch',
        );
      }
      audit(org, 'org.deleted', { slug: org.slug, name: org.name });
      state.orgs = state.orgs.filter((o) => o.id !== org.id);
      return json(res, 202, { purge_after: new Date(clock() + retentionMs).toISOString() });
    }
    if (rest === '/settings' && method === 'GET') return json(res, 200, settingsOf(org));
    if (rest === '/settings' && method === 'PATCH') return patchSettings(org, await readBody(req), res);
    if (rest === '/audit' && method === 'GET') {
      const q = url.searchParams;
      const limit = Number(q.get('limit') ?? 100);
      if (!Number.isInteger(limit) || limit < 1 || limit > 500)
        return problem(res, 400, 'Bad Request', 'limit is 1 to 500');
      const before = Number(q.get('before') ?? 0);
      const factory = q.get('factory');
      const factoryId = factory ? findFactory(factory, org.id)?.id : undefined;
      if (factory && !factoryId) return problem(res, 404, 'Not Found', `factory ${factory}`);
      const matching = state.audit
        .filter(({ org: o, entry: e }) => o === org.id && (!before || e.id < before))
        .map(({ entry }) => entry)
        .filter(
          (e) =>
            (!q.get('action') || e.action === q.get('action')) &&
            (!q.get('target_kind') || e.target.kind === q.get('target_kind')) &&
            (!q.get('target_id') || e.target.id === q.get('target_id')) &&
            (!factoryId || e.factory_id === factoryId),
        )
        .reverse();
      const entries = matching.slice(0, limit);
      const page: AuditPage = {
        entries,
        next_before: matching.length > limit ? (entries.at(-1)?.id ?? null) : null,
      };
      return json(res, 200, page);
    }
    if (rest === '/factories') {
      return json(res, 200, { factories: state.factories.filter((f) => f.org_id === org.id) });
    }

    if (rest === '/attention' && method === 'GET') {
      const factories = factoriesOf(org.id).map(attentionOf);
      const body: OrgAttention = { waiting_on_you: factories.reduce((n, a) => n + a.waiting_on_you, 0), factories };
      return json(res, 200, body);
    }

    if (rest === '/overview' && method === 'GET') {
      const window = url.searchParams.get('window') ?? '7d';
      const days = Number(windowRule.exec(window)?.[1] ?? 0);
      if (days < 1 || days > 90) return problem(res, 400, 'Bad Request', `window "${window}" is not 1d to 90d`);
      const factories = factoriesOf(org.id).map((f) => overviewOf(f, days));
      const budgets = factories.flatMap((o) => (o.budget_usd === null ? [] : [o.budget_usd]));
      const body: FactoryOverviewList = {
        window,
        since: utcDay(days - 1, clock()),
        until: utcDay(0, clock()),
        waiting_on_you: factories.reduce((n, o) => n + o.waiting.on_you, 0),
        spend_usd: cents(factories.reduce((n, o) => n + o.spend_usd, 0)),
        budget_usd: budgets.length > 0 ? cents(budgets.reduce((n, b) => n + b, 0)) : null,
        factories,
      };
      return json(res, 200, body);
    }

    if (rest === '/inbox' && method === 'GET') {
      const limit = Number(url.searchParams.get('limit') ?? 200);
      if (!Number.isInteger(limit) || limit < 1 || limit > 500) {
        return problem(res, 400, 'Bad Request', 'limit must be in [1, 500]');
      }
      const key = url.searchParams.get('factory') ?? '';
      const only = key ? findFactory(key, org.id) : undefined;
      if (key && !only) return problem(res, 404, 'Factory not found', key);
      const entries = (only ? [only] : factoriesOf(org.id))
        .flatMap(inboxOf)
        .sort((a, b) => b.since.localeCompare(a.since) || a.id.localeCompare(b.id));
      // The most recent entries, by kind and then oldest first, as goald orders them.
      const kept = entries
        .slice(0, limit)
        .sort(
          (a, b) =>
            kindOrder.indexOf(a.kind) - kindOrder.indexOf(b.kind) ||
            a.since.localeCompare(b.since) ||
            a.id.localeCompare(b.id),
        );
      const body: Inbox = { entries: kept, truncated: entries.length > limit };
      return json(res, 200, body);
    }

    if (rest === '/stream') {
      const factory = url.searchParams.get('factory') ?? '';
      if (factory && !findFactory(factory, org.id)) return problem(res, 404, 'Factory not found', factory);
      res.writeHead(200, {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        Connection: 'keep-alive',
      });
      res.write('retry: 3000\n\n');
      const sub = { org: org.id, factory, res };
      subscribers.add(sub);
      req.on('close', () => subscribers.delete(sub));
      return;
    }

    let m = rest.match(/^\/factories\/([^/]+)\/agents\/([^/]+)\/stats$/);
    if (m && method === 'GET') {
      const key = decodeURIComponent(m[1] ?? '');
      const factory = findFactory(key, org.id);
      if (!factory) return problem(res, 404, 'Factory not found', key);
      const stats = agentStatsOf(factory, decodeURIComponent(m[2] ?? ''), url.searchParams);
      if (stats === null)
        return problem(res, 404, 'Not Found', `factory ${factory.name} has no such agent`, 'agent-not-found');
      return typeof stats === 'string' ? problem(res, 400, 'Bad Request', stats) : json(res, 200, stats);
    }

    m = rest.match(
      /^\/factories\/([^/]+)\/(board|items|constraint|metrics|metrics\/weekly|metrics\/lead-time|andon|andon\/clear)$/,
    );
    if (m) {
      const key = decodeURIComponent(m[1] ?? '');
      const factory = findFactory(key, org.id);
      if (!factory) return problem(res, 404, 'Factory not found', key);

      if (m[2] === 'andon' || m[2] === 'andon/clear') return andon(factory, m[2] !== 'andon', method, url, req, res);

      if (m[2] === 'board' && method === 'GET') return json(res, 200, board(factory.id));
      if (m[2] === 'constraint' && method === 'GET') {
        if (url.searchParams.has('window')) return problem(res, 400, 'Bad Request', 'the report takes from and to');
        const range = rangeOf(url.searchParams, clock());
        return typeof range === 'string'
          ? problem(res, 400, 'Bad Request', range)
          : json(res, 200, constraintReport(factory, range));
      }
      if (m[2]?.startsWith('metrics') && method === 'GET') {
        const type = url.searchParams.get('type') ?? '';
        if (type && factory.work_types.length > 0 && !factory.work_types.includes(type)) {
          return problem(res, 400, 'Bad Request', `factory ${factory.name} has no work type "${type}"`);
        }
        const input = flowInput(factory, type);
        if (m[2] === 'metrics/weekly') {
          const weekly = weeklyOf(input, url.searchParams);
          return typeof weekly === 'string' ? problem(res, 400, 'Bad Request', weekly) : json(res, 200, weekly);
        }
        const range = rangeOf(url.searchParams, input.now);
        if (m[2] === 'metrics/lead-time') {
          const busy = input.drum ? (state.stationStats[factory.name]?.[input.drum]?.busy_ratio ?? null) : null;
          return typeof range === 'string'
            ? problem(res, 400, 'Bad Request', range)
            : json(res, 200, leadTimeOf(input, range, busy));
        }
        const compare = url.searchParams.get('compare');
        if (typeof range === 'string' || (compare !== null && compare !== 'previous')) {
          const detail = typeof range === 'string' ? range : 'compare is previous or absent';
          return problem(res, 400, 'Bad Request', detail);
        }
        return json(res, 200, metricsOf(input, range, compare === 'previous'));
      }
      if (m[2] === 'items' && method === 'GET') {
        const st = url.searchParams.get('state');
        const station = url.searchParams.get('station');
        const items = state.items
          .filter(
            (it) => it.factory === factory.name && (!st || it.state === st) && (!station || it.station === station),
          )
          .sort((a, b) => b.number - a.number)
          .map(card);
        const body: ItemPage = { items, next_after: null };
        return json(res, 200, body);
      }
      if (m[2] === 'items' && method === 'POST') {
        const body = await readBody(req);
        const title = typeof body.title === 'string' ? body.title.trim() : '';
        const type = typeof body.type === 'string' ? body.type : '';
        if (!title) return problem(res, 400, 'Invalid item', 'title is required');
        if (!factory.work_types.includes(type)) return problem(res, 400, 'Invalid item', `unknown work type "${type}"`);
        const number =
          Math.max(0, ...state.items.filter((it) => it.factory === factory.name).map((it) => it.number)) + 1;
        const now = new Date(clock()).toISOString();
        const it: MockItem = {
          id: crypto.randomUUID(),
          factory: factory.name,
          number,
          type,
          lane: typeof body.lane === 'string' && body.lane ? body.lane : null,
          title,
          body: typeof body.body === 'string' ? body.body : '',
          station: factory.stations[0]?.id ?? 'inbox',
          state: 'queued',
          class_of_service: 'standard',
          priority: 0,
          rework_count: 0,
          cost_usd: 0,
          outputs: {},
          created_at: now,
          updated_at: now,
          agent: null,
          pr_url: null,
          spans: [],
        };
        state.items.push(it);
        emit(it, 'created', 'engine');
        return json(res, 201, full(it));
      }
      return problem(res, 405, 'Method not allowed');
    }

    m = rest.match(/^\/runs\/([^/]+)$/);
    if (m && method === 'GET') {
      const run = state.runs.find((r) => r.id === decodeURIComponent(m?.[1] ?? ''));
      const of = run && state.items.find((it) => it.id === run.item_id);
      if (!run || !of || findFactory(of.factory)?.org_id !== org.id) return problem(res, 404, 'Run not found', path);
      return json(res, 200, run);
    }

    m = rest.match(/^\/items\/([^/]+)(?:\/(events|runs|notes|approve|reject|merge|retry|move|pause))?$/);
    if (!m) return problem(res, 404, 'Not found', path);

    const it = findItem(decodeURIComponent(m[1] ?? ''), org.id);
    if (!it) return problem(res, 404, 'Item not found', decodeURIComponent(m[1] ?? ''));
    const action = m[2];

    if (!action && method === 'GET') return json(res, 200, { ...full(it), flow: flowOf(it) });
    if (action === 'events' && method === 'GET') {
      const after = Number(url.searchParams.get('after') ?? 0);
      const limit = Math.min(1000, Number(url.searchParams.get('limit') ?? 200));
      const events = state.events.filter((e) => e.item_id === it.id && e.id > after).slice(0, limit);
      return json(res, 200, { events, next_after: events.at(-1)?.id ?? after });
    }
    if (action === 'runs' && method === 'GET') {
      const runs = state.runs.filter((r) => r.item_id === it.id);
      const body: RunList = { runs: runs.slice(0, 200), truncated: runs.length > 200 };
      return json(res, 200, body);
    }
    if (method !== 'POST') return problem(res, 405, 'Method not allowed');

    const body = await readBody(req);
    const by = localUser;
    const stations = stationsOf(it);
    const index = stations.findIndex((s) => s.id === it.station);

    if (action === 'notes') {
      const text = typeof body.text === 'string' ? body.text.trim() : '';
      if (!text || [...text].length > 2000) return problem(res, 400, 'Bad Request', 'a note is 1 to 2000 characters');
      if (it.state === 'done' || it.state === 'cancelled') {
        return problem(res, 409, 'Conflict', `${ref(it)} is ${it.state}`, 'item-closed');
      }
      emit(it, 'note', by, { text, actor: 'user:local' });
      const receipt: NoteReceipt = { event_id: state.nextEventId - 1, delivery: 'next_run' };
      return json(res, 202, receipt);
    }

    if (action === 'retry') {
      if (it.state !== 'blocked') return problem(res, 409, 'Item is not blocked', `${ref(it)} is ${it.state}`);
      const kind = stations[index]?.kind;
      it.state = kind === 'human' ? 'waiting_human' : 'queued';
      emit(it, 'retried', by);
      return json(res, 200, full(it));
    }

    const closed = it.state === 'done' || it.state === 'cancelled';
    const reason = typeof body.reason === 'string' ? body.reason.trim() : '';
    // A person acting on an item in progress takes its lease: goald stops the run.
    const take = (verb: string) => {
      if (it.state !== 'in_progress') return;
      emit(it, 'lease_taken', by, { action: verb, actor: 'user:local' });
      it.agent = null;
    };

    if (action === 'move') {
      const to = typeof body.to === 'string' ? body.to.trim() : '';
      const target = stations.findIndex((s) => s.id === to);
      if (closed) return problem(res, 409, 'Conflict', `${ref(it)} is ${it.state}`, 'item-closed');
      if (target < 0) return problem(res, 400, 'Bad Request', `station "${to}" is not on the line`, 'unknown-station');
      if (target === index) return problem(res, 409, 'Conflict', `${ref(it)} is already at ${to}`, 'same-station');
      if (stations[target]?.kind === 'done') {
        return problem(res, 409, 'Conflict', 'an item reaches a done station by approval or merge', 'done-station');
      }
      const upstream = target < index;
      if (upstream && !reason)
        return problem(res, 400, 'Bad Request', 'a move upstream needs a reason', 'reason-required');
      take('move');
      if (upstream) it.rework_count += 1;
      moveTo(it, to, by, { manual: true, actor: 'user:local', ...(reason ? { reason } : {}) });
      return json(res, 200, full(it));
    }

    if (action === 'pause') {
      if (closed) return problem(res, 409, 'Conflict', `${ref(it)} is ${it.state}`, 'item-closed');
      if (it.state === 'blocked')
        return problem(res, 409, 'Conflict', `${ref(it)} is already blocked`, 'already-blocked');
      take('pause');
      it.state = 'blocked';
      emit(it, 'paused', by, { actor: 'user:local', ...(reason ? { reason } : {}) });
      return json(res, 200, full(it));
    }

    if (action === 'reject') {
      if (!reason) return problem(res, 400, 'Invalid decision', 'a rejection needs a reason');
      const back = stations
        .slice(0, index)
        .reverse()
        .find((s) => s.kind === 'agent');
      if (!['waiting_human', 'in_progress', 'blocked'].includes(it.state) || !back) {
        return problem(res, 409, 'Conflict', `${ref(it)} cannot be sent back now`, 'cannot-send-back');
      }
      take('reject');
      emit(it, 'rejected', by, { reason });
      it.rework_count += 1;
      moveTo(it, back.id, by);
      return json(res, 200, full(it));
    }

    if (it.state !== 'waiting_human') {
      return problem(res, 409, 'Item is not waiting for a human', `${ref(it)} is ${it.state} at ${it.station}`);
    }

    if (action === 'approve' || action === 'merge') {
      const station = stations[index];
      if (action === 'merge') {
        if (station?.action !== 'merge')
          return problem(res, 409, 'Not a merge station', `${it.station} does not merge`);
        if (!it.pr_url) return problem(res, 409, 'Nothing to merge', `${ref(it)} has no pull request`);
        emit(it, 'merged', by, { pull_request: it.pr_url, method: 'squash' });
      } else if (station?.action === 'merge') {
        return problem(res, 409, 'Merge, do not approve', `${it.station} merges the pull request; use merge`);
      }
      emit(it, 'approved', by);
      moveTo(it, stations[index + 1]?.id ?? it.station, by);
      return json(res, 200, full(it));
    }

    return problem(res, 404, 'Not found', path);
  }

  // Live mode: running agents keep calling tools so the stream has something to say.
  const timer = options.live
    ? setInterval(() => {
        const running = state.items.filter((it) => it.state === 'in_progress' && it.agent);
        const it = running[Math.floor(Math.random() * running.length)];
        if (!it?.agent) return;
        const n = state.events.length;
        emit(it, 'tool_call', it.agent, { id: `live${n}`, tool: 'Bash', input: '{"command":"go test ./..."}' });
        emit(it, 'tool_result', it.agent, { id: `live${n}`, is_error: false, summary: 'ok' });
        it.cost_usd = Math.round((it.cost_usd + 0.03) * 100) / 100;
      }, 4000)
    : undefined;

  return {
    handle: (req, res, next) => {
      route(req, res, next).catch((err: unknown) => problem(res, 500, 'Mock failure', String(err)));
    },
    close: () => {
      if (timer) clearInterval(timer);
      for (const sub of subscribers) sub.res.end();
    },
  };
}
