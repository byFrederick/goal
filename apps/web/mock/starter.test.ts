// The mock's starter course behaves like goald: a line without runtime.modelKey blocks the claimed item with the
// code model_key_missing and starts no run; a line with one claims it and runs it.
import type { Run } from '@goal/api-client';
import { describe, expect, it } from 'vitest';
import type { MockItem } from './fixtures.ts';
import { runStarterCourse } from './starter.ts';

function course(keyed: boolean) {
  const item = { id: 'item-abcdef123456', station: 'inbox', state: 'queued', cost_usd: 0 } as unknown as MockItem;
  const events: { kind: string; data: Record<string, unknown> }[] = [];
  const runs: Run[] = [];
  runStarterCourse({
    item,
    runs,
    emit: (_it, kind, _actor, data = {}) => void events.push({ kind, data }),
    now: Date.parse('2026-11-06T12:00:00Z'),
    keyed,
    agent: 'builder',
    version: { id: 'v1', number: 1 },
  });
  return { item, events, runs };
}

describe('runStarterCourse', () => {
  it('blocks the claimed item with model_key_missing and no run when the line has no key', () => {
    const { item, events, runs } = course(false);
    expect(item.state).toBe('blocked');
    expect(events.map((e) => e.kind)).toEqual(['created', 'claimed', 'blocked']);
    expect(events.at(-1)?.data.code).toBe('model_key_missing');
    expect(runs).toEqual([]);
  });

  it('claims the item and runs it when the line has a key', () => {
    const { item, events, runs } = course(true);
    expect(item.state).toBe('done');
    expect(events.map((e) => e.kind)).toContain('claimed');
    expect(events.map((e) => e.kind)).not.toContain('blocked');
    expect(runs).toHaveLength(1);
    expect(runs[0]).toMatchObject({ item_id: item.id, agent: 'builder', status: 'succeeded' });
  });
});
