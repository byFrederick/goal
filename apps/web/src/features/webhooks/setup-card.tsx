// Set it up in <sender>: the steps to take there, and the events to choose, which are the ones the triggers
// of the factories it is open to name (webhook.setup_events). With a trigger that names no event, the sender
// should send every one.
import type { Webhook } from '../../api/webhooks';
import { Card } from './parts';
import { senderLabel, setupSteps } from './words';

export function SetupCard({ webhook }: { webhook: Webhook }) {
  const { where, steps, eventsLabel } = setupSteps(webhook.sender);
  const name = webhook.sender === 'standard' || webhook.sender === 'token' ? senderLabel(webhook.sender) : where;
  return (
    <Card
      title={`Set it up in ${where}`}
      tip={{
        about: 'setting it up',
        text: `Needs someone who can add webhooks in ${name}. Nobody else in Goal needs access there: a factory uses the webhook once it is opened to it here.`,
      }}
    >
      <ol className="m-0 flex list-decimal flex-col gap-2 py-3 ps-9 pe-4 text-secondary text-ink-soft marker:text-muted">
        {steps.map((step) => (
          <li key={step}>{step}</li>
        ))}
        <li>
          {eventsLabel}:{' '}
          {webhook.setup_all_events ? (
            <span className="font-medium text-ink">every event</span>
          ) : webhook.setup_events.length === 0 ? (
            <span className="text-muted">none yet, no trigger names an event</span>
          ) : (
            <span className="inline-flex flex-wrap gap-1 align-middle">
              {webhook.setup_events.map((event) => (
                <code
                  key={event}
                  className="rounded-chip border border-line bg-panel px-2 font-mono text-meta text-ink"
                >
                  {event}
                </code>
              ))}
            </span>
          )}
        </li>
      </ol>
    </Card>
  );
}
