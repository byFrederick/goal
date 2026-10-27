// The app frame (MASTER.md "The shell"): it fills the viewport like a normal web app, with no outer
// frame or margin. From 768 px: the sidebar | the top bar over main, the sidebar pinned to the left
// edge at full height. Below: the top row, the crumb, main and, on factory pages, the bottom tab bar.
//
// AppShell (the /o/$org layout) draws what every organization page shares: the skip link, the
// sidebar or the top row, and the reconnecting banner. Each child layout passes its phone crumb and
// tab bar to ShellFrame, which draws them around `main`.
import { type ReactNode, useState } from 'react';
import type { OrgSummary } from '../api/types';
import { useStreamStatus } from '../shared/stream';
import { cn } from '../ui/cn';
import { AccountProvider } from './account-menu';
import { PhoneTopRow } from './phone-chrome';
import { Sidebar } from './sidebar';
import { TopBar, TopBarSetContext, type TopBarState } from './top-bar';

export function AppShell({ org, children }: { org: OrgSummary; children: ReactNode }) {
  return (
    <AccountProvider org={org}>
      <div className="flex min-h-dvh flex-col bg-shell pr-[env(safe-area-inset-right)] pl-[env(safe-area-inset-left)] md:flex-row">
        <a
          href="#main"
          className="sr-only z-50 rounded-control bg-ink px-4 py-2 text-white focus:not-sr-only focus:fixed focus:top-3 focus:left-3"
        >
          Skip to content
        </a>
        <Sidebar org={org} />
        <PhoneTopRow org={org} />
        {children}
        <ReconnectingBanner />
      </div>
    </AccountProvider>
  );
}

interface FrameProps {
  /** Above main on phones only ("Acme / sre"). */
  crumb?: ReactNode;
  /** Fixed to the bottom on phones only; main leaves room for it. */
  tabBar?: ReactNode;
  children: ReactNode;
}

export function ShellFrame({ crumb, tabBar, children }: FrameProps) {
  // A page's last crumb and actions, set by its <TopBarSlot> and shown in the top bar (top-bar.tsx).
  const [topBar, setTopBar] = useState<TopBarState | null>(null);
  return (
    <TopBarSetContext.Provider value={setTopBar}>
      <div className="flex min-w-0 flex-1 flex-col">
        {crumb}
        <TopBar state={topBar} />
        {/* A container: screens lay out by the width left beside the sidebar, not the viewport's. */}
        <main
          id="main"
          tabIndex={-1}
          className={cn(
            '@container flex min-w-0 flex-1 flex-col gap-5 px-4 pt-5 outline-none md:px-7 md:py-5',
            tabBar ? 'pb-28' : 'pb-8',
          )}
        >
          {children}
        </main>
      </div>
      {tabBar}
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
