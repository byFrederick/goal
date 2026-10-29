// The sidebar (≥ 768 px, MASTER.md "The shell"): 256 px by default, resizable from 216 to 400 px, or the
// 60 px rail, pinned left at full height beside the content on every organization page and on the New factory wizard. Top to bottom: the Goal mark and the
// organization's name (its menu), Search (sidebar-search.tsx) and Collapse (the 60 px rail: sidebar-rail.tsx);
// New item (new-item.tsx, with C); the organization-wide pages; Factories (sidebar-factories.tsx); and at the
// bottom the spend this week and the person (account-menu.tsx). Search replaces that body with its input and
// results. Command K expands the rail into search. Below 768 px the same content opens as the phone's drawer
// from the Goal mark (phone-chrome.tsx), so SidebarContent is the one list of entries for both.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ChevronsUpDown, CircleHelp, PanelLeft, Search } from 'lucide-react';
import { type CSSProperties, type ReactNode, type RefObject, useCallback, useEffect, useRef, useState } from 'react';
import { attentionQuery, overviewQuery } from '../api/queries';
import type { FactoryOverviewList, OrgSummary } from '../api/types';
import { useConnectionTrouble } from '../shared/connections';
import { formatUsdCompact } from '../shared/format';
import { NewItemFromSidebar } from '../shared/new-item';
import { useOrgGrants } from '../shared/org';
import { useSidebarSize } from '../shared/sidebar-choice';
import { shownWidth } from '../shared/sidebar-width';
import { Count } from '../ui/count';
import { GoalMark } from '../ui/goal-mark';
import { Menu, MenuTrigger } from '../ui/menu';
import { NotAvailable } from '../ui/not-available';
import { Tooltip } from '../ui/tooltip';
import { AccountMenu } from './account-menu';
import { CreateOrgDialog } from './create-org-dialog';
import { OrgMenuContent } from './org-menu';
import { docsLink } from './rail-items';
import { screensFor, sidebarOrgScreens } from './screens';
import { FactoryGroups, rowClass } from './sidebar-factories';
import { SidebarRail } from './sidebar-rail';
import { SidebarResizeHandle, sidebarWidthVar } from './sidebar-resize-handle';
import { iconButton, SidebarSearch, useSearchShortcut } from './sidebar-search';

const iconProps = { size: 18, strokeWidth: 1.5, 'aria-hidden': true } as const;

/**
 * The sidebar from 768 px: 256 px by default, resizable from 216 to 400 px by its right edge, or the 60 px
 * rail once it folds (the size is saved: sidebar-choice.ts). Its content is SidebarContent, which the
 * phone's drawer renders too. The column keeps the same element whether it shows the sidebar or the rail,
 * so the resize handle stays mounted across the swap.
 */
export function Sidebar({ org }: { org: OrgSummary }) {
  const busy = useSidebarBusy(org.id);
  const [size, setSize] = useSidebarSize();
  const column = useRef<HTMLDivElement>(null);
  // The control that was pressed hands focus to its counterpart, so the keyboard does not fall to the page.
  const focus = useRef<'rail' | 'top' | null>(null);
  // Command K on the rail expands the sidebar straight into search.
  const [searchOnExpand, setSearchOnExpand] = useState(false);

  const expand = useCallback(
    (search: boolean) => {
      focus.current = 'top';
      setSearchOnExpand(search);
      setSize({ ...size, collapsed: false });
    },
    [size, setSize],
  );
  const collapse = useCallback(() => {
    focus.current = 'rail';
    setSearchOnExpand(false);
    setSize({ ...size, collapsed: true });
  }, [size, setSize]);

  return (
    <>
      {/* C keeps working with the rail shown, and on phones where the sidebar is hidden. */}
      {size.collapsed ? <NewItemFromSidebar button={false} /> : null}
      <div
        ref={column}
        className="sticky top-0 hidden h-dvh shrink-0 transition-[width] duration-200 ease-out motion-reduce:transition-none md:flex"
        style={{ width: `var(${sidebarWidthVar})`, [sidebarWidthVar]: `${shownWidth(size)}px` } as CSSProperties}
      >
        {size.collapsed ? (
          <SidebarRail org={org} busy={busy} focusOnMount={focus.current === 'rail'} onExpand={expand} />
        ) : (
          <nav
            aria-label="Organization and factories"
            aria-busy={busy}
            className="flex h-full min-w-0 flex-1 flex-col overflow-x-hidden overflow-y-auto border-r border-line bg-sidebar px-2 pt-2 pb-3"
          >
            <SidebarContent
              org={org}
              onCollapse={collapse}
              focusCollapse={focus.current === 'top'}
              startSearching={searchOnExpand}
            />
          </nav>
        )}
        <SidebarResizeHandle size={size} column={column} onChange={setSize} />
      </div>
    </>
  );
}

interface SidebarContentProps {
  org: OrgSummary;
  /** Called when a link in the content is followed (the phone's drawer closes). */
  onNavigate?: () => void;
  /** Folds the sidebar into the rail (the sidebar's Collapse). The drawer passes `close` instead. */
  onCollapse?: () => void;
  /** Collapse takes focus on mount: the rail's Expand was pressed. */
  focusCollapse?: boolean;
  /** Opens straight into search: Command K on the rail expanded the sidebar. */
  startSearching?: boolean;
  /** Replaces Collapse in the top row (the drawer's Close). */
  close?: ReactNode;
}

/**
 * The sidebar's content: the organization's menu, Search, New item, the organization pages,
 * Factories, the spend and the person. Shared by the sidebar and the phone's drawer (phone-chrome.tsx).
 * Search lives here, so the drawer searches the same way the sidebar does.
 */
export function SidebarContent({
  org,
  onNavigate,
  onCollapse,
  focusCollapse = false,
  startSearching = false,
  close,
}: SidebarContentProps) {
  const can = useOrgGrants()?.can;
  const attention = useQuery(attentionQuery(org.id));
  const overview = useQuery(overviewQuery(org.id));
  const trouble = useConnectionTrouble(org.id);
  const busy = useSidebarBusy(org.id);
  const [searching, setSearching] = useState(startSearching);
  const [composeWith, setComposeWith] = useState<{ title: string } | null>(null);
  const searchIcon = useRef<HTMLButtonElement>(null);
  const returnFocus = useRef(false);

  const openSearch = useCallback(() => setSearching(true), []);
  useSearchShortcut(openSearch);

  const closeSearch = useCallback((focusIcon: boolean) => {
    returnFocus.current = focusIcon;
    setSearching(false);
  }, []);
  // The search's New item row opens the composer with the query as its title, once the sidebar's New item is back.
  const openComposer = useCallback((title: string) => setComposeWith({ title }), []);
  const composed = useCallback(() => setComposeWith(null), []);
  // The Search icon is back in the DOM once the search closes; Escape and Close search return focus to it.
  useEffect(() => {
    if (!searching && returnFocus.current) {
      returnFocus.current = false;
      searchIcon.current?.focus();
    }
  }, [searching]);

  return (
    <div className="flex min-h-full flex-1 flex-col">
      {busy ? <span className="sr-only">Loading</span> : null}
      {searching ? (
        <SidebarSearch org={org} onClose={closeSearch} onNewItem={openComposer} />
      ) : (
        <>
          <SidebarTop
            org={org}
            close={close}
            onCollapse={onCollapse}
            focusCollapse={focusCollapse}
            searchIcon={searchIcon}
            onSearch={openSearch}
          />
          <NewItemFromSidebar openWith={composeWith} onOpened={composed} />
          <ul className="m-0 mt-2 flex list-none flex-col gap-1 p-0">
            {screensFor(sidebarOrgScreens, can).map((screen) => (
              <li key={screen.id}>
                <OrgEntry
                  org={org}
                  screen={screen}
                  waiting={attention.data?.waiting_on_you ?? 0}
                  trouble={trouble}
                  onNavigate={onNavigate}
                />
              </li>
            ))}
          </ul>
          <div className="mt-4">
            <FactoryGroups org={org} onNavigate={onNavigate} />
          </div>
          <div className="mt-auto flex flex-col gap-2 pt-4">
            {/* A plain link out of the SPA: /docs is served by the same origin, so the same tab loads it. */}
            <a href={docsLink.href} className={rowClass}>
              <CircleHelp {...iconProps} className="flex-none" />
              <span className="min-w-0 flex-1 truncate">{docsLink.label}</span>
            </a>
            <SpendThisWeek org={org} overview={overview.data} />
            <AccountMenu org={org} />
          </div>
        </>
      )}
    </div>
  );
}

/** Whether the sidebar's attention or overview is still loading (the sidebar is aria-busy then). */
export function useSidebarBusy(orgId: string): boolean {
  const attention = useQuery(attentionQuery(orgId));
  const overview = useQuery(overviewQuery(orgId));
  return attention.isPending || overview.isPending;
}

/**
 * The organization's name opens its menu, and Search opens search; Collapse folds the sidebar into the
 * rail (ADR 0016, decision 4), unless `close` takes its place (the drawer).
 */
function SidebarTop({
  org,
  close,
  onCollapse,
  focusCollapse,
  searchIcon,
  onSearch,
}: {
  org: OrgSummary;
  close?: ReactNode;
  onCollapse?: (() => void) | undefined;
  focusCollapse: boolean;
  searchIcon: RefObject<HTMLButtonElement | null>;
  onSearch: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const navigating = useRef(false);
  const collapse = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (focusCollapse) collapse.current?.focus();
  }, [focusCollapse]);

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
      <button
        ref={searchIcon}
        type="button"
        aria-label="Search"
        aria-keyshortcuts="Meta+K Control+K"
        onClick={onSearch}
        className={iconButton}
      >
        <Search {...iconProps} />
      </button>
      {close ?? (
        <Tooltip label="Collapse sidebar">
          <button
            ref={collapse}
            type="button"
            aria-label="Collapse sidebar"
            onClick={onCollapse}
            className={iconButton}
          >
            <PanelLeft {...iconProps} />
          </button>
        </Tooltip>
      )}
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
  onNavigate,
}: {
  org: OrgSummary;
  screen: (typeof sidebarOrgScreens)[number];
  waiting: number;
  trouble: number;
  onNavigate?: (() => void) | undefined;
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
      onClick={onNavigate}
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
 * Spend this week against the weekly budgets (getFactoryOverview, window 7d), a link to Spend and
 * billing. Without a budget the amount shows alone. Missing while the overview is loading or has
 * failed; the rest of the sidebar still renders.
 */
function SpendThisWeek({ org, overview }: { org: OrgSummary; overview: FactoryOverviewList | undefined }) {
  if (!overview) return null;
  const { spend_usd: spend, budget_usd: budget } = overview;
  const spent = formatUsdCompact(spend);
  if (budget === null) {
    return (
      <Link
        to="/o/$org/spend"
        params={{ org: org.slug }}
        className="flex flex-wrap items-baseline justify-between gap-x-2 rounded-cell border border-line bg-card p-3 text-meta text-muted"
      >
        <span className="min-w-0 truncate">Spend this week</span>
        <span className="whitespace-nowrap text-secondary font-semibold text-ink">{spent}</span>
      </Link>
    );
  }
  const of = `${spent} of ${formatUsdCompact(budget)}`;
  const pct = budget > 0 ? Math.min(100, (spend / budget) * 100) : 0;
  return (
    <Link
      to="/o/$org/spend"
      params={{ org: org.slug }}
      className="flex flex-col gap-2 rounded-cell border border-line bg-card p-3"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-x-2 text-meta text-muted">
        <span className="min-w-0 truncate">Spend this week</span>
        <span className="whitespace-nowrap text-secondary font-semibold text-ink">{of}</span>
      </div>
      {/* Decorative: the amount above says the same in words. */}
      <div aria-hidden="true" className="h-1.5 overflow-hidden rounded-chip bg-hover">
        <span className="block h-full rounded-chip bg-run" style={{ width: `${pct}%` }} />
      </div>
    </Link>
  );
}
