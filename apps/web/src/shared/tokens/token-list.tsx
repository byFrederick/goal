// The token lists the API keys page and the account page share: live tokens, and greyed, those
// revoked in the last 30 days (counted, not shown, on phones), with Revoke… where the caller may
// revoke. RevokeDialog confirms a revoke and shows goald's refusal.
import type { UseQueryResult } from '@tanstack/react-query';
import { KeyRound } from 'lucide-react';
import type { ReactNode } from 'react';
import { ApiError } from '../../api/client';
import type { PersonalToken } from '../../api/types';
import { formatDay } from '../../shared/format';
import { displayName } from '../../shared/names';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { expiryText, lastUsedText } from './scopes';
import { Scopes, Soon } from './shared';

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

export function TokenList({ list, what, caption, empty, owner = false, onRevoke }: TokenListProps) {
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
      <p className="m-0 flex items-start gap-3 text-sm text-muted">
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
          <tr className="text-left text-secondary text-muted [&>th]:pb-2 [&>th]:font-normal">
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
                  <td colSpan={3} className="text-secondary">
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
            <li key={t.id} className="flex flex-col gap-2 rounded-control border border-line bg-card p-4">
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
              <p className="m-0 text-secondary text-muted">
                Created {formatDay(t.created_at)}. Last used {lastUsedText(t.last_used_at, now).toLowerCase()}
              </p>
            </li>
          );
        })}
      </ul>
      {revoked.length > 0 ? (
        <p className="m-0 text-secondary text-muted @[960px]:hidden">
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
      <span className="truncate text-body font-medium">{token.name}</span>
      <span className="font-mono text-xs text-muted">{token.hint}…</span>
    </span>
  );
}

function revokedText(t: PersonalToken): string {
  const at = t.revoked_at ? formatDay(t.revoked_at) : '';
  return t.revoked_by ? `Revoked ${at} by ${t.revoked_by.name}` : `Revoked ${at}`;
}

export function RevokeDialog({
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
