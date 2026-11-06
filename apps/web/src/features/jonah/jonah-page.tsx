// Jonah's Overview and Proposals tabs (ADR 0039, the Jonah screens in ADR 0016). Overview shows what waits for
// a person, what happened after each publish, what Jonah noticed and how it works here; each panel reads its own
// query and shows its own skeleton. Proposals lists the factory's proposals in the state the URL names. Nothing
// polls: a `learning` or `definition` event or a resync refreshes them.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { Play, Settings } from 'lucide-react';
import { type ReactNode, useState } from 'react';
import { toast } from 'sonner';
import { ApiError, queueJonahReview } from '../../api/client';
import { jonahOverviewQuery, keys, proposalsQuery } from '../../api/queries';
import type { JonahOverview, ProposalEntry, ProposalState } from '../../api/types';
import { PendingSkeleton } from '../../app/route-skeletons';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { formatUsd } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { useArrivals } from '../../ui/arrivals';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { Count } from '../../ui/count';
import { EmptyState } from '../../ui/empty-state';
import { SegmentedControl, type SegmentedOption } from '../../ui/segmented-control';
import { Skeleton } from '../../ui/skeleton';
import { AfterPublishPanel } from './after-publish';
import { InfoTip } from './info-tip';
import { modelText, observationChip, proposalStateLabels, proposeAfterText, reviewersText } from './jonah-format';
import { JonahHeader } from './jonah-header';
import { proposalStates } from './jonah-search';
import { ProposalCard } from './proposal-card';

const block = 'rounded-control bg-hover';

const rowKeys = ['a', 'b', 'c', 'd', 'e'];

/** A card's layout in muted blocks: its title and a few rows. */
function PanelShape({ rows, row = 'h-16' }: { rows: number; row?: string }) {
  return (
    <div className="flex flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5">
      <div className={`${block} h-5 w-48`} />
      {rowKeys.slice(0, rows).map((key) => (
        <div key={key} className={`${block} ${row}`} />
      ))}
    </div>
  );
}

/** The route's layout in muted blocks: the header and its tabs, then the panels. */
export function JonahShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <div className={`${block} h-6 w-24`} />
          <div className={`${block} h-4 w-96 max-w-full`} />
        </div>
        <div className={`${block} h-11 w-80 max-w-full md:h-9`} />
      </div>
      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <PanelShape rows={2} row="h-28" />
        <PanelShape rows={4} row="h-5" />
      </div>
    </div>
  );
}

/** The route's pendingComponent: the layout, pulsing until the page arrives. */
export function JonahPending() {
  return <PendingSkeleton placeholder={<JonahShape />} />;
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
      {unavailable
        ? 'Jonah is not turned on for this installation yet.'
        : 'goald is not answering. Try again in a moment.'}
    </EmptyState>
  );
}

/** What Look for improvements now says when it is refused, by the API's code. */
export function reviewFailure(err: unknown): string {
  const code = err instanceof ApiError ? err.code : undefined;
  if (code === 'review-in-flight') return 'A review is already running. Its proposals appear here.';
  if (code === 'learning-budget-exhausted') return "Jonah's budget for this week is used up, so no review was started.";
  if (code === 'version-not-found') return 'Publish the definition once before Jonah looks for improvements.';
  return 'goald is not answering. Try again in a moment.';
}

/** The top bar's Look for improvements now: queues a review at once. Needs definition:edit. On a phone only its icon shows. */
function LookButton() {
  const org = useOrg();
  const factory = useFactory();
  const queryClient = useQueryClient();
  const look = useMutation({
    mutationFn: () => queueJonahReview(org.id, factory.id),
    onSuccess: async () => {
      toast.success('Jonah is looking. New proposals appear here.');
      await queryClient.invalidateQueries({ queryKey: keys.jonah(org.id, factory.id) });
    },
    onError: (err) => toast.error(reviewFailure(err)),
  });
  return (
    <Button
      variant="secondary"
      disabled={!factory.can.includes('definition:edit') || look.isPending}
      aria-label="Look for improvements now"
      className="max-md:min-w-11"
      onClick={() => look.mutate()}
    >
      <Play aria-hidden="true" />
      <span className="max-md:hidden">Look for improvements now</span>
    </Button>
  );
}

/** The top bar's actions: Settings, and Look for improvements now. On a phone the two show their icons only. */
function topBarActions(params: { org: string; factory: string }) {
  return [
    <Button key="settings" asChild variant="secondary">
      <Link to="/o/$org/f/$factory/settings" params={params}>
        <Settings aria-hidden="true" />
        <span className="max-md:sr-only">Settings</span>
      </Link>
    </Button>,
    <LookButton key="look" />,
  ];
}

/** One row of the policy card: a label and its value. */
function PolicyRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex gap-3 text-secondary text-ink-soft">
      <dt className="w-28 shrink-0 text-muted">{label}</dt>
      <dd className="m-0 min-w-0 break-words">{children}</dd>
    </div>
  );
}

/** The factory's policy (learning.jonah) in words, with Change to the definition's YAML. */
function PolicyCard({ overview }: { overview: JonahOverview }) {
  const params = useFactoryParams();
  const { policy } = overview;
  return (
    <SectionCard
      title={
        <span className="inline-flex items-center gap-1">
          How Jonah works here
          <InfoTip label="How Jonah works">
            Jonah reviews the line once a day, tries each change on past items, then watches it after publish.
          </InfoTip>
        </span>
      }
      aside={
        <Link
          to="/o/$org/f/$factory/definition/$section"
          params={{ ...params, section: 'basics' }}
          search={{ mode: 'yaml', file: 'factory.yaml' }}
          className="press text-secondary font-control text-link no-underline hover:text-run-ink max-md:-my-3 max-md:py-3"
        >
          Change
        </Link>
      }
    >
      <dl className="m-0 flex flex-col gap-3">
        <PolicyRow label="Reads">Send-backs, touches and failed runs</PolicyRow>
        <PolicyRow label="Reviews">Once a day</PolicyRow>
        <PolicyRow label="Proposes after">{proposeAfterText(policy)}</PolicyRow>
        <PolicyRow label="Tries">The items it cites, and your kept tests</PolicyRow>
        <PolicyRow label="Watches">Each change, station by station</PolicyRow>
        <PolicyRow label="Reviewers">{reviewersText(policy.approver_roles)}</PolicyRow>
        <PolicyRow label="Model">
          <span className="font-mono">{modelText(policy)}</span>
        </PolicyRow>
        <PolicyRow label="Budget">
          {`${Math.round(policy.share * 100)}% of the line's spend, ${formatUsd(policy.budget_usd)} a week; ${formatUsd(overview.month_cost_usd)} this month`}
        </PolicyRow>
      </dl>
    </SectionCard>
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
              <span className="block font-control text-ink break-words">{observation.summary}</span>
              <span className="block text-secondary text-muted break-words">{observation.detail}</span>
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

/** A list of proposal cards in one bordered panel; a card that arrives after the list loaded rises in. */
function ProposalList({ proposals, firstPrimary }: { proposals: ProposalEntry[]; firstPrimary: boolean }) {
  const now = Date.now();
  const arrived = useArrivals(proposals.map((p) => String(p.number)));
  return (
    <div className="flex min-w-0 flex-col overflow-hidden rounded-card border border-line bg-card">
      {proposals.map((proposal, index) => (
        <div
          key={proposal.number}
          className={
            arrived(String(proposal.number)) ? 'motion-safe:animate-row-in motion-reduce:animate-fade-in' : undefined
          }
        >
          <ProposalCard
            proposal={proposal}
            now={now}
            primary={firstPrimary && index === 0}
            waiting={proposal.state === 'proposed'}
          />
        </div>
      ))}
    </div>
  );
}

/** Waiting for your review: the proposals still proposed, the first with the primary Review. */
function WaitingPanel() {
  const org = useOrg();
  const factory = useFactory();
  const params = useFactoryParams();
  const waiting = useQuery(proposalsQuery(org.id, factory.id, 'proposed'));
  const proposals = waiting.data?.proposals ?? [];
  return (
    <Skeleton ready={!waiting.isPending} placeholder={<PanelShape rows={2} row="h-28" />}>
      <section aria-labelledby="waiting-h" className="flex min-w-0 flex-col gap-3">
        <div className="flex items-center gap-2">
          <h2 id="waiting-h" className="m-0 text-heading">
            Waiting for your review
          </h2>
          {proposals.length > 0 ? <Count value={proposals.length} label=" waiting for your review" /> : null}
        </div>
        {waiting.isError ? (
          <JonahError error={waiting.error} retry={() => void waiting.refetch()} />
        ) : proposals.length === 0 ? (
          <p className="m-0 text-secondary text-muted">
            Nothing waits for you.{' '}
            <Link
              to="/o/$org/f/$factory/jonah/proposals"
              params={params}
              className="text-link underline underline-offset-3 hover:text-run-ink"
            >
              All proposals
            </Link>
          </p>
        ) : (
          <ProposalList proposals={proposals} firstPrimary />
        )}
      </section>
    </Skeleton>
  );
}

/** The overview: waiting proposals, After publish and what Jonah noticed on the left, how Jonah works here on the right. */
export function JonahOverviewPage() {
  const factory = useFactory();
  const org = useOrg();
  const params = useFactoryParams();
  const overview = useQuery(jonahOverviewQuery(org.id, factory.id));
  const actions = topBarActions(params);

  return (
    <div className="@container flex min-w-0 flex-col gap-5">
      <TopBarSlot actions={actions} />
      <JonahHeader tab="overview" />
      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="flex min-w-0 flex-col gap-5">
          <WaitingPanel />
          <AfterPublishPanel />
          <Skeleton ready={!overview.isPending} placeholder={<PanelShape rows={3} />}>
            {overview.isError ? (
              <JonahError error={overview.error} retry={() => void overview.refetch()} />
            ) : overview.data && overview.data.observations.length > 0 ? (
              <NoticedSection observations={overview.data.observations} />
            ) : (
              <SectionCard title="What Jonah noticed this week">
                <p className="m-0 text-secondary text-muted">Nothing yet. Jonah reviews the line once a day.</p>
              </SectionCard>
            )}
          </Skeleton>
        </div>
        <div className="min-w-0 max-lg:order-last">
          <Skeleton ready={!overview.isPending} placeholder={<PanelShape rows={5} row="h-5" />}>
            {overview.data ? <PolicyCard overview={overview.data} /> : null}
          </Skeleton>
        </div>
      </div>
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
  const list = useQuery(proposalsQuery(org.id, factory.id, state));
  // The radio shows the choice on the click; the URL catches up once the navigation settles, and back and forward
  // set the choice again from it.
  const [chosen, setChosen] = useState(state);
  const [seen, setSeen] = useState(state);
  if (seen !== state) {
    setSeen(state);
    setChosen(state);
  }

  const filters: SegmentedOption<'all' | ProposalState>[] = [
    { value: 'all', label: 'All' },
    ...proposalStates.map((value) => ({ value, label: proposalStateLabels[value] })),
  ];
  const proposals = list.data?.proposals ?? [];

  return (
    <div className="@container flex min-w-0 flex-col gap-5">
      <TopBarSlot actions={actions} />
      <JonahHeader tab="proposals" />
      <SegmentedControl
        label="Proposal state"
        options={filters}
        value={chosen ?? 'all'}
        onChange={(value) => {
          setChosen(value === 'all' ? undefined : value);
          void navigate({
            to: '/o/$org/f/$factory/jonah/proposals',
            params,
            search: { state: value === 'all' ? undefined : value },
          });
        }}
      />
      <Skeleton ready={!list.isPending} placeholder={<PanelShape rows={3} row="h-28" />}>
        {list.isError ? (
          <JonahError error={list.error} retry={() => void list.refetch()} />
        ) : proposals.length === 0 ? (
          <EmptyState title={state === undefined ? 'No proposals yet' : 'No proposals in this state'}>
            Jonah proposes a change once a pattern repeats in sent-back runs.
          </EmptyState>
        ) : (
          <ProposalList proposals={proposals} firstPrimary={false} />
        )}
      </Skeleton>
    </div>
  );
}
