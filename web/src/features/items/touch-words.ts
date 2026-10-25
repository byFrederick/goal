// What a person did on an item, in words, for the Item's Human touches (spec 4 §4.9): named by the
// touch's event and, where it says more (where an item was moved, a send-back's reason, a note),
// by that event's data.
import type { ItemEvent, Touch } from '../../api/types';
import { humanize } from '../../shared/format';
import { kindLabel } from '../../shared/touches';

const quoted = (text: unknown) => {
  if (typeof text !== 'string' || text.trim() === '') return '';
  const t = text.trim().replace(/\s+/g, ' ');
  return `: “${t.length > 120 ? `${t.slice(0, 119)}…` : t}”`;
};

/** "moved it to Triage", "sent it back: “No evidence”", "left a note: “Check the pool”". */
export function touchSentence(touch: Pick<Touch, 'kind'>, data: ItemEvent['data'] = {}): string {
  switch (touch.kind) {
    case 'merged':
      return 'merged the pull request';
    case 'rejected':
      return `sent it back${quoted(data.reason)}`;
    case 'gate_rejected':
      return `sent it back at the gate${quoted(data.reason)}`;
    case 'gate_approved':
      return 'passed it at the gate';
    case 'approved':
      return 'approved it';
    case 'moved':
      return typeof data.to === 'string' ? `moved it to ${humanize(data.to)}` : 'moved it';
    case 'paused':
      return `paused it${quoted(data.reason)}`;
    case 'retried':
      return 'retried it';
    case 'note':
      return `left a note${quoted(data.text)}`;
    case 'released':
      return 'released it to the queue';
    default:
      return kindLabel(touch.kind).toLowerCase();
  }
}

/** "Inbox, moved, unplanned": where it happened, what it was, and whether the line asked for it. */
export function touchMeta(touch: Pick<Touch, 'kind' | 'station' | 'planned'>): string[] {
  return [
    touch.station ? humanize(touch.station) : 'No station',
    kindLabel(touch.kind).toLowerCase(),
    touch.planned ? 'planned' : 'unplanned',
  ];
}
