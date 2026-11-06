// Your account (docs/design/screens/account/account.jpg, ADR 0016): the person's profile, the theme,
// notifications, personal tokens, connected apps and sessions, under a left section nav. Live here:
// the name and the email for notifications (Save changes), the theme, the notification table
// (notifications-section.tsx), the personal tokens and Sign out everywhere. Disabled with
// "Not available yet", because goald has no endpoint for them yet: connected apps and the session list
// with per-session sign-out (docs/design/screens/README.md, "Not built yet"). With sign-in off it names
// the local user, has no Save or sign-out actions, and has no notification preferences to change.
import { useQuery } from '@tanstack/react-query';
import { createLink, useLocation } from '@tanstack/react-router';
import { SquareTerminal } from 'lucide-react';
import { type ComponentProps, type FormEvent, useLayoutEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { meQuery, notificationsQuery, orgNotificationsQuery } from '../../api/queries';
import type { Me, NotificationPreferences, OrgSummary } from '../../api/types';
import { useAccount } from '../../app/account-menu';
import { AppearanceChoice } from '../../app/appearance-choice';
import { TopBarSlot } from '../../app/top-bar';
import { roleLine, useUpdateName } from '../../shared/account';
import { displayName, initialsOf } from '../../shared/names';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { Field, Input } from '../../ui/field';
import { Avatar } from '../../ui/mark';
import { NotAvailable } from '../../ui/not-available';
import { sectionHeadingClass } from '../../ui/page-header';
import { useSaveNotifications } from './notification-preferences';
import { NotificationsTable } from './notifications-section';
import { PersonalTokensSection } from './personal-tokens';

/** The sections of the left nav, in the picture's order. The ids are the anchors the nav links to. */
const sections = [
  { id: 'profile', label: 'Profile' },
  { id: 'appearance', label: 'Appearance' },
  { id: 'notifications', label: 'Notifications' },
  { id: 'personal-tokens', label: 'Personal tokens' },
  { id: 'connected-apps', label: 'Connected apps' },
  { id: 'sessions', label: 'Sessions' },
] as const;

/** The form the top bar's Save changes submits. */
const profileForm = 'account-profile';

export function AccountPage() {
  const org = useOrg();
  const { data: me, isPending, isError, error } = useQuery(meQuery);

  if (isPending) {
    return (
      <div aria-busy="true" className="flex flex-1 flex-col">
        <span role="status" className="sr-only">
          Loading
        </span>
      </div>
    );
  }
  // A failed load goes to the route's error screen (ErrorScreen, with its retry), as on the other routes.
  if (isError) throw error;
  return <AccountContent me={me} org={org} />;
}

function AccountContent({ me, org }: { me: Me; org: OrgSummary }) {
  const person = me.mode === 'session' ? me.user : undefined;
  const hash = useLocation({ select: (l) => l.hash.replace(/^#/, '') });
  const active = sections.some((s) => s.id === hash) ? hash : 'profile';
  const update = useUpdateName();
  const preferences = useQuery({ ...notificationsQuery, enabled: person !== undefined });
  const orgPreferences = useQuery({ ...orgNotificationsQuery(org.id), enabled: person !== undefined });
  const save = useSaveNotifications();

  return (
    <>
      <TopBarSlot actions={person ? <SaveChanges pending={update.isPending || save.isPending} /> : undefined} />
      <h1 className="m-0 text-page-title tracking-display">Your account</h1>
      <div className="grid gap-8 lg:grid-cols-[200px_minmax(0,1fr)]">
        <SectionNav active={active} />

        <div className="flex min-w-0 flex-col gap-4">
          <ProfileSection person={person} org={org} update={update} saved={preferences.data} save={save} />
          <AppearanceSection />
          <section id="notifications" aria-labelledby="notifications-heading" className={card}>
            <h2 id="notifications-heading" className={sectionHeadingClass}>
              Notifications
            </h2>
            <NotificationsTable org={org.id} signedIn={person !== undefined} />
          </section>
          <PersonalTokensSection
            org={org}
            settled={person === undefined || !(preferences.isPending || orgPreferences.isPending)}
          />
          <ConnectedAppsSection />
          <SessionsSection signedIn={person !== undefined} />
        </div>
      </div>
    </>
  );
}

/**
 * A link to a section of this page. The router marks every link to /account as the current page
 * (aria-current="page") whatever the hash, so this link drops that and says "true" for the one section
 * that is current: a place on the page, not the page.
 */
const SectionLink = createLink(function SectionAnchor({
  current,
  'aria-current': _page,
  ...props
}: ComponentProps<'a'> & { current: boolean }) {
  return <a {...props} aria-current={current ? 'true' : undefined} />;
});

/**
 * The section nav: a horizontal row above the content below 1024 px, the left column from there up.
 * The current section sits on one `hover` pill that slides to the new one
 * (MOTION.md, Tabs). It is measured from the links, so it is placed without a slide on first paint
 * and on resize; reduced motion removes the slide (styles.css).
 */
function SectionNav({ active }: { active: string }) {
  const nav = useRef<HTMLElement>(null);
  const pill = useRef<HTMLSpanElement>(null);
  const placed = useRef(false);

  useLayoutEffect(() => {
    const place = (animate: boolean) => {
      const el = pill.current;
      if (!el) return;
      const current = nav.current?.querySelector<HTMLElement>('[aria-current="true"]');
      if (!current) {
        el.style.opacity = '0';
        return;
      }
      if (!animate) el.style.transition = 'none';
      el.style.opacity = '1';
      el.style.height = `${current.offsetHeight}px`;
      if (getComputedStyle(nav.current as HTMLElement).flexDirection === 'row') {
        // Transform only: a 100 px pill stretched to the link's width.
        el.style.width = '100px';
        el.style.transform = `translate(${current.offsetLeft}px, ${current.offsetTop}px) scaleX(${current.offsetWidth / 100})`;
      } else {
        el.style.width = '100%';
        el.style.transform = `translate(0px, ${current.offsetTop}px)`;
      }
      if (!animate) {
        void el.offsetHeight;
        el.style.transition = '';
      }
    };
    place(placed.current);
    placed.current = true;
    const onResize = () => place(false);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  });

  return (
    <nav
      ref={nav}
      aria-label="Account sections"
      className="relative flex min-w-0 flex-row gap-1 self-start overflow-x-auto lg:flex-col lg:overflow-visible"
    >
      <span
        ref={pill}
        aria-hidden="true"
        className="pointer-events-none absolute left-0 top-0 origin-left rounded-control bg-hover opacity-0 transition-[transform,height] duration-(--duration-fast) ease-out motion-reduce:transition-none"
      />
      {sections.map((s) => (
        <SectionLink
          key={s.id}
          to="/account"
          hash={s.id}
          current={active === s.id}
          className="press relative flex h-11 shrink-0 items-center whitespace-nowrap lg:h-10 rounded-control px-3 text-body text-ink-soft no-underline hover:text-ink aria-[current=true]:font-semibold aria-[current=true]:text-ink"
        >
          {s.label}
        </SectionLink>
      ))}
    </nav>
  );
}

const card = 'flex min-w-0 flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5';

/** The name and the role. With sign-in off there is no name to change: the local user is shown. */
function ProfileSection({
  person,
  org,
  update,
  saved,
  save,
}: {
  person: Me['user'] | undefined;
  org: OrgSummary;
  update: ReturnType<typeof useUpdateName>;
  /** The saved notification preferences; undefined until they load, and without a person. */
  saved: NotificationPreferences | undefined;
  save: ReturnType<typeof useSaveNotifications>;
}) {
  const role = roleLine(org.role, org.name);
  return (
    <section id="profile" aria-labelledby="profile-heading" className={card}>
      <h2 id="profile-heading" className={sectionHeadingClass}>
        Profile
      </h2>
      <div className="flex flex-wrap items-center gap-4">
        <Avatar initials={person ? initialsOf(person) : 'LU'} size="card" />
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-heading font-semibold" title={person ? displayName(person) : 'Local user'}>
            {person ? displayName(person) : 'Local user'}
          </span>
          <span className="truncate text-secondary text-muted" title={person ? person.email : 'This install'}>
            {person ? person.email : 'This install'}
          </span>
        </div>
        {role ? <Chip tone={org.role === 'owner' ? 'ok' : org.role === 'admin' ? 'you' : 'plain'}>{role}</Chip> : null}
      </div>
      {person ? (
        <NameForm name={person.name} signInEmail={person.email} saved={saved} update={update} save={save} />
      ) : null}
    </section>
  );
}

/** Save changes is in the top bar; it submits this form by its id. */
function SaveChanges({ pending }: { pending: boolean }) {
  return (
    <Button type="submit" form={profileForm} variant="action" busy={pending} busyLabel="Saving…">
      Save changes
    </Button>
  );
}

/** Why a name was not saved: the identity service was not reached, or it refused the name. */
function saveFailure(err: unknown): string {
  return (err as { kind?: string }).kind === 'unreachable'
    ? 'Goal couldn’t save it. Check your connection and try again.'
    : 'Goal couldn’t save that name. A name is 1 to 80 characters.';
}

/** Why the notification email was not saved: goald refused the address, or it was not reached. */
function emailFailure(err: unknown): string {
  return err instanceof ApiError && err.code === 'invalid-notification-email'
    ? 'Enter one email address, or leave it blank to use your sign-in email.'
    : 'Goal couldn’t save it. Check your connection and try again.';
}

/**
 * The name and the email for notifications. Save changes saves the name, and the email when it changed.
 * The email is disabled until the saved preferences load.
 */
function NameForm({
  name,
  signInEmail,
  saved,
  update,
  save,
}: {
  name: string;
  signInEmail: string;
  saved: NotificationPreferences | undefined;
  update: ReturnType<typeof useUpdateName>;
  save: ReturnType<typeof useSaveNotifications>;
}) {
  const [value, setValue] = useState(name);
  const [invalid, setInvalid] = useState<string>();
  // The email field shows the saved address once it loads, until the person types: the form is not
  // remounted by the load, so what they typed in the name field is kept.
  const [typedEmail, setEmail] = useState<string>();
  const email = typedEmail ?? saved?.email ?? '';
  const [emailInvalid, setEmailInvalid] = useState<string>();

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const next = value.trim();
    if (next.length < 1 || next.length > 80) {
      setInvalid('A name is 1 to 80 characters.');
      return;
    }
    setInvalid(undefined);
    update.mutate(next, {
      onSuccess: () => toast.success('Name saved'),
    });
    const address = email.trim();
    if (saved && address !== saved.email) {
      setEmailInvalid(undefined);
      save.mutate((current) => ({ ...current, email: address }), {
        onSuccess: () => toast.success('Email for notifications saved'),
        onError: (err) => setEmailInvalid(emailFailure(err)),
      });
    }
  };

  return (
    <form id={profileForm} onSubmit={onSubmit} noValidate className="grid gap-4 md:grid-cols-2">
      <Field label="Name" error={invalid ?? (update.isError ? saveFailure(update.error) : undefined)}>
        <Input value={value} maxLength={80} autoComplete="name" onChange={(e) => setValue(e.target.value)} />
      </Field>
      <Field label="Email for notifications" error={emailInvalid}>
        <Input
          type="email"
          value={email}
          disabled={!saved}
          placeholder={signInEmail}
          autoComplete="email"
          onChange={(e) => setEmail(e.target.value)}
        />
      </Field>
    </form>
  );
}

function AppearanceSection() {
  return (
    <section id="appearance" aria-labelledby="appearance-heading" className={card}>
      <h2 id="appearance-heading" className={sectionHeadingClass}>
        Appearance
      </h2>
      <AppearanceChoice layout="cards" />
    </section>
  );
}

/**
 * The one connected app goald can name: the goal CLI, signed in on a device. The picture's other sample
 * row names a product of another company, so it is not drawn; the identity service has no list of apps.
 */
function ConnectedAppsSection() {
  return (
    <section id="connected-apps" aria-labelledby="connected-apps-heading" className={card}>
      <h2 id="connected-apps-heading" className={sectionHeadingClass}>
        Connected apps
      </h2>
      <div className="flex items-center justify-between gap-3 border-t border-line pt-4">
        <span className="flex min-w-0 items-center gap-3">
          <SquareTerminal size={16} strokeWidth={1.75} aria-hidden="true" className="shrink-0 text-muted" />
          <span className="flex min-w-0 flex-col">
            <span className="text-body font-semibold">goal CLI</span>
            <span className="text-meta text-muted">Signed in on this device. Full access as you</span>
          </span>
        </span>
        <NotAvailable label="Revoke goal CLI">
          <Button variant="white" size="sm">
            Revoke
          </Button>
        </NotAvailable>
      </div>
    </section>
  );
}

/**
 * Sessions. Sign out everywhere works (the identity service's revoke-sessions). goald's /api/auth proxy
 * does not forward list-sessions or revoke-session, so the list shows only this browser and no other
 * session can be signed out one by one.
 */
function SessionsSection({ signedIn }: { signedIn: boolean }) {
  const account = useAccount();
  return (
    <section id="sessions" aria-labelledby="sessions-heading" className={card}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 id="sessions-heading" className={sectionHeadingClass}>
          Sessions
        </h2>
        {signedIn ? (
          <Button variant="white" size="sm" className="text-rework-ink" onClick={account.signOutEverywhere}>
            Sign out everywhere
          </Button>
        ) : null}
      </div>
      <div className="flex items-center gap-2 border-t border-line pt-4">
        <span className="text-body font-semibold">This browser</span>
        <Chip tone="ok">This device</Chip>
        <span className="ml-auto text-meta text-muted">Active now</span>
      </div>
      {/* The other sessions are not listed: goald cannot list them yet, so the row only shows that it is not available. */}
      <div className="flex items-center gap-2 border-t border-line pt-4">
        <span className="flex min-w-0 flex-col">
          <span className="text-body font-semibold">Another browser or device</span>
          <span className="text-meta text-muted">Not listed yet</span>
        </span>
        <span className="ml-auto">
          <NotAvailable label="Sign out of another browser or device">
            <Button variant="white" size="sm">
              Sign out
            </Button>
          </NotAvailable>
        </span>
      </div>
    </section>
  );
}
