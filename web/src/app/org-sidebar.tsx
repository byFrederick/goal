// The organization's sidebar on its own pages: "Organization" as eyebrow, its name and factory
// count, then Factories, Connector catalog, Credentials and Settings (C §10.5, canvas
// "Factories"). Members and roles arrive with phase 4 (no-auth).
import { Link } from '@tanstack/react-router';
import type { OrgSummary } from '../api/types';
import { Count } from '../ui/count';
import { orgScreens } from './screens';
import { entryClass, iconProps, LaterEntry, Sidebar, SidebarGroup } from './sidebar';

function factoryCount(n: number): string {
  return `${n} ${n === 1 ? 'factory' : 'factories'}`;
}

export function OrgSidebar({ org }: { org: OrgSummary }) {
  return (
    <Sidebar
      label={`${org.name} organization`}
      eyebrow="Organization"
      title={org.name}
      subtitle={`${factoryCount(org.factory_count)}.`}
    >
      <SidebarGroup>
        {orgScreens.map((s) =>
          s.available ? (
            <li key={s.id}>
              <Link
                to={s.to}
                params={{ org: org.slug }}
                activeOptions={{ exact: s.exact ?? false }}
                className={entryClass}
              >
                <s.icon {...iconProps} />
                {s.label}
                {s.id === 'factories' ? <Count value={org.factory_count} label=" factories" /> : null}
              </Link>
            </li>
          ) : (
            <LaterEntry key={s.id} label={s.label} icon={s.icon} />
          ),
        )}
      </SidebarGroup>
    </Sidebar>
  );
}
