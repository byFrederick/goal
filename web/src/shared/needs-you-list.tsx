// The rows of what waits on a person and the approval card (E §10.2, Main's Needs you and Phone):
// used by the Floor's Needs you card and the phone's Items view.
import { Link } from '@tanstack/react-router';
import { ChevronRight, CircleCheck, CircleDot, TriangleAlert, Zap } from 'lucide-react';
import type { InboxEntry, Station } from '../api/types';
import { cn } from '../ui/cn';
import { refParam } from './factory';
import { formatAge, formatUsd, humanize } from './format';
import { ItemDecisions } from './item-actions';
import type { NeedLink, NeedRow, NeedTone } from './needs-you';
import { useOrg } from './org';

const tiles: Record<NeedTone, string> = {
  you: 'bg-you-tint text-you',
  rework: 'bg-rework-tint text-rework',
  run: 'bg-run-tint text-run',
  plain: 'bg-control text-ink-soft',
};

const iconProps = { size: 18, strokeWidth: 1.5, 'aria-hidden': true } as const;

function RowIcon({ kind }: { kind: NeedRow['kind'] }) {
  switch (kind) {
    case 'decisions':
    case 'decision':
      return <CircleCheck {...iconProps} />;
    case 'andon':
    case 'rework_warning':
    case 'blocked':
      return <TriangleAlert {...iconProps} />;
    case 'expedite':
      return <Zap {...iconProps} />;
    default:
      return <CircleDot {...iconProps} />;
  }
}

/** A row's link: the decisions (Items waiting on you), the board, or an item. */
function RowLink({ link, className, children }: { link: NeedLink; className: string; children: React.ReactNode }) {
  const org = useOrg();
  const params = { org: org.slug, factory: link.factory };
  switch (link.to) {
    case 'decisions':
      return (
        <Link to="/o/$org/f/$factory/items" params={params} search={{ state: 'waiting_human' }} className={className}>
          {children}
        </Link>
      );
    case 'board':
      return (
        <Link to="/o/$org/f/$factory/board" params={params} className={className}>
          {children}
        </Link>
      );
    case 'item':
      return (
        <Link to="/o/$org/f/$factory/items/$ref" params={{ ...params, ref: refParam(link.ref) }} className={className}>
          {children}
        </Link>
      );
  }
}

/** The rows, each a link with its icon tile, title and one line of detail. */
export function NeedRows({ rows, className }: { rows: readonly NeedRow[]; className?: string }) {
  return (
    <ul className={cn('m-0 flex list-none flex-col p-0', className)}>
      {rows.map((row) => (
        <li key={row.key} className="border-t border-line first:border-t-0">
          <RowLink
            link={row.link}
            className="grid min-h-14 grid-cols-[40px_minmax(0,1fr)_12px] items-center gap-3 py-3 text-ink no-underline transition-colors duration-200 hover:bg-shell"
          >
            <span className={cn('flex size-10 items-center justify-center rounded-[10px]', tiles[row.tone])}>
              <RowIcon kind={row.kind} />
            </span>
            <span className="min-w-0">
              <span className="block text-[15px] leading-snug font-medium break-words">{row.title}</span>
              {row.sub ? <span className="mt-0.5 block text-sm leading-snug text-muted">{row.sub}</span> : null}
            </span>
            <ChevronRight size={12} strokeWidth={1.75} aria-hidden="true" className="text-muted" />
          </RowLink>
        </li>
      ))}
    </ul>
  );
}

/**
 * A decision, expanded so it can be made where it is (Phone): ref, type and age, title, the brief's
 * summary and facts with the spend, a link to the trace, and the decisions the station allows.
 */
export function ApprovalCard({
  entry,
  station,
  headingLevel = 2,
}: {
  entry: InboxEntry;
  station: Pick<Station, 'kind' | 'action'> | undefined;
  headingLevel?: 2 | 3;
}) {
  const org = useOrg();
  const item = entry.item;
  if (!item) return null;

  const brief = entry.approval;
  const age = formatAge(Math.max(0, (Date.now() - Date.parse(entry.since)) / 1000));
  const Heading = headingLevel === 2 ? 'h2' : 'h3';
  const facts = [
    ...(brief?.facts ?? []).map((f) => ({ key: humanize(f.key), value: f.value })),
    { key: 'Spend', value: formatUsd(brief?.spend_usd ?? item.cost_usd) },
  ];

  return (
    <article
      aria-labelledby={`approve-${item.id}`}
      className="flex flex-col gap-3.5 rounded-card border border-line bg-card p-4"
    >
      <div className="flex items-center gap-2 text-[13px] text-muted">
        <span>
          {item.ref}, {humanize(item.type).toLowerCase()}
        </span>
        <span className="ml-auto">{age} ago</span>
      </div>
      <Heading id={`approve-${item.id}`} className="m-0 text-[21px] leading-tight font-medium tracking-[-0.015em]">
        {item.title}
      </Heading>
      {brief?.summary ? <p className="m-0 text-[15px] leading-normal">{brief.summary}</p> : null}
      <dl className="m-0 grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-sm">
        {facts.map((f) => (
          <div key={f.key} className="contents">
            <dt className="text-muted">{f.key}</dt>
            <dd className="m-0 break-words">{f.value}</dd>
          </div>
        ))}
      </dl>
      <Link
        to="/o/$org/f/$factory/items/$ref"
        params={{ org: org.slug, factory: entry.factory.name, ref: refParam(item.ref) }}
        className="inline-flex min-h-11 items-center gap-1 self-start text-sm underline transition-colors duration-200 hover:text-run"
      >
        Read the trace
        <ChevronRight size={12} strokeWidth={1.75} aria-hidden="true" />
      </Link>
      <ItemDecisions
        item={{ key: item.ref, ref: item.ref, title: item.title, state: item.state, pr_url: item.pr_url }}
        factory={entry.factory.id}
        station={station}
        layout="stack"
        sendBackLabel="Send back with a note"
      />
    </article>
  );
}
