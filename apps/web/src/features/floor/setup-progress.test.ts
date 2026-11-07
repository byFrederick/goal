import { describe, expect, it } from 'vitest';
import type { IntakeTrigger, KnowledgeSource, Member } from '../../api/types';
import type { Model } from '../../shared/definition/model';
import { doneCount, type SetupInput, type SetupRow, setupRows } from './setup-progress';

const member = { member_id: 'm-1' } as Member;
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
  slackSet: false,
  knowledge: [],
  model: { basics: {}, line: { stations: [] }, review: {} } as unknown as Model,
  triggers: [],
  finishedItems: 0,
  paths: ['factory.yaml'],
  factoryYaml: undefined,
  factoryName: 'software',
};

const withAll: SetupInput = {
  members: [member, { member_id: 'm-2' } as Member],
  invitations: [],
  slackSet: true,
  knowledge: [source],
  model,
  triggers: [trigger(true), trigger(true), trigger(true)],
  finishedItems: 1,
  paths: ['factory.yaml'],
  factoryYaml: undefined,
  factoryName: 'software',
};

const row = (rows: SetupRow[], id: SetupRow['id']) => {
  const found = rows.find((r) => r.id === id);
  if (!found) throw new Error(`no row ${id}`);
  return found;
};

describe('Model key row', () => {
  const onEnv = { ...empty, paths: ['factory.yaml', 'environments/default.yaml'] };
  const keyed = 'spec:\n  runtime:\n    modelKey: "{{secret.anthropic}}"\n';

  it('is not done for a line with an environment file and no model key', () => {
    const rows = setupRows({ ...onEnv, factoryYaml: 'spec: {}\n' });
    expect(row(rows, 'model-key')).toMatchObject({ done: false, text: 'Model key', hint: 'Agents run on it' });
    expect(rows).toHaveLength(10);
  });

  it('is done once modelKey is set', () => {
    expect(row(setupRows({ ...onEnv, factoryYaml: keyed }), 'model-key').done).toBe(true);
  });

  it('is absent for a line that is not on secrets', () => {
    expect(setupRows(empty).some((r) => r.id === 'model-key')).toBe(false);
  });
});

describe('setupRows', () => {
  it('lists the nine rows in the picture order', () => {
    expect(setupRows(empty).map((r) => r.id)).toEqual([
      'invite',
      'slack',
      'knowledge',
      'organization',
      'code',
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

  it('starts at two of nine: the organization and the code choice are always done', () => {
    expect(doneCount(setupRows(empty))).toBe(2);
  });

  it('counts invited people as done, with a second member or a pending invitation', () => {
    expect(row(setupRows(empty), 'invite').done).toBe(false);
    expect(row(setupRows({ ...empty, members: [member, member] }), 'invite').done).toBe(true);
    expect(
      row(setupRows({ ...empty, invitations: [{ id: 'i-1', email: 'x@example.com' } as never] }), 'invite').done,
    ).toBe(true);
  });

  it('marks Set up Slack notifications done only when the setting is set', () => {
    expect(row(setupRows(empty), 'slack').done).toBe(false);
    const set = row(setupRows({ ...empty, slackSet: true }), 'slack');
    expect(set.done).toBe(true);
    expect(set.text).toBe('Set up Slack notifications');
  });

  it('marks Add knowledge done when any knowledge source exists', () => {
    expect(row(setupRows(empty), 'knowledge').done).toBe(false);
    expect(row(setupRows({ ...empty, knowledge: [source] }), 'knowledge').done).toBe(true);
  });

  it("names the factory's code choice", () => {
    const text = (basics: object) => row(setupRows({ ...empty, model: { basics } as unknown as Model }), 'code');
    const entry = (access: 'read' | 'write') => ({ name: 'app', url: 'https://github.com/acme/app', access });
    expect(text({})).toMatchObject({ done: true, text: 'No code' });
    expect(text({ repositories: [entry('read')] })).toMatchObject({
      done: true,
      text: 'Code, read only, 1 repository',
    });
    expect(text({ repositories: [entry('read'), entry('write')] }).text).toBe('Code, read and write, 2 repositories');
    expect(text({ repository: { url: 'https://github.com/acme/app' } }).text).toBe(
      'Code, read and write, 1 repository',
    );
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

  it('words the first-item row so it fits a line without code', () => {
    expect(row(setupRows(empty), 'first-item').text).toBe('First item done');
  });

  it('marks the first item done once an item of the factory is done', () => {
    expect(row(setupRows(empty), 'first-item').done).toBe(false);
    expect(row(setupRows({ ...empty, finishedItems: 1 }), 'first-item').done).toBe(true);
  });
});
