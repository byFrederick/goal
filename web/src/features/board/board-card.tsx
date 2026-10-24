import { Link } from '@tanstack/react-router';
import { ExternalLink, Zap } from 'lucide-react';
import type { ItemCard } from '../../api/types';
import { refParam, useFactoryParams } from '../../shared/factory';
import { formatAge, formatUsd, humanize } from '../../shared/format';
import { describeState, pullRequestLabel, toneText } from '../../shared/item-state';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';

/**
 * One work item on the board. The title is the link (stretched over the card); the pull request
 * link sits above it so both stay separately focusable.
 */
export function BoardCard({
  item,
  atConstraint,
  className,
}: {
  item: ItemCard;
  atConstraint: boolean;
  className?: string;
}) {
  const params = useFactoryParams();
  const state = describeState(item, { atConstraint });
  const buffered = atConstraint && (item.state === 'ready' || item.state === 'queued');

  return (
    <article
      className={cn(
        'relative flex flex-col gap-2 rounded-control border bg-surface p-3 transition-[border-color,box-shadow] duration-200',
        'hover:border-faint hover:shadow-lift has-[a:focus-visible]:outline-2 has-[a:focus-visible]:outline-offset-2 has-[a:focus-visible]:outline-run',
        buffered
          ? 'border-amber-line bg-amber-tint'
          : item.state === 'waiting_human'
            ? 'border-you-line'
            : item.state === 'blocked'
              ? 'border-rework-line'
              : 'border-hairline',
        className,
      )}
    >
      <div className="flex justify-between gap-2 text-xs text-muted tabular-nums">
        <span>{item.ref}</span>
        <span>{formatAge(item.age_seconds)}</span>
      </div>
      <h3 className="m-0 text-sm leading-snug font-medium">
        <Link
          to="/o/$org/f/$factory/items/$ref"
          params={{ ...params, ref: refParam(item.ref) }}
          className="no-underline outline-none after:absolute after:inset-0 after:rounded-control"
        >
          {item.title}
        </Link>
      </h3>
      <div className="flex flex-wrap items-center gap-1.5">
        <Chip tone="quiet">{humanize(item.type)}</Chip>
        {item.lane ? <Chip tone="quiet">{item.lane}</Chip> : null}
        {item.rework_count > 0 ? <Chip tone="rework">Sent back {item.rework_count}×</Chip> : null}
      </div>
      <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1">
        <span className={cn('text-xs', toneText[state.tone])}>{state.text}</span>
        {item.pr_url ? (
          <a
            href={item.pr_url}
            target="_blank"
            rel="noreferrer"
            className="relative z-10 inline-flex items-center gap-1 text-xs text-ink underline-offset-2 hover:text-run"
          >
            {pullRequestLabel(item.pr_url)}
            <ExternalLink size={12} strokeWidth={1.5} aria-hidden="true" />
            <span className="sr-only">(opens in a new tab)</span>
          </a>
        ) : null}
      </div>
    </article>
  );
}

/** An expedited item: it skips the queue, so it rides in its own lane above the columns. */
export function ExpediteCard({ item }: { item: ItemCard }) {
  const params = useFactoryParams();
  return (
    <article className="relative grid grid-cols-[36px_minmax(0,1fr)] items-center gap-3 rounded-control border border-rework-line bg-surface p-3 transition-shadow duration-200 hover:shadow-lift has-[a:focus-visible]:outline-2 has-[a:focus-visible]:outline-offset-2 has-[a:focus-visible]:outline-run md:grid-cols-[36px_minmax(0,1fr)_auto]">
      <span
        aria-hidden="true"
        className="flex size-9 items-center justify-center rounded-control bg-rework-tint text-rework"
      >
        <Zap size={16} strokeWidth={1.5} />
      </span>
      <span className="min-w-0">
        <Link
          to="/o/$org/f/$factory/items/$ref"
          params={{ ...params, ref: refParam(item.ref) }}
          className="block text-[15px] leading-snug font-medium no-underline outline-none after:absolute after:inset-0 after:rounded-control"
        >
          {item.title}
        </Link>
        <span className="mt-0.5 block text-[13px] text-muted">
          {item.ref}, {humanize(item.type)}, {item.agent ? `${item.agent} at ` : ''}
          {humanize(item.station)}
        </span>
      </span>
      <span className="col-start-2 text-sm whitespace-nowrap text-muted md:col-start-auto">
        {formatAge(item.age_seconds)}, {formatUsd(item.cost_usd)}
      </span>
    </article>
  );
}
