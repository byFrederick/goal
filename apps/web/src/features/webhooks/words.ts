// What the Webhooks screen says about its endpoints and deliveries, and the sums it shows. Pure, so the
// wording and the counts are tested without a page.
import type { WebhookDelivery, WebhookDeliveryDay, WebhookEndpoint } from '../../api/webhooks';
import { plural } from '../../shared/format';

/** The deliveries of the last seven days, summed over its days. */
export function weekTotals(days: readonly WebhookDeliveryDay[]): { delivered: number; refused: number } {
  return days.reduce((sum, day) => ({ delivered: sum.delivered + day.delivered, refused: sum.refused + day.refused }), {
    delivered: 0,
    refused: 0,
  });
}

/** "7 delivered, 2 refused": the last seven days of an endpoint, in words. */
export function weekSummary(days: readonly WebhookDeliveryDay[]): string {
  const { delivered, refused } = weekTotals(days);
  return `${delivered} delivered, ${refused} refused`;
}

/** The chip's words, shown only when a delivery was refused this week. */
export function refusedChip(refused: number): string {
  return `${refused} refused this week`;
}

/**
 * What an endpoint with no trigger shows under Feeds: the deliveries it kept in the week, none of which
 * created an item. The count is the week's deliveries, refused ones included.
 */
export function feedsNothing(days: readonly WebhookDeliveryDay[]): string {
  const { delivered, refused } = weekTotals(days);
  const kept = delivered + refused;
  return `Nothing. ${plural(kept, 'delivery', 'deliveries')} ${kept === 1 ? 'was' : 'were'} kept but created no items.`;
}

/** A delivery's outcome line: refused with its reason, accepted with no trigger, or created. */
export function outcomeOf(delivery: WebhookDelivery): { kind: 'refused' | 'accepted' | 'created'; text: string } {
  if (delivery.outcome === 'refused') {
    return { kind: 'refused', text: delivery.reason ? `Refused: ${delivery.reason}` : 'Refused' };
  }
  if (delivery.outcome === 'created') return { kind: 'created', text: 'Accepted, created' };
  return { kind: 'accepted', text: 'Accepted, no trigger matched' };
}

/**
 * The slug check the form shows before it sends: lower-case letters, digits and single hyphens, as
 * a trigger's `on: webhook.<name>` reads. goald checks it again and answers webhook-invalid.
 */
export function nameError(name: string): string | undefined {
  const value = name.trim();
  if (value === '') return 'Give the webhook a name';
  if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(value)) return 'Use lower-case letters, digits and single hyphens';
  return undefined;
}

/** The URL a sender posts to: the page's origin and the endpoint's address (/hooks/{id}). */
export function addressUrl(endpoint: Pick<WebhookEndpoint, 'address'>, origin: string): string {
  return `${origin}${endpoint.address}`;
}

/** The signature header a sender sets, by the endpoint's kind (ADR 0026, decision 3). */
export function signatureHeader(kind: WebhookEndpoint['kind']): string {
  return kind === 'github' ? 'X-Hub-Signature-256' : 'X-Goal-Signature';
}
