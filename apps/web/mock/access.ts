// The Access read model for the mock API (ADR 0037 decision 14): GET …/access?by=&factory=. acme starts
// with the picture of the Access screen: four environments (software default and deploy, sre default,
// support default), four MCP servers and five keys, one secret expiring in 3 days and one failed test.
// The times are set from the mock clock, so "Tested 2 min ago" shows whenever the mock runs.
import type { ServerResponse } from 'node:http';
import type { Org } from '@goal/api-client';
import type { Access, AccessRow, AccessStatus, AccessUse, AccessView } from '../src/api/types.ts';

const minuteMs = 60_000;
const hourMs = 3_600_000;
const dayMs = 86_400_000;

interface Deps {
  clock: () => number;
  acme: string;
}

export interface MockAccess {
  handle: (org: Org, rest: string, method: string, url: URL, res: ServerResponse) => boolean;
}

const factoryIds: Record<string, string> = { software: 'f-software', sre: 'f-sre', support: 'f-support' };

const use = (place: string, status: AccessStatus = 'ok', error?: string): AccessUse => {
  const [factory = '', environment = ''] = place.split('/');
  return {
    factory: { id: factoryIds[factory] ?? factory, name: factory },
    environment,
    agents: ['builder'],
    status,
    ...(error ? { test: { at: '', ok: false, error, tools: 0 } } : {}),
  };
};

const places = (list: string[], status?: AccessStatus) => list.map((p) => use(p, status));

/** The rows of the picture, with their times set from `now`. */
function rows(now: number): AccessRow[] {
  const ago = (ms: number) => new Date(now - ms).toISOString();
  return [
    {
      kind: 'mcp',
      name: 'linear',
      uses: places(['software/default']),
      secrets: ['linear-key'],
      status: 'tested',
      tested_at: ago(2 * minuteMs),
    },
    {
      kind: 'mcp',
      name: 'sentry',
      uses: places(['sre/default']),
      secrets: ['sentry-token'],
      status: 'tested',
      tested_at: ago(hourMs),
    },
    {
      kind: 'mcp',
      name: 'context7',
      uses: places(['software/default']),
      secrets: [],
      status: 'tested',
      tested_at: ago(2 * minuteMs),
    },
    {
      kind: 'mcp',
      name: 'playwright',
      uses: places(['software/default', 'support/default']),
      secrets: [],
      status: 'tested',
      tested_at: ago(2 * minuteMs),
    },
    {
      kind: 'env',
      name: 'GH_TOKEN',
      uses: places(['software/default', 'sre/default', 'support/default']),
      secrets: ['github-token'],
      status: 'ok',
      last_used_at: ago(2 * minuteMs),
    },
    {
      kind: 'env',
      name: 'DD_API_KEY',
      uses: places(['software/default', 'sre/default']),
      secrets: ['dd-api-key'],
      status: 'ok',
      last_used_at: ago(3 * hourMs),
    },
    {
      kind: 'env',
      name: 'CLOUDFLARE_API_TOKEN',
      uses: places(['software/deploy'], 'expiring'),
      secrets: ['cloudflare-deploy'],
      status: 'expiring',
      expires_at: new Date(now + 3 * dayMs).toISOString(),
    },
    {
      kind: 'env',
      name: 'ZENDESK_TOKEN',
      uses: [use('support/default', 'failed', '401')],
      secrets: ['zendesk-token'],
      status: 'failed',
      test_error: '401',
    },
    {
      kind: 'file',
      name: '~/.kube/config',
      uses: places(['software/deploy']),
      secrets: ['kube-staging'],
      status: 'ok',
      last_used_at: ago(2 * dayMs),
    },
  ];
}

const attention = new Set<AccessStatus>(['missing', 'failed', 'expired', 'expiring']);
const kindTitles = { mcp: 'MCP servers', env: 'Keys in variables and files', file: 'Keys in variables and files' };

/** The rows grouped as `by` says, the contract's groups. */
function group(list: AccessRow[], by: AccessView): Access['groups'] {
  const groups = new Map<string, { key: string; title: string; rows: AccessRow[] }>();
  const add = (key: string, title: string, row: AccessRow) => {
    const g = groups.get(key) ?? { key, title, rows: [] };
    g.rows.push(row);
    groups.set(key, g);
  };
  for (const row of list) {
    if (by === 'reach') add(row.kind === 'mcp' ? 'mcp' : 'env', kindTitles[row.kind], row);
    else if (by === 'factory') {
      for (const f of new Set(row.uses.map((u) => u.factory.name))) add(f, f, row);
    } else if (row.secrets.length === 0) add('no-key', 'No key', row);
    else for (const s of row.secrets) add(s, s, row);
  }
  return [...groups.values()].sort((a, b) => (by === 'reach' ? 0 : a.title.localeCompare(b.title)));
}

export function createAccess({ clock, acme }: Deps): MockAccess {
  return {
    handle(org, rest, method, url, res) {
      if (rest !== '/access' || method !== 'GET') return false;
      const requested = url.searchParams.get('by');
      const by: AccessView = requested === 'factory' || requested === 'secret' ? requested : 'reach';
      const only = url.searchParams.get('factory');
      const all = org.id === acme ? rows(clock()) : [];
      const list = only
        ? all
            .map((r) => ({ ...r, uses: r.uses.filter((u) => u.factory.name === only || u.factory.id === only) }))
            .filter((r) => r.uses.length > 0)
        : all;
      const answer: Access = {
        by,
        reached: list.length,
        need_attention: list.filter((r) => attention.has(r.status)).length,
        groups: group(list, by),
      };
      res.statusCode = 200;
      res.setHeader('content-type', 'application/json');
      res.end(JSON.stringify(answer));
      return true;
    },
  };
}
