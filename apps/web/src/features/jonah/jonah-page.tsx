// Jonah's overview and its Proposals tab (ADR 0034, the Jonah screens in ADR 0016). Overview shows what waits
// for a person, how Jonah works in this factory, the pass rate by judge (not built yet) and what Jonah noticed
// this week. Proposals lists the factory's proposals in the state the URL names. Both read the Jonah queries
// and show loading, error, empty and ready. Nothing polls: a `learning` or `definition` event or a resync
// refreshes them.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { Play, Settings } from 'lucide-react';
import type { ReactNode } from 'react';
import { ApiError } from '../../api/client';
import { jonahOverviewQuery, proposalsQuery } from '../../api/queries';
import type { JonahOverview, ProposalState } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { formatUsd } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { Count } from '../../ui/count';
import { EmptyState } from '../../ui/empty-state';
import { NotAvailable } from '../../ui/not-available';
import { SegmentedControl, type SegmentedOption } from '../../ui/segmented-control';
import { modelText, observationChip, proposalStateLabels, proposeAfterText, reviewersText } from './jonah-format';
import { JonahHeader } from './jonah-header';
import { proposalStates } from './jonah-search';
import { ProposalCard } from './proposal-card';

/** Loading shows the app background and an empty region for the screen reader, never a placeholder shape. */
export function Loading() {
  return (
    <div aria-busy="true" className="min-h-40">
      <p className="sr-only">Loading</p>
    </div>
  );
}

/** The page's error state: a plain sentence and Try again. A goald without Jonah says so. */
export function JonahError({ error, retry }: { error: unknown; retry: () => void }) {
  const unavailable = error instanceof ApiError && error.code === 'jonah-unavailable';
  return (
    <EmptyState
      title="Jonah could not be loaded"
      actions={
        <Button variant="secondary" onClick={retry}>
          Try again
        </Button>
      }
    >
      {unavailable ? 'Jonah is not available on this goald yet.' : 'goald is not answering. Try again in a moment.'}
    </EmptyState>
  );
}

/**
 * The header's "Look for improvements now" stays disabled until Jonah's review loop ships: a review queued
 * now would wait for a loop that never claims it, so this page never calls the review API.
 */
function topBarActions(params: { org: string; factory: string }) {
  const settings = (
    <Button key="settings" asChild variant="secondary">
      <Link to="/o/$org/f/$factory/settings" params={params}>
        <Settings aria-hidden="true" />
        Settings
      </Link>
    </Button>
  );
  const look = (
    <Button key="look" variant="primary" disabled aria-describedby="look-meta">
      <Play aria-hidden="true" />
      Look for improvements now
    </Button>
  );
  const meta = (
    <span key="look-meta" id="look-meta" className="text-meta text-muted">
      Reviews start when Jonah's review loop ships
    </span>
  );
  return [settings, look, meta];
}

/** One row of the policy card: a label and its value. */
function PolicyRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex gap-3 text-secondary">
      <dt className="w-28 shrink-0 text-muted">{label}</dt>
      <dd className="m-0 min-w-0">{children}</dd>
    </div>
  );
}

/** The factory's policy (learning.jonah) in words, with Change to the definition's YAML. Judges and Tests are not built yet. */
function PolicyCard({ overview }: { overview: JonahOverview }) {
  const params = useFactoryParams();
  const { policy, month_cost_usd } = overview;
  return (
    <SectionCard
      title="How Jonah works here"
      aside={
        <Link
          to="/o/$org/f/$factory/definition/$section"
          params={{ ...params, section: 'basics' }}
          search={{ mode: 'yaml', file: 'factory.yaml' }}
          className="text-secondary font-control text-run no-underline hover:text-run-ink"
        >
          Change
        </Link>
      }
    >
      <dl className="m-0 flex flex-col gap-3">
        <PolicyRow label="Reads">Judged runs, sent-back reasons and eval results</PolicyRow>
        <PolicyRow label="Judges">
          <NotAvailable label="Judges">
            <button type="button" className="text-left text-muted">
              Not available yet
            </button>
          </NotAvailable>
        </PolicyRow>
        <PolicyRow label="Proposes after">{proposeAfterText(policy)}</PolicyRow>
        <PolicyRow label="Tests with">
          <NotAvailable label="Tests with">
            <button type="button" className="text-left text-muted">
              Not available yet
            </button>
          </NotAvailable>
        </PolicyRow>
        <PolicyRow label="Reviewers">{reviewersText(policy.approver_roles)}</PolicyRow>
        <PolicyRow label="Model">{modelText(policy)}</PolicyRow>
        <PolicyRow label="Spent">{`${formatUsd(month_cost_usd)} this month`}</PolicyRow>
      </dl>
    </SectionCard>
  );
}

/** The pass rate by judge (Judges, not built yet): the section stays, with no numbers in place of the tiles. */
function PassRateSection() {
  const params = useFactoryParams();
  return (
    <section aria-labelledby="pass-rate-h" className="flex min-w-0 flex-col gap-3">
      <div className="flex items-center gap-2">
        <h2 id="pass-rate-h" className="m-0 text-heading">
          Pass rate by judge, last 30 days
        </h2>
        <Link
          to="/o/$org/f/$factory/jonah/judges"
          params={params}
          className="ml-auto text-secondary font-control text-run no-underline hover:text-run-ink"
        >
          Judges
        </Link>
      </div>
      <p className="m-0 rounded-card border border-line bg-card p-4 text-secondary text-muted">Not available yet</p>
    </section>
  );
}

/** The week's observations: Proposed once one became a proposal, else how far it has come. */
function NoticedSection({ observations }: { observations: JonahOverview['observations'] }) {
  return (
    <SectionCard title="What Jonah noticed this week">
      <ul className="m-0 flex list-none flex-col p-0">
        {observations.map((observation) => (
          <li
            key={observation.id}
            className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 border-t border-line py-3 first:border-t-0"
          >
            <span className="min-w-0">
              <span className="block font-control text-ink">{observation.summary}</span>
              <span className="block text-secondary text-muted">{observation.detail}</span>
            </span>
            <Chip tone={observation.state === 'proposed' ? 'you' : 'plain'}>
              {observationChip(observation.state, observation.count, observation.propose_after)}
            </Chip>
          </li>
        ))}
      </ul>
    </SectionCard>
  );
}

/** The overview: the waiting proposals and the policy, then the judges and what Jonah noticed. */
export function JonahOverviewPage() {
  const factory = useFactory();
  const org = useOrg();
  const params = useFactoryParams();
  const actions = topBarActions(params);
  const overview = useQuery(jonahOverviewQuery(org.id, factory.id));
  const waiting = useQuery({ ...proposalsQuery(org.id, factory.id, 'proposed'), enabled: overview.isSuccess });

  if (overview.isPending) return <Loading />;
  if (overview.isError) {
    return (
      <div className="flex min-w-0 flex-col gap-5">
        <TopBarSlot crumb="Jonah" actions={actions} />
        <JonahHeader tab="overview" />
        <JonahError error={overview.error} retry={() => void overview.refetch()} />
      </div>
    );
  }

  const data = overview.data;
  const now = Date.now();
  const proposals = waiting.data?.proposals ?? [];
  const empty = data.waiting === 0 && data.observations.length === 0;

  return (
    <div className="@container flex min-w-0 flex-col gap-5">
      <TopBarSlot crumb="Jonah" actions={actions} />
      <JonahHeader tab="overview" counts={{ proposals: data.waiting }} />
      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        {empty ? (
          <EmptyState title="Jonah has nothing to review yet">
            Jonah reads the runs that were sent back. When a pattern repeats, its proposal and what it noticed appear
            here.
          </EmptyState>
        ) : (
          <section aria-labelledby="waiting-h" className="flex min-w-0 flex-col gap-3">
            <div className="flex items-center gap-2">
              <h2 id="waiting-h" className="m-0 text-heading">
                Waiting for your review
              </h2>
              <Count value={data.waiting} label=" waiting for your review" tone="you" />
              <Link
                to="/o/$org/f/$factory/jonah"
                params={params}
                search={{ tab: 'proposals' }}
                className="text-secondary font-control text-run no-underline hover:text-run-ink"
              >
                All proposals
              </Link>
            </div>
            {waiting.isError ? (
              <JonahError error={waiting.error} retry={() => void waiting.refetch()} />
            ) : proposals.length === 0 ? (
              <p className="m-0 text-secondary text-muted">
                Nothing waits for you. Jonah proposes a change once a pattern repeats in sent-back runs.
              </p>
            ) : (
              proposals.map((proposal, index) => (
                <ProposalCard key={proposal.number} proposal={proposal} now={now} primary={index === 0} waiting />
              ))
            )}
          </section>
        )}

        <PolicyCard overview={data} />
      </div>

      <PassRateSection />
      {data.observations.length > 0 ? <NoticedSection observations={data.observations} /> : null}
    </div>
  );
}

/** The Proposals tab: the factory's proposals in the state the URL names (All when none is named). */
export function JonahProposalsPage({ state }: { state?: ProposalState | undefined }) {
  const factory = useFactory();
  const org = useOrg();
  const params = useFactoryParams();
  const navigate = useNavigate();
  const actions = topBarActions(params);
  const overview = useQuery(jonahOverviewQuery(org.id, factory.id));
  const list = useQuery(proposalsQuery(org.id, factory.id, state));

  if (list.isPending) return <Loading />;

  const counts = overview.data ? { proposals: overview.data.waiting } : {};
  if (list.isError) {
    return (
      <div className="flex min-w-0 flex-col gap-5">
        <TopBarSlot crumb="Jonah" actions={actions} />
        <JonahHeader tab="proposals" counts={counts} />
        <JonahError error={list.error} retry={() => void list.refetch()} />
      </div>
    );
  }

  const filters: SegmentedOption<'all' | ProposalState>[] = [
    { value: 'all', label: 'All' },
    ...proposalStates.map((value) => ({ value, label: proposalStateLabels[value] })),
  ];
  const now = Date.now();
  const { proposals } = list.data;

  return (
    <div className="@container flex min-w-0 flex-col gap-5">
      <TopBarSlot crumb="Jonah" actions={actions} />
      <JonahHeader tab="proposals" counts={counts} />
      <SegmentedControl
        label="Proposal state"
        options={filters}
        value={state ?? 'all'}
        onChange={(value) =>
          void navigate({
            to: '/o/$org/f/$factory/jonah',
            params,
            search: { tab: 'proposals', state: value === 'all' ? undefined : value },
          })
        }
      />

      {proposals.length === 0 ? (
        <EmptyState title={state === undefined ? 'No proposals yet' : 'No proposals in this state'}>
          Jonah proposes a change once a pattern repeats in sent-back runs.
        </EmptyState>
      ) : (
        <div className="flex flex-col gap-3">
          {proposals.map((proposal) => (
            <ProposalCard key={proposal.number} proposal={proposal} now={now} waiting={proposal.state === 'proposed'} />
          ))}
        </div>
      )}
    </div>
  );
}
