// Phone chrome (< 768 px, C §10.6): a top row with the organization mark and the factory squares,
// the crumb "Acme / sre" on factory pages, and the factory's bottom tab bar (canvas
// "Mobile-Floor", "Mobile-Factories").
import { Link } from '@tanstack/react-router';
import { Bot, ChevronRight, Ellipsis, Plus } from 'lucide-react';
import { useState } from 'react';
import type { Factory, OrgSummary } from '../api/types';
import { cn } from '../ui/cn';
import { Sheet, SheetContent, SheetLabel, SheetTrigger } from '../ui/sheet';
import { useFactorySquares } from './factory-squares';
import { OrgMark } from './org-mark';
import { AccountMenu, FactorySquareLink, NewFactoryLink, useRailKeys } from './rail-items';
import { factoriesScreen, factoryScreens, laterText } from './screens';

export function PhoneTopRow({ org }: { org: OrgSummary }) {
  const squares = useFactorySquares(org);
  const onKeyDown = useRailKeys('horizontal');

  return (
    <nav
      aria-label="Organization and factories"
      onKeyDown={onKeyDown}
      className="flex min-h-14 items-center gap-1.5 bg-shell px-3 pt-2 md:hidden"
    >
      <OrgMark org={org} size="phone" />
      <span aria-hidden="true" className="mx-0.5 h-[26px] w-px shrink-0 bg-rule" />
      {/* The squares scroll inside the row, never the page; the padding keeps badges unclipped. */}
      <ul className="m-0 -my-1.5 flex min-w-0 flex-1 list-none items-center gap-1.5 overflow-x-auto py-1.5 pr-1.5 pl-0">
        {squares.map((square) => (
          <li key={square.factory.id}>
            <FactorySquareLink org={org} square={square} size="phone" />
          </li>
        ))}
        <li>
          <NewFactoryLink org={org} size="phone">
            <Plus size={16} strokeWidth={1.5} aria-hidden="true" />
          </NewFactoryLink>
        </li>
      </ul>
      <AccountMenu size="phone" />
    </nav>
  );
}

/** "Acme / sre" above a factory page's heading on phones. */
export function PhoneCrumb({ org, factory }: { org: OrgSummary; factory: Factory }) {
  return (
    <p className="m-0 mx-4 mt-1 flex gap-1.5 text-[13px] text-muted md:hidden">
      <span>{org.name}</span>
      <span aria-hidden="true">/</span>
      <span className="font-medium text-ink">{factory.name}</span>
    </p>
  );
}

const tab =
  'flex min-h-14 flex-col items-center justify-center gap-1 text-xs text-muted no-underline transition-colors duration-200';
const tabActive = { className: 'font-semibold text-ink shadow-[inset_0_2px_0_var(--color-ink)]' } as const;

/**
 * Floor, Board, Items, Build, More. Build goes to the first Build screen that exists (Definition);
 * More opens a sheet with Knowledge and Jonah (later) and the organization's Factories.
 */
export function TabBar({ org, factory }: { org: OrgSummary; factory: Factory }) {
  const params = { org: org.slug, factory: factory.name };
  const work = factoryScreens.filter((s) => s.group === 'work');
  // Build goes to the first Build screen that exists.
  const build = factoryScreens.find((s) => s.group === 'build' && s.available);

  return (
    <nav
      aria-label="Factory"
      className="fixed inset-x-0 bottom-0 z-30 grid grid-cols-5 border-t border-line bg-card pb-[env(safe-area-inset-bottom)] md:hidden"
    >
      {work.map((s) =>
        s.available ? (
          <Link
            key={s.id}
            to={s.to}
            params={params}
            activeOptions={{ exact: s.exact ?? false }}
            className={tab}
            activeProps={tabActive}
          >
            <s.icon size={20} strokeWidth={1.5} aria-hidden="true" />
            {s.label}
          </Link>
        ) : null,
      )}
      {build?.available ? (
        <Link to={build.to} params={params} className={tab} activeProps={tabActive}>
          <Bot size={20} strokeWidth={1.5} aria-hidden="true" />
          Build
        </Link>
      ) : (
        <span aria-disabled="true" className={cn(tab, 'cursor-default')}>
          <Bot size={20} strokeWidth={1.5} aria-hidden="true" />
          Build
          <span className="sr-only">, {laterText}</span>
        </span>
      )}
      <MoreSheet org={org} />
    </nav>
  );
}

const sheetRow =
  'flex min-h-12 items-center gap-3 rounded-control px-2.5 text-base text-ink no-underline transition-colors duration-200';

function MoreSheet({ org }: { org: OrgSummary }) {
  const [open, setOpen] = useState(false);
  const learn = factoryScreens.filter((s) => s.group === 'learn');
  const Factories = factoriesScreen.icon;

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger className={tab}>
        <Ellipsis size={20} strokeWidth={1.5} aria-hidden="true" />
        More
      </SheetTrigger>
      <SheetContent title="More">
        <SheetLabel id="more-learn">Learn</SheetLabel>
        <ul aria-labelledby="more-learn" className="m-0 flex list-none flex-col gap-0.5 p-0">
          {learn.map((s) => (
            <li key={s.id} aria-disabled="true" className={cn(sheetRow, 'text-muted')}>
              <s.icon size={18} strokeWidth={1.5} aria-hidden="true" />
              {s.label}
              <span className="ml-auto text-xs">{laterText}</span>
            </li>
          ))}
        </ul>
        <div aria-hidden="true" className="my-2 h-px bg-line" />
        <SheetLabel id="more-org">{org.name}</SheetLabel>
        <ul aria-labelledby="more-org" className="m-0 flex list-none flex-col gap-0.5 p-0">
          <li>
            <Link
              to="/o/$org"
              params={{ org: org.slug }}
              onClick={() => setOpen(false)}
              className={cn(sheetRow, 'hover:bg-shell')}
            >
              <Factories size={18} strokeWidth={1.5} aria-hidden="true" />
              {factoriesScreen.label}
              <ChevronRight size={16} strokeWidth={1.5} aria-hidden="true" className="ml-auto text-muted" />
            </Link>
          </li>
        </ul>
      </SheetContent>
    </Sheet>
  );
}
