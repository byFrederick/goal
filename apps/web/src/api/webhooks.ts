// A factory's webhook endpoints and their deliveries (ADR 0026; the Webhooks screen) over client.ts's
// goald client. A created or rotated endpoint's signing secret is in its answer once: the callers
// show it in a dialog and never put the answer in the query cache (features/webhooks). Creating and
// rotating need a sign-in from the last 10 minutes; the client sends the request once more after
// the re-auth dialog (session.ts).
import type { components } from '@goal/api-client';
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';

type Schemas = components['schemas'];

export type WebhookKind = Schemas['WebhookKind'];
export type WebhookEndpoint = Schemas['WebhookEndpoint'];
export type WebhookDeliveryDay = Schemas['WebhookDeliveryDay'];
export type NewWebhookEndpoint = Schemas['NewWebhookEndpoint'];
export type CreatedWebhookEndpoint = Schemas['CreatedWebhookEndpoint'];
export type WebhookDelivery = Schemas['WebhookDelivery'];
export type WebhookDeliveryPage = Schemas['WebhookDeliveryPage'];
export type WebhookDeliveryRequest = Schemas['WebhookDeliveryRequest'];

/** The endpoints of the factory, each with its triggers and its last seven days. Never a secret. */
export async function listWebhookEndpoints(
  org: string,
  factory: string,
  signal?: AbortSignal,
): Promise<WebhookEndpoint[]> {
  const body = await unwrap(
    goal.GET(orgPath('/factories/{factory}/webhooks'), { params: { path: { org, factory } }, ...cancel(signal) }),
  );
  return body.endpoints;
}

/** 400 webhook-invalid, 409 webhook-name-taken, 503 webhooks-unavailable. The secret comes back once. */
export function createWebhookEndpoint(
  org: string,
  factory: string,
  endpoint: NewWebhookEndpoint,
): Promise<CreatedWebhookEndpoint> {
  return unwrap(
    goal.POST(orgPath('/factories/{factory}/webhooks'), { params: { path: { org, factory } }, body: endpoint }),
  );
}

/** A new secret; the old one stops opening at once. The secret comes back once. */
export function rotateWebhookSecret(org: string, factory: string, endpoint: string): Promise<CreatedWebhookEndpoint> {
  return unwrap(
    goal.POST(orgPath('/factories/{factory}/webhooks/{endpoint}/rotate'), {
      params: { path: { org, factory, endpoint } },
    }),
  );
}

/** The newest deliveries of an endpoint, without their headers and body. */
export function listWebhookDeliveries(
  org: string,
  factory: string,
  endpoint: string,
  limit: number,
  signal?: AbortSignal,
): Promise<WebhookDeliveryPage> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/webhooks/{endpoint}/deliveries'), {
      params: { path: { org, factory, endpoint }, query: { limit } },
      ...cancel(signal),
    }),
  );
}

/** One delivery as received: its headers and its body. */
export function getWebhookDelivery(
  org: string,
  factory: string,
  endpoint: string,
  delivery: string,
  signal?: AbortSignal,
): Promise<WebhookDeliveryRequest> {
  return unwrap(
    goal.GET(orgPath('/factories/{factory}/webhooks/{endpoint}/deliveries/{delivery}'), {
      params: { path: { org, factory, endpoint, delivery } },
      ...cancel(signal),
    }),
  );
}
