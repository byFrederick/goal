// Activity on the Floor (Floor, Activity): the factory's latest milestone events, newest first, one
// sentence each with the time and the actor's avatar. It refreshes when an item event arrives (the
// stream invalidates it); it never polls.
import { useQuery } from '@tanstack/react-query';
import { activityQuery } from '../../api/queries';
import type { Activity } from '../../api/types';
import { useFactory } from '../../shared/factory';
import { formatClock } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { SectionCard } from '../../ui/card';
import { Chip, Dot } from '../../ui/chip';
import { activityWords, avatarOf } from './activity-words';
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

function Feed({ activity }: { activity: Activity }) {
  return (
    <ol className="m-0 flex list-none flex-col gap-3 p-0">
      {activity.events.map((event) => {
        const { head, rest } = activityWords(event);
        return (
          <li key={event.id} className="flex items-start gap-3">
            <time dateTime={event.at} className="w-14 shrink-0 pt-1 text-meta text-muted tabular-nums">
              {formatClock(event.at)}
            </time>
            <span
              aria-hidden="true"
              className="flex size-7 shrink-0 items-center justify-center rounded-full bg-run-tint text-meta font-semibold text-run-ink"
            >
              {avatarOf(event.actor)}
            </span>
            <p className="m-0 min-w-0 text-body text-ink">
              <span className="font-semibold">{head}</span>
              <span className="text-ink-soft">{rest}</span>
            </p>
          </li>
        );
      })}
    </ol>
  );
}
