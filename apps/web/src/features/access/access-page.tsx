// Access (ADR 0037 decision 14, canvas docs/design/screens/connections/access.jpg, access-768.jpg and
// mobile/access.jpg): what every factory's agents can reach, read from their environment files. It is
// read only: a row opens the environment in the factory's definition, where it changes. The view
// (?by=reach, factory or secret) and the search text live in the URL. From 1000 px up the rows are a
// matrix with a column per factory and environment; below it, a list with those as chips.
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { KeyRound, Search, TriangleAlert } from 'lucide-react';
import { type ReactNode, useDeferredValue, useRef, useState } from 'react';
import { accessQuery, inboxQuery, secretsQuery } from '../../api/queries';
import type { Access, AccessGroup, AccessRow, AccessUse, AccessView } from '../../api/types';
import { usePhone } from '../../app/phone-chrome';
import { TopBarSlot } from '../../app/top-bar';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { EmptyState } from '../../ui/empty-state';
import { SegmentedControl } from '../../ui/segmented-control';
import { Skeleton } from '../../ui/skeleton';
import { type AccessSearch, accessSearch } from './search';
import {
  attentionDetail,
  daysUntil,
  environmentFile,
  placeLabel,
  places,
  reachedDetail,
  readsAs,
  rowKindLabels,
  rowMatches,
  soonestExpiring,
  statusOf,
  uniqueRows,
} from './words';

const tone = { ok: 'bg-ok', bad: 'bg-rework', muted: 'bg-muted' } as const;

/** One row's status: a dot and its words, never colour alone. */
function Status({ row, now }: { row: AccessRow; now: number }) {
  const status = statusOf(row, now);
  return (
    <span
      className={cn(
        'inline-flex items-start gap-2 text-secondary',
        status.tone === 'bad' ? 'text-rework-ink' : 'text-ink-soft',
      )}
    >
      <span aria-hidden="true" className={cn('mt-2 size-1.5 shrink-0 rounded-full', tone[status.tone])} />
      <span className="min-w-0 break-words">{status.text}</span>
    </span>
  );
}

/** The secrets a row references, as chips with a key; "No key" for none. */
function Keys({ row }: { row: AccessRow }) {
  if (row.secrets.length === 0) return <span className="text-secondary text-muted">No key</span>;
  return (
    <span className="inline-flex min-w-0 flex-wrap gap-1">
      {row.secrets.map((name) => (
        <span
          key={name}
          translate="no"
          className="inline-flex h-5.5 max-w-full items-center gap-1 rounded-chip bg-run-tint px-2 font-mono text-meta text-run-ink"
        >
          <KeyRound size={12} strokeWidth={1.75} aria-hidden="true" className="shrink-0" />
          <span title={name} className="min-w-0 truncate">
            {name}
          </span>
        </span>
      ))}
    </span>
  );
}

/** A link to one environment's file in the factory's definition. */
function EnvLink({
  use,
  children,
  className,
  label,
}: {
  use: AccessUse;
  children: ReactNode;
  className?: string;
  label?: string;
}) {
  const org = useOrg();
  return (
    <Link
      to="/o/$org/f/$factory/definition/$section"
      params={{ org: org.slug, factory: use.factory.name, section: 'basics' }}
      search={{ mode: 'yaml', file: environmentFile(use) }}
      aria-label={label}
      className={className}
    >
      {children}
    </Link>
  );
}

function Stat({
  label,
  short,
  value,
  children,
  danger,
}: {
  label: string;
  short: string;
  value: number;
  children: ReactNode;
  danger?: boolean;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1 border-t border-line px-0 py-4 first:border-t-0 min-[1000px]:border-t-0 min-[1000px]:border-l min-[1000px]:px-5 min-[1000px]:first:border-l-0 min-[1000px]:first:pl-0 max-md:border-t-0 max-md:border-l max-md:px-3 max-md:py-3 max-md:first:border-l-0 max-md:first:pl-0">
      <dt className="text-secondary text-muted">
        <span className="md:hidden">{short}</span>
        <span className="max-md:hidden">{label}</span>
      </dt>
      <dd className="m-0 flex flex-col gap-1">
        <span
          className={cn(
            'text-page-title leading-tight font-medium tabular-nums',
            danger ? 'text-rework-ink' : 'text-ink',
          )}
        >
          {value}
        </span>
        {children}
      </dd>
    </div>
  );
}

function StatStrip({ access, waiting, orgSlug }: { access: Access; waiting: number; orgSlug: string }) {
  return (
    <dl className="m-0 grid grid-cols-1 border-y border-line max-md:grid-cols-3 min-[1000px]:grid-cols-3">
      <Stat label="Reached" short="Reached" value={access.reached}>
        <span className="text-secondary text-muted max-md:hidden">{reachedDetail(access)}</span>
      </Stat>
      <Stat label="Need attention" short="Attention" value={access.need_attention} danger={access.need_attention > 0}>
        <span className="text-secondary text-muted max-md:hidden">{attentionDetail(access)}</span>
      </Stat>
      <Stat label="Waiting for your OK" short="Your OK" value={waiting}>
        <Link
          to="/o/$org/needs-you"
          params={{ org: orgSlug }}
          className="text-secondary text-ink underline underline-offset-2 max-md:hidden"
        >
          Review in Needs you
        </Link>
      </Stat>
    </dl>
  );
}

function ExpiryAlert({ access, now }: { access: Access; now: number }) {
  const org = useOrg();
  const soonest = soonestExpiring(access);
  const secrets = useQuery(secretsQuery(org.id)).data;
  if (!soonest) return null;
  const { row, use } = soonest;
  // The row carries the soonest expiry of its secrets, not which one: the secret with that date is it.
  const name =
    row.secrets.find((n) => secrets?.find((s) => s.name === n)?.expires_at === row.expires_at) ?? row.secrets[0];
  const days = row.expires_at ? daysUntil(row.expires_at, now) : 0;
  const when =
    days < 0 ? 'expired' : days === 0 ? 'expires today' : `expires in ${days} ${days === 1 ? 'day' : 'days'}`;
  return (
    <section
      aria-label="Expiring"
      className="flex flex-col gap-3 rounded-card bg-rework-tint p-4 min-[1000px]:flex-row min-[1000px]:items-center"
    >
      <TriangleAlert
        size={16}
        strokeWidth={1.5}
        aria-hidden="true"
        className="hidden shrink-0 text-rework-ink min-[1000px]:block"
      />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="m-0 text-body font-semibold break-words text-ink">{name ? `${name} ${when}` : `A key ${when}`}</p>
        <p className="m-0 text-secondary break-words text-ink-soft">
          {placeLabel(use)} {readsAs(row)}.
        </p>
      </div>
      {name ? (
        <Button asChild variant="white" className="h-11 max-md:w-full md:h-9">
          <Link to="/o/$org/secrets/$secret" params={{ org: org.slug, secret: name }}>
            Replace value
          </Link>
        </Button>
      ) : null}
    </section>
  );
}

/** The matrix: a column per environment, a dot where the row is used and an en dash elsewhere. */
function Matrix({ groups, now }: { groups: readonly AccessGroup[]; now: number }) {
  const org = useOrg();
  const columns = places(groups.flatMap((g) => g.rows));
  const span = columns.length + 3;
  return (
    <div className="overflow-x-auto rounded-card border border-line bg-card max-[999px]:hidden">
      <table className="w-full border-collapse text-secondary">
        <caption className="sr-only">What the agents of {org.name} reach</caption>
        <thead>
          <tr className="text-left text-muted [&>th]:px-4 [&>th]:py-3 [&>th]:font-medium">
            <th scope="col" className="min-w-52">
              What agents reach
            </th>
            {columns.map((c) => (
              <th key={placeLabel(c)} scope="col" className="w-24 text-center leading-snug">
                <span className="block">{c.factory.name}</span>
                <span className="block">{c.environment}</span>
              </th>
            ))}
            <th scope="col">Key</th>
            <th scope="col" className="min-w-40">
              Status
            </th>
          </tr>
        </thead>
        {groups.map((g) => (
          <tbody key={g.key}>
            <tr className="border-t border-line bg-panel">
              <th scope="colgroup" colSpan={span} className="px-4 py-3 text-left font-medium text-ink-soft">
                {g.title}
              </th>
            </tr>
            {g.rows.map((row) => {
              const first = row.uses[0];
              return (
                <tr
                  key={`${row.kind}:${row.name}`}
                  className="border-t border-line align-middle hover:bg-panel [&>td]:px-4 [&>td]:py-3"
                >
                  <td className="min-w-0">
                    <span className="flex min-w-0 flex-col">
                      {first ? (
                        <EnvLink
                          use={first}
                          className="font-mono text-body font-medium break-all text-ink no-underline hover:underline"
                        >
                          {row.name}
                        </EnvLink>
                      ) : (
                        <span className="font-mono text-body font-medium break-all">{row.name}</span>
                      )}
                      <span className="text-meta text-muted">{rowKindLabels[row.kind]}</span>
                    </span>
                  </td>
                  {columns.map((c) => {
                    const use = row.uses.find((u) => placeLabel(u) === placeLabel(c));
                    return (
                      <td key={placeLabel(c)} className="text-center">
                        {use ? (
                          <EnvLink
                            use={use}
                            label={`${row.name} in ${placeLabel(use)}`}
                            className="inline-flex size-6 items-center justify-center"
                          >
                            <span aria-hidden="true" className="size-2 rounded-full bg-ink" />
                          </EnvLink>
                        ) : (
                          <span aria-hidden="true" className="text-muted">
                            –
                          </span>
                        )}
                      </td>
                    );
                  })}
                  <td>
                    <Keys row={row} />
                  </td>
                  <td>
                    <Status row={row} now={now} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        ))}
      </table>
    </div>
  );
}

/** Below 1000 px: a row each, with the environments it is used in as chips. */
function RowList({ groups, now }: { groups: readonly AccessGroup[]; now: number }) {
  return (
    <div className="overflow-hidden rounded-card border border-line bg-card min-[1000px]:hidden">
      {groups.map((g) => (
        <section key={g.key} aria-label={g.title}>
          <h2 className="m-0 border-t border-line bg-panel px-4 py-3 text-secondary font-medium text-ink-soft first:border-t-0">
            {g.title}
          </h2>
          <ul className="m-0 flex list-none flex-col p-0">
            {g.rows.map((row) => {
              const first = row.uses[0];
              return (
                <li key={`${row.kind}:${row.name}`} className="flex flex-col gap-2 border-t border-line p-4">
                  <span className="flex items-start justify-between gap-3">
                    {first ? (
                      <EnvLink
                        use={first}
                        className="-my-2 flex min-h-11 min-w-0 items-center font-mono text-body font-semibold break-all text-ink no-underline"
                      >
                        {row.name}
                      </EnvLink>
                    ) : (
                      <span className="font-mono text-body font-semibold break-all">{row.name}</span>
                    )}
                    <Status row={row} now={now} />
                  </span>
                  <span className="flex flex-wrap items-center gap-x-2 gap-y-1 text-secondary text-muted">
                    <span>{rowKindLabels[row.kind]}</span>
                    <Keys row={row} />
                  </span>
                  <ul className="m-0 flex list-none flex-wrap gap-2 p-0">
                    {row.uses.map((u) => (
                      <li key={placeLabel(u)}>
                        <EnvLink
                          use={u}
                          className="press inline-flex h-6 items-center rounded-chip border border-line bg-panel px-2 text-meta text-ink-soft no-underline hover:border-rule max-md:h-11 max-md:px-3"
                        >
                          {placeLabel(u)}
                        </EnvLink>
                      </li>
                    ))}
                  </ul>
                </li>
              );
            })}
          </ul>
        </section>
      ))}
    </div>
  );
}

function AccessSkeleton() {
  const block = 'rounded-control bg-hover';
  return (
    <div className="flex flex-col gap-5">
      <div className={cn(block, 'h-24')} />
      <div className={cn(block, 'h-9 w-72 max-w-full')} />
      <div className="flex flex-col gap-3 rounded-card border border-line bg-card p-5">
        <div className={cn(block, 'h-8 w-64 max-w-full')} />
        <div className={cn(block, 'h-12')} />
        <div className={cn(block, 'h-12')} />
        <div className={cn(block, 'h-12')} />
      </div>
    </div>
  );
}

export function AccessPage() {
  const org = useOrg();
  const search = accessSearch(useSearch({ strict: false }));
  const navigate = useNavigate({ from: '/o/$org/access' });
  const phone = usePhone();
  const by: AccessView = search.by ?? 'reach';
  const access = useQuery({ ...accessQuery(org.id, by), placeholderData: keepPreviousData });
  const waiting = (useQuery(inboxQuery(org.id)).data?.entries ?? []).filter((e) => e.tool_approval).length;
  const [text, setText] = useState(search.q ?? '');
  const q = useDeferredValue(text.trim().toLowerCase());
  const keyboard = useRef(false);
  const now = Date.now();

  const setSearch = (patch: Partial<AccessSearch>) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true });

  const data = access.data;
  const groups = (data?.groups ?? [])
    .map((g) => ({ ...g, rows: q ? g.rows.filter((r) => rowMatches(r, q)) : g.rows }))
    .filter((g) => g.rows.length > 0);
  const empty = data !== undefined && uniqueRows(data).length === 0;

  const searchField = (
    <label className="relative flex h-11 w-40 items-center md:h-8.5 md:w-55">
      <span className="sr-only">Search tools and keys</span>
      <Search
        size={15}
        strokeWidth={1.5}
        aria-hidden="true"
        className="pointer-events-none absolute left-3 text-muted"
      />
      <input
        type="search"
        value={text}
        placeholder="Search tools and keys"
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
      <TopBarSlot actions={<span className="max-md:hidden">{searchField}</span>} />
      <div className="@container flex min-w-0 flex-col gap-5">
        <div className="flex min-w-0 flex-col gap-1 md:gap-2">
          <h1 className="m-0 text-page-title tracking-display max-md:sr-only">Access</h1>
          <p className="m-0 max-w-2xl text-body text-ink-soft">
            <span className="md:hidden">What every factory’s agents can reach, from their environment files.</span>
            <span className="max-md:hidden">
              What every factory’s agents can reach, read from their environment files. To change it, open the
              environment in the factory’s definition.
            </span>
          </p>
        </div>

        {access.isPending ? (
          <Skeleton ready={false} placeholder={<AccessSkeleton />}>
            <span />
          </Skeleton>
        ) : access.isError || !data ? (
          <div
            className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-5"
            role="alert"
          >
            <Callout tone="rework">Couldn’t load access. Check your connection and try again.</Callout>
            <Button variant="white" onClick={() => void access.refetch()}>
              Try again
            </Button>
          </div>
        ) : empty ? (
          <EmptyState title="No agent reaches a tool or a key yet">
            Add an MCP server, a variable or a file to an environment in a factory’s definition, and it shows here.
          </EmptyState>
        ) : (
          <>
            <StatStrip access={data} waiting={waiting} orgSlug={org.slug} />
            <ExpiryAlert access={data} now={now} />
            <div
              className="flex"
              onKeyDownCapture={() => {
                keyboard.current = true;
              }}
              onPointerDownCapture={() => {
                keyboard.current = false;
              }}
            >
              <SegmentedControl<AccessView>
                label="View"
                value={by}
                onChange={(value) => setSearch({ by: value === 'reach' ? undefined : value })}
                options={[
                  { value: 'reach', label: phone ? 'What they reach' : 'By what they reach' },
                  { value: 'factory', label: phone ? 'Factory' : 'By factory' },
                  { value: 'secret', label: phone ? 'Secret' : 'By secret' },
                ]}
              />
            </div>
            <div key={by} className={cn('min-w-0', !keyboard.current && 'animate-fade-in motion-reduce:animate-none')}>
              {groups.length === 0 ? (
                <div className="flex flex-col items-start gap-2 rounded-card border border-line bg-card p-4">
                  <p className="m-0 text-body text-muted">Nothing matches this search.</p>
                  <Button
                    variant="white"
                    onClick={() => {
                      setText('');
                      setSearch({ q: undefined });
                    }}
                  >
                    Show everything
                  </Button>
                </div>
              ) : (
                <>
                  <Matrix groups={groups} now={now} />
                  <RowList groups={groups} now={now} />
                </>
              )}
            </div>
          </>
        )}
      </div>
    </>
  );
}
