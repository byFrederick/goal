// One proposal of Jonah's, for a person to decide (proposal.jpg, proposal-memory.jpg, proposal-model.jpg, ADR 0039):
// its estimate, what was tried on past items, the files as diffs, the items that prompted it with their reasons, and
// its details. A memory proposal lists its facts with the quotes they come from; a model proposal shows its three
// tries per item beside the item's line result. Approve publishes it, Edit writes it into the draft and Reject records
// the reason Jonah reads (proposal-actions.tsx). A decided proposal shows its state and no actions.
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { ApiError } from '../../api/client';
import { jonahOverviewQuery, proposalQuery } from '../../api/queries';
import type { Proposal, ProposalFile, ProposalState } from '../../api/types';
import { PendingSkeleton } from '../../app/route-skeletons';
import { TopBarSlot } from '../../app/top-bar';
import { since } from '../../shared/connections';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { formatUsd, plural } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { SectionCard } from '../../ui/card';
import { Chip, type ChipTone } from '../../ui/chip';
import { Disclosure } from '../../ui/disclosure';
import { EmptyState } from '../../ui/empty-state';
import { Skeleton } from '../../ui/skeleton';
import { type Stat, StatStrip } from '../../ui/stat-strip';
import { SuccessLabel } from '../../ui/success-label';
import { DiffView } from '../factory-editor/review/changes-card';
import { hunksOf } from '../factory-editor/review/review';
import { InfoTip } from './info-tip';
import {
  addedFacts,
  costPerItemText,
  costPerTry,
  estimateDelta,
  estimateOf,
  factCount,
  fixedCount,
  itemNumber,
  keptCount,
  lineCostPerItem,
  lineCount,
  modelOfProposal,
  type ProposalKind,
  proposalKind,
  proposalStateLabels,
  reviewersText,
  triesPassed,
  watchText,
  watchVerdicts,
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
import { ModelTries, TriedPanel } from './tried-panel';
import { TryAgain } from './try-again';

/** The chip tone of a decided proposal's state: published is done, rejected is sent back. */
const stateTones: Record<ProposalState, ChipTone> = {
  proposed: 'you',
  published: 'ok',
  applied: 'run',
  rejected: 'rework',
  superseded: 'plain',
};

/** The line beside the chip: who proposed it and when, or what became of a decided proposal. */
function statusLine(p: Proposal, now: number): string {
  switch (p.state) {
    case 'proposed':
      return `Jonah, ${since(p.created_at, now)}`;
    case 'published':
      return `Published as v${p.published_version ?? p.publishes_as_version}. Runs in progress kept the version they started on.`;
    case 'applied':
      return 'Edited in the draft. Publish the draft from the definition to put it in place.';
    case 'rejected':
      return `Rejected: "${p.reject_reason ?? ''}"`;
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
      disabled={!canEdit}
      busy={decisions.edit.isPending}
      busyLabel="Saving…"
      onClick={() => decisions.edit.mutate()}
    >
      Edit
    </Button>,
    <RejectDialog
      key="reject"
      disabled={!canEdit}
      busy={decisions.reject.isPending}
      onReject={(reason) => decisions.reject.mutateAsync(reason)}
    />,
    // After success the button stays at full opacity to show the check, and ignores clicks without `disabled`.
    <Button
      key="approve"
      variant="primary"
      disabled={!canPublish}
      busy={decisions.approve.isPending}
      busyLabel={`Publishing v${p.publishes_as_version}…`}
      {...(decisions.approve.isSuccess ? { 'aria-label': 'Published' } : {})}
      onClick={() => {
        if (!decisions.approve.isSuccess) decisions.approve.mutate();
      }}
    >
      <SuccessLabel done={decisions.approve.isSuccess} doneLabel="Published">
        Approve and publish v{p.publishes_as_version}
      </SuccessLabel>
    </Button>,
  ];
}

/** One file of the change: a bordered block with its path and the lines it adds and removes, then its diff. */
function FileDiff({ file }: { file: ProposalFile }) {
  const hunks = hunksOf(file.base_content, file.content);
  const lines = hunks.flatMap((hunk) => hunk.lines);
  const added = lines.filter((line) => line.kind === 'added').length;
  const removed = lines.filter((line) => line.kind === 'removed').length;
  return (
    <div className="flex min-w-0 flex-col overflow-hidden rounded-control border border-line bg-card">
      <div className="flex min-w-0 items-center gap-3 border-b border-line bg-panel px-4 py-2">
        <span className="min-w-0 break-all font-mono text-secondary font-medium">{file.path}</span>
        <span className="ml-auto flex gap-2 font-control tabular-nums">
          {added > 0 ? <span className="text-ok-ink">+{added}</span> : null}
          {removed > 0 ? <span className="text-rework-ink">−{removed}</span> : null}
        </span>
      </div>
      <DiffView hunks={hunks} label={`Changes to ${file.path}`} bare />
    </div>
  );
}

/** The strip's three figures, by the proposal's kind. */
function expectedEffect(p: Proposal, kind: ProposalKind): Stat[] {
  const tried = p.tried;
  if (kind === 'memory') {
    const file = p.files[0];
    const after = file ? factCount(file.content) : 0;
    const before = file ? factCount(file.base_content) : 0;
    return [
      { label: 'Facts in memory', value: after, note: `Now ${before}` },
      { label: 'Evidence', value: plural(p.evidence_count, 'item', 'items'), note: 'Each quotes a person' },
      { label: 'Tried', value: 'None', note: 'Memory changes run no try' },
    ];
  }
  if (kind === 'model' && tried) {
    const passed = triesPassed(tried);
    const line = lineCount(tried);
    const perTry = costPerTry(tried);
    const now = lineCostPerItem(tried);
    const each = tried.items[0]?.tries.length ?? 3;
    return [
      {
        label: 'Tries passed',
        value: `${passed.passed} of ${passed.total}`,
        note: `${each} tries on each of ${plural(tried.items.length, 'item', 'items')}`,
      },
      { label: 'On the line', value: `${line.passed} of ${line.total}`, note: 'Passed first time' },
      {
        label: 'Cost per try',
        value: perTry === undefined ? null : formatUsd(perTry),
        ...(now === undefined ? {} : { note: `Now ${formatUsd(now)}` }),
      },
    ];
  }
  const figures = p.estimate ? estimateOf(p.estimate) : undefined;
  const stats: Stat[] = [];
  if (figures && p.estimate) {
    stats.push({
      label: figures.label,
      value: `about ${figures.after}`,
      target: `Now ${figures.before}, Jonah's estimate`,
      delta: estimateDelta(p.estimate),
    });
  }
  if (tried) {
    const fixed = fixedCount(tried);
    const kept = keptCount(tried);
    const noneTried = tried.state === 'budget_used' && tried.items.every((item) => item.tries.every((t) => !t.verdict));
    stats.push({
      label: 'Tried on past items',
      value: noneTried ? null : `${fixed.fixed} of ${fixed.total} fixed`,
      note: noneTried
        ? 'Not tried: budget used'
        : kept.total > 0
          ? `${kept.passed} of ${kept.total} kept tests pass`
          : tried.state === 'trying'
            ? 'Still trying'
            : 'No kept tests',
    });
  } else {
    stats.push({ label: 'Tried on past items', value: null, note: 'Not tried' });
  }
  if (p.estimate) {
    stats.push({ label: 'Cost per item', value: costPerItemText(p.estimate.cost_per_item_usd) });
  }
  return stats;
}

/** A row of the details card: its label and its value. */
function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex gap-3 border-t border-line pt-3 text-secondary first:border-t-0 first:pt-0">
      <dt className="w-28 shrink-0 text-muted">{label}</dt>
      <dd className="m-0 min-w-0 break-words text-ink-soft">{children}</dd>
    </div>
  );
}

/** Details: what it affects, what it publishes as, what is watched, who may approve. Open on a desktop, shut on a phone. */
function DetailsCard({ p, reviewers, kind }: { p: Proposal; reviewers: string | undefined; kind: ProposalKind }) {
  const figures = p.estimate ? estimateOf(p.estimate) : undefined;
  const open = typeof window === 'undefined' || (window.matchMedia?.('(min-width: 768px)').matches ?? true);
  return (
    <div className="rounded-card border border-line bg-card px-2 py-1 md:px-3">
      <Disclosure summary={<span className="text-heading">Details</span>} defaultOpen={open}>
        <dl className="m-0 flex flex-col gap-3 px-2 pt-1 pb-3">
          <DetailRow label="Affects">{p.agents.length > 0 ? p.agents.join(', ') : p.paths.join(', ')}</DetailRow>
          {kind === 'model' ? null : (
            <DetailRow label="Found in">{plural(p.evidence_count, 'item', 'items')} sent back</DetailRow>
          )}
          <DetailRow label="Publishes">
            v{p.publishes_as_version}; running items keep v{p.made_against_version}
          </DetailRow>
          {figures ? <DetailRow label="Watched">{`${figures.label}, now ${figures.before}`}</DetailRow> : null}
          {p.watch ? (
            <DetailRow label="Watch">
              {watchText(p.watch)}, {watchVerdicts[p.watch.verdict].label.toLowerCase()}
            </DetailRow>
          ) : null}
          <DetailRow label="Undo">Undo this change, any time</DetailRow>
          {reviewers === undefined ? null : <DetailRow label="Reviewers">{reviewers}</DetailRow>}
        </dl>
      </Disclosure>
    </div>
  );
}

/** The facts a memory proposal adds, each with the quote it comes from. */
function MemoryFacts({ p }: { p: Proposal }) {
  const file = p.files[0];
  const facts = file ? addedFacts(file.base_content, file.content) : [];
  const quoteOf = (ref: string) => p.evidence.find((e) => e.item_ref === ref);
  return (
    <SectionCard
      title="New facts"
      aside={<span className="text-secondary text-muted">{plural(facts.length, 'fact', 'facts')}</span>}
    >
      <ul className="m-0 flex list-none flex-col p-0">
        {facts.map((fact) => (
          <li key={fact.text} className="flex flex-col gap-1 border-t border-line py-3 first:border-t-0">
            <span className="font-control text-ink break-words">{fact.text}</span>
            {fact.from.map((ref) => {
              const evidence = quoteOf(ref);
              return (
                <span key={ref} className="text-secondary text-muted break-words">
                  <span className="mr-2 tabular-nums">{itemNumber(ref)}</span>
                  {evidence ? <>&ldquo;{evidence.quote}&rdquo;</> : null}
                </span>
              );
            })}
          </li>
        ))}
      </ul>
    </SectionCard>
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
                <Link to="/o/$org/f/$factory/jonah/proposals" params={params}>
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
  const kind = proposalKind(p);
  const now = Date.now();
  const waiting = p.state === 'proposed';
  const reviewers = overview.data ? reviewersText(overview.data.policy.approver_roles) : undefined;
  const actions = waiting || decisions.approve.isSuccess ? proposalActions(p, factory.can, decisions) : [];
  const tried = p.tried;
  const tryAgain =
    waiting && factory.can.includes('definition:edit') && tried && tried.items.length > 0 ? (
      <TryAgain number={p.number} />
    ) : undefined;

  return (
    <div className="@container flex min-w-0 flex-col gap-5 max-md:motion-safe:animate-pane-forward">
      <TopBarSlot crumb={crumb} actions={actions.length > 0 ? actions : undefined} />

      <div className="flex min-w-0 flex-col gap-2">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <Chip tone={stateTones[p.state]} dot>
            {waiting ? 'Waiting for your review' : proposalStateLabels[p.state]}
          </Chip>
          <span className="text-secondary text-ink-soft">{statusLine(p, now)}</span>
        </div>
        <h1 className="m-0 text-page-title tracking-display">{p.title}</h1>
      </div>

      <JonahTabs tab="proposals" />

      <div aria-live="polite" className="flex flex-col gap-3">
        {decisions.approve.isError ? <Callout tone="rework">{approveFailure(decisions.approve.error)}</Callout> : null}
        {decisions.edit.isError ? <Callout tone="rework">{editFailure(decisions.edit.error)}</Callout> : null}
      </div>

      <StatStrip label="Expected effect" stats={expectedEffect(p, kind)} />

      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="flex min-w-0 flex-col gap-5">
          <section aria-labelledby="changes-h" className="flex min-w-0 flex-col gap-3">
            <div className="flex flex-wrap items-baseline gap-2">
              <h2 id="changes-h" className="m-0 text-heading">
                {kind === 'memory' ? 'Changes to factory memory' : 'Changes to the definition'}
              </h2>
              <span className="text-secondary text-muted">
                {plural(p.files.length, 'file', 'files')}, v{p.made_against_version} to v{p.publishes_as_version}
              </span>
            </div>
            {p.files.map((file) => (
              <FileDiff key={file.path} file={file} />
            ))}
          </section>

          {kind === 'memory' ? <MemoryFacts p={p} /> : null}
          {kind === 'model' && tried ? (
            <ModelTries tried={tried} model={modelOfProposal(p.files)} action={tryAgain} />
          ) : null}

          {kind === 'model' ? null : (
            <SectionCard
              title={
                <span className="inline-flex items-center gap-1">
                  Why
                  <InfoTip label="What Why shows">
                    Each item that prompted this proposal, with the reason it was sent back.
                  </InfoTip>
                </span>
              }
              aside={
                <span className="text-secondary text-muted">
                  {p.evidence.length} of {plural(p.evidence_count, 'item', 'items')}
                </span>
              }
            >
              <ul className="m-0 flex list-none flex-col p-0">
                {p.evidence.map((evidence) => (
                  <li
                    key={`${evidence.item_ref}:${evidence.quote}`}
                    className="flex flex-col gap-1 border-t border-line py-3 first:border-t-0"
                  >
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                      <span className="text-muted tabular-nums">{itemNumber(evidence.item_ref)}</span>
                      <span className="min-w-0 font-control text-ink break-words">{evidence.item_title}</span>
                      {evidence.station ? (
                        <span className="ml-auto text-secondary text-muted">{evidence.station}</span>
                      ) : null}
                    </div>
                    <p className="m-0 text-secondary text-ink-soft break-words">&ldquo;{evidence.quote}&rdquo;</p>
                  </li>
                ))}
              </ul>
            </SectionCard>
          )}
        </div>

        <div className="flex min-w-0 flex-col gap-5">
          {kind === 'definition' && tried ? <TriedPanel tried={tried} action={tryAgain} /> : null}
          <DetailsCard p={p} reviewers={reviewers} kind={kind} />
        </div>
      </div>
    </div>
  );
}
