// Personal tokens on the API keys page (spec 3 §10.2, PR 3-12b): "Personal tokens", the caller's
// own in this organization, with New token (personal_token:manage) and Revoke… (no recent sign-in
// needed, so a leaked token goes at once); and "Members' tokens" for owners and admins
// (api_key:read), every member's, with Revoke… under api_key:manage (a recent sign-in). Both list
// live tokens and, greyed, those revoked in the last 30 days (counted, not shown, on phones). A
// link to #personal-tokens (the account menu's) scrolls there and puts focus on its heading.
import { type UseQueryResult, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation } from '@tanstack/react-router';
import { KeyRound } from 'lucide-react';
import { type ReactNode, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { revokeMemberToken, revokePersonalToken } from '../../api/keys';
import { factoriesQuery, keys, memberTokensQuery, meQuery, personalTokensQuery } from '../../api/queries';
import type { CreatedPersonalToken, OrgSummary, PersonalToken } from '../../api/types';
import { formatDay } from '../../shared/format';
import { displayName } from '../../shared/names';
import { useOrgGrants, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { CreatedTokenDialog } from './created-dialog';
import { NewTokenDialog } from './new-token-dialog';
import { expiryText, lastUsedText } from './scopes';
import { Scopes, Soon } from './shared';

/** The id the account menu's link names. */
export const personalTokensAnchor = 'personal-tokens';

/** The caller's own tokens, New token and Revoke…. */
export function PersonalTokensSection({ org }: { org: OrgSummary }) {
  const client = useQueryClient();
  const grants = useOrgGrants();
  const manage = useOrgHolds('personal_token:manage');
  const { data: signedIn } = useQuery({ ...meQuery, select: (me) => me.mode === 'session' });
  const list = useQuery({ ...personalTokensQuery(org.id), enabled: signedIn === true });
  const factories = useQuery(factoriesQuery(org.id));
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedPersonalToken | null>(null);
  const [revoking, setRevoking] = useState<PersonalToken | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const hash = useLocation({ select: (l) => l.hash });

  useEffect(() => {
    if (hash.replace(/^#/, '') !== personalTokensAnchor || !heading.current) return;
    heading.current.scrollIntoView({ block: 'start' });
    heading.current.focus({ preventScroll: true });
  }, [hash]);

  const revoke = useMutation({
    mutationFn: (t: PersonalToken) => revokePersonalToken(org.id, t.id),
    onSuccess: async (_, t) => {
      setRevoking(null);
      toast.success(`Revoked ${t.name}`, { description: 'Anything using it stops working at once.' });
      await Promise.all([
        client.invalidateQueries({ queryKey: keys.personalTokens(org.id) }),
        client.invalidateQueries({ queryKey: keys.memberTokens(org.id) }),
      ]);
    },
  });

  const newToken =
    manage && signedIn ? (
      <Button variant="white" className="h-11 md:h-10" onClick={() => setCreating(true)}>
        <span aria-hidden="true">+</span> New token
      </Button>
    ) : null;

  return (
    <section
      id={personalTokensAnchor}
      aria-labelledby="personal-tokens-heading"
      className="flex scroll-mt-6 flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex max-w-[680px] min-w-0 flex-col gap-1.5">
          <h2 id="personal-tokens-heading" ref={heading} tabIndex={-1} className="m-0 text-xl font-medium outline-none">
            Personal tokens
          </h2>
          <p className="m-0 text-sm leading-normal text-muted">
            A personal token lets the CLI or a script on your machine act as you in {org.name}, with no more than your
            role allows. Tokens expire after at most 90 days.
          </p>
        </div>
        {newToken}
      </div>

      {signedIn === false ? (
        <Callout tone="note">
          Personal tokens need sign-in. With sign-in off, the CLI acts as the local user without one.
        </Callout>
      ) : (
        <TokenList
          list={list}
          what="your personal tokens"
          caption={`Your personal tokens in ${org.name}`}
          empty="No personal tokens. Use goal login for the CLI, or create a token for a headless machine."
          onRevoke={setRevoking}
        />
      )}

      {creating ? (
        <NewTokenDialog
          org={org}
          orgCan={grants?.can}
          factories={factories.data ?? []}
          onClose={() => setCreating(false)}
          onCreated={(t) => {
            setCreating(false);
            setCreated(t);
            void client.invalidateQueries({ queryKey: keys.personalTokens(org.id) });
            void client.invalidateQueries({ queryKey: keys.memberTokens(org.id) });
          }}
        />
      ) : null}
      <CreatedTokenDialog created={created} orgName={org.name} onDone={() => setCreated(null)} />
      <RevokeDialog
        token={revoking}
        title={`Revoke ${revoking?.name ?? 'token'}?`}
        pending={revoke.isPending}
        error={revoke.error}
        onCancel={() => {
          setRevoking(null);
          revoke.reset();
        }}
        onConfirm={() => {
          if (revoking) revoke.mutate(revoking);
        }}
      />
    </section>
  );
}

/** Every member's tokens, for owners and admins. */
export function MemberTokensSection({ org }: { org: OrgSummary }) {
  const client = useQueryClient();
  const manage = useOrgHolds('api_key:manage');
  const list = useQuery(memberTokensQuery(org.id));
  const [revoking, setRevoking] = useState<PersonalToken | null>(null);

  const revoke = useMutation({
    mutationFn: (t: PersonalToken) => revokeMemberToken(org.id, t.id),
    onSuccess: async (_, t) => {
      setRevoking(null);
      toast.success(`Revoked ${displayName(t.owner)}’s token ${t.name}`, {
        description: 'Anything using it stops working at once.',
      });
      await Promise.all([
        client.invalidateQueries({ queryKey: keys.memberTokens(org.id) }),
        client.invalidateQueries({ queryKey: keys.personalTokens(org.id) }),
      ]);
    },
  });

  return (
    <section
      aria-labelledby="member-tokens-heading"
      className="flex flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex max-w-[680px] min-w-0 flex-col gap-1.5">
        <h2 id="member-tokens-heading" className="m-0 text-xl font-medium">
          Members’ tokens
        </h2>
        <p className="m-0 text-sm leading-normal text-muted">
          Every member’s personal tokens in {org.name}. Each acts as its owner, so removing someone from {org.name}{' '}
          deletes theirs.
        </p>
      </div>
      <TokenList
        list={list}
        what="members’ tokens"
        caption={`Members’ personal tokens in ${org.name}`}
        empty="No member has a personal token."
        owner
        onRevoke={manage ? setRevoking : undefined}
      />
      <RevokeDialog
        token={revoking}
        title={revoking ? `Revoke ${displayName(revoking.owner)}’s token ${revoking.name}?` : 'Revoke token?'}
        pending={revoke.isPending}
        error={revoke.error}
        onCancel={() => {
          setRevoking(null);
          revoke.reset();
        }}
        onConfirm={() => {
          if (revoking) revoke.mutate(revoking);
        }}
      />
    </section>
  );
}

interface TokenListProps {
  list: UseQueryResult<PersonalToken[]>;
  /** What the list is, in "Loading …" and "Couldn't load …". */
  what: string;
  caption: string;
  empty: ReactNode;
  /** Show each token's owner (the members' list). */
  owner?: boolean;
  /** Offer Revoke… on live tokens; absent: no revoking here. */
  onRevoke?: ((t: PersonalToken) => void) | undefined;
}

function TokenList({ list, what, caption, empty, owner = false, onRevoke }: TokenListProps) {
  if (list.isPending) {
    return (
      <div aria-busy="true" className="flex flex-col gap-3 py-1">
        <span role="status" className="sr-only">
          Loading {what}
        </span>
        {[0, 1].map((i) => (
          <div key={i} className="grid grid-cols-[1fr_2fr_1fr] gap-4">
            <div className="h-3 rounded-chip bg-panel" />
            <div className="h-3 rounded-chip bg-panel" />
            <div className="h-3 rounded-chip bg-panel" />
          </div>
        ))}
      </div>
    );
  }
  if (list.isError) {
    return (
      <div className="flex flex-col items-start gap-3" role="alert">
        <Callout tone="rework">Couldn’t load {what}. Check your connection and try again.</Callout>
        <Button variant="white" onClick={() => void list.refetch()}>
          Try again
        </Button>
      </div>
    );
  }

  const live = list.data.filter((t) => !t.revoked_at);
  const revoked = list.data.filter((t) => t.revoked_at);
  if (live.length === 0 && revoked.length === 0) {
    return (
      <p className="m-0 flex items-start gap-2.5 text-sm text-muted">
        <KeyRound size={18} strokeWidth={1.5} aria-hidden="true" className="mt-px shrink-0" />
        {empty}
      </p>
    );
  }
  const now = Date.now();
  const label = (t: PersonalToken) =>
    owner ? `Revoke ${displayName(t.owner)}’s token ${t.name}…` : `Revoke ${t.name}…`;

  return (
    <>
      <table className="hidden w-full border-collapse text-sm @[960px]:table">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr className="text-left text-[13px] text-muted [&>th]:pb-2 [&>th]:font-normal">
            <th scope="col">Token</th>
            {owner ? <th scope="col">Owner</th> : null}
            <th scope="col">Can</th>
            <th scope="col">Created</th>
            <th scope="col">Expires</th>
            <th scope="col">Last used</th>
            <th scope="col">
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {[...live, ...revoked].map((t) => {
            const expiry = expiryText(t.expires_at, now);
            return (
              <tr
                key={t.id}
                className={cn(
                  'border-t border-line align-middle [&>td]:py-3 [&>td]:pr-3',
                  t.revoked_at && 'text-muted',
                )}
              >
                <td>
                  <TokenName token={t} />
                </td>
                {owner ? <td className="max-w-40 truncate">{displayName(t.owner)}</td> : null}
                <td>
                  <Scopes scopes={t.scopes} />
                </td>
                <td className="whitespace-nowrap">{formatDay(t.created_at)}</td>
                {t.revoked_at ? (
                  <td colSpan={3} className="text-[13px]">
                    {revokedText(t)}
                  </td>
                ) : (
                  <>
                    <td className="whitespace-nowrap">
                      <span className="block">{expiry.day}</span>
                      {expiry.soon ? <Soon text={expiry.soon} /> : null}
                    </td>
                    <td className="whitespace-nowrap text-muted">{lastUsedText(t.last_used_at, now)}</td>
                    <td className="text-right">
                      {onRevoke ? (
                        <Button variant="white" size="sm" onClick={() => onRevoke(t)} aria-label={label(t)}>
                          Revoke…
                        </Button>
                      ) : null}
                    </td>
                  </>
                )}
              </tr>
            );
          })}
        </tbody>
      </table>

      <ul className="m-0 flex list-none flex-col gap-3 p-0 @[960px]:hidden">
        {live.map((t) => {
          const expiry = expiryText(t.expires_at, now);
          return (
            <li key={t.id} className="flex flex-col gap-2 rounded-control border border-line bg-card p-3.5">
              <div className="flex items-start justify-between gap-3">
                <TokenName token={t} />
                {onRevoke ? (
                  <Button variant="white" className="h-11" onClick={() => onRevoke(t)} aria-label={label(t)}>
                    Revoke…
                  </Button>
                ) : null}
              </div>
              {owner ? <p className="m-0 text-sm">{displayName(t.owner)}</p> : null}
              <Scopes scopes={t.scopes} />
              <p className="m-0 text-sm">
                Expires {expiry.day}
                {expiry.soon ? <Soon text={expiry.soon} /> : null}
              </p>
              <p className="m-0 text-[13px] text-muted">
                Created {formatDay(t.created_at)}. Last used {lastUsedText(t.last_used_at, now).toLowerCase()}
              </p>
            </li>
          );
        })}
      </ul>
      {revoked.length > 0 ? (
        <p className="m-0 text-[13px] text-muted @[960px]:hidden">
          {revoked.length === 1 ? '1 revoked token is hidden.' : `${revoked.length} revoked tokens are hidden.`} Revoked
          tokens stay listed for 30 days.
        </p>
      ) : null}
    </>
  );
}

function TokenName({ token }: { token: PersonalToken }) {
  return (
    <span className="flex min-w-0 flex-col">
      <span className="truncate text-[15px] font-medium">{token.name}</span>
      <span className="font-mono text-xs text-muted">{token.hint}…</span>
    </span>
  );
}

function revokedText(t: PersonalToken): string {
  const at = t.revoked_at ? formatDay(t.revoked_at) : '';
  return t.revoked_by ? `Revoked ${at} by ${t.revoked_by.name}` : `Revoked ${at}`;
}

function RevokeDialog({
  token,
  title,
  pending,
  error,
  onCancel,
  onConfirm,
}: {
  token: PersonalToken | null;
  title: string;
  pending: boolean;
  error: Error | null;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <ConfirmDialog
      open={token !== null}
      onOpenChange={(open) => {
        if (!open) onCancel();
      }}
      title={title}
      description="Anything using it stops working at once."
      confirmLabel="Revoke token"
      pendingLabel="Revoking…"
      tone="danger"
      pending={pending}
      onConfirm={onConfirm}
    >
      {error ? (
        <p role="alert" className="m-0 text-sm text-rework-ink">
          {error instanceof ApiError
            ? (error.detail ?? error.title)
            : 'goald is not answering. Try again once it is running.'}
        </p>
      ) : null}
    </ConfirmDialog>
  );
}
