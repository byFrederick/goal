// The phone shell (below 768 px, MASTER.md "Phones", ADR 0016; screens/mobile/drawer.jpg): a top bar with
// the Goal mark, the page's title and its actions; a bottom tab bar with Needs you, Floor, Board, Items
// and More; and a drawer that the Goal mark and More open. The drawer shows the sidebar's content
// (sidebar.tsx), so the phone and the desktop list the same entries. ShellFrame (app-shell.tsx) draws
// these and keeps the drawer's state.
import { useQuery } from '@tanstack/react-query';
import { Link, useParams, useRouterState } from '@tanstack/react-router';
import { Ellipsis, Inbox, Kanban, LayoutGrid, type LucideIcon, TextAlignStart, X } from 'lucide-react';
import { Dialog as D } from 'radix-ui';
import { type ReactNode, type RefObject, useEffect, useRef, useSyncExternalStore } from 'react';
import { attentionQuery, factoriesQuery } from '../api/queries';
import type { Factory, OrgSummary } from '../api/types';
import { lastFactory } from '../shared/org';
import { GoalMark } from '../ui/goal-mark';
import { SidebarContent, useSidebarBusy } from './sidebar';

const phoneQuery = '(max-width: 767px)';

/** Whether the viewport is below 768 px, the width the phone shell is drawn at. False without matchMedia. */
export function usePhone(): boolean {
  return useSyncExternalStore(
    (onChange) => {
      if (typeof window.matchMedia !== 'function') return () => {};
      const query = window.matchMedia(phoneQuery);
      query.addEventListener('change', onChange);
      return () => query.removeEventListener('change', onChange);
    },
    () => typeof window.matchMedia === 'function' && window.matchMedia(phoneQuery).matches,
    () => false,
  );
}

interface PhoneTopBarProps {
  /** The page's title: its last crumb, or the page's name. */
  title: string | undefined;
  /** The page's actions, from its TopBarSlot. */
  actions: ReactNode | undefined;
  /** Opens the drawer (the sidebar's content). */
  onMenu: () => void;
  drawerOpen: boolean;
  /** The Goal mark's button: the drawer returns focus to it when it closes. */
  menuRef: RefObject<HTMLButtonElement | null>;
}

/** The top bar: the Goal mark (opens the drawer), the page title and the page's actions. */
export function PhoneTopBar({ title, actions, onMenu, drawerOpen, menuRef }: PhoneTopBarProps) {
  return (
    <header className="sticky top-0 z-20 flex min-h-14 flex-none items-center gap-1 border-b border-rule bg-shell pt-[env(safe-area-inset-top)] px-1 md:hidden">
      <button
        ref={menuRef}
        type="button"
        aria-label="Open menu"
        aria-haspopup="dialog"
        aria-expanded={drawerOpen}
        onClick={onMenu}
        className="flex size-11 flex-none items-center justify-center rounded-control text-ink-soft hover:bg-hover hover:text-ink"
      >
        <GoalMark size="shell" />
      </button>
      <p className="m-0 min-w-0 flex-1 truncate text-heading text-ink">{title}</p>
      {actions ? <div className="flex min-w-0 flex-none items-center gap-2">{actions}</div> : null}
    </header>
  );
}

interface PhoneDrawerProps {
  org: OrgSummary;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Where focus goes when the drawer closes: the Goal mark. */
  returnFocus: RefObject<HTMLButtonElement | null>;
}

/**
 * The sidebar's content as a drawer from the left, labelled by the organization's name. Radix traps
 * focus while it is open, Escape and Close dismiss it, and focus goes back to the Goal mark.
 * Following a link, or any page change, closes it and leaves focus on the new page.
 */
export function PhoneDrawer({ org, open, onOpenChange, returnFocus }: PhoneDrawerProps) {
  const busy = useSidebarBusy(org.id);
  // Why the drawer closes: a navigation leaves focus where the new page puts it; Escape and Close return it.
  const closedBy = useRef<'person' | 'navigation'>('person');
  return (
    <D.Root open={open} onOpenChange={onOpenChange}>
      {/* Any page change closes the drawer, including the organization menu's switch, which has no link here. */}
      <PageWatch
        onChange={() => {
          closedBy.current = 'navigation';
          onOpenChange(false);
        }}
      />
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-40 bg-ink/45 md:hidden data-[state=open]:animate-[fade-in_150ms_ease-out]" />
        <D.Content
          aria-describedby={undefined}
          onOpenAutoFocus={() => {
            closedBy.current = 'person';
          }}
          onCloseAutoFocus={(e) => {
            const navigated = closedBy.current === 'navigation';
            closedBy.current = 'person';
            e.preventDefault();
            if (!navigated) returnFocus.current?.focus();
          }}
          className="fixed inset-y-0 left-0 z-50 flex h-dvh w-80 flex-col overflow-y-auto overscroll-contain border-r border-line bg-sidebar px-2 pt-[calc(8px_+_env(safe-area-inset-top))] pb-[calc(12px_+_env(safe-area-inset-bottom))] shadow-pop outline-none data-[state=open]:animate-[fade-in_150ms_ease-out] md:hidden"
        >
          <D.Title className="sr-only">{org.name}</D.Title>
          <nav aria-label="Organization and factories" aria-busy={busy} className="flex min-h-full flex-1 flex-col">
            <SidebarContent
              org={org}
              onNavigate={() => {
                closedBy.current = 'navigation';
                onOpenChange(false);
              }}
              close={
                <D.Close asChild>
                  <button
                    type="button"
                    aria-label="Close menu"
                    className="flex size-11 flex-none items-center justify-center rounded-control text-ink-soft hover:bg-hover hover:text-ink"
                  >
                    <X size={18} strokeWidth={1.5} aria-hidden="true" />
                  </button>
                </D.Close>
              }
            />
          </nav>
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}

/** Calls `onChange` when the page changes after it mounted. */
function PageWatch({ onChange }: { onChange: () => void }) {
  const path = useRouterState({ select: (s) => s.location.pathname });
  const seen = useRef(path);
  useEffect(() => {
    if (seen.current === path) return;
    seen.current = path;
    onChange();
  }, [path, onChange]);
  return null;
}

const tab =
  'flex min-h-16 flex-col items-center justify-center gap-1 text-meta font-medium text-muted no-underline transition-colors duration-200';
const tabActive = { className: 'font-semibold text-run-ink' } as const;

interface PhoneTabBarProps {
  org: OrgSummary;
  /** Opens the drawer, the same one the Goal mark opens. */
  onMore: () => void;
}

/**
 * Needs you (with its violet count), Floor, Board, Items and More. Floor, Board and Items go to the open
 * factory; outside a factory, to the last factory visited here, else the first one, else the
 * organization's Factories home.
 */
export function PhoneTabBar({ org, onMore }: PhoneTabBarProps) {
  // The factory the URL names (a factory page), by name or id as the URL has it.
  const { factory: openFactory } = useParams({ strict: false });
  const { data: factories = [] } = useQuery(factoriesQuery(org.id));
  const factory = openFactory ?? tabFactory(org.id, factories)?.name;

  return (
    <nav
      aria-label="Tabs"
      className="fixed inset-x-0 bottom-0 z-30 grid grid-cols-5 border-t border-line bg-card pb-[env(safe-area-inset-bottom)] md:hidden"
    >
      <NeedsYouTab org={org} />
      <FactoryTab org={org} factory={factory} to="/o/$org/f/$factory" exact label="Floor" icon={LayoutGrid} />
      <FactoryTab org={org} factory={factory} to="/o/$org/f/$factory/board" label="Board" icon={Kanban} />
      <FactoryTab org={org} factory={factory} to="/o/$org/f/$factory/items" label="Items" icon={TextAlignStart} />
      <button type="button" aria-haspopup="dialog" onClick={onMore} className={tab}>
        <Ellipsis size={20} strokeWidth={1.5} aria-hidden="true" />
        More
      </button>
    </nav>
  );
}

/** Needs you: the organization's page, with the count of what waits on a person in a violet badge. */
function NeedsYouTab({ org }: { org: OrgSummary }) {
  const waiting = useQuery(attentionQuery(org.id)).data?.waiting_on_you ?? 0;
  return (
    <Link
      to="/o/$org/needs-you"
      params={{ org: org.slug }}
      className={tab}
      activeProps={tabActive}
      aria-label={waiting > 0 ? `Needs you, ${waiting} waiting on you` : 'Needs you'}
    >
      <span className="relative">
        <Inbox size={20} strokeWidth={1.5} aria-hidden="true" />
        {waiting > 0 ? (
          <span
            aria-hidden="true"
            className="absolute -top-1 -right-2 flex h-4 min-w-4 items-center justify-center rounded-chip bg-you px-1 text-meta leading-none font-semibold text-white"
          >
            {waiting}
          </span>
        ) : null}
      </span>
      Needs you
    </Link>
  );
}

/** The factory a tab goes to outside a factory page: the last one visited here, else the first listed. */
function tabFactory(orgId: string, factories: readonly Factory[]): Factory | undefined {
  return lastFactory(orgId, factories) ?? factories[0];
}

interface FactoryTabProps {
  org: OrgSummary;
  /** The factory the tab opens; undefined when the organization has none, so the tab opens its home. */
  factory: string | undefined;
  to: '/o/$org/f/$factory' | '/o/$org/f/$factory/board' | '/o/$org/f/$factory/items';
  exact?: boolean;
  label: string;
  icon: LucideIcon;
}

function FactoryTab({ org, factory, to, exact = false, label, icon: Icon }: FactoryTabProps) {
  const body = (
    <>
      <Icon size={20} strokeWidth={1.5} aria-hidden="true" />
      {label}
    </>
  );
  if (factory === undefined) {
    return (
      <Link to="/o/$org" params={{ org: org.slug }} className={tab} activeProps={tabActive}>
        {body}
      </Link>
    );
  }
  return (
    <Link to={to} params={{ org: org.slug, factory }} activeOptions={{ exact }} className={tab} activeProps={tabActive}>
      {body}
    </Link>
  );
}
