import { describe, expect, it } from 'vitest';
import type { AssemblyPart, BoardLane } from '../../api/types';
import {
  agentLabel,
  barSegments,
  hasOpenParts,
  openPartCount,
  partStatus,
  partTone,
  pauseAction,
  planWaves,
  spend,
  timeToFinish,
} from './assembly-plan';

function part(n: number, over: Partial<AssemblyPart> = {}): AssemblyPart {
  return {
    id: `id-${n}`,
    ref: `goal#${100 + n}`,
    number: 100 + n,
    index: n,
    key: `part-${n}`,
    title: `Part ${n}`,
    paths: [],
    station: 'build',
    state: 'queued',
    needs: [],
    waiting_on: [],
    wave: 0,
    cost_usd: 0,
    pr_url: null,
    agent: null,
    created_at: '2026-10-20T10:00:00Z',
    done_at: null,
    ...over,
  } as AssemblyPart;
}

const day = 24 * 3600 * 1000;

describe('barSegments', () => {
  it('has one segment per part, in plan order, toned by state', () => {
    const parts = [
      part(1, { state: 'done' }),
      part(2, { state: 'in_progress' }),
      part(3, { state: 'queued' }),
      part(4, { state: 'cancelled' }),
    ];
    expect(barSegments(parts).map((s) => s.tone)).toEqual(['done', 'working', 'waiting', 'cancelled']);
    expect(barSegments(parts).map((s) => s.key)).toEqual(['id-1', 'id-2', 'id-3', 'id-4']);
  });

  it('tones a blocked or person-held part as waiting', () => {
    expect(partTone({ state: 'blocked' })).toBe('waiting');
    expect(partTone({ state: 'waiting_human' })).toBe('waiting');
  });
});

describe('partStatus', () => {
  it('is working while in progress and ready once done', () => {
    expect(partStatus(part(1, { state: 'in_progress' }))).toBe('working');
    expect(partStatus(part(1, { state: 'done' }))).toBe('ready');
  });

  it('waits for another item while it names open needs', () => {
    expect(partStatus(part(2, { state: 'queued', waiting_on: ['goal#101'] }))).toBe('waiting');
  });

  it('is ready when its own state says ready, and waiting otherwise', () => {
    expect(partStatus(part(2, { state: 'ready' }))).toBe('ready');
    expect(partStatus(part(2, { state: 'queued' }))).toBe('waiting');
  });
});

describe('planWaves', () => {
  it('groups parts by wave, earliest first, in plan order within a wave, and leaves cancelled parts out', () => {
    const waves = planWaves([
      part(1, { wave: 0 }),
      part(2, { wave: 1 }),
      part(3, { wave: 0, state: 'cancelled' }),
      part(4, { wave: 0 }),
    ]);
    expect(waves.map((w) => w.wave)).toEqual([0, 1]);
    expect(waves[0]?.parts.map((p) => p.index)).toEqual([1, 4]);
    expect(waves[1]?.parts.map((p) => p.index)).toEqual([2]);
  });
});

describe('spend', () => {
  it('adds the assembly cost to its parts and shows no budget without a per-item budget', () => {
    const parts = [part(1, { cost_usd: 1.5 }), part(2, { cost_usd: 0.25 })];
    expect(spend({ cost_usd: 0.5 }, parts, null)).toEqual({ spent: 2.25, budget: null });
  });

  it('budgets each item times the parts in all', () => {
    const parts = [part(1), part(2), part(3)];
    expect(spend({ cost_usd: 0 }, parts, 4)).toEqual({ spent: 0, budget: 12 });
  });
});

describe('timeToFinish', () => {
  it('is null with no merged part to measure by', () => {
    expect(timeToFinish([part(1), part(2, { state: 'in_progress' })])).toBeNull();
  });

  it('is the mean lead time of merged parts times the waves that still hold an open part', () => {
    const parts = [
      part(1, { state: 'done', created_at: '2026-10-20T00:00:00Z', done_at: '2026-10-21T00:00:00Z' }),
      part(2, { state: 'done', created_at: '2026-10-20T00:00:00Z', done_at: '2026-10-23T00:00:00Z' }),
      part(3, { wave: 1 }),
      part(4, { wave: 2 }),
      part(5, { wave: 2 }),
    ];
    // Mean of 1 and 3 days is 2 days; open parts sit in waves 1 and 2.
    expect(timeToFinish(parts)).toBe(2 * 2 * day);
  });

  it('has nothing to estimate once no part is open', () => {
    const parts = [part(1, { state: 'done', done_at: '2026-10-21T00:00:00Z' })];
    expect(hasOpenParts(parts)).toBe(false);
    expect(hasOpenParts([part(2)])).toBe(true);
  });
});

describe('agentLabel', () => {
  const lanes: BoardLane[] = [
    {
      key: 'infra-engineer',
      kind: 'agent',
      role: 'Infra engineer',
      model: 'claude-opus-5-5',
      model_label: 'Opus 5.5',
      slots: 1,
    },
    { key: 'person', kind: 'person', role: 'Owner', model: null, model_label: null, slots: null },
  ] as BoardLane[];

  it('names the role and model of the agent lane', () => {
    expect(agentLabel('infra-engineer', lanes)).toBe('Infra engineer · Opus 5.5');
  });

  it('falls back to the agent name as words while the board is not loaded', () => {
    expect(agentLabel('infra-engineer', [])).toBe('Infra engineer');
  });
});

describe('openPartCount', () => {
  it('counts the parts neither done nor cancelled', () => {
    const parts = [part(1, { state: 'done' }), part(2, { state: 'cancelled' }), part(3), part(4, { state: 'blocked' })];
    expect(openPartCount(parts)).toBe(2);
  });
});

describe('pauseAction', () => {
  it('pauses while some open part still runs', () => {
    expect(pauseAction([part(1, { state: 'in_progress' }), part(2, { state: 'blocked' })])).toBe('pause');
  });

  it('resumes once every open part is blocked; done and cancelled parts do not count', () => {
    expect(
      pauseAction([part(1, { state: 'done' }), part(2, { state: 'blocked' }), part(3, { state: 'blocked' })]),
    ).toBe('resume');
  });

  it('offers nothing when no part is open', () => {
    expect(pauseAction([part(1, { state: 'done' }), part(2, { state: 'cancelled' })])).toBeNull();
  });
});
