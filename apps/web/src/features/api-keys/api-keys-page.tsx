// API keys (spec 3 §10.2, canvas "Api-Keys" and "Mobile-Api-Keys", docs/design/screens/organization/api-keys.jpg):
// the organization's keys with what each can do, where it reaches, how far it acts, who made it,
// when it expires and when it was last used; New key (with api_key:manage, in the top bar) and
// Revoke. Revoked keys stay listed for 30 days, greyed (on phones they are counted, not shown). The
// list loads on its own: when it fails the page keeps its header and New key. Beside the keys: how
// to sign in from a terminal, and the Goal MCP server for other agents (not built yet, so disabled).
// The secret of a new key is shown once (shared/tokens/created-dialog.tsx). Below them, for owners
// and admins (api_key:read), the members' tokens (member-tokens.tsx). A person's own personal tokens
// are on the account page (/account, features/account/personal-tokens.tsx).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, SquareTerminal } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { revokeApiKey } from '../../api/keys';
import { apiKeysQuery, factoriesQuery, keys } from '../../api/queries';
import type { ApiKey, CreatedApiKey } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { formatDay } from '../../shared/format';
import { useOrg, useOrgHolds } from '../../shared/org';
import { CreatedKeyDialog } from '../../shared/tokens/created-dialog';
import { ceilings, expiryText, lastUsedText } from '../../shared/tokens/scopes';
import { Scopes, Soon } from '../../shared/tokens/shared';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { EmptyState } from '../../ui/empty-state';
import { NotAvailable } from '../../ui/not-available';
import { MemberTokensSection } from './member-tokens';
import { NewKeyDialog } from './new-key-dialog';

const roleLabel = (role: string) => ceilings.find((c) => c.value === role)?.label ?? role;

export function ApiKeysPage() {
  const org = useOrg();
  const client = useQueryClient();
  const readKeys = useOrgHolds('api_key:read');
  const manage = useOrgHolds('api_key:manage');
  const list = useQuery({ ...apiKeysQuery(org.id), enabled: readKeys });
  const factories = useQuery(factoriesQuery(org.id));
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);

  const names = new Map((factories.data ?? []).map((f) => [f.id, f.name]));
  const where = (k: ApiKey) => (k.factories ? k.factories.map((id) => names.get(id) ?? id).join(', ') : 'All');
  // The terminal lines name the first factory, so the commands fit this organization.
  const sample = factories.data?.[0]?.name ?? 'factory';
  // The Goal MCP server lives at the address this app is served from.
  const mcpAddress = `${window.location.origin}/mcp`;

  const revoke = useMutation({
    mutationFn: (k: ApiKey) => revokeApiKey(org.id, k.id),
    onSuccess: async (_, k) => {
      setRevoking(null);
      toast.success(`Revoked ${k.name}`, { description: 'Anything using it stops working at once.' });
      await client.invalidateQueries({ queryKey: keys.apiKeys(org.id) });
    },
  });

  const newKey = manage ? (
    <Button variant="primary" className="h-11 md:h-8.5" onClick={() => setCreating(true)}>
      <Plus size={15} strokeWidth={1.75} aria-hidden="true" />
      New key
    </Button>
  ) : null;

  const live = (list.data ?? []).filter((k) => !k.revoked_at);
  const revoked = (list.data ?? []).filter((k) => k.revoked_at);
  const now = Date.now();

  return (
    <>
      <TopBarSlot actions={newKey} />
      <div className="flex min-w-0 flex-col gap-1 md:gap-2">
        <h1 className="m-0 text-page-title tracking-display">API keys</h1>
        {readKeys ? (
          <p className="m-0 text-body text-ink-soft">
            For scripts and services that use Goal’s API as the organization, not as a person.
          </p>
        ) : null}
      </div>

      {!readKeys ? null : (
        <div className="grid gap-4 @[960px]:grid-cols-[minmax(0,1fr)_360px] @[960px]:items-start">
          <div className="flex min-w-0 flex-col">
            {list.isPending ? (
              <div aria-busy="true" className="flex flex-1 flex-col">
                <span role="status" className="sr-only">
                  Loading
                </span>
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
              <section aria-labelledby="active-keys" className="rounded-card border border-line bg-card px-2 pt-4 pb-2">
                <div className="flex items-center gap-2 px-3 pb-3">
                  <h2 id="active-keys" className="m-0 text-heading">
                    Keys
                  </h2>
                  <Chip tone="quiet">{live.length}</Chip>
                </div>

                <table className="hidden w-full border-collapse text-body @[960px]:table">
                  <caption className="sr-only">API keys of {org.name}</caption>
                  <thead>
                    <tr className="border-b border-line text-left text-secondary text-muted [&>th]:px-3 [&>th]:pb-2 [&>th]:font-semibold">
                      <th scope="col">Name</th>
                      <th scope="col">Can</th>
                      <th scope="col">Factories</th>
                      <th scope="col">Acts at most as</th>
                      <th scope="col">Created by</th>
                      <th scope="col">Last used</th>
                      <th scope="col">Expires</th>
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
                            'border-b border-line align-middle last:border-b-0 [&>td]:px-3 [&>td]:py-3',
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
                          <td className="whitespace-nowrap">{k.created_by.name}</td>
                          {k.revoked_at ? (
                            <td colSpan={3} className="text-secondary">
                              {revokedText(k)}
                            </td>
                          ) : (
                            <>
                              <td className="whitespace-nowrap text-muted">{lastUsedText(k.last_used_at, now)}</td>
                              <td className="whitespace-nowrap">
                                <span className="block">{expiry.day}</span>
                                {expiry.soon ? <Soon text={expiry.soon} /> : null}
                              </td>
                              <td className="text-right">
                                {manage ? (
                                  <Button
                                    variant="danger"
                                    size="sm"
                                    onClick={() => setRevoking(k)}
                                    aria-label={`Revoke ${k.name}`}
                                  >
                                    Revoke
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

                <ul className="m-0 flex list-none flex-col gap-2 p-0 px-1 @[960px]:hidden">
                  {live.map((k) => {
                    const expiry = expiryText(k.expires_at, now);
                    return (
                      <li key={k.id} className="flex flex-col gap-2 rounded-control border border-line bg-card p-3">
                        <div className="flex items-start justify-between gap-3">
                          <KeyName apiKey={k} />
                          {manage ? (
                            <Button
                              variant="danger"
                              className="h-11"
                              onClick={() => setRevoking(k)}
                              aria-label={`Revoke ${k.name}`}
                            >
                              Revoke
                            </Button>
                          ) : null}
                        </div>
                        <Scopes scopes={k.scopes} />
                        <p className="m-0 text-body">
                          {roleLabel(k.role_ceiling)} at most, {k.factories ? where(k) : 'all factories'}. Expires{' '}
                          {expiry.day}
                          {expiry.soon ? <Soon text={expiry.soon} /> : null}
                        </p>
                        <p className="m-0 text-secondary text-muted">
                          By {k.created_by.name}, {formatDay(k.created_at)}. Last used{' '}
                          {lastUsedText(k.last_used_at, now).toLowerCase()}
                        </p>
                      </li>
                    );
                  })}
                </ul>
                {revoked.length > 0 ? (
                  <p className="m-0 mt-3 px-3 text-secondary text-muted @[960px]:hidden">
                    {revoked.length === 1 ? '1 revoked key is hidden.' : `${revoked.length} revoked keys are hidden.`}{' '}
                    Revoked keys stay listed for 30 days.
                  </p>
                ) : null}
              </section>
            )}
          </div>

          <aside className="flex min-w-0 flex-col gap-4">
            <section
              aria-labelledby="cli-h"
              className="flex flex-col gap-2 rounded-card border border-line bg-card p-5"
            >
              <h2 id="cli-h" className="m-0 text-heading">
                From your terminal
              </h2>
              <p className="m-0 text-secondary text-ink-soft">People sign in as themselves instead of using a key.</p>
              <pre className="m-0 overflow-x-auto rounded-cell bg-andon p-3 font-mono text-secondary leading-relaxed whitespace-pre text-andon-text">
                <span aria-hidden="true" className="text-run-soft">
                  ${' '}
                </span>
                goal login
                {'\n'}
                <span aria-hidden="true" className="text-run-soft">
                  ${' '}
                </span>
                goal item create --factory {sample}
              </pre>
            </section>
            <section
              aria-labelledby="mcp-h"
              className="flex flex-col gap-2 rounded-card border border-line bg-card p-5"
            >
              <h2 id="mcp-h" className="m-0 text-heading">
                Goal for other agents
              </h2>
              <p className="m-0 text-secondary text-ink-soft">
                Agents outside Goal can create items and read the board through Goal’s MCP server.
              </p>
              <div className="flex h-9 items-center gap-2 rounded-control border border-rule bg-panel pr-2 pl-3">
                <span className="min-w-0 flex-1 truncate font-mono text-secondary">{mcpAddress}</span>
                <NotAvailable>
                  <Button variant="white" size="sm">
                    Copy
                  </Button>
                </NotAvailable>
              </div>
            </section>
          </aside>
        </div>
      )}

      {readKeys ? <MemberTokensSection org={org} /> : null}

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
      <CreatedKeyDialog created={created} factoryNames={names} onDone={() => setCreated(null)} />
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
          <p role="alert" className="m-0 text-body text-rework-ink">
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
      <span className="truncate font-semibold">{apiKey.name}</span>
      <span className="font-mono text-meta text-muted">{apiKey.hint}…</span>
    </span>
  );
}
