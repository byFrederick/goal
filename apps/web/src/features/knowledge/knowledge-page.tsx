// Knowledge (ADR 0038 decision 12, canvas "Knowledge" and "Mobile-Knowledge"): the organization's
// sources as folders of files. Each source shows what it says agents should read it for, where it is
// read from, the factories that list it, its sync, its size and its reads of the last 7 days. A
// failing source gets an alert. Find a file looks across every source by path or title; Most read
// lists the files runs opened. ?factory= (the factory sidebar's Knowledge) shows only the sources that
// factory's current version lists.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { BookOpen } from 'lucide-react';
import { useDeferredValue } from 'react';
import { factoriesQuery, knowledgeFindQuery, knowledgeReadsQuery, knowledgeSourcesQuery } from '../../api/queries';
import type { KnowledgeSource } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { formatClock, plural } from '../../shared/format';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { EmptyState } from '../../ui/empty-state';
import { SearchField } from '../../ui/search-field';
import { Skeleton, SkeletonRows } from '../../ui/skeleton';
import { touchLink } from '../../ui/touch-target';
import { fromDetail, mostRead, readsBySource, sizeOf } from './facts';
import { KindLabel, StatusText, UsedBy } from './parts';
import { formatBytes, sourceStatus } from './words';

const card = 'flex min-w-0 flex-col rounded-card border border-line bg-card';

export function KnowledgePage() {
  const org = useOrg();
  const search = useSearch({ from: '/o/$org/_org/knowledge/' });
  const manage = useOrgHolds('knowledge:manage');
  const listed = useQuery(factoriesQuery(org.id));
  const sourcesQ = useQuery(knowledgeSourcesQuery(org.id));
  const readsQ = useQuery(knowledgeReadsQuery(org.id));
  const now = Date.now();

  // A ?factory= the organization has no factory of (a typo, a renamed factory) is said, never unfiltered.
  const missing = Boolean(search.factory) && listed.isSuccess && !listed.data.some((f) => f.name === search.factory);
  const all = sourcesQ.data ?? [];
  const sources = search.factory ? all.filter((s) => s.used_by.factories.some((f) => f.name === search.factory)) : all;
  const reads = readsBySource(readsQ.data ?? []);
  const failing = all.filter((s) => s.status === 'failed');
  const totalBytes = all.reduce((n, s) => n + (s.latest_snapshot?.bytes ?? 0), 0);

  const addSource = manage ? (
    <Button asChild variant="primary" className="h-11 md:h-8.5">
      <Link to="/o/$org/knowledge/new" params={{ org: org.slug }}>
        <span aria-hidden="true">+</span> Add a source
      </Link>
    </Button>
  ) : null;

  return (
    <>
      <TopBarSlot crumb="Knowledge" actions={addSource} />
      <header className="flex flex-col gap-1">
        <h1 className="m-0 text-page-title">Knowledge</h1>
        <p className="m-0 max-w-[680px] text-body text-muted">
          Folders of files agents read before they act. A factory’s definition lists the ones its runs get, read-only,
          under <code className="font-mono">/knowledge</code>.
        </p>
      </header>

      {failing.map((s) => (
        <div key={s.id} role="alert" className="flex flex-wrap items-center gap-3 rounded-card bg-rework-tint p-4">
          <div className="min-w-0 flex-1">
            <Callout tone="rework">
              <span translate="no" className="font-medium">
                {s.name}
              </span>{' '}
              couldn’t sync: {s.last_error ?? 'it stopped before the end'}.
              {s.last_synced_at
                ? ` Runs use its files from ${formatClock(s.last_synced_at, new Date(now))} until it syncs again.`
                : ''}
            </Callout>
          </div>
          <Button asChild variant="white" className="h-11 md:h-8.5">
            <Link to="/o/$org/knowledge/$source" params={{ org: org.slug, source: s.name }}>
              Open source
            </Link>
          </Button>
        </div>
      ))}

      <Skeleton ready={!sourcesQ.isPending} placeholder={<ListPlaceholder />}>
        {sourcesQ.isError ? (
          <div role="alert" className="flex flex-col items-start gap-3 rounded-card border border-line bg-card p-5">
            <Callout tone="rework">Couldn’t load the knowledge sources. Check your connection and try again.</Callout>
            <Button variant="secondary" onClick={() => void sourcesQ.refetch()}>
              Try again
            </Button>
          </div>
        ) : all.length === 0 ? (
          <EmptyState
            icon={<BookOpen size={20} strokeWidth={1.5} />}
            title="No knowledge sources yet"
            actions={addSource}
          >
            {manage
              ? 'Upload the runbooks and docs your agents should read first, or sync a repository or a Confluence space.'
              : `Owners and admins of ${org.name} add knowledge sources.`}
          </EmptyState>
        ) : (
          <SourcesCard
            sources={sources}
            reads={reads}
            factory={search.factory}
            missing={missing}
            totalBytes={totalBytes}
            now={now}
          />
        )}
      </Skeleton>

      {all.length > 0 ? (
        <div className="grid items-start gap-4 @[960px]:grid-cols-2">
          <FindFile />
          <MostRead reads={readsQ.data ?? []} pending={readsQ.isPending} />
        </div>
      ) : null}
    </>
  );
}

function ListPlaceholder() {
  return (
    <div className={cn(card, 'p-5')}>
      <SkeletonRows rows={5} />
    </div>
  );
}

function SourcesCard({
  sources,
  reads,
  factory,
  missing,
  totalBytes,
  now,
}: {
  sources: KnowledgeSource[];
  reads: Map<string, number>;
  factory: string | undefined;
  missing: boolean;
  totalBytes: number;
  now: number;
}) {
  const org = useOrg();

  return (
    <section aria-labelledby="sources-h" className={card}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-5 pt-4 pb-3">
        <h2 id="sources-h" className="m-0 text-heading">
          Sources
        </h2>
        <Chip>
          <span className="tabular-nums">{sources.length}</span>
        </Chip>
        {totalBytes > 0 && !factory ? (
          <span className="text-secondary text-muted">{formatBytes(totalBytes)}</span>
        ) : null}
        {factory ? (
          <>
            <span className="text-secondary text-muted">{factory} reads</span>
            <Link
              to="/o/$org/knowledge"
              params={{ org: org.slug }}
              className={cn(touchLink, 'ml-auto text-secondary text-ink underline underline-offset-3 hover:text-link')}
            >
              Show every source
            </Link>
          </>
        ) : null}
      </div>

      {sources.length === 0 ? (
        <p className="m-0 border-t border-line px-5 py-4 text-body text-muted">
          {missing
            ? `${org.name} has no factory named ${factory}.`
            : `${factory}’s current version lists no knowledge source.`}
        </p>
      ) : (
        <>
          <table className="hidden w-full border-collapse text-body @[960px]:table">
            <caption className="sr-only">Knowledge sources of {org.name}</caption>
            <thead>
              <tr className="bg-panel text-left text-secondary text-muted [&>th]:border-t [&>th]:border-line [&>th]:px-4 [&>th]:py-2 [&>th]:font-control">
                <th scope="col">Source</th>
                <th scope="col">From</th>
                <th scope="col">Factories</th>
                <th scope="col">Sync</th>
                <th scope="col" className="text-right">
                  Size
                </th>
                <th scope="col" className="text-right">
                  Reads, 7 days
                </th>
              </tr>
            </thead>
            <tbody>
              {sources.map((s) => {
                const size = sizeOf(s);
                return (
                  <tr
                    key={s.id}
                    className="animate-row-in align-top [&>td]:border-t [&>td]:border-line [&>td]:px-4 [&>td]:py-3"
                  >
                    <td className="min-w-0">
                      <SourceName s={s} />
                    </td>
                    <td className="min-w-0">
                      <span className="flex flex-col items-start gap-1">
                        <Chip>
                          <KindLabel kind={s.kind} />
                        </Chip>
                        <span className="text-secondary break-words text-muted">{fromDetail(s)}</span>
                      </span>
                    </td>
                    <td className="min-w-0">
                      <UsedBy source={s} />
                    </td>
                    <td className="min-w-0">
                      <SyncCell s={s} now={now} />
                    </td>
                    <td className="text-right tabular-nums">
                      {size ? (
                        <span className="flex flex-col">
                          <span>{size.files}</span>
                          <span className="text-secondary text-muted">{size.bytes}</span>
                        </span>
                      ) : (
                        <span className="text-muted">None yet</span>
                      )}
                    </td>
                    <td className="text-right tabular-nums">{reads.get(s.name) ?? 0}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>

          <ul
            aria-label={`Knowledge sources of ${org.name}`}
            className="m-0 flex list-none flex-col gap-3 border-t border-line p-4 @[960px]:hidden"
          >
            {sources.map((s) => {
              const size = sizeOf(s);
              return (
                <li key={s.id} className="flex animate-row-in flex-col gap-2 rounded-card border border-line px-3 py-3">
                  <SourceName s={s} />
                  <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-secondary text-muted">
                    <Chip>
                      <KindLabel kind={s.kind} />
                    </Chip>
                    <span className="break-words">{fromDetail(s)}</span>
                  </span>
                  <SyncCell s={s} now={now} />
                  <span className="flex flex-wrap items-center gap-x-3 text-secondary text-muted">
                    <span>{size ? `${size.files}, ${size.bytes}` : 'None yet'}</span>
                    <span className="tabular-nums">{plural(reads.get(s.name) ?? 0, 'read')}, 7 days</span>
                  </span>
                  <span className="flex flex-wrap items-center gap-x-2 text-secondary text-muted">
                    Read by <UsedBy source={s} />
                  </span>
                </li>
              );
            })}
          </ul>
        </>
      )}
    </section>
  );
}

function SourceName({ s }: { s: KnowledgeSource }) {
  const org = useOrg();
  return (
    <span className="flex min-w-0 flex-col">
      <Link
        to="/o/$org/knowledge/$source"
        params={{ org: org.slug, source: s.name }}
        translate="no"
        className={cn(touchLink, 'font-mono text-body text-ink break-words underline-offset-3 hover:underline')}
      >
        {s.name}
      </Link>
      {s.read_when ? <span className="text-secondary break-words text-muted">{s.read_when}</span> : null}
    </span>
  );
}

function SyncCell({ s, now }: { s: KnowledgeSource; now: number }) {
  const at =
    s.status === 'syncing' || !s.last_synced_at || s.kind === 'live'
      ? undefined
      : formatClock(s.last_synced_at, new Date(now));
  return <StatusText line={sourceStatus(s)} time={at} busy={s.status === 'syncing'} />;
}

/** Find a file: files whose path or title starts with the query, across every source; the query is in the URL (?q=). */
function FindFile() {
  const org = useOrg();
  const search = useSearch({ from: '/o/$org/_org/knowledge/' });
  const navigate = useNavigate({ from: '/o/$org/knowledge/' });
  const q = search.q ?? '';
  const asked = useDeferredValue(q.trim());
  const found = useQuery({ ...knowledgeFindQuery(org.id, asked), enabled: asked !== '' });

  return (
    <section aria-labelledby="find-h" className={card}>
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 px-5 py-3">
        <h2 id="find-h" className="m-0 text-heading">
          Find a file
        </h2>
        <span className="text-secondary text-muted">By path or title, across every source</span>
      </div>
      <div className="flex flex-col gap-3 border-t border-line p-4">
        <SearchField
          value={q}
          onChange={(v) =>
            void navigate({ search: (prev) => ({ ...prev, q: v === '' ? undefined : v }), replace: true })
          }
          label="Find a file"
        />
        {asked === '' ? null : found.isError ? (
          <Callout tone="rework">Couldn’t search the files. Try again.</Callout>
        ) : found.data && found.data.length === 0 ? (
          <p className="m-0 text-body text-muted">No file matches “{asked}”.</p>
        ) : (
          <ul aria-label="Files found" className="m-0 flex list-none flex-col p-0">
            {(found.data ?? []).map((f) => (
              <li key={`${f.source}/${f.path}`} className="animate-row-in border-t border-line first:border-t-0">
                <Link
                  to="/o/$org/knowledge/$source"
                  params={{ org: org.slug, source: f.source }}
                  search={{ find: f.path }}
                  className={cn(
                    touchLink,
                    'flex min-w-0 items-start justify-between gap-3 py-2 text-ink hover:text-link',
                  )}
                >
                  <span className="flex min-w-0 flex-col">
                    <span translate="no" className="font-mono text-body break-all">
                      {f.source}/{f.path}
                    </span>
                    <span className="text-secondary break-words text-muted">{f.title}</span>
                  </span>
                  <span className="shrink-0 text-secondary text-muted tabular-nums">{formatBytes(f.bytes)}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}

function MostRead({ reads, pending }: { reads: Parameters<typeof mostRead>[0]; pending: boolean }) {
  const org = useOrg();
  const rows = mostRead(reads);

  return (
    <section aria-labelledby="most-h" className={card}>
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 px-5 py-3">
        <h2 id="most-h" className="m-0 text-heading">
          Most read
        </h2>
        <span className="text-secondary text-muted">Files runs opened, last 7 days</span>
      </div>
      <Skeleton
        ready={!pending}
        placeholder={
          <div className="border-t border-line p-4">
            <SkeletonRows rows={5} />
          </div>
        }
      >
        {rows.length === 0 ? (
          <p className="m-0 border-t border-line px-5 py-4 text-body text-muted">No run has opened a file yet.</p>
        ) : (
          <table className="w-full border-collapse text-body">
            <caption className="sr-only">Most read files</caption>
            <thead>
              <tr className="text-left text-secondary text-muted [&>th]:border-t [&>th]:border-line [&>th]:px-5 [&>th]:py-2 [&>th]:font-control">
                <th scope="col">File</th>
                <th scope="col" className="text-right">
                  Runs
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr
                  key={`${r.source}/${r.path}`}
                  className="animate-row-in [&>td]:border-t [&>td]:border-line [&>td]:px-5 [&>td]:py-3"
                >
                  <td className="min-w-0">
                    <Link
                      to="/o/$org/knowledge/$source"
                      params={{ org: org.slug, source: r.source }}
                      search={{ find: r.path }}
                      translate="no"
                      className={cn(touchLink, 'font-mono text-ink break-all underline-offset-3 hover:underline')}
                    >
                      {r.source}/{r.path}
                    </Link>
                  </td>
                  <td className="text-right tabular-nums">{r.reads}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Skeleton>
    </section>
  );
}
