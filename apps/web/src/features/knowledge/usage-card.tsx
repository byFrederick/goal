// Where it was used (ADR 0027, decision 3): one source's documents, with the runs that opened each in the
// last 7 days and the agents' verdicts on them. A document opens its uses, each linked to its run and its
// item. A document that misled runs gets a callout and "Make an item to fix it", which opens the new item
// composer for a factory that uses the source. The usage is read per source and refreshed by `knowledge`
// events, never polled.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useId, useState } from 'react';
import { knowledgeDocumentUsesQuery, knowledgeUsageQuery } from '../../api/queries';
import type { Factory, KnowledgeDocumentUsage, KnowledgeSource } from '../../api/types';
import { refParam } from '../../shared/factory';
import { plural } from '../../shared/format';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';

/** The card's grid: the document, its bar (from md), the runs and the verdicts. */
const row =
  'grid grid-cols-[minmax(0,1fr)_4.5rem_7rem] items-center gap-x-3 md:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_4.5rem_7rem]';

export function UsageCard({ source, factories }: { source: KnowledgeSource | undefined; factories: Factory[] }) {
  return (
    <section aria-labelledby="usage-h" className="flex min-w-0 flex-col rounded-card border border-line bg-card p-5">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 pb-3">
        <h2 id="usage-h" className="m-0 text-heading">
          Where it was used
        </h2>
        <span className="text-secondary text-muted">Runs that opened each document, last 7 days</span>
      </div>
      {source ? (
        <UsageRows source={source} factories={factories} />
      ) : (
        <p className="m-0 border-t border-line pt-3 text-secondary text-muted">
          No runs used this knowledge in the last 7 days
        </p>
      )}
    </section>
  );
}

function UsageRows({ source, factories }: { source: KnowledgeSource; factories: Factory[] }) {
  const org = useOrg();
  const usage = useQuery(knowledgeUsageQuery(org.id, source.name));

  if (usage.isPending) {
    return (
      <div aria-busy="true" className="border-t border-line bg-shell pt-3">
        <span role="status" className="sr-only">
          Loading
        </span>
      </div>
    );
  }
  if (usage.isError) {
    return (
      <div role="alert" className="flex flex-col items-start gap-3 border-t border-line pt-3">
        <Callout tone="rework">Couldn’t load where knowledge was used. Check your connection and try again.</Callout>
        <Button variant="secondary" size="sm" onClick={() => void usage.refetch()}>
          Try again
        </Button>
      </div>
    );
  }

  const documents = usage.data.documents;
  if (documents.length === 0) {
    return (
      <p className="m-0 border-t border-line pt-3 text-secondary text-muted">
        No runs used this knowledge in the last 7 days
      </p>
    );
  }

  const top = documents[0]?.runs ?? 1;
  // The document that misled the most runs gets the callout (the picture shows one).
  const misled = documents.reduce<KnowledgeDocumentUsage | undefined>(
    (worst, d) => (d.agents_misled > (worst?.agents_misled ?? 0) ? d : worst),
    undefined,
  );
  const fixer = factories.find(
    (f) => !f.archived_at && holds(f, 'item:create') && source.used_by.factories.some((u) => u.id === f.id),
  );

  return (
    <>
      <div className={`${row} border-t border-line py-2 text-secondary text-muted`}>
        <span>Document</span>
        <span className="hidden md:block" />
        <span className="text-right">Runs</span>
        <span>Agents’ verdict</span>
      </div>
      <ul className="m-0 flex list-none flex-col p-0">
        {documents.map((d) => (
          <UsageRow key={d.id} doc={d} width={(d.runs / top) * 100} />
        ))}
      </ul>
      {misled && misled.agents_misled > 0 ? (
        <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-cell bg-rework-tint px-3 py-3 text-secondary text-ink">
          <span className="min-w-0 flex-1 break-words">
            <b className="font-control">
              {misled.title} misled {plural(misled.agents_misled, 'run')}.
            </b>
          </span>
          {fixer ? (
            <NewItemButton
              factory={fixer}
              title={`Fix ${misled.title}`}
              trigger={
                <Button variant="secondary" size="sm">
                  Make an item to fix it
                </Button>
              }
            />
          ) : null}
        </div>
      ) : null}
    </>
  );
}

function UsageRow({ doc, width }: { doc: KnowledgeDocumentUsage; width: number }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  return (
    <li className="border-t border-line">
      <div className={`${row} py-2`}>
        <button
          type="button"
          aria-expanded={open}
          aria-controls={id}
          onClick={() => setOpen((o) => !o)}
          className="min-w-0 break-all text-left font-mono text-secondary text-ink underline-offset-3 hover:underline"
        >
          {doc.title}
        </button>
        <span aria-hidden="true" className="hidden h-2 overflow-hidden rounded-chip bg-panel md:block">
          <span className="block h-full bg-run" style={{ width: `${width}%` }} />
        </span>
        <span className="text-right tabular-nums text-body text-ink">{doc.runs}</span>
        <span className="flex gap-2 text-secondary tabular-nums">
          <span className="text-ok-ink">▲ {doc.agents_helped}</span>
          <span className={doc.agents_misled > 0 ? 'text-rework-ink' : 'text-muted'}>▼ {doc.agents_misled}</span>
        </span>
      </div>
      {open ? <DocumentUses id={id} doc={doc} /> : null}
    </li>
  );
}

/** The runs that used a document: each links to its run and to its item. */
function DocumentUses({ id, doc }: { id: string; doc: KnowledgeDocumentUsage }) {
  const org = useOrg();
  const uses = useQuery(knowledgeDocumentUsesQuery(org.id, doc.id));

  if (uses.isPending) {
    return (
      <div id={id} aria-busy="true" className="pb-2 pl-1">
        <span role="status" className="sr-only">
          Loading the runs of {doc.title}
        </span>
      </div>
    );
  }
  if (uses.isError) {
    return (
      <div id={id} role="alert" className="flex flex-col items-start gap-2 pb-2 pl-1">
        <Callout tone="rework">Couldn’t load the runs of {doc.title}.</Callout>
        <Button variant="secondary" size="sm" onClick={() => void uses.refetch()}>
          Try again
        </Button>
      </div>
    );
  }
  return (
    <ul id={id} aria-label={`Runs that used ${doc.title}`} className="m-0 flex list-none flex-col gap-2 pb-2 pl-1">
      {uses.data.uses.map((use) => (
        <li key={use.run_id} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-secondary">
          <Link
            to="/o/$org/f/$factory/items/$ref"
            params={{ org: org.slug, factory: use.factory.name, ref: refParam(use.item.ref) }}
            className="min-w-0 break-words text-ink underline underline-offset-3 hover:text-run"
          >
            {use.item.title}
          </Link>
          <span className="tabular-nums text-muted">{use.item.ref}</span>
          <Link
            to="/o/$org/f/$factory/items/$ref"
            params={{ org: org.slug, factory: use.factory.name, ref: refParam(use.item.ref) }}
            hash={`run-${use.run_id}`}
            className="text-ink underline underline-offset-3 hover:text-run"
          >
            Run
          </Link>
        </li>
      ))}
    </ul>
  );
}
