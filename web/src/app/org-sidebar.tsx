// The organization's sidebar on its own pages: "Organization" as eyebrow, its name and factory
// count, then Factories, Members and roles, API keys (with api_key:read or personal_token:read),
// Connections (with connection:read; a rust dot while one fails), Knowledge (with knowledge:read)
// and Settings (C §10.5, MASTER.md navigation, canvas "Factories", "Members", "Api-Keys",
// "Connections" and "Knowledge"), with the caller's role under the count.
import { Link } from '@tanstack/react-router';
import type { OrgSummary } from '../api/types';
import { useConnectionTrouble } from '../shared/connections';
import { factoryCount, useOrgGrants } from '../shared/org';
import { Count } from '../ui/count';
import { orgScreens, screensFor } from './screens';
import { entryClass, iconProps, Sidebar, SidebarGroup } from './sidebar';

/** "You’re an owner." */
const roleArticle = { owner: 'an owner', admin: 'an admin', member: 'a member' } as const;

export function OrgSidebar({ org }: { org: OrgSummary }) {
  const can = useOrgGrants()?.can;
  const trouble = useConnectionTrouble(org.id);
  return (
    <Sidebar
      label={`${org.name} organization`}
      eyebrow="Organization"
      title={org.name}
      subtitle={`${factoryCount(org.factory_count)}.${org.role ? ` You’re ${roleArticle[org.role]}.` : ''}`}
    >
      <SidebarGroup>
        {screensFor(orgScreens, can).map((s) => (
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
              {s.id === 'connections' && trouble > 0 ? <TroubleDot count={trouble} /> : null}
            </Link>
          </li>
        ))}
      </SidebarGroup>
    </Sidebar>
  );
}

/** A rust dot at the end of the entry, said as ", 1 failing". */
function TroubleDot({ count }: { count: number }) {
  return (
    <span className="ml-auto inline-flex items-center">
      <span
        aria-hidden="true"
        className="size-2.5 rounded-full bg-rework shadow-[0_0_0_2px_var(--color-shell)] group-aria-[current=page]:shadow-[0_0_0_2px_var(--color-ink)]"
      />
      <span className="sr-only">, {count} failing</span>
    </span>
  );
}
