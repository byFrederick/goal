import { describe, expect, it } from 'vitest';
import type { Item, ItemEvent, ItemFlow, Station } from '../../api/types';
import { noFlow, stepsOf, summaryOf } from './steps';

const stations: Station[] = [
  { id: 'intake', kind: 'queue' },
  { id: 'spec', kind: 'agent', description: 'Writes the spec' },
  { id: 'build', kind: 'agent' },
  { id: 'review', kind: 'agent', description: 'Then merge' },
  { id: 'done', kind: 'done' },
];

const created = '2026-10-25T08:00:00Z';
const now = Date.parse('2026-10-25T11:00:00Z');

function item(overrides: Partial<Item> = {}): Item {
  return {
    id: 'item-1',
    ref: 'goal#1',
    factory_id: 'factory-1',
    number: 1,
    type: 'feature',
    title: 'A change',
    body: '',
    station: 'build',
    state: 'in_progress',
    class_of_service: 'standard',
    priority: 0,
    rework_count: 0,
    cost_usd: 0,
    outputs: {},
    created_at: created,
    updated_at: created,
    ...overrides,
  };
}

/** The item at build: intake for 12 minutes, spec for 38, and build open since 08:50. */
const flow: ItemFlow = {
  ...noFlow,
  target_seconds: 48 * 3600,
  spans: [
    {
      station: 'intake',
      state: 'done',
      rework_round: 0,
      rework_by: null,
      entered_at: created,
      left_at: '2026-10-25T08:12:00Z',
    },
    {
      station: 'spec',
      state: 'done',
      rework_round: 0,
      rework_by: null,
      entered_at: '2026-10-25T08:12:00Z',
      left_at: '2026-10-25T08:50:00Z',
    },
    {
      station: 'build',
      state: 'in_progress',
      rework_round: 0,
      rework_by: null,
      entered_at: '2026-10-25T08:50:00Z',
      left_at: null,
    },
  ],
};

function event(kind: string, station: string): ItemEvent {
  return { id: 1, item_id: 'item-1', at: created, kind, station, actor: 'Freddy Cruz', data: {} };
}

describe('stepsOf', () => {
  it('lists every station of the factory when the item has no flow', () => {
    const steps = stepsOf({ item: item(), stations, events: [], flow: noFlow, now });

    expect(steps.map((s) => s.name)).toEqual(['Intake', 'Spec', 'Build', 'Review', 'Done']);
    expect(steps.map((s) => s.state)).toEqual(['todo', 'todo', 'current', 'todo', 'todo']);
  });

  it('gives a station that has not been reached its description', () => {
    const steps = stepsOf({ item: item(), stations, events: [], flow: noFlow, now });

    expect(steps[3]?.detail).toBe('Then merge');
  });

  it('gives a done step its time and the outcome of its decisions', () => {
    const steps = stepsOf({
      item: item(),
      stations,
      events: [event('approved', 'spec')],
      flow,
      now,
    });

    expect(steps[1]).toEqual({ name: 'Spec', state: 'done', detail: '38 min, accepted' });
  });

  it('counts the sent-backs of a station in its outcome', () => {
    const steps = stepsOf({
      item: item(),
      stations,
      events: [event('rejected', 'spec'), event('rejected', 'spec')],
      flow,
      now,
    });

    expect(steps[1]?.detail).toBe('38 min, sent back 2×');
  });

  it('shows the time so far at the current step, and waiting on you when the item waits for a person', () => {
    const waiting = stepsOf({ item: item({ state: 'waiting_human' }), stations, events: [], flow, now });
    const running = stepsOf({ item: item(), stations, events: [], flow, now });

    expect(waiting[2]).toEqual({ name: 'Build', state: 'current', detail: '2 h 10 min, waiting on you' });
    expect(running[2]?.detail).toBe('2 h 10 min');
  });

  it('says the target and the time so far at the done station', () => {
    const steps = stepsOf({ item: item(), stations, events: [], flow, now });

    expect(steps[4]).toEqual({ name: 'Done', state: 'todo', detail: 'Target 48 h, 3 h so far' });
  });

  it('says the time so far at the done station when the factory has no target', () => {
    const steps = stepsOf({ item: item(), stations, events: [], flow: { ...flow, target_seconds: null }, now });

    expect(steps[4]?.detail).toBe('3 h so far');
  });
});

describe('the phone summary line under the stepper', () => {
  it('says the time at the station, the time in the line and the target', () => {
    expect(summaryOf({ item: item(), flow, now })).toBe('2 h 10 min in Build, 3 h in the line of a 48 h target');
  });

  it('leaves the target out when the flow has none', () => {
    expect(summaryOf({ item: item(), flow: { ...flow, target_seconds: null }, now })).toBe(
      '2 h 10 min in Build, 3 h in the line',
    );
  });

  it('stops the clock at the item's end once it is done', () => {
    const done = item({ state: 'done', station: 'done', done_at: '2026-10-25T09:00:00Z' });
    expect(summaryOf({ item: done, flow: { ...flow, target_seconds: null }, now })).toBe(
      '0 s in Done, 1 h in the line',
    );
  });
});
