// The standing rules for the mock API (ADR 0037 decision 8): GET …/standing-rules (the active ones, the
// revoked too with ?revoked=true) and DELETE …/standing-rules/{rule}, refusing as goald does: a fresh
// sign-in, 404 for an unknown rule, 409 standing-rule-revoked for one revoked already. acme starts with
// the rule of the Environments picture: create_comment on linear of the goal factory's default environment,
// for architect; the factory is resolved by name to its id when answered.
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Actor, Factory, Org } from '@goal/api-client';
import type { StandingRule } from '../src/api/types.ts';

interface Deps {
  clock: () => number;
  actor: (req: IncomingMessage) => Actor;
  stale: (req: IncomingMessage, res: ServerResponse) => boolean;
  factories: (org: string) => Factory[];
  acme: string;
}

export interface MockStandingRules {
  reset: () => void;
  handle: (org: Org, rest: string, method: string, url: URL, req: IncomingMessage, res: ServerResponse) => boolean;
}

interface Seed {
  id: string;
  factory: string;
  environment: string;
  server: string;
  tool: string;
  agent: string;
  by: string;
  at: string;
  revoked?: boolean;
}

const seeds: Seed[] = [
  {
    id: 'rule-linear-create-comment',
    factory: 'goal',
    environment: 'default',
    server: 'linear',
    tool: 'create_comment',
    agent: 'architect',
    by: 'Freddy Cruz',
    at: '2026-10-30T09:12:00Z',
  },
  {
    id: 'rule-linear-update-issue',
    factory: 'goal',
    environment: 'default',
    server: 'linear',
    tool: 'update_issue_status',
    agent: 'frontend-engineer',
    by: 'Freddy Cruz',
    at: '2026-10-28T14:40:00Z',
    revoked: true,
  },
];

const send = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const refuse = (res: ServerResponse, status: number, detail: string, code: string) =>
  send(res, status, { title: { 404: 'Not Found', 409: 'Conflict' }[status] ?? 'Error', status, detail, code });

export function createStandingRules(deps: Deps): MockStandingRules {
  const { clock, actor, stale, factories, acme } = deps;
  let rules: StandingRule[] = [];

  const reset = () => {
    const goal = factories(acme).find((f) => f.name === 'goal');
    rules = seeds.map((s) => ({
      id: s.id,
      connection_id: null,
      connection: null,
      factory_id: goal?.id ?? s.factory,
      environment: s.environment,
      server: s.server,
      tool: s.tool,
      agent: s.agent,
      created_by: { kind: 'user', id: 'freddy', name: s.by },
      created_at: s.at,
      item_id: 'item-1',
      revoked_by: s.revoked ? { kind: 'user', id: 'freddy', name: s.by } : null,
      revoked_at: s.revoked ? '2026-10-31T10:00:00Z' : null,
    }));
  };

  return {
    reset,
    handle(org, rest, method, url, req, res) {
      if (org.id !== acme) return false;
      if (rest === '/standing-rules' && method === 'GET') {
        if (rules.length === 0) reset();
        const all = url.searchParams.get('revoked') === 'true';
        send(res, 200, { rules: rules.filter((r) => all || r.revoked_at === null) });
        return true;
      }
      const m = /^\/standing-rules\/([^/]+)$/.exec(rest);
      if (!m || method !== 'DELETE') return false;
      if (stale(req, res)) return true;
      if (rules.length === 0) reset();
      const rule = rules.find((r) => r.id === decodeURIComponent(m[1] ?? ''));
      if (!rule) {
        refuse(res, 404, 'no such standing rule', 'standing-rule-not-found');
        return true;
      }
      if (rule.revoked_at !== null) {
        refuse(res, 409, 'this rule was revoked already', 'standing-rule-revoked');
        return true;
      }
      rule.revoked_at = new Date(clock()).toISOString();
      rule.revoked_by = actor(req);
      send(res, 200, rule);
      return true;
    },
  };
}
