// The Archived filter of the Factories home: the organization's archived factories, each with when
// and by whom it was archived, and Restore for whoever may archive or restore (factory:archive on
// the organization's `can`). An archived factory stays readable, so its name links to its pages.
import { useSuspenseQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Archive } from 'lucide-react';
import { useId } from 'react';
import { ApiError } from '../../api/client';
import { archivedFactoriesQuery } from '../../api/queries';
import type { Factory, OrgSummary } from '../../api/types';
import { factoryTitle } from '../../shared/factory';
import { formatDay } from '../../shared/format';
import { useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { EmptyState } from '../../ui/empty-state';
import { useRestoreFactory } from '../factory-settings/restore';

/** "Archived Oct 25 by Dana", or "Archived Oct 25" when goald does not name who. */
function archivedLine(factory: Factory): string {
  if (!factory.archived_at) return 'Archived';
  const when = `Archived ${formatDay(factory.archived_at)}`;
  return factory.archived_by ? `${when} by ${factory.archived_by.name}` : when;
}

function ArchivedRow({ org, factory, mayRestore }: { org: OrgSummary; factory: Factory; mayRestore: boolean }) {
  const restore = useRestoreFactory(org, factory);
  const error = restore.error instanceof ApiError ? (restore.error.detail ?? restore.error.title) : null;
  return (
    <li className="flex flex-col gap-3 rounded-card border border-line bg-card p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="m-0 flex min-w-0 flex-col">
          <Link
            to="/o/$org/f/$factory"
            params={{ org: org.slug, factory: factory.name }}
            className="text-[15px] font-medium text-ink no-underline hover:underline"
          >
            {factoryTitle(factory)}
          </Link>
          <span className="text-sm text-muted">{archivedLine(factory)}</span>
        </p>
        {mayRestore ? (
          <Button
            variant="raised"
            className="h-11 border border-line shadow-none md:h-10"
            disabled={restore.isPending}
            onClick={() => restore.mutate()}
          >
            {restore.isPending ? 'Restoring…' : `Restore ${factory.name}`}
          </Button>
        ) : null}
      </div>
      {error ? (
        <p role="alert" className="m-0 text-sm text-rework-ink">
          {error}
        </p>
      ) : null}
    </li>
  );
}

export function ArchivedFactories({ org }: { org: OrgSummary }) {
  const { data } = useSuspenseQuery(archivedFactoriesQuery(org.id));
  const listId = useId();
  const mayRestore = useOrgHolds('factory:archive');
  const factories = data.toSorted((a, b) => a.name.localeCompare(b.name));

  if (factories.length === 0) {
    return (
      <EmptyState icon={<Archive size={20} strokeWidth={1.5} />} title="No archived factories">
        A factory you archive stays here, readable, until you restore it or delete it.
      </EmptyState>
    );
  }

  return (
    <section aria-labelledby={listId}>
      <h2 id={listId} className="sr-only">
        Archived factories of {org.name}
      </h2>
      <ul className="m-0 grid list-none gap-3 p-0">
        {factories.map((factory) => (
          <ArchivedRow key={factory.id} org={org} factory={factory} mayRestore={mayRestore} />
        ))}
      </ul>
    </section>
  );
}
