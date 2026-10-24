// API keys (spec 3 §10.2, canvas "Api-Keys" and "Mobile-Api-Keys"): the organization's keys with
// what each can do, where it reaches, how far it acts, who made it, when it expires and when it
// was last used; New key (with api_key:manage) and Revoke…. Revoked keys stay listed for 30 days,
// greyed (on phones they are counted, not shown). The list loads on its own: when it fails the
// page keeps its header and New key. The secret of a new key is shown once (created-dialog.tsx).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Clock, SquareTerminal } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { revokeApiKey } from '../../api/keys';
import { apiKeysQuery, factoriesQuery, keys } from '../../api/queries';
import type { ApiKey, CreatedApiKey } from '../../api/types';
import { formatDay, plural } from '../../shared/format';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { EmptyState } from '../../ui/empty-state';
import { CreatedKeyDialog } from './created-dialog';
import { NewKeyDialog } from './new-key-dialog';
import { ceilings, expiryText, lastUsedText, scopeLabel } from './scopes';

/** How many scope chips a row shows before "+N more". */
const chipsShown = 2;

const roleLabel = (role: string) => ceilings.find((c) => c.value === role)?.label ?? role;

export function ApiKeysPage() {
  const org = useOrg();
  const client = useQueryClient();
  const manage = useOrgHolds('api_key:manage');
  const list = useQuery(apiKeysQuery(org.id));
  const factories = useQuery(factoriesQuery(org.id));
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);

  const names = new Map((factories.data ?? []).map((f) => [f.id, f.name]));
  const where = (k: ApiKey) => (k.factories ? k.factories.map((id) => names.get(id) ?? id).join(', ') : 'All');

  const revoke = useMutation({
    mutationFn: (k: ApiKey) => revokeApiKey(org.id, k.id),
    onSuccess: async (_, k) => {
      setRevoking(null);
      toast.success(`Revoked ${k.name}`, { description: 'Anything using it stops working at once.' });
      await client.invalidateQueries({ queryKey: keys.apiKeys(org.id) });
    },
  });

  const newKey = manage ? (
    <Button variant="primary" className="h-11 md:h-10" onClick={() => setCreating(true)}>
      <span aria-hidden="true">+</span> New key
    </Button>
  ) : null;

  const live = (list.data ?? []).filter((k) => !k.revoked_at);
  const revoked = (list.data ?? []).filter((k) => k.revoked_at);
  const now = Date.now();
  const sample = factories.data?.[0]?.name ?? 'sre';

  return (
    <>
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex max-w-[760px] min-w-0 flex-col gap-2">
          <p className="m-0 text-[13px] text-muted md:hidden">{org.name}</p>
          <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-5xl">API keys</h1>
          <p className="m-0 text-[15px] leading-normal text-muted">
            Keys let scripts and CI act for {org.name}. A key never manages people, keys or ownership. Keys expire after
            at most 90 days.
          </p>
        </div>
        {newKey}
      </header>

      {list.isPending ? (
        <div aria-busy="true" className="rounded-card border border-line bg-card p-5">
          <span role="status" className="sr-only">
            Loading API keys
          </span>
          <div className="flex flex-col gap-3">
            {[0, 1, 2].map((i) => (
              <div key={i} className="grid grid-cols-[1fr_2fr_1fr] gap-4">
                <div className="h-3 rounded-chip bg-control" />
                <div className="h-3 rounded-chip bg-control" />
                <div className="h-3 rounded-chip bg-control" />
              </div>
            ))}
          </div>
        </div>
      ) : list.isError ? (
        <div
          className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-5"
          role="alert"
        >
          <Callout tone="rework">Couldn’t load API keys. Check your connection and try again.</Callout>
          <Button variant="white" onClick={() => void list.refetch()}>
            Try again
          </Button>
        </div>
      ) : live.length === 0 && revoked.length === 0 ? (
        <EmptyState icon={<SquareTerminal size={20} strokeWidth={1.5} />} title="No keys yet" actions={newKey}>
          Create one for CI or a script.
        </EmptyState>
      ) : (
        <section aria-labelledby="active-keys" className="rounded-card border border-line bg-card p-4 md:p-5">
          <div className="mb-3 flex flex-wrap items-baseline justify-between gap-2">
            <h2 id="active-keys" className="m-0 text-lg font-medium">
              {live.length === 0 ? 'No active keys' : plural(live.length, 'active key')}
            </h2>
            <p className="m-0 hidden text-[13px] text-muted @[960px]:block">Revoked keys stay listed for 30 days</p>
          </div>

          <table className="hidden w-full border-collapse text-sm @[960px]:table">
            <caption className="sr-only">API keys of {org.name}</caption>
            <thead>
              <tr className="text-left text-[13px] text-muted [&>th]:pb-2 [&>th]:font-normal">
                <th scope="col">Key</th>
                <th scope="col">Can</th>
                <th scope="col">Factories</th>
                <th scope="col">Acts at most as</th>
                <th scope="col">Created</th>
                <th scope="col">Expires</th>
                <th scope="col">Last used</th>
                <th scope="col">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {[...live, ...revoked].map((k) => {
                const expiry = expiryText(k.expires_at, now);
                return (
                  <tr
                    key={k.id}
                    className={cn(
                      'border-t border-line align-middle [&>td]:py-3 [&>td]:pr-3',
                      k.revoked_at && 'text-muted',
                    )}
                  >
                    <td>
                      <KeyName apiKey={k} />
                    </td>
                    <td>
                      <Scopes scopes={k.scopes} />
                    </td>
                    <td className="max-w-40 truncate">{where(k)}</td>
                    <td>{roleLabel(k.role_ceiling)}</td>
                    <td className="whitespace-nowrap">
                      <span className="block">{k.created_by.name}</span>
                      <span className="block text-[13px] text-muted">{formatDay(k.created_at)}</span>
                    </td>
                    {k.revoked_at ? (
                      <td colSpan={3} className="text-[13px]">
                        {revokedText(k)}
                      </td>
                    ) : (
                      <>
                        <td className="whitespace-nowrap">
                          <span className="block">{expiry.day}</span>
                          {expiry.soon ? <Soon text={expiry.soon} /> : null}
                        </td>
                        <td className="whitespace-nowrap text-muted">{lastUsedText(k.last_used_at, now)}</td>
                        <td className="text-right">
                          {manage ? (
                            <Button
                              variant="white"
                              size="sm"
                              onClick={() => setRevoking(k)}
                              aria-label={`Revoke ${k.name}…`}
                            >
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
            {live.map((k) => {
              const expiry = expiryText(k.expires_at, now);
              return (
                <li key={k.id} className="flex flex-col gap-2 rounded-control border border-line bg-surface p-3.5">
                  <div className="flex items-start justify-between gap-3">
                    <KeyName apiKey={k} />
                    {manage ? (
                      <Button
                        variant="white"
                        className="h-11"
                        onClick={() => setRevoking(k)}
                        aria-label={`Revoke ${k.name}…`}
                      >
                        Revoke…
                      </Button>
                    ) : null}
                  </div>
                  <Scopes scopes={k.scopes} />
                  <p className="m-0 text-sm">
                    {roleLabel(k.role_ceiling)} at most, {k.factories ? where(k) : 'all factories'}. Expires{' '}
                    {expiry.day}
                    {expiry.soon ? <Soon text={expiry.soon} /> : null}
                  </p>
                  <p className="m-0 text-[13px] text-muted">
                    By {k.created_by.name}, {formatDay(k.created_at)}. Last used{' '}
                    {lastUsedText(k.last_used_at, now).toLowerCase()}
                  </p>
                </li>
              );
            })}
          </ul>
          {revoked.length > 0 ? (
            <p className="m-0 mt-3 text-[13px] text-muted @[960px]:hidden">
              {revoked.length === 1 ? '1 revoked key is hidden.' : `${revoked.length} revoked keys are hidden.`} Revoked
              keys stay listed for 30 days.
            </p>
          ) : null}
        </section>
      )}

      <div className="grid gap-4 @[960px]:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
        <section
          aria-labelledby="key-ci"
          className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-5"
        >
          <h2 id="key-ci" className="m-0 text-lg font-medium">
            Use a key from CI
          </h2>
          <p className="m-0 text-sm leading-normal text-muted">
            Put the secret in your CI’s secret store as GOAL_API_KEY. The CLI reads it from the environment and never
            writes it anywhere.
          </p>
          <pre className="m-0 rounded-control border border-line bg-surface p-3.5 font-mono text-[13px] leading-relaxed break-words whitespace-pre-wrap">
            {`export GOAL_API_KEY="$GOAL_KEY_FROM_CI"\ngoal factory list\ngoal run ${sample} "Deploy 1.4.2 to staging" --detach`}
          </pre>
        </section>
        <section
          aria-labelledby="key-never"
          className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-5"
        >
          <h2 id="key-never" className="m-0 text-lg font-medium">
            What a key can never do
          </h2>
          <p className="m-0 text-sm leading-normal text-muted">
            Invite or remove people, change roles, see invitations, manage keys, transfer or delete {org.name}, or
            create organizations. Those need a person signed in.
          </p>
          <p className="m-0 text-sm leading-normal text-muted">
            Creating or revoking a key needs a sign-in from the last 10 minutes. Both go into the audit log.
          </p>
        </section>
      </div>

      {creating ? (
        <NewKeyDialog
          org={org}
          factories={factories.data ?? []}
          onClose={() => setCreating(false)}
          onCreated={(k) => {
            setCreating(false);
            setCreated(k);
            void client.invalidateQueries({ queryKey: keys.apiKeys(org.id) });
          }}
        />
      ) : null}
      <CreatedKeyDialog created={created} onDone={() => setCreated(null)} />
      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(open) => {
          if (!open) {
            setRevoking(null);
            revoke.reset();
          }
        }}
        title={`Revoke ${revoking?.name ?? 'key'}?`}
        description="Anything using it stops working at once."
        confirmLabel="Revoke key"
        pendingLabel="Revoking…"
        tone="danger"
        pending={revoke.isPending}
        onConfirm={() => {
          if (revoking) revoke.mutate(revoking);
        }}
      >
        {revoke.error ? (
          <p role="alert" className="m-0 text-sm text-rework-ink">
            {revoke.error instanceof ApiError
              ? (revoke.error.detail ?? revoke.error.title)
              : 'goald is not answering. Try again once it is running.'}
          </p>
        ) : null}
      </ConfirmDialog>
    </>
  );
}

function revokedText(k: ApiKey): string {
  const at = k.revoked_at ? formatDay(k.revoked_at) : '';
  return k.revoked_by ? `Revoked ${at} by ${k.revoked_by.name}` : `Revoked ${at}`;
}

function KeyName({ apiKey }: { apiKey: ApiKey }) {
  return (
    <span className="flex min-w-0 flex-col">
      <span className="truncate text-[15px] font-medium">{apiKey.name}</span>
      <span className="font-mono text-xs text-muted">{apiKey.hint}…</span>
    </span>
  );
}

function Scopes({ scopes }: { scopes: readonly string[] }) {
  const shown = scopes.slice(0, chipsShown);
  const more = scopes.length - shown.length;
  return (
    <span className="flex flex-wrap gap-1.5">
      {shown.map((s) => (
        <Chip key={s} tone="quiet">
          {scopeLabel(s)}
        </Chip>
      ))}
      {more > 0 ? (
        <Chip tone="plain">
          <span aria-hidden="true">+{more} more</span>
          <span className="sr-only">and {scopes.slice(chipsShown).map(scopeLabel).join(', ')}</span>
        </Chip>
      ) : null}
    </span>
  );
}

function Soon({ text }: { text: string }) {
  return (
    <span className="ml-1 inline-flex items-center gap-1 text-[13px] font-medium text-rework-ink md:ml-0 md:flex">
      <Clock size={13} strokeWidth={2} aria-hidden="true" />
      {text}
    </span>
  );
}
