// Step 8, "Watch your first item" (docs/design/screens/get-started/first-item.jpg; ADR 0032 decision 3). A factory
// with no items gets its starter item (factory.yaml's starter.item, from the published version) submitted once,
// through submitItem. The submission is a query under its own key with no refetch, so a re-render or a remount
// after it does not submit again. A factory that already has items shows its newest one. A factory with no starter
// item shows the empty state and submits nothing. The item card refreshes from the stream's item events (the keys
// the stream invalidates), never by polling.
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Check } from 'lucide-react';
import { submitItem } from '../../api/client';
import { eventsQuery, factoryQuery, intakeQuery, itemQuery, itemsQuery, versionModelQuery } from '../../api/queries';
import type { Factory, FactoryModel, Intake, Item, ItemEvent, OrgSummary } from '../../api/types';
import { modelOf } from '../../shared/definition/model';
import { formatUsd, humanize } from '../../shared/format';
import { pullRequestLabel, pullRequestsOf } from '../../shared/item-state';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { stationLabel } from '../factory-editor/line/labels';
import { touchSentence } from '../items/touch-words';
import type { OnboardingSearch } from './code-search';
import { OnboardingLayout } from './onboarding-layout';
import { stepPanel } from './step-panel';

/** The item events the timeline shows: what was made, moved and decided, and notes. Run progress is not shown here. */
const timelineKinds = new Set([
  'created',
  'submitted',
  'moved',
  'approved',
  'gate_approved',
  'rejected',
  'gate_rejected',
  'blocked',
  'paused',
  'cancelled',
  'done',
  'note',
]);

const decidedKinds = new Set(['approved', 'gate_approved', 'done']);

export function FirstItemStep({ org, search }: { org: OrgSummary; search: OnboardingSearch }) {
  const factoryId = search.factory;
  const factory = useQuery({ ...factoryQuery(org.id, factoryId ?? ''), enabled: factoryId !== undefined });

  if (!factoryId) {
    return <NoFactory org={org} search={search} message="Choose a template first, then watch your first item." />;
  }
  if (factory.isError) {
    return <NoFactory org={org} search={search} message="This factory can’t be read. Choose a template again." />;
  }
  if (factory.isPending) return <Loading org={org} search={search} factory={factoryId} />;
  return <Watch org={org} search={search} factoryId={factoryId} factory={factory.data} />;
}

/** Back to step 7 while the factory exists; back to step 3 (which makes one) when it does not. */
function Back({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory?: string }) {
  return (
    <Button asChild size="lg" variant="secondary">
      {factory ? (
        <Link to="/o/$org/get-started/$step" params={{ org: org.slug, step: 'people' }} search={{ factory }}>
          Back
        </Link>
      ) : (
        <Link
          to="/o/$org/get-started/$step"
          params={{ org: org.slug, step: 'factory' }}
          search={{ connection: search.connection, owner: search.owner, repos: search.repos }}
        >
          Back
        </Link>
      )}
    </Button>
  );
}

function NoFactory({ org, search, message }: { org: OrgSummary; search: OnboardingSearch; message: string }) {
  return (
    <OnboardingLayout step="first-item" org={org} back={<Back org={org} search={search} />}>
      <div className="flex flex-col gap-2">
        <h1 className="m-0 font-numeral text-kpi tracking-display">Watch your first item</h1>
      </div>
      <Callout tone="rework">{message}</Callout>
    </OnboardingLayout>
  );
}

function Loading({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory: string }) {
  return (
    <OnboardingLayout step="first-item" org={org} back={<Back org={org} search={search} factory={factory} />}>
      <div aria-busy="true">
        <span role="status" className="sr-only">
          Loading
        </span>
      </div>
    </OnboardingLayout>
  );
}

/** Reads the published model, the intake (for the panel) and the newest item; loading until all three are there. */
function Watch({
  org,
  search,
  factoryId,
  factory,
}: {
  org: OrgSummary;
  search: OnboardingSearch;
  factoryId: string;
  factory: Factory;
}) {
  const model = useQuery(versionModelQuery(org.id, factoryId, 'current'));
  const intake = useQuery(intakeQuery(org.id, factoryId));
  const items = useQuery(itemsQuery(org.id, factoryId, { limit: 1 }));

  if (model.isPending || intake.isPending || items.isPending) {
    return <Loading org={org} search={search} factory={factoryId} />;
  }
  if (model.isError || intake.isError || items.isError) {
    const retry = () => {
      void model.refetch();
      void intake.refetch();
      void items.refetch();
    };
    return (
      <OnboardingLayout step="first-item" org={org} back={<Back org={org} search={search} factory={factoryId} />}>
        <div className="flex flex-col gap-2">
          <h1 className="m-0 font-numeral text-kpi tracking-display">Watch your first item</h1>
        </div>
        <Callout tone="rework">
          <span>The factory can’t be read.</span>
          <Button type="button" size="sm" variant="secondary" className="mt-2" onClick={retry}>
            Retry
          </Button>
        </Callout>
      </OnboardingLayout>
    );
  }

  const newest = items.data.items[0];
  const starter = model.data.model.starter?.item;
  const page = { org, search, factoryId, factory, model: model.data.model, intake: intake.data };

  if (newest) return <Watched {...page} itemId={newest.id} />;
  if (!starter) return <Empty {...page} />;
  return <Submitted {...page} starter={starter} />;
}

/**
 * Submits the starter item once. Its key is outside the org's prefix, so no stream event refetches it, and it
 * never refetches on mount or focus: a remount after the submission reads the cached item.
 */
function Submitted({
  starter,
  ...page
}: Omit<WatchPage, 'itemId'> & { starter: { title: string; type?: string; body?: string } }) {
  const client = useQueryClient();
  const { org, factoryId } = page;
  const submitted = useQuery({
    queryKey: ['onboarding', 'starter-item', org.id, factoryId],
    queryFn: async () => {
      const item = await submitItem(org.id, factoryId, {
        title: starter.title,
        ...(starter.type ? { type: starter.type } : {}),
        ...(starter.body ? { body: starter.body } : {}),
      });
      void client.invalidateQueries({ queryKey: itemsQuery(org.id, factoryId, { limit: 1 }).queryKey });
      return item;
    },
    staleTime: Number.POSITIVE_INFINITY,
    gcTime: Number.POSITIVE_INFINITY,
    retry: false,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

  if (submitted.isPending) return <Loading org={org} search={page.search} factory={factoryId} />;
  if (submitted.isError) {
    return (
      <OnboardingLayout step="first-item" org={org} back={<Back org={org} search={page.search} factory={factoryId} />}>
        <div className="flex flex-col gap-2">
          <h1 className="m-0 font-numeral text-kpi tracking-display">Watch your first item</h1>
        </div>
        <Callout tone="rework">
          <span>The first item can’t be submitted.</span>
          <Button type="button" size="sm" variant="secondary" className="mt-2" onClick={() => void submitted.refetch()}>
            Retry
          </Button>
        </Callout>
      </OnboardingLayout>
    );
  }
  return <Watched {...page} itemId={submitted.data.id} />;
}

/** What the steps pass down: the organization, the factory and its published model, and the intake for the panel. */
interface WatchPage {
  org: OrgSummary;
  search: OnboardingSearch;
  factoryId: string;
  factory: Factory;
  model: FactoryModel;
  intake: Intake;
  itemId?: string;
}

/** A factory without a starter item: the layout, with the empty state and the way to the Floor. */
function Empty({ org, search, factoryId, factory, model, intake }: WatchPage) {
  return (
    <OnboardingLayout
      step="first-item"
      org={org}
      back={<Back org={org} search={search} factory={factoryId} />}
      action={<FloorButton org={org} factory={factory.name} />}
      preview={stepPanel({ org, search, factory, model, intake })}
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 font-numeral text-kpi tracking-display">Watch your first item</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">
          This factory has no starter item, so nothing runs yet. Add an item from the Floor when you are ready.
        </p>
      </div>
    </OnboardingLayout>
  );
}

function FloorButton({ org, factory }: { org: OrgSummary; factory: string }) {
  return (
    <Button asChild size="lg" variant="primary">
      <Link to="/o/$org/f/$factory" params={{ org: org.slug, factory }}>
        Go to the Floor
      </Link>
    </Button>
  );
}

/** The item's card, read by id; the stream's item events refresh it. */
function Watched(props: WatchPage & { itemId: string }) {
  const client = useQueryClient();
  const { org, search, factoryId, itemId } = props;
  const item = useQuery(itemQuery(org.id, itemId));
  const events = useQuery(eventsQuery(client, org.id, itemId));

  if (item.isPending || events.isPending) return <Loading org={org} search={search} factory={factoryId} />;
  if (item.isError || events.isError) {
    return (
      <OnboardingLayout step="first-item" org={org} back={<Back org={org} search={search} factory={factoryId} />}>
        <Callout tone="rework">
          <span>The item can’t be read.</span>
          <Button
            type="button"
            size="sm"
            variant="secondary"
            className="mt-2"
            onClick={() => {
              void item.refetch();
              void events.refetch();
            }}
          >
            Retry
          </Button>
        </Callout>
      </OnboardingLayout>
    );
  }
  return <Ready {...props} item={item.data} events={events.data} />;
}

/** One station of the bar: done, the item's station, or still to come. */
type StationState = 'done' | 'current' | 'todo';

const stateWords: Record<StationState, string> = { done: 'done', current: 'current step', todo: 'not started' };

function StationBars({ stations }: { stations: readonly { id: string; label: string; state: StationState }[] }) {
  return (
    <ol aria-label="Stations" className="m-0 flex list-none gap-2 p-0">
      {stations.map((s) => (
        <li
          key={s.id}
          aria-current={s.state === 'current' ? 'step' : undefined}
          className="flex min-w-0 flex-1 flex-col gap-2 text-meta font-control"
        >
          <span
            aria-hidden="true"
            className={cn(
              'block h-1 rounded-full',
              s.state === 'done' ? 'bg-run-soft' : s.state === 'current' ? 'bg-run' : 'bg-line',
            )}
          />
          <span className={cn('truncate', s.state === 'current' ? 'font-semibold text-run-ink' : 'text-ink-soft')}>
            {s.label}
            <span className="sr-only">{`, ${stateWords[s.state]}`}</span>
          </span>
        </li>
      ))}
    </ol>
  );
}

/** Elapsed time since the item was created, as the picture writes it: "0:12", "4:30", "12:05". */
function clockOf(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

/** An event in words: "Created from the setup", "Spec accepted", "Moved it to Build". */
function eventWords(e: ItemEvent, labelOf: (station: string) => string): string {
  if (e.kind === 'created' || e.kind === 'submitted') return 'Created from the setup';
  if (e.kind === 'approved' || e.kind === 'gate_approved') {
    return `${e.station ? labelOf(e.station) : 'The item'} accepted`;
  }
  const words = touchSentence({ kind: e.kind }, e.data);
  return words.charAt(0).toUpperCase() + words.slice(1);
}

function Ready({
  org,
  search,
  factoryId,
  factory,
  model,
  intake,
  item,
  events,
}: WatchPage & { item: Item; events: readonly ItemEvent[] }) {
  const { line } = modelOf(model);
  const stations = line.stations ?? [];
  const labelOf = (id: string) => {
    const s = stations.find((x) => x.id === id);
    return s ? stationLabel(s) : humanize(id);
  };

  // The clock is read when the card renders; a stream event renders it again, so nothing polls.
  const now = Date.now();
  const created = Date.parse(item.created_at);
  const minutes = Math.max(0, Math.floor((now - created) / 60_000));
  const current = stations.findIndex((s) => s.id === item.station);
  const barStations = stations.map((s, i) => ({
    id: s.id,
    label: stationLabel(s),
    state: (i < current ? 'done' : i === current ? 'current' : 'todo') as StationState,
  }));
  const timeline = events.filter((e) => timelineKinds.has(e.kind));
  const pullRequest = pullRequestsOf(
    item.outputs,
    stations.map((s) => s.id),
  )[0];
  const pullNumber = pullRequest ? /\/pull\/(\d+)/.exec(pullRequest)?.[1] : undefined;
  const repo = search.repos?.[0];
  const station = labelOf(item.station);

  return (
    <OnboardingLayout
      step="first-item"
      org={org}
      back={
        <>
          <Back org={org} search={search} factory={factoryId} />
          {pullRequest ? (
            <Button asChild size="lg" variant="ghost">
              <a href={pullRequest} target="_blank" rel="noopener noreferrer">
                {pullNumber ? `Open pull request #${pullNumber}` : `Open ${pullRequestLabel(pullRequest)}`}
              </a>
            </Button>
          ) : null}
        </>
      }
      action={<FloorButton org={org} factory={factory.name} />}
      preview={stepPanel({
        org,
        search,
        factory,
        model,
        intake,
        item: {
          number: item.number,
          title: item.title,
          station: item.station,
          minutes,
          cost: item.cost_usd,
        },
      })}
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 font-numeral text-kpi tracking-display">Watch your first item</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">
          {`A small, safe change runs through the whole line${repo ? ` on ${repo}` : ''}, so you see every station work before real requests arrive.`}
        </p>
      </div>

      <section aria-label="Item" className="flex min-w-0 flex-col gap-4 rounded-card border border-line p-5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-control text-secondary text-ink-soft">{`#${item.number}`}</span>
          <Chip>{item.type}</Chip>
          {item.lane ? <Chip>{item.lane}</Chip> : null}
          <span className="ml-auto">
            <Chip tone="run" dot>{`At ${station}, ${minutes} min`}</Chip>
          </span>
        </div>
        <h2 className="m-0 text-heading font-semibold text-ink">{item.title}</h2>
        <StationBars stations={barStations} />

        <ol aria-label="Timeline" className="m-0 flex list-none flex-col p-0">
          {timeline.map((e) => (
            <li key={e.id} className="flex items-center gap-4 border-t border-line py-2 text-body">
              <span className="w-12 flex-none text-secondary text-muted tabular-nums">
                {clockOf(Date.parse(e.at) - created)}
              </span>
              {decidedKinds.has(e.kind) ? (
                <Check aria-hidden="true" className="size-3.5 flex-none text-run" />
              ) : (
                <span aria-hidden="true" className="size-1.5 flex-none rounded-full bg-muted" />
              )}
              <span className="min-w-0 break-words text-ink">{eventWords(e, labelOf)}</span>
            </li>
          ))}
        </ol>

        <div className="flex items-center justify-between border-t border-line pt-3 text-body">
          <span className="text-ink-soft">Cost so far</span>
          <span className="font-semibold tabular-nums text-ink">{formatUsd(item.cost_usd)}</span>
        </div>
      </section>

      <p className="m-0 text-secondary text-muted">
        Review merges it when it passes. Nothing else in the repository changes.
      </p>
    </OnboardingLayout>
  );
}
