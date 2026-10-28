// Jonah's judges in the mock API (contracts/api/openapi.yaml, tag judges): a factory's judges of its current
// version with their last 30 days, one judge with its file and newest scores, a judge saved or switched
// (each publishes a new version, as the contract says), a judge added, and a try queued. A seeded factory
// (fixtures.ts's softwareJudges) starts with the four judges of the judges picture; every other factory has
// none (version null). The mock runs no scores: a try is queued and stays so.
import type { ServerResponse } from 'node:http';
import type { Org } from '@goal/api-client';
import type { CreateJudgeRequest, Judge, JudgeList, JudgeListEntry, JudgeTries, JudgeWrite } from '../src/api/types.ts';
import { softwareJudges, softwareJudgeVersion } from './fixtures.ts';

export interface JudgesDeps {
  /** The organization's factories by id or name, as goald's lookup reads them. */
  factory: (org: string, key: string) => { id: string; name: string } | undefined;
}

export interface MockJudges {
  /** Serves a judges route; false when rest is none of them. */
  handle: (
    org: Org,
    rest: string,
    method: string,
    res: ServerResponse,
    readBody: () => Promise<Record<string, unknown>>,
  ) => Promise<boolean>;
  reset: () => void;
}

interface FactoryJudges {
  /** The current version's number; null before the first publish. */
  version: number | null;
  judges: Judge[];
}

const json = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const problem = (res: ServerResponse, status: number, title: string, code: string, detail?: string) =>
  json(res, status, { title, status, code, ...(detail ? { detail } : {}) });

const slug = /^[a-z][a-z0-9-]{2,39}$/;

/** The list entry a judge is in the table: its file without the rubric and the outcomes. */
const entryOf = ({ slug, title, criterion, agents, sample, pass_mark, enabled, summary }: Judge): JudgeListEntry => ({
  slug,
  title,
  criterion,
  agents,
  sample,
  pass_mark,
  enabled,
  summary,
});

const totals = (judges: readonly Judge[]) =>
  Math.round(judges.reduce((sum, j) => sum + j.summary.cost_usd, 0) * 100) / 100;

export function createJudges({ factory }: JudgesDeps): MockJudges {
  let byFactory = new Map<string, FactoryJudges>();
  // The factories whose seed is in place: a seeded factory is seeded once after each reset.
  const seeded = new Set<string>();

  const reset = () => {
    byFactory = new Map();
    seeded.clear();
  };

  // A factory's judges: the Software factory (by name) starts with the picture's four, once per reset.
  const judgesOf = (id: string, name: string): FactoryJudges => {
    let j = byFactory.get(id);
    if (!j) {
      j = { version: null, judges: [] };
      byFactory.set(id, j);
    }
    if (name === 'software' && !seeded.has(id)) {
      seeded.add(id);
      j.version = softwareJudgeVersion;
      j.judges = softwareJudges.map((judge) => ({ ...judge, recent: [...judge.recent] }));
    }
    return j;
  };

  // A publish takes the next version number, as goald's draft does.
  const publish = (j: FactoryJudges) => {
    j.version = (j.version ?? 0) + 1;
  };

  const withVersion = (judge: Judge, version: number | null): Judge => ({
    ...judge,
    version: version ?? judge.version,
  });

  return {
    reset,
    async handle(org, rest, method, res, readBody) {
      const m = rest.match(/^\/factories\/([^/]+)\/judges(?:\/([^/]+)(\/try)?)?$/);
      if (!m) return false;
      const f = factory(org.id, decodeURIComponent(m[1] ?? ''));
      if (!f) {
        problem(res, 404, 'Factory not found', 'factory-not-found', m[1]);
        return true;
      }
      const j = judgesOf(f.id, f.name);
      const name = m[2] === undefined ? undefined : decodeURIComponent(m[2]);

      if (name === undefined && method === 'GET') {
        json(res, 200, {
          version: j.version,
          judges: j.judges.map(entryOf),
          judge_cost_usd: totals(j.judges),
        } satisfies JudgeList);
        return true;
      }
      if (name === undefined && method === 'POST') {
        const body = (await readBody()) as Partial<CreateJudgeRequest>;
        if (!body.slug || !slug.test(body.slug)) {
          problem(res, 422, 'Invalid judge name', 'definition-invalid', 'The file name must be 3 to 40 characters');
          return true;
        }
        if (j.judges.some((judge) => judge.slug === body.slug)) {
          problem(res, 409, 'Judge exists', 'judge-exists', body.slug);
          return true;
        }
        const judge = fromWrite(body.slug, body as CreateJudgeRequest);
        j.judges.push(judge);
        publish(j);
        json(res, 201, withVersion(judge, j.version));
        return true;
      }

      if (name === undefined) return false;
      const found = j.judges.find((judge) => judge.slug === name);
      if (!found) {
        problem(res, 404, 'Judge not found', 'judge-not-found', name);
        return true;
      }
      if (m[3] === undefined && method === 'GET') {
        json(res, 200, withVersion(found, j.version));
        return true;
      }
      if (m[3] === undefined && method === 'PUT') {
        const write = (await readBody()) as unknown as JudgeWrite;
        const next = fromWrite(name, write, found);
        j.judges = j.judges.map((judge) => (judge.slug === name ? next : judge));
        publish(j);
        json(res, 200, withVersion(next, j.version));
        return true;
      }
      if (m[3] !== undefined && method === 'POST') {
        // Ten queued tries on the newest runs; the mock finishes none of them.
        const tries: JudgeTries = {
          tries: Array.from({ length: 10 }, (_, i) => ({
            id: `try-${name}-${i + 1}`,
            state: 'queued',
            item_ref: `${f.name}#${149 - i}`,
            item_title: `Recent run ${i + 1}`,
          })),
        };
        json(res, 202, tries);
        return true;
      }
      return false;
    },
  };
}

/** A judge as its file reads after a write: the written fields, its summary and scores kept. */
function fromWrite(slug: string, write: JudgeWrite | CreateJudgeRequest, before?: Judge): Judge {
  return {
    slug,
    title: write.title,
    criterion: write.criterion,
    agents: write.agents ?? [],
    outcomes: write.outcomes,
    pass_mark: write.pass_mark,
    sample: write.sample,
    ...(write.model ? { model: write.model } : {}),
    enabled: write.enabled ?? true,
    rubric: write.rubric,
    version: before?.version ?? 0,
    summary: before?.summary ?? { scored: 0, passed: 0, pass_rate: 0, cost_usd: 0, days: [] },
    recent: before?.recent ?? [],
    runs_on: before?.runs_on ?? { size: 'small', connections: [], sees: ['spec'], inherited: true },
  };
}
