// Home, the organization's page (ADR 0016, wave 2; docs/design/screens/daily-work/home.jpg): the top bar
// with New factory and New item, the summary of what waits, one row per factory with its
// line and three figures, then "Needs you first" and "Spend this week". Everything comes from the
// overview (7 days and today), the inbox and the attention list, which the organization's stream keeps
// fresh. A quiet "Archived factories" link under the list opens ?archived=true, which lists the archived
// factories instead (ArchivedFactories), with Restore.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useId } from 'react';
import { attentionQuery, inboxQuery, overviewQuery } from '../../api/queries';
import { usePhone } from '../../app/phone-chrome';
import { TopBarSlot } from '../../app/top-bar';
import { plural } from '../../shared/format';
import { factoryMark } from '../../shared/marks';
import { NewItemFromSidebar } from '../../shared/new-item';
import { useOrg, useOrgHolds } from '../../shared/org';
import { reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { EmptyState } from '../../ui/empty-state';
import { Skeleton } from '../../ui/skeleton';
import { ArchivedFactories } from './archived-factories';
import { FactoryRow } from './factory-row';
import { MiniLineShape } from './mini-line';
import { NeedsYouFirst } from './needs-you-first';
import { NewFactoryButtons, StartLink } from './new-factory';
import { SpendWeek } from './spend-week';

export function FactoriesPage({ archived = false }: { archived?: boolean }) {
  const org = useOrg();
  const phone = usePhone();
  const listId = useId();
  const mayCreate = useOrgHolds('factory:create');
  const overview = useQuery(overviewQuery(org.id));
  const today = useQuery(overviewQuery(org.id, '1d'));
  const inbox = useQuery(inboxQuery(org.id));
  const attention = useQuery(attentionQuery(org.id));

  // The top bar's actions: New factory only for whoever may create one, and once a factory exists to
  // show; New item always, except on the archived list.
  const hasFactories = overview.data !== undefined && overview.data.factories.length > 0;
  const topBar = (
    <TopBarSlot
      actions={
        archived ? null : (
          <>
            {mayCreate && hasFactories ? (
              <Button asChild variant="secondary">
                <StartLink>New factory</StartLink>
              </Button>
            ) : null}
            <NewItemFromSidebar compact={phone} />
          </>
        )
      }
    />
  );

  if (archived) {
    return (
      <>
        {topBar}
        <Link
          to="/o/$org"
          params={{ org: org.slug }}
          className="self-start text-secondary font-control text-run-ink no-underline hover:underline"
        >
          Home
        </Link>
        <h1 className="m-0 text-page-title">Archived factories</h1>
        <ArchivedFactories org={org} />
      </>
    );
  }

  // Today's spend only adds to a row, so it is not waited for: a row shows "…" until it loads.
  const failed = overview.error ?? inbox.error;
  if (failed) {
    return (
      <>
        {topBar}
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

  // The route's loader fills these before the page renders; the wait shows the page's own shape.
  if (!overview.data || !inbox.data) {
    return (
      <>
        {topBar}
        <Skeleton ready={false} placeholder={<HomeShape />}>
          {null}
        </Skeleton>
      </>
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
        {topBar}
        <Header subtitle={null} />
        {mayCreate ? (
          <EmptyState title={`No factories in ${org.name} yet`} actions={<NewFactoryButtons variant="empty" />}>
            Start one blank, from a template or from a factory.yaml.
          </EmptyState>
        ) : (
          <EmptyState title="No factories to show yet">
            You see a factory once an owner or admin of {org.name} gives you a role on it.{' '}
            {reasons.newFactory(org.name)}
          </EmptyState>
        )}
        <ArchivedLink slug={org.slug} />
      </>
    );
  }

  return (
    <>
      {topBar}
      <Header subtitle={summary(factories.length, week.waiting_on_you, andons)} />

      <section aria-labelledby={listId} className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 id={listId} className="m-0 text-heading">
            Factories
          </h2>
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
        <ArchivedLink slug={org.slug} />
      </section>

      <div className="grid gap-4 @[960px]:grid-cols-2">
        <NeedsYouFirst org={org} inbox={inbox.data} factories={named} mark={mark} />
        <SpendWeek org={org} overview={week} mark={mark} />
      </div>
    </>
  );
}

/** The quiet way to the archived factories. */
function ArchivedLink({ slug }: { slug: string }) {
  return (
    <Link
      to="/o/$org"
      params={{ org: slug }}
      search={{ archived: true }}
      className="self-start text-secondary font-control text-muted no-underline hover:underline"
    >
      Archived factories
    </Link>
  );
}

/** The title with its subtitle inline after it on one baseline; the subtitle wraps under the title on a phone. */
function Header({ subtitle }: { subtitle: string | null }) {
  return (
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <h1 className="m-0 text-page-title tracking-display">Home</h1>
      {subtitle ? <p className="m-0 text-secondary text-muted">{subtitle}</p> : null}
    </div>
  );
}

/** Home's skeleton: the header line, three compact factory rows and the two panels. */
function HomeShape() {
  const block = 'rounded-card border border-line bg-card';
  return (
    <div className="flex flex-col gap-6">
      <span className="block h-8 w-72 max-w-full rounded-control bg-hover" />
      <ul className="m-0 flex list-none flex-col gap-3 p-0">
        {['a', 'b', 'c'].map((k) => (
          <li key={k} className={`${block} flex flex-wrap items-center gap-4 p-4 md:p-5`}>
            <span className="block h-9 w-40 rounded-control bg-hover" />
            <span className="min-w-0 flex-1">
              <MiniLineShape />
            </span>
          </li>
        ))}
      </ul>
      <div className="grid gap-4 @[960px]:grid-cols-2">
        <span className={`${block} block h-48`} />
        <span className={`${block} block h-48`} />
      </div>
    </div>
  );
}

/** "3 factories, 5 things need you, one andon pulled": the clause for andons is left out when none is pulled. */
export function summary(factories: number, waiting: number, andons: number): string {
  const need = waiting === 1 ? '1 thing needs you' : `${waiting} things need you`;
  const pulled = andons === 1 ? 'one andon pulled' : `${andons} andons pulled`;
  return [plural(factories, 'factory', 'factories'), need, ...(andons > 0 ? [pulled] : [])].join(', ');
}
