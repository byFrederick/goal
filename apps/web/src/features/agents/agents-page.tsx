// The Agents page (build/agents, ADR 0016 wave 2): the factory's defaults, the line with each station's
// agents, and each agent's model, cap and the statistics measured over the last 7 days. The definition
// comes from the running version; the statistics are one request per agent.
import { useQueries, useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { ChevronRight, Code, Plus } from 'lucide-react';
import { type ReactNode, useState } from 'react';
import { agentStatsQuery, boardQuery, versionModelQuery } from '../../api/queries';
import type { AgentStats } from '../../api/types';
import { ErrorScreen, PendingScreen } from '../../app/states';
import { TopBarSlot } from '../../app/top-bar';
import { type Model, type ModelStation, modelOf } from '../../shared/definition/model';
import { factoryTitle, useFactory, useFactoryParams } from '../../shared/factory';
import { formatPercent, formatUsd, humanize } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { NotAvailable } from '../../ui/not-available';
import { NumberRoll } from '../../ui/number-roll';
import { AddAgentDialog } from '../factory-editor/agents/agent-dialogs';
import { DraftSessionProvider, useDraftSession } from '../factory-editor/draft/use-draft';
import {
  agentNamesOf,
  agentsOf,
  initialsOf,
  lineSentence,
  nextAgentName,
  type Staffing,
  sizeLabel,
  staffingOf,
  stationsOf,
} from './agents-line';

const none = '—';

/** The page: loading, error and ready. A definition with no agents is ready, with its stations empty. */
export function AgentsPage() {
  const org = useOrg();
  const factory = useFactory();
  const running = useQuery(versionModelQuery(org.id, factory.id, 'current'));
  const names = running.data ? agentNamesOf(running.data.model) : [];
  const stats = useQueries({ queries: names.map((name) => agentStatsQuery(org.id, factory.id, name)) });
  // The live count at each station head; a board that cannot be read leaves the heads without counts.
  const board = useQuery(boardQuery(org.id, factory.id));

  if (running.isPending || board.isPending || stats.some((query) => query.isPending)) return <PendingScreen />;
  if (running.isError) return <ErrorScreen error={running.error} reset={() => void running.refetch()} />;
  const failed = stats.find((query) => query.isError);
  if (failed) return <ErrorScreen error={failed.error} reset={() => void failed.refetch()} />;

  const model = modelOf(running.data.model);
  const statsByAgent = new Map<string, AgentStats | undefined>(names.map((name, i) => [name, stats[i]?.data]));
  const counts = new Map<string, number>((board.data?.stations ?? []).map((s) => [s.id, s.waiting + s.in_progress]));
  // The draft session the New agent dialog writes to; a viewer sees the running version and no control.
  const canEdit = holds(factory, 'definition:edit');
  return (
    <DraftSessionProvider key={`${factory.id}:${canEdit}`} org={org.id} factory={factory.id} readOnly={!canEdit}>
      <Ready model={model} statsByAgent={statsByAgent} counts={counts} canEdit={canEdit} />
    </DraftSessionProvider>
  );
}

function Ready({
  model,
  statsByAgent,
  counts,
  canEdit,
}: {
  model: Model;
  statsByAgent: Map<string, AgentStats | undefined>;
  counts: Map<string, number>;
  canEdit: boolean;
}) {
  const factory = useFactory();
  const params = useFactoryParams();
  const navigate = useNavigate();
  const session = useDraftSession();
  const [adding, setAdding] = useState(false);
  const agents = agentsOf(model);
  const stations = stationsOf(model);
  const positions = stations.reduce((sum, station) => sum + staffingOf(station, agents).length, 0);
  const sentence = lineSentence(agents.length, positions, factoryTitle(factory));

  // The agent is in the draft once its save lands; the definition then opens on it.
  const added = async (name: string) => {
    await session.flush();
    await navigate({
      to: '/o/$org/f/$factory/definition/$section',
      params: { ...params, section: 'agents' },
      search: { agent: name },
    });
  };

  return (
    <>
      <TopBarSlot
        actions={[
          <ViewAsFiles key="files" />,
          canEdit ? (
            <Button key="new" variant="primary" onClick={() => setAdding(true)}>
              <Plus aria-hidden="true" />
              New agent
            </Button>
          ) : (
            <NotAvailable key="new">
              <Button variant="primary">
                <Plus aria-hidden="true" />
                New agent
              </Button>
            </NotAvailable>
          ),
        ]}
      />
      {canEdit ? <AddAgentDialog open={adding} onOpenChange={setAdding} onAdded={added} /> : null}
      <div className="flex flex-col gap-5">
        <header className="flex flex-col gap-1">
          <h1 className="m-0 text-page-title">Agents</h1>
          <p className="m-0 text-body text-muted">{sentence}</p>
        </header>
        <FactoryDefaults model={model} />
        <Line model={model} statsByAgent={statsByAgent} counts={counts} />
        <p className="m-0 text-meta text-muted">
          Copies share the station's work in progress. Change who staffs a station in the definition; every change
          publishes a new version.
        </p>
      </div>
    </>
  );
}

function ViewAsFiles() {
  const params = useFactoryParams();
  return (
    <Button asChild variant="secondary">
      <Link to="/o/$org/f/$factory/definition/$section" params={{ ...params, section: 'agents' }}>
        <Code aria-hidden="true" />
        View as files
      </Link>
    </Button>
  );
}

function FactoryDefaults({ model }: { model: Model }) {
  const params = useFactoryParams();
  const review = model.review.budget;
  const defaults = [
    { label: 'Model', value: model.agents.default_model ?? none },
    { label: 'Box size', value: sizeLabel(model.basics.size) },
    { label: 'Per item', value: review?.per_item_usd === undefined ? none : formatUsd(review.per_item_usd) },
    {
      label: 'Turns per run',
      value: review?.max_turns_per_task === undefined ? none : String(review.max_turns_per_task),
    },
    {
      label: 'Connections',
      value: model.connections.length > 0 ? model.connections.map((connection) => connection.name).join(', ') : none,
    },
  ];
  return (
    <section
      aria-labelledby="factory-defaults"
      className="flex flex-wrap items-center gap-x-7 gap-y-2 rounded-card border border-line bg-panel p-4"
    >
      <h2 id="factory-defaults" className="m-0 mr-1 text-heading">
        Factory defaults
      </h2>
      <dl className="m-0 flex flex-wrap gap-x-7 gap-y-2">
        {defaults.map((item) => (
          <div key={item.label} className="flex flex-col">
            <dt className="text-meta text-muted">{item.label}</dt>
            <dd className="m-0 text-secondary font-control text-ink">{item.value}</dd>
          </div>
        ))}
      </dl>
      <Button asChild size="sm" className="ml-auto">
        <Link to="/o/$org/f/$factory/definition/$section" params={{ ...params, section: 'basics' }}>
          Edit defaults
        </Link>
      </Button>
    </section>
  );
}

/** The line: each station in order, its agents under it. Phones stack the stations; wider screens set them in a row. */
function Line({
  model,
  statsByAgent,
  counts,
}: {
  model: Model;
  statsByAgent: Map<string, AgentStats | undefined>;
  counts: Map<string, number>;
}) {
  const stations = stationsOf(model);
  const agents = agentsOf(model);
  const reworkRate = model.line.flow?.andon?.rework_rate;
  const constraints = constraintsOf(stations, statsByAgent, model.line.flow?.constraint);

  return (
    <div className="-mx-4 overflow-x-auto px-4 md:-mx-7 md:px-7">
      <ol
        className="m-0 flex list-none flex-col gap-6 p-0 md:grid md:gap-3.5"
        style={{ gridTemplateColumns: columnsOf(stations), minWidth: minWidthOf(stations) }}
      >
        {stations.map((station, index) => {
          const staffed = staffingOf(station, agents);
          const next = nextAgentName(stations, index);
          return (
            <li key={station.id} className="flex min-w-0 flex-col gap-3 md:relative">
              <StationHead
                station={station}
                isConstraint={constraints.has(station.id)}
                first={index === 0}
                count={counts.get(station.id)}
              />
              <StationBody
                station={station}
                staffed={staffed}
                next={next}
                statsByAgent={statsByAgent}
                defaultModel={model.agents.default_model}
                reworkRate={reworkRate}
              />
            </li>
          );
        })}
      </ol>
    </div>
  );
}

/** Station widths on a wide screen, as the design's grid: narrow cells, flexible agent stations, a wider routed one. */
const columns = {
  narrow: { track: '6.5rem', min: 104 },
  agent: { track: 'minmax(0,1fr)', min: 176 },
  routed: { track: 'minmax(0,1.7fr)', min: 300 },
};
const gapPx = 14;

function columnOf(station: ModelStation) {
  if (station.route) return columns.routed;
  return station.kind === 'agent' ? columns.agent : columns.narrow;
}

function columnsOf(stations: ModelStation[]) {
  return stations.map((station) => columnOf(station).track).join(' ');
}

/** Below this width the line scrolls in its own box instead of squeezing its cards. */
function minWidthOf(stations: ModelStation[]) {
  const total = stations.reduce((sum, station) => sum + columnOf(station).min, 0);
  return `${total + gapPx * Math.max(0, stations.length - 1)}px`;
}

/** The constraint comes from the statistics; the configured one stands in when none reports it. */
function constraintsOf(
  stations: ModelStation[],
  statsByAgent: Map<string, AgentStats | undefined>,
  configured: string | undefined,
) {
  const reported = new Set<string>();
  for (const stats of statsByAgent.values()) {
    for (const station of stats?.stations ?? []) if (station.constraint) reported.add(station.id);
  }
  const known = new Set(stations.map((station) => station.id));
  const found = new Set([...reported].filter((id) => known.has(id)));
  if (found.size > 0) return found;
  return new Set(configured ? [configured] : []);
}

function StationHead({
  station,
  isConstraint,
  first,
  count,
}: {
  station: ModelStation;
  isConstraint: boolean;
  first: boolean;
  /** Items waiting and in progress here now; undefined when the board could not be read. */
  count: number | undefined;
}) {
  const load = station.wip && count !== undefined ? Math.min(1, count / station.wip) : 0;
  const done = station.kind === 'done';
  const subtitle = [
    station.wip === undefined ? null : `WIP ${station.wip}`,
    station.buffer ? `buffer ${station.buffer}` : null,
  ]
    .filter(Boolean)
    .join(', ');
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2 text-secondary font-control text-ink">
        {first ? null : <ChevronRight aria-hidden="true" className="size-3.5 shrink-0 text-muted" />}
        <h2 className="m-0 flex items-center gap-2 text-secondary font-control">
          {station.name ?? humanize(station.id)}
          {isConstraint ? (
            <span className="inline-flex animate-constraint-in items-center gap-1.5 text-meta text-amber-ink">
              <span aria-hidden="true" className="size-1.5 rounded-full bg-amber" />
              Constraint
            </span>
          ) : null}
          {done ? <span aria-hidden="true" className="size-1.5 rounded-full bg-ok" /> : null}
        </h2>
        <span aria-hidden="true" className="h-px min-w-4 flex-1 bg-line" />
      </div>
      <p className="m-0 text-page-title tabular-nums">
        {done || count === undefined ? '' : <NumberRoll value={count} />}
        {done || count === undefined ? null : <span className="sr-only"> items here now</span>}
      </p>
      <span aria-hidden="true" className={cn('h-1 overflow-hidden rounded-full bg-rule', done && 'bg-ok')}>
        <span
          className={cn(
            'block h-full origin-left bg-ink transition-transform duration-(--duration-fast) ease-(--ease-out)',
            isConstraint && 'bg-amber',
          )}
          style={{ transform: `scaleX(${load})` }}
        />
      </span>
      {subtitle ? <p className="m-0 text-meta text-muted">{subtitle}</p> : null}
    </div>
  );
}

function StationBody({
  station,
  staffed,
  next,
  statsByAgent,
  defaultModel,
  reworkRate,
}: {
  station: ModelStation;
  staffed: Staffing[];
  next: string | undefined;
  statsByAgent: Map<string, AgentStats | undefined>;
  defaultModel: string | undefined;
  reworkRate: number | undefined;
}) {
  if (station.kind === 'queue') {
    return (
      <Waiting>
        <b className="font-control text-ink">No agent</b>
        {next ? `Items wait here for ${next}` : 'Items wait here'}
      </Waiting>
    );
  }
  if (station.kind === 'human') return <Waiting>A person decides here.</Waiting>;
  if (station.kind === 'done') return <Waiting>Finished work</Waiting>;
  if (staffed.length === 0) {
    return (
      <Waiting>
        <b className="font-control text-ink">No agent</b>
        Items wait here for the station's agent
      </Waiting>
    );
  }
  const cards = (places: Staffing[]) =>
    places.map((place) => (
      <AgentCard
        key={place.key}
        place={place}
        stats={statsByAgent.get(place.agent.name)}
        defaultModel={defaultModel}
        reworkRate={reworkRate}
      />
    ));
  if (station.route) {
    return (
      <div className="flex flex-col gap-4">
        {staffed.map((place) => (
          <div key={place.key} className="flex items-center gap-3">
            <span className="flex w-21 shrink-0 items-center gap-2 text-meta font-control text-ink-soft">
              {place.lane}
              <span aria-hidden="true" className="h-px min-w-2 flex-1 bg-line" />
            </span>
            <div className="flex min-w-0 flex-1 flex-col gap-3">
              <AgentCard
                place={place}
                stats={statsByAgent.get(place.agent.name)}
                defaultModel={defaultModel}
                reworkRate={reworkRate}
              />
            </div>
          </div>
        ))}
      </div>
    );
  }
  return <div className="flex flex-col gap-3">{cards(staffed)}</div>;
}

/** A station with nothing to show but its text: dashed, as the picture's queue and done cells are. */
function Waiting({ children }: { children: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-1 rounded-card border border-dashed border-rule p-4 text-center text-secondary text-muted">
      {children}
    </div>
  );
}

function AgentCard({
  place,
  stats,
  defaultModel,
  reworkRate,
}: {
  place: Staffing;
  stats: AgentStats | undefined;
  defaultModel: string | undefined;
  reworkRate: number | undefined;
}) {
  const params = useFactoryParams();
  const { agent } = place;
  const own = agent.model !== undefined && agent.model !== '';
  const model = stats?.model ?? agent.model ?? defaultModel;
  const sentBack = stats?.sent_back.ratio;
  const rework = sentBack !== null && sentBack !== undefined && reworkRate !== undefined && sentBack > reworkRate;
  return (
    <Link
      to="/o/$org/f/$factory/agents/$agent"
      params={{ ...params, agent: agent.name }}
      className="flex flex-col gap-2 rounded-card border border-line bg-card p-4 text-ink no-underline transition-colors duration-(--duration-quick) hover:border-rule hover:shadow-raised"
    >
      <span className="flex items-center gap-2">
        <span
          aria-hidden="true"
          className="grid size-8 shrink-0 place-items-center rounded-full bg-hover text-meta font-control text-ink-soft"
        >
          {initialsOf(agent.name)}
        </span>
        <span className="min-w-0 text-heading">{agent.name}</span>
      </span>
      {agent.description ? <span className="text-secondary text-muted">{agent.description}</span> : null}
      <span className="flex flex-wrap items-center gap-2">
        {model ? <Chip>{model}</Chip> : null}
        <Chip tone={own ? 'run' : 'quiet'}>{own ? 'Own model' : 'Default'}</Chip>
        {agent.per_item_usd === undefined ? null : <Chip>Cap {formatUsd(agent.per_item_usd)}</Chip>}
      </span>
      <span className="grid grid-cols-2 gap-x-3 gap-y-2 border-t border-line pt-2 text-secondary text-muted">
        <span>
          Sent back{' '}
          <b className={cn('font-control text-ink', rework && 'text-rework-ink')}>
            {sentBack === null || sentBack === undefined ? none : formatPercent(sentBack)}
          </b>
        </span>
        <span>
          Tasks finished <b className="font-control text-ink">{stats?.tasks_finished ?? none}</b>
        </span>
        <span>
          <b className="font-control text-ink">{agent.skills?.length ?? 0}</b> skills
        </span>
        <span>
          <b className="font-control text-ink">{agent.connections?.length ?? 0}</b> connections
        </span>
      </span>
    </Link>
  );
}
