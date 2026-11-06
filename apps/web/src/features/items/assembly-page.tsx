import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useParams } from '@tanstack/react-router';
import { ExternalLink } from 'lucide-react';
import { useId, useMemo, useState } from 'react';
import { boardQuery, itemQuery, partsQuery } from '../../api/queries';
import type { AssemblyPart, AssemblyParts, BoardLane, Item } from '../../api/types';
import { usePhone, usePhoneBack } from '../../app/phone-chrome';
import { TopBarSlot } from '../../app/top-bar';
import { itemKey, refParam, useFactory, useFactoryParams } from '../../shared/factory';
import { formatDuration, formatUsd, humanize } from '../../shared/format';
import { pullRequestLabel, stateNames } from '../../shared/item-state';
import { useOrg } from '../../shared/org';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { StatStrip } from '../../ui/stat-strip';
import { StatusIcon } from '../../ui/status-icon';
import { touchLink } from '../../ui/touch-target';
import { AssemblyActions } from './assembly-actions';
import {
  agentRole,
  barSegments,
  hasOpenParts,
  type PartTone,
  partStatus,
  partTone,
  planWaves,
  spend,
  timeToFinish,
} from './assembly-plan';

const segmentTone: Record<PartTone, string> = {
  done: 'bg-ink',
  working: 'bg-rule',
  waiting: 'bg-hover',
  cancelled: 'bg-line',
};

/** The part's own page, named by its ref. */
function usePartLink() {
  const params = useFactoryParams();
  return (part: Pick<AssemblyPart, 'ref'>) => ({ ...params, ref: refParam(part.ref) });
}

/** The progress card: one segment per part, the counts, the lead time, the spend, the touches, the finish. */
function ProgressCard({ item, assembly, className }: { item: Item; assembly: AssemblyParts; className?: string }) {
  const { parts } = assembly;
  const spent = spend(item, parts);
  const finish = timeToFinish(parts);
  const merged = `${assembly.done} of ${assembly.total} parts merged`;
  return (
    <SectionCard title="Progress" className={className}>
      <div role="img" aria-label={merged} className="flex h-3 gap-1">
        {barSegments(parts).map((s) => (
          <span key={s.key} aria-hidden="true" className={cn('min-w-0 flex-1 rounded-chip', segmentTone[s.tone])} />
        ))}
      </div>
      <p className="m-0 text-[15px] tabular-nums">{merged}</p>
      <StatStrip
        narrow
        label="Assembly figures"
        stats={[
          { label: 'Lead time', value: formatDuration(assembly.lead_time_seconds * 1000) },
          { label: 'Spend', value: formatUsd(spent) },
          { label: 'Human touches', value: item.touches?.count ?? 0 },
          {
            label: 'Time to finish',
            value: !hasOpenParts(parts) ? null : finish === null ? null : `About ${formatDuration(finish)}`,
          },
        ]}
      />
    </SectionCard>
  );
}

/** A part being worked: its title (two lines at most), its station and who works it. */
function WorkCard({ part, sub }: { part: AssemblyPart; sub: string }) {
  const link = usePartLink();
  return (
    <li className="flex min-w-0 items-start gap-2.5">
      <StatusIcon status="working" className="mt-0.5" />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <Link
          to="/o/$org/f/$factory/items/$ref"
          params={link(part)}
          title={part.title}
          className="line-clamp-2 text-[15px] leading-snug font-medium no-underline hover:underline"
        >
          {part.title}
        </Link>
        <span className="truncate text-[13px] text-muted" title={sub}>
          {sub}
        </span>
      </div>
    </li>
  );
}

/** A part not being worked: one line, its status icon, and what it waits for. */
function PartRow({ part, detail }: { part: AssemblyPart; detail: string }) {
  const link = usePartLink();
  return (
    <li className="flex min-w-0 items-center gap-2.5 text-[15px]">
      <StatusIcon status={partStatus(part)} />
      <Link
        to="/o/$org/f/$factory/items/$ref"
        params={link(part)}
        title={part.title}
        className="min-w-0 flex-1 truncate no-underline hover:underline"
      >
        {part.title}
      </Link>
      <span className="max-w-[45%] shrink truncate text-[13px] text-muted" title={detail}>
        {detail}
      </span>
    </li>
  );
}

/** The plan in waves: a card for each part being worked, a row for every other part. */
function PlanCard({
  parts,
  lanes,
  className,
}: {
  parts: AssemblyPart[];
  lanes: readonly BoardLane[];
  className?: string;
}) {
  const waves = planWaves(parts);
  return (
    <SectionCard
      title="Plan"
      description="Parts in waves. A part waits for the parts it needs to be merged."
      className={className}
    >
      {waves.map((w) => (
        <section key={w.wave} aria-label={`Wave ${w.wave + 1}`} className="flex flex-col gap-2">
          <h3 className="m-0 text-[13px] font-normal text-muted">Wave {w.wave + 1}</h3>
          <ul className="m-0 flex list-none flex-col gap-3 p-0">
            {w.parts.map((part) => {
              if (partTone(part) === 'working') {
                const sub = part.agent
                  ? `${humanize(part.station)}, ${agentRole(part.agent, lanes)}`
                  : humanize(part.station);
                return <WorkCard key={part.id} part={part} sub={sub} />;
              }
              const detail =
                partTone(part) === 'done'
                  ? 'Merged'
                  : part.waiting_on.length > 0
                    ? `Waiting for ${part.waiting_on.join(', ')}`
                    : stateNames[part.state];
              return <PartRow key={part.id} part={part} detail={detail} />;
            })}
          </ul>
        </section>
      ))}
    </SectionCard>
  );
}

/**
 * Ref, Station and Cost are columns only while the table's box is 896 px (4xl) wide; below that they sit on a
 * second line under the title, so the title keeps the leftover width. Both classes name the same width.
 */
const partsColumn = 'hidden @4xl:table-cell';
const partsLine = '@4xl:hidden';

/** Every part in plan order, with its state, cost and pull request. */
function PartsTable({
  parts,
  total,
  item,
  className,
}: {
  parts: AssemblyPart[];
  total: number;
  item: string;
  className?: string;
}) {
  const link = usePartLink();
  return (
    <SectionCard title="Parts" className={className}>
      <div className="@container relative -mx-4 overflow-x-auto px-4 md:-mx-5 md:px-5">
        <table className="w-full min-w-[320px] border-collapse text-[15px]">
          <caption className="sr-only">Parts of {item}, in plan order</caption>
          <thead>
            <tr className="border-b border-line text-left text-[13px] text-muted">
              <th scope="col" className="py-2 pr-3 font-normal whitespace-nowrap">
                Part
              </th>
              <th scope="col" className={cn('py-2 pr-3 font-normal', partsColumn)}>
                Ref
              </th>
              <th scope="col" className="py-2 pr-3 font-normal">
                Title
              </th>
              <th scope="col" className={cn('min-w-20 py-2 pr-3 font-normal whitespace-nowrap', partsColumn)}>
                Station
              </th>
              <th scope="col" className="min-w-[4.5rem] py-2 pr-3 font-normal whitespace-nowrap">
                State
              </th>
              <th scope="col" className={cn('min-w-[3.75rem] py-2 pr-3 text-right font-normal whitespace-nowrap', partsColumn)}>
                Cost
              </th>
              <th scope="col" className="py-2 font-normal whitespace-nowrap max-md:hidden">
                Pull request
              </th>
            </tr>
          </thead>
          <tbody>
            {parts.map((part) => (
              <tr key={part.id} className="border-b border-line align-top last:border-b-0">
                <td className="py-2.5 pr-3 whitespace-nowrap text-muted tabular-nums">
                  {part.index} of {total}
                </td>
                <td className={cn('py-2.5 pr-3 whitespace-nowrap text-muted', partsColumn)}>{part.ref}</td>
                <td className="w-full max-w-0 min-w-24 py-2.5 pr-3 @2xl:min-w-72">
                  <Link
                    to="/o/$org/f/$factory/items/$ref"
                    params={link(part)}
                    title={part.title}
                    className={cn('block truncate font-medium no-underline hover:underline', touchLink)}
                  >
                    {part.title}
                  </Link>
                  <div className={cn('mt-1 flex flex-wrap gap-x-3 text-secondary text-muted', partsLine)}>
                    <span>
                      <span className="sr-only">Ref: </span>
                      {part.ref}
                    </span>
                    <span>
                      <span className="sr-only">Station: </span>
                      {humanize(part.station)}
                    </span>
                    <span className="tabular-nums">
                      <span className="sr-only">Cost: </span>
                      {formatUsd(part.cost_usd)}
                    </span>
                  </div>
                </td>
                <td className={cn('py-2.5 pr-3 whitespace-nowrap', partsColumn)}>{humanize(part.station)}</td>
                <td className="py-2.5 pr-3 whitespace-nowrap">{stateNames[part.state]}</td>
                <td className={cn('py-2.5 pr-3 text-right whitespace-nowrap tabular-nums', partsColumn)}>
                  {formatUsd(part.cost_usd)}
                </td>
                <td className="py-2.5 whitespace-nowrap max-md:hidden">
                  {part.pr_url ? (
                    <a
                      href={part.pr_url}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 text-ink underline-offset-2 hover:text-link"
                    >
                      {pullRequestLabel(part.pr_url)}
                      <ExternalLink size={12} strokeWidth={1.5} aria-hidden="true" />
                    </a>
                  ) : (
                    <span className="text-muted">—</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </SectionCard>
  );
}

/** The longest brief shown whole; a longer one is clamped to three lines under a Show more button. */
const briefLimit = 240;

/** The brief, collapsed by default (MOTION.md, Accordion and disclosure: the full text opens under the clamp). */
function Brief({ body }: { body: string }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const long = body.length > briefLimit || body.split('\n').length > 3;
  if (!long)
    return (
      <p className="m-0 max-w-[820px] text-[15px] leading-relaxed whitespace-pre-wrap text-muted [overflow-wrap:anywhere]">
        {body}
      </p>
    );
  return (
    <div className="flex w-full min-w-0 max-w-[820px] flex-col items-start gap-1">
      <div
        id={id}
        data-state={open ? 'open' : 'closed'}
        className="motion-disclosure motion-disclosure-peek w-full min-w-0 grid-cols-[minmax(0,1fr)] text-[15px]"
      >
        <div className="min-w-0">
          <p className="m-0 text-[15px] leading-relaxed whitespace-pre-wrap text-muted [overflow-wrap:anywhere]">
            {body}
          </p>
        </div>
      </div>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen((o) => !o)}
        className="inline-flex min-h-11 cursor-pointer items-center text-[15px] text-ink underline-offset-2 hover:underline"
      >
        {open ? 'Show less' : 'Show more'}
      </button>
    </div>
  );
}

/**
 * An assembly: an item whose parts exist, in any state: the request split into parts (ADR 0012). The header offers Add a part
 * (AddPartDialog: one more part, queued at the assembly's station), Pause (Resume once every open part
 * is blocked) and Cancel assembly, the last two acting on the open parts (AssemblyActions).
 */
export function AssemblyPage() {
  const factory = useFactory();
  const params = useFactoryParams();
  const { ref } = useParams({ from: '/o/$org/f/$factory/items/$ref' });
  const key = itemKey(factory.name, ref);
  const org = useOrg();
  const { data: item } = useSuspenseQuery(itemQuery(org.id, key));
  const { data: assembly } = useSuspenseQuery(partsQuery(org.id, key));
  // The board's lanes name who works each part. Read only when the board is already loaded: the
  // page does not wait for it, and the agents' names read as words until it is.
  const board = useQuery({ ...boardQuery(org.id, factory.id), enabled: false }).data;
  const lanes = board?.stations.flatMap((s) => s.lanes ?? []) ?? [];
  // The station the assembly was planned at: the part Add a part queues there names its agents.
  const stationLanes = useMemo(
    () => board?.stations.find((s) => s.id === item.station)?.lanes ?? [],
    [board, item.station],
  );
  const phone = usePhone();
  // From 768 px the top bar holds every action. The phone's bar keeps the primary one (Resume, else Add a part)
  // and the others sit under the brief.
  const actions = useMemo(
    () => (
      <AssemblyActions
        item={item}
        itemKey={key}
        parts={assembly.parts}
        factory={factory}
        stationLanes={stationLanes}
        part={phone ? 'primary' : 'all'}
      />
    ),
    [item, key, assembly.parts, factory, stationLanes, phone],
  );
  const back = usePhoneBack('Back to Board', '/o/$org/f/$factory/board', params);

  return (
    <>
      <TopBarSlot crumb={`#${item.number}`} actions={actions} back={back} />
      <header className="flex flex-col gap-2.5">
        <div className="flex min-w-0 max-w-[820px] flex-wrap items-center gap-2">
          <Chip tone="plain">Assembly</Chip>
          <span className="text-sm text-muted">{item.ref}</span>
        </div>
        <h1 title={item.title} className="line-clamp-2 m-0 max-w-[820px] text-detail-title tracking-display">
          {item.title}
        </h1>
        {item.body ? <Brief body={item.body} /> : null}
        {phone ? (
          <AssemblyActions
            item={item}
            itemKey={key}
            parts={assembly.parts}
            factory={factory}
            stationLanes={stationLanes}
            part="rest"
          />
        ) : null}
      </header>

      <div className="flex flex-col gap-4 md:grid md:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)] md:items-start">
        <ProgressCard item={item} assembly={assembly} />
        <PlanCard parts={assembly.parts} lanes={lanes} />
      </div>
      <PartsTable parts={assembly.parts} total={assembly.total} item={item.ref} />
    </>
  );
}
