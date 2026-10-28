// A proposal in a list (MASTER.md, cards and chips; jonah.html's proposal): its number chip, the agents and
// the files it edits, its age, its title, the estimate before → about after, the cost per item and the
// evidence it rests on. Review opens the proposal's page, which is not built yet (part proposal-ui), so it
// renders through NotAvailable. The Evals figure needs the eval loop's numbers, which the estimate lacks.
import { ArrowRight } from 'lucide-react';
import type { ProposalEntry } from '../../api/types';
import { since } from '../../shared/connections';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { NotAvailable } from '../../ui/not-available';
import { basedOnText, costPerItemText, estimateOf, proposalStateLabels } from './jonah-format';

/** A figure in the estimate line: its label, the value it has now, and what it is about after the change. */
function Figure({ label, now, after }: { label: string; now: string; after?: string }) {
  return (
    <span className="inline-flex items-center gap-2">
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
  /** A card the person has to decide on: its number chip is violet. */
  waiting: boolean;
}) {
  const { number, title, state, created_at, paths, agents, estimate, evidence_count } = proposal;
  const headingId = `proposal-${number}-title`;
  const figures = estimate ? estimateOf(estimate) : undefined;

  return (
    <article
      aria-labelledby={headingId}
      className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex flex-wrap items-center gap-2">
        <Chip tone={waiting ? 'you' : 'plain'} dot={waiting}>
          Proposal {number}
        </Chip>
        {waiting ? null : <Chip>{proposalStateLabels[state]}</Chip>}
        {agents.map((agent) => (
          <Chip key={agent}>{agent}</Chip>
        ))}
        {paths.map((path) => (
          <Chip key={path} className="font-mono">
            {path}
          </Chip>
        ))}
        <span className="ml-auto text-meta text-muted">{since(created_at, now)}</span>
      </div>

      <h3 id={headingId} className="m-0 text-heading">
        {title}
      </h3>

      <div className="flex flex-wrap gap-x-5 gap-y-2 text-secondary">
        {figures ? <Figure label={figures.label} now={figures.before} after={figures.after} /> : null}
        <NotAvailable label="Evals">
          <button type="button" className="inline-flex items-center gap-2 text-ink-soft">
            Evals
          </button>
        </NotAvailable>
        {estimate ? (
          <span className="inline-flex items-center gap-2">
            Cost per item <span className="font-control">{costPerItemText(estimate.cost_per_item_usd)}</span>
          </span>
        ) : null}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <span className="text-secondary text-ink-soft">{basedOnText(evidence_count)}</span>
        <NotAvailable label="Review">
          <Button variant={primary ? 'primary' : 'secondary'} size="sm">
            Review
          </Button>
        </NotAvailable>
      </div>
    </article>
  );
}
