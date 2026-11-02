// One proposal of Jonah's, for a person to decide (jonah/proposal.jpg, ADR 0017): its estimate and cost per item,
// the definition's files as diffs, the sent-back runs that prompted it, and its details. Approve publishes it,
// Edit writes it into the draft and Reject records the reason Jonah reads (proposal-actions.tsx). A decided
// proposal shows its state and no actions. Judges, Evals and their results are not built yet, so they render
// through NotAvailable.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Check } from 'lucide-react';
import type { ReactNode } from 'react';
import { ApiError } from '../../api/client';
import { jonahOverviewQuery, proposalQuery } from '../../api/queries';
import type { Proposal, ProposalFile, ProposalState } from '../../api/types';
import { PendingSkeleton } from '../../app/route-skeletons';
import { TopBarSlot } from '../../app/top-bar';
import { since } from '../../shared/connections';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { plural } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { SectionCard } from '../../ui/card';
import { Chip, type ChipTone } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { EmptyState } from '../../ui/empty-state';
import { NotAvailable } from '../../ui/not-available';
import { Skeleton } from '../../ui/skeleton';
import { type Stat, StatStrip } from '../../ui/stat-strip';
import { DiffView } from '../factory-editor/review/changes-card';
import { hunksOf } from '../factory-editor/review/review';
import {
  basedOnText,
  costPerItemText,
  estimateDelta,
  estimateOf,
  proposalStateLabels,
  reviewersText,
} from './jonah-format';
import { JonahTabs } from './jonah-header';
import { JonahError } from './jonah-page';
import {
  approveFailure,
  editFailure,
  type ProposalDecisions,
  RejectDialog,
  useProposalDecisions,
} from './proposal-actions';

/** The chip tone of a decided proposal's state: published is done, rejected is sent back. */
const stateTones: Record<ProposalState, ChipTone> = {
  proposed: 'you',
  published: 'ok',
  applied: 'run',
  rejected: 'rework',
  superseded: 'plain',
};

/** The line under the chip: who proposed it and why, or what became of a decided proposal. */
function statusLine(p: Proposal, now: number): string {
  switch (p.state) {
    case 'proposed':
      return `Proposed by Jonah ${since(p.created_at, now)}, after ${plural(p.evidence_count, 'run', 'runs')} sent back. Rejecting tells Jonah why, so it does not propose this again.`;
    case 'published':
      return `Published as v${p.published_version ?? p.publishes_as_version}. Runs in progress kept the version they started on.`;
    case 'applied':
      return 'Edited in the draft. Publish the draft from the definition to put it in place.';
    case 'rejected':
      return `Rejected. Jonah reads the reason before it proposes again: "${p.reject_reason ?? ''}"`;
    case 'superseded':
      return 'A later version replaced this proposal.';
  }
}

/** The page's actions for a proposal still waiting: Edit and Reject need definition:edit, Approve definition:publish. */
function proposalActions(p: Proposal, can: readonly string[], decisions: ProposalDecisions): ReactNode[] {
  const canEdit = can.includes('definition:edit');
  const canPublish = can.includes('definition:publish');
  return [
    <Button
      key="edit"
      variant="secondary"
      disabled={!canEdit || decisions.edit.isPending}
      onClick={() => decisions.edit.mutate()}
    >
      Edit
    </Button>,
    <RejectDialog key="reject" disabled={!canEdit} onReject={(reason) => decisions.reject.mutateAsync(reason)} />,
    <Button
      key="approve"
      variant="primary"
      className="relative"
      disabled={!canPublish || decisions.approve.isPending || decisions.approve.isSuccess}
      onClick={() => decisions.approve.mutate()}
      {...(decisions.approve.isSuccess ? { 'aria-label': 'Published' } : {})}
    >
      <span
        className={cn('transition-opacity duration-(--duration-quick)', decisions.approve.isSuccess && 'opacity-0')}
      >
        Approve and publish v{p.publishes_as_version}
      </span>
      {decisions.approve.isSuccess ? (
        <Check
          size={16}
          strokeWidth={1.5}
          aria-hidden="true"
          className="absolute inset-0 m-auto motion-safe:animate-success-check"
        />
      ) : null}
    </Button>,
  ];
}

/** One file of the change: a bordered block with its path and the lines it adds, then its diff. */
function FileDiff({ file }: { file: ProposalFile }) {
  const hunks = hunksOf(file.base_content, file.content);
  const added = hunks.reduce((n, hunk) => n + hunk.lines.filter((line) => line.kind === 'added').length, 0);
  return (
    <div className="flex min-w-0 flex-col overflow-hidden rounded-control border border-line bg-card">
      <div className="flex min-w-0 items-center gap-3 border-b border-line bg-panel px-4 py-2">
        <span className="min-w-0 break-all font-mono text-secondary font-medium">{file.path}</span>
        <span className="ml-auto font-control text-ok-ink tabular-nums">+{added}</span>
      </div>
      <DiffView hunks={hunks} label={`Changes to ${file.path}`} bare />
    </div>
  );
}

/** The strip's three figures: the estimate with its change, the new eval case (not built yet) and the cost per item. */
function expectedEffect(p: Proposal): Stat[] {
  const figures = p.estimate ? estimateOf(p.estimate) : undefined;
  const stats: Stat[] = [];
  if (figures && p.estimate) {
    stats.push({
      label: figures.label,
      value: `about ${figures.after}`,
      target: `Now ${figures.before}`,
      delta: estimateDelta(p.estimate),
    });
  }
  stats.push({ label: 'New eval case', value: null, note: 'Not available yet' });
  if (p.estimate) {
    stats.push({
      label: 'Cost per item',
      value: costPerItemText(p.estimate.cost_per_item_usd),
      note: basedOnText(p.evidence_count),
    });
  }
  return stats;
}

/** A row of the details card: its label and its value. */
function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex gap-3 border-t border-line pt-3 text-secondary first:border-t-0 first:pt-0">
      <dt className="w-28 shrink-0 text-muted">{label}</dt>
      <dd className="m-0 min-w-0">{children}</dd>
    </div>
  );
}

const block = 'rounded-control bg-hover';

/** The page's layout in muted blocks: chip and title, tabs, the three-figure strip, the diffs and the side cards. */
export function ProposalShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <div className="flex flex-col gap-2">
        <div className={`${block} h-5 w-64 max-w-full`} />
        <div className={`${block} h-6 w-96 max-w-full`} />
      </div>
      <div className={`${block} h-11 w-80 max-w-full md:h-9`} />
      <div className="flex flex-wrap gap-4">
        <div className={`${block} h-16 min-w-36 flex-1`} />
        <div className={`${block} h-16 min-w-36 flex-1`} />
        <div className={`${block} h-16 min-w-36 flex-1`} />
      </div>
      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="flex flex-col gap-3">
          <div className={`${block} h-5 w-48`} />
          <div className={`${block} h-40`} />
          <div className={`${block} h-24`} />
          <div className={`${block} h-48`} />
        </div>
        <div className="flex flex-col gap-5">
          <div className={`${block} h-56`} />
          <div className={`${block} h-32`} />
        </div>
      </div>
    </div>
  );
}

/** The route's pendingComponent: this page's layout, pulsing until the proposal arrives. */
export function ProposalPending() {
  return <PendingSkeleton placeholder={<ProposalShape />} />;
}

/** The proposal page at jonah/proposals/$number. Loading, not found, error and ready each render their own state. */
export function ProposalPage({ number }: { number: number }) {
  const factory = useFactory();
  const org = useOrg();
  const params = useFactoryParams();
  const proposal = useQuery(proposalQuery(org.id, factory.id, number));
  const overview = useQuery(jonahOverviewQuery(org.id, factory.id));
  const decisions = useProposalDecisions(org.id, factory.id, number);
  const crumb = `Proposal ${number}`;

  if (proposal.isPending)
    return (
      <Skeleton ready={false} placeholder={<ProposalShape />}>
        {null}
      </Skeleton>
    );

  if (proposal.isError) {
    const missing = proposal.error instanceof ApiError && proposal.error.code === 'proposal-not-found';
    return (
      <div className="flex min-w-0 flex-col gap-5">
        <TopBarSlot crumb={crumb} />
        {missing ? (
          <EmptyState
            title="This proposal does not exist"
            actions={
              <Button asChild variant="secondary">
                <Link to="/o/$org/f/$factory/jonah" params={params} search={{ tab: 'proposals' }}>
                  All proposals
                </Link>
              </Button>
            }
          >
            It may have been removed. The Proposals tab lists the ones this factory has.
          </EmptyState>
        ) : (
          <JonahError error={proposal.error} retry={() => void proposal.refetch()} />
        )}
      </div>
    );
  }

  const p = proposal.data;
  const now = Date.now();
  const waiting = p.state === 'proposed';
  const reviewers = overview.data ? reviewersText(overview.data.policy.approver_roles) : undefined;
  const actions = waiting || decisions.approve.isSuccess ? proposalActions(p, factory.can, decisions) : [];
  const counts = overview.data ? { proposals: overview.data.waiting } : {};

  return (
    <div className="@container flex min-w-0 flex-col gap-5">
      <TopBarSlot crumb={crumb} actions={actions.length > 0 ? actions : undefined} />

      <div className="flex min-w-0 flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <Chip tone={stateTones[p.state]} dot>
            {waiting ? 'Waiting for your review' : proposalStateLabels[p.state]}
          </Chip>
          <span className="text-secondary text-ink-soft">{statusLine(p, now)}</span>
        </div>
        <h1 className="m-0 text-page-title tracking-display">{p.title}</h1>
      </div>

      <JonahTabs tab="proposals" counts={counts} />

      <div aria-live="polite" className="flex flex-col gap-3">
        {decisions.approve.isError ? <Callout tone="rework">{approveFailure(decisions.approve.error)}</Callout> : null}
        {decisions.edit.isError ? <Callout tone="rework">{editFailure(decisions.edit.error)}</Callout> : null}
      </div>

      <StatStrip label="Expected effect" stats={expectedEffect(p)} />

      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="flex min-w-0 flex-col gap-5">
          <section aria-labelledby="changes-h" className="flex min-w-0 flex-col gap-3">
            <div className="flex flex-wrap items-baseline gap-2">
              <h2 id="changes-h" className="m-0 text-heading">
                Changes to the definition
              </h2>
              <span className="text-secondary text-muted">
                {plural(p.files.length, 'file', 'files')}, v{p.made_against_version} to v{p.publishes_as_version}
              </span>
            </div>
            {p.files.map((file) => (
              <FileDiff key={file.path} file={file} />
            ))}
          </section>

          <SectionCard title={`Why: ${plural(p.evidence_count, 'run', 'runs')} sent back`}>
            <ul className="m-0 flex list-none flex-col p-0">
              {p.evidence.map((evidence) => (
                <li
                  key={`${evidence.item_ref}:${evidence.quote}`}
                  className="flex flex-col gap-1 border-t border-line py-3 first:border-t-0"
                >
                  <div className="flex flex-wrap items-center gap-2">
                    <Chip className="font-mono">{evidence.item_ref}</Chip>
                    <span className="font-control text-ink">{evidence.item_title}</span>
                  </div>
                  <p className="m-0 text-secondary text-ink-soft">&ldquo;{evidence.quote}&rdquo;</p>
                </li>
              ))}
            </ul>
          </SectionCard>
        </div>

        <div className="flex min-w-0 flex-col gap-5">
          <SectionCard title="Details">
            <dl className="m-0 flex flex-col gap-3">
              <DetailRow label="Affects">{p.agents.length > 0 ? p.agents.join(', ') : p.paths.join(', ')}</DetailRow>
              <DetailRow label="Found by">
                <NotAvailable label="Judge">
                  <button type="button" className="cursor-not-allowed text-ink-soft">
                    The judge
                  </button>
                </NotAvailable>{' '}
                and sent-back reasons
              </DetailRow>
              <DetailRow label="Tested on">
                <NotAvailable label="Tested on">
                  <button type="button" className="cursor-not-allowed text-left text-muted">
                    Not available yet
                  </button>
                </NotAvailable>
              </DetailRow>
              <DetailRow label="Publishes">
                v{p.publishes_as_version}; runs in progress keep v{p.made_against_version}
              </DetailRow>
              {reviewers === undefined ? null : <DetailRow label="Reviewers">{reviewers}</DetailRow>}
            </dl>
          </SectionCard>

          <SectionCard title="Eval results">
            <NotAvailable label="Eval results">
              <button type="button" className="cursor-not-allowed text-left text-muted">
                Not available yet
              </button>
            </NotAvailable>
          </SectionCard>
        </div>
      </div>
    </div>
  );
}
