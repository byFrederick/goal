// Home, the organization's page (ADR 0016, wave 2; docs/design/screens/daily-work/home.jpg): a top bar
// with New factory and New item, a greeting with a summary of what waits, one row per factory with its
// navy line and three figures, then "Needs you first" and "Spend this week". Everything comes from the
// overview (7 days and today), the inbox and the attention list, which the organization's stream keeps
// fresh. The Active | Archived control stays: ?archived=true lists the archived factories instead
// (ArchivedFactories), with Restore.
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { Factory as FactoryIcon } from 'lucide-react';
import { useId } from 'react';
import { attentionQuery, inboxQuery, meQuery, overviewQuery } from '../../api/queries';
import { plural } from '../../shared/format';
import { factoryMark } from '../../shared/marks';
import { NewItemFromSidebar } from '../../shared/new-item';
import { useOrg, useOrgHolds } from '../../shared/org';
import { reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { EmptyState } from '../../ui/empty-state';
import { SegmentedControl } from '../../ui/segmented-control';
import { ArchivedFactories } from './archived-factories';
import { FactoryRow } from './factory-row';
import { NeedsYouFirst } from './needs-you-first';
import { NewFactoryButtons, StartLink } from './new-factory';
import { SpendWeek } from './spend-week';

export function FactoriesPage({ archived = false }: { archived?: boolean }) {
  const org = useOrg();
  const navigate = useNavigate();
  const listId = useId();
  const mayCreate = useOrgHolds('factory:create');
  const overview = useQuery(overviewQuery(org.id));
  const today = useQuery(overviewQuery(org.id, '1d'));
  const inbox = useQuery(inboxQuery(org.id));
  const attention = useQuery(attentionQuery(org.id));
  const me = useQuery(meQuery);

  const header = (
    <header className="flex flex-wrap items-center gap-3 border-b border-line pb-4">
      <p className="m-0 mr-auto text-heading font-semibold">Home</p>
      {!archived && mayCreate && overview.data && overview.data.factories.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          <Button asChild variant="secondary">
            <StartLink>New factory</StartLink>
          </Button>
          <NewItemFromSidebar />
        </div>
      ) : !archived ? (
        <NewItemFromSidebar />
      ) : null}
    </header>
  );

  const segmented = (
    <SegmentedControl
      label="Show"
      className="self-start"
      options={[
        { value: 'active', label: 'Active' },
        { value: 'archived', label: 'Archived' },
      ]}
      value={archived ? 'archived' : 'active'}
      onChange={(value) =>
        navigate({
          to: '/o/$org',
          params: { org: org.slug },
          search: value === 'archived' ? { archived: true } : {},
        })
      }
    />
  );

  if (archived) {
    return (
      <>
        {header}
        <h1 className="m-0 text-page-title">Archived factories</h1>
        {segmented}
        <ArchivedFactories org={org} />
      </>
    );
  }

  // Today's spend only adds to a row, so it is not waited for: a row shows "…" until it loads.
  const failed = overview.error ?? inbox.error;
  if (failed) {
    return (
      <>
        {header}
        <section className="flex flex-col items-start gap-3 rounded-card border border-line bg-card p-5" role="alert">
          <h1 className="m-0 text-heading">Home could not load</h1>
          <p className="m-0 text-secondary text-muted">{failed.message}</p>
          <Button
            onClick={() => {
              overview.refetch();
              inbox.refetch();
            }}
          >
            Retry
          </Button>
        </section>
      </>
    );
  }

  // The route's loader fills these before the page renders; the wait shows the app background, not a shape.
  if (!overview.data || !inbox.data) {
    return (
      <div aria-busy="true" className="min-h-40">
        <span className="sr-only">Loading</span>
      </div>
    );
  }

  const { data: week } = overview;
  const factories = week.factories.toSorted((a, b) => a.factory.name.localeCompare(b.factory.name));
  const named = factories.map((o) => o.factory);
  const mark = (factory: { name: string }) => factoryMark(factory, named);
  const todayById = new Map(today.data?.factories.map((o) => [o.factory.id, o]));
  const andons = attention.data
    ? attention.data.factories.filter((f) => f.andon !== null).length
    : factories.filter((o) => o.andon !== null).length;

  if (factories.length === 0) {
    return (
      <>
        {header}
        <h1 className="m-0 text-page-title">{greeting(new Date(), me.data?.user?.name)}</h1>
        {segmented}
        {mayCreate ? (
          <EmptyState
            icon={<FactoryIcon size={20} strokeWidth={1.5} />}
            title={`No factories in ${org.name} yet`}
            actions={<NewFactoryButtons variant="empty" />}
          >
            A factory runs from a definition: its line, agents and connections. Start one blank, from a template or from
            a factory.yaml.
          </EmptyState>
        ) : (
          <EmptyState icon={<FactoryIcon size={20} strokeWidth={1.5} />} title="No factories to show yet">
            You see a factory once an owner or admin of {org.name} gives you a role on it.{' '}
            {reasons.newFactory(org.name)}
          </EmptyState>
        )}
      </>
    );
  }

  return (
    <>
      {header}
      <div className="flex flex-col gap-1">
        <h1 className="m-0 text-page-title">{greeting(new Date(), me.data?.user?.name)}</h1>
        <p className="m-0 text-secondary text-muted">{summary(factories.length, week.waiting_on_you, andons)}</p>
      </div>

      <section aria-labelledby={listId} className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 id={listId} className="m-0 text-heading">
            Factories
          </h2>
          {segmented}
        </div>
        <ul className="m-0 flex list-none flex-col gap-3 p-0">
          {factories.map((o) => {
            return (
              <li key={o.factory.id}>
                <FactoryRow org={org} overview={o} today={todayById.get(o.factory.id)} mark={mark(o.factory)} />
              </li>
            );
          })}
        </ul>
      </section>

      <div className="grid gap-4 @[960px]:grid-cols-2">
        <NeedsYouFirst org={org} inbox={inbox.data} factories={named} mark={mark} />
        <SpendWeek org={org} overview={week} mark={mark} />
      </div>
    </>
  );
}

/** "Good morning", "Good afternoon" or "Good evening", then the first word of the caller's name. */
export function greeting(now: Date, name: string | undefined): string {
  const hour = now.getHours();
  const part = hour < 12 ? 'morning' : hour < 18 ? 'afternoon' : 'evening';
  const first = name?.trim().split(/\s+/)[0];
  return first ? `Good ${part}, ${first}` : `Good ${part}`;
}

/** "3 factories, 5 things need you, one andon pulled": the clause for andons is left out when none is pulled. */
export function summary(factories: number, waiting: number, andons: number): string {
  const need = waiting === 1 ? '1 thing needs you' : `${waiting} things need you`;
  const pulled = andons === 1 ? 'one andon pulled' : `${andons} andons pulled`;
  return [plural(factories, 'factory', 'factories'), need, ...(andons > 0 ? [pulled] : [])].join(', ');
}
