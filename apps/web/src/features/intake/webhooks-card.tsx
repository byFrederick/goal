// Webhooks open to the factory (Intake, ADR 0040 decision 18): the organization's webhooks an admin opened to
// this factory, each with whether a trigger of the current definition uses it. Managing them is the
// Webhooks page.
import { Link } from '@tanstack/react-router';
import { CornerDownRight } from 'lucide-react';
import type { IntakeWebhook } from '../../api/types';
import { factoryTitle, useFactory, useFactoryParams } from '../../shared/factory';
import { cn } from '../../ui/cn';
import { Panel } from './panel';

const info = 'Connected once for the organization by an admin, and opened to the factories that may use them.';

const states: Record<IntakeWebhook['state'], { label: string; dot: string }> = {
  receiving: { label: 'Receiving', dot: 'bg-ok' },
  no_trigger: { label: 'No trigger yet', dot: 'bg-muted' },
  closed: { label: 'Closed', dot: 'bg-muted' },
};

export function WebhooksCard({ webhooks }: { webhooks: readonly IntakeWebhook[] }) {
  const params = useFactoryParams();
  const factory = useFactory();
  return (
    <Panel
      title={`Webhooks open to ${factoryTitle(factory)}`}
      info={info}
      aside={
        <Link
          to="/o/$org/f/$factory/intake/webhooks"
          params={params}
          className="font-medium text-link no-underline hover:underline"
        >
          Manage
        </Link>
      }
    >
      {webhooks.length === 0 ? (
        <p className="m-0 border-t border-line px-5 py-4 text-body text-muted">
          No webhook is open to this factory. Open one from Manage.
        </p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {webhooks.map((webhook) => (
            <li
              key={webhook.id}
              className="flex items-center justify-between gap-4 border-t border-line px-5 py-3 first:border-t-0"
            >
              <span className="flex min-w-0 flex-col">
                <span className="truncate font-medium" title={webhook.name}>
                  {webhook.name}
                </span>
                <span className="flex min-w-0 items-center gap-2 text-secondary text-muted">
                  <CornerDownRight size={12} strokeWidth={1.75} aria-hidden="true" className="flex-none" />
                  <span className="break-all font-mono text-meta">{webhook.sender}</span>
                </span>
              </span>
              <span className="inline-flex flex-none items-center gap-2 text-secondary font-medium motion-safe:transition-colors">
                <i aria-hidden="true" className={cn('block size-1.5 rounded-full', states[webhook.state].dot)} />
                {states[webhook.state].label}
              </span>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}
