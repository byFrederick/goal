// The sidebar's Factories (sidebar.tsx): the label with New factory (+, for holders of factory:create),
// then one collapsible group per factory that is not archived. A group is its letter badge and name,
// with what it needs from people (a violet count) or an andon pull (a red dot). Only the open factory
// starts expanded; its pages are plain text under a guide line, and the current one is a white chip.
import { useQuery } from '@tanstack/react-query';
import { Link, useMatch } from '@tanstack/react-router';
import { ChevronDown, Plus } from 'lucide-react';
import { useId, useState } from 'react';
import { jonahOverviewQuery } from '../api/queries';
import type { OrgSummary } from '../api/types';
import { findFactory } from '../shared/factory';
import { useOrgHolds } from '../shared/org';
import { cn } from '../ui/cn';
import { Count } from '../ui/count';
import { NotAvailable } from '../ui/not-available';
import { FactoryBadge } from './factory-badge';
import { type FactorySquare, useFactorySquares } from './factory-squares';
import { newFactoryScreen, sidebarFactoryPages } from './screens';

/** A row of the sidebar: the current page is a white chip (aria-current); a disabled row stays in the tab order. */
export const rowClass =
  'group flex h-9 w-full items-center gap-3 rounded-control px-3 text-left text-body font-control text-ink-soft no-underline hover:bg-hover hover:text-ink aria-[current=page]:bg-card aria-[current=page]:text-ink aria-disabled:cursor-default aria-disabled:text-muted aria-disabled:hover:bg-transparent aria-disabled:hover:text-muted';

export function FactoryGroups({ org, onNavigate }: { org: OrgSummary; onNavigate?: (() => void) | undefined }) {
  const squares = useFactorySquares(org);
  // New factory only for whoever may create one (owners and admins).
  const mayCreate = useOrgHolds('factory:create');
  const open = useMatch({ from: '/o/$org/f/$factory', shouldThrow: false });
  const openId = open
    ? findFactory(
        squares.map((s) => s.factory),
        open.params.factory,
      )?.id
    : undefined;
  const labelId = useId();

  return (
    <div className="flex flex-col gap-1">
      <div className="flex h-8 items-center justify-between pr-1 pl-3">
        <p id={labelId} className="m-0 text-meta font-semibold text-muted">
          Factories
        </p>
        {mayCreate ? (
          <Link
            to={newFactoryScreen.to}
            params={{ org: org.slug, step: 'start' }}
            onClick={onNavigate}
            aria-label={newFactoryScreen.label}
            className="flex size-7 items-center justify-center rounded-control text-muted hover:bg-hover hover:text-ink"
          >
            <Plus size={16} strokeWidth={1.5} aria-hidden="true" />
          </Link>
        ) : null}
      </div>
      <ul aria-labelledby={labelId} className="m-0 flex list-none flex-col gap-1 p-0">
        {squares.map((square) => (
          <li key={square.factory.id}>
            <FactoryGroup org={org} square={square} current={square.factory.id === openId} onNavigate={onNavigate} />
          </li>
        ))}
      </ul>
    </div>
  );
}

/** A factory's group. Its header opens the factory's Floor; its chevron collapses or expands the pages. */
function FactoryGroup({
  org,
  square,
  current,
  onNavigate,
}: {
  org: OrgSummary;
  square: FactorySquare;
  current: boolean;
  onNavigate: (() => void) | undefined;
}) {
  // Expanded while it is the open factory, until the person collapses or expands it by hand.
  const [toggled, setToggled] = useState<boolean | null>(null);
  const expanded = toggled ?? current;
  const pagesId = useId();
  const { factory, mark, attention, label } = square;
  const params = { org: org.slug, factory: factory.name };
  // Jonah's waiting count is read for the expanded factory only; without it (a 503 while Jonah is off, or an error) the row shows no count.
  const jonah = useQuery({ ...jonahOverviewQuery(org.id, factory.id), enabled: expanded });
  const jonahWaiting = jonah.data?.waiting ?? 0;

  return (
    <>
      <div className="flex items-center gap-1">
        <Link
          to="/o/$org/f/$factory"
          params={params}
          activeOptions={{ exact: true }}
          onClick={onNavigate}
          aria-label={label}
          className="flex h-9 min-w-0 flex-1 items-center gap-2 rounded-control px-2 text-body font-control text-ink no-underline hover:bg-hover"
        >
          <FactoryBadge mark={mark} />
          <span className="min-w-0 flex-1 truncate">{factory.name}</span>
          {attention.waiting > 0 ? <Count value={attention.waiting} label=" waiting on you" tone="you" /> : null}
          {attention.andon !== null || attention.blocked > 0 ? (
            <span aria-hidden="true" className="mr-1 size-2.5 flex-none rounded-full bg-rework" />
          ) : null}
        </Link>
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={expanded ? pagesId : undefined}
          aria-label={`${expanded ? 'Collapse' : 'Expand'} ${factory.name}`}
          onClick={() => setToggled(!expanded)}
          className="flex size-8 flex-none items-center justify-center rounded-control text-muted hover:bg-hover hover:text-ink"
        >
          <ChevronDown
            size={16}
            strokeWidth={1.5}
            aria-hidden="true"
            className={cn('transition-transform duration-200', !expanded && '-rotate-90')}
          />
        </button>
      </div>
      {expanded ? (
        <ul id={pagesId} className="m-0 ml-5 flex list-none flex-col gap-1 border-l border-line p-0 pl-3">
          {sidebarFactoryPages.map((page) => (
            <li key={page.id}>
              {page.available ? (
                <Link
                  to={page.to}
                  params={params}
                  activeOptions={{ exact: page.exact ?? false }}
                  onClick={onNavigate}
                  className={rowClass}
                >
                  {page.label}
                  {page.id === 'jonah' && jonahWaiting > 0 ? (
                    <Count value={jonahWaiting} label=" waiting for your review" />
                  ) : null}
                </Link>
              ) : (
                <NotAvailable>
                  <button type="button" className={rowClass}>
                    {page.label}
                  </button>
                </NotAvailable>
              )}
            </li>
          ))}
        </ul>
      ) : null}
    </>
  );
}
