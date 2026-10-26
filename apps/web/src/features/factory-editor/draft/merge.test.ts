import { describe, expect, it } from 'vitest';
import type { LineSection, ModelStation } from '../../../shared/definition/model';
import { merge3, resolve } from './merge';

const base: LineSection = {
  stations: [
    { id: 'inbox', kind: 'queue' },
    { id: 'build', kind: 'agent', agent: 'engineer', wip: 2, checks: ['task check'] },
    { id: 'review', kind: 'agent', agent: 'reviewer', wip: 1 },
    { id: 'done', kind: 'done' },
  ],
  flow: { constraint: 'build' },
};

const edit = (f: (s: ModelStation[], line: LineSection) => void): LineSection => {
  const line = structuredClone(base);
  f(line.stations as ModelStation[], line);
  return line;
};

const station = (line: LineSection, id: string) => line.stations?.find((s) => s.id === id);

describe('merge3', () => {
  it('merges edits of different fields without asking', () => {
    const theirs = edit((s) => {
      (s[1] as ModelStation).wip = 4;
    });
    const ours = edit((s, line) => {
      (s[2] as ModelStation).wip = 3;
      line.flow = { constraint: 'review' };
    });

    const { value, conflicts } = merge3(base, theirs, ours);

    expect(conflicts).toEqual([]);
    expect(station(value, 'build')?.wip).toBe(4);
    expect(station(value, 'review')?.wip).toBe(3);
    expect(value.flow).toEqual({ constraint: 'review' });
  });

  it('asks about a field both sides changed, and keeps theirs when told', () => {
    const theirs = edit((s) => {
      (s[1] as ModelStation).wip = 4;
    });
    const ours = edit((s) => {
      (s[1] as ModelStation).wip = 2.5;
      (s[2] as ModelStation).wip = 3;
    });

    const merged = merge3(base, theirs, ours);

    expect(merged.conflicts).toEqual([
      { pointer: '/stations/1/wip', path: ['stations', 'build', 'wip'], base: 2, theirs: 4, ours: 2.5 },
    ]);
    expect(station(merged.value, 'build')?.wip).toBe(2.5);
    const kept = resolve(merged, { '/stations/1/wip': 'theirs' });
    expect(station(kept, 'build')?.wip).toBe(4);
    expect(station(kept, 'review')?.wip).toBe(3);
  });

  it('takes the same change from both sides as one', () => {
    const both = edit((s) => {
      (s[1] as ModelStation).wip = 4;
    });
    expect(merge3(base, both, structuredClone(both))).toEqual({ value: both, conflicts: [] });
  });

  it('matches stations by id across a reorder', () => {
    const theirs = edit((s) => {
      const [review] = s.splice(2, 1);
      s.splice(1, 0, review as ModelStation);
    });
    const ours = edit((s) => {
      (s[1] as ModelStation).wip = 3; // build, at index 1 in base and ours
    });

    const { value, conflicts } = merge3(base, theirs, ours);

    expect(conflicts).toEqual([]);
    expect(value.stations?.map((s) => s.id)).toEqual(['inbox', 'review', 'build', 'done']);
    expect(station(value, 'build')?.wip).toBe(3);
  });

  it('keeps stations each side added and drops one a side removed', () => {
    const theirs = edit((s) => {
      s.splice(3, 0, { id: 'approve', kind: 'human' });
    });
    const ours = edit((s) => {
      s.splice(1, 0, { id: 'spec', kind: 'agent', agent: 'writer' });
      s.splice(3, 1); // review
    });

    const { value, conflicts } = merge3(base, theirs, ours);

    expect(conflicts).toEqual([]);
    expect(value.stations?.map((s) => s.id)).toEqual(['inbox', 'spec', 'build', 'approve', 'done']);
  });

  it('asks when one side removed a station the other changed', () => {
    const theirs = edit((s) => {
      s.splice(2, 1); // review
    });
    const ours = edit((s) => {
      (s[2] as ModelStation).wip = 5;
    });

    const merged = merge3(base, theirs, ours);

    expect(merged.conflicts).toMatchObject([
      { pointer: '/stations/2', path: ['stations', 'review'], theirs: undefined },
    ]);
    expect(resolve(merged, { '/stations/2': 'theirs' }).stations?.map((s) => s.id)).toEqual(['inbox', 'build', 'done']);
    expect(station(resolve(merged, {}), 'review')?.wip).toBe(5);
  });

  it('treats a list without identities as one value', () => {
    const theirs = edit((s) => {
      (s[1] as ModelStation).checks = ['task check', 'pnpm test'];
    });
    const ours = edit((s) => {
      (s[1] as ModelStation).checks = ['make'];
    });
    expect(merge3(base, theirs, ours).conflicts.map((c) => c.pointer)).toEqual(['/stations/1/checks']);
  });

  it('merges connections, a list at the top of its section, by name', () => {
    const b = [
      { name: 'github', agents: ['a'] },
      { name: 'browser', agents: ['b'] },
    ];
    const t = [
      { name: 'github', agents: ['a', 'c'] },
      { name: 'browser', agents: ['b'] },
    ];
    const o = [
      { name: 'browser', agents: [] as string[] },
      { name: 'github', agents: ['a'] },
    ];
    const { value, conflicts } = merge3(b, t, o);
    expect(conflicts).toEqual([]);
    expect(value).toEqual([
      { name: 'browser', agents: [] },
      { name: 'github', agents: ['a', 'c'] },
    ]);
  });
});
