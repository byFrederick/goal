// How an item came to be, in words (ADR 0040 decision 2): who filed it and through what, a plan, a trigger
// or another item's follow-up. One home for the Intake's Coming up rows and the item page's Came from.
import type { Actor, ItemCameFrom } from '../api/types';
import { RefLink } from './ref-link';

interface Came {
  origin?: 'person' | 'plan' | 'trigger' | 'follow-up' | undefined;
  via?: 'web' | 'cli' | 'api' | undefined;
  created_by?: Actor | undefined;
  /** The ref of the item whose follow-up filed it. */
  from_item?: string | undefined;
  /** Where a follow-up's or a trigger's item came from; only getItem sends it. */
  came_from?: ItemCameFrom | undefined;
}

const viaWords = { web: 'New item', cli: 'the CLI', api: 'the API' } as const;

export function CameFrom({ item }: { item: Came }) {
  switch (item.origin) {
    case 'plan':
      return <>Part of an assembly</>;
    case 'trigger': {
      const trigger = item.came_from?.trigger;
      if (!trigger) return <>Filed by a trigger</>;
      if (trigger.kind !== 'check') return <>{trigger.name}</>;
      return <>The check {trigger.key ? `${trigger.name}, ${trigger.key}` : trigger.name}</>;
    }
    case 'follow-up': {
      const followUps = item.came_from?.follow_ups ?? [];
      if (followUps.length > 0) {
        return (
          <>
            {followUps.map((f) => (
              <span key={f.id} className="block">
                A follow-up by {f.agent} on <RefLink item={f.from_item} />, filed when <RefLink item={f.landed_item} />{' '}
                landed
              </span>
            ))}
          </>
        );
      }
      return item.from_item ? (
        <>
          Follow-up from <RefLink item={item.from_item} />
        </>
      ) : (
        <>A follow-up</>
      );
    }
    default:
      return (
        <>
          {[item.created_by?.name, item.via ? viaWords[item.via] : undefined].filter(Boolean).join(', ') ||
            'Filed by a person'}
        </>
      );
  }
}
