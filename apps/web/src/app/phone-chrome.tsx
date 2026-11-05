// The phone shell (below 768 px, MASTER.md "Phones", ADR 0016; screens/mobile/drawer.jpg): a top bar with
// the Goal mark, the page's title and its actions; a bottom tab bar with Needs you, Floor, Board, Items
// and More; and a drawer that the Goal mark and More open. The drawer shows the sidebar's content
// (sidebar.tsx), so the phone and the desktop list the same entries. ShellFrame (app-shell.tsx) draws
// these and keeps the drawer's state.
import { useQuery } from '@tanstack/react-query';
import { Link, useParams, useRouterState } from '@tanstack/react-router';
import { ChevronLeft, Ellipsis, Inbox, Kanban, LayoutGrid, type LucideIcon, TextAlignStart, X } from 'lucide-react';
import { Dialog as D } from 'radix-ui';
import {
  Children,
  Fragment,
  isValidElement,
  type ReactNode,
  type RefObject,
  useEffect,
  useMemo,
  useRef,
  useSyncExternalStore,
} from 'react';
import { attentionQuery, factoriesQuery } from '../api/queries';
import type { Factory, OrgSummary } from '../api/types';
import { lastFactory } from '../shared/org';
import { GoalMark } from '../ui/goal-mark';
import { Menu, MenuContent, MenuItem, MenuSub, MenuSubContent, MenuSubTrigger, MenuTrigger } from '../ui/menu';
import { NumberRoll } from '../ui/number-roll';
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

/**
 * The Back link a detail page gives the phone top bar (TopBarSlot `back`), to the page its crumb
 * pointed at. Undefined above 768 px, where the top bar's trail is the way back.
 */
export function usePhoneBack(label: string, to: string, params: Record<string, string>): ReactNode | undefined {
  const phone = usePhone();
  const key = JSON.stringify(params);
  return useMemo(
    () =>
      phone ? (
        <Link
          to={to}
          params={JSON.parse(key)}
          aria-label={label}
          className="flex size-11 flex-none items-center justify-center rounded-control text-ink-soft no-underline hover:bg-hover hover:text-ink"
        >
          <ChevronLeft size={20} strokeWidth={1.5} aria-hidden="true" />
        </Link>
      ) : undefined,
    [phone, label, to, key],
  );
}

interface PhoneTopBarProps {
  /** The page's title: its last crumb, or the page's name. */
  title: string | undefined;
  /** The page's actions, from its TopBarSlot. */
  actions: ReactNode | undefined;
  /** The page's Back link, from its TopBarSlot: after the Goal mark, before the title. */
  back?: ReactNode | undefined;
  /** Opens the drawer (the sidebar's content). */
  onMenu: () => void;
  drawerOpen: boolean;
  /** The Goal mark's button: the drawer returns focus to it when it closes. */
  menuRef: RefObject<HTMLButtonElement | null>;
}

/** The top bar: the Goal mark (opens the drawer), the page title and the page's actions. */
export function PhoneTopBar({ title, actions, back, onMenu, drawerOpen, menuRef }: PhoneTopBarProps) {
  return (
    <header className="shell-topbar sticky top-0 z-20 flex min-h-14 flex-none items-center gap-1 border-b border-rule bg-shell pt-[env(safe-area-inset-top)] px-4 md:hidden">
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
      {back ? <div className="flex-none">{back}</div> : null}
      <p className="m-0 min-w-16 flex-1 truncate text-heading text-ink">{title}</p>
      <TopBarActions>{actions}</TopBarActions>
    </header>
  );
}

/** One level of fragments and arrays flattened, so a page's actions count one by one. */
function flattenActions(node: ReactNode): ReactNode[] {
  return Children.toArray(node).flatMap((child) =>
    isValidElement<{ children?: ReactNode }>(child) && child.type === Fragment
      ? flattenActions(child.props.children)
      : [child],
  );
}

/**
 * The page's actions, except those marked data-desktop-only (the page reaches them another way on a phone): the last (the primary one) stays in the bar, the others sit behind More actions, so the
 * title keeps its room. Keycap hints are hidden and every control is 44 px tall.
 */
function TopBarActions({ children }: { children: ReactNode }) {
  const actions = flattenActions(children).filter(
    (a) => !(isValidElement<{ 'data-desktop-only'?: boolean }>(a) && a.props['data-desktop-only']),
  );
  if (actions.length === 0) return null;
  const primary = actions[actions.length - 1];
  const more = actions.slice(0, -1);
  return (
    <div className="flex min-w-0 shrink items-center gap-1 [&_a]:min-h-11 [&_button]:min-h-11 [&_kbd]:hidden">
      {more.length > 0 ? (
        <Menu>
          <MenuTrigger asChild>
            <button
              type="button"
              aria-label="More actions"
              className="flex size-11 flex-none items-center justify-center rounded-control text-ink-soft hover:bg-hover hover:text-ink"
            >
              <Ellipsis size={18} strokeWidth={1.5} aria-hidden="true" />
            </button>
          </MenuTrigger>
          <MenuContent align="end" side="bottom">
            {more.map((action, i) => (
              <MoreAction key={isValidElement(action) && action.key != null ? action.key : i}>{action}</MoreAction>
            ))}
          </MenuContent>
        </Menu>
      ) : null}
      {primary}
    </div>
  );
}

/** What a page's menu action is made of: its trigger's label and its panel's entries. */
function menuParts(action: ReactNode): { label: ReactNode; entries: ReactNode } | undefined {
  if (!isValidElement<{ children?: ReactNode }>(action) || action.type !== Menu) return undefined;
  const parts = Children.toArray(action.props.children);
  const trigger = parts.find((c) => isValidElement(c) && c.type === MenuTrigger);
  const content = parts.find((c) => isValidElement(c) && c.type === MenuContent);
  if (!isValidElement<{ children?: ReactNode }>(trigger) || !isValidElement<{ children?: ReactNode }>(content)) {
    return undefined;
  }
  const button = Children.toArray(trigger.props.children)[0];
  if (!isValidElement<{ children?: ReactNode }>(button)) return undefined;
  return { label: button.props.children, entries: content.props.children };
}

/**
 * One hidden action as an entry of the More actions menu: a menu action of the page becomes a submenu; any other
 * is a menu item that activates the page's own button or link, and closes the menu.
 */
function MoreAction({ children }: { children: ReactNode }) {
  const parts = menuParts(children);
  if (parts) {
    return (
      <MenuSub>
        <MenuSubTrigger>{parts.label}</MenuSubTrigger>
        <MenuSubContent>{parts.entries}</MenuSubContent>
      </MenuSub>
    );
  }
  return (
    <MenuItem
      asChild
      onSelect={(event) => {
        const control = (event.currentTarget as HTMLElement).querySelector<HTMLElement>('a, button');
        control?.click();
      }}
    >
      <div className="[&_*]:pointer-events-none [&_kbd]:hidden">{children}</div>
    </MenuItem>
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
        <D.Overlay className="motion-panel fixed inset-0 z-40 bg-ink/45 md:hidden" />
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
          className="motion-panel motion-drawer-left fixed inset-y-0 left-0 z-50 flex h-dvh w-80 flex-col overflow-y-auto overscroll-contain border-r border-line bg-sidebar px-2 pt-[calc(8px_+_env(safe-area-inset-top))] pb-[calc(12px_+_env(safe-area-inset-bottom))] shadow-pop outline-none md:hidden"
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

// The current tab (aria-current) has ink text and a 2px ink indicator along its top edge. The indicator only
// fades, so reduced motion needs no transform to drop (MOTION.md, Phone tab bar indicator).
const tab =
  'relative flex min-h-16 flex-col items-center justify-center gap-1 text-meta font-medium text-muted no-underline transition-colors duration-quick after:pointer-events-none after:absolute after:inset-x-4 after:top-0 after:h-0.5 after:rounded-chip after:bg-ink after:opacity-0 after:transition-opacity after:duration-fast after:ease-out aria-[current=page]:font-semibold aria-[current=page]:text-ink aria-[current=page]:after:opacity-100';

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

/** Needs you: the organization's page, with the count of what waits on a person in an orange badge. */
function NeedsYouTab({ org }: { org: OrgSummary }) {
  const waiting = useQuery(attentionQuery(org.id)).data?.waiting_on_you ?? 0;
  return (
    <Link
      to="/o/$org/needs-you"
      params={{ org: org.slug }}
      className={tab}
      aria-label={waiting > 0 ? `Needs you, ${waiting} waiting on you` : 'Needs you'}
    >
      <span className="relative">
        <Inbox size={20} strokeWidth={1.5} aria-hidden="true" />
        {waiting > 0 ? (
          <span
            aria-hidden="true"
            className="absolute -top-1 -right-2 flex h-[18px] min-w-[18px] items-center justify-center rounded-full bg-you px-1 text-meta leading-none font-semibold text-white motion-safe:animate-notification-badge motion-reduce:animate-fade-in"
          >
            <NumberRoll value={waiting} />
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
      <Link to="/o/$org" params={{ org: org.slug }} className={tab}>
        {body}
      </Link>
    );
  }
  return (
    <Link to={to} params={{ org: org.slug, factory }} activeOptions={{ exact }} className={tab}>
      {body}
    </Link>
  );
}
