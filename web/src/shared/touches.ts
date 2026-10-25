// Human touches as the screens say them (spec 4 §4.9, §4.10): reasons, kinds and rates in words,
// from the API's counts only. A touch is one action by a person on an item; fewer is better.
import type { TouchHeadline, TouchReason, TouchStats } from '../api/types';
import { formatPercent, humanize, plural } from './format';

/** The reasons a person may give a touch, in the order the select lists them. */
export const touchReasons: readonly { value: TouchReason; label: string }[] = [
  { value: 'missing_knowledge', label: 'Missing knowledge' },
  { value: 'wrong_decision', label: 'Wrong decision' },
  { value: 'required_gate', label: 'Required gate' },
  { value: 'other', label: 'Other' },
];

/** "Missing knowledge", or "No reason set" for a touch nobody gave one. */
export function reasonLabel(reason: TouchReason | null): string {
  return touchReasons.find((r) => r.value === reason)?.label ?? 'No reason set';
}

const kinds: Record<string, string> = {
  merged: 'Merged',
  rejected: 'Sent back',
  gate_rejected: 'Sent back at a gate',
  gate_approved: 'Passed a gate',
  approved: 'Approved',
  moved: 'Moved',
  paused: 'Paused',
  retried: 'Retried',
  note: 'Note',
  released: 'Released',
};

/** The event kind that names a touch, as words: "Sent back", "Note". */
export function kindLabel(kind: string): string {
  return kinds[kind] ?? humanize(kind);
}

/** Touches per finished item: one decimal for a headline ("1.4"), two for a station's share ("0.38"). */
export function perItem(n: number, places: 1 | 2 = 1): string {
  return n.toFixed(places);
}

/** "1.4 touches per item, 38% touchless", or null when no item finished in the window. */
export function headlineText(h: TouchHeadline): string | null {
  if (h.finished === 0) return null;
  return `${perItem(h.per_item_mean)} touches per item, ${formatPercent(h.touchless_rate)} touchless`;
}

/** Touches per finished item at each station, by station id; empty when nothing finished. */
export function stationRates(stats: TouchStats): Map<string, number> {
  if (stats.finished === 0) return new Map();
  return new Map(stats.by_station.filter((s) => s.key !== '').map((s) => [s.key, s.touches / stats.finished]));
}

/** "8 planned, 3 unplanned", "All planned", "All unplanned". */
function plannedCaption(planned: number, unplanned: number): string {
  if (unplanned === 0) return 'All planned';
  if (planned === 0) return 'All unplanned';
  return `${planned} planned, ${unplanned} unplanned`;
}

/** How the Floor's Touches panel groups them. */
export const touchGroupings = [
  { value: 'station', label: 'Station' },
  { value: 'kind', label: 'Kind' },
  { value: 'reason', label: 'Reason' },
  { value: 'version', label: 'Version' },
] as const;

export type TouchGrouping = (typeof touchGroupings)[number]['value'];

export const isTouchGrouping = (v: unknown): v is TouchGrouping => touchGroupings.some((g) => g.value === v);

/** One bar of the Touches panel. */
export interface TouchRow {
  key: string;
  label: string;
  touches: number;
  /** Its touches per finished item. */
  perItem: number;
  /** What the bar's touches were: planned or not, or how a version's items did. */
  caption: string;
}

/**
 * The bars of one grouping, the most touches first (as goald orders them; versions newest first):
 * a station or kind with its planned and unplanned touches, a reason, a definition version with
 * how its items did. Touches with no station are "No station"; items no agent ran, "No agent run".
 */
export function touchRows(stats: TouchStats, by: TouchGrouping): TouchRow[] {
  const share = (n: number) => (stats.finished > 0 ? n / stats.finished : 0);
  switch (by) {
    case 'station':
      return stats.by_station.map((s) => ({
        key: s.key,
        label: s.key === '' ? 'No station' : humanize(s.key),
        touches: s.touches,
        perItem: share(s.touches),
        caption: plannedCaption(s.planned, s.unplanned),
      }));
    case 'kind':
      return stats.by_kind.map((k) => ({
        key: k.key,
        label: kindLabel(k.key),
        touches: k.touches,
        perItem: share(k.touches),
        caption: plannedCaption(k.planned, k.unplanned),
      }));
    case 'reason':
      return stats.by_reason.map((r) => ({
        key: r.reason ?? 'none',
        label: reasonLabel(r.reason),
        touches: r.touches,
        perItem: share(r.touches),
        caption: plannedCaption(r.planned, r.unplanned),
      }));
    case 'version':
      return stats.by_version.map((v) => ({
        key: v.version_id ?? 'none',
        label: v.number === null ? 'No agent run' : `v${v.number}`,
        touches: v.touches,
        perItem: v.per_item_mean,
        caption: `${plural(v.finished, 'item')}, ${perItem(v.per_item_mean)} per item, ${formatPercent(v.touchless_rate)} touchless`,
      }));
  }
}

/** "Most touches: Approve 22, Investigate 17.", or null when nobody touched a finished item. */
export function mostTouched(stats: TouchStats): string | null {
  const top = touchRows(stats, 'station')
    .filter((r) => r.touches > 0)
    .slice(0, 2);
  if (top.length === 0) return null;
  return `Most touches: ${top.map((r) => `${r.label} ${r.touches}`).join(', ')}.`;
}
