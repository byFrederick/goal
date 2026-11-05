// How an item came to be, in words (ADR 0040 decision 2): who filed it and through what, a plan, a trigger
// or another item's follow-up. One home for the Intake's Coming up rows and the item page's Came from.
import type { Actor } from '../api/types';
import { RefLink } from './ref-link';

interface Came {
  origin?: 'person' | 'plan' | 'trigger' | 'follow-up' | undefined;
  via?: 'web' | 'cli' | 'api' | undefined;
  created_by?: Actor | undefined;
  /** The ref of the item whose follow-up filed it. */
  from_item?: string | undefined;
}

const viaWords = { web: 'New item', cli: 'the CLI', api: 'the API' } as const;

export function CameFrom({ item }: { item: Came }) {
  switch (item.origin) {
    case 'plan':
      return <>Part of an assembly</>;
    case 'trigger':
      return <>Filed by a trigger</>;
    case 'follow-up':
      return item.from_item ? (
        <>
          Follow-up from <RefLink item={item.from_item} />
        </>
      ) : (
        <>Follow-up</>
      );
    default:
      return (
        <>
          {[item.created_by?.name, item.via ? viaWords[item.via] : undefined].filter(Boolean).join(', ') ||
            'Filed by a person'}
        </>
      );
  }
}
