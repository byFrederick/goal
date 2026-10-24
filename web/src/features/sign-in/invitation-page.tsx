// Invite-Accept, signed in (spec 3 §10.2; canvas "Invite-Accept", "Mobile-Invite-Accept"): the
// invitation the link names, as the identity service shows it to its recipient only (who sent it,
// the role and factories joining gives, until when), with Join and Decline. Joining opens the
// organization. An invitation for another address, one that cannot be used any more (expired,
// cancelled, used, unknown: one message for all) and one for an organization the person is
// already in each have their own card. The link carries only the invitation's id.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { Info, LogOut } from 'lucide-react';
import { toast } from 'sonner';
import type { AuthFailure, InvitationDetails } from '../../api/auth';
import { keys, meQuery, orgsQuery } from '../../api/queries';
import { useSignOut } from '../../shared/account';
import { formatDay } from '../../shared/format';
import { orgMark } from '../../shared/marks';
import { Button } from '../../ui/button';
import { Spinner } from '../../ui/spinner';
import { AuthHeading, AuthPage } from './auth-page';

const identityService = () => import('../../api/auth');

const article = { owner: 'an owner', admin: 'an admin', member: 'a member' } as const;

/** Why the invitation cannot be shown or used: someone else's, or gone. */
const failureOf = (err: unknown) =>
  (err as Partial<AuthFailure> | null)?.kind === 'not-recipient' ? 'wrong-account' : 'unusable';

const primary =
  'inline-flex h-12 w-full items-center justify-center gap-2 rounded-control bg-ink px-4 text-[15px] font-medium text-white no-underline transition-colors duration-200 hover:bg-ink/90';

export function InvitationPage({ id }: { id: string }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const me = useQuery(meQuery);
  const orgs = useQuery(orgsQuery);
  const invitation = useQuery({
    queryKey: ['auth', 'invitation', id],
    queryFn: async () => (await identityService()).getInvitation(id),
    retry: false,
    staleTime: Number.POSITIVE_INFINITY,
  });

  const join = useMutation({
    mutationFn: async (i: InvitationDetails) => (await identityService()).acceptInvitation(i.id),
    onSuccess: async (_, i) => {
      await client.refetchQueries({ queryKey: keys.orgs });
      toast.success(`You joined ${i.organizationName}`);
      await navigate({ to: '/o/$org', params: { org: i.organizationSlug }, replace: true });
    },
  });
  const decline = useMutation({
    mutationFn: async (i: InvitationDetails) => (await identityService()).rejectInvitation(i.id),
  });

  if (invitation.isPending || orgs.isPending) {
    return (
      <AuthPage>
        <p role="status" className="m-0 flex items-center gap-2.5 text-[15px] text-ink-soft">
          <Spinner tone="dark" />
          Opening your invitation…
        </p>
      </AuthPage>
    );
  }

  const email = me.data?.user?.email;
  const failure =
    invitation.error ??
    (join.error && (join.error as Partial<AuthFailure>).kind !== 'already-member' ? join.error : null);
  if (failure) {
    return failureOf(failure) === 'wrong-account' ? (
      <WrongAccount email={email} path={`/invite/${id}`} />
    ) : (
      <Unusable inOrgs={(orgs.data ?? []).length > 0} />
    );
  }

  const i = invitation.data;
  if (!i) return <Unusable inOrgs={(orgs.data ?? []).length > 0} />;
  // Once joined, the page stays as it is until Acme opens: the person is a member by then.
  const member = !join.isPending && !join.isSuccess && (orgs.data ?? []).some((o) => o.id === i.organizationId);
  if (member || (join.error as Partial<AuthFailure> | null)?.kind === 'already-member') {
    return (
      <AuthPage>
        <AuthHeading>You’re already in {i.organizationName}</AuthHeading>
        <p className="m-0 text-[15px] leading-normal text-ink-soft">Your role and factories stay as they are.</p>
        <Link to="/o/$org" params={{ org: i.organizationSlug }} className={primary}>
          Open {i.organizationName}
        </Link>
      </AuthPage>
    );
  }

  if (decline.isSuccess) {
    return (
      <AuthPage>
        <AuthHeading>Invitation declined</AuthHeading>
        <p className="m-0 text-[15px] leading-normal text-ink-soft">
          You declined to join {i.organizationName}. Ask an owner for a new invitation if you change your mind.
        </p>
        <Link to="/" className={primary}>
          Open Goal
        </Link>
      </AuthPage>
    );
  }

  const inviter = i.inviterName || i.inviterEmail;
  const factories = (i.factories ?? []).map((f) => `${f.name}, ${f.role}`).join('; ');
  const busy = join.isPending || join.isSuccess || decline.isPending;

  return (
    <AuthPage>
      <div className="flex items-center gap-3">
        <span
          aria-hidden="true"
          className="flex size-11 shrink-0 items-center justify-center rounded-[10px] bg-ink text-sm font-semibold text-white"
        >
          {orgMark({ name: i.organizationName, mark: '' })}
        </span>
        <span className="min-w-0">
          <span className="block truncate text-lg font-medium">{i.organizationName}</span>
          <span className="block truncate text-[13px] text-muted">
            {window.location.host}/o/{i.organizationSlug}
          </span>
        </span>
      </div>
      <AuthHeading>
        {inviter} invited you to join {i.organizationName} as {article[i.role]}.
      </AuthHeading>
      <dl className="m-0 grid gap-x-6 gap-y-2.5 text-sm sm:grid-cols-[136px_minmax(0,1fr)]">
        {factories ? <Detail term="In factories">{factories}</Detail> : null}
        <Detail term="Expires">{formatDay(i.expiresAt)}</Detail>
        {email ? <Detail term="Signed in as">{email}</Detail> : null}
      </dl>
      <div className="flex flex-col gap-2 sm:flex-row">
        <Button
          variant="primary"
          size="lg"
          aria-busy={join.isPending || join.isSuccess || undefined}
          disabled={busy}
          onClick={() => join.mutate(i)}
          className="h-12 justify-center aria-busy:cursor-progress sm:flex-1"
        >
          {busy && !decline.isPending ? <Spinner /> : null}
          {busy && !decline.isPending ? `Joining ${i.organizationName}…` : `Join ${i.organizationName}`}
        </Button>
        <Button
          variant="white"
          size="lg"
          disabled={busy}
          onClick={() => decline.mutate(i)}
          className="h-12 justify-center sm:px-6"
        >
          Decline
        </Button>
      </div>
      <div aria-live="polite" className="empty:hidden">
        {decline.error ? (
          <p className="m-0 text-sm text-rework-ink">Not declined: Goal could not be reached. Try again.</p>
        ) : null}
      </div>
      <p className="m-0 flex items-start gap-2 text-[13px] leading-snug text-ink-soft">
        <Info size={16} strokeWidth={1.5} aria-hidden="true" className="mt-px shrink-0 text-muted" />
        Joining gives you the roles above. An owner or admin of {i.organizationName} can change them later in Members
        and roles.
      </p>
    </AuthPage>
  );
}

function Detail({ term, children }: { term: string; children: string }) {
  return (
    <div className="contents">
      <dt className="text-muted">{term}</dt>
      <dd className="m-0 break-words text-ink">{children}</dd>
    </div>
  );
}

/** Signed in with another address than the invitation's: sign out, then in with the right one. */
function WrongAccount({ email, path }: { email: string | undefined; path: string }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const switchAccount = useMutation({
    mutationFn: async () => (await identityService()).signOut(),
    onSuccess: async () => {
      client.clear();
      // Back here once signed in with the invitation's address.
      await navigate({ to: '/sign-in', search: { next: path }, replace: true });
    },
    onError: () => toast.error('Can’t reach Goal. Check your connection and try again.'),
  });
  return (
    <AuthPage>
      <AuthHeading>This invitation is for another email address</AuthHeading>
      {email ? <p className="m-0 text-[15px] leading-normal text-ink-soft">You’re signed in as {email}.</p> : null}
      <Button
        variant="primary"
        size="lg"
        aria-busy={switchAccount.isPending || undefined}
        onClick={() => {
          if (!switchAccount.isPending) switchAccount.mutate();
        }}
        className="h-12 w-full justify-center aria-busy:cursor-progress"
      >
        {switchAccount.isPending ? <Spinner /> : <LogOut size={16} strokeWidth={1.5} aria-hidden="true" />}
        Sign out and switch
      </Button>
    </AuthPage>
  );
}

/** Expired, cancelled, already used or unknown: one message for all. */
function Unusable({ inOrgs }: { inOrgs: boolean }) {
  const { signOut } = useSignOut();
  return (
    <AuthPage>
      <AuthHeading>This invitation can’t be used</AuthHeading>
      <p className="m-0 text-[15px] leading-normal text-ink-soft">
        It may have expired or been cancelled. Ask for a new one.
      </p>
      {inOrgs ? (
        <Link to="/" className={primary}>
          Open Goal
        </Link>
      ) : (
        <Button
          variant="white"
          size="lg"
          aria-busy={signOut.isPending || undefined}
          onClick={() => {
            if (!signOut.isPending) signOut.mutate();
          }}
          className="h-12 w-full justify-center aria-busy:cursor-progress"
        >
          Back to sign in
        </Button>
      )}
    </AuthPage>
  );
}
