import { Link } from '@tanstack/react-router';
import { CircleCheck, TriangleAlert, Zap } from 'lucide-react';
import type { Board, BoardStation, Factory, ItemCard } from '../../api/types';
import { refParam } from '../../shared/factory';
import { formatAge, humanize, plural } from '../../shared/format';
import { ItemDecisions } from '../../shared/item-actions';
import { SectionCard } from '../../ui/card';
import { cn } from '../../ui/cn';

interface Need {
  item: ItemCard;
  station: BoardStation;
  why: 'decide' | 'blocked' | 'expedite';
}

/** What waits on a person: decisions first, then blocked work, then expedited work to watch. */
export function needsOf(board: Board): Need[] {
  const all = board.stations.flatMap((station) => station.items.map((item) => ({ item, station })));
  const order = { decide: 0, blocked: 1, expedite: 2 } as const;
  return all
    .flatMap(({ item, station }): Need[] => {
      if (item.state === 'waiting_human' && station.kind === 'human') return [{ item, station, why: 'decide' }];
      if (item.state === 'blocked') return [{ item, station, why: 'blocked' }];
      if (item.class_of_service === 'expedite' && item.state !== 'done' && item.state !== 'cancelled') {
        return [{ item, station, why: 'expedite' }];
      }
      return [];
    })
    .sort((a, b) => order[a.why] - order[b.why] || b.item.age_seconds - a.item.age_seconds);
}

const icons = {
  decide: { Icon: CircleCheck, tile: 'bg-you-tint text-you' },
  blocked: { Icon: TriangleAlert, tile: 'bg-rework-tint text-rework' },
  expedite: { Icon: Zap, tile: 'bg-run-tint text-run' },
} as const;

function subline(n: Need): string {
  const at = humanize(n.item.station);
  switch (n.why) {
    case 'decide':
      return `${n.station.action === 'merge' ? 'Ready to merge' : 'Waiting for your OK'} at ${at}, ${formatAge(n.item.age_seconds)}`;
    case 'blocked':
      return `Blocked at ${at}${n.item.rework_count > 0 ? `, sent back ${n.item.rework_count}×` : ''}`;
    case 'expedite':
      return `Expedite lane, ${n.item.agent ? `${n.item.agent} at ${at}` : at}, ${formatAge(n.item.age_seconds)}`;
  }
}

export function NeedsYouCard({ board, factory, className }: { board: Board; factory: Factory; className?: string }) {
  const needs = needsOf(board);
  const decisions = needs.filter((n) => n.why !== 'expedite').length;

  return (
    <SectionCard
      title="Needs you"
      description={decisions > 0 ? `${plural(decisions, 'thing')} right now` : 'Nothing right now'}
      className={className}
    >
      {needs.length === 0 ? (
        <p className="m-0 border-t border-line pt-3 text-[15px] text-muted">
          Nothing waits on you. Approvals and blocked work show up here as they happen.
        </p>
      ) : (
        <ul className="@container m-0 -mt-1 flex list-none flex-col p-0">
          {needs.map((n) => {
            const { Icon, tile } = icons[n.why];
            return (
              <li
                key={n.item.id}
                className="flex flex-col gap-2.5 border-t border-line py-3 @lg:flex-row @lg:items-center @lg:gap-3"
              >
                <div className="grid min-w-0 flex-1 grid-cols-[40px_minmax(0,1fr)] items-center gap-3">
                  <span
                    aria-hidden="true"
                    className={cn('flex size-10 items-center justify-center rounded-[10px]', tile)}
                  >
                    <Icon size={18} strokeWidth={1.5} />
                  </span>
                  <span className="min-w-0">
                    <Link
                      to="/f/$factory/items/$ref"
                      params={{ factory: factory.name, ref: refParam(n.item.ref) }}
                      className="block text-[15px] leading-snug font-medium no-underline hover:underline"
                    >
                      <span className="text-muted">{n.item.ref}</span> {n.item.title}
                    </Link>
                    <span className="mt-0.5 block text-sm text-muted">{subline(n)}</span>
                  </span>
                </div>
                <ItemDecisions
                  item={{
                    key: n.item.ref,
                    ref: n.item.ref,
                    title: n.item.title,
                    state: n.item.state,
                    pr_url: n.item.pr_url,
                  }}
                  factory={factory.name}
                  station={n.station}
                  className="pl-[52px] @lg:pl-0"
                />
              </li>
            );
          })}
        </ul>
      )}
    </SectionCard>
  );
}
