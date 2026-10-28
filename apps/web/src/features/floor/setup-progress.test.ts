import { describe, expect, it } from 'vitest';
import type { Connection, IntakeTrigger, KnowledgeSource, Member } from '../../api/types';
import type { Model } from '../../shared/definition/model';
import { doneCount, type SetupInput, type SetupRow, setupRows } from './setup-progress';

const member = { member_id: 'm-1' } as Member;
const connection = (kind: string) => ({ id: `c-${kind}`, name: kind, kind }) as Connection;
const source = { id: 'k-1', name: 'handbook' } as KnowledgeSource;
const trigger = (enabled: boolean) => ({ name: 't', enabled }) as IntakeTrigger;

// The line's stations as the published model writes them; a Done station is not counted.
const stations = [
  { id: 'intake', kind: 'queue' },
  { id: 'spec', kind: 'agent' },
  { id: 'build', kind: 'agent' },
  { id: 'review', kind: 'agent' },
  { id: 'done', kind: 'done' },
];

const model = {
  basics: { repository: { url: 'https://github.com/acme/app', connection: 'github' } },
  line: { stations, flow: {} },
  review: { budget: { weekly_usd: 800 } },
} as unknown as Model;

/** Nothing set up yet: the factory exists with one owner and no published line. */
const empty: SetupInput = {
  members: [member],
  invitations: [],
  connections: [],
  knowledge: [],
  model: { basics: {}, line: { stations: [] }, review: {} } as unknown as Model,
  triggers: [],
  finishedItems: 0,
  factoryName: 'software',
};

const withAll: SetupInput = {
  members: [member, { member_id: 'm-2' } as Member],
  invitations: [],
  connections: [connection('slack'), connection('github')],
  knowledge: [source],
  model,
  triggers: [trigger(true), trigger(true), trigger(true)],
  finishedItems: 1,
  factoryName: 'software',
};

const row = (rows: SetupRow[], id: SetupRow['id']) => {
  const found = rows.find((r) => r.id === id);
  if (!found) throw new Error(`no row ${id}`);
  return found;
};

describe('setupRows', () => {
  it('lists the nine rows in the picture order', () => {
    expect(setupRows(empty).map((r) => r.id)).toEqual([
      'invite',
      'slack',
      'knowledge',
      'organization',
      'github',
      'line',
      'budget',
      'triggers',
      'first-item',
    ]);
  });

  it('counts a factory with everything set up as nine of nine', () => {
    const rows = setupRows(withAll);
    expect(doneCount(rows)).toBe(9);
    expect(rows.every((r) => r.done)).toBe(true);
  });

  it('starts at one of nine: the organization is always done and nothing else is', () => {
    expect(doneCount(setupRows(empty))).toBe(1);
  });

  it('counts invited people as done, with a second member or a pending invitation', () => {
    expect(row(setupRows(empty), 'invite').done).toBe(false);
    expect(row(setupRows({ ...empty, members: [member, member] }), 'invite').done).toBe(true);
    expect(
      row(setupRows({ ...empty, invitations: [{ id: 'i-1', email: 'x@example.com' } as never] }), 'invite').done,
    ).toBe(true);
  });

  it('marks Connect Slack done only for a slack connection, not a GitHub one', () => {
    expect(row(setupRows({ ...empty, connections: [connection('github')] }), 'slack').done).toBe(false);
    expect(row(setupRows({ ...empty, connections: [connection('slack')] }), 'slack').done).toBe(true);
  });

  it('marks Add knowledge done when any knowledge source exists', () => {
    expect(row(setupRows(empty), 'knowledge').done).toBe(false);
    expect(row(setupRows({ ...empty, knowledge: [source] }), 'knowledge').done).toBe(true);
  });

  it('reads GitHub from a github connection and counts the repository the definition names', () => {
    const connected = setupRows({ ...empty, connections: [connection('github')], model });
    expect(row(connected, 'github')).toMatchObject({ done: true, text: 'GitHub, 1 repository' });
    expect(row(setupRows({ ...empty, model: { basics: {} } as unknown as Model }), 'github').text).toBe(
      'GitHub, 0 repositories',
    );
    expect(row(setupRows({ ...empty, connections: [connection('github')] }), 'github').done).toBe(true);
    expect(row(setupRows(empty), 'github').done).toBe(false);
  });

  it('counts the line without its Done station, and is done once a station exists', () => {
    expect(row(setupRows({ ...withAll }), 'line').text).toBe('The line, 4 stations');
    expect(row(setupRows(empty), 'line')).toMatchObject({ done: false, text: 'The line, 0 stations' });
    const one = { ...model, line: { stations: [{ id: 'build', kind: 'agent' }] } } as unknown as Model;
    expect(row(setupRows({ ...empty, model: one }), 'line').text).toBe('The line, 1 station');
  });

  it('is done for budgets and targets once the definition sets one of either', () => {
    expect(row(setupRows(empty), 'budget').done).toBe(false);
    const budgetOnly = { ...model, line: { stations: [] } } as unknown as Model;
    expect(row(setupRows({ ...empty, model: budgetOnly }), 'budget').done).toBe(true);
    const targetOnly = {
      basics: {},
      line: { stations: [], flow: { targets: { throughput_per_day: 10 } } },
      review: {},
    } as unknown as Model;
    expect(row(setupRows({ ...empty, model: targetOnly }), 'budget').done).toBe(true);
  });

  it('counts every trigger the factory has, on or off, with singular and plural copy', () => {
    expect(row(setupRows(withAll), 'triggers')).toMatchObject({ done: true, text: '3 triggers' });
    const off = setupRows({ ...empty, triggers: [trigger(true), trigger(false)] });
    expect(row(off, 'triggers')).toMatchObject({ done: true, text: '2 triggers' });
    expect(row(setupRows({ ...empty, triggers: [trigger(false)] }), 'triggers')).toMatchObject({
      done: true,
      text: '1 trigger',
    });
    expect(row(setupRows(empty), 'triggers')).toMatchObject({ done: false, text: '0 triggers' });
  });

  it('marks the first item merged once an item of the factory is done', () => {
    expect(row(setupRows(empty), 'first-item').done).toBe(false);
    expect(row(setupRows({ ...empty, finishedItems: 1 }), 'first-item').done).toBe(true);
  });
});
