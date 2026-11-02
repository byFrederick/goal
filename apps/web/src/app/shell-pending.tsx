// What the organization and factory layouts show while they load the org and the factory: the shell's
// frame in muted blocks (the sidebar, the top bar with the crumbs the URL already tells, the content
// padding) and the page skeleton in the content slot, so nothing jumps when the shell arrives
// (MOTION.md, Loading; docs/design/screens/account/states.jpg, Pending). The frame is still: only the
// page skeleton in the slot pulses and fades (Skeleton).
//
// A nested layout's pending component renders inside AppShell, which already drew the sidebar. AppShell
// says so through InsideShellContext, and ShellPending then draws only the top bar and the content.
import { useParams } from '@tanstack/react-router';
import { ChevronRight } from 'lucide-react';
import { createContext, useContext } from 'react';
import { useSidebarSize } from '../shared/sidebar-choice';
import { shownWidth } from '../shared/sidebar-width';
import { cn } from '../ui/cn';
import { PageShape, PendingSkeleton } from './route-skeletons';

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

export function ShellPending() {
  const inside = useContext(InsideShellContext);
  const body = (
    <div className="flex min-w-0 flex-1 flex-col">
      <header className="shell-topbar hidden h-14 flex-none items-center gap-2 border-b border-rule bg-shell px-7 md:flex">
        <Crumbs />
      </header>
      <header className="shell-topbar sticky top-0 z-20 flex min-h-14 flex-none items-center gap-1 border-b border-rule bg-shell px-1 md:hidden">
        <div className="size-11 flex-none" />
        <Crumbs />
      </header>
      <main
        id="main"
        tabIndex={-1}
        className="shell-main @container flex min-w-0 flex-1 flex-col gap-5 px-4 pt-5 pb-28 outline-none md:px-7 md:py-5"
      >
        <PendingSkeleton placeholder={<PageShape />} />
      </main>
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
