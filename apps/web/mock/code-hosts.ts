// The organization's code hosts for the mock API (ADR 0041): GET and PUT …/code-hosts. github.com and
// gitlab.com are built in; PUT replaces the added hosts. acme starts with one added host with a long name,
// so the layout specs measure a row that has to wrap or truncate.
import type { ServerResponse } from 'node:http';
import type { Org } from '@goal/api-client';

interface Host {
  host: string;
  kind: 'github' | 'gitlab';
}

export interface MockCodeHosts {
  reset: () => void;
  handle: (
    org: Org,
    rest: string,
    method: string,
    res: ServerResponse,
    body: () => Promise<Record<string, unknown>>,
  ) => Promise<boolean>;
}

const send = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const apiBase = (h: Host) => {
  if (h.kind === 'gitlab') return `https://${h.host}/api/v4`;
  if (h.host === 'github.com') return 'https://api.github.com';
  return `https://${h.host}/api/v3`;
};

export function createCodeHosts({ acme }: { acme: string }): MockCodeHosts {
  let added = new Map<string, Host[]>();
  const reset = () => {
    added = new Map([[acme, [{ host: 'git.a-very-long-subdomain.example.com', kind: 'github' }]]]);
  };
  reset();

  const answer = (org: string) => {
    const built: Host[] = [
      { host: 'github.com', kind: 'github' },
      { host: 'gitlab.com', kind: 'gitlab' },
    ];
    const extra = [...(added.get(org) ?? [])].sort((a, b) => a.host.localeCompare(b.host));
    return {
      hosts: [
        ...built.map((h) => ({ ...h, api_base: apiBase(h), built_in: true })),
        ...extra.map((h) => ({ ...h, api_base: apiBase(h), built_in: false })),
      ],
    };
  };

  const handle: MockCodeHosts['handle'] = async (org, rest, method, res, body) => {
    if (rest !== '/code-hosts') return false;
    if (method === 'GET') {
      send(res, 200, answer(org.id));
      return true;
    }
    if (method !== 'PUT') return false;
    const input = await body().catch(() => ({}) as Record<string, unknown>);
    const list = Array.isArray(input.hosts) ? (input.hosts as Host[]) : [];
    const next: Host[] = [];
    for (const h of list) {
      const host = String(h.host ?? '')
        .trim()
        .toLowerCase();
      if (host === 'github.com' || host === 'gitlab.com') {
        send(res, 400, {
          title: 'Bad Request',
          status: 400,
          detail: `${host} is built in`,
          code: 'code-host-built-in',
        });
        return true;
      }
      if (!/^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$/.test(host)) {
        send(res, 400, { title: 'Bad Request', status: 400, detail: 'not a DNS name', code: 'code-host-invalid' });
        return true;
      }
      if (!next.some((n) => n.host === host)) next.push({ host, kind: h.kind === 'gitlab' ? 'gitlab' : 'github' });
    }
    added.set(org.id, next);
    send(res, 200, answer(org.id));
    return true;
  };

  return { reset, handle };
}
