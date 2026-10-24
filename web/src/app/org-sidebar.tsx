// The organization's sidebar on its own pages: "Organization" as eyebrow, its name and factory
// count, then Factories, API keys (with api_key:read), Connector catalog, Credentials and Settings
// (C §10.5, canvas "Factories" and "Api-Keys").
import { Link } from '@tanstack/react-router';
import type { OrgSummary } from '../api/types';
import { factoryCount, useOrgGrants } from '../shared/org';
import { Count } from '../ui/count';
import { orgScreens, screensFor } from './screens';
import { entryClass, iconProps, LaterEntry, Sidebar, SidebarGroup } from './sidebar';

export function OrgSidebar({ org }: { org: OrgSummary }) {
  const can = useOrgGrants()?.can;
  return (
    <Sidebar
      label={`${org.name} organization`}
      eyebrow="Organization"
      title={org.name}
      subtitle={`${factoryCount(org.factory_count)}.`}
    >
      <SidebarGroup>
        {screensFor(orgScreens, can).map((s) =>
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
