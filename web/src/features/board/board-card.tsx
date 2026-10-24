import { Link } from '@tanstack/react-router';
import { ExternalLink, Zap } from 'lucide-react';
import type { ItemCard } from '../../api/types';
import { refParam, useFactoryParams } from '../../shared/factory';
import { formatAge, formatUsd, humanize } from '../../shared/format';
import { pullRequestLabel, toneText } from '../../shared/item-state';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { agentCopy, cardState } from './stats';

/**
 * One work item on the board. The title is the link (stretched over the card); the pull request
 * link sits above it so both stay separately focusable. `compact` is the phone's list row: the
 * reference, type and age on one line, no chips.
 */
export function BoardCard({ item, compact = false }: { item: ItemCard; compact?: boolean }) {
  const params = useFactoryParams();
  const state = cardState(item);
  const age = formatAge(item.age_seconds);

  return (
    <article
      className={cn(
        'relative flex flex-col rounded-control border bg-surface transition-[border-color,box-shadow] duration-200',
        'hover:border-faint hover:shadow-lift has-[a:focus-visible]:outline-2 has-[a:focus-visible]:outline-offset-2 has-[a:focus-visible]:outline-run',
        compact ? 'gap-1 p-3' : 'gap-2 p-3',
        item.in_buffer
          ? 'border-amber-line bg-amber-tint'
          : item.state === 'waiting_human'
            ? 'border-you-line'
            : item.state === 'blocked'
              ? 'border-rework-line'
              : 'border-hairline',
      )}
    >
      {compact ? (
        <span className="text-xs text-muted tabular-nums">
          {item.ref}, {humanize(item.type)}, {age}
        </span>
      ) : (
        <div className="flex justify-between gap-2 text-xs text-muted tabular-nums">
          <span>{item.ref}</span>
          <span>{age}</span>
        </div>
      )}
      <h3 className={cn('m-0 leading-snug font-medium', compact ? 'text-[15px]' : 'text-sm')}>
        <Link
          to="/o/$org/f/$factory/items/$ref"
          params={{ ...params, ref: refParam(item.ref) }}
          className="no-underline outline-none after:absolute after:inset-0 after:rounded-control"
        >
          {item.title}
        </Link>
      </h3>
      {compact ? null : (
        <div className="flex flex-wrap items-center gap-1.5">
          <Chip tone="quiet">{humanize(item.type)}</Chip>
          {item.lane ? <Chip tone="quiet">{item.lane}</Chip> : null}
        </div>
      )}
      <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1">
        <span className={cn(compact ? 'text-[13px]' : 'text-xs', toneText[state.tone])}>{state.text}</span>
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

/** "investigator-2 on turn 14", "investigator-2", or where the item is when nobody works it. */
function expediteProgress(item: ItemCard): string {
  if (item.state !== 'in_progress' || !item.agent) return `at ${humanize(item.station)}`;
  return item.turn ? `${agentCopy(item)} on turn ${item.turn}` : agentCopy(item);
}

/** An expedited item on a phone, under the station picker: the lane's name, its title, who works it. */
export function ExpediteRow({ item }: { item: ItemCard }) {
  const params = useFactoryParams();
  const who = item.state === 'in_progress' && item.agent ? agentCopy(item) : humanize(item.station);
  return (
    <article className="relative grid grid-cols-[36px_minmax(0,1fr)] items-center gap-3 rounded-card border border-rework-line bg-rework-tint p-3 has-[a:focus-visible]:outline-2 has-[a:focus-visible]:outline-offset-2 has-[a:focus-visible]:outline-run">
      <span
        aria-hidden="true"
        className="flex size-9 items-center justify-center rounded-control bg-surface text-rework"
      >
        <Zap size={16} strokeWidth={1.5} />
      </span>
      <span className="min-w-0">
        <span className="block text-xs font-medium text-rework-ink">Expedite lane</span>
        <Link
          to="/o/$org/f/$factory/items/$ref"
          params={{ ...params, ref: refParam(item.ref) }}
          className="block text-[15px] leading-snug font-medium no-underline outline-none after:absolute after:inset-0 after:rounded-card"
        >
          {item.title}
        </Link>
        <span className="block text-[13px] text-muted">
          {item.ref}, {who}, {formatAge(item.age_seconds)}
        </span>
      </span>
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
          {item.ref}, {item.type}
          {item.lane ? ` ${item.lane}` : ''}, {expediteProgress(item)}
        </span>
      </span>
      <span className="col-start-2 text-sm whitespace-nowrap text-muted md:col-start-auto">
        {formatAge(item.age_seconds)}, {formatUsd(item.cost_usd)}
      </span>
    </article>
  );
}
