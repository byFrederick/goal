// Secrets (ADR 0037 decision 14, canvas docs/design/screens/connections/secrets.jpg, secrets-768.jpg and
// mobile/secrets.jpg): the organization's secrets by name, where each is used, who may open it, when it
// expires and when it was last used. Nobody reads a value back. The filters (Expiring, Not used), the search
// text and the open New secret sheet live in the URL. Owners and admins (secret:manage) get New secret and
// Replace value; members read.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { Plus, Search, TriangleAlert } from 'lucide-react';
import { useDeferredValue, useState } from 'react';
import { factoriesQuery, secretsQuery } from '../../api/queries';
import type { Secret } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { EmptyState } from '../../ui/empty-state';
import { SegmentedControl } from '../../ui/segmented-control';
import { Skeleton } from '../../ui/skeleton';
import { NewSecretSheet } from './new-secret-sheet';
import { ExpiryText, FactoryChips } from './parts';
import { type SecretFilter, secretsSearch } from './search';
import { ReplaceValueDialog } from './secret-dialogs';
import { expiringHeadline, expiringSoon, expiryOf, lastUsedText, placesText, usedAtLabel } from './words';

type Filter = SecretFilter | 'all';

/** "software · deploy and 4 more": the first use, and how many more there are. */
function UsedIn({ secret }: { secret: Secret }) {
  const [first, ...more] = secret.uses;
  if (!first) return <span className="text-muted">Not used</span>;
  return (
    <span className="block min-w-0 break-words">
      {usedAtLabel(first)}
      {more.length > 0 ? <span className="text-muted"> and {more.length} more</span> : null}
    </span>
  );
}

export function SecretsPage() {
  const org = useOrg();
  const search = secretsSearch(useSearch({ strict: false }));
  const navigate = useNavigate({ from: '/o/$org/secrets/' });
  const manage = useOrgHolds('secret:manage');
  const list = useQuery(secretsQuery(org.id));
  const factories = useQuery(factoriesQuery(org.id)).data ?? [];
  const [replacing, setReplacing] = useState<Secret | null>(null);
  const [text, setText] = useState(search.q ?? '');
  const q = useDeferredValue(text.trim().toLowerCase());
  const now = Date.now();

  const all = list.data ?? [];
  const expiring = expiringSoon(all, now);
  const unused = all.filter((s) => s.uses.length === 0);
  const shown = all.filter(
    (s) =>
      (search.filter === 'expiring' ? expiring.includes(s) : search.filter === 'unused' ? s.uses.length === 0 : true) &&
      (!q || [s.name, s.description].some((t) => t.toLowerCase().includes(q))),
  );
  const soonest = [...expiring].sort((a, b) => Date.parse(a.expires_at ?? '') - Date.parse(b.expires_at ?? ''))[0];

  const setSearch = (patch: { filter?: SecretFilter | undefined; q?: string | undefined; new?: true | undefined }) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true });

  const searchField = (
    <label className="relative flex h-11 w-40 items-center md:h-8.5 md:w-55">
      <span className="sr-only">Search secrets</span>
      <Search
        size={15}
        strokeWidth={1.5}
        aria-hidden="true"
        className="pointer-events-none absolute left-3 text-muted"
      />
      <input
        type="search"
        value={text}
        placeholder="Search secrets"
        autoComplete="off"
        className="h-full w-full rounded-control border border-rule bg-card pr-3 pl-9 text-body text-ink placeholder:text-muted transition-colors duration-(--duration-quick) hover:border-muted"
        onChange={(e) => {
          setText(e.target.value);
          setSearch({ q: e.target.value.trim() || undefined });
        }}
      />
    </label>
  );

  return (
    <>
      <TopBarSlot
        actions={
          <>
            <span className="max-md:hidden">{searchField}</span>
            {manage ? (
              <Button asChild variant="white" className="h-11 md:h-8.5">
                <Link to="/o/$org/secrets/import" params={{ org: org.slug }}>
                  Import
                </Link>
              </Button>
            ) : null}
            {manage ? (
              <Button variant="action" className="h-11 md:h-8.5" onClick={() => setSearch({ new: true })}>
                <Plus size={15} strokeWidth={1.8} aria-hidden="true" />
                <span className="md:hidden">
                  New<span className="sr-only"> secret</span>
                </span>
                <span className="max-md:hidden">New secret</span>
              </Button>
            ) : null}
          </>
        }
      />
      <div className="@container flex min-w-0 flex-col gap-5">
        <div className="flex min-w-0 flex-col gap-1 md:gap-2">
          <h1 className="m-0 text-page-title tracking-display max-md:sr-only">Secrets</h1>
          <p className="m-0 max-w-[680px] text-body text-ink-soft">
            Every key agents use, in one place. Environment files and the repository refer to a secret by name; nobody
            can read a value back.
          </p>
        </div>

        {soonest ? (
          <section
            aria-label="Expiring"
            className="flex flex-col gap-3 rounded-card bg-rework-tint p-4 @[640px]:flex-row @[640px]:items-center"
          >
            <TriangleAlert
              size={16}
              strokeWidth={1.5}
              aria-hidden="true"
              className="hidden shrink-0 text-rework-ink @[640px]:block"
            />
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <p className="m-0 text-body font-semibold text-ink">{expiringHeadline(soonest, now)}</p>
              <p className="m-0 text-secondary text-ink-soft">
                {soonest.uses[0] ? `${usedAtLabel(soonest.uses[0])} uses it.` : 'Nothing uses it yet.'}
                {expiring.length > 1 ? ` ${expiring.length - 1} more expiring.` : ''}
              </p>
            </div>
            {manage ? (
              <Button variant="white" className="h-11 max-@[640px]:w-full md:h-9" onClick={() => setReplacing(soonest)}>
                Replace value
              </Button>
            ) : null}
          </section>
        ) : null}

        {list.isPending ? (
          <Skeleton
            ready={false}
            placeholder={
              <div className="flex flex-col gap-3 rounded-card border border-line bg-card p-5">
                <div className="h-8 w-64 rounded-control bg-hover" />
                <div className="h-12 rounded-control bg-hover" />
                <div className="h-12 rounded-control bg-hover" />
              </div>
            }
          >
            <span />
          </Skeleton>
        ) : list.isError ? (
          <div
            className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-5"
            role="alert"
          >
            <Callout tone="rework">Couldn’t load secrets. Check your connection and try again.</Callout>
            <Button variant="white" onClick={() => void list.refetch()}>
              Try again
            </Button>
          </div>
        ) : all.length === 0 ? (
          <EmptyState title="No secrets yet">
            {manage
              ? 'Add the keys and tokens your agents use, or import a .env file.'
              : `Owners and admins of ${org.name} add secrets.`}
          </EmptyState>
        ) : (
          <>
            <SegmentedControl<Filter>
              label="Filter"
              value={search.filter ?? 'all'}
              onChange={(value) => setSearch({ filter: value === 'all' ? undefined : value })}
              options={[
                { value: 'all', label: 'All', count: all.length },
                { value: 'expiring', label: 'Expiring', count: expiring.length },
                { value: 'unused', label: 'Not used', count: unused.length },
              ]}
            />

            {shown.length === 0 ? (
              <div className="flex flex-col items-start gap-2 rounded-card border border-line bg-card p-4">
                <p className="m-0 text-body text-muted">No secret matches these filters.</p>
                <Button
                  variant="white"
                  onClick={() => {
                    setText('');
                    setSearch({ filter: undefined, q: undefined });
                  }}
                >
                  Show every secret
                </Button>
              </div>
            ) : (
              <>
                <ul className="m-0 flex list-none flex-col rounded-card border border-line bg-card p-0 md:hidden">
                  {shown.map((s) => {
                    const expiry = expiryOf(s, now);
                    return (
                      <li key={s.id} className="border-t border-line first:border-t-0">
                        <Link
                          to="/o/$org/secrets/$secret"
                          params={{ org: org.slug, secret: s.name }}
                          className="flex min-h-11 flex-col gap-1 p-4 text-ink no-underline"
                        >
                          <span className="flex items-baseline justify-between gap-3">
                            <span className="min-w-0 break-all font-mono text-body font-medium">{s.name}</span>
                            <span className="shrink-0 text-secondary">
                              {expiry.state === 'reminder' || expiry.state === 'expired' ? (
                                <ExpiryText expiry={expiry} />
                              ) : s.uses.length === 0 ? (
                                <span className="inline-flex items-center gap-2 text-muted">
                                  <span aria-hidden="true" className="size-1.5 rounded-full bg-muted" />
                                  Not used
                                </span>
                              ) : (
                                <span className="text-muted">Used {lastUsedText(s, now).toLowerCase()}</span>
                              )}
                            </span>
                          </span>
                          <span className="text-secondary text-muted">
                            {s.description ? `${s.description} · ` : ''}
                            {placesText(s.uses.length)}
                          </span>
                        </Link>
                      </li>
                    );
                  })}
                </ul>

                <div className="rounded-card border border-line bg-card max-md:hidden">
                  <table className="w-full table-fixed border-collapse text-secondary">
                    <caption className="sr-only">Secrets of {org.name}</caption>
                    <thead>
                      <tr className="text-left text-muted [&>th]:px-4 [&>th]:py-3 [&>th]:font-medium">
                        <th scope="col" className="w-[45%] @[880px]:w-[26%]">
                          Name
                        </th>
                        <th scope="col">Used in</th>
                        <th scope="col" className="hidden @[880px]:table-cell @[880px]:w-[11%]">
                          Factories
                        </th>
                        <th scope="col" className="hidden @[880px]:table-cell @[880px]:w-[13%]">
                          Expires
                        </th>
                        <th scope="col" className="hidden @[880px]:table-cell @[880px]:w-[12%]">
                          Last used
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {shown.map((s) => (
                        <tr
                          key={s.id}
                          className="relative border-t border-line align-middle hover:bg-panel [&>td]:px-4 [&>td]:py-3"
                        >
                          <td className="min-w-0">
                            <Link
                              to="/o/$org/secrets/$secret"
                              params={{ org: org.slug, secret: s.name }}
                              className="flex min-w-0 flex-col text-ink no-underline after:absolute after:inset-0 after:content-['']"
                            >
                              <span title={s.name} className="truncate font-mono text-body font-medium">
                                {s.name}
                              </span>
                              <span title={s.description} className="truncate text-meta text-muted">
                                {s.description}
                              </span>
                            </Link>
                          </td>
                          <td>
                            <UsedIn secret={s} />
                          </td>
                          <td className="hidden @[880px]:table-cell">
                            <FactoryChips scope={s.scope} factories={factories} />
                          </td>
                          <td className="hidden @[880px]:table-cell">
                            <ExpiryText expiry={expiryOf(s, now)} />
                          </td>
                          <td className="hidden whitespace-nowrap text-muted @[880px]:table-cell">
                            {lastUsedText(s, now)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </>
            )}
          </>
        )}
      </div>

      <NewSecretSheet
        org={org.id}
        open={manage && search.new === true}
        onOpenChange={(open) => setSearch({ new: open ? true : undefined })}
        factories={factories}
      />
      <ReplaceValueDialog org={org.id} secret={replacing} onClose={() => setReplacing(null)} />
    </>
  );
}
