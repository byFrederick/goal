// The organization's home (C §10.7, canvas "Factories" and "Mobile-Factories"): a sentence on
// what waits and what the week cost, one card per factory, what waits on you across them, and
// the ways to start a new factory. Everything comes from GET /overview (7 days) and GET /inbox,
// which the organization's stream keeps fresh.
import { useSuspenseQuery } from '@tanstack/react-query';
import { Factory as FactoryIcon } from 'lucide-react';
import { useId } from 'react';
import { inboxQuery, overviewQuery } from '../../api/queries';
import { factoryMark } from '../../shared/marks';
import { useOrg, useOrgHolds } from '../../shared/org';
import { reasons } from '../../shared/permissions';
import { cn } from '../../ui/cn';
import { EmptyState } from '../../ui/empty-state';
import { actionsClass, descriptionClass, eyebrowClass, PageHeader } from '../../ui/page-header';
import { FactoryCard } from './factory-card';
import { NewFactoryButton, NewFactoryButtons, NewFactoryCard } from './new-factory';
import { budgetLine, headerSentence, possessive } from './sentences';
import { WaitingBanner, WaitingList } from './waiting';

/** The cards: one column below 640 px of room, two below 1000 px, else three (container queries on main). */
const cardGrid = 'm-0 grid list-none gap-3 p-0 @[640px]:grid-cols-2 @[640px]:gap-4 @[1000px]:grid-cols-3';

export function FactoriesPage() {
  const org = useOrg();
  const { data: overview } = useSuspenseQuery(overviewQuery(org.id));
  const { data: inbox } = useSuspenseQuery(inboxQuery(org.id));
  const listId = useId();
  const factories = overview.factories.toSorted((a, b) => a.factory.name.localeCompare(b.factory.name));
  const named = factories.map((o) => o.factory);
  // The ways to start a factory are offered only to whoever may create one (owners and admins).
  const mayCreate = useOrgHolds('factory:create');

  const actions = (
    <div className={cn(actionsClass, 'hidden md:flex')}>
      <NewFactoryButtons variant="header" />
    </div>
  );

  return (
    <>
      <PageHeader
        title="Factories"
        eyebrow={<p className={cn(eyebrowClass, 'md:hidden')}>{org.name}</p>}
        description={
          factories.length > 0 ? (
            <p className={cn(descriptionClass, 'hidden md:block')}>
              {headerSentence(org.name, overview.waiting_on_you, overview.spend_usd, overview.budget_usd)}
            </p>
          ) : null
        }
        actions={factories.length > 0 && mayCreate ? actions : null}
      />

      {factories.length === 0 && mayCreate ? (
        <EmptyState
          icon={<FactoryIcon size={20} strokeWidth={1.5} />}
          title={`No factories in ${org.name} yet`}
          actions={<NewFactoryButtons variant="empty" />}
        >
          A factory runs from a definition: its line, agents and connections. Start one blank, from a template or from a
          factory.yaml.
        </EmptyState>
      ) : factories.length === 0 ? (
        <EmptyState icon={<FactoryIcon size={20} strokeWidth={1.5} />} title="No factories to show yet">
          You see a factory once an owner or admin of {org.name} gives you a role on it. {reasons.newFactory(org.name)}
        </EmptyState>
      ) : (
        <>
          <WaitingBanner org={org} overview={overview} inbox={inbox} />

          <section aria-labelledby={listId}>
            <h2 id={listId} className="sr-only">
              {possessive(org.name)} factories
            </h2>
            <ul className={cardGrid}>
              {factories.map((o) => (
                <li key={o.factory.id} className="flex">
                  <FactoryCard org={org.slug} overview={o} mark={factoryMark(o.factory, named)} />
                </li>
              ))}
            </ul>
          </section>

          <div className={cn('grid gap-3 md:gap-4', mayCreate && '@[1000px]:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]')}>
            <WaitingList org={org} inbox={inbox} className="scroll-mt-4" />
            {mayCreate ? <NewFactoryCard className="hidden md:flex" /> : null}
          </div>

          {mayCreate ? <NewFactoryButton className="md:hidden" /> : null}
          <p className="m-0 text-center text-[13px] text-muted md:hidden">
            {budgetLine(org.name, overview.spend_usd, overview.budget_usd)}
          </p>
        </>
      )}
    </>
  );
}
