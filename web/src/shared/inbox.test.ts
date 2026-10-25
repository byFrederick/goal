import { describe, expect, test } from 'vitest';
import type { InboxEntry, ItemCard } from '../api/types';
import { inboxCount, inboxGroups } from './inbox';

const factory = { id: 'f1', name: 'sre', title: 'SRE' };

const card = (ref: string, station: string): ItemCard =>
  ({ id: ref, ref, title: `Title of ${ref}`, type: 'change', station, state: 'waiting_human' }) as ItemCard;

const entry = (over: Partial<InboxEntry>): InboxEntry => ({
  id: 'x',
  kind: 'decision',
  factory,
  since: '2026-10-24T09:00:00Z',
  item: null,
  station: null,
  action: null,
  approval: null,
  blocked: null,
  andon: null,
  rework: null,
  ...over,
});

const merge = (ref: string, since: string) =>
  entry({ id: `decision:${ref}`, item: card(ref, 'merge'), station: 'merge', action: 'merge', since });
const approve = (ref: string, since: string) =>
  entry({ id: `decision:${ref}`, item: card(ref, 'review'), station: 'review', since });

describe('inboxGroups', () => {
  test('groups the entries as Ready to merge, Needs a decision, Blocked, each with its count', () => {
    const groups = inboxGroups([
      entry({
        id: 'blocked:sre#9',
        kind: 'blocked',
        item: card('sre#9', 'build'),
        station: 'build',
        blocked: { reason: 'tests fail', paused: false } as InboxEntry['blocked'],
      }),
      approve('sre#2', '2026-10-24T08:00:00Z'),
      merge('sre#4', '2026-10-24T07:00:00Z'),
    ]);

    expect(groups.map((g) => [g.id, g.title, g.rows.length])).toEqual([
      ['ready', 'Ready to merge', 1],
      ['decide', 'Needs a decision', 1],
      ['blocked', 'Blocked', 1],
    ]);
    expect(inboxCount(groups)).toBe(3);
  });

  test('leaves out empty groups', () => {
    const groups = inboxGroups([approve('sre#2', '2026-10-24T08:00:00Z')]);
    expect(groups.map((g) => g.id)).toEqual(['decide']);
  });

  test('orders the rows of each group oldest first', () => {
    const [ready] = inboxGroups([
      merge('sre#7', '2026-10-24T09:00:00Z'),
      merge('sre#4', '2026-10-24T07:00:00Z'),
      merge('sre#5', '2026-10-24T08:00:00Z'),
    ]);
    expect(ready?.rows.map((r) => r.item?.ref)).toEqual(['sre#4', 'sre#5', 'sre#7']);
  });

  test('maps each row to its one action and its status icon', () => {
    const rows = Object.fromEntries(
      inboxGroups([
        merge('sre#4', '2026-10-24T07:00:00Z'),
        approve('sre#2', '2026-10-24T08:00:00Z'),
        entry({
          id: 'blocked:sre#9',
          kind: 'blocked',
          item: card('sre#9', 'build'),
          station: 'build',
          blocked: { reason: 'tests fail', paused: false } as InboxEntry['blocked'],
        }),
        entry({ id: 'andon:pull-1', kind: 'andon', andon: { reason: 'Checkout is down' } as InboxEntry['andon'] }),
      ]).flatMap((g) => g.rows.map((r) => [r.key, r])),
    );

    expect(rows['decision:sre#4']).toMatchObject({ action: 'merge', status: 'ready', to: 'item' });
    expect(rows['decision:sre#2']).toMatchObject({ action: 'approve', status: 'waiting', to: 'item' });
    expect(rows['blocked:sre#9']).toMatchObject({ action: 'retry', status: 'waiting', to: 'item' });
    expect(rows['andon:pull-1']).toMatchObject({ action: 'open', status: 'waiting', to: 'board' });
    expect(rows['andon:pull-1']?.why).toBe('Line stopped: Checkout is down');
  });

  test('a row carries its item title, or what the entry is when it is about no item', () => {
    const [blocked] = inboxGroups([
      entry({ id: 'andon:pull-1', kind: 'andon', andon: { reason: 'Checkout is down' } as InboxEntry['andon'] }),
    ]);
    expect(blocked?.rows[0]?.title).toBe('Line stopped');
    expect(blocked?.rows[0]?.item).toBeNull();
  });

  test('leaves expedite entries out, since they wait on no person', () => {
    const groups = inboxGroups([
      entry({ id: 'expedite:item-12', kind: 'expedite', item: card('sre#12', 'investigate') }),
    ]);
    expect(groups).toEqual([]);
  });

  test('shows a kind this build does not know as an Open row under Needs a decision', () => {
    const [decide] = inboxGroups([
      entry({ id: 'tool_approval:x', kind: 'tool_approval' as InboxEntry['kind'], item: card('sre#3', 'build') }),
    ]);
    expect(decide?.id).toBe('decide');
    expect(decide?.rows[0]).toMatchObject({ action: 'open', to: 'item' });
  });

  test('a decision with no item cannot be approved or merged, so it opens the board', () => {
    const [decide] = inboxGroups([entry({ id: 'decision:none', kind: 'decision', station: 'review' })]);
    expect(decide?.rows[0]).toMatchObject({ action: 'open', to: 'board' });
  });
});
