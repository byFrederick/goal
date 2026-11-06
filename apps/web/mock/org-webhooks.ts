// The organization's webhooks for the mock API (ADR 0040): GET /v1/orgs/{org}/webhooks, as the Webhooks
// screen and Intake → Webhooks read it. acme starts with two: acme-deploys (github, open to the software
// factory, with the trigger "Deploy failed") and uptime (standard, open to nothing yet). Any other
// organization has none. No answer carries a secret's value, only the secret's name. The rest of the
// organization's webhook family is not served (see `unserved` in coverage.test.ts).
import type { ServerResponse } from 'node:http';
import type { components, Factory } from '@goal/api-client';

type Webhook = components['schemas']['Webhook'];

interface Deps {
  clock: () => number;
  factories: (org: string) => Factory[];
  acme: string;
}

export interface MockOrgWebhooks {
  handle(org: string, rest: string, method: string | undefined, res: ServerResponse): boolean;
}

/** The last seven UTC days, oldest first, with the deliveries the seed gives each. */
function days(now: number, delivered: number[]): Webhook['days'] {
  const today = new Date(now);
  return delivered.map((n, i) => ({
    day: new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate() - (6 - i)))
      .toISOString()
      .slice(0, 10),
    delivered: n,
    refused: 0,
  }));
}

export function createOrgWebhooks({ clock, factories, acme }: Deps): MockOrgWebhooks {
  const seeded = (): Webhook[] => {
    const now = clock();
    const software = factories(acme).find((f) => f.name === 'software');
    return [
      {
        id: '3c1e7a52-6f0b-4d8e-9a1c-5b2d7e9f0a01',
        name: 'acme-deploys',
        sender: 'github',
        address: '/hooks/3c1e7a52-6f0b-4d8e-9a1c-5b2d7e9f0a01',
        secret: 'acme-deploys-hook-secret',
        enabled: true,
        token: null,
        openings: software ? [{ factory: software.id, repositories: null }] : [],
        triggers: software ? [{ factory: software.id, factory_name: software.name, trigger: 'Deploy failed' }] : [],
        created_at: '2026-10-01T00:00:00Z',
        days: days(now, [4, 6, 3, 5, 7, 2, 4]),
        pinged_at: '2026-10-01T00:05:00Z',
        setup_events: ['deploy.failed'],
        setup_all_events: false,
      },
      {
        id: '3c1e7a52-6f0b-4d8e-9a1c-5b2d7e9f0a02',
        name: 'uptime',
        sender: 'standard',
        address: '/hooks/3c1e7a52-6f0b-4d8e-9a1c-5b2d7e9f0a02',
        secret: 'uptime-hook-secret',
        enabled: true,
        token: null,
        openings: [],
        triggers: [],
        created_at: '2026-10-20T09:00:00Z',
        days: days(now, [0, 0, 1, 0, 2, 0, 1]),
        pinged_at: null,
        setup_events: [],
        setup_all_events: false,
      },
    ];
  };

  return {
    handle(org, rest, method, res) {
      if (rest !== '/webhooks' || method !== 'GET') return false;
      res.statusCode = 200;
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ webhooks: org === acme ? seeded() : [] }));
      return true;
    },
  };
}
