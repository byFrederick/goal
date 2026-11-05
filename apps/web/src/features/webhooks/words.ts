// What the Webhooks screens say: the senders by name, a webhook's status, the delivery log's words and the
// setup steps of each sender. Pure, so the wording is tested without a page.
import type { Webhook, WebhookDelivery, WebhookDeliveryEvaluation, WebhookSender } from '../../api/webhooks';

/** The six senders in the order New webhook offers them. */
export const senders: readonly { value: WebhookSender; label: string; blurb: string }[] = [
  { value: 'github', label: 'GitHub', blurb: 'An organization webhook, or one per repository' },
  { value: 'gitlab', label: 'GitLab', blurb: 'A group or project webhook' },
  { value: 'pagerduty', label: 'PagerDuty', blurb: 'Incidents and their changes' },
  { value: 'sentry', label: 'Sentry', blurb: 'Issues and alerts' },
  { value: 'standard', label: 'Standard Webhooks', blurb: 'Any sender that signs this way' },
  { value: 'token', label: 'Token in a header', blurb: 'Datadog, scripts, anything that sets a header' },
];

export function senderLabel(sender: WebhookSender): string {
  return senders.find((s) => s.value === sender)?.label ?? sender;
}

/** The senders that issue their own secret: they are given the one Goal holds, so a secret must exist already. */
export const issuesOwnSecret: readonly WebhookSender[] = ['pagerduty', 'sentry'];

const week = (webhook: Pick<Webhook, 'days'>) => webhook.days.reduce((n, d) => n + d.delivered + d.refused, 0);

/** The deliveries of the last seven days, refused ones included. */
export function weekCount(webhook: Pick<Webhook, 'days'>): number {
  return week(webhook);
}

/** "1,204 this week", or "None yet". */
export function weekText(webhook: Pick<Webhook, 'days'>): string {
  const n = week(webhook);
  return n === 0 ? 'None yet' : `${n.toLocaleString('en-US')} this week`;
}

export type WebhookStatus = { label: string; tone: 'ok' | 'quiet' };

/** Receiving while a trigger names it; "No trigger yet" otherwise; "Switched off" when its address answers 404. */
export function statusOf(webhook: Pick<Webhook, 'enabled' | 'triggers'>): WebhookStatus {
  if (!webhook.enabled) return { label: 'Switched off', tone: 'quiet' };
  if (webhook.triggers.length === 0) return { label: 'No trigger yet', tone: 'quiet' };
  return { label: 'Receiving', tone: 'ok' };
}

const pingTime = new Intl.DateTimeFormat('en-US', {
  month: 'short',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
});

/** "Received GitHub’s ping on Nov 3 at 08:55"; GitHub only. Before a ping arrives, what to wait for. */
export function pingText(webhook: Pick<Webhook, 'sender' | 'pinged_at'>): string | undefined {
  if (webhook.sender !== 'github') return undefined;
  if (!webhook.pinged_at) return 'Waiting for GitHub’s ping';
  const at = pingTime.format(new Date(webhook.pinged_at)).replace(', ', ' at ');
  return `Received GitHub’s ping on ${at}`;
}

/** The receipt's word: Accepted, Duplicate or Refused. A delivery kept before the receiver has none: its outcome says it. */
export function receiptOf(delivery: WebhookDelivery): {
  label: 'Accepted' | 'Duplicate' | 'Refused';
  refused: boolean;
} {
  const receipt = delivery.receipt ?? (delivery.outcome === 'refused' ? 'refused' : 'accepted');
  if (receipt === 'refused') return { label: 'Refused', refused: true };
  return { label: receipt === 'duplicate' ? 'Duplicate' : 'Accepted', refused: false };
}

/** The line under a delivery's event: why it was refused, or that it repeats one, or the count of repeats. */
export function receiptNote(delivery: WebhookDelivery): string | undefined {
  const { label } = receiptOf(delivery);
  if (label === 'Refused') return delivery.reason || undefined;
  if (label === 'Duplicate') return 'Same delivery as an earlier one';
  if (delivery.duplicates > 0) return `Sent ${delivery.duplicates + 1} times`;
  return undefined;
}

/** What one factory made of a delivery: "Created", "Skipped: <reason>", "Failed: <reason>", or still waiting. */
export function evaluationText(e: WebhookDeliveryEvaluation): {
  kind: 'created' | 'skipped' | 'failed' | 'pending';
  text: string;
} {
  if (e.state === 'pending') return { kind: 'pending', text: 'Waiting' };
  if (e.outcome === 'created') return { kind: 'created', text: 'Created' };
  if (e.outcome === 'failed' || e.state === 'failed')
    return { kind: 'failed', text: e.reason ? `Failed: ${e.reason}` : 'Failed' };
  return { kind: 'skipped', text: e.reason ? `Skipped: ${e.reason}` : 'Skipped' };
}

/** The slug check the form shows before it sends; goald checks again (webhook-invalid). */
export function nameProblem(name: string): string | undefined {
  if (name === '') return 'Give the webhook a name.';
  if (name.length < 3 || name.length > 40) return 'Use 3 to 40 characters.';
  if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(name)) return 'Use lower-case letters, digits and single hyphens.';
  if (
    (['github', 'gitlab', 'standard', 'pagerduty', 'sentry', 'token', 'schedule', 'webhook'] as string[]).includes(name)
  )
    return 'This name is reserved. Add the organization or the team to it.';
  return undefined;
}

/** The URL a sender posts to: goald's public origin and the webhook's address (/hooks/{id}). */
export function addressUrl(webhook: Pick<Webhook, 'address'>, origin: string): string {
  return `${origin}${webhook.address}`;
}

/** The steps to take in the sender, and the events to choose there. `events` is empty with `all` when a trigger names no event. */
export function setupSteps(sender: WebhookSender): { where: string; steps: string[]; eventsLabel: string } {
  switch (sender) {
    case 'github':
      return {
        where: 'GitHub',
        steps: [
          'In the organization’s Settings, open Webhooks, then Add webhook. A personal account adds it to each repository.',
          'Paste the address and the secret, with content type JSON.',
        ],
        eventsLabel: 'Choose individual events',
      };
    case 'gitlab':
      return {
        where: 'GitLab',
        steps: [
          'In the group or project, open Settings, then Webhooks, then Add new webhook.',
          'Paste the address, and the secret as the secret token.',
        ],
        eventsLabel: 'Tick the triggers',
      };
    case 'pagerduty':
      return {
        where: 'PagerDuty',
        steps: [
          'Open Integrations, then Generic Webhooks (v3), then New Webhook.',
          'Paste the address. PagerDuty gives you a signing secret: save it in Secrets, then use it here.',
        ],
        eventsLabel: 'Subscribe to events',
      };
    case 'sentry':
      return {
        where: 'Sentry',
        steps: [
          'Open Settings, Developer Settings, then create an internal integration.',
          'Paste the address as its webhook URL. Sentry gives you a client secret: save it in Secrets, then use it here.',
        ],
        eventsLabel: 'Choose resources',
      };
    case 'standard':
      return {
        where: 'the sender',
        steps: ['Add an endpoint with the address.', 'Give it the secret, as the sender’s signing secret.'],
        eventsLabel: 'Send these events',
      };
    case 'token':
      return {
        where: 'the sender',
        steps: ['Post to the address.', 'Set the secret as the value of the header named above.'],
        eventsLabel: 'Send these events',
      };
  }
}
