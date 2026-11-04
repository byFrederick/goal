// A proposal in a list (MASTER.md, cards and chips; jonah.jpg's Waiting for your review): its number chip,
// the files it edits, its age, its title, then what it expects and what was tried, by kind. Review opens
// the proposal's page. A waiting proposal reads its page's data once (shared with the page) to say what was tried.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ArrowRight } from 'lucide-react';
import { proposalQuery } from '../../api/queries';
import type { Proposal, ProposalEntry } from '../../api/types';
import { since } from '../../shared/connections';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { formatUsd } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import {
  basedOnText,
  costPerItemText,
  costPerTry,
  estimateOf,
  lineCostPerItem,
  lineCount,
  type ProposalKind,
  proposalKind,
  proposalStateLabels,
  triedLine,
} from './jonah-format';

/** A figure in the estimate line: its label, the value it has now, and what it is about after the change. */
function Figure({ label, now, after }: { label: string; now: string; after?: string }) {
  return (
    <span className="inline-flex flex-wrap items-center gap-x-2">
      {label} <span className="font-control">{now}</span>
      {after === undefined ? null : (
        <>
          <ArrowRight size={14} strokeWidth={1.5} aria-hidden="true" className="text-muted" />
          <span className="font-control text-ok-ink">about {after}</span>
        </>
      )}
    </span>
  );
}

/** The lines under the title, by the proposal's kind. */
function Lines({ entry, full }: { entry: ProposalEntry; full: Proposal | undefined }) {
  const kind: ProposalKind = proposalKind(full ?? entry);
  const tried = full?.tried;
  const tries = triedLine(tried, kind);
  if (kind === 'memory') {
    const first = full?.evidence[0]?.quote;
    const more = (full?.evidence.length ?? 0) - 1;
    return first === undefined ? null : (
      <p className="m-0 text-secondary text-ink-soft">
        &ldquo;{first}&rdquo;
        {more > 0 ? ` and ${more} more` : ''}
      </p>
    );
  }
  if (kind === 'model' && tried) {
    const line = lineCount(tried);
    const perTry = costPerTry(tried);
    const lineCost = lineCostPerItem(tried);
    return (
      <div className="flex flex-col gap-1 text-secondary text-ink-soft">
        {tries ? <span>{tries}</span> : null}
        <span>
          On the line:{' '}
          <span className="font-control">
            {line.passed} of {line.total}
          </span>{' '}
          passed first time
        </span>
        {perTry === undefined ? null : (
          <span>
            Cost per try <span className="font-control">{formatUsd(perTry)}</span>
            {lineCost === undefined ? '' : `, now ${formatUsd(lineCost)}`}
          </span>
        )}
      </div>
    );
  }
  const figures = entry.estimate ? estimateOf(entry.estimate) : undefined;
  return (
    <div className="flex flex-col gap-1 text-secondary text-ink-soft">
      <div className="flex flex-wrap gap-x-5 gap-y-1">
        {figures ? <Figure label={figures.label} now={figures.before} after={figures.after} /> : null}
        {tries ? <span>{tries}</span> : null}
      </div>
      {entry.estimate ? (
        <span>
          Cost per item <span className="font-control">{costPerItemText(entry.estimate.cost_per_item_usd)}</span>
        </span>
      ) : null}
    </div>
  );
}

export function ProposalCard({
  proposal,
  now,
  primary = false,
  waiting,
}: {
  proposal: ProposalEntry;
  /** The time the card's age is counted from, in epoch milliseconds. */
  now: number;
  /** The first card of a list is the one Review leads with. */
  primary?: boolean;
  /** A card the person has to decide on: its number chip has a violet dot. */
  waiting: boolean;
}) {
  const { number, title, state, created_at, paths, evidence_count } = proposal;
  const params = useFactoryParams();
  const org = useOrg();
  const factory = useFactory();
  const headingId = `proposal-${number}-title`;
  const full = useQuery({ ...proposalQuery(org.id, factory.id, number), enabled: waiting });

  return (
    <article
      aria-labelledby={headingId}
      className="flex min-w-0 flex-col gap-3 border-t border-line p-4 first:border-t-0 md:p-5"
    >
      <div className="flex flex-wrap items-center gap-2">
        <Chip tone={waiting ? 'you' : 'plain'} dot={waiting}>
          Proposal {number}
        </Chip>
        {waiting ? null : <Chip>{proposalStateLabels[state]}</Chip>}
        {paths.map((path) => (
          <Chip key={path} className="max-w-full font-mono break-all whitespace-normal">
            {path}
          </Chip>
        ))}
        <span className="ml-auto text-meta text-muted">{since(created_at, now)}</span>
      </div>

      <h3 id={headingId} className="m-0 text-heading break-words">
        {title}
      </h3>

      <Lines entry={proposal} full={full.data} />

      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <span className="text-secondary text-muted">
          {full.data && proposalKind(full.data) === 'memory'
            ? `Quoted from ${evidence_count} people's words`
            : full.data && proposalKind(full.data) === 'model'
              ? null
              : evidence_count > 0
                ? basedOnText(evidence_count)
                : null}
        </span>
        <Button asChild variant={primary ? 'primary' : 'secondary'} size="sm">
          <Link to="/o/$org/f/$factory/jonah/proposals/$number" params={{ ...params, number: String(number) }}>
            Review<span className="sr-only"> proposal {number}</span>
          </Link>
        </Button>
      </div>
    </article>
  );
}
