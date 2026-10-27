// The sidebar (≥ 768 px, MASTER.md "The shell"): 256 px, pinned left at full height beside the content
// on every organization page and on the New factory wizard. Top to bottom: the Goal mark and the
// organization's name (its menu), Search and Collapse (not built yet); New item (new-item.tsx, with C); the
// organization-wide pages; Factories (sidebar-factories.tsx); and at the bottom the spend this week and
// the person (account-menu.tsx). Below 768 px the top row and the tab bar take its place (phone-chrome.tsx).
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ChevronsUpDown, PanelLeft, Search } from 'lucide-react';
import { useRef, useState } from 'react';
import { attentionQuery, overviewQuery } from '../api/queries';
import type { FactoryOverviewList, OrgSummary } from '../api/types';
import { useConnectionTrouble } from '../shared/connections';
import { formatUsdCompact } from '../shared/format';
import { NewItemFromSidebar } from '../shared/new-item';
import { useOrgGrants } from '../shared/org';
import { Count } from '../ui/count';
import { GoalMark } from '../ui/goal-mark';
import { Menu, MenuTrigger } from '../ui/menu';
import { NotAvailable } from '../ui/not-available';
import { AccountMenu } from './account-menu';
import { CreateOrgDialog } from './create-org-dialog';
import { OrgMenuContent } from './org-menu';
import { screensFor, sidebarOrgScreens } from './screens';
import { FactoryGroups, rowClass } from './sidebar-factories';

const iconProps = { size: 18, strokeWidth: 1.5, 'aria-hidden': true } as const;

const iconButton =
  'flex size-8 flex-none items-center justify-center rounded-control text-ink-soft hover:bg-hover hover:text-ink aria-disabled:cursor-default aria-disabled:text-muted aria-disabled:hover:bg-transparent';

export function Sidebar({ org }: { org: OrgSummary }) {
  const can = useOrgGrants()?.can;
  const attention = useQuery(attentionQuery(org.id));
  const overview = useQuery(overviewQuery(org.id));
  const trouble = useConnectionTrouble(org.id);
  const busy = attention.isPending || overview.isPending;

  return (
    <nav
      aria-label="Organization and factories"
      aria-busy={busy}
      className="sticky top-0 hidden h-dvh w-64 shrink-0 flex-col overflow-y-auto border-r border-line bg-sidebar px-2 pt-2 pb-3 md:flex"
    >
      {busy ? <span className="sr-only">Loading</span> : null}
      <SidebarTop org={org} />
      <NewItemFromSidebar />
      <ul className="m-0 mt-2 flex list-none flex-col gap-1 p-0">
        {screensFor(sidebarOrgScreens, can).map((screen) => (
          <li key={screen.id}>
            <OrgEntry org={org} screen={screen} waiting={attention.data?.waiting_on_you ?? 0} trouble={trouble} />
          </li>
        ))}
      </ul>
      <div className="mt-4">
        <FactoryGroups org={org} />
      </div>
      <div className="mt-auto flex flex-col gap-2 pt-4">
        <SpendThisWeek overview={overview.data} />
        <AccountMenu org={org} size="sidebar" />
      </div>
    </nav>
  );
}

/** The organization's name opens its menu; Search and Collapse are not built yet (ADR 0016, decision 4). */
function SidebarTop({ org }: { org: OrgSummary }) {
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const navigating = useRef(false);

  return (
    <div className="flex items-center gap-1">
      <Menu open={open} onOpenChange={setOpen}>
        <MenuTrigger
          ref={trigger}
          aria-label={`${org.name} organization, switch organization`}
          className="flex h-10 min-w-0 flex-1 items-center gap-2 rounded-control px-2 text-left text-heading text-ink hover:bg-hover aria-expanded:bg-hover"
        >
          <GoalMark size="shell" />
          <span className="min-w-0 flex-1 truncate">{org.name}</span>
          <ChevronsUpDown {...iconProps} size={16} className="flex-none text-muted" />
        </MenuTrigger>
        <OrgMenuContent
          org={org}
          onCreate={() => {
            setOpen(false);
            setCreating(true);
          }}
          navigating={navigating}
        />
      </Menu>
      <NotAvailable>
        <button type="button" aria-label="Search" aria-keyshortcuts="Meta+K" className={iconButton}>
          <Search {...iconProps} />
        </button>
      </NotAvailable>
      <NotAvailable>
        <button type="button" aria-label="Collapse sidebar" className={iconButton}>
          <PanelLeft {...iconProps} />
        </button>
      </NotAvailable>
      <CreateOrgDialog open={creating} onOpenChange={setCreating} returnFocus={trigger} />
    </div>
  );
}

/** An organization-wide entry: a link to its page, or a disabled button while the page is not built. */
function OrgEntry({
  org,
  screen,
  waiting,
  trouble,
}: {
  org: OrgSummary;
  screen: (typeof sidebarOrgScreens)[number];
  waiting: number;
  trouble: number;
}) {
  const body = (
    <>
      {screen.icon ? <screen.icon {...iconProps} className="flex-none" /> : null}
      <span className="min-w-0 flex-1 truncate">{screen.label}</span>
      {screen.id === 'needs-you' && waiting > 0 ? ' ' : null}
      {screen.id === 'needs-you' && waiting > 0 ? <Count value={waiting} label=" waiting on you" tone="you" /> : null}
      {screen.id === 'connections' && trouble > 0 ? <TroubleDot count={trouble} /> : null}
    </>
  );
  if (!screen.available) {
    return (
      <NotAvailable>
        <button type="button" className={rowClass}>
          {body}
        </button>
      </NotAvailable>
    );
  }
  return (
    <Link
      to={screen.to}
      params={{ org: org.slug }}
      activeOptions={{ exact: screen.exact ?? false }}
      className={rowClass}
    >
      {body}
    </Link>
  );
}

/** A rust dot at the end of the row, said as ", 1 failing" (Connections, while a connection's last test failed). */
function TroubleDot({ count }: { count: number }) {
  return (
    <>
      <span aria-hidden="true" className="ml-auto size-2.5 flex-none rounded-full bg-rework" />
      <span className="sr-only">, {count} failing</span>
    </>
  );
}

/**
 * Spend this week against the weekly budgets (getFactoryOverview, window 7d). Not a link: Spend and
 * billing has no page yet. Without a budget the amount shows alone. Missing while the overview is
 * loading or has failed; the rest of the sidebar still renders.
 */
function SpendThisWeek({ overview }: { overview: FactoryOverviewList | undefined }) {
  if (!overview) return null;
  const { spend_usd: spend, budget_usd: budget } = overview;
  const spent = formatUsdCompact(spend);
  if (budget === null) {
    return (
      <div className="flex items-baseline justify-between gap-2 rounded-cell border border-line bg-card p-3 text-meta text-muted">
        <span>Spend this week</span>
        <span className="text-secondary font-semibold text-ink">{spent}</span>
      </div>
    );
  }
  const of = `${spent} of ${formatUsdCompact(budget)}`;
  const pct = budget > 0 ? Math.min(100, (spend / budget) * 100) : 0;
  return (
    <div className="flex flex-col gap-2 rounded-cell border border-line bg-card p-3">
      <div className="flex items-baseline justify-between gap-2 text-meta text-muted">
        <span>Spend this week</span>
        <span className="text-secondary font-semibold text-ink">{of}</span>
      </div>
      {/* Decorative: the amount above says the same in words. */}
      <div aria-hidden="true" className="h-1.5 overflow-hidden rounded-chip bg-hover">
        <span className="block h-full rounded-chip bg-run" style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}
