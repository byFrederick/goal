// What each factory of the organization needs from people: the rail's badges and dots, the
// sidebar's Items count and the factories' accessible names (C §10.3).
import { useQuery } from '@tanstack/react-query';
import { attentionQuery } from '../api/queries';
import type { OrgAttention } from '../api/types';

export interface Attention {
  /** Items waiting for a person's decision. */
  waiting: number;
  /** Items stopped until someone retries them. */
  blocked: number;
  /** The reason of the open andon pull, or null while the line runs. */
  andon: string | null;
}

export const noAttention: Attention = { waiting: 0, blocked: 0, andon: null };

function byFactory(attention: OrgAttention): ReadonlyMap<string, Attention> {
  return new Map(
    attention.factories.map((f) => [
      f.factory_id,
      { waiting: f.waiting_on_you, blocked: f.blocked_items, andon: f.andon?.reason ?? null },
    ]),
  );
}

const none: ReadonlyMap<string, Attention> = new Map();

/**
 * What each factory of the organization needs from people, by factory id (GET /attention, which
 * the stream refreshes on every event). Until it has loaded every factory reads as quiet: the
 * badges are a hint, never worth holding the page for.
 */
export function useAttention(org: string): ReadonlyMap<string, Attention> {
  return useQuery({ ...attentionQuery(org), select: byFactory }).data ?? none;
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/**
 * A factory's square: its accessible name ("S, sre factory, 3 waiting on you") and the tooltip's
 * second line, which says the same status, or the factory's title when there is none.
 */
export function factorySignal(
  factory: { name: string; title?: string | undefined },
  mark: string,
  attention: Attention,
): { label: string; detail: string } {
  const status = [
    attention.waiting > 0 ? `${attention.waiting} waiting on you` : '',
    attention.andon !== null ? 'blocked: andon pulled' : '',
    attention.andon === null && attention.blocked > 0 ? plural(attention.blocked, 'item blocked', 'items blocked') : '',
  ].filter(Boolean);
  const label = [`${mark}, ${factory.name} factory`, ...status].join(', ');
  const detail = status.length > 0 ? status.join(', ').replace('blocked: andon pulled', 'Andon pulled') : '';
  return { label, detail: detail || factory.title || '' };
}
