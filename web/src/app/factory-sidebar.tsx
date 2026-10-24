// A factory's sidebar: the organization as eyebrow, the factory's name and title, then Work
// (Floor, Board, Items with what waits on you), Build and Learn (C §10.5, canvas "Main").
import { Link } from '@tanstack/react-router';
import type { Factory, OrgSummary } from '../api/types';
import { Count } from '../ui/count';
import { useFactorySquares } from './factory-squares';
import { factoryGroups, factoryScreens } from './screens';
import { entryClass, iconProps, LaterEntry, Sidebar, SidebarGroup } from './sidebar';

export function FactorySidebar({ org, factory }: { org: OrgSummary; factory: Factory }) {
  const params = { org: org.slug, factory: factory.name };
  const waiting = useFactorySquares(org).find((s) => s.factory.id === factory.id)?.attention.waiting ?? 0;
  // The title, unless it only repeats the name ("SRE" under "sre").
  const subtitle = factory.title && factory.title.toLowerCase() !== factory.name.toLowerCase() ? factory.title : null;

  return (
    <Sidebar label={`${factory.name} factory`} eyebrow={org.name} title={factory.name} subtitle={subtitle}>
      {factoryGroups.map((group) => (
        <SidebarGroup key={group.id} label={group.label}>
          {factoryScreens
            .filter((s) => s.group === group.id)
            .map((s) =>
              s.available ? (
                <li key={s.id}>
                  <Link to={s.to} params={params} activeOptions={{ exact: s.exact ?? false }} className={entryClass}>
                    <s.icon {...iconProps} />
                    {s.label}
                    {s.id === 'items' && waiting > 0 ? (
                      <Count value={waiting} label=" waiting on you" tone="you" />
                    ) : null}
                  </Link>
                </li>
              ) : (
                <LaterEntry key={s.id} label={s.label} icon={s.icon} />
              ),
            )}
        </SidebarGroup>
      ))}
    </Sidebar>
  );
}
