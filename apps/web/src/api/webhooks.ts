// The organization's webhooks (ADR 0040 decisions 12 and 13; the Webhooks screens) over client.ts's goald
// client: the list, one webhook, create, change (open, close, limit, rename), delete, and the delivery
// log. A created webhook may carry the generated secret's value once: the callers show it in a dialog and
// never put the answer in the query cache (features/webhooks). Every change needs a sign-in from the last
// 10 minutes; the client sends the request once more after the re-auth dialog (session.ts).
import type { components } from '@goal/api-client';
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';

type Schemas = components['schemas'];

export type WebhookSender = Schemas['WebhookSender'];
export type Webhook = Schemas['Webhook'];
export type WebhookOpening = Schemas['WebhookOpening'];
export type WebhookTrigger = Schemas['WebhookTrigger'];
export type WebhookTokenSettings = Schemas['WebhookTokenSettings'];
export type NewWebhook = Schemas['NewWebhook'];
export type CreatedWebhook = Schemas['CreatedWebhook'];
export type WebhookPatch = Schemas['WebhookPatch'];
export type WebhookDelivery = Schemas['WebhookDelivery'];
export type WebhookDeliveryEvaluation = Schemas['WebhookDeliveryEvaluation'];
export type WebhookDeliveryPage = Schemas['WebhookDeliveryPage'];
export type WebhookDeliveryRequest = Schemas['WebhookDeliveryRequest'];

/** The organization's webhooks, each with its sender, secret name, openings and triggers. Never a secret's value. */
export async function listWebhooks(org: string, signal?: AbortSignal): Promise<Webhook[]> {
  const body = await unwrap(goal.GET(orgPath('/webhooks'), { params: { path: { org } }, ...cancel(signal) }));
  return body.webhooks;
}

/** One webhook by name or id; 404 webhook-not-found. */
export function getWebhook(org: string, webhook: string, signal?: AbortSignal): Promise<Webhook> {
  return unwrap(goal.GET(orgPath('/webhooks/{webhook}'), { params: { path: { org, webhook } }, ...cancel(signal) }));
}

/**
 * 400 webhook-invalid, 404 secret-not-found, 409 webhook-name-taken or secret-name-taken, 503
 * secrets-unavailable. The generated secret's value is in the answer once.
 */
export function createWebhook(org: string, webhook: NewWebhook): Promise<CreatedWebhook> {
  return unwrap(goal.POST(orgPath('/webhooks'), { params: { path: { org } }, body: webhook }));
}

/** Opens, closes, limits, renames or switches off; 409 webhook-in-use lists the triggers that stop it. */
export function patchWebhook(org: string, webhook: string, patch: WebhookPatch): Promise<Webhook> {
  return unwrap(goal.PATCH(orgPath('/webhooks/{webhook}'), { params: { path: { org, webhook } }, body: patch }));
}

/** Deletes the webhook, its openings and its deliveries; its secret stays. 409 webhook-in-use. */
export async function deleteWebhook(org: string, webhook: string): Promise<void> {
  await unwrap(goal.DELETE(orgPath('/webhooks/{webhook}'), { params: { path: { org, webhook } } }));
}

/** One page of deliveries, newest first, without their headers and body. `after` is the previous page's next_after. */
export function listWebhookDeliveries(
  org: string,
  webhook: string,
  query: { limit: number; after?: string | undefined },
  signal?: AbortSignal,
): Promise<WebhookDeliveryPage> {
  return unwrap(
    goal.GET(orgPath('/webhooks/{webhook}/deliveries'), {
      params: { path: { org, webhook }, query: { limit: query.limit, ...(query.after ? { after: query.after } : {}) } },
      ...cancel(signal),
    }),
  );
}

/** One delivery as received: its headers and its body. Only webhook:manage reads it. */
export function getWebhookDelivery(
  org: string,
  webhook: string,
  delivery: string,
  signal?: AbortSignal,
): Promise<WebhookDeliveryRequest> {
  return unwrap(
    goal.GET(orgPath('/webhooks/{webhook}/deliveries/{delivery}'), {
      params: { path: { org, webhook, delivery } },
      ...cancel(signal),
    }),
  );
}
