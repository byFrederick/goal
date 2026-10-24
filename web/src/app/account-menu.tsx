// The account (canvas "Account-Menu", "Mobile-Org-Switcher"; spec 3 §10.2): the avatar at the
// bottom of the rail (and in the phone's top row) opens a panel naming the person, their role in
// this organization and whether they are a platform admin, with Edit name…, Personal tokens (to
// this organization's API keys page, 3-12b), Sign out and Sign out everywhere…; on phones the same
// block sits at the bottom of the organization sheet. With sign-in off (GOAL_AUTH=none) it names
// the local user and has no actions.
//
// AccountProvider (in AppShell) keeps the dialogs those actions open, so they outlive the panel or
// sheet that opened them, and keeps the session alive while signed in.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CircleCheck, KeyRound, LogOut, Pencil } from 'lucide-react';
import { createContext, type FormEvent, type ReactNode, use, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { meQuery, orgQuery } from '../api/queries';
import type { Me, OrgSummary } from '../api/types';
import { initialsOf, keepSessionAlive, roleLine, useSignOut, useUpdateName } from '../shared/account';
import { holds } from '../shared/permissions';
import { Button } from '../ui/button';
import { Callout } from '../ui/callout';
import { Chip } from '../ui/chip';
import { cn } from '../ui/cn';
import { ConfirmDialog } from '../ui/confirm-dialog';
import { Dialog, DialogClose, DialogContent } from '../ui/dialog';
import { Field, Input } from '../ui/field';
import { Avatar } from '../ui/mark';
import { menuRow } from '../ui/menu';
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover';
import { Tooltip } from '../ui/tooltip';
import { railItem, railItemClass } from './rail-items';

/** Better Auth extends a session its endpoints see (spec 3 §8.3): on load, then hourly. */
const keepAliveMs = 60 * 60_000;

interface Account {
  /** The signed-in person; undefined with sign-in off (the local user) or while loading. */
  person: NonNullable<Me['user']> | undefined;
  platformAdmin: boolean;
  editName: () => void;
  signOut: () => void;
  signOutEverywhere: () => void;
}

const AccountContext = createContext<Account | null>(null);

function useAccount(): Account {
  const account = use(AccountContext);
  if (!account) throw new Error('useAccount outside AccountProvider');
  return account;
}

export function AccountProvider({ org, children }: { org: OrgSummary; children: ReactNode }) {
  const { data: me } = useQuery(meQuery);
  const person = me?.mode === 'session' ? me.user : undefined;
  const [dialog, setDialog] = useState<'name' | 'everywhere' | null>(null);
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
    platformAdmin: me?.platform_admin ?? false,
    editName: () => setDialog('name'),
    signOut: () => {
      if (!signOut.isPending) signOut.mutate();
    },
    signOutEverywhere: () => setDialog('everywhere'),
  };

  return (
    <AccountContext value={account}>
      {children}
      {person ? (
        <EditNameDialog
          open={dialog === 'name'}
          onOpenChange={(open) => setDialog(open ? 'name' : null)}
          name={person.name}
          orgName={org.name}
        />
      ) : null}
      <ConfirmDialog
        open={dialog === 'everywhere'}
        onOpenChange={(open) => {
          if (!open) everywhere.reset();
          setDialog(open ? 'everywhere' : null);
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

/** The account's trigger and panel: on the rail (beside it) or in the phone's top row (below). */
export function AccountMenu({ org, size }: { org: OrgSummary; size: 'rail' | 'phone' }) {
  const { person } = useAccount();
  const [open, setOpen] = useState(false);
  const name = person?.name ?? 'Local user';
  const initials = person ? initialsOf(person.name) : 'LU';

  const trigger = (
    <PopoverTrigger aria-label={`${initials}, ${name}, account`} className={railItemClass} {...railItem}>
      <Avatar initials={initials} size={size} />
    </PopoverTrigger>
  );
  return (
    <Popover open={open} onOpenChange={setOpen}>
      {size === 'rail' ? (
        <Tooltip label={name} detail={person?.email ?? 'This install'}>
          {trigger}
        </Tooltip>
      ) : (
        trigger
      )}
      <PopoverContent side={size === 'rail' ? 'right' : 'bottom'} aria-label="Account">
        <AccountPanel org={org} onAction={() => setOpen(false)} />
      </PopoverContent>
    </Popover>
  );
}

/** The anchor of the API keys page's Personal tokens section (personal-tokens.tsx). */
const tokensAnchor = 'personal-tokens';

/** Whether the person may see their personal tokens in `org` (personal_token:read). */
function useTokensLink(org: OrgSummary): boolean {
  const { data } = useQuery(orgQuery(org.id));
  return holds(data, 'personal_token:read');
}

function AccountPanel({ org, onAction }: { org: OrgSummary; onAction: () => void }) {
  const account = useAccount();
  const { person } = account;
  const tokens = useTokensLink(org);
  if (!person) return <LocalUser />;
  const role = roleLine(org.role, org.name);
  const act = (action: () => void) => () => {
    onAction();
    action();
  };
  return (
    <>
      <div className="grid grid-cols-[40px_minmax(0,1fr)] items-center gap-3 p-2">
        <Avatar initials={initialsOf(person.name)} size="card" />
        <span className="min-w-0">
          <span className="block truncate text-[15px] font-medium">{person.name}</span>
          <span className="block truncate text-[13px] text-muted">{person.email}</span>
        </span>
      </div>
      {role || account.platformAdmin ? (
        <p className="m-0 flex flex-wrap items-center gap-2 px-2 pb-2 pl-[60px] text-[13px] text-ink-soft">
          {role}
          {account.platformAdmin ? <Chip tone="you">Platform admin</Chip> : null}
        </p>
      ) : null}
      <div aria-hidden="true" className="my-1 h-px bg-line" />
      <ul className="m-0 flex list-none flex-col p-0">
        <li>
          <button type="button" aria-haspopup="dialog" onClick={act(account.editName)} className={actionRow}>
            <Pencil {...iconProps} />
            Edit name…
          </button>
        </li>
        {tokens ? (
          <li>
            <Link
              to="/o/$org/api-keys"
              params={{ org: org.slug }}
              hash={tokensAnchor}
              onClick={onAction}
              className={actionRow}
            >
              <KeyRound {...iconProps} />
              Personal tokens
            </Link>
          </li>
        ) : null}
        <li>
          <button type="button" onClick={act(account.signOut)} className={actionRow}>
            <LogOut {...iconProps} />
            Sign out
          </button>
        </li>
        <li>
          <button type="button" aria-haspopup="dialog" onClick={act(account.signOutEverywhere)} className={actionRow}>
            <LogOut {...iconProps} />
            Sign out everywhere…
          </button>
        </li>
      </ul>
    </>
  );
}

const iconProps = { size: 18, strokeWidth: 1.5, 'aria-hidden': true, className: 'text-muted' } as const;
const actionRow = cn(menuRow, 'cursor-pointer border-0 bg-transparent hover:bg-shell focus-visible:-outline-offset-2');

/** Sign-in is off: the local user of this install, and nothing to sign out of. */
function LocalUser() {
  return (
    <div className="flex flex-col gap-2 p-2">
      <div className="grid grid-cols-[40px_minmax(0,1fr)] items-center gap-3">
        <span aria-hidden="true" className="flex size-10 items-center justify-center rounded-full bg-control text-ink">
          <CircleCheck size={18} strokeWidth={1.5} />
        </span>
        <span className="min-w-0">
          <span className="block text-[15px] font-medium">Local user</span>
          <span className="block text-[13px] text-muted">This install</span>
        </span>
      </div>
      <Callout tone="note">Sign-in is off. Anyone who can reach this address acts as the local user.</Callout>
    </div>
  );
}

/**
 * The account at the bottom of the phone's organization sheet: the same person and actions as
 * the panel, as buttons. `onAction` closes the sheet first.
 */
export function AccountBlock({ org, onAction }: { org: OrgSummary; onAction: () => void }) {
  const account = useAccount();
  const { person } = account;
  const tokens = useTokensLink(org);
  if (!person) return <LocalUser />;
  const role = roleLine(org.role, org.name);
  const act = (action: () => void) => () => {
    onAction();
    action();
  };
  return (
    <section aria-label="Account" className="flex flex-col gap-3 px-1 pt-1">
      <div className="grid grid-cols-[40px_minmax(0,1fr)] items-center gap-3">
        <Avatar initials={initialsOf(person.name)} size="card" />
        <span className="min-w-0">
          <span className="flex flex-wrap items-center gap-2 text-[15px] font-medium">
            {person.name}
            {account.platformAdmin ? <Chip tone="you">Platform admin</Chip> : null}
          </span>
          <span className="block text-[13px] text-muted">
            {person.email}
            {role ? `, ${role.charAt(0).toLowerCase()}${role.slice(1)}` : ''}
          </span>
        </span>
      </div>
      <div className="grid grid-cols-2 gap-2">
        <Button
          variant="white"
          size="lg"
          aria-haspopup="dialog"
          onClick={act(account.editName)}
          className="justify-center"
        >
          Edit name…
        </Button>
        {tokens ? (
          <Button asChild variant="white" size="lg" className="col-span-2 justify-center">
            <Link to="/o/$org/api-keys" params={{ org: org.slug }} hash={tokensAnchor} onClick={onAction}>
              Personal tokens
            </Link>
          </Button>
        ) : null}
        <Button variant="white" size="lg" onClick={act(account.signOut)} className="justify-center">
          Sign out
        </Button>
        <Button
          variant="white"
          size="lg"
          aria-haspopup="dialog"
          onClick={act(account.signOutEverywhere)}
          className="col-span-2 justify-center"
        >
          Sign out everywhere…
        </Button>
      </div>
    </section>
  );
}

/** Why a name was not saved: the identity service was not reached, or it refused the name. */
function saveFailure(err: unknown): string {
  return (err as { kind?: string }).kind === 'unreachable'
    ? 'Goal couldn’t save it. Check your connection and try again.'
    : 'Goal couldn’t save that name. A name is 1 to 80 characters.';
}

function EditNameDialog({
  open,
  onOpenChange,
  name,
  orgName,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  name: string;
  orgName: string;
}) {
  const [value, setValue] = useState(name);
  const [invalid, setInvalid] = useState<string>();
  const field = useRef<HTMLInputElement>(null);
  const update = useUpdateName();

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const next = value.trim();
    if (next.length < 1 || next.length > 80) {
      setInvalid('A name is 1 to 80 characters.');
      return;
    }
    update.mutate(next, {
      onSuccess: () => {
        onOpenChange(false);
        toast.success('Name saved');
      },
    });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) {
          setValue(name);
          setInvalid(undefined);
          update.reset();
        }
        onOpenChange(next);
      }}
    >
      <DialogContent title="Edit name" initialFocus={field}>
        <form onSubmit={onSubmit} noValidate className="flex flex-col gap-4">
          <Field
            label="Name"
            help={`1 to 80 characters. ${orgName}’s members and the audit log show it.`}
            error={invalid ?? (update.isError ? saveFailure(update.error) : undefined)}
          >
            <Input
              ref={field}
              value={value}
              maxLength={80}
              autoComplete="name"
              onChange={(e) => setValue(e.target.value)}
            />
          </Field>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="white" size="lg" className="md:h-10 md:text-sm">
                Cancel
              </Button>
            </DialogClose>
            <Button
              type="submit"
              variant="primary"
              size="lg"
              disabled={update.isPending}
              className="md:h-10 md:text-sm"
            >
              {update.isPending ? 'Saving…' : 'Save'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
