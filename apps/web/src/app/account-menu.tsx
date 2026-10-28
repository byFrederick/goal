// The account menu (canvas "Account-Menu", "Mobile-Org-Switcher"; spec 3 §10.2): the person at the
// bottom of the sidebar (on phones, of the drawer) opens a panel naming the person, their role in
// this organization and whether they are a platform admin. Its rows: the theme, Profile and Personal
// tokens (both on the account page, /account), Notifications, Connected apps and Keyboard shortcuts
// (disabled, "Not available yet"), then Sign out and Sign out everywhere…. With sign-in off (GOAL_AUTH=none)
// it names the local user and has no actions. The account page itself is features/account/account-page.tsx.
//
// AccountProvider (in AppShell) keeps the sign-out confirmation, so it outlives the panel or sheet that
// opened it, and keeps the session alive while signed in.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Bell, CircleCheck, Keyboard, KeyRound, LogOut, Puzzle, UserRound } from 'lucide-react';
import { createContext, type ReactNode, use, useEffect, useState } from 'react';
import { meQuery, orgQuery } from '../api/queries';
import type { Me, OrgSummary } from '../api/types';
import { keepSessionAlive, roleName, useSignOut } from '../shared/account';
import { displayName, initialsOf } from '../shared/names';
import { holds } from '../shared/permissions';
import { Callout } from '../ui/callout';
import { Chip } from '../ui/chip';
import { cn } from '../ui/cn';
import { ConfirmDialog } from '../ui/confirm-dialog';
import { Avatar } from '../ui/mark';
import { menuRow } from '../ui/menu';
import { NotAvailable } from '../ui/not-available';
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover';
import { AppearanceChoice } from './appearance-choice';
import { railItemClass } from './rail-items';

/** Better Auth extends a session its endpoints see (spec 3 §8.3): on load, then hourly. */
const keepAliveMs = 60 * 60_000;

/** The account's state: loading the person, unable to load them, the local user (sign-in off), or ready. */
type AccountStatus = 'loading' | 'error' | 'local' | 'ready';

interface Account {
  /** The signed-in person; undefined with sign-in off (the local user) or while loading. */
  person: NonNullable<Me['user']> | undefined;
  status: AccountStatus;
  platformAdmin: boolean;
  signOut: () => void;
  signOutEverywhere: () => void;
}

const AccountContext = createContext<Account | null>(null);

/** The account's state and actions, for the account menu and the account page inside AppShell. */
export function useAccount(): Account {
  const account = use(AccountContext);
  if (!account) throw new Error('useAccount outside AccountProvider');
  return account;
}

export function AccountProvider({ children }: { children: ReactNode }) {
  const { data: me, isPending, isError } = useQuery(meQuery);
  const person = me?.mode === 'session' ? me.user : undefined;
  const status: AccountStatus = isPending ? 'loading' : isError ? 'error' : me?.mode === 'session' ? 'ready' : 'local';
  const [everywhereOpen, setEverywhereOpen] = useState(false);
  const { signOut, everywhere } = useSignOut();
  const signedIn = me?.mode === 'session';

  useEffect(() => {
    if (!signedIn) return;
    void keepSessionAlive();
    const timer = setInterval(() => void keepSessionAlive(), keepAliveMs);
    return () => clearInterval(timer);
  }, [signedIn]);

  const account: Account = {
    person,
    status,
    platformAdmin: me?.platform_admin ?? false,
    signOut: () => {
      if (!signOut.isPending) signOut.mutate();
    },
    signOutEverywhere: () => setEverywhereOpen(true),
  };

  return (
    <AccountContext value={account}>
      {children}
      <ConfirmDialog
        open={everywhereOpen}
        onOpenChange={(open) => {
          if (!open) everywhere.reset();
          setEverywhereOpen(open);
        }}
        title="Sign out of every browser and the CLI?"
        description="Every session ends, this one too. API keys and personal tokens keep working until they are revoked."
        confirmLabel="Sign out everywhere"
        pendingLabel="Signing out…"
        tone="danger"
        pending={everywhere.isPending}
        onConfirm={() => everywhere.mutate()}
      >
        {everywhere.isError ? (
          <div aria-live="polite">
            <Callout tone="rework">Can’t reach Goal. Check your connection and try again.</Callout>
          </div>
        ) : null}
      </ConfirmDialog>
    </AccountContext>
  );
}

/**
 * The account's trigger and panel: the person's name and role at the bottom of the sidebar, or of the
 * phone's drawer (the panel opens above it). `rail` is the avatar alone in the collapsed rail; the panel
 * opens to its right.
 */
export function AccountMenu({ org, size = 'sidebar' }: { org: OrgSummary; size?: 'sidebar' | 'rail' }) {
  const { person } = useAccount();
  const [open, setOpen] = useState(false);
  const name = person ? displayName(person) : 'Local user';
  const initials = person ? initialsOf(person) : 'LU';
  const label = `${initials}, ${name}, account`;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      {size === 'rail' ? (
        <PopoverTrigger aria-label={label} className={railItemClass}>
          <Avatar initials={initials} size="sidebar" />
        </PopoverTrigger>
      ) : (
        <PopoverTrigger
          aria-label={label}
          className="flex w-full items-center gap-3 rounded-cell p-2 text-left text-ink hover:bg-hover aria-expanded:bg-hover"
        >
          <Avatar initials={initials} size="sidebar" />
          <span className="flex min-w-0 flex-col">
            <span className="truncate text-body font-semibold">{name}</span>
            {roleName(org.role) ? <span className="truncate text-meta text-muted">{roleName(org.role)}</span> : null}
          </span>
        </PopoverTrigger>
      )}
      <PopoverContent
        side={size === 'rail' ? 'right' : 'top'}
        align={size === 'rail' ? 'end' : 'start'}
        aria-label="Account"
      >
        <AccountBody org={org} onAction={() => setOpen(false)} />
      </PopoverContent>
    </Popover>
  );
}

/** The anchor of the account page's Personal tokens section (features/account/personal-tokens.tsx). */
const tokensAnchor = 'personal-tokens';

/** Whether the person may see their personal tokens in `org` (personal_token:read). */
function useTokensLink(org: OrgSummary): boolean {
  const { data } = useQuery(orgQuery(org.id));
  return holds(data, 'personal_token:read');
}

/** The role's pill: owners in the ok tone, admins in the you tone, members plain. */
function rolePillTone(role: OrgSummary['role']): 'ok' | 'you' | 'plain' {
  if (role === 'owner') return 'ok';
  if (role === 'admin') return 'you';
  return 'plain';
}

const iconProps = { size: 16, strokeWidth: 1.5, 'aria-hidden': true, className: 'shrink-0 text-muted' } as const;
const actionRow = cn(
  menuRow,
  'cursor-pointer border-0 bg-transparent text-body hover:bg-hover data-[highlighted]:bg-hover',
);

/**
 * The account's content, in the popover and the phone's sheet alike: the person and their role, the
 * theme, the account's pages and the two sign-out actions. Loading, error and sign-in off each have
 * their own state; the error state keeps the theme and the sign-out actions usable.
 */
function AccountBody({ org, onAction }: { org: OrgSummary; onAction: () => void }) {
  const account = useAccount();
  const { person, status } = account;

  if (status === 'loading') {
    return (
      <div aria-busy="true" className="p-2">
        <span className="sr-only">Loading</span>
      </div>
    );
  }
  if (status === 'local') return <LocalUser />;
  return (
    <>
      {status === 'error' ? (
        <div className="p-2">
          <Callout tone="rework">Can’t reach Goal. Check your connection and try again.</Callout>
        </div>
      ) : (
        <AccountHeader person={person} org={org} platformAdmin={account.platformAdmin} />
      )}
      <AccountRows org={org} onAction={onAction} />
    </>
  );
}

/** The person's avatar, name and role pill (and the platform admin pill). */
function AccountHeader({
  person,
  org,
  platformAdmin,
}: {
  person: Account['person'];
  org: OrgSummary;
  platformAdmin: boolean;
}) {
  if (!person) return null;
  const role = roleName(org.role);
  return (
    <div className="flex items-center gap-3 p-2">
      <Avatar initials={initialsOf(person)} size="card" />
      <span className="flex min-w-0 flex-wrap items-center gap-2">
        <span className="min-w-0 truncate text-heading font-medium">{displayName(person)}</span>
        {role ? <Chip tone={rolePillTone(org.role)}>{role}</Chip> : null}
        {platformAdmin ? <Chip tone="you">Platform admin</Chip> : null}
      </span>
    </div>
  );
}

/**
 * The theme, then the rows: Profile and Personal tokens open their sections of the account page, and the
 * three pages not built yet are disabled. Sign out and Sign out everywhere end it.
 */
function AccountRows({ org, onAction }: { org: OrgSummary; onAction: () => void }) {
  const account = useAccount();
  const tokens = useTokensLink(org);
  const act = (action: () => void) => () => {
    onAction();
    action();
  };
  return (
    <>
      <AppearanceChoice />
      <ul className="m-0 flex list-none flex-col p-0">
        {account.person ? (
          <li>
            <Link to="/account" onClick={onAction} className={actionRow}>
              <UserRound {...iconProps} />
              Profile
            </Link>
          </li>
        ) : null}
        <li>
          <NotAvailable>
            <button type="button" className={actionRow}>
              <Bell {...iconProps} />
              Notifications
            </button>
          </NotAvailable>
        </li>
        {tokens ? (
          <li>
            <Link to="/account" hash={tokensAnchor} onClick={onAction} className={actionRow}>
              <KeyRound {...iconProps} />
              Personal tokens
            </Link>
          </li>
        ) : null}
        <li>
          <NotAvailable>
            <button type="button" className={actionRow}>
              <Puzzle {...iconProps} />
              Connected apps
            </button>
          </NotAvailable>
        </li>
        <li>
          <NotAvailable>
            <button type="button" className={actionRow}>
              <Keyboard {...iconProps} />
              Keyboard shortcuts
              <kbd className="ml-auto inline-grid h-5 min-w-5 place-items-center rounded-chip border border-rule px-1 font-sans text-meta font-semibold text-ink-soft">
                ?
              </kbd>
            </button>
          </NotAvailable>
        </li>
      </ul>
      <div aria-hidden="true" className="my-1 h-px bg-line" />
      <ul className="m-0 flex list-none flex-col p-0">
        <li>
          <button type="button" onClick={act(account.signOut)} className={actionRow}>
            <LogOut {...iconProps} />
            Sign out
          </button>
        </li>
        <li>
          <button
            type="button"
            aria-haspopup="dialog"
            onClick={act(account.signOutEverywhere)}
            className={cn(actionRow, 'text-rework-ink')}
          >
            <LogOut {...iconProps} className="shrink-0 text-rework-ink" />
            Sign out everywhere
          </button>
        </li>
      </ul>
    </>
  );
}

/** Sign-in is off: the local user of this install, and nothing to sign out of. */
function LocalUser() {
  return (
    <div className="flex flex-col gap-2 p-2">
      <div className="flex items-center gap-3">
        <span
          aria-hidden="true"
          className="flex size-10 shrink-0 items-center justify-center rounded-full bg-panel text-ink"
        >
          <CircleCheck size={18} strokeWidth={1.5} />
        </span>
        <span className="min-w-0">
          <span className="block text-heading font-medium">Local user</span>
          <span className="block text-secondary text-muted">This install</span>
        </span>
      </div>
      <Callout tone="note">Sign-in is off. Anyone who can reach this address acts as the local user.</Callout>
      <AppearanceChoice />
    </div>
  );
}

/**
 * The account at the bottom of the phone's organization sheet: the same person, theme and actions as
 * the popover. `onAction` closes the sheet first.
 */
export function AccountBlock({ org, onAction }: { org: OrgSummary; onAction: () => void }) {
  return (
    <section aria-label="Account" className="flex flex-col pt-1">
      <AccountBody org={org} onAction={onAction} />
    </section>
  );
}
