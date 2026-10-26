// How the Floor's lead-time bridge says its steps and its insight. Station names come from the
// factory; nothing here knows a station.
import type { BridgeStep, Factory, LeadTimeBridge } from '../../api/types';
import { formatPercent, humanize } from '../../shared/format';

type Stations = Factory['stations'];

const isQueue = (station: string, stations: Stations) => stations.find((s) => s.id === station)?.kind === 'queue';

/** "Waiting for Investigate", "Triage queue", "Review rework", "Waiting on you", … */
export function stepLabel(step: BridgeStep, stations: Stations): string {
  const at = humanize(step.station ?? '');
  switch (step.component) {
    case 'waiting':
      return isQueue(step.station ?? '', stations) ? `${at} queue` : `Waiting for ${at}`;
    case 'rework':
      return `${at} rework`;
    case 'waiting_on_you':
      return 'Waiting on you';
    case 'blocked':
      return `Blocked at ${at}`;
    case 'working':
      return `Working at ${at}`;
    default:
      return 'Other';
  }
}

const words: [number, string][] = [
  [3 / 4, 'three quarters'],
  [2 / 3, 'two thirds'],
  [1 / 2, 'half'],
  [1 / 3, 'a third'],
  [1 / 4, 'a quarter'],
];

/** A share of the gap in words: "two thirds", "half", "nearly all", else "41%". */
export function shareWords(share: number): string {
  if (share >= 0.95) return 'nearly all';
  return words.find(([w]) => Math.abs(share - w) < 0.03)?.[1] ?? formatPercent(share);
}

/** The insight under the bridge: what explains most of the gap, and the constraint's busy share. */
export function bridgeInsight(
  largest: BridgeStep,
  constraint: LeadTimeBridge['constraint'],
  stations: Stations,
): { lead: string; constraint: string | null } {
  const label =
    largest.component === 'waiting' && !isQueue(largest.station ?? '', stations)
      ? `Waiting in front of ${humanize(largest.station ?? '')}`
      : stepLabel(largest, stations);
  const lead = `${label} explains ${shareWords(largest.share)} of the gap.`;
  if (!constraint) return { lead, constraint: null };
  const who = constraint.station === largest.station ? 'It' : humanize(constraint.station);
  const busy = constraint.busy_ratio !== null ? `: busy ${formatPercent(constraint.busy_ratio)} of the time` : '';
  return { lead, constraint: `${who} is the constraint${busy}.` };
}
