import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useParams } from '@tanstack/react-router';
import { ChevronLeft, ExternalLink } from 'lucide-react';
import { boardQuery, itemQuery, partsQuery } from '../../api/queries';
import type { AssemblyPart, AssemblyParts, BoardLane, Item } from '../../api/types';
import { factoryTitle, itemKey, refParam, useFactory, useFactoryParams } from '../../shared/factory';
import { formatDuration, formatUsd, humanize } from '../../shared/format';
import { pullRequestLabel, stateNames } from '../../shared/item-state';
import { useOrg } from '../../shared/org';
import { SectionCard, Stat, StatGrid } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { StatusIcon } from '../../ui/status-icon';
import {
  agentLabel,
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
  done: 'bg-ok',
  working: 'bg-run',
  waiting: 'bg-target',
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
  const { spent, budget } = spend(item, parts, item.flow?.budget.per_item_usd ?? null);
  const finish = timeToFinish(parts);
  const merged = `${assembly.done} of ${assembly.total} parts merged`;
  return (
    <SectionCard title="Progress" className={className}>
      <div role="img" aria-label={merged} className="flex h-3 gap-1">
        {barSegments(parts).map((s) => (
          <span key={s.key} aria-hidden="true" className={cn('min-w-0 flex-1 rounded-[3px]', segmentTone[s.tone])} />
        ))}
      </div>
      <p className="m-0 text-[15px] tabular-nums">{merged}</p>
      <StatGrid>
        <Stat label="Lead time">
          <dd
            className="m-0 truncate text-[17px] tabular-nums"
            title={formatDuration(assembly.lead_time_seconds * 1000)}
          >
            {formatDuration(assembly.lead_time_seconds * 1000)}
          </dd>
        </Stat>
        <Stat label="Spend">
          <dd
            className="m-0 truncate text-[17px] tabular-nums"
            title={budget ? `${formatUsd(spent)} of ${formatUsd(budget)}` : formatUsd(spent)}
          >
            {formatUsd(spent)}
            {budget ? <span className="text-muted"> of {formatUsd(budget)}</span> : null}
          </dd>
        </Stat>
        <Stat label="Human touches">
          <dd className="m-0 text-[17px] tabular-nums">{item.touches?.count ?? 0}</dd>
        </Stat>
        <Stat label="Time to finish">
          <dd className="m-0 truncate text-[17px] tabular-nums">
            {!hasOpenParts(parts) ? null : finish === null ? '—' : `About ${formatDuration(finish)}`}
          </dd>
        </Stat>
      </StatGrid>
    </SectionCard>
  );
}

/** A part being worked: its title (two lines at most), its station and who works it. */
function WorkCard({ part, sub }: { part: AssemblyPart; sub: string }) {
  const link = usePartLink();
  return (
    <li className="flex min-w-0 items-start gap-2.5 rounded-control border border-line bg-surface p-3">
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
          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {w.parts.map((part) => {
              if (partTone(part) === 'working') {
                const sub = part.agent
                  ? `${humanize(part.station)} · ${agentLabel(part.agent, lanes)}`
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
      <div className="-mx-4 overflow-x-auto px-4 md:-mx-5 md:px-5">
        <table className="w-full min-w-[320px] border-collapse text-[15px]">
          <caption className="sr-only">Parts of {item}, in plan order</caption>
          <thead>
            <tr className="border-b border-line text-left text-[13px] text-muted">
              <th scope="col" className="py-2 pr-3 font-normal whitespace-nowrap">
                Part
              </th>
              <th scope="col" className="py-2 pr-3 font-normal max-md:hidden">
                Ref
              </th>
              <th scope="col" className="py-2 pr-3 font-normal">
                Title
              </th>
              <th scope="col" className="py-2 pr-3 font-normal whitespace-nowrap max-md:hidden">
                Station
              </th>
              <th scope="col" className="py-2 pr-3 font-normal whitespace-nowrap">
                State
              </th>
              <th scope="col" className="py-2 pr-3 text-right font-normal whitespace-nowrap max-md:hidden">
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
                <td className="py-2.5 pr-3 whitespace-nowrap text-muted max-md:hidden">{part.ref}</td>
                <td className="w-full max-w-0 py-2.5 pr-3">
                  <Link
                    to="/o/$org/f/$factory/items/$ref"
                    params={link(part)}
                    title={part.title}
                    className="block truncate font-medium no-underline hover:underline"
                  >
                    {part.title}
                  </Link>
                </td>
                <td className="py-2.5 pr-3 whitespace-nowrap max-md:hidden">{humanize(part.station)}</td>
                <td className="py-2.5 pr-3 whitespace-nowrap">{stateNames[part.state]}</td>
                <td className="py-2.5 pr-3 text-right whitespace-nowrap tabular-nums max-md:hidden">
                  {formatUsd(part.cost_usd)}
                </td>
                <td className="py-2.5 whitespace-nowrap max-md:hidden">
                  {part.pr_url ? (
                    <a
                      href={part.pr_url}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 text-ink underline-offset-2 hover:text-run"
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

/**
 * An item in state assembly: the request split into parts (ADR 0012). Pause and Cancel assembly
 * and Add a part are not offered: they wait for the cascade and an add-part operation.
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
  const boardName = `${factoryTitle(factory)} board`;

  return (
    <>
      <header className="flex flex-col gap-2.5">
        <Link
          to="/o/$org/f/$factory/board"
          params={params}
          className="inline-flex min-h-11 items-center gap-1.5 self-start text-[15px] text-muted no-underline md:hidden"
        >
          <ChevronLeft size={16} strokeWidth={1.75} aria-hidden="true" />
          {boardName}
        </Link>
        <nav aria-label="Breadcrumb" className="hidden text-sm text-muted md:block">
          <ol className="m-0 flex list-none gap-1.5 p-0">
            <li>
              <Link to="/o/$org/f/$factory/board" params={params} className="text-muted hover:text-run">
                {boardName}
              </Link>
            </li>
            <li aria-hidden="true">/</li>
            <li aria-current="page">{item.ref}</li>
          </ol>
        </nav>
        <div className="flex min-w-0 max-w-[820px] flex-wrap items-center gap-2">
          <Chip tone="plain">Assembly</Chip>
          <span className="text-sm text-muted">{item.ref}</span>
        </div>
        <h1
          title={item.title}
          className="line-clamp-2 m-0 max-w-[820px] text-[26px] leading-[1.15] font-normal tracking-display md:text-[42px] md:leading-[1.08]"
        >
          {item.title}
        </h1>
        {item.body ? (
          <p className="m-0 max-w-[820px] text-[15px] leading-relaxed whitespace-pre-wrap text-muted">{item.body}</p>
        ) : null}
      </header>

      <div className="flex flex-col gap-4 md:grid md:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)] md:items-start">
        <div className="flex min-w-0 flex-col gap-4">
          <ProgressCard item={item} assembly={assembly} />
          <PartsTable parts={assembly.parts} total={assembly.total} item={item.ref} />
        </div>
        <PlanCard parts={assembly.parts} lanes={lanes} />
      </div>
    </>
  );
}
