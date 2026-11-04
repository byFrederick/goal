// Jonah in the mock API (contracts/api/openapi.yaml, tag jonah): a factory's overview (its policy, the proposals
// waiting, the month's cost and the week's observations), its proposals by state, and a manual review. A seeded
// factory (the Software factory, by name) starts with four waiting proposals (7 and 8, memory proposal 9 and model proposal 10), an applied one, four
// observations (one about the line's send-backs) and $6.40 this month. A manual review answers 202 with a queued review, and 409 review-in-flight
// for the next one while it is queued. The Tests tab's kept tests are listed and removed; Undo answers 409
// proposal-not-published, as no seeded proposal is published.
// A proposal's page reads its files and evidence, and its approve, edit and reject record the decision. Proposal 8
// is the flagged case: approving it answers 409 draft-open, as a factory with an open draft does.
import type { ServerResponse } from 'node:http';
import type {
  JonahObservation,
  JonahOverview,
  JonahPolicy,
  JonahReview,
  KeptTest,
  Proposal,
  ProposalEntry,
  ProposalFile,
  ProposalState,
  ProposalTried,
  TriedTry,
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
  /** The kept tests a person removed, by id. */
  removed: Set<string>;
  /** How many times Try again was confirmed on a proposal, by number. */
  tried: Map<number, number>;
  /** The queued review, once a person asked for one. */
  review: JonahReview | undefined;
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
/** The Software factory's four kept tests (tests.jpg), kept by the local user over the last weeks. */
function softwareKeptTests(now: number): KeptTest[] {
  const result = (r: 'passed' | 'failed' | 'error', daysAgo: number, run: string): KeptTest['last_result'] => ({
    result: r,
    at: new Date(now - daysAgo * 86_400_000).toISOString(),
    run_id: run,
  });
  const kept = (
    id: string,
    number: number,
    title: string,
    station: string,
    daysAgo: number,
    last: KeptTest['last_result'] = null,
  ): KeptTest => ({
    id,
    item: { id: `item-${number}`, number, title },
    station,
    kept_by: 'user:local',
    kept_at: new Date(now - daysAgo * 86_400_000).toISOString(),
    last_result: last,
  });
  return [
    kept('kept-1', 451, 'A sidebar request gets the frontend lane', 'spec', 7, result('passed', 2, 'run-451')),
    kept('kept-2', 418, 'Needs you header in the top bar', 'build', 4, result('failed', 1, 'run-418')),
    kept('kept-3', 493, 'Phone fixes on three screens', 'build', 3, result('error', 1, 'run-493')),
    kept('kept-4', 497, 'Board column header meta', 'review', 4),
  ];
}

const softwarePolicy: JonahPolicy = {
  enabled: true,
  propose_after: 5,
  share: 0.05,
  budget_usd: 20,
  model: 'claude-opus-5-5',
  approver_roles: ['owner', 'admin'],
};

const defaultPolicy: JonahPolicy = {
  enabled: true,
  propose_after: 3,
  share: 0.05,
  budget_usd: 10,
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
    updated_at: new Date(now - 2 * 3_600_000).toISOString(),
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
    updated_at: new Date(now - 26 * 3_600_000).toISOString(),
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
    updated_at: new Date(now - 30 * 3_600_000).toISOString(),
  },
  {
    id: 'obs-phone-check',
    summary: 'Review sends back 1 in 4 frontend items for a missing phone check',
    detail: 'The frontend station is sent back more than any other; 4 of the last 16 items lacked a check at 375 px',
    item_refs: ['software#508', 'software#511', 'software#502', 'software#499'],
    state: 'watching',
    count: 4,
    propose_after: 5,
    created_at: new Date(now - 50 * 3_600_000).toISOString(),
    updated_at: new Date(now - 50 * 3_600_000).toISOString(),
  },
];

/** The Software factory's proposals, newest first. */
const softwareProposals = (now: number): ProposalEntry[] => [
  {
    number: 10,
    title: 'frontend-engineer on claude-haiku-5-5',
    state: 'proposed',
    created_at: new Date(now - 20 * 60_000).toISOString(),
    paths: ['factory.yaml'],
    agents: [],
    evidence_count: 0,
  },
  {
    number: 9,
    title: 'Add 3 facts from answers, notes and send-backs',
    state: 'proposed',
    created_at: new Date(now - 24 * 3_600_000).toISOString(),
    paths: ['memory.yaml'],
    agents: [],
    evidence_count: 3,
  },
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
  // The proposal page reads files; its edits come with the Jonah pages (software#600).
  edits: [],
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

/** One try, done and graded fixed. */
const doneTry = (attempt: number): TriedTry => ({ attempt, state: 'done', verdict: 'fixed', cost_usd: 1.4 });

/** What proposal 7 was tried on: two past items, each with its first try and one more per confirmed Try again. */
const triedSeven = (rounds: number): ProposalTried => ({
  state: 'done',
  items: [
    ['software#511', 'Board Done cards overflow'],
    ['software#497', 'Board column header meta'],
  ].map(([ref, title]) => ({
    item_ref: ref as string,
    item_title: title as string,
    station: 'frontend',
    tries: Array.from({ length: rounds + 1 }, (_, i) => doneTry(i + 1)),
  })),
  cost_usd: 2.8 * (rounds + 1),
  seconds: 2460,
  kept_tests: [],
});

/** Try again is refused with learning-budget-exhausted from the third round on. */
const maxRounds = 2;

/** Proposal 8, the flagged case: one file, two runs as evidence. Approving it answers 409 draft-open. */
const proposalEight = (now: number): Proposal => ({
  ...(softwareProposals(now).find((p) => p.number === 8) as ProposalEntry),
  summary: 'Large requests take the longest to review. Splitting them into an assembly shortens that.',
  made_against_version: 21,
  publishes_as_version: 22,
  // The proposal page reads files; its edits come with the Jonah pages (software#600).
  edits: [],
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

/** A memory.yaml of the given facts, each with the item it came from. */
const memoryYaml = (facts: [string, string, string][]): string =>
  facts.map(([text, from, added]) => `- text: ${text}\n  from: [${from}]\n  added: ${added}`).join('\n');

const baseFacts: [string, string, string][] = Array.from({ length: 11 }, (_, i) => [
  `Earlier fact ${i + 1}.`,
  `software#${400 + i}`,
  '2026-10-20',
]);
baseFacts.push([
  'The pre-push hook runs task check with GIT_DIR set, so tests that run git must clear it.',
  'software#524',
  '2026-11-02',
]);

/** Proposal 9 as proposal-memory.jpg shows it: three facts that each quote a person, and no try. */
const proposalNine = (now: number): Proposal => ({
  ...(softwareProposals(now).find((p) => p.number === 9) as ProposalEntry),
  summary: 'Three things people wrote are worth keeping for every run.',
  made_against_version: 40,
  publishes_as_version: 41,
  edits: [],
  files: [
    {
      path: 'memory.yaml',
      base_sha256: 'd4',
      base_content: memoryYaml(baseFacts),
      content: memoryYaml([
        ...baseFacts,
        ['Phones are 390 wide; check 375 too.', 'software#493', '2026-11-03'],
        [
          'The docs site is apps/docs, served at /docs; change it in the same pull request.',
          'software#371',
          '2026-11-03',
        ],
        [
          'An ADR’s number is taken when its branch merges; check main before you number one.',
          'software#308',
          '2026-11-03',
        ],
      ]),
    },
  ] satisfies ProposalFile[],
  evidence: [
    {
      item_ref: 'software#493',
      item_title: 'Phone fixes',
      station: 'An answer',
      quote:
        'frontend-engineer asked: “Which phone width should I check?” Freddy Cruz answered: “Phones are 390 wide; check 375 too.”',
    },
    {
      item_ref: 'software#371',
      item_title: 'Docs for the CLI',
      station: 'A note',
      quote: 'Freddy Cruz: “The docs site is apps/docs, served at /docs. Change it in the same pull request.”',
    },
    {
      item_ref: 'software#308',
      item_title: 'ADR numbering',
      station: 'A send-back',
      quote:
        'Freddy Cruz moved it back: “An ADR’s number is taken when its branch merges; check main before you number one.”',
    },
  ],
});

/** Proposal 10's seven items as proposal-model.jpg shows them: [ref, title, verdicts of the three tries, line result]. */
const modelItems: [string, string, boolean[], string][] = [
  ['software#418', 'Needs you header', [true, true, false], 'passed first time, $0.40'],
  ['software#493', 'Phone fixes', [true, true, true], 'passed first time, $0.50'],
  ['software#511', 'Done cards overflow', [true, false, false], 'sent back 2 times at review, $1.10'],
  ['software#508', 'Item tabs first', [false, false, false], 'sent back once at review, $0.78'],
  ['software#502', 'Tablet details', [true, false, true], 'passed first time, $0.46'],
  ['software#499', 'Trace took column', [false, true, true], 'passed first time, $0.58'],
  ['software#497', 'Board header meta', [true, false, false], 'passed first time, $0.52'],
];

/** Proposal 10: a model change tried three times on each of seven past items, and no estimate, as goald sends it. */
const proposalTen = (now: number): Proposal => ({
  ...(softwareProposals(now).find((p) => p.number === 10) as ProposalEntry),
  summary: 'frontend-engineer on claude-haiku-5-5, tried on its seven newest finished items.',
  made_against_version: 40,
  publishes_as_version: 41,
  edits: [],
  files: [
    {
      path: 'factory.yaml',
      base_sha256: 'e5',
      base_content: [...filler(), '- name: frontend-engineer', '  model: claude-sonnet-5-5'].join('\n'),
      content: [...filler(), '- name: frontend-engineer', '  model: claude-haiku-5-5'].join('\n'),
    },
  ] satisfies ProposalFile[],
  evidence: [],
  tried: {
    state: 'done',
    items: modelItems.map(([ref, title, passes, line]) => ({
      item_ref: ref,
      item_title: title,
      station: 'frontend',
      line_result: line,
      tries: passes.map((fixed, i) => ({
        attempt: i + 1,
        state: 'done' as const,
        verdict: fixed ? ('fixed' as const) : ('not_fixed' as const),
        cost_usd: 0.21,
      })),
    })),
    cost_usd: 4.4,
    seconds: 3900,
    kept_tests: [],
  },
});

/** The detail of a seeded proposal; only 7 to 10 have one, the other numbers answer 404. */
const softwareDetail = (now: number, number: number): Proposal | undefined => {
  if (number === 7) return proposalSeven(now);
  if (number === 8) return proposalEight(now);
  if (number === 9) return proposalNine(now);
  if (number === 10) return proposalTen(now);
  return undefined;
};

/** Proposal 7 with the tries Try again added, so far. */
const withTries = (p: Proposal, rounds: number): Proposal => (p.number === 7 ? { ...p, tried: triedSeven(rounds) } : p);

/** What the next Try again costs: one more try on each of proposal 7's two items. */
const tryEstimate = (rounds: number) => ({
  number: 7,
  items: [
    {
      item_ref: 'software#511',
      item_title: 'Board Done cards overflow',
      station: 'frontend',
      tries: 1,
      run_cost_usd: 1.4,
    },
    {
      item_ref: 'software#497',
      item_title: 'Board column header meta',
      station: 'frontend',
      tries: 1,
      run_cost_usd: 1.4,
    },
  ],
  tries: 2,
  estimate_usd: 2.8,
  budget_left_usd: 20 - 2.8 * (rounds + 1),
});

export function createJonah({ factory, clock }: JonahDeps): MockJonah {
  let byFactory = new Map<string, FactoryJonah>();

  const reset = () => {
    byFactory = new Map();
  };

  const jonahOf = (id: string): FactoryJonah => {
    let j = byFactory.get(id);
    if (!j) {
      j = { decisions: new Map(), removed: new Set(), tried: new Map(), review: undefined };
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
        /^\/factories\/([^/]+)\/jonah(?:\/(proposals|reviews|tests)(?:\/([^/]+)(?:\/(approve|edit|reject|undo|try))?)?)?$/,
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
      const number = m[3] !== undefined && /^\d+$/.test(m[3]) ? Number(m[3]) : undefined;
      const action = m[4];

      if (route === 'tests') {
        const tests = seeded(f.name) ? softwareKeptTests(now).filter((t) => !j.removed.has(t.id)) : [];
        if (m[3] === undefined && method === 'GET') {
          json(res, 200, { tests });
          return true;
        }
        if (m[3] !== undefined && method === 'DELETE') {
          if (!tests.some((t) => t.id === m[3])) {
            problem(res, 404, 'Kept test not found', 'kept-test-not-found', m[3]);
            return true;
          }
          j.removed.add(m[3]);
          res.statusCode = 204;
          res.end();
          return true;
        }
        return false;
      }

      if (route === 'proposals' && number !== undefined) {
        const detail = seeded(f.name) ? softwareDetail(now, number) : undefined;
        if (!detail) {
          problem(res, 404, 'Proposal not found', 'proposal-not-found', m[3]);
          return true;
        }
        const decided = j.decisions.get(number);
        if (action === undefined && method === 'GET') {
          json(res, 200, withTries(withDecision(j, detail), j.tried.get(number) ?? 0));
          return true;
        }
        if (method !== 'POST' || action === undefined) return false;
        if (action === 'undo') {
          problem(res, 409, 'Proposal not published', 'proposal-not-published', m[3]);
          return true;
        }
        if (action === 'try') {
          const rounds = j.tried.get(number) ?? 0;
          const confirm = (await readBody()).confirm === true;
          if (decided) {
            problem(res, 409, 'Proposal already decided', 'proposal-decided', m[3]);
          } else if (number !== 7) {
            problem(res, 409, 'Proposal not tried', 'proposal-not-tried', m[3]);
          } else if (rounds >= maxRounds) {
            problem(res, 409, 'Jonah’s budget is used up', 'learning-budget-exhausted', m[3]);
          } else {
            const estimate = tryEstimate(rounds);
            if (confirm) j.tried.set(number, rounds + 1);
            json(res, confirm ? 201 : 200, {
              estimate,
              ...(confirm ? { proposal: withTries(withDecision(j, detail), rounds + 1) } : {}),
            });
          }
          return true;
        }
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
        if (j.review) {
          problem(res, 409, 'A review is in flight', 'review-in-flight', 'A review is pending or running.');
          return true;
        }
        j.review = { id: 'review-1', state: 'pending', trigger: 'manual', created_at: new Date(now).toISOString() };
        json(res, 202, j.review);
        return true;
      }

      return false;
    },
  };
}
