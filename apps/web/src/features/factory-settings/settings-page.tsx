// Settings of a factory (canvas "Settings"): the title, with Save changes in the top bar, then General, Code, Runtime,
// Budgets and targets, Flow, Jonah, Notifications, People with access and the Danger zone, with a list of the sections at the right
// from 1024 px. The four groups between General and People edit the factory definition through a draft
// session, the same one the Definition editor uses, and Save changes publishes it. The Danger zone shows the rows the caller may use:
// Pause on factory:manage in the factory's `can` (not for an archived factory), Archive or Restore on
// factory:archive and Delete on factory:delete, both in the organization's `can`. The card shows when at
// least one row does. The factory is the detail goald answers, so archived and paused stay current after an
// action; the sidebar's factory list leaves archived factories out.
import { useQuery } from '@tanstack/react-query';
import { type ReactNode, useEffect, useState } from 'react';
import { factoryQuery } from '../../api/queries';
import type { Factory } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory } from '../../shared/factory';
import { useOrg, useOrgHolds } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { SectionCard } from '../../ui/card';
import { DraftSessionProvider } from '../factory-editor/draft/use-draft';
import { BudgetsSection } from './budgets-section';
import { CodeSection } from './code-section';
import { DangerZone } from './danger-zone';
import { FlowSection } from './flow-section';
import { GeneralCard } from './general';
import { JonahSection } from './jonah-section';
import { NotificationsSection } from './notifications-section';
import { PauseRow } from './pause-row';
import { PeopleCard } from './people';
import { RuntimeSection } from './runtime-section';
import { SaveChangesAction, SaveRefusal, useSaveChanges } from './save-changes';

const sections = [
  { id: 'general', label: 'General' },
  { id: 'code', label: 'Code' },
  { id: 'runtime', label: 'Runtime' },
  { id: 'budgets', label: 'Budgets and targets' },
  { id: 'flow', label: 'Flow' },
  { id: 'jonah', label: 'Jonah' },
  { id: 'notifications', label: 'Notifications' },
  { id: 'people', label: 'People with access' },
  { id: 'danger', label: 'Danger zone' },
];

/** The id of the section in view: the topmost one crossing the upper part of the viewport; the first without IntersectionObserver. */
function useSectionInView(ids: string[]): string {
  const [active, setActive] = useState(ids[0] ?? '');
  const key = ids.join(',');
  useEffect(() => {
    if (typeof IntersectionObserver === 'undefined') return;
    const visible = new Set<string>();
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) visible.add(entry.target.id);
          else visible.delete(entry.target.id);
        }
        const first = key.split(',').find((id) => visible.has(id));
        if (first) setActive(first);
      },
      { rootMargin: '0px 0px -60% 0px' },
    );
    for (const id of key.split(',')) {
      const el = document.getElementById(id);
      if (el) observer.observe(el);
    }
    return () => observer.disconnect();
  }, [key]);
  return active;
}

export function FactorySettingsPage() {
  const org = useOrg();
  const listed = useFactory();
  // The detail answers archived and paused as they are now; the list stands in until it loads.
  const factory = useQuery(factoryQuery(org.id, listed.id)).data ?? listed;
  const mayArchive = useOrgHolds('factory:archive');
  const mayDelete = useOrgHolds('factory:delete');
  const mayPause = holds(factory, 'factory:manage') && !factory.archived_at;
  const showDanger = mayPause || mayArchive || mayDelete;
  // The draft session the groups write to; a caller without definition:edit sees the running version and no save.
  const canEdit = holds(factory, 'definition:edit');

  return (
    <DraftSessionProvider key={`${factory.id}:${canEdit}`} org={org.id} factory={factory.id} readOnly={!canEdit}>
      <SettingsBody org={org} factory={factory} canEdit={canEdit} showDanger={showDanger}>
        {mayPause ? <PauseRow org={org} factory={factory} /> : null}
        <DangerZone org={org} factory={factory} archives={mayArchive} deletes={mayDelete} />
      </SettingsBody>
    </DraftSessionProvider>
  );
}

function SettingsBody({
  org,
  factory,
  canEdit,
  showDanger,
  children,
}: {
  org: ReturnType<typeof useOrg>;
  factory: Factory;
  canEdit: boolean;
  showDanger: boolean;
  children: ReactNode;
}) {
  const save = useSaveChanges(org.id, factory);
  const visible = sections.filter((section) => section.id !== 'danger' || showDanger);
  const active = useSectionInView(visible.map((section) => section.id));
  return (
    <div className="flex max-w-6xl flex-col gap-4">
      {/* The top bar renders outside the draft provider, so the action carries the state read here. */}
      <TopBarSlot actions={canEdit ? <SaveChangesAction state={save} /> : undefined} />
      <h1 className="m-0 text-page-title">Settings</h1>
      <SaveRefusal refusal={save.refusal} />
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_200px] lg:gap-10">
        <div className="flex min-w-0 flex-col gap-4">
          <GeneralCard factory={factory} />
          <CodeSection />
          <RuntimeSection />
          <BudgetsSection />
          <FlowSection />
          <JonahSection />
          <NotificationsSection />
          <PeopleCard org={org} factory={factory} />
          {showDanger ? (
            <SectionCard
              id="danger"
              title={<span className="text-rework-ink">Danger zone</span>}
              className="border-rework/40"
            >
              {children}
            </SectionCard>
          ) : null}
        </div>
        <nav
          aria-label="On this page"
          className="hidden lg:sticky lg:top-6 lg:flex lg:flex-col lg:gap-1 lg:self-start lg:border-l lg:border-line lg:pl-4"
        >
          {visible.map((section) => (
            <a
              key={section.id}
              href={`#${section.id}`}
              aria-current={active === section.id ? 'true' : undefined}
              className="py-1 text-secondary text-ink-soft hover:text-ink aria-[current=true]:font-medium aria-[current=true]:text-ink"
            >
              {section.label}
            </a>
          ))}
        </nav>
      </div>
    </div>
  );
}
