// The item page's side column, the cards that say what the item is: Details (who, which agent, where and
// its pull request), Cost against the cap, the Overlaps with other items and the Rules this run follows
// (daily-work/item). Each reads the item, its runs and events, or the factory's running version through
// apps/web/src/api only. A card with no data is left out.
import { useQueries, useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Fragment, type ReactNode } from 'react';
import { membersQuery, versionFileQuery, versionModelQuery, versionQuery } from '../../api/queries';
import type { Factory, Item, ItemEvent, Run } from '../../api/types';
import { type EffectiveRule, effectiveRules, type RuleForAgent } from '../../shared/definition/effective-rules';
import { modelOf } from '../../shared/definition/model';
import { cleanPath } from '../../shared/definition/place';
import { parseRule } from '../../shared/definition/rule-md';
import { whereAgentWorks } from '../../shared/definition/where';
import { refParam } from '../../shared/factory';
import { formatUsd, formatUsdCompact, humanize, plural } from '../../shared/format';
import { pullRequestLabel, pullRequestsOf } from '../../shared/item-state';
import { useOrg } from '../../shared/org';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { activeRun, type RunEntry, runRole, type Trace } from './timeline';

/** The run the item is in, else the one that worked it last. */
function workingRun(trace: Trace): RunEntry | undefined {
  return activeRun(trace) ?? trace.entries.findLast((e): e is RunEntry => e.kind === 'run');
}

/** The factory a ref names: everything before its last "#", the item number after it. */
function factoryOf(ref: string): string {
  return ref.slice(0, ref.lastIndexOf('#'));
}

/** The person on the created event, as the id the members list keeps: "user:<id>" in actor or in its data. */
function creatorOf(events: readonly ItemEvent[]): string | undefined {
  const created = events.find((e) => e.kind === 'created');
  if (!created) return undefined;
  const data = created.data.actor;
  const actor = created.actor.startsWith('user:') ? created.actor : typeof data === 'string' ? data : '';
  return actor.startsWith('user:') ? actor.slice('user:'.length) : undefined;
}

/** The branch an output names, from the first station output that has one. */
function branchOf(outputs: Record<string, unknown>): string | undefined {
  for (const out of Object.values(outputs)) {
    const branch = out && typeof out === 'object' ? (out as { branch?: unknown }).branch : undefined;
    if (typeof branch === 'string' && branch !== '') return branch;
  }
  return undefined;
}

/** The number a GitHub pull request link names, prefixed with the hash; the label of any other link. */
function pullRequestNumber(url: string): string {
  const github = /\/pull\/(\d+)/.exec(url);
  return github ? `#${github[1]}` : pullRequestLabel(url);
}

export function DetailsCard({
  item,
  factory,
  events,
  trace,
  runs = [],
  summary = false,
  className,
}: {
  item: Item;
  factory: Factory;
  events: readonly ItemEvent[];
  trace: Trace;
  runs?: readonly Run[];
  /** Phones: the four rows the phone's Details card shows (Working on it, Pull request, Cost, Owner). */
  summary?: boolean;
  className?: string;
}) {
  const org = useOrg();
  const members = useQuery(membersQuery(org.id));
  const creator = creatorOf(events);
  const owner = creator ? members.data?.find((m) => m.user.id === creator)?.user.name : undefined;
  const run = workingRun(trace);
  const branch = branchOf(item.outputs);
  const pulls = pullRequestsOf(
    item.outputs,
    factory.stations.map((s) => s.id),
  );
  const pullLinks = pulls.map((url, i) => (
    <Fragment key={url}>
      {i > 0 ? ', ' : null}
      <a href={url} target="_blank" rel="noreferrer" className="font-medium text-run no-underline hover:underline">
        {pullRequestNumber(url)}
      </a>
    </Fragment>
  ));

  const rows: { label: string; value: ReactNode }[] = [];
  if (summary) {
    if (run) rows.push({ label: 'Working on it', value: runRole(run) });
    if (pulls.length > 0) rows.push({ label: 'Pull request', value: pullLinks });
    rows.push({ label: 'Cost', value: <CostValue item={item} factory={factory} runs={runs} /> });
    if (owner) rows.push({ label: 'Owner', value: owner });
  } else {
    if (owner) rows.push({ label: 'Owner', value: owner });
    if (run) rows.push({ label: 'Working on it', value: runRole(run) });
    if (run?.model) rows.push({ label: 'Model', value: run.model });
    const station = item.lane ? `${humanize(item.station)}, ${item.lane} lane` : humanize(item.station);
    rows.push({ label: 'Station', value: station });
    if (branch) rows.push({ label: 'Branch', value: <span className="font-mono">{branch}</span> });
    if (pulls.length > 0) rows.push({ label: 'Pull request', value: pullLinks });
    if (item.assembly) {
      const { ref, title } = item.assembly;
      const value = (
        <>
          Part of{' '}
          <Link
            to="/o/$org/f/$factory/items/$ref"
            params={{ org: org.slug, factory: factoryOf(ref), ref: refParam(ref) }}
            className="font-medium text-run no-underline hover:underline"
          >
            {ref}
          </Link>{' '}
          {title}
        </>
      );
      rows.push({ label: 'Came from', value });
    }
  }

  return (
    <SectionCard title="Details" className={className}>
      <dl className="m-0 grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-3 gap-y-2 text-secondary">
        {rows.map(({ label, value }) => (
          <Fragment key={label}>
            <dt className="m-0 text-muted">{label}</dt>
            <dd className="m-0 min-w-0 break-words">{value}</dd>
          </Fragment>
        ))}
      </dl>
    </SectionCard>
  );
}

/** The item's cost, "$3.00 of $12" against its cap, with the cost bar under it: the phone's Details row. */
function CostValue({ item, factory, runs }: { item: Item; factory: Factory; runs: readonly Run[] }) {
  const cap = item.flow?.budget.per_item_usd ?? null;
  const segments = costByStation(
    runs,
    factory.stations.map((s) => s.id),
  );
  return (
    <>
      <span className="tabular-nums">
        {formatUsd(item.cost_usd)}
        {cap !== null ? <span className="font-normal text-muted"> of {formatUsdCompact(cap)}</span> : null}
      </span>
      {cap !== null && cap > 0 ? <CostBar segments={segments} cap={cap} className="mt-2" /> : null}
    </>
  );
}

/** The cost bar: each station's share of the cap, in the line's order. Its label names the stations. */
function CostBar({
  segments,
  cap,
  className,
}: {
  segments: { id: string; cost: number }[];
  cap: number;
  className?: string;
}) {
  const legend = segments.map((s) => `${humanize(s.id)} ${formatUsd(s.cost)}`);
  return (
    <span
      role="img"
      aria-label={`${legend.join(', ')} of a ${formatUsdCompact(cap)} cap`}
      className={cn('flex h-1.5 overflow-hidden rounded-full bg-hover', className)}
    >
      {segments.map((s, i) => (
        <span
          key={s.id}
          className={cn('h-full', i % 2 === 0 ? 'bg-run-soft' : 'bg-run')}
          style={{ width: `${(s.cost / cap) * 100}%` }}
        />
      ))}
    </span>
  );
}

/** The cost of each station's runs, in the factory's line order, the stations without cost left out. */
function costByStation(runs: readonly Run[], order: readonly string[]) {
  const totals = new Map<string, number>();
  for (const run of runs) totals.set(run.station, (totals.get(run.station) ?? 0) + run.cost_usd);
  return [...new Set([...order, ...totals.keys()])]
    .map((id) => ({ id, cost: totals.get(id) ?? 0 }))
    .filter((s) => s.cost > 0);
}

export function CostCard({
  item,
  factory,
  runs,
  className,
}: {
  item: Item;
  factory: Factory;
  runs: readonly Run[];
  className?: string;
}) {
  const cap = item.flow?.budget.per_item_usd ?? null;
  const segments = costByStation(
    runs,
    factory.stations.map((s) => s.id),
  );
  const legend = segments.map((s) => `${humanize(s.id)} ${formatUsd(s.cost)}`);

  return (
    <SectionCard
      title="Cost"
      className={className}
      aside={
        <span className="text-heading font-semibold tabular-nums">
          {formatUsd(item.cost_usd)}
          {cap !== null ? (
            <span className="text-secondary font-normal text-muted"> of {formatUsdCompact(cap)}</span>
          ) : null}
        </span>
      }
    >
      {cap !== null && cap > 0 ? <CostBar segments={segments} cap={cap} /> : null}
      {segments.length > 0 || cap !== null ? (
        <ul className="m-0 flex list-none flex-wrap gap-x-4 gap-y-1 p-0 text-meta text-muted">
          {legend.map((text) => (
            <li key={text}>{text}</li>
          ))}
          {cap !== null ? <li className="ml-auto">Stops at the cap</li> : null}
        </ul>
      ) : null}
    </SectionCard>
  );
}

export function OverlapsCard({ item, className }: { item: Item; className?: string }) {
  const org = useOrg();
  const waiting = item.waiting;
  if (!waiting || waiting.needs.length === 0) return null;

  return (
    <SectionCard title="Overlaps" className={className}>
      <ul className="m-0 flex list-none flex-col gap-3 p-0">
        {waiting.needs.map(({ ref, title, paths }) => {
          const number = ref.slice(ref.lastIndexOf('#'));
          return (
            <li key={ref}>
              <Link
                to="/o/$org/f/$factory/items/$ref"
                params={{ org: org.slug, factory: factoryOf(ref), ref: refParam(ref) }}
                className="flex items-start gap-3 text-ink no-underline"
              >
                <Chip tone="constraint" className="mt-px">
                  {number}
                </Chip>
                <span className="text-secondary">
                  {waiting.kind === 'files' ? (
                    <>
                      Also changes {paths.length > 0 ? <span className="font-mono">{paths.join(', ')}</span> : null}
                      {paths.length > 0 ? '. ' : ''}Goal merges {number} first.
                    </>
                  ) : (
                    `Waits for ${number} ${title}.`
                  )}
                </span>
              </Link>
            </li>
          );
        })}
      </ul>
    </SectionCard>
  );
}

/** Whether a rule applies to the item here: its stations, lanes and work types, where each one it names must hold. */
function appliesToItem(rule: EffectiveRule, item: Item, stationsHere: readonly string[]): boolean {
  return (
    (rule.stations.length === 0 || rule.stations.some((s) => stationsHere.includes(s))) &&
    (rule.lanes.length === 0 || (item.lane !== undefined && rule.lanes.includes(item.lane))) &&
    (rule.workTypes.length === 0 || rule.workTypes.includes(item.type))
  );
}

export function RulesCard({
  item,
  factory,
  trace,
  className,
}: {
  item: Item;
  factory: Factory;
  trace: Trace;
  className?: string;
}) {
  const org = useOrg();
  const agent = workingRun(trace)?.agent;
  // The running version, not the one the run started on: a publish during a run shows here first.
  const model = useQuery({
    ...versionModelQuery(org.id, factory.id, 'current'),
    enabled: agent !== undefined,
    select: (v) => modelOf(v.model),
  });
  const rules = model.data?.agents?.rules ?? [];
  const files = useQuery({ ...versionQuery(org.id, factory.id, 'current'), enabled: rules.length > 0 });
  const texts = useQueries({
    queries: rules.map((rule) => {
      const path = cleanPath(rule.source);
      const sha256 = files.data?.files.find((f) => f.path === path)?.sha256;
      return {
        ...versionFileQuery(org.id, factory.id, 'current', path, sha256 ?? ''),
        enabled: sha256 !== undefined,
      };
    }),
  });

  if (agent === undefined) return null;
  const title = 'Rules this run follows';
  if (model.isError) {
    return (
      <SectionCard title={title} className={className}>
        <p className="m-0 text-secondary text-muted">The rules could not be loaded.</p>
      </SectionCard>
    );
  }
  const loading =
    model.isPending ||
    (rules.length > 0 && files.isPending) ||
    texts.some((t) => t.isPending && t.fetchStatus !== 'idle');
  if (loading) {
    return (
      <SectionCard title={title} className={className} aria-busy="true">
        <p className="sr-only">Loading</p>
      </SectionCard>
    );
  }

  const line = model.data?.line ?? {};
  const forAgent: RuleForAgent[] = rules.flatMap((rule, i) => {
    const text = texts[i]?.data?.text;
    if (text === undefined) return [];
    const parsed = parseRule(text);
    return [{ name: rule.name, description: parsed.description, appliesTo: parsed.appliesTo }];
  });
  const stationsHere = whereAgentWorks(line, agent)
    .filter((p) => p.station === item.station)
    .map((p) => p.label);
  const applying = effectiveRules(line, agent, forAgent).filter((r) => appliesToItem(r, item, stationsHere));
  if (applying.length === 0) return null;

  const skills = model.data?.agents?.agents?.find((a) => a.name === agent)?.skills?.length ?? 0;
  return (
    <SectionCard title={title} className={className}>
      <ul className="m-0 flex list-none flex-wrap gap-2 p-0">
        {applying.map((rule) => (
          <li key={rule.name}>
            <Chip>{rule.name}</Chip>
          </li>
        ))}
      </ul>
      {skills > 0 ? (
        <p className="m-0 text-meta text-muted">And {plural(skills, 'skill')}, loaded when the agent needs them</p>
      ) : null}
    </SectionCard>
  );
}
