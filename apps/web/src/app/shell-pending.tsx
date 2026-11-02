// What the organization and factory layouts show while they load the org and the factory: the shell's
// frame in muted blocks (the sidebar, the top bar with the crumbs the URL already tells, the content
// padding) and the page skeleton in the content slot, so nothing jumps when the shell arrives
// (MOTION.md, Loading; docs/design/screens/account/states.jpg, Pending). The frame is still: only the
// page skeleton in the slot pulses and fades (Skeleton).
//
// A nested layout's pending component renders inside AppShell, which already drew the sidebar. AppShell
// says so through InsideShellContext, and ShellPending then draws only the top bar and the content.
import { useParams, useRouterState } from '@tanstack/react-router';
import { ChevronRight, Ellipsis, Inbox, Kanban, LayoutGrid, type LucideIcon, TextAlignStart } from 'lucide-react';
import { createContext, type ReactNode, useContext } from 'react';
import { JonahShape } from '../features/jonah/jonah-page';
import { useSidebarSize } from '../shared/sidebar-choice';
import { shownWidth } from '../shared/sidebar-width';
import { cn } from '../ui/cn';
import {
  AgentShape,
  AgentsShape,
  BoardShape,
  FloorShape,
  HomeShape,
  ItemShape,
  ItemsShape,
  ListShape,
  PageShape,
  PendingSkeleton,
} from './route-skeletons';

/** True under AppShell: the sidebar is already on screen. */
export const InsideShellContext = createContext(false);

const block = 'rounded-control bg-hover';

function SidebarPlaceholder() {
  const [size] = useSidebarSize();
  return (
    <div className="shell-sidebar sticky top-0 hidden h-dvh shrink-0 md:flex" style={{ width: shownWidth(size) }}>
      <nav
        aria-label="Organization and factories"
        aria-busy="true"
        className="flex h-full min-w-0 flex-1 flex-col gap-3 overflow-hidden border-r border-line bg-sidebar px-2 pt-2 pb-3"
      >
        <div className={cn(block, 'h-9')} />
        <div className={cn(block, 'h-9')} />
        <div className="flex flex-col gap-2">
          <div className={cn(block, 'h-8')} />
          <div className={cn(block, 'h-8')} />
          <div className={cn(block, 'h-8')} />
        </div>
      </nav>
    </div>
  );
}

/** The crumbs as far as the URL tells: the organization and, on a factory page, its name. Plain text. */
function Crumbs() {
  const { org, factory } = useParams({ strict: false }) as { org?: string; factory?: string };
  const crumb = 'truncate text-secondary text-muted';
  return (
    <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-2">
      {org ? <span className={crumb}>{org}</span> : null}
      {factory ? (
        <>
          <ChevronRight aria-hidden="true" className="size-4 flex-none text-muted" />
          <span className={crumb}>{factory}</span>
        </>
      ) : null}
    </nav>
  );
}

/** The shape of the page the URL is opening: the same shapes the pages' own pendingComponents draw. */
export function shapeFor(pathname: string): ReactNode {
  const parts = pathname.split('/').filter(Boolean);
  // /o/<org>[/f/<factory>[/<page>[/<id>]]]
  if (parts[0] !== 'o') return <PageShape />;
  if (parts[2] !== 'f') {
    if (parts[2] === undefined) return <HomeShape />;
    return parts[2] === 'runs' ? <ListShape /> : <PageShape />;
  }
  const page = parts[4];
  if (page === undefined) return <FloorShape />;
  if (page === 'board') return <BoardShape />;
  if (page === 'items') return parts[5] === undefined ? <ItemsShape /> : <ItemShape />;
  if (page === 'agents') return parts[5] === undefined ? <AgentsShape /> : <AgentShape />;
  if (page === 'jonah' && parts[5] === undefined) return <JonahShape />;
  return <PageShape />;
}

const tabClass = 'flex min-h-16 flex-col items-center justify-center gap-1 text-meta font-medium text-muted';

function TabPlaceholder({ icon: Icon, label, current }: { icon: LucideIcon; label: string; current: boolean }) {
  return (
    <span className={cn(tabClass, current && 'font-semibold text-ink')}>
      <Icon size={20} strokeWidth={1.5} aria-hidden="true" />
      {label}
    </span>
  );
}

/** The phone's tab bar at PhoneTabBar's place and height, still and without a count; the current tab from the URL. */
function TabBarPlaceholder() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const page = pathname.split('/').filter(Boolean)[4];
  const factoryPage = pathname.split('/').filter(Boolean)[2] === 'f';
  const needsYou = pathname.endsWith('/needs-you');
  return (
    <nav
      aria-label="Tabs"
      className="fixed inset-x-0 bottom-0 z-30 grid grid-cols-5 border-t border-line bg-card pb-[env(safe-area-inset-bottom)] md:hidden"
    >
      <TabPlaceholder icon={Inbox} label="Needs you" current={needsYou} />
      <TabPlaceholder icon={LayoutGrid} label="Floor" current={factoryPage && page === undefined} />
      <TabPlaceholder icon={Kanban} label="Board" current={page === 'board'} />
      <TabPlaceholder icon={TextAlignStart} label="Items" current={page === 'items'} />
      <TabPlaceholder icon={Ellipsis} label="More" current={false} />
    </nav>
  );
}

export function ShellPending() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const inside = useContext(InsideShellContext);
  const body = (
    <div className="flex min-w-0 flex-1 flex-col">
      <header className="shell-topbar hidden h-14 flex-none items-center gap-2 border-b border-rule bg-shell px-7 md:flex">
        <Crumbs />
      </header>
      <header className="shell-topbar sticky top-0 z-20 flex min-h-14 flex-none items-center gap-1 border-b border-rule bg-shell px-1 md:hidden">
        <div className="size-11 flex-none" />
        <div className="flex min-w-16 flex-1 items-center">
          <div className={cn(block, 'h-5 w-32 max-w-full')} />
        </div>
      </header>
      <main
        id="main"
        tabIndex={-1}
        className="shell-main @container flex min-w-0 flex-1 flex-col gap-5 px-4 pt-5 pb-28 outline-none md:px-7 md:py-5"
      >
        <PendingSkeleton placeholder={shapeFor(pathname)} />
      </main>
      {inside ? null : <TabBarPlaceholder />}
    </div>
  );
  if (inside) return body;
  return (
    <div className="flex min-h-dvh flex-col bg-shell md:flex-row">
      <SidebarPlaceholder />
      {body}
    </div>
  );
}
