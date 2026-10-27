// A factory's webhook endpoints and their deliveries for the mock API (ADR 0026, the Webhooks screen):
// GET and POST /v1/orgs/{org}/factories/{factory}/webhooks, POST …/webhooks/{endpoint}/rotate, GET
// …/webhooks/{endpoint}/deliveries and …/deliveries/{delivery}. The software factory starts with the
// picture's two endpoints: cf-deploys (two refused deliveries and one created item this week, one
// accepted with no trigger matched, and the trigger "Deploy failed") and uptime (feeds nothing; three
// deliveries kept). A created or rotated secret is answered once and kept nowhere.
import { randomBytes } from 'node:crypto';
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { components, Factory } from '@goal/api-client';

type Schemas = components['schemas'];
type Endpoint = Schemas['WebhookEndpoint'];
type Delivery = Schemas['WebhookDelivery'] & { headers: Record<string, string>; body: string };

interface MockEndpoint {
  endpoint: Endpoint;
  deliveries: Delivery[];
}

export interface MockWebhooks {
  reset(): void;
  handle(
    org: string,
    rest: string,
    method: string | undefined,
    req: IncomingMessage,
    res: ServerResponse,
    body: () => Promise<Record<string, unknown>>,
  ): Promise<boolean>;
}

interface Deps {
  clock: () => number;
  factories: (org: string) => Factory[];
}

const send = (res: ServerResponse, status: number, body: unknown) => {
  res.statusCode = status;
  if (status === 204) return res.end();
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const titles: Record<number, string> = {
  400: 'Bad Request',
  404: 'Not Found',
  409: 'Conflict',
  503: 'Service Unavailable',
};

const refuse = (res: ServerResponse, status: number, detail: string, code: string) =>
  send(res, status, { title: titles[status] ?? 'Error', status, detail, code });

const hours = 3_600_000;

/** A delivery seen `ago` hours before now, with the headers and body the sender posted. */
function delivery(
  now: number,
  id: string,
  ago: number,
  outcome: Delivery['outcome'],
  extra: { reason?: string; item?: { id: string; number: number }; event: string },
): Delivery {
  const body = JSON.stringify({
    event: extra.event,
    service: 'goal-api',
    commit: 'a41f9c2',
    log_url: 'https://dash.example.com/logs/a41f9c2',
  });
  return {
    id,
    received_at: new Date(now - ago * hours).toISOString(),
    outcome,
    reason: extra.reason ?? '',
    item: extra.item ?? null,
    headers: {
      'content-type': 'application/json',
      'user-agent': 'example-sender/1.0',
      'content-length': String(body.length),
    },
    body,
  };
}

function seedCfDeploys(now: number, endpointId: string): MockEndpoint {
  const deliveries = [
    delivery(now, `${endpointId}-d1`, 3, 'refused', { reason: 'signature did not match', event: 'deploy.failed' }),
    delivery(now, `${endpointId}-d2`, 7, 'accepted', { event: 'deploy.succeeded' }),
    delivery(now, `${endpointId}-d3`, 30, 'created', { event: 'deploy.failed', item: { id: 'item-131', number: 131 } }),
    delivery(now, `${endpointId}-d4`, 52, 'accepted', { event: 'deploy.succeeded' }),
    delivery(now, `${endpointId}-d5`, 75, 'refused', { reason: 'signature did not match', event: 'deploy.failed' }),
    delivery(now, `${endpointId}-d6`, 90, 'accepted', { event: 'deploy.succeeded' }),
    delivery(now, `${endpointId}-d7`, 100, 'accepted', { event: 'deploy.succeeded' }),
    delivery(now, `${endpointId}-d8`, 110, 'accepted', { event: 'deploy.succeeded' }),
    delivery(now, `${endpointId}-d9`, 118, 'accepted', { event: 'deploy.succeeded' }),
  ];
  return {
    endpoint: endpointOf(now, endpointId, 'cf-deploys', 'generic', ['Deploy failed'], null, deliveries),
    deliveries,
  };
}

function seedUptime(now: number, endpointId: string): MockEndpoint {
  const deliveries = [
    delivery(now, `${endpointId}-d1`, 10, 'accepted', { event: 'check.up' }),
    delivery(now, `${endpointId}-d2`, 40, 'accepted', { event: 'check.down' }),
    delivery(now, `${endpointId}-d3`, 70, 'accepted', { event: 'check.up' }),
  ];
  return {
    endpoint: endpointOf(now, endpointId, 'uptime', 'generic', [], '2026-10-20T09:00:00Z', deliveries),
    deliveries,
  };
}

/** The endpoint as the contract has it: its triggers, and its last seven UTC days, oldest first. */
function endpointOf(
  now: number,
  id: string,
  name: string,
  kind: Endpoint['kind'],
  triggers: string[],
  rotatedAt: string | null,
  deliveries: Delivery[],
): Endpoint {
  return {
    id,
    name,
    kind,
    address: `/hooks/${id}`,
    secret_name: `${name}-hook-secret`,
    created_at: '2026-10-01T00:00:00Z',
    rotated_at: rotatedAt,
    triggers,
    days: days(now, deliveries),
  };
}

function days(now: number, deliveries: Delivery[]): Endpoint['days'] {
  const today = new Date(now);
  return Array.from({ length: 7 }, (_, i) => {
    const day = new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate() - (6 - i)));
    const key = day.toISOString().slice(0, 10);
    const on = deliveries.filter((d) => d.received_at.slice(0, 10) === key);
    return {
      day: key,
      delivered: on.filter((d) => d.outcome !== 'refused').length,
      refused: on.filter((d) => d.outcome === 'refused').length,
    };
  });
}

export function createWebhooks({ clock, factories }: Deps): MockWebhooks {
  let byFactory = new Map<string, MockEndpoint[]>();
  let seq = 0;

  const reset = () => {
    byFactory = new Map();
    seq = 0;
  };

  /** A factory's endpoints; the software factory of acme starts with the picture's two. */
  const endpointsOf = (factoryId: string, factoryName: string): MockEndpoint[] => {
    const known = byFactory.get(factoryId);
    if (known) return known;
    const now = clock();
    const seeded =
      factoryName === 'software'
        ? [
            seedCfDeploys(now, '3c1e7a52-6f0b-4d8e-9a1c-5b2d7e9f0a01'),
            seedUptime(now, '3c1e7a52-6f0b-4d8e-9a1c-5b2d7e9f0a02'),
          ]
        : [];
    byFactory.set(factoryId, seeded);
    return seeded;
  };

  const factoryOf = (org: string, ref: string) => factories(org).find((f) => f.id === ref || f.name === ref);

  /** The endpoint with its days read now: the mock's clock moves between requests. */
  const view = (e: MockEndpoint): Endpoint => ({ ...e.endpoint, days: days(clock(), e.deliveries) });

  return {
    reset,
    async handle(org, rest, method, _req, res, body) {
      const m = rest.match(/^\/factories\/([^/]+)\/webhooks(?:\/([^/]+)(\/rotate|\/deliveries(?:\/([^/]+))?)?)?$/);
      if (!m) return false;
      const factory = factoryOf(org, decodeURIComponent(m[1] ?? ''));
      if (!factory) {
        refuse(res, 404, 'factory not found', 'factory-not-found');
        return true;
      }
      const list = endpointsOf(factory.id, factory.name);
      const ref = m[2] ? decodeURIComponent(m[2]) : undefined;
      const tail = m[3] ?? '';
      const found = ref ? list.find((e) => e.endpoint.id === ref) : undefined;

      if (!ref) {
        if (method === 'GET') {
          send(res, 200, { endpoints: list.map(view) });
          return true;
        }
        if (method === 'POST') {
          const input = await body();
          const name = typeof input.name === 'string' ? input.name.trim() : '';
          const kind = input.kind === 'github' ? 'github' : input.kind === 'generic' ? 'generic' : undefined;
          if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(name) || !kind) {
            refuse(res, 400, 'the name is not a slug, or the kind is unknown', 'webhook-invalid');
            return true;
          }
          if (list.some((e) => e.endpoint.name === name)) {
            refuse(res, 409, 'a webhook of this factory has this name', 'webhook-name-taken');
            return true;
          }
          const now = clock();
          const id = `3c1e7a52-6f0b-4d8e-9a1c-5b2d7e9f1${String(++seq).padStart(3, '0')}`;
          const created = endpointOf(now, id, name, kind, [], null, []);
          list.push({ endpoint: created, deliveries: [] });
          // The secret is answered here, once: nothing in the mock keeps it.
          send(res, 201, { ...created, secret: randomBytes(32).toString('hex') });
          return true;
        }
        return false;
      }

      if (!found) {
        refuse(res, 404, `webhook endpoint "${ref}" not found`, 'webhook-not-found');
        return true;
      }
      if (tail === '/rotate' && method === 'POST') {
        found.endpoint = { ...found.endpoint, rotated_at: new Date(clock()).toISOString() };
        send(res, 200, { ...view(found), secret: randomBytes(32).toString('hex') });
        return true;
      }
      if (tail === '' && method === 'GET') {
        send(res, 200, view(found));
        return true;
      }
      if (tail === '' && method === 'DELETE') {
        // Its address stops accepting requests, and its deliveries go with it.
        list.splice(list.indexOf(found), 1);
        send(res, 204, undefined);
        return true;
      }
      if (tail === '/deliveries' && method === 'GET') {
        const deliveries = [...found.deliveries]
          .sort((a, b) => b.received_at.localeCompare(a.received_at))
          .slice(0, 5)
          .map(({ headers: _h, body: _b, ...d }) => d);
        send(res, 200, { deliveries, next_after: null });
        return true;
      }
      const one = m[4] ? decodeURIComponent(m[4]) : undefined;
      if (one && method === 'GET') {
        const d = found.deliveries.find((x) => x.id === one);
        if (!d) {
          refuse(res, 404, 'delivery not found', 'webhook-delivery-not-found');
          return true;
        }
        const { headers, body: raw, ...summary } = d;
        send(res, 200, {
          ...summary,
          headers,
          body: raw,
          body_encoding: 'text',
        });
        return true;
      }
      return false;
    },
  };
}
