// Knowledge (canvas "Knowledge", ADR 0016): the organization's knowledge base. The Sources card
// lists each source with its sync, the factories that read it and its document count; managers
// sync, retry, upload and add from it. Below it, "Look something up" searches every source, and
// "Where it was used" lists the documents of one source. Its runs and agents' verdicts are not
// built yet, so their controls render as NotAvailable. ?factory= (the factory sidebar's Knowledge)
// shows only the sources that factory's current version lists.
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Link, useSearch } from '@tanstack/react-router';
import { BookOpen, Plus } from 'lucide-react';
import { useState } from 'react';
import { factoriesQuery, knowledgeDocumentsQuery, knowledgeSourcesQuery } from '../../api/queries';
import type { KnowledgeSource } from '../../api/types';
import { plural } from '../../shared/format';
import { sourceKinds, sourceSummary } from '../../shared/knowledge';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { EmptyState } from '../../ui/empty-state';
import { NotAvailable } from '../../ui/not-available';
import { StatusText, SyncNowButton, UsedBy, useSyncSource } from './parts';
import { SearchCard } from './search-card';
import { UploadDialog } from './upload';
import { sourceStatus, syncTimes } from './words';

export function KnowledgePage() {
  const org = useOrg();
  const search = useSearch({ from: '/o/$org/_org/knowledge/' });
  const manage = useOrgHolds('knowledge:manage');
  const listed = useQuery(factoriesQuery(org.id));
  const factory = search.factory ? (listed.data ?? []).find((f) => f.name === search.factory) : undefined;
  // A ?factory= the organization has no factory of (a typo, a renamed factory) is said, never unfiltered.
  const missing = search.factory !== undefined && listed.isSuccess && !factory;
  // The search and the usage card read every source; the Sources card is narrowed by ?factory=.
  const everything = useQuery(knowledgeSourcesQuery(org.id));
  const list = useQuery({
    ...knowledgeSourcesQuery(org.id, factory ? { factory: factory.id } : {}),
    enabled: !search.factory || factory !== undefined,
  });
  const sync = useSyncSource(org.id);
  const [uploading, setUploading] = useState<KnowledgeSource | null>(null);
  const now = Date.now();

  const sources = missing ? [] : (list.data ?? []);
  const all = everything.data ?? [];
  const selected = pickSource(sources);

  const addSource = manage ? (
    <Button asChild variant="primary" className="h-11 md:h-10">
      <Link to="/o/$org/knowledge/new" params={{ org: org.slug }}>
        <Plus size={16} strokeWidth={1.5} aria-hidden="true" />
        Add a source
      </Link>
    </Button>
  ) : null;

  return (
    <>
      <header className="flex flex-wrap items-end justify-between gap-x-4 gap-y-3">
        <div className="flex min-w-0 flex-col gap-1">
          <h1 className="m-0 text-page-title tracking-display">Knowledge</h1>
          <p className="m-0 text-body text-ink-soft">
            What agents can look up before they act: synced documents, searched on every run.
          </p>
        </div>
        {addSource}
      </header>

      {list.isPending && !missing ? (
        <div aria-busy="true" className="rounded-card border border-line bg-card">
          <span role="status" className="sr-only">
            Loading knowledge sources
          </span>
        </div>
      ) : list.isError ? (
        <div role="alert" className="flex flex-col items-start gap-3 rounded-card border border-line bg-card p-5">
          <Callout tone="rework">Couldn’t load the knowledge sources. Check your connection and try again.</Callout>
          <Button variant="secondary" onClick={() => void list.refetch()}>
            Try again
          </Button>
        </div>
      ) : sources.length === 0 && !search.factory ? (
        <EmptyState
          icon={<BookOpen size={20} strokeWidth={1.5} />}
          title="No knowledge sources yet"
          actions={addSource}
        >
          {manage
            ? 'Upload the runbooks and docs your agents should read first, or sync a repository’s docs through a connection.'
            : `Owners and admins of ${org.name} add knowledge sources.`}
        </EmptyState>
      ) : (
        <SourcesCard
          sources={sources}
          factory={search.factory}
          missing={missing}
          manage={manage}
          sync={sync}
          onUpload={setUploading}
          now={now}
        />
      )}

      <div className="grid items-start gap-4 @[960px]:grid-cols-2">
        <SearchCard sources={all} />
        <UsageCard source={selected} />
      </div>

      <UploadDialog source={uploading} onClose={() => setUploading(null)} />
    </>
  );
}

/** The source whose documents the usage card lists: the first that failed its sync, else the first with documents. */
function pickSource(sources: KnowledgeSource[]): KnowledgeSource | undefined {
  const stored = sources.filter((s) => s.kind !== 'live');
  return stored.find((s) => s.status === 'failed') ?? stored.find((s) => s.documents > 0) ?? stored[0];
}

function SourcesCard({
  sources,
  factory,
  missing,
  manage,
  sync,
  onUpload,
  now,
}: {
  sources: KnowledgeSource[];
  factory: string | undefined;
  /** No factory of the organization is named `factory`. */
  missing: boolean;
  manage: boolean;
  sync: ReturnType<typeof useSyncSource>;
  onUpload: (s: KnowledgeSource) => void;
  now: number;
}) {
  const org = useOrg();

  return (
    <section aria-labelledby="sources-h" className="flex min-w-0 flex-col rounded-card border border-line bg-card">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-5 pt-4 pb-3">
        <h2 id="sources-h" className="m-0 text-heading">
          Sources
        </h2>
        <Chip>
          <span className="tabular-nums">{sources.length}</span>
        </Chip>
        {factory ? (
          <>
            <span className="text-secondary text-muted">{factory} may search</span>
            <Link
              to="/o/$org/knowledge"
              params={{ org: org.slug }}
              className="ml-auto text-secondary text-ink underline underline-offset-3 hover:text-run"
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
            : `${factory}’s current version lists no knowledge source. Its definition’s Connections and knowledge says which sources its agents may search.`}
        </p>
      ) : (
        <>
          <table className="hidden w-full border-collapse text-body @[960px]:table">
            <caption className="sr-only">Knowledge sources of {org.name}</caption>
            <thead>
              <tr className="bg-panel text-left text-secondary text-muted [&>th]:border-t [&>th]:border-line [&>th]:px-4 [&>th]:py-2 [&>th]:font-control">
                <th scope="col">Source</th>
                <th scope="col">Sync</th>
                <th scope="col">Read by</th>
                <th scope="col" className="w-30 text-right">
                  Documents
                </th>
              </tr>
            </thead>
            <tbody>
              {sources.map((s) => {
                const status = sourceStatus(s);
                const times = syncTimes(s, now);
                const Icon = sourceKinds[s.kind].icon;
                return (
                  <tr key={s.id} className="align-top [&>td]:border-t [&>td]:border-line [&>td]:px-4 [&>td]:py-3">
                    <td className="min-w-0">
                      <span className="flex min-w-0 items-center gap-3">
                        <span
                          aria-hidden="true"
                          className="grid size-9 shrink-0 place-items-center rounded-cell bg-ink text-shell"
                        >
                          <Icon size={18} strokeWidth={1.5} />
                        </span>
                        <span className="flex min-w-0 flex-col">
                          <Link
                            to="/o/$org/knowledge/$source"
                            params={{ org: org.slug, source: s.name }}
                            className="font-control text-ink underline-offset-3 break-words hover:underline"
                          >
                            {s.title}
                          </Link>
                          <span translate="no" className="text-meta break-words text-muted">
                            {sourceSummary(s)}
                          </span>
                        </span>
                      </span>
                    </td>
                    <td className="min-w-0">
                      <span className="flex flex-col items-start gap-1">
                        <StatusText line={status} busy={s.status === 'syncing'} />
                        <span className="text-secondary text-muted">{times.next ?? times.last}</span>
                        {manage && s.kind === 'synced' ? (
                          <SyncNowButton
                            source={s}
                            sync={sync}
                            label={s.status === 'failed' ? 'Retry now' : 'Sync now'}
                            className="mt-1"
                          />
                        ) : null}
                      </span>
                    </td>
                    <td className="min-w-0">
                      <UsedBy source={s} />
                    </td>
                    <td className="text-right">
                      {s.kind === 'live' ? (
                        <span className="text-muted">None stored</span>
                      ) : (
                        <Link
                          to="/o/$org/knowledge/$source"
                          params={{ org: org.slug, source: s.name }}
                          className="tabular-nums text-ink underline underline-offset-3 hover:text-run"
                        >
                          {s.documents}
                          <span className="sr-only"> documents of {s.title}, open the source</span>
                        </Link>
                      )}
                      {manage && s.kind === 'upload' ? (
                        <div className="mt-2 flex justify-end">
                          <UploadButton source={s} onUpload={onUpload} />
                        </div>
                      ) : null}
                    </td>
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
              const status = sourceStatus(s);
              const times = syncTimes(s, now);
              const readers = s.used_by.factories.map((f) => f.name);
              return (
                <li key={s.id} className="flex flex-col gap-2 rounded-card border border-line px-3 py-3">
                  <div className="flex items-baseline justify-between gap-3">
                    <Link
                      to="/o/$org/knowledge/$source"
                      params={{ org: org.slug, source: s.name }}
                      className="min-w-0 font-control text-ink break-words underline-offset-3 hover:underline"
                    >
                      {s.title}
                    </Link>
                    {s.kind === 'live' ? null : (
                      <span className="shrink-0 text-secondary text-muted">{plural(s.documents, 'document')}</span>
                    )}
                  </div>
                  <StatusText line={status} busy={s.status === 'syncing'} className="text-secondary" />
                  <span className="text-secondary text-muted">{times.next ?? times.last}</span>
                  <span className="text-secondary text-muted">
                    Read by {readers.length > 0 ? readers.join(', ') : 'no factory yet'}
                  </span>
                  {manage && s.kind === 'synced' ? (
                    <SyncNowButton
                      source={s}
                      sync={sync}
                      label={s.status === 'failed' ? 'Retry now' : 'Sync now'}
                      className="h-11 self-start"
                    />
                  ) : null}
                  {manage && s.kind === 'upload' ? <UploadButton source={s} onUpload={onUpload} /> : null}
                </li>
              );
            })}
          </ul>
        </>
      )}
    </section>
  );
}

/** Upload… for an upload source; its name says which source the button adds to. */
function UploadButton({ source, onUpload }: { source: KnowledgeSource; onUpload: (s: KnowledgeSource) => void }) {
  return (
    <Button variant="secondary" size="sm" aria-label={`Upload…, ${source.title}`} onClick={() => onUpload(source)}>
      Upload…
    </Button>
  );
}

/**
 * Where it was used: the documents of one source, failures first, with the runs that opened each
 * and the agents' verdict. Those two are not recorded in the API yet, so their controls are
 * NotAvailable and show no count.
 */
function UsageCard({ source }: { source: KnowledgeSource | undefined }) {
  return (
    <section aria-labelledby="usage-h" className="flex min-w-0 flex-col rounded-card border border-line bg-card p-5">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 pb-3">
        <h2 id="usage-h" className="m-0 text-heading">
          Where it was used
        </h2>
        <span className="text-secondary text-muted">Runs that opened each document, last 7 days</span>
      </div>
      {source ? (
        <UsageRows source={source} />
      ) : (
        <p className="m-0 border-t border-line pt-3 text-secondary text-muted">No documents yet.</p>
      )}
    </section>
  );
}

function UsageRows({ source }: { source: KnowledgeSource }) {
  const org = useOrg();
  const pages = useInfiniteQuery(knowledgeDocumentsQuery(org.id, source.name));
  const first = pages.data?.pages[0]?.documents ?? [];
  const shown = [...first].sort((a, b) => Number(b.status === 'failed') - Number(a.status === 'failed')).slice(0, 5);

  if (pages.isPending) {
    return (
      <div aria-busy="true" className="border-t border-line pt-3">
        <span role="status" className="sr-only">
          Loading documents
        </span>
      </div>
    );
  }
  if (pages.isError) {
    return (
      <p role="alert" className="m-0 border-t border-line pt-3 text-secondary text-rework-ink">
        Couldn’t load the documents.
      </p>
    );
  }
  if (shown.length === 0) {
    return <p className="m-0 border-t border-line pt-3 text-secondary text-muted">No documents yet.</p>;
  }
  return (
    <>
      <div className="grid grid-cols-[minmax(0,1fr)_72px_112px] items-center gap-4 border-t border-line py-2 font-control text-secondary text-muted">
        <span>Document</span>
        <span className="text-right">Runs</span>
        <span>Agents’ verdict</span>
      </div>
      <ul className="m-0 flex list-none flex-col p-0">
        {shown.map((d) => (
          <li
            key={d.id}
            className="grid grid-cols-[minmax(0,1fr)_72px_112px] items-center gap-4 border-t border-line py-3 text-body"
          >
            <span className="font-mono text-secondary break-all text-ink">{d.title}</span>
            <NotAvailable label="Runs">
              <button
                type="button"
                aria-label={`Runs of ${d.title}`}
                className="justify-self-end tabular-nums text-muted"
              >
                –
              </button>
            </NotAvailable>
            <NotAvailable label="Agents’ verdict">
              <button type="button" aria-label={`Agents’ verdict on ${d.title}`} className="text-muted">
                –
              </button>
            </NotAvailable>
          </li>
        ))}
      </ul>
      <div className="flex justify-end border-t border-line pt-3">
        <NotAvailable label="Make an item to fix it">
          <Button variant="secondary" size="sm">
            Make an item to fix it
          </Button>
        </NotAvailable>
      </div>
    </>
  );
}
