// Activity on the Floor (Floor, Activity): the factory's latest milestone events, newest first, one
// line each: a mono time, a 6 px dot coloured by the event and the sentence, which truncates. It refreshes when an item event arrives (the
// stream invalidates it); it never polls.
import { useQuery } from '@tanstack/react-query';
import { useRef } from 'react';
import { activityQuery } from '../../api/queries';
import type { Activity } from '../../api/types';
import { useFactory } from '../../shared/factory';
import { formatClock } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { SectionCard } from '../../ui/card';
import { Chip, Dot } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { activityTone, activityWords } from './activity-words';
import { CardEmpty, CardFailed, CardLoading } from './card-states';

export function ActivityCard({ className }: { className?: string }) {
  const org = useOrg();
  const factory = useFactory();
  const query = useQuery(activityQuery(org.id, factory.id));

  return (
    <SectionCard
      title="Activity"
      aria-busy={query.isPending || undefined}
      aside={
        <Chip tone="run">
          <Dot className="animate-pulse-dot bg-run motion-reduce:animate-none" />
          Live
        </Chip>
      }
      className={className}
    >
      {query.isPending ? <CardLoading /> : null}
      {query.isError ? <CardFailed what="activity" onRetry={() => void query.refetch()} /> : null}
      {query.data && query.data.events.length === 0 ? <CardEmpty>Nothing has happened yet.</CardEmpty> : null}
      {query.data && query.data.events.length > 0 ? <Feed activity={query.data} /> : null}
    </SectionCard>
  );
}

const dotTone = {
  ok: 'bg-ok',
  rework: 'bg-rework',
  run: 'bg-run',
  muted: 'bg-muted',
} as const;

function Feed({ activity }: { activity: Activity }) {
  // The rows already on screen at the last render; a row that arrives after it fades in (MOTION.md, A new
  // item appears in a list). The first render shows every row still.
  const seen = useRef<Set<number> | null>(null);
  const before = seen.current;
  seen.current = new Set(activity.events.map((event) => event.id));
  return (
    <ol className="m-0 flex list-none flex-col p-0">
      {activity.events.map((event) => {
        const { head, rest } = activityWords(event);
        return (
          <li
            key={event.id}
            className={cn(
              'flex items-center gap-3 border-b border-line py-2 last:border-b-0 first:pt-0',
              before && !before.has(event.id) && 'motion-safe:animate-item-in',
            )}
          >
            <time dateTime={event.at} className="w-11 shrink-0 font-mono text-meta text-muted tabular-nums">
              {formatClock(event.at)}
            </time>
            <span
              aria-hidden="true"
              className={cn('size-1.5 shrink-0 rounded-full', dotTone[activityTone(event.kind)])}
            />
            <p className="m-0 min-w-0 flex-1 truncate text-body text-ink" title={`${head}${rest}`}>
              <span className="font-semibold">{head}</span>
              <span className="text-ink-soft">{rest}</span>
            </p>
          </li>
        );
      })}
    </ol>
  );
}
