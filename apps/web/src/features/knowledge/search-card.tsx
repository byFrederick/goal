// Look something up (canvas "Knowledge"): what an agent gets from knowledge__search over every
// source, ranked as goald ranks them, each hit's snippet cut around the query's words with them
// marked, its score, and a link: the page of a synced document at its source, else the document's
// indexed text. The query lives in the URL (?q=), so a search can be linked.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { ExternalLink, Search } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { knowledgeSearchQuery } from '../../api/queries';
import type { KnowledgeHit, KnowledgeSource } from '../../api/types';
import { externalUrl } from '../../shared/knowledge';
import { useOrg } from '../../shared/org';
import { KindLabel } from './parts';
import { excerpt, refusalText } from './words';

export function SearchCard({ sources }: { sources: KnowledgeSource[] }) {
  const org = useOrg();
  const search = useSearch({ from: '/o/$org/_org/knowledge/' });
  const navigate = useNavigate({ from: '/o/$org/knowledge/' });
  const [text, setText] = useState(search.q ?? '');
  const id = useId();

  const q = search.q ?? '';
  const hits = useQuery({ ...knowledgeSearchQuery(org.id, { q }), enabled: q !== '' });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void navigate({
      search: (prev) => ({ ...prev, q: text.trim() || undefined }),
      replace: true,
    });
  };

  return (
    <section aria-labelledby={id} className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-5">
      <h2 id={id} className="m-0 text-heading">
        Look something up
      </h2>
      <search aria-labelledby={id}>
        <form onSubmit={submit}>
          <div className="flex h-12 items-center gap-3 rounded-control border border-run bg-card pr-2 pl-4">
            <Search size={16} strokeWidth={1.5} aria-hidden="true" className="shrink-0 text-muted" />
            <input
              type="search"
              aria-label="Search knowledge"
              value={text}
              onChange={(e) => setText(e.target.value)}
              maxLength={1000}
              autoComplete="off"
              className="min-w-0 flex-1 bg-transparent text-body text-ink outline-none placeholder:text-muted"
            />
            <span className="hidden shrink-0 text-meta text-muted @[640px]:inline">What an agent would find</span>
          </div>
        </form>
      </search>

      {q !== '' && hits.isPending ? (
        <p role="status" className="m-0 text-secondary text-muted">
          Searching…
        </p>
      ) : null}
      {q !== '' && hits.isError ? (
        <p role="alert" className="m-0 text-secondary text-rework-ink">
          {refusalText(hits.error)}
        </p>
      ) : null}
      {q !== '' && hits.isSuccess && hits.data.length === 0 ? (
        <p role="status" className="m-0 text-secondary text-muted">
          Nothing in the knowledge sources matches “{q}”. Try fewer or other words.
        </p>
      ) : null}
      {q !== '' && hits.data && hits.data.length > 0 ? (
        <ol aria-label="Results" className="m-0 flex list-none flex-col p-0">
          {hits.data.map((h) => (
            <Hit
              key={`${h.document}:${h.heading}:${h.score}`}
              hit={h}
              q={q}
              source={sources.find((s) => s.name === h.source)}
            />
          ))}
        </ol>
      ) : null}
    </section>
  );
}

function Hit({ hit, q, source }: { hit: KnowledgeHit; q: string; source: KnowledgeSource | undefined }) {
  const org = useOrg();
  const title = hit.heading && hit.heading !== hit.title ? `${hit.title} › ${hit.heading}` : hit.title;
  const link = 'font-mono text-secondary text-ink underline-offset-3 hover:text-run hover:underline break-all';
  const url = externalUrl(hit.url);
  return (
    <li className="grid min-w-0 grid-cols-[minmax(0,1fr)_120px] gap-x-4 gap-y-1 border-t border-line py-3 first:border-t-0 first:pt-0">
      {url ? (
        <a href={url} target="_blank" rel="noreferrer noopener" className={`${link} inline-flex items-center gap-2`}>
          {title}
          <ExternalLink size={14} strokeWidth={1.5} aria-hidden="true" className="shrink-0" />
          <span className="sr-only">, opens at its source</span>
        </a>
      ) : (
        <Link
          to="/o/$org/knowledge/$source"
          params={{ org: org.slug, source: hit.source }}
          search={{ document: hit.document }}
          className={link}
        >
          {title}
        </Link>
      )}
      <p className="m-0 text-body leading-normal break-words text-ink-soft">
        {excerpt(hit.snippet, q).map((s, i) =>
          s.mark ? (
            // biome-ignore lint/suspicious/noArrayIndexKey: the pieces of one snippet, in order
            <mark key={i} className="bg-amber-tint text-ink">
              {s.text}
            </mark>
          ) : (
            s.text
          ),
        )}
      </p>
      <span className="row-span-2 flex flex-col items-end gap-1 text-meta text-muted">
        <span className="tabular-nums text-body font-control text-ink">{hit.score.toFixed(2)}</span>
        {source ? <KindLabel kind={source.kind} /> : null}
        <span translate="no">{source?.title ?? hit.source}</span>
      </span>
    </li>
  );
}
