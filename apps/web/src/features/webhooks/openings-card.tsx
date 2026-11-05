// Open to: the factories the webhook reaches and the repositories each is limited to, with Edit for
// webhook:manage; and Triggers that use it, each with its factory.
import { Link } from '@tanstack/react-router';
import type { Factory } from '../../api/types';
import type { Webhook } from '../../api/webhooks';
import { Button } from '../../ui/button';
import { Card, FactoryChip } from './parts';

export function OpeningsCard({
  webhook,
  orgSlug,
  factories,
  canManage,
  onEdit,
}: {
  webhook: Webhook;
  orgSlug: string;
  factories: readonly Factory[];
  canManage: boolean;
  onEdit: () => void;
}) {
  return (
    <Card
      title="Open to"
      tip={{
        about: 'openings',
        text: 'It carries every repository’s events. Each factory gets only the repositories its opening allows, and only what its triggers name.',
      }}
      aside={
        canManage ? (
          <Button variant="secondary" size="sm" onClick={onEdit}>
            Edit
          </Button>
        ) : null
      }
    >
      {webhook.openings.length === 0 ? (
        <p className="m-0 px-4 py-3 text-secondary text-muted">Open to no factory yet.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {webhook.openings.map((o) => (
            <li
              key={o.factory}
              className="flex items-center justify-between gap-3 border-t border-line px-4 py-3 first:border-t-0"
            >
              <FactoryChip id={o.factory} factories={factories} />
              <span title={(o.repositories ?? []).join(', ')} className="truncate text-secondary text-muted">
                {o.repositories && o.repositories.length > 0 ? o.repositories.join(', ') : 'All repositories'}
              </span>
            </li>
          ))}
        </ul>
      )}
      <h3 className="m-0 border-t border-line px-4 pt-3 text-secondary font-medium text-ink-soft">
        Triggers that use it
      </h3>
      {webhook.triggers.length === 0 ? (
        <p className="m-0 px-4 py-3 text-secondary text-muted">No trigger names it yet.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {webhook.triggers.map((t) => (
            <li key={`${t.factory}/${t.trigger}`} className="flex items-center justify-between gap-3 px-4 py-3">
              <Link
                to="/o/$org/f/$factory/intake/$trigger"
                params={{ org: orgSlug, factory: t.factory_name, trigger: t.trigger }}
                title={t.trigger}
                className="min-w-0 truncate text-body font-medium text-ink no-underline hover:underline"
              >
                {t.trigger}
              </Link>
              <FactoryChip id={t.factory} factories={factories} name={t.factory_name} />
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
