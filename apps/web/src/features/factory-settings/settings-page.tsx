// Settings of a factory (canvas "Settings"): the title, General, People with access and the Danger zone,
// with a list of the sections at the right from 1024 px. The Danger zone shows the rows the caller may use:
// Pause on factory:manage in the factory's `can` (not for an archived factory), Archive or Restore on
// factory:archive and Delete on factory:delete, both in the organization's `can`. The card shows when at
// least one row does. The factory is the detail goald answers, so archived and paused stay current after an
// action; the sidebar's factory list leaves archived factories out.
import { useQuery } from '@tanstack/react-query';
import { factoryQuery } from '../../api/queries';
import { useFactory } from '../../shared/factory';
import { useOrg, useOrgHolds } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { SectionCard } from '../../ui/card';
import { DangerZone } from './danger-zone';
import { GeneralCard } from './general';
import { PauseRow } from './pause-row';
import { PeopleCard } from './people';

const sections = [
  { id: 'general', label: 'General' },
  { id: 'people', label: 'People with access' },
  { id: 'danger', label: 'Danger zone' },
];

export function FactorySettingsPage() {
  const org = useOrg();
  const listed = useFactory();
  // The detail answers archived and paused as they are now; the list stands in until it loads.
  const factory = useQuery(factoryQuery(org.id, listed.id)).data ?? listed;
  const mayArchive = useOrgHolds('factory:archive');
  const mayDelete = useOrgHolds('factory:delete');
  const mayPause = holds(factory, 'factory:manage') && !factory.archived_at;
  const showDanger = mayPause || mayArchive || mayDelete;

  return (
    <div className="flex max-w-6xl flex-col gap-4">
      <h1 className="m-0 text-page-title">Settings</h1>
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_200px] lg:gap-10">
        <div className="flex min-w-0 flex-col gap-4">
          <GeneralCard factory={factory} />
          <PeopleCard org={org} factory={factory} />
          {showDanger ? (
            <SectionCard
              id="danger"
              title={<span className="text-rework-ink">Danger zone</span>}
              className="border-rework/40"
            >
              {mayPause ? <PauseRow org={org} factory={factory} /> : null}
              <DangerZone org={org} factory={factory} archives={mayArchive} deletes={mayDelete} />
            </SectionCard>
          ) : null}
        </div>
        <nav
          aria-label="On this page"
          className="hidden lg:sticky lg:top-6 lg:flex lg:flex-col lg:gap-1 lg:self-start lg:border-l lg:border-line lg:pl-4"
        >
          {sections
            .filter((section) => section.id !== 'danger' || showDanger)
            .map((section) => (
              <a key={section.id} href={`#${section.id}`} className="py-1 text-secondary text-ink-soft hover:text-ink">
                {section.label}
              </a>
            ))}
        </nav>
      </div>
    </div>
  );
}
