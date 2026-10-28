// Jonah in the mock API (contracts/api/openapi.yaml, tag jonah): a factory's overview (its policy, the proposals
// waiting, the month's cost and the week's observations), its proposals by state, and a manual review. A seeded
// factory (the Software factory, by name) starts with the picture's two waiting proposals, an applied one, four
// observations and $6.40 this month; every other factory has no version yet. A manual review answers 409
// review-loop-not-running for every known factory, as goald will until Jonah's review loop ships (software#344):
// no review is stored.
// A proposal's page reads its files and evidence, and its approve, edit and reject record the decision. Proposal 8
// is the flagged case: approving it answers 409 draft-open, as a factory with an open draft does.
import type { ServerResponse } from 'node:http';
import type {
  JonahObservation,
  JonahOverview,
  JonahPolicy,
  Proposal,
  ProposalEntry,
  ProposalFile,
  ProposalState,
} from '../src/api/types.ts';

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
  /** The decisions made on proposals, by number: what a person approved, edited or rejected. */
  decisions: Map<number, Decision>;
}

/** A decision on a proposal: its new state and what the decision left behind. */
interface Decision {
  state: ProposalState;
  published_version?: number;
  reject_reason?: string;
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
    paths: ['agents/frontend-engineer.md', 'rules/evidence.md'],
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

/** Lines 1 to 40 of a file the mock does not need to show: the diff only shows its context. */
const filler = (): string[] => Array.from({ length: 40 }, (_, i) => `Line ${i + 1}`);

/** Proposal 7 as the picture shows it: two files, four sent-back runs as evidence (proposal.jpg). */
const proposalSeven = (now: number): Proposal => ({
  ...(softwareProposals(now).find((p) => p.number === 7) as ProposalEntry),
  summary: 'Four UI changes came back for want of a screenshot. The frontend engineer attaches one per changed screen.',
  made_against_version: 21,
  publishes_as_version: 22,
  files: [
    {
      path: 'agents/frontend-engineer.md',
      base_sha256: 'a1',
      base_content: [
        ...filler(),
        '## Before you open a pull request',
        '',
        'Run task check:changed and fix what fails.',
        'Then open the pull request with gh pr create.',
      ].join('\n'),
      content: [
        ...filler(),
        '## Before you open a pull request',
        '',
        'Run task check:changed and fix what fails.',
        'For every change under apps/web, attach a screenshot of each changed screen,',
        'in the light and the dark theme, to the pull request description.',
        'Then open the pull request with gh pr create.',
      ].join('\n'),
    },
    {
      path: 'rules/evidence.md',
      base_sha256: 'b2',
      base_content: [
        ...filler(),
        '- Tests: the command you ran and its result.',
        '- Migrations: proof they apply from zero.',
      ].join('\n'),
      content: [
        ...filler(),
        '- Tests: the command you ran and its result.',
        '- UI changes: a screenshot per changed screen, light and dark.',
        '- Migrations: proof they apply from zero.',
      ].join('\n'),
    },
  ] satisfies ProposalFile[],
  evidence: [
    {
      item_ref: 'software#149',
      item_title: 'Empty state for Intake',
      station: 'review',
      quote: "No screenshot, so I can't check the empty state against the design.",
    },
    {
      item_ref: 'software#138',
      item_title: 'Andon dialog copy and focus order',
      station: 'review',
      quote: 'Show the dialog in both themes; the focus ring is not visible in the description.',
    },
    {
      item_ref: 'software#131',
      item_title: 'Org switcher hover states',
      station: 'review',
      quote: 'Criterion 3 says it works in dark mode; there is no evidence of that.',
    },
    {
      item_ref: 'software#127',
      item_title: 'Board card spacing',
      station: 'review',
      quote: 'Attach a screenshot of the board after the change.',
    },
  ],
});

/** Proposal 8, the flagged case: one file, two runs as evidence. Approving it answers 409 draft-open. */
const proposalEight = (now: number): Proposal => ({
  ...(softwareProposals(now).find((p) => p.number === 8) as ProposalEntry),
  summary: 'Large requests take the longest to review. Splitting them into an assembly shortens that.',
  made_against_version: 21,
  publishes_as_version: 22,
  files: [
    {
      path: 'agents/architect.md',
      base_sha256: 'c3',
      base_content: 'Plan the change in one item.',
      content: 'Split a request over 400 changed lines into an assembly of parts.',
    },
  ],
  evidence: [
    {
      item_ref: 'software#150',
      item_title: 'Settings tabs',
      station: 'review',
      quote: 'Too big to review in one pass.',
    },
    { item_ref: 'software#142', item_title: 'Board filters', station: 'review', quote: 'Split this into parts.' },
  ],
});

/** The detail of a seeded proposal; only 7 and 8 have one, the other numbers answer 404. */
const softwareDetail = (now: number, number: number): Proposal | undefined => {
  if (number === 7) return proposalSeven(now);
  if (number === 8) return proposalEight(now);
  return undefined;
};

export function createJonah({ factory, clock }: JonahDeps): MockJonah {
  let byFactory = new Map<string, FactoryJonah>();

  const reset = () => {
    byFactory = new Map();
  };

  const jonahOf = (id: string): FactoryJonah => {
    let j = byFactory.get(id);
    if (!j) {
      j = { decisions: new Map() };
      byFactory.set(id, j);
    }
    return j;
  };

  /** The Software factory is seeded; any other factory has no version, so no policy is set and nothing waits. */
  const seeded = (name: string) => name === 'software';

  /** A proposal as the factory's record has it now: its state moved by any decision a person made. */
  const withDecision = <T extends ProposalEntry>(j: FactoryJonah, p: T): T => {
    const d = j.decisions.get(p.number);
    if (!d) return p;
    return {
      ...p,
      state: d.state,
      ...(d.published_version === undefined ? {} : { published_version: d.published_version }),
      ...(d.reject_reason === undefined ? {} : { reject_reason: d.reject_reason }),
    };
  };

  return {
    reset,
    async handle(org, rest, method, url, res, readBody) {
      const m = rest.match(
        /^\/factories\/([^/]+)\/jonah(?:\/(proposals|reviews)(?:\/(\d+)(?:\/(approve|edit|reject))?)?)?$/,
      );
      if (!m) return false;
      const f = factory(org.id, decodeURIComponent(m[1] ?? ''));
      if (!f) {
        problem(res, 404, 'Factory not found', 'factory-not-found', m[1]);
        return true;
      }
      const j = jonahOf(f.id);
      const now = clock();
      const route = m[2];
      const number = m[3] === undefined ? undefined : Number(m[3]);
      const action = m[4];

      if (route === 'proposals' && number !== undefined) {
        const detail = seeded(f.name) ? softwareDetail(now, number) : undefined;
        if (!detail) {
          problem(res, 404, 'Proposal not found', 'proposal-not-found', m[3]);
          return true;
        }
        const decided = j.decisions.get(number);
        if (action === undefined && method === 'GET') {
          json(res, 200, withDecision(j, detail));
          return true;
        }
        if (method !== 'POST' || action === undefined) return false;
        if (decided) {
          problem(res, 409, 'Proposal already decided', 'proposal-decided', m[3]);
          return true;
        }
        if (action === 'approve') {
          if (number === 8) {
            problem(res, 409, 'Draft open', 'draft-open', 'The factory has an open draft.');
            return true;
          }
          j.decisions.set(number, { state: 'published', published_version: 22 });
          json(res, 200, { number, state: 'published', version: 22 });
          return true;
        }
        if (action === 'edit') {
          j.decisions.set(number, { state: 'applied' });
          json(res, 200, { number, state: 'applied', draft_revision: 1 });
          return true;
        }
        const body = await readBody();
        const reason = typeof body.reason === 'string' ? body.reason.trim() : '';
        if (reason === '' || (body.reason as string).length > 2000) {
          problem(res, 400, 'Invalid request', 'invalid-request', 'A reason is required.');
          return true;
        }
        j.decisions.set(number, { state: 'rejected', reject_reason: body.reason as string });
        json(res, 200, { number, state: 'rejected' });
        return true;
      }

      if (route === undefined && method === 'GET') {
        const waiting = seeded(f.name)
          ? softwareProposals(now)
              .map((p) => withDecision(j, p))
              .filter((p) => p.state === 'proposed')
          : [];
        const body: JonahOverview = {
          policy: seeded(f.name) ? softwarePolicy : defaultPolicy,
          waiting: waiting.length,
          month_cost_usd: seeded(f.name) ? 6.4 : 0,
          observations: seeded(f.name) ? softwareObservations(now) : [],
        };
        json(res, 200, body);
        return true;
      }

      if (route === 'proposals' && method === 'GET') {
        const state = url.searchParams.get('state');
        const all = seeded(f.name) ? softwareProposals(now).map((p) => withDecision(j, p)) : [];
        const proposals = all.filter((p) => state === null || p.state === state);
        json(res, 200, { proposals });
        return true;
      }

      if (route === 'reviews' && method === 'POST') {
        problem(
          res,
          409,
          'Review loop not running',
          'review-loop-not-running',
          "Reviews start when Jonah's review loop ships",
        );
        return true;
      }

      return false;
    },
  };
}
