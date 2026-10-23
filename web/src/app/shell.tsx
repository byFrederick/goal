// The app frame: the concrete shell with a sidebar from 768 px, a bottom tab bar below it.
import { Link } from '@tanstack/react-router';
import { Columns3, Ellipsis, LayoutGrid, List, SearchCheck } from 'lucide-react';
import { type ReactNode, useState } from 'react';
import type { Factory } from '../api/types';
import { FactoryLinks, iconProps, LaterLinks, LiveStatus, navItem, railLabel } from '../shared/chrome';
import { cn } from '../ui/cn';
import { Dialog, DialogContent } from '../ui/dialog';
import { LogoMark } from '../ui/logo';

export function Shell({ factory, children }: { factory: Factory; children: ReactNode }) {
  return (
    <div className="min-h-dvh bg-shell md:bg-ground md:p-3 lg:p-6">
      <a
        href="#main"
        className="sr-only z-50 rounded-control bg-ink px-4 py-2 text-white focus:not-sr-only focus:fixed focus:top-3 focus:left-3"
      >
        Skip to content
      </a>
      <div className="mx-auto flex max-w-[1392px] gap-6 md:min-h-[calc(100dvh-24px)] md:rounded-shell md:border md:border-line md:bg-shell md:py-5 md:pr-5 md:pl-3 md:shadow-shell lg:min-h-[calc(100dvh-48px)] lg:pr-6 lg:pl-4">
        <Sidebar factory={factory} />
        <main
          id="main"
          tabIndex={-1}
          className="flex min-w-0 flex-1 flex-col gap-5 px-4 pt-4 pb-28 outline-none md:p-0 md:pt-1"
        >
          {children}
        </main>
      </div>
      <TabBar factory={factory} />
    </div>
  );
}

const sideActive = { className: 'bg-ink text-white hover:bg-ink', 'aria-current': 'page' } as const;

function Sidebar({ factory }: { factory: Factory }) {
  const params = { factory: factory.name };
  const item = cn(navItem, 'hover:bg-nav-hover');
  return (
    <nav aria-label="Main" className="hidden shrink-0 flex-col gap-1 md:flex md:w-11 lg:w-[208px] xl:w-[228px]">
      <Link
        to="/f/$factory"
        params={params}
        className="mb-4 flex items-center gap-2.5 px-1 py-1 text-[21px] tracking-display no-underline"
      >
        <LogoMark className="md:max-lg:mx-auto" />
        <span className="md:max-lg:sr-only">goal</span>
      </Link>
      <Link to="/f/$factory" params={params} activeOptions={{ exact: true }} className={item} activeProps={sideActive}>
        <LayoutGrid {...iconProps} />
        <span className={railLabel}>Floor</span>
      </Link>
      <Link to="/f/$factory/board" params={params} className={item} activeProps={sideActive}>
        <Columns3 {...iconProps} />
        <span className={railLabel}>Board</span>
      </Link>
      <Link to="/f/$factory/items" params={params} className={item} activeProps={sideActive}>
        <List {...iconProps} />
        <span className={railLabel}>Items</span>
      </Link>
      <LaterLinks />

      <h2 className="m-0 mt-6 mb-1.5 ml-3 text-[13px] font-normal text-muted md:max-lg:sr-only">Factories</h2>
      <FactoryLinks current={factory.name} />

      <div className="mt-auto border-t border-line px-2 pt-3.5 md:max-lg:px-0">
        <LiveStatus className="md:max-lg:justify-center" textClassName={railLabel} />
      </div>
    </nav>
  );
}

const tab =
  'flex min-h-14 flex-col items-center justify-center gap-1 text-xs text-muted no-underline transition-colors duration-200';
const tabActive = {
  className: 'font-semibold text-ink shadow-[inset_0_2px_0_var(--color-ink)]',
  'aria-current': 'page',
} as const;

function TabBar({ factory }: { factory: Factory }) {
  const params = { factory: factory.name };
  const [more, setMore] = useState(false);

  return (
    <nav
      aria-label="Main"
      className="fixed inset-x-0 bottom-0 z-30 grid grid-cols-5 border-t border-line bg-card pb-[env(safe-area-inset-bottom)] md:hidden"
    >
      <Link to="/f/$factory" params={params} activeOptions={{ exact: true }} className={tab} activeProps={tabActive}>
        <LayoutGrid size={20} strokeWidth={1.5} aria-hidden="true" />
        Floor
      </Link>
      <Link to="/f/$factory/board" params={params} className={tab} activeProps={tabActive}>
        <Columns3 size={20} strokeWidth={1.5} aria-hidden="true" />
        Board
      </Link>
      <Link to="/f/$factory/items" params={params} className={tab} activeProps={tabActive}>
        <List size={20} strokeWidth={1.5} aria-hidden="true" />
        Items
      </Link>
      <span aria-disabled="true" title="Coming in a later phase" className={cn(tab, 'cursor-default')}>
        <SearchCheck size={20} strokeWidth={1.5} aria-hidden="true" />
        Jonah
      </span>
      <Dialog open={more} onOpenChange={setMore}>
        <button type="button" className={tab} onClick={() => setMore(true)} aria-haspopup="dialog">
          <Ellipsis size={20} strokeWidth={1.5} aria-hidden="true" />
          More
        </button>
        <DialogContent title="More" variant="sheet">
          <section aria-labelledby="more-factories" className="flex flex-col gap-1">
            <h3 id="more-factories" className="m-0 mb-1 text-[13px] font-normal text-muted">
              Factories
            </h3>
            <FactoryLinks current={factory.name} onNavigate={() => setMore(false)} />
          </section>
          <section aria-labelledby="more-later" className="flex flex-col gap-1">
            <h3 id="more-later" className="m-0 mb-1 text-[13px] font-normal text-muted">
              Coming later
            </h3>
            <LaterLinks />
          </section>
          <LiveStatus />
        </DialogContent>
      </Dialog>
    </nav>
  );
}
