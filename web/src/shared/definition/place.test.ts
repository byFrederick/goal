import { describe, expect, it } from 'vitest';
import type { Ranges } from '../../api/types';
import type { Model } from './model';
import { firstLine, placeOf, placeOfFile, pointerAt, pointerOf, promptFile } from './place';

const model: Model = {
  basics: { name: 'goal', work_types: ['feature'] },
  line: {
    stations: [
      { id: 'inbox', kind: 'queue' },
      { id: 'build', kind: 'agent', agent: 'engineer', wip: 2 },
      { id: 'done', kind: 'done' },
    ],
  },
  agents: { agents: [{ name: 'engineer', prompt: { path: './agents/engineer.md' } }] },
  connections: [{ name: 'github', agents: ['engineer'] }],
  review: {},
};

const ranges: Ranges = {
  '/basics': [3, 6],
  '/basics/name': [4, 4],
  '/agents/agents': [10, 12],
  '/agents/agents/0': [10, 12],
  '/agents/agents/0/prompt': [11, 11],
  '/connections': [14, 15],
  '/connections/0': [15, 15],
  '/line': [17, 25],
  '/line/stations': [18, 25],
  '/line/stations/0': [19, 19],
  '/line/stations/1': [20, 24],
  '/line/stations/1/wip': [23, 23],
  '/line/stations/2': [25, 25],
};

describe('pointer ↔ line', () => {
  it('finds the innermost pointer whose lines hold the cursor', () => {
    expect(pointerAt(ranges, 23)).toBe('/line/stations/1/wip');
    expect(pointerAt(ranges, 21)).toBe('/line/stations/1');
    expect(pointerAt(ranges, 18)).toBe('/line/stations');
    expect(pointerAt(ranges, 1)).toBeUndefined();
  });

  it("gives a pointer's first line, or its nearest parent's", () => {
    expect(firstLine(ranges, '/line/stations/1')).toBe(20);
    expect(firstLine(ranges, '/line/stations/1/checks')).toBe(20);
    expect(firstLine(ranges, '/review/budget')).toBeUndefined();
  });
});

describe('pointer ↔ place', () => {
  it('names the section and entity a pointer is in', () => {
    expect(placeOf(model, '/line/stations/1/wip')).toEqual({ section: 'line', station: 'build' });
    expect(placeOf(model, '/agents/agents/0/prompt')).toEqual({ section: 'agents', agent: 'engineer' });
    expect(placeOf(model, '/connections/0')).toEqual({ section: 'connections', connection: 'github' });
    expect(placeOf(model, '/basics/name')).toEqual({ section: 'basics' });
    expect(placeOf(model, '/line/stations/9')).toEqual({ section: 'line' });
    expect(placeOf(model, '/spec/nothing')).toBeUndefined();
  });

  it("gives the entity's pointer back, so a jump lands on its first line", () => {
    expect(pointerOf(model, { section: 'line', station: 'build' })).toBe('/line/stations/1');
    expect(pointerOf(model, { section: 'agents', agent: 'engineer' })).toBe('/agents/agents/0');
    expect(pointerOf(model, { section: 'connections', connection: 'github' })).toBe('/connections/0');
    expect(pointerOf(model, { section: 'line', station: 'gone' })).toBe('/line');
    expect(firstLine(ranges, pointerOf(model, placeOf(model, '/line/stations/1/wip') ?? { section: 'line' }))).toBe(20);
  });

  it("maps an agent's prompt file to the agent, and back", () => {
    expect(placeOfFile(model, 'agents/engineer.md')).toEqual({ section: 'agents', agent: 'engineer' });
    expect(placeOfFile(model, 'factory.yaml')).toBeUndefined();
    expect(promptFile(model, 'engineer')).toBe('agents/engineer.md');
    expect(promptFile(model, 'nobody')).toBeUndefined();
  });
});
