// Claude slots at full WIP: how many agents a line runs at once when every agent station is
// full, against the factory's slot cap (else the organization's), for Line's slots sentence and
// Review's Claude slots meter.
import type { OrgSlots } from '../../api/types';
import type { LineSection, ModelStation } from './model';

/** The agents an agent station runs at once: min(agents ?? 1, wip ?? agents ?? 1); 0 elsewhere. */
export function stationSlots(s: ModelStation): number {
  if (s.kind !== 'agent') return 0;
  const agents = s.agents ?? 1;
  return Math.min(agents, s.wip ?? agents);
}

/** The agents the line runs at once at full WIP. */
export function lineSlots(line: LineSection): number {
  return (line.stations ?? []).reduce((n, s) => n + stationSlots(s), 0);
}

export interface SlotCap {
  cap: number;
  /** Whose cap it is: the factory's own, or the organization's. */
  scope: 'factory' | 'org';
}

/** The cap that limits the factory: its own, else the organization's; undefined without one. */
export function slotCap(slots: OrgSlots | undefined, factoryId: string): SlotCap | undefined {
  if (!slots) return undefined;
  const own = slots.factories.find((f) => f.factory_id === factoryId)?.max;
  if (own !== null && own !== undefined) return { cap: own, scope: 'factory' };
  return slots.max !== null ? { cap: slots.max, scope: 'org' } : undefined;
}

/** "5 of your 6 Claude slots at full WIP". */
export function slotsSentence(used: number, cap: number): string {
  const slots = cap === 1 ? 'slot' : 'slots';
  return used <= cap
    ? `${used} of your ${cap} Claude ${slots} at full WIP`
    : `${used} agents at full WIP, more than your ${cap} Claude ${slots}`;
}
