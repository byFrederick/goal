import { describe, expect, test } from 'vitest';
import type { InboxEntry, ItemCard } from '../api/types';
import { decisionsSentence, needRows } from './needs-you';

const now = Date.parse('2026-10-24T10:00:00Z');
const factory = { id: 'f1', name: 'sre', title: 'SRE' };

const card = (ref: string, type: string, station: string): ItemCard =>
  ({ id: ref, ref, title: `Title of ${ref}`, type, station, state: 'waiting_human' }) as ItemCard;

const entry = (over: Partial<InboxEntry>): InboxEntry => ({
  id: 'x',
  kind: 'decision',
  factory,
  since: '2026-10-24T09:22:00Z',
  item: null,
  station: null,
  action: null,
  approval: null,
  blocked: null,
  andon: null,
  rework: null,
  ...over,
});

const decision = (ref: string, type: string, station: string) =>
  entry({ id: `decision:${ref}`, item: card(ref, type, station), station });

describe('needRows', () => {
  test('groups the decisions in one row that leads to them, then one row per other entry', () => {
    const rows = needRows(
      [
        decision('sre#4', 'change', 'approve'),
        decision('sre#5', 'change', 'approve'),
        decision('sre#6', 'fix', 'merge'),
        entry({
          id: 'rework_warning:f1:review',
          kind: 'rework_warning',
          rework: {
            station: 'review',
            rejected: 3,
            verdicts: 10,
            window: 10,
            threshold: 0.4,
            top_reason: 'missing evidence',
          },
        }),
        entry({ id: 'expedite:sre#12', kind: 'expedite', item: card('sre#12', 'incident', 'investigate') }),
      ],
      now,
    );

    expect(rows.map((r) => [r.kind, r.tone, r.title, r.sub])).toEqual([
      ['decisions', 'you', '3 things wait for your OK', '2 changes at Approve, 1 fix at Merge'],
      ['rework_warning', 'rework', 'Review sent back 3 of the last 10', 'Missing evidence. Andon at 40%'],
      ['expedite', 'run', 'sre#12 is in the expedite lane', 'Title of sre#12, 38 min'],
    ]);
    expect(rows[0]?.link).toEqual({ to: 'decisions', factory: 'sre' });
    expect(rows[2]?.link).toEqual({ to: 'item', factory: 'sre', ref: 'sre#12' });
  });

  test('says why an item is blocked or paused, and that a line is stopped', () => {
    const rows = needRows(
      [
        entry({
          id: 'andon:1',
          kind: 'andon',
          since: '2026-10-24T09:25:00Z',
          andon: { reason: 'Bad answers' } as InboxEntry['andon'],
        }),
        entry({
          id: 'blocked:a',
          kind: 'blocked',
          item: card('sre#7', 'fix', 'build'),
          station: 'build',
          blocked: { reason: 'checks failed', paused: false },
        }),
        entry({
          id: 'blocked:b',
          kind: 'blocked',
          item: card('sre#8', 'fix', 'build'),
          station: 'build',
          blocked: { reason: null, paused: true },
        }),
      ],
      now,
    );

    expect(rows.map((r) => [r.title, r.sub])).toEqual([
      ['Line stopped: Bad answers', 'Andon pulled 35 min ago'],
      ['sre#7 Title of sre#7', 'Blocked at Build: checks failed'],
      ['sre#8 Title of sre#8', 'Paused at Build'],
    ]);
  });

  test('one decision waits; nothing yields no rows', () => {
    expect(needRows([decision('sre#4', 'change', 'approve')], now)[0]?.title).toBe('1 thing waits for your OK');
    expect(needRows([], now)).toEqual([]);
  });
});

describe('decisionsSentence', () => {
  test('counts each work type at each station', () => {
    expect(decisionsSentence([decision('a#1', 'feature', 'approve-spec')])).toBe('1 feature at Approve spec');
  });
});
