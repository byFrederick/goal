// Jonah in the mock API (contracts/api/openapi.yaml, tag jonah): a factory's overview (its policy, the proposals
// waiting, the month's cost and the week's observations), its proposals by state, and a manual review. A seeded
// factory (the Software factory, by name) starts with the picture's two waiting proposals, an applied one, four
// observations and $6.40 this month; every other factory has no version yet, so a review answers 404
// version-not-found. A review is queued and stays in flight: a second one answers 409 review-in-flight.
import type { ServerResponse } from 'node:http';
import type { JonahObservation, JonahOverview, JonahPolicy, JonahReview, ProposalEntry } from '../src/api/types.ts';

export interface JonahDeps {
  /** The organization's factories by id or name, as goald's lookup reads them. */
  factory: (org: string, key: string) => { id: string; name: string } | undefined;
  /** The mock's clock in epoch milliseconds (the instant a reset fixed, or real time). */
  clock: () => number;
}

export interface MockJonah {
  /** Serves a jonah route; false when rest is none of them. */
  handle: (
    org: { id: string },
    rest: string,
    method: string,
    url: URL,
    res: ServerResponse,
    readBody: () => Promise<Record<string, unknown>>,
  ) => Promise<boolean>;
  reset: () => void;
}

interface FactoryJonah {
  /** The review queued by a person and still in flight; a second one answers 409 until a reset. */
  review?: JonahReview;
}

const json = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const problem = (res: ServerResponse, status: number, title: string, code: string, detail?: string) =>
  json(res, status, { title, status, code, ...(detail ? { detail } : {}) });

/** The policy the Software factory's learning.jonah sets, with the defaults the rest of the factories read. */
const softwarePolicy: JonahPolicy = {
  enabled: true,
  propose_after: 5,
  weekly_usd: 20,
  model: 'claude-opus-5-5',
  approver_roles: ['owner', 'admin'],
};

const defaultPolicy: JonahPolicy = {
  enabled: true,
  propose_after: 5,
  weekly_usd: 10,
  approver_roles: ['owner', 'admin'],
};

/** The picture's four observations, newest first; two became proposals or are watched toward one. */
const softwareObservations = (now: number): JonahObservation[] => [
  {
    id: 'obs-screenshot',
    summary: 'reviewer sent back 4 UI changes because they had no screenshot',
    detail: '#149, #138, #131, #127',
    item_refs: ['software#149', 'software#138', 'software#131', 'software#127'],
    state: 'proposed',
    count: 5,
    propose_after: 5,
    proposal: 7,
    created_at: new Date(now - 2 * 3_600_000).toISOString(),
  },
  {
    id: 'obs-full-suite',
    summary: 'backend-engineer ran the whole test suite where the changed checks were enough',
    detail: '9 runs, about $0.60 more each',
    item_refs: ['software#146', 'software#140'],
    state: 'watching',
    count: 3,
    propose_after: 5,
    created_at: new Date(now - 26 * 3_600_000).toISOString(),
  },
  {
    id: 'obs-rollback',
    summary: 'Specs for infra items leave out how to roll back',
    detail: '2 of the last 5 infra items',
    item_refs: ['software#133', 'software#120'],
    state: 'watching',
    count: 2,
    propose_after: 5,
    created_at: new Date(now - 30 * 3_600_000).toISOString(),
  },
  {
    id: 'obs-plain-language',
    summary: 'Plain-language judge passes 96% of runs',
    detail: 'Could sample 10% instead of 25% and save $1.10 a month',
    item_refs: [],
    state: 'watching',
    count: 1,
    propose_after: 5,
    created_at: new Date(now - 50 * 3_600_000).toISOString(),
  },
];

/** The Software factory's proposals, newest first. */
const softwareProposals = (now: number): ProposalEntry[] => [
  {
    number: 8,
    title: 'Split requests over 400 changed lines into an assembly',
    state: 'proposed',
    created_at: new Date(now - 24 * 3_600_000).toISOString(),
    paths: ['agents/architect.md'],
    agents: ['architect'],
    estimate: { metric: 'lead_time_p85', station: '', before: 31, after: 26, cost_per_item_usd: 0 },
    evidence_count: 6,
  },
  {
    number: 7,
    title: 'Ask frontend-engineer for a screenshot of every UI change',
    state: 'proposed',
    created_at: new Date(now - 2 * 3_600_000).toISOString(),
    paths: ['rules/evidence.md'],
    agents: ['frontend-engineer'],
    estimate: { metric: 'sent_back_rate', station: 'frontend', before: 0.18, after: 0.1, cost_per_item_usd: 0.04 },
    evidence_count: 4,
  },
  {
    number: 6,
    title: 'Name the agent in every sent-back note',
    state: 'applied',
    created_at: new Date(now - 9 * 24 * 3_600_000).toISOString(),
    paths: ['agents/reviewer.md'],
    agents: ['reviewer'],
    evidence_count: 3,
  },
];

export function createJonah({ factory, clock }: JonahDeps): MockJonah {
  let byFactory = new Map<string, FactoryJonah>();

  const reset = () => {
    byFactory = new Map();
  };

  const jonahOf = (id: string): FactoryJonah => {
    let j = byFactory.get(id);
    if (!j) {
      j = {};
      byFactory.set(id, j);
    }
    return j;
  };

  /** The Software factory is seeded; any other factory has no version, so no policy is set and nothing waits. */
  const seeded = (name: string) => name === 'software';

  return {
    reset,
    async handle(org, rest, method, url, res) {
      const m = rest.match(/^\/factories\/([^/]+)\/jonah(?:\/(proposals|reviews))?$/);
      if (!m) return false;
      const f = factory(org.id, decodeURIComponent(m[1] ?? ''));
      if (!f) {
        problem(res, 404, 'Factory not found', 'factory-not-found', m[1]);
        return true;
      }
      const j = jonahOf(f.id);
      const now = clock();
      const route = m[2];

      if (route === undefined && method === 'GET') {
        const waiting = seeded(f.name) ? softwareProposals(now).filter((p) => p.state === 'proposed') : [];
        const body: JonahOverview = {
          policy: seeded(f.name) ? softwarePolicy : defaultPolicy,
          waiting: waiting.length,
          month_cost_usd: seeded(f.name) ? 6.4 : 0,
          observations: seeded(f.name) ? softwareObservations(now) : [],
          ...(j.review ? { last_review: j.review } : {}),
        };
        json(res, 200, body);
        return true;
      }

      if (route === 'proposals' && method === 'GET') {
        const state = url.searchParams.get('state');
        const all = seeded(f.name) ? softwareProposals(now) : [];
        const proposals = all.filter((p) => state === null || p.state === state);
        json(res, 200, { proposals });
        return true;
      }

      if (route === 'reviews' && method === 'POST') {
        if (!seeded(f.name)) {
          problem(res, 404, 'Version not found', 'version-not-found', f.name);
          return true;
        }
        if (j.review && (j.review.state === 'pending' || j.review.state === 'running')) {
          problem(res, 409, 'Review in flight', 'review-in-flight', f.name);
          return true;
        }
        j.review = {
          id: `review-${now}`,
          state: 'pending',
          trigger: 'manual',
          created_at: new Date(now).toISOString(),
        };
        json(res, 202, j.review);
        return true;
      }

      return false;
    },
  };
}
