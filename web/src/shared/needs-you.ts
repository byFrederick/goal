// What waits on a person, from the organization's inbox (E §10.2): the rows of the Floor's Needs
// you card and of the phone's Items view. Pure, so the grouping and the words are tested without a
// DOM. Kinds this build does not know read as a generic row.
import type { InboxEntry } from '../api/types';
import { formatAge, humanize, plural } from './format';

/** The colour of a row's icon tile: violet waits on you, rust is a stop or rework, teal runs. */
export type NeedTone = 'you' | 'rework' | 'run' | 'plain';

/** Where a row leads. */
export type NeedLink =
  | { to: 'decisions'; factory: string }
  | { to: 'board'; factory: string }
  | { to: 'item'; factory: string; ref: string };

export interface NeedRow {
  key: string;
  kind: InboxEntry['kind'] | 'decisions';
  tone: NeedTone;
  title: string;
  sub: string;
  link: NeedLink;
}

const ageSince = (since: string, now: number) => formatAge(Math.max(0, (now - Date.parse(since)) / 1000));

/** "2 changes at Approve, 1 fix at Merge": the decisions by station, then by work type. */
export function decisionsSentence(decisions: readonly InboxEntry[]): string {
  const byStation = new Map<string, Map<string, number>>();
  for (const e of decisions) {
    const station = e.station ?? e.item?.station ?? '';
    const types = byStation.get(station) ?? new Map<string, number>();
    const type = e.item?.type || 'item';
    types.set(type, (types.get(type) ?? 0) + 1);
    byStation.set(station, types);
  }

  return [...byStation]
    .map(([station, types]) => {
      const counts = [...types].map(([type, n]) => plural(n, humanize(type).toLowerCase()));
      return `${counts.join(', ')} at ${humanize(station)}`;
    })
    .join(', ');
}

/** One row for an entry that is not a decision. */
export function needRow(e: InboxEntry, now: number): NeedRow {
  const factory = e.factory.name;
  const item = e.item;
  const base = { key: e.id, kind: e.kind } as const;

  switch (e.kind) {
    case 'andon':
      return {
        ...base,
        tone: 'rework',
        title: `Line stopped: ${e.andon?.reason ?? 'the andon cord is pulled'}`,
        sub: `Andon pulled ${ageSince(e.since, now)} ago`,
        link: { to: 'board', factory },
      };
    case 'rework_warning': {
      const r = e.rework;
      const rate = r ? `Andon at ${Math.round(r.threshold * 100)}%` : '';
      return {
        ...base,
        tone: 'rework',
        title: r ? `${humanize(r.station)} sent back ${r.rejected} of the last ${r.verdicts}` : 'Rework is rising',
        sub: [r?.top_reason ? humanize(r.top_reason) : '', rate].filter(Boolean).join('. '),
        link: { to: 'board', factory },
      };
    }
    case 'blocked':
      return {
        ...base,
        tone: 'rework',
        title: item ? `${item.ref} ${item.title}` : 'An item is blocked',
        sub: `${e.blocked?.paused ? 'Paused' : 'Blocked'} at ${humanize(e.station ?? item?.station ?? '')}${e.blocked?.reason ? `: ${e.blocked.reason}` : ''}`,
        link: item ? { to: 'item', factory, ref: item.ref } : { to: 'board', factory },
      };
    case 'expedite':
      return {
        ...base,
        tone: 'run',
        title: item ? `${item.ref} is in the expedite lane` : 'Expedited work',
        sub: item ? `${item.title}, ${ageSince(e.since, now)}` : '',
        link: item ? { to: 'item', factory, ref: item.ref } : { to: 'board', factory },
      };
    default:
      return {
        ...base,
        tone: e.kind === 'decision' ? 'you' : 'plain',
        title: item ? `${item.ref} ${item.title}` : humanize(e.kind),
        sub: e.station ? `At ${humanize(e.station)}` : '',
        link: item ? { to: 'item', factory, ref: item.ref } : { to: 'board', factory },
      };
  }
}

/**
 * The Needs you rows of one factory's entries, in the inbox's order: its decisions grouped in one
 * row ("3 things wait for your OK") that leads to them, every other entry a row of its own.
 */
export function needRows(entries: readonly InboxEntry[], now: number): NeedRow[] {
  const decisions = entries.filter((e) => e.kind === 'decision');
  const rows = entries.filter((e) => e.kind !== 'decision').map((e) => needRow(e, now));
  const first = decisions[0];
  if (!first) return rows;

  const n = decisions.length;
  return [
    {
      key: 'decisions',
      kind: 'decisions',
      tone: 'you',
      title: `${plural(n, 'thing')} ${n === 1 ? 'waits' : 'wait'} for your OK`,
      sub: decisionsSentence(decisions),
      link: { to: 'decisions', factory: first.factory.name },
    },
    ...rows,
  ];
}
