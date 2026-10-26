// The app frame (C §10.2): it fills the viewport like a normal web app, with no outer frame or
// margin. From 768 px: rail | sidebar | main, each pinned to the left edge at full height. Below:
// the top row, the crumb, main and, on factory pages, the bottom tab bar.
//
// AppShell (the /o/$org layout) draws what every organization page shares: the skip link, the
// rail or the top row, and the reconnecting banner. Each child layout passes its own sidebar,
// phone crumb and tab bar to ShellFrame, which draws them around `main`: the organization's
// pages (/o/$org/_org) the organization sidebar, a factory's pages the factory sidebar.
import type { ReactNode } from 'react';
import type { OrgSummary } from '../api/types';
import { useStreamStatus } from '../shared/stream';
import { cn } from '../ui/cn';
import { AccountProvider } from './account-menu';
import { PhoneTopRow } from './phone-chrome';
import { Rail } from './rail';

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
        <Rail org={org} />
        <PhoneTopRow org={org} />
        {children}
        <ReconnectingBanner />
      </div>
    </AccountProvider>
  );
}

interface FrameProps {
  /** The sidebar (hidden below 768 px). */
  sidebar: ReactNode;
  /** Above main on phones only ("Acme / sre"). */
  crumb?: ReactNode;
  /** Fixed to the bottom on phones only; main leaves room for it. */
  tabBar?: ReactNode;
  children: ReactNode;
}

export function ShellFrame({ sidebar, crumb, tabBar, children }: FrameProps) {
  return (
    <>
      {sidebar}
      <div className="flex min-w-0 flex-1 flex-col">
        {crumb}
        {/* A container: screens lay out by the width left beside the sidebar, not the viewport's. */}
        <main
          id="main"
          tabIndex={-1}
          className={cn(
            '@container flex min-w-0 flex-1 flex-col gap-5 px-4 pt-[18px] outline-none md:px-6 md:pt-7 md:pb-10 lg:px-9',
            tabBar ? 'pb-28' : 'pb-8',
          )}
        >
          {children}
        </main>
      </div>
      {tabBar}
    </>
  );
}

/** Says so, politely, only while the organization's live stream is reconnecting. */
function ReconnectingBanner() {
  const status = useStreamStatus();
  return (
    <p role="status" className="pointer-events-none fixed inset-x-0 top-3 z-40 m-0 flex justify-center px-4">
      {status === 'reconnecting' ? (
        <span className="inline-flex items-center gap-2 rounded-control border border-rework-line bg-surface px-3 py-2 text-[13px] text-ink shadow-lift">
          <span aria-hidden="true" className="size-2 rounded-full bg-rework" />
          Reconnecting to goald; changes appear once it answers.
        </span>
      ) : null}
    </p>
  );
}
