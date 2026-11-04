// The organization's notification settings for the mock API (ADR 0037): GET and PUT
// …/settings/notifications, the secret that holds the Slack bot token. PUT stores a secret name or null and
// refuses an unknown name as goald does (404 secret-not-found); org:manage is checked by the permission table
// before the request gets here. acme starts with slack-bot, which mock/secrets.ts seeds.
import type { ServerResponse } from 'node:http';
import type { Org } from '@goal/api-client';
import type { Secret } from '../src/api/types.ts';

interface Deps {
  /** The organization's secrets, metadata only. */
  secrets: (org: string) => Secret[];
  acme: string;
}

export interface MockOrgNotifications {
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

export function createOrgNotifications({ secrets, acme }: Deps): MockOrgNotifications {
  let slack = new Map<string, string | null>();
  const reset = () => {
    slack = new Map([[acme, 'slack-bot']]);
  };
  reset();

  const handle: MockOrgNotifications['handle'] = async (org, rest, method, res, body) => {
    if (rest === '/settings/notifications/test') {
      if (method !== 'POST') return false;
      // Tests the named secret, else the saved one; saves nothing and never answers a secret's value.
      const input = await body().catch(() => ({}) as Record<string, unknown>);
      const given = input.slack_secret;
      if (given !== undefined && given !== null && typeof given !== 'string') {
        send(res, 400, {
          title: 'Bad Request',
          status: 400,
          detail: 'slack_secret is a name or null',
          code: 'invalid',
        });
        return true;
      }
      const name = typeof given === 'string' ? given : given === null ? null : (slack.get(org.id) ?? null);
      if (name === null) {
        send(res, 200, { ok: false, error: 'slack-not-set' });
        return true;
      }
      const found = secrets(org.id).find((s) => s.name === name);
      if (!found) {
        send(res, 404, {
          title: 'Not Found',
          status: 404,
          detail: `no secret named ${name}`,
          code: 'secret-not-found',
        });
        return true;
      }
      if (!found.scope.all_factories) {
        send(res, 400, {
          title: 'Bad Request',
          status: 400,
          detail: `secret ${name} is limited to some factories`,
          code: 'secret-not-allowed',
        });
        return true;
      }
      send(res, 200, { ok: true, workspace: 'Acme' });
      return true;
    }
    if (rest !== '/settings/notifications') return false;
    if (method === 'GET') {
      send(res, 200, { slack_secret: slack.get(org.id) ?? null });
      return true;
    }
    if (method !== 'PUT') return false;
    const value = (await body()).slack_secret;
    if (value !== null && typeof value !== 'string') {
      send(res, 400, { title: 'Bad Request', status: 400, detail: 'slack_secret is a name or null', code: 'invalid' });
      return true;
    }
    if (value !== null && !secrets(org.id).some((s) => s.name === value)) {
      send(res, 404, {
        title: 'Not Found',
        status: 404,
        detail: `no secret named ${value}`,
        code: 'secret-not-found',
      });
      return true;
    }
    slack.set(org.id, value);
    send(res, 200, { slack_secret: value });
    return true;
  };
  return { reset, handle };
}
