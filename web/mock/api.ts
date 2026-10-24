// An in-memory stand-in for goald's /v1 API, typed with the contract's shapes from
// @goal/api-client, mounted into the Vite dev and preview servers when GOAL_API=mock
// (mock/plugin.ts). Like goald it serves /v1/orgs and every other route under
// /v1/orgs/{org} (slug or id; unknown is 404), keeps state per process and broadcasts each
// organization's item events on its /v1/orgs/{org}/stream. Decisions are the local user's.
// POST /__mock/reset restores the seed.
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Board, BoardStation, Item, ItemCard, ItemEvent, ItemPage, Org, StreamEvent } from '@goal/api-client';
import { type MockItem, type MockState, seed } from './fixtures.ts';

type Next = (err?: unknown) => void;

interface Subscriber {
  org: string;
  factory: string;
  res: ServerResponse;
}

/** Who decides: goald's request actor in phase 2.2. */
const localUser = 'Local user';

/** The slug rule and reserved words of POST /v1/orgs. */
const slugRule = /^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$/;
const reserved = ['new', 'settings', 'api', 'v1', 'o', 'me', 'orgs', 'auth', 'invite', 'admin', 'default'];

export interface MockApi {
  handle: (req: IncomingMessage, res: ServerResponse, next: Next) => void;
  close: () => void;
}

const json = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const problem = (res: ServerResponse, status: number, title: string, detail?: string) =>
  json(res, status, { title, status, ...(detail ? { detail } : {}) });

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
  let state: MockState = seed();
  const subscribers = new Set<Subscriber>();

  const ref = (it: MockItem) => `${it.factory}#${it.number}`;

  const card = (it: MockItem): ItemCard => ({
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
    age_seconds: Math.round((Date.now() - Date.parse(it.created_at)) / 1000),
    agent: it.agent,
    pr_url: it.pr_url,
  });

  const full = (it: MockItem): Item => {
    const factory = state.factories.find((f) => f.name === it.factory);
    const { agent: _agent, factory: _factory, pr_url: _pr, lane, ...rest } = it;
    return { ...rest, ...(lane ? { lane } : {}), ref: ref(it), factory_id: factory?.id ?? '' };
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

  const emit = (it: MockItem, kind: string, actor: string, data: Record<string, unknown> = {}) => {
    const at = new Date().toISOString();
    const event: ItemEvent = { id: state.nextEventId++, item_id: it.id, at, kind, station: it.station, actor, data };
    state.events.push(event);
    it.updated_at = at;

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

  const moveTo = (it: MockItem, to: string, actor: string) => {
    const from = it.station;
    it.station = to;
    const kind = findFactory(it.factory)?.stations.find((s) => s.id === to)?.kind;
    it.state = kind === 'done' ? 'done' : kind === 'human' ? 'waiting_human' : 'queued';
    it.agent = null;
    if (it.state === 'done') it.done_at = new Date().toISOString();
    emit(it, 'moved', actor, { from, to });
    if (it.state === 'waiting_human') emit(it, 'waiting_human', 'engine');
    if (it.state === 'done') emit(it, 'done', 'engine');
  };

  const stationsOf = (it: MockItem) => findFactory(it.factory)?.stations ?? [];

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
      };
    });
    return {
      factory: { id: factory.id, name: factory.name, ...(factory.title ? { title: factory.title } : {}) },
      stations,
      constraint: state.constraint[factory.name] ?? null,
    };
  };

  async function route(req: IncomingMessage, res: ServerResponse, next: Next) {
    const url = new URL(req.url ?? '/', 'http://mock');
    const path = url.pathname;
    const method = req.method ?? 'GET';

    if (path === '/__mock/reset' && method === 'POST') {
      state = seed();
      return json(res, 200, { ok: true });
    }
    if (!path.startsWith('/v1/')) return next();

    if (path === '/v1/health') return json(res, 200, { status: 'ok' });

    if (path === '/v1/orgs' && method === 'GET') {
      const orgs = [...state.orgs].sort((a, b) => a.name.localeCompare(b.name)).map(withCount);
      return json(res, 200, { orgs: orgs.map(({ created_at: _created, ...summary }) => summary) });
    }
    if (path === '/v1/orgs' && method === 'POST') {
      const body = await readBody(req);
      const name = typeof body.name === 'string' ? body.name.trim() : '';
      const slug = typeof body.slug === 'string' ? body.slug : '';
      if (!name || !slugRule.test(slug) || reserved.includes(slug) || findOrg(slug)) {
        return problem(res, 409, 'Conflict', `slug "${slug}" is taken, reserved or malformed, or the name is empty`);
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
        created_at: new Date().toISOString(),
      };
      state.orgs.push(org);
      return json(res, 201, org);
    }

    const scoped = path.match(/^\/v1\/orgs\/([^/]+)(\/.*)?$/);
    if (!scoped) return problem(res, 404, 'Not Found', path);
    const org = findOrg(decodeURIComponent(scoped[1] ?? ''));
    if (!org) return json(res, 404, { title: 'Organization not found', status: 404, code: 'org-not-found' });
    const rest = scoped[2] ?? '';

    if (rest === '' && method === 'GET') return json(res, 200, withCount(org));
    if (rest === '/factories') {
      return json(res, 200, { factories: state.factories.filter((f) => f.org_id === org.id) });
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

    let m = rest.match(/^\/factories\/([^/]+)\/(board|items|metrics)$/);
    if (m) {
      const key = decodeURIComponent(m[1] ?? '');
      const factory = findFactory(key, org.id);
      if (!factory) return problem(res, 404, 'Factory not found', key);

      if (m[2] === 'board' && method === 'GET') return json(res, 200, board(factory.id));
      if (m[2] === 'metrics' && method === 'GET') return json(res, 200, state.metrics[factory.name]);
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
        const now = new Date().toISOString();
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
        };
        state.items.push(it);
        emit(it, 'created', 'engine');
        return json(res, 201, full(it));
      }
      return problem(res, 405, 'Method not allowed');
    }

    m = rest.match(/^\/items\/([^/]+)(?:\/(events|approve|reject|merge|retry))?$/);
    if (!m) return problem(res, 404, 'Not found', path);

    const it = findItem(decodeURIComponent(m[1] ?? ''), org.id);
    if (!it) return problem(res, 404, 'Item not found', decodeURIComponent(m[1] ?? ''));
    const action = m[2];

    if (!action && method === 'GET') return json(res, 200, full(it));
    if (action === 'events' && method === 'GET') {
      const after = Number(url.searchParams.get('after') ?? 0);
      const limit = Math.min(1000, Number(url.searchParams.get('limit') ?? 200));
      const events = state.events.filter((e) => e.item_id === it.id && e.id > after).slice(0, limit);
      return json(res, 200, { events, next_after: events.at(-1)?.id ?? after });
    }
    if (method !== 'POST') return problem(res, 405, 'Method not allowed');

    const body = await readBody(req);
    const by = localUser;
    const stations = stationsOf(it);
    const index = stations.findIndex((s) => s.id === it.station);

    if (action === 'retry') {
      if (it.state !== 'blocked') return problem(res, 409, 'Item is not blocked', `${ref(it)} is ${it.state}`);
      it.state = 'queued';
      emit(it, 'retried', by);
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

    if (action === 'reject') {
      const reason = typeof body.reason === 'string' ? body.reason.trim() : '';
      if (!reason) return problem(res, 400, 'Invalid decision', 'a rejection needs a reason');
      const back = stations
        .slice(0, index)
        .reverse()
        .find((s) => s.kind === 'agent');
      it.rework_count += 1;
      emit(it, 'rejected', by, { reason });
      moveTo(it, back?.id ?? it.station, by);
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
