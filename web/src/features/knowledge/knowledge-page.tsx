// Knowledge (spec 4 §10, canvas "Knowledge" and "Mobile-Knowledge"): the organization's knowledge
// base. Its sources, each with its kind, live status (the stream's `event: knowledge` re-reads
// them), document count, last and next sync and the factories that may search it: a table when
// `main` is 960 px wide, cards below. Owners and admins (knowledge:manage) get New source, Sync
// now and Upload…. Beside the table, the documents of the source ?source= picks (failures first)
// and how factories find what; below it, Try a search. ?factory= (the factory sidebar's
// Knowledge) shows only the sources that factory's current version lists.
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Link, useSearch } from '@tanstack/react-router';
import { BookOpen, Code, RefreshCw, Zap } from 'lucide-react';
import { useState } from 'react';
import { factoriesQuery, knowledgeDocumentsQuery, knowledgeSourcesQuery } from '../../api/queries';
import type { KnowledgeSource } from '../../api/types';
import { plural } from '../../shared/format';
import { sourceSummary } from '../../shared/knowledge';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { EmptyState } from '../../ui/empty-state';
import { descriptionClass, eyebrowClass, PageHeader } from '../../ui/page-header';
import { DocumentLine } from './documents';
import { KindLabel, StatusText, SyncNowButton, UsedBy, useSyncSource } from './parts';
import { SearchCard } from './search-card';
import { UploadDialog } from './upload';
import { sourceStatus, syncTimes } from './words';

export function KnowledgePage() {
  const org = useOrg();
  const search = useSearch({ from: '/o/$org/_org/knowledge/' });
  const manage = useOrgHolds('knowledge:manage');
  const listed = useQuery(factoriesQuery(org.id));
  const factories = listed.data ?? [];
  const factory = search.factory ? factories.find((f) => f.name === search.factory) : undefined;
  // A ?factory= the organization has no factory of (a typo, a renamed factory) is said, never unfiltered.
  const missing = search.factory !== undefined && listed.isSuccess && !factory;
  // The search tries every source, and "as a factory" those it lists: it reads the whole list.
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
  const selected = pickSource(sources, search.source);

  const newSource = manage ? (
    <Button asChild variant="primary" className="h-11 md:h-10">
      <Link to="/o/$org/knowledge/new" params={{ org: org.slug }}>
        <span aria-hidden="true">+</span> New source
      </Link>
    </Button>
  ) : null;

  return (
    <>
      <PageHeader
        eyebrow={<p className={cn(eyebrowClass, 'md:hidden')}>{org.name}</p>}
        title="Knowledge"
        description={
          <>
            <p className={cn(descriptionClass, 'max-w-[80ch] md:hidden')}>
              {org.name}’s knowledge base. Each factory’s definition lists the sources its agents search.
            </p>
            <p className={cn(descriptionClass, 'hidden max-w-[80ch] md:block')}>
              {org.name}’s knowledge base. Agents search it before they ask a person or guess, and get snippets with
              links.
            </p>
          </>
        }
        actions={newSource}
      />

      {list.isPending && !missing ? (
        <div aria-busy="true" className="rounded-card border border-line bg-card p-5">
          <span role="status" className="sr-only">
            Loading knowledge sources
          </span>
          <div className="flex flex-col gap-4">
            {[0, 1, 2].map((i) => (
              <div key={i} className="grid grid-cols-[1fr_1fr_1fr] items-center gap-4">
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
          <Callout tone="rework">Couldn’t load the knowledge sources. Check your connection and try again.</Callout>
          <Button variant="white" onClick={() => void list.refetch()}>
            Try again
          </Button>
        </div>
      ) : sources.length === 0 && !search.factory ? (
        <EmptyState
          icon={<BookOpen size={20} strokeWidth={1.5} />}
          title="No knowledge sources yet"
          actions={newSource}
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
          selected={selected?.name}
          manage={manage}
          sync={sync}
          onUpload={setUploading}
          now={now}
        />
      )}

      <div className="grid items-start gap-4 @[960px]:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
        <SearchCard sources={all} loading={everything.isPending} factories={factories} />
        <div className="flex min-w-0 flex-col gap-4">
          {selected ? <DocumentsCard source={selected} now={now} /> : null}
          <WhoFindsWhat />
        </div>
      </div>

      <UploadDialog source={uploading} onClose={() => setUploading(null)} />
    </>
  );
}

/**
 * The source whose documents show beside the list: the one ?source= names, else the first that
 * failed its sync, else the first with documents of its own (a live source has none).
 */
function pickSource(sources: KnowledgeSource[], named: string | undefined): KnowledgeSource | undefined {
  const stored = sources.filter((s) => s.kind !== 'live');
  return (
    stored.find((s) => s.name === named) ??
    stored.find((s) => s.status === 'failed') ??
    stored.find((s) => s.documents > 0) ??
    stored[0]
  );
}

function SourcesCard({
  sources,
  factory,
  missing,
  selected,
  manage,
  sync,
  onUpload,
  now,
}: {
  sources: KnowledgeSource[];
  factory: string | undefined;
  /** No factory of the organization is named `factory`. */
  missing: boolean;
  selected: string | undefined;
  manage: boolean;
  sync: ReturnType<typeof useSyncSource>;
  onUpload: (s: KnowledgeSource) => void;
  now: number;
}) {
  const org = useOrg();
  const action = (s: KnowledgeSource, className: string) =>
    !manage ? null : s.kind === 'synced' ? (
      <SyncNowButton source={s} sync={sync} className={className} />
    ) : s.kind === 'upload' ? (
      <Button variant="white" className={className} aria-label={`Upload…, ${s.title}`} onClick={() => onUpload(s)}>
        Upload…
      </Button>
    ) : null;

  return (
    <section
      aria-labelledby="sources-count"
      className="@[960px]:rounded-card @[960px]:border @[960px]:border-line @[960px]:bg-card @[960px]:p-5"
    >
      <div className="mb-3 flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h2 id="sources-count" className="m-0 text-lg font-medium">
          {plural(sources.length, 'source')}
          {factory ? <span className="text-muted"> {factory} may search</span> : null}
        </h2>
        {factory ? (
          <Link
            to="/o/$org/knowledge"
            params={{ org: org.slug }}
            className="text-sm text-ink underline underline-offset-3 hover:text-run"
          >
            Show every source
          </Link>
        ) : (
          <span className="hidden text-sm text-muted @[640px]:inline">Owners and admins add and change sources.</span>
        )}
      </div>

      {sources.length === 0 ? (
        <p className="m-0 border-t border-line pt-4 text-[15px] text-muted">
          {missing
            ? `${org.name} has no factory named ${factory}.`
            : `${factory}’s current version lists no knowledge source. Its definition’s Connections and knowledge says which sources its agents may search.`}
        </p>
      ) : (
        <>
          <table className="hidden w-full border-collapse text-sm @[960px]:table">
            <caption className="sr-only">Knowledge sources of {org.name}</caption>
            <thead>
              <tr className="text-left text-[13px] text-muted [&>th]:pb-2 [&>th]:font-normal">
                <th scope="col">Source</th>
                <th scope="col">Kind</th>
                <th scope="col">Status</th>
                <th scope="col">Documents</th>
                <th scope="col">Last sync</th>
                <th scope="col">Used by</th>
                <th scope="col">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {sources.map((s) => {
                const status = sourceStatus(s);
                const times = syncTimes(s, now);
                return (
                  <tr
                    key={s.id}
                    className={cn(
                      'border-t border-line align-middle [&>td]:py-2.5 [&>td]:pr-3',
                      s.name === selected ? 'bg-surface' : status.tone === 'rework' ? 'bg-rework-tint/40' : undefined,
                    )}
                  >
                    <td className="max-w-[240px]">
                      <span className="flex min-w-0 flex-col">
                        <Link
                          to="/o/$org/knowledge/$source"
                          params={{ org: org.slug, source: s.name }}
                          className="text-[15px] font-medium text-ink underline-offset-3 hover:underline"
                        >
                          {s.title}
                        </Link>
                        <span translate="no" className="text-[13px] break-words text-muted">
                          {sourceSummary(s)}
                        </span>
                      </span>
                    </td>
                    <td>
                      <KindLabel kind={s.kind} />
                    </td>
                    <td className="max-w-[240px]">
                      <StatusText line={status} busy={s.status === 'syncing'} />
                    </td>
                    <td className="tabular-nums">
                      {s.kind === 'live' ? (
                        <span className="text-muted">None stored</span>
                      ) : (
                        <Link
                          to="/o/$org/knowledge"
                          params={{ org: org.slug }}
                          search={(prev) => ({ ...prev, source: s.name })}
                          replace
                          resetScroll={false}
                          aria-current={s.name === selected ? 'true' : undefined}
                          className="text-ink underline underline-offset-3 hover:text-run"
                        >
                          {s.documents}
                          <span className="sr-only"> documents of {s.title}, show them</span>
                        </Link>
                      )}
                    </td>
                    <td className="whitespace-nowrap">
                      <span className="flex flex-col">
                        <span>{times.last}</span>
                        {times.next ? <span className="text-[13px] text-muted">{times.next}</span> : null}
                      </span>
                    </td>
                    <td className="max-w-[180px]">
                      <UsedBy source={s} />
                    </td>
                    <td className="text-right whitespace-nowrap">{action(s, 'h-8 px-2.5 text-[13px]')}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>

          <ul
            aria-label={`Knowledge sources of ${org.name}`}
            className="m-0 flex list-none flex-col gap-3 p-0 @[960px]:hidden"
          >
            {sources.map((s) => {
              const status = sourceStatus(s);
              const users = s.used_by.factories.map((f) => f.name);
              return (
                <li key={s.id} className="flex flex-col gap-1.5 rounded-card border border-line bg-card px-3.5 py-3">
                  <div className="flex items-baseline justify-between gap-3">
                    <Link
                      to="/o/$org/knowledge/$source"
                      params={{ org: org.slug, source: s.name }}
                      className="min-h-6 min-w-0 text-[15px] font-medium break-words text-ink underline-offset-3 hover:underline"
                    >
                      {s.title}
                    </Link>
                    <KindLabel kind={s.kind} className="shrink-0 text-[13px] text-muted" />
                  </div>
                  <StatusText
                    line={{ tone: status.tone, text: status.text }}
                    busy={s.status === 'syncing'}
                    className="text-sm"
                  />
                  <p className="m-0 text-[13px] text-muted">
                    {users.length > 0 ? `Used by ${users.join(', ')}` : 'No factory lists it yet'}
                    {s.kind === 'live' ? '' : `, ${plural(s.documents, 'document')}`}
                  </p>
                  {s.kind === 'synced' && manage ? <div className="mt-1">{action(s, 'h-11')}</div> : null}
                </li>
              );
            })}
          </ul>
        </>
      )}
    </section>
  );
}

/** The selected source's documents: the first page, failures first, and a link to them all. */
function DocumentsCard({ source, now }: { source: KnowledgeSource; now: number }) {
  const org = useOrg();
  const pages = useInfiniteQuery(knowledgeDocumentsQuery(org.id, source.name));
  const first = pages.data?.pages[0]?.documents ?? [];
  const shown = [...first].sort((a, b) => Number(b.status === 'failed') - Number(a.status === 'failed')).slice(0, 5);
  return (
    <section
      aria-labelledby="documents-of"
      className="hidden min-w-0 flex-col gap-1.5 rounded-card border border-line bg-card p-5 @[960px]:flex"
    >
      <div className="flex items-baseline justify-between gap-3">
        <h2 id="documents-of" className="m-0 text-lg font-medium">
          {source.title}
        </h2>
        <span className="shrink-0 text-[13px] text-muted">{plural(source.documents, 'document')}</span>
      </div>
      <p className="m-0 mb-1 text-sm text-muted">Documents of the selected source, with failure reasons.</p>
      {pages.isPending ? (
        <p aria-busy="true" className="m-0 text-sm text-muted">
          Loading documents…
        </p>
      ) : pages.isError ? (
        <p role="alert" className="m-0 text-sm text-rework-ink">
          Couldn’t load the documents.
        </p>
      ) : shown.length === 0 ? (
        <p className="m-0 border-t border-line pt-2.5 text-sm text-muted">No documents yet.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {shown.map((d) => (
            <li key={d.id} className="border-t border-line py-2.5">
              <DocumentLine source={source.name} document={d} now={now} />
            </li>
          ))}
        </ul>
      )}
      <Link
        to="/o/$org/knowledge/$source"
        params={{ org: org.slug, source: source.name }}
        className="mt-1 self-start text-sm text-ink underline underline-offset-3 hover:text-run"
      >
        {source.documents > shown.length ? `All ${plural(source.documents, 'document')}` : `Open ${source.title}`}
      </Link>
    </section>
  );
}

/** Who finds what (canvas): how factories reach each kind of source. */
function WhoFindsWhat() {
  const rows = [
    {
      icon: Code,
      text: (
        <>
          A factory’s definition lists the sources it may search. Agents find them with{' '}
          <code translate="no" className="font-mono text-[13px]">
            knowledge__search
          </code>{' '}
          and read an upload whole with{' '}
          <code translate="no" className="font-mono text-[13px]">
            knowledge__read
          </code>
          .
        </>
      ),
    },
    {
      icon: RefreshCw,
      text: 'Synced sources are read with the connection’s own access, so every factory that lists one sees everything that access sees. Page restrictions at the source aren’t applied per agent.',
    },
    { icon: Zap, text: 'Live sources store nothing: a factory listing one gets that connection’s search tools.' },
  ];
  return (
    <section
      aria-labelledby="who-finds"
      className="flex min-w-0 flex-col gap-1.5 rounded-card border border-line bg-card p-5"
    >
      <h2 id="who-finds" className="m-0 mb-1 text-lg font-medium">
        Who finds what
      </h2>
      <ul className="m-0 flex list-none flex-col p-0">
        {rows.map(({ icon: Icon, text }, i) => (
          <li
            // biome-ignore lint/suspicious/noArrayIndexKey: a fixed list
            key={i}
            className="grid grid-cols-[20px_minmax(0,1fr)] gap-2.5 border-t border-line py-2.5 text-sm leading-normal first:border-t-0"
          >
            <Icon size={16} strokeWidth={1.5} aria-hidden="true" className="mt-0.5 text-ink-soft" />
            <span>{text}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
