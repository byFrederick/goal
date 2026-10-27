// The collapsed sidebar (MASTER.md "Collapsed rail", shell.html "Collapsed"): a 60 px column in the
// sidebar's place, from 768 px up. Top to bottom: the Goal mark, Expand sidebar, New item (not built
// yet), the organization-wide pages as icons, the factories as squares with their counts, New factory
// (+), the spend ring and the person's avatar. It reads the same data as the 256 px sidebar, which it
// replaces (sidebar.tsx), and it shares the factory squares with the phone's top row (rail-items.tsx).
import { Link } from '@tanstack/react-router';
import { PanelLeft, Plus } from 'lucide-react';
import { useEffect, useRef } from 'react';
import type { FactoryOverviewList, OrgSummary } from '../api/types';
import { formatUsdCompact } from '../shared/format';
import { useOrgGrants, useOrgHolds } from '../shared/org';
import { GoalMark } from '../ui/goal-mark';
import { NotAvailable } from '../ui/not-available';
import { Tooltip } from '../ui/tooltip';
import { AccountMenu } from './account-menu';
import { useFactorySquares } from './factory-squares';
import { FactorySquareLink, NewFactoryLink } from './rail-items';
import { screensFor, sidebarOrgScreens } from './screens';

const iconProps = { size: 18, strokeWidth: 1.5, 'aria-hidden': true } as const;

/** A 40 px square: a row's hover and current states, and the disabled ones of the sidebar's rows. */
const railButton =
  'relative flex size-10 flex-none items-center justify-center rounded-control text-ink-soft no-underline hover:bg-hover hover:text-ink aria-[current=page]:bg-card aria-[current=page]:text-ink aria-disabled:cursor-default aria-disabled:text-muted aria-disabled:hover:bg-transparent';

interface SidebarRailProps {
  org: OrgSummary;
  /** Attention or overview still loading: aria-busy and a spoken "Loading", no placeholder shapes. */
  busy: boolean;
  /** What waits on a person (0 while the counts are missing). */
  waiting: number;
  /** Connections whose last test failed (0 while the counts are missing). */
  trouble: number;
  /** Missing while the overview is loading or has failed: no spend ring then. */
  overview: FactoryOverviewList | undefined;
  /** The Expand button takes focus when the rail appears by the person's own Collapse. */
  focusOnMount: boolean;
  onExpand: () => void;
}

export function SidebarRail({ org, busy, waiting, trouble, overview, focusOnMount, onExpand }: SidebarRailProps) {
  const can = useOrgGrants()?.can;
  const mayCreate = useOrgHolds('factory:create');
  const expand = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (focusOnMount) expand.current?.focus();
  }, [focusOnMount]);

  return (
    <nav
      aria-label="Organization and factories"
      aria-busy={busy}
      className="sticky top-0 hidden h-dvh w-15 shrink-0 flex-col items-center gap-1 overflow-y-auto border-r border-line bg-sidebar pt-2 pb-3 md:flex"
    >
      {busy ? <span className="sr-only">Loading</span> : null}
      <span className="flex size-10 flex-none items-center justify-center">
        <GoalMark size="shell" />
      </span>
      <Tooltip label="Expand sidebar">
        <button ref={expand} type="button" aria-label="Expand sidebar" onClick={onExpand} className={railButton}>
          <PanelLeft {...iconProps} />
        </button>
      </Tooltip>
      <NotAvailable>
        <button
          type="button"
          aria-label="New item"
          className="flex size-10 flex-none items-center justify-center rounded-control bg-run text-white aria-disabled:cursor-default"
        >
          <Plus {...iconProps} size={16} />
        </button>
      </NotAvailable>
      <span aria-hidden="true" className="my-1 h-px w-7 flex-none bg-rule" />
      <ul className="m-0 flex list-none flex-col items-center gap-1 p-0">
        {screensFor(sidebarOrgScreens, can).map((screen) => (
          <li key={screen.id}>
            <RailEntry org={org} screen={screen} waiting={waiting} trouble={trouble} />
          </li>
        ))}
      </ul>
      <span aria-hidden="true" className="my-1 h-px w-7 flex-none bg-rule" />
      <ul className="m-0 flex list-none flex-col items-center gap-1 p-0">
        <RailFactories org={org} mayCreate={mayCreate} />
      </ul>
      <div className="mt-auto flex flex-col items-center gap-1 pt-4">
        <SpendRing overview={overview} />
        <AccountMenu org={org} size="rail" />
      </div>
    </nav>
  );
}

/** One organization-wide page as an icon: a link, or a disabled button while its page is not built. */
function RailEntry({
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
  const needs = screen.id === 'needs-you' && waiting > 0;
  const failing = screen.id === 'connections' && trouble > 0;
  const name = needs
    ? `${screen.label}, ${waiting} waiting on you`
    : failing
      ? `${screen.label}, ${trouble} failing`
      : screen.label;
  const body = (
    <>
      {screen.icon ? <screen.icon {...iconProps} /> : null}
      {needs ? (
        <span
          aria-hidden="true"
          className="absolute top-0.5 right-0 flex h-4 min-w-4 items-center justify-center rounded-chip border-2 border-sidebar bg-you px-1 text-meta leading-none font-semibold text-white"
        >
          {waiting}
        </span>
      ) : null}
      {failing ? (
        <span
          aria-hidden="true"
          className="absolute top-1 right-1 size-2.5 rounded-full border-2 border-sidebar bg-rework"
        />
      ) : null}
    </>
  );
  if (!screen.available) {
    return (
      <NotAvailable>
        <button type="button" aria-label={name} className={railButton}>
          {body}
        </button>
      </NotAvailable>
    );
  }
  return (
    <Tooltip label={screen.label}>
      <Link
        to={screen.to}
        params={{ org: org.slug }}
        activeOptions={{ exact: screen.exact ?? false }}
        aria-label={name}
        className={railButton}
      >
        {body}
      </Link>
    </Tooltip>
  );
}

/** The factories as squares, then New factory for whoever may create one. Each square says its status. */
function RailFactories({ org, mayCreate }: { org: OrgSummary; mayCreate: boolean }) {
  const squares = useFactorySquares(org);
  return (
    <>
      {squares.map((square) => (
        <li key={square.factory.id}>
          <FactorySquareLink org={org} square={square} tip />
        </li>
      ))}
      {mayCreate ? (
        <li>
          <NewFactoryLink org={org} tip>
            <Plus size={16} strokeWidth={1.5} aria-hidden="true" />
          </NewFactoryLink>
        </li>
      ) : null}
    </>
  );
}

const ringRadius = 10;
const circumference = 2 * Math.PI * ringRadius;

/**
 * Spend this week as a ring, the arc the share of the weekly budgets spent. Not a control (Spend and
 * billing has no page yet), so it is an image with its words in its name, not a focusable tooltip.
 * Without a budget the ring is an empty track and the name carries the amount.
 */
function SpendRing({ overview }: { overview: FactoryOverviewList | undefined }) {
  if (!overview) return null;
  const { spend_usd: spend, budget_usd: budget } = overview;
  const share = budget ? Math.min(1, spend / budget) : 0;
  const label =
    budget === null
      ? `Spend this week, ${formatUsdCompact(spend)}`
      : `Spend this week, ${Math.round(share * 100)}% of budget`;
  return (
    <span role="img" aria-label={label} className="flex size-7 flex-none">
      <svg viewBox="0 0 26 26" aria-hidden="true" focusable="false" className="size-7">
        <circle cx={13} cy={13} r={ringRadius} fill="none" strokeWidth={3} className="stroke-hover" />
        {share > 0 ? (
          <circle
            cx={13}
            cy={13}
            r={ringRadius}
            fill="none"
            strokeWidth={3}
            strokeLinecap="round"
            className="stroke-run"
            strokeDasharray={`${share * circumference} ${circumference}`}
            transform="rotate(-90 13 13)"
          />
        ) : null}
      </svg>
    </span>
  );
}
