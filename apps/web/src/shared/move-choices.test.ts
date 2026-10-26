import { describe, expect, it } from 'vitest';
import type { Board } from '../api/types';
import { arrivalState, moveChoices, movedOnBoard } from './move-choices';

const line = [
  { id: 'inbox', kind: 'queue' as const },
  { id: 'spec', kind: 'agent' as const },
  { id: 'build', kind: 'agent' as const },
  { id: 'review', kind: 'human' as const },
  { id: 'done', kind: 'done' as const },
];

describe('moveChoices', () => {
  it('offers every station but the current one and done stations, upstream ones needing a reason', () => {
    expect(moveChoices(line, 'build')).toEqual([
      { id: 'inbox', kind: 'queue', upstream: true },
      { id: 'spec', kind: 'agent', upstream: true },
      { id: 'review', kind: 'human', upstream: false },
    ]);
  });

  it('treats every station as downstream of one that left the line', () => {
    expect(moveChoices(line, 'gone').every((c) => !c.upstream)).toBe(true);
  });
});

describe('arrivalState', () => {
  it('waits for a person at a human station and queues everywhere else', () => {
    expect(arrivalState('human')).toBe('waiting_human');
    expect(arrivalState('agent')).toBe('queued');
    expect(arrivalState('queue')).toBe('queued');
  });
});

describe('movedOnBoard', () => {
  const card = {
    id: 'i1',
    ref: 'goal#1',
    state: 'in_progress',
    station: 'build',
    rework_count: 0,
    agent: 'builder-1',
  } as Board['stations'][number]['items'][number];
  const board = {
    stations: line.map((s) => ({ id: s.id, kind: s.kind, items: s.id === 'build' ? [card] : [] })),
  } as unknown as Board;

  it('takes the card out of its column and puts it in the target, as goald will', () => {
    const next = movedOnBoard(board, 'i1', 'spec', line);
    expect(next.stations.find((s) => s.id === 'build')?.items).toEqual([]);
    expect(next.stations.find((s) => s.id === 'spec')?.items).toEqual([
      { ...card, station: 'spec', state: 'queued', rework_count: 1, agent: null },
    ]);
    expect(board.stations.find((s) => s.id === 'build')?.items).toHaveLength(1);
  });

  it('counts no rework downstream and waits on a person at a human station', () => {
    const card2 = movedOnBoard(board, 'i1', 'review', line).stations.find((s) => s.id === 'review')?.items[0];
    expect(card2).toMatchObject({ state: 'waiting_human', rework_count: 0 });
  });
});
