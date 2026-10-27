// The app frame (MASTER.md "The shell"): it fills the viewport like a normal web app, with no outer
// frame or margin. From 768 px: the sidebar | the top bar over main, the sidebar pinned to the left
// edge at full height. Below: the top bar (the Goal mark, the page's title and actions) over main, the
// tab bar at the bottom on organization pages and the drawer from the Goal mark (phone-chrome.tsx).
//
// AppShell (the /o/$org layout) draws what every organization page shares: the skip link, the
// sidebar, and the reconnecting banner. ShellFrame draws the top bar, main and the phone's tab bar
// and drawer, and holds the state they share.
import { type ReactNode, useRef, useState } from 'react';
import type { OrgSummary } from '../api/types';
import { useOrg } from '../shared/org';
import { useStreamStatus } from '../shared/stream';
import { cn } from '../ui/cn';
import { AccountProvider } from './account-menu';
import { PhoneDrawer, PhoneTabBar, PhoneTopBar, usePhone } from './phone-chrome';
import { Sidebar } from './sidebar';
import { TopBar, TopBarSetContext, type TopBarState, usePageLabel } from './top-bar';

export function AppShell({ org, children }: { org: OrgSummary; children: ReactNode }) {
  return (
    <AccountProvider>
      <div className="flex min-h-dvh flex-col bg-shell pr-[env(safe-area-inset-right)] pl-[env(safe-area-inset-left)] md:flex-row">
        <a
          href="#main"
          className="sr-only z-50 rounded-control bg-ink px-4 py-2 text-white focus:not-sr-only focus:fixed focus:top-3 focus:left-3"
        >
          Skip to content
        </a>
        <Sidebar org={org} />
        {children}
        <ReconnectingBanner />
      </div>
    </AccountProvider>
  );
}

interface FrameProps {
  /** Whether the phone's tab bar shows at the bottom. False on the New factory wizard, which has its own footer. */
  tabs?: boolean;
  children: ReactNode;
}

export function ShellFrame({ tabs = true, children }: FrameProps) {
  // A page's last crumb and actions, set by its <TopBarSlot> and shown in the top bar (top-bar.tsx).
  const [topBar, setTopBar] = useState<TopBarState | null>(null);
  // The drawer the Goal mark and More open on phones (phone-chrome.tsx).
  const [drawer, setDrawer] = useState(false);
  const menu = useRef<HTMLButtonElement>(null);
  const phone = usePhone();
  const org = useOrg();
  const pageTitle = usePageLabel();
  const openDrawer = () => setDrawer(true);

  return (
    <TopBarSetContext.Provider value={setTopBar}>
      <div className="flex min-w-0 flex-1 flex-col">
        {phone ? (
          <PhoneTopBar
            title={topBar?.crumb ?? pageTitle}
            actions={topBar?.actions}
            onMenu={openDrawer}
            drawerOpen={drawer}
            menuRef={menu}
          />
        ) : (
          <TopBar state={topBar} />
        )}
        {/* A container: screens lay out by the width left beside the sidebar, not the viewport's. */}
        <main
          id="main"
          tabIndex={-1}
          className={cn(
            '@container flex min-w-0 flex-1 flex-col gap-5 px-4 pt-5 outline-none md:px-7 md:py-5',
            tabs ? 'pb-28' : 'pb-8',
          )}
        >
          {children}
        </main>
      </div>
      {phone && tabs ? <PhoneTabBar org={org} onMore={openDrawer} /> : null}
      <PhoneDrawer org={org} open={phone && drawer} onOpenChange={setDrawer} returnFocus={menu} />
    </TopBarSetContext.Provider>
  );
}

/** Says so, politely, only while the organization's live stream is reconnecting. */
function ReconnectingBanner() {
  const status = useStreamStatus();
  return (
    <p role="status" className="pointer-events-none fixed inset-x-0 top-3 z-40 m-0 flex justify-center px-4">
      {status === 'reconnecting' ? (
        <span className="inline-flex items-center gap-2 rounded-control border border-rework-tint bg-card px-3 py-2 text-secondary text-ink shadow-pop">
          <span aria-hidden="true" className="size-2 rounded-full bg-rework" />
          Reconnecting to goald; changes appear once it answers.
        </span>
      ) : null}
    </p>
  );
}
