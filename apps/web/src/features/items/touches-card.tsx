// The Item's human touches (spec 4 §4.9, §4.10; canvas Item and Mobile-Item): each time a person
// had to act on it, who, what, where and when, planned or not, and why. Anyone who may operate the
// item sets the reason with a select; the change is a person's choice goald audits, not a touch.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useId } from 'react';
import { ApiError, setTouchReason } from '../../api/client';
import { keys, touchesQuery } from '../../api/queries';
import type { Factory, Item, ItemEvent, Touch, TouchReason } from '../../api/types';
import { formatClock } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { perItem, reasonLabel, touchReasons } from '../../shared/touches';
import { SectionCard } from '../../ui/card';
import { cn } from '../../ui/cn';
import { touchMeta, touchSentence } from './touch-words';

interface Props {
  item: Item;
  /** The key the item is cached under (its ref within the factory). */
  itemKey: string;
  factory: Factory;
  events: readonly ItemEvent[];
  className?: string;
}

export function TouchesCard({ item, itemKey, factory, events, className }: Props) {
  const org = useOrg();
  // The factory's average over the board's week, said beside the item's own count.
  const { data: line } = useQuery(touchesQuery(org.id, factory.id));
  const touches = item.touches;
  if (!touches) return null;

  const operates = holds(factory, 'item:operate');
  const dataOf = new Map(events.map((e) => [e.id, e.data]));
  const average =
    line && line.stats.finished > 0 ? `${factory.name} averages ${perItem(line.stats.per_item_mean)} per item.` : '';
  const ask = operates && touches.count > 0 ? ' Set a reason so the metric says why people stepped in.' : '';

  return (
    <SectionCard
      title="Human touches"
      aside={<span className="text-sm whitespace-nowrap text-muted">{touches.count} so far</span>}
      description={average || ask ? `${average}${ask}`.trim() : undefined}
      className={cn('gap-2', className)}
    >
      {touches.count === 0 ? (
        <p className="m-0 text-sm text-muted">No one has had to act on it yet.</p>
      ) : (
        <ol className="m-0 list-none p-0">
          {touches.list.map((t) => (
            <TouchRow
              key={t.event_id}
              touch={t}
              data={dataOf.get(t.event_id)}
              itemKey={itemKey}
              factoryId={factory.id}
              operates={operates}
            />
          ))}
        </ol>
      )}
    </SectionCard>
  );
}

function TouchRow({
  touch,
  data,
  itemKey,
  factoryId,
  operates,
}: {
  touch: Touch;
  data: ItemEvent['data'] | undefined;
  itemKey: string;
  factoryId: string;
  operates: boolean;
}) {
  const time = formatClock(touch.at);
  const [station, kind, planned] = touchMeta(touch);

  return (
    <li className="grid grid-cols-[minmax(0,1fr)] gap-x-3.5 gap-y-1 border-t border-line py-3 text-sm leading-[1.45] first:border-t-0 @[720px]:grid-cols-[44px_minmax(0,1fr)]">
      <time dateTime={touch.at} className="text-muted tabular-nums @max-[720px]:hidden">
        {time}
      </time>
      <div className="flex min-w-0 flex-col gap-1">
        <p className="m-0">
          <span className="text-muted tabular-nums @[720px]:hidden">{time} </span>
          <strong className="font-medium">{touch.actor.name}</strong> {touchSentence(touch, data)}.
        </p>
        <p className="m-0 text-[13px] text-muted">
          {station}
          <span className="@max-[720px]:hidden">, {kind}</span>, {planned}
          {operates ? null : `, ${reasonLabel(touch.reason).toLowerCase()}`}
        </p>
        {operates ? <ReasonSelect touch={touch} time={time} itemKey={itemKey} factoryId={factoryId} /> : null}
      </div>
    </li>
  );
}

function ReasonSelect({
  touch,
  time,
  itemKey,
  factoryId,
}: {
  touch: Touch;
  time: string;
  itemKey: string;
  factoryId: string;
}) {
  const org = useOrg();
  const client = useQueryClient();
  const errorId = useId();
  const itemKeyOf = keys.item(org.id, itemKey);
  const mutationKey = ['touch-reason', org.id, touch.event_id];
  const change = useMutation({
    mutationKey,
    mutationFn: (reason: TouchReason) => setTouchReason(org.id, itemKey, touch.event_id, reason),
    // The select shows the choice at once; a refusal puts goald's answer back.
    onMutate: async (reason) => {
      await client.cancelQueries({ queryKey: itemKeyOf, exact: true });
      client.setQueryData<Item>(itemKeyOf, (it) =>
        it?.touches
          ? {
              ...it,
              touches: {
                ...it.touches,
                list: it.touches.list.map((t) => (t.event_id === touch.event_id ? { ...t, reason } : t)),
              },
            }
          : it,
      );
    },
    // Only the last of quick changes re-reads: an earlier one's refetch would show its reason
    // again while the later one is on its way.
    onSettled: () =>
      client.isMutating({ mutationKey }) > 1
        ? undefined
        : Promise.all([
            client.invalidateQueries({ queryKey: itemKeyOf, exact: true }),
            client.invalidateQueries({ queryKey: [...keys.factory(org.id, factoryId), 'touches'] }),
          ]),
  });
  const reason = touch.reason;

  return (
    <div className="flex flex-col gap-1">
      <select
        aria-label={`Reason for the ${time} touch`}
        aria-describedby={change.isError ? errorId : undefined}
        value={reason ?? ''}
        onChange={(e) => change.mutate(e.target.value as TouchReason)}
        className={cn(
          'h-11 w-full max-w-[220px] cursor-pointer rounded-control border px-2.5 text-base transition-colors duration-200 md:h-9 md:text-sm',
          reason === 'missing_knowledge'
            ? 'border-you-tint bg-you-tint text-you-ink hover:border-you'
            : 'border-line bg-card text-ink hover:border-muted',
        )}
      >
        {reason === null ? (
          <option value="" disabled>
            Choose a reason
          </option>
        ) : null}
        {touchReasons.map((r) => (
          <option key={r.value} value={r.value}>
            {r.label}
          </option>
        ))}
      </select>
      {change.isError ? (
        <p id={errorId} role="alert" className="m-0 text-[13px] text-rework-ink">
          Not saved:{' '}
          {change.error instanceof ApiError ? (change.error.problem.detail ?? change.error.message) : 'try again'}.
        </p>
      ) : null}
    </div>
  );
}
