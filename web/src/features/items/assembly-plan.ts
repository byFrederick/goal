// The Assembly page's arithmetic and grouping: the segmented bar, the status of each part, the plan
// in waves, the spend, and the time to finish. Pure, so the rules are tested without a DOM.
import type { AssemblyPart, BoardLane, Item } from '../../api/types';
import { humanize } from '../../shared/format';
import { itemStatus } from '../../shared/item-state';
import type { StatusKind } from '../../ui/status-icon';

/** A part as the bar and the icons see it: done, being worked, waiting, or cancelled. */
export type PartTone = 'done' | 'working' | 'waiting' | 'cancelled';

export function partTone(part: Pick<AssemblyPart, 'state'>): PartTone {
  switch (part.state) {
    case 'done':
      return 'done';
    case 'in_progress':
      return 'working';
    case 'cancelled':
      return 'cancelled';
    default:
      return 'waiting';
  }
}

/** One segment per part, in plan order. */
export function barSegments(parts: readonly AssemblyPart[]): { key: string; tone: PartTone }[] {
  return parts.map((p) => ({ key: p.id, tone: partTone(p) }));
}

/**
 * The status icon of a part: the item's status everywhere (`itemStatus`), except that a part still
 * waiting on the open items it names is waiting too, even when it is ready to start. A blocked
 * part has no icon in `itemStatus`; the plan shows it as waiting.
 */
export function partStatus(part: Pick<AssemblyPart, 'state' | 'waiting_on'>): StatusKind {
  if (part.state !== 'in_progress' && part.state !== 'done' && part.waiting_on.length > 0) return 'waiting';
  return itemStatus(part) ?? 'waiting';
}

export interface Wave {
  /** The part's wave as the API counts it (0 for no needs). */
  wave: number;
  /** Plan order within the wave. Cancelled parts are left out. */
  parts: AssemblyPart[];
}

/** The parts in waves, the earliest first. */
export function planWaves(parts: readonly AssemblyPart[]): Wave[] {
  const byWave = new Map<number, AssemblyPart[]>();
  for (const part of parts) {
    if (part.state === 'cancelled') continue;
    byWave.set(part.wave, [...(byWave.get(part.wave) ?? []), part]);
  }
  return [...byWave].sort(([a], [b]) => a - b).map(([wave, waveParts]) => ({ wave, parts: waveParts }));
}

/** A part still to finish: neither merged nor cancelled. */
function isOpen(part: Pick<AssemblyPart, 'state'>): boolean {
  return part.state !== 'done' && part.state !== 'cancelled';
}

/**
 * The spend of the assembly: its own cost plus its parts' cost, against the task budget of each
 * item times the parts in all. The budget is null when the factory sets no per-item budget.
 */
export function spend(
  assembly: Pick<Item, 'cost_usd'>,
  parts: readonly Pick<AssemblyPart, 'cost_usd'>[],
  perItemUsd: number | null,
): { spent: number; budget: number | null } {
  const spent = assembly.cost_usd + parts.reduce((sum, p) => sum + p.cost_usd, 0);
  return { spent, budget: perItemUsd ? perItemUsd * parts.length : null };
}

/**
 * The time to finish, in milliseconds: the mean lead time of the merged parts (done_at − created_at)
 * times the number of waves that still hold an open part. Null with no merged part to measure by.
 */
export function timeToFinish(parts: readonly AssemblyPart[]): number | null {
  const leads = parts
    .filter((p) => p.state === 'done' && p.done_at)
    .map((p) => Date.parse(p.done_at as string) - Date.parse(p.created_at));
  if (leads.length === 0) return null;
  const mean = leads.reduce((sum, ms) => sum + ms, 0) / leads.length;
  const openWaves = new Set(parts.filter(isOpen).map((p) => p.wave)).size;
  return mean * openWaves;
}

/** Whether any part is still to finish (the time to finish says nothing once none is). */
export function hasOpenParts(parts: readonly Pick<AssemblyPart, 'state'>[]): boolean {
  return parts.some(isOpen);
}

/**
 * Who works a part, as the board names the agent's lane: "Infra engineer · Opus 5.5". Without a
 * lane (the board is not loaded, or the agent has none) it is the agent's name as words, never a
 * numbered copy of it.
 */
export function agentLabel(agent: string, lanes: readonly BoardLane[]): string {
  const lane = lanes.find((l) => l.kind === 'agent' && l.key === agent);
  if (!lane) return humanize(agent);
  return lane.model_label ? `${lane.role} · ${lane.model_label}` : lane.role;
}
