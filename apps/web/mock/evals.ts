// Jonah's evals in the mock API (contracts/api/openapi.yaml, tag evals): a factory's cases of its
// current version with their last tries, the estimate of a run, the runs started and the case added
// from a past run. A seeded factory (fixtures.ts's softwareEvalCases) starts with the five cases the
// Jonah evals picture shows; every other factory has no cases. A run is queued and stays so: the mock
// runs no tries. Adding a case takes a new version number, as a publish does.
import type { ServerResponse } from 'node:http';
import type { EvalCaseEntry, EvalCaseFromRun, EvalEstimate, EvalList, EvalRun, Org, Version } from '@goal/api-client';
import { softwareEvalCases, softwareEvalVersion } from './fixtures.ts';

export interface EvalsDeps {
  clock: () => number;
  /** The organization's factories by id or name, as goald's lookup reads them. */
  factory: (org: string, key: string) => { id: string; name: string } | undefined;
}

export interface MockEvals {
  /** Serves an evals route; false when rest is none of them. */
  handle: (
    org: Org,
    rest: string,
    method: string,
    res: ServerResponse,
    readBody: () => Promise<Record<string, unknown>>,
  ) => Promise<boolean>;
  reset: () => void;
}

interface FactoryEvals {
  version: number;
  cases: EvalCaseEntry[];
  runs: EvalRun[];
  /** The newest run's time, when the picture says "Today 09:12". */
  latestRunAt: string;
}

const slug = /^[a-z0-9-]{3,40}$/;

const json = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const problem = (res: ServerResponse, status: number, title: string, detail?: string, code?: string) =>
  json(res, status, { title, status, ...(detail ? { detail } : {}), ...(code ? { code } : {}) });

/** The cost of one run of the cases: each case's latest recorded cost, summed. */
const priced = (cases: readonly EvalCaseEntry[]): EvalEstimate => ({
  usd: Math.round(cases.reduce((sum, c) => sum + (c.cost_usd ?? 0), 0) * 100) / 100,
  unpriced: cases.filter((c) => c.cost_usd === undefined).map((c) => c.name),
});

export function createEvals({ clock, factory }: EvalsDeps): MockEvals {
  let byFactory = new Map<string, FactoryEvals>();
  // The factories whose seed is in place: a seeded factory is seeded once after each reset.
  const seeded = new Set<string>();

  const reset = () => {
    byFactory = new Map();
    seeded.clear();
  };

  // A factory's evals: a seeded factory (by name) starts with its cases the first time it is read.
  const evalsOf = (id: string, name: string): FactoryEvals => {
    let e = byFactory.get(id);
    if (!e) {
      e = { version: 0, cases: [], runs: [], latestRunAt: new Date(clock()).toISOString() };
      byFactory.set(id, e);
    }
    if (name === 'software' && !seeded.has(id)) {
      seeded.add(id);
      e.version = softwareEvalVersion;
      e.cases = softwareEvalCases.map((c) => ({ ...c, last_tries: [...c.last_tries] }));
      e.latestRunAt = todayAt(clock(), 9, 12);
    }
    return e;
  };

  const summary = (e: FactoryEvals): EvalList['summary'] => {
    const tries = e.cases.flatMap((c) => c.last_tries);
    const passed = tries.filter((t) => t.state === 'pass').length;
    return {
      ...(e.version > 0 ? { version: e.version } : {}),
      cases: e.cases.length,
      cases_passing: e.cases.filter((c) => c.last_tries.length > 0 && c.last_tries.every((t) => t.state === 'pass'))
        .length,
      tries: tries.length,
      tries_passed: passed,
      ...(tries.length > 0 ? { first_try_pass_rate: passed / tries.length } : {}),
      latest_run_id: e.runs.at(-1)?.id ?? 'run-9',
      latest_run_at: e.latestRunAt,
      latest_tries_per_case: Math.max(0, ...e.cases.map((c) => c.last_tries.length)),
    };
  };

  return {
    reset,
    async handle(org, rest, method, res, readBody) {
      const m = rest.match(/^\/factories\/([^/]+)\/evals(\/.*)?$/);
      if (!m) return false;
      const f = factory(org.id, decodeURIComponent(m[1] ?? ''));
      if (!f) {
        problem(res, 404, 'Factory not found', m[1], 'factory-not-found');
        return true;
      }
      const e = evalsOf(f.id, f.name);
      const sub = m[2] ?? '';

      if (sub === '' && method === 'GET') {
        json(res, 200, { summary: summary(e), cases: e.cases } satisfies EvalList);
        return true;
      }
      if (sub === '/estimate' && method === 'POST') {
        const body = (await readBody()) as Partial<{ case: string }>;
        const cases = body.case ? e.cases.filter((c) => c.name === body.case) : e.cases;
        json(res, 200, priced(cases));
        return true;
      }
      if (sub === '/runs' && method === 'POST') {
        const body = (await readBody()) as { case?: string };
        if (body.case !== undefined && !e.cases.some((c) => c.name === body.case)) {
          problem(res, 400, 'Bad Request', `no case ${body.case}`, 'eval-case-unknown');
          return true;
        }
        const cases = body.case ? e.cases.filter((c) => c.name === body.case) : e.cases;
        const estimate = priced(cases);
        const run: EvalRun = {
          id: `run-${e.runs.length + 100}`,
          state: 'queued',
          trigger: 'manual',
          ...(body.case ? { case: body.case } : {}),
          version: e.version,
          content_sha256: 'mock',
          estimate_usd: estimate.usd,
          cost_usd: 0,
          created_at: new Date(clock()).toISOString(),
          results: [],
        };
        e.runs.push(run);
        json(res, 202, { run, estimate });
        return true;
      }
      if (sub === '/cases' && method === 'POST') {
        const body = (await readBody()) as Partial<EvalCaseFromRun>;
        const name = String(body.name ?? '');
        if (!slug.test(name)) {
          problem(
            res,
            400,
            'Bad Request',
            'the name is 3 to 40 lowercase letters, digits and hyphens',
            'invalid-request',
          );
          return true;
        }
        if (e.cases.some((c) => c.name === name)) {
          problem(res, 409, 'Conflict', `the version has a case ${name}`, 'eval-case-exists');
          return true;
        }
        const at = new Date(clock()).toISOString();
        e.cases.push({
          name,
          description: body.description ?? 'Added from a past run',
          agent: 'reviewer',
          tries: body.tries ?? 1,
          checked_by: body.judge ? { kind: 'judge', judge: body.judge } : { kind: 'contract' },
          last_tries: [],
        });
        e.version += 1;
        e.latestRunAt = at;
        const version: Version = {
          id: `version-${e.version}`,
          number: e.version,
          revision: 0,
          state: 'published',
          message: `Add eval case ${name}`,
          source: { kind: 'edit' },
          published_at: at,
          published_by: { kind: 'user', id: 'local', name: 'Local user' },
          current: true,
          counts: { stations: 0, agents: 0, skills: 0, connections: 0, contracts: 0, changes: 1 },
        };
        json(res, 201, version);
        return true;
      }
      problem(res, 404, 'Not Found', rest);
      return true;
    },
  };
}

/** The given local hour and minute of the day of `ms`, as an ISO time. */
function todayAt(ms: number, hour: number, minute: number): string {
  const d = new Date(ms);
  d.setHours(hour, minute, 0, 0);
  return d.toISOString();
}
