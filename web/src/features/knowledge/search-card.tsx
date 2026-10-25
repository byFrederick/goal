// Try a search (spec 4 §10, canvas "Knowledge"): what an agent of one factory gets from
// knowledge__search, over the sources that factory's current version lists (or every source),
// ranked as goald ranks them, each hit's snippet cut around the query's words with them marked
// and a link: the page of a synced document at its source, else the document's indexed text.
// The query and the factory live in the URL (?q=, ?as=), so a search can be linked.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { ExternalLink, Search } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { knowledgeSearchQuery } from '../../api/queries';
import type { Factory, KnowledgeHit, KnowledgeSource } from '../../api/types';
import { plural } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Field, Input, NativeSelect } from '../../ui/field';
import { KindLabel } from './parts';
import { excerpt, refusalText } from './words';

export function SearchCard({ sources, factories }: { sources: KnowledgeSource[]; factories: Factory[] }) {
  const org = useOrg();
  const search = useSearch({ from: '/o/$org/_org/knowledge/' });
  const navigate = useNavigate({ from: '/o/$org/knowledge/' });
  const [text, setText] = useState(search.q ?? '');
  const [as, setAs] = useState(search.as ?? '');
  const id = useId();

  // As a factory: only the sources its current version lists; live ones store nothing to search.
  const listed = search.as ? sources.filter((s) => s.used_by.factories.some((f) => f.name === search.as)) : sources;
  const indexed = listed.filter((s) => s.kind !== 'live');
  const q = search.q ?? '';
  const runs = q !== '' && (!search.as || listed.length > 0);
  const hits = useQuery({
    ...knowledgeSearchQuery(org.id, { q, ...(search.as ? { sources: listed.map((s) => s.name) } : {}) }),
    enabled: runs,
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void navigate({
      search: (prev) => ({ ...prev, q: text.trim() || undefined, as: as || undefined }),
      replace: true,
    });
  };

  return (
    <section
      aria-labelledby={id}
      className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex flex-col gap-1">
        <h2 id={id} className="m-0 text-lg font-medium">
          Try a search
        </h2>
        <p className="m-0 text-sm text-muted">
          What an agent of one factory gets from{' '}
          <code translate="no" className="font-mono text-[13px]">
            knowledge__search
          </code>
          .
        </p>
      </div>
      <search aria-labelledby={id}>
        <form
          onSubmit={submit}
          className="grid grid-cols-1 items-end gap-2 @[640px]:grid-cols-[160px_minmax(0,1fr)_auto]"
        >
          <Field label="As a factory">
            <NativeSelect value={as} onChange={(e) => setAs(e.target.value)}>
              <option value="">Every source</option>
              {factories.map((f) => (
                <option key={f.id} value={f.name}>
                  {f.name}
                </option>
              ))}
            </NativeSelect>
          </Field>
          <Field label="Query">
            <span className="relative flex">
              <Search
                size={16}
                strokeWidth={1.5}
                aria-hidden="true"
                className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted"
              />
              <Input
                type="search"
                value={text}
                onChange={(e) => setText(e.target.value)}
                placeholder="ECS task out of memory"
                maxLength={1000}
                autoComplete="off"
                className="pl-9"
              />
            </span>
          </Field>
          <Button type="submit" variant="primary" className="h-11 justify-center md:h-10">
            Search
          </Button>
        </form>
      </search>

      <p role="status" className="m-0 text-sm text-muted">
        {resultLine({ q, as: search.as, listed, indexed, hits: hits.data, pending: runs && hits.isPending })}
      </p>
      {hits.isError ? (
        <p role="alert" className="m-0 text-sm text-rework-ink">
          {refusalText(hits.error)}
        </p>
      ) : null}
      {runs && hits.data && hits.data.length > 0 ? (
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

/** What the search did, in a sentence: how many hits from which sources, ranked how. */
function resultLine({
  q,
  as,
  listed,
  indexed,
  hits,
  pending,
}: {
  q: string;
  as: string | undefined;
  listed: KnowledgeSource[];
  indexed: KnowledgeSource[];
  hits: KnowledgeHit[] | undefined;
  pending: boolean;
}): string {
  if (q === '') return 'Type what an agent would ask, then Search.';
  if (as && listed.length === 0)
    return `${as}’s current version lists no knowledge source, so its agents find nothing.`;
  if (pending || !hits) return 'Searching…';
  const from = as ? `the ${plural(indexed.length, 'indexed source')} ${as} lists` : `every indexed source`;
  const ranked = indexed.some((s) => s.embed_model) ? 'ranked by meaning and keywords' : 'ranked by keywords';
  if (hits.length === 0)
    return `Nothing in ${from} matches “${q}”. Try fewer or other words${as ? ', or search every source' : ''}.`;
  return `${plural(hits.length, 'result')} from ${from}, ${ranked}`;
}

function Hit({ hit, q, source }: { hit: KnowledgeHit; q: string; source: KnowledgeSource | undefined }) {
  const org = useOrg();
  const title = hit.heading && hit.heading !== hit.title ? `${hit.title} › ${hit.heading}` : hit.title;
  const link = 'text-[15px] font-medium text-ink underline underline-offset-3 hover:text-run';
  return (
    <li className="flex min-w-0 flex-col gap-1 border-t border-line py-3 first:border-t-0 first:pt-0">
      {hit.url ? (
        <a
          href={hit.url}
          target="_blank"
          rel="noreferrer noopener"
          className={`${link} inline-flex items-center gap-1.5 break-words`}
        >
          {title}
          <ExternalLink size={14} strokeWidth={1.5} aria-hidden="true" className="shrink-0" />
          <span className="sr-only">, opens at its source</span>
        </a>
      ) : (
        <Link
          to="/o/$org/knowledge/$source"
          params={{ org: org.slug, source: hit.source }}
          search={{ document: hit.document }}
          className={`${link} break-words`}
        >
          {title}
        </Link>
      )}
      <span className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[13px] text-muted">
        {source ? <KindLabel kind={source.kind} /> : null}
        <span>{source?.title ?? hit.source}</span>
      </span>
      <p className="m-0 text-sm leading-normal break-words text-ink-soft">
        {excerpt(hit.snippet, q).map((s, i) =>
          s.mark ? (
            // biome-ignore lint/suspicious/noArrayIndexKey: the pieces of one snippet, in order
            <mark key={i} className="rounded-[3px] bg-control px-px font-medium text-ink">
              {s.text}
            </mark>
          ) : (
            s.text
          ),
        )}
      </p>
    </li>
  );
}
