// Jonah's bake-offs in the mock API (contracts/api/openapi.yaml, tag evals; ADR 0029): a factory's bake-offs,
// newest first, one of them with its arms, its tries per case and its recommendation, a re-run and the apply of
// an arm. The software factory starts with the done bake-off of the bake-off picture (frontend-engineer on
// three models, $14.20); every other factory has none. A re-run is queued and stays so: the mock runs no tries.
// Apply answers with a draft stub, because the mock keeps no draft of its own here.
import type { ServerResponse } from 'node:http';
import type { BakeOff, BakeOffArm, BakeOffCase, Draft, EvalEstimate, Org } from '@goal/api-client';

export interface BakeOffsDeps {
  clock: () => number;
  /** The organization's factories by id or name, as goald's lookup reads them. */
  factory: (org: string, key: string) => { id: string; name: string } | undefined;
}

export interface MockBakeOffs {
  /** Serves a bake-offs route; false when rest is none of them. */
  handle: (
    org: Org,
    rest: string,
    method: string,
    res: ServerResponse,
    readBody: () => Promise<Record<string, unknown>>,
  ) => Promise<boolean>;
  reset: () => void;
}

const json = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const problem = (res: ServerResponse, status: number, title: string, code: string, detail?: string) =>
  json(res, status, { title, status, code, ...(detail ? { detail } : {}) });

/** The five cases of the picture, with how many of the three tries each model passed. */
const pictured: { name: string; passed: [number, number, number] }[] = [
  { name: 'empty-state-matches-design', passed: [3, 3, 3] },
  { name: 'dark-theme-contrast', passed: [3, 2, 3] },
  { name: 'board-card-move-menu', passed: [2, 2, 3] },
  { name: 'form-errors-near-field', passed: [3, 3, 2] },
  { name: 'usage-bar-thresholds', passed: [3, 1, 3] },
];

const models = ['claude-haiku-5-5', 'claude-sonnet-5-5', 'claude-opus-5-5'];

/**
 * The done bake-off of the picture. Local times without a zone: the screen shows the day and the clock as the
 * person's own clock says them, so the picture's "Oct 25 at 18:40" holds in any zone.
 */
export const pictureBakeOff = (): BakeOff => {
  const trials = 3;
  const perModel = [
    { passed: 14, cost: 0.24, seconds: 14 * 60, turns: 38, judged: 0.95 },
    { passed: 13, cost: 0.9, seconds: 17 * 60, turns: 44, judged: 0.96 },
    { passed: 14, cost: 2.4, seconds: 22 * 60, turns: 41, judged: 0.97 },
  ];
  const tries = pictured.length * trials;
  const arms: BakeOffArm[] = models.map((model, i) => {
    const m = perModel[i] as (typeof perModel)[number];
    return {
      arm: i + 1,
      model,
      is_current: model === 'claude-opus-5-5',
      summary: {
        tries,
        passed: m.passed,
        errors: 0,
        pass_rate: m.passed / tries,
        cost_per_item_usd: m.cost,
        time_per_item_seconds: m.seconds,
        turns: m.turns,
        judged: { judge: 'plain-language', title: 'Plain language', share: m.judged },
      },
    };
  });
  const perCase: BakeOffCase[] = pictured.map((c) => ({
    case: c.name,
    arms: c.passed.map((passed, i) => ({ arm: i + 1, passed })),
  }));
  return {
    id: 'bake-off-1',
    state: 'done',
    agent: 'frontend-engineer',
    station: 'frontend',
    version: 21,
    trials,
    cases: pictured.length,
    estimate_usd: 15.6,
    cost_usd: 14.2,
    created_at: '2026-10-25T18:12:00',
    finished_at: '2026-10-25T18:40:00',
    arms,
    per_case: perCase,
    recommended_arm: 1,
  };
};

/** A bake-off just queued: no arm has a summary, no case a pass, and nothing is recommended. */
const queuedFrom = (source: BakeOff, id: string, at: string): BakeOff => ({
  id,
  state: 'queued',
  agent: source.agent,
  station: source.station,
  version: source.version,
  trials: source.trials,
  cases: source.cases,
  estimate_usd: source.estimate_usd,
  cost_usd: 0,
  created_at: at,
  arms: source.arms.map((a) => ({ ...a, summary: null })),
  per_case: source.per_case.map((c) => ({ case: c.case, arms: c.arms.map((a) => ({ arm: a.arm, passed: 0 })) })),
  recommended_arm: null,
});

export function createBakeOffs({ clock, factory }: BakeOffsDeps): MockBakeOffs {
  let byFactory = new Map<string, BakeOff[]>();
  let seq = 1;

  const reset = () => {
    byFactory = new Map();
    seq = 1;
  };

  /** The factory's bake-offs, newest first. The software factory is seeded with the picture's bake-off once. */
  const listOf = (factoryId: string, name: string): BakeOff[] => {
    let list = byFactory.get(factoryId);
    if (!list) {
      list = name === 'software' ? [pictureBakeOff()] : [];
      byFactory.set(factoryId, list);
    }
    return list;
  };

  const nowIso = () => new Date(clock()).toISOString();

  return {
    reset,
    async handle(org, rest, method, res, readBody) {
      const m = rest.match(/^\/factories\/([^/]+)\/evals\/bake-offs(?:\/([^/]+)(\/rerun|\/apply)?)?$/);
      if (!m) return false;
      const f = factory(org.id, decodeURIComponent(m[1] ?? ''));
      if (!f) {
        problem(res, 404, 'Factory not found', 'factory-not-found', m[1]);
        return true;
      }
      const list = listOf(f.id, f.name);
      const id = m[2] === undefined ? undefined : decodeURIComponent(m[2]);
      const action = m[3];

      if (id === undefined && method === 'GET') {
        json(res, 200, { bake_offs: list.slice(0, 20) });
        return true;
      }

      const found = id === undefined ? undefined : list.find((b) => b.id === id);
      if (id !== undefined && !found) {
        problem(res, 404, 'Bake-off not found', 'eval-run-not-found', id);
        return true;
      }

      if (found && action === undefined && method === 'GET') {
        json(res, 200, found);
        return true;
      }

      if (found && action === '/rerun' && method === 'POST') {
        const bakeOff = queuedFrom(found, `bake-off-${++seq}`, nowIso());
        list.unshift(bakeOff);
        const estimate: EvalEstimate = { usd: found.estimate_usd, unpriced: [] };
        json(res, 202, { bake_off: bakeOff, estimate });
        return true;
      }

      if (found && action === '/apply' && method === 'POST') {
        const body = await readBody();
        const arm = typeof body.arm === 'number' ? body.arm : Number.NaN;
        if (!found.arms.some((a) => a.arm === arm)) {
          problem(res, 422, 'Invalid bake-off', 'invalid-bake-off', 'the bake-off has no such arm');
          return true;
        }
        // A stub of the draft's shape: the screen reads only that the write succeeded.
        json(res, 200, { id: `draft-${found.id}`, base: { number: found.version } } as unknown as Draft);
        return true;
      }

      return false;
    },
  };
}
