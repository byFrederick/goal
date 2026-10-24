import { describe, expect, test } from 'vitest';
import type { DefinitionProblem, SectionSummary } from '../../../api/types';
import type { Model } from '../../../shared/definition/model';
import { merge3 } from '../draft/merge';
import {
  baseSentence,
  changesSentence,
  conflictRows,
  problemPlace,
  reviewTile,
  saveSentence,
  sectionTile,
} from './state';

const summary = (section: SectionSummary['section'], changes: number, problems: number): SectionSummary => ({
  section,
  changes,
  problems,
  lines: [1, 2],
});

describe('section tiles', () => {
  test('say a problem first, then changes, then nothing', () => {
    expect(sectionTile(summary('connections', 1, 1))).toEqual({ state: 'problem', text: '1 change, 1 problem' });
    expect(sectionTile(summary('agents', 0, 2))).toEqual({ state: 'problem', text: '2 problems' });
    expect(sectionTile(summary('line', 2, 0))).toEqual({ state: 'edited', text: '2 changes' });
    expect(sectionTile(summary('basics', 0, 0))).toEqual({ state: 'unchanged', text: 'No changes' });
    expect(sectionTile(undefined)).toEqual({ state: 'unchanged', text: 'No changes' });
  });

  test('Review and publish says what stands between the draft and a publish', () => {
    expect(reviewTile({ problems: 1, changes: 3, hasDraft: true })).toEqual({
      state: 'problem',
      text: 'Fix 1 problem first',
    });
    expect(reviewTile({ problems: 0, changes: 3, hasDraft: true })).toEqual({
      state: 'ready',
      text: 'Ready to publish',
    });
    expect(reviewTile({ problems: 0, changes: 0, hasDraft: true })).toEqual({ state: 'unchanged', text: 'No changes' });
    expect(reviewTile({ problems: 0, changes: 0, hasDraft: false })).toEqual({
      state: 'unchanged',
      text: 'No changes',
    });
  });
});

describe('the status line', () => {
  test('counts changes by the sections that have them', () => {
    const sections = [summary('basics', 0, 0), summary('line', 2, 0), summary('agents', 1, 1)];
    expect(changesSentence(3, sections)).toBe('3 changes in 2 sections');
    expect(changesSentence(1, [summary('line', 1, 0)])).toBe('1 change in 1 section');
    expect(changesSentence(0, sections)).toBeNull();
  });

  test('says what the draft is based on and what runs', () => {
    const v = (number: number) => ({ id: `v${number}`, number });
    expect(baseSentence({ hasDraft: true, base: v(8), current: v(8) })).toEqual({
      text: 'Based on v8, the version running now.',
    });
    expect(baseSentence({ hasDraft: true, base: v(7), current: v(8) })).toEqual({
      text: 'Based on v7; v8 is running.',
      differs: true,
    });
    expect(baseSentence({ hasDraft: true, base: null, current: null })).toEqual({
      text: 'A first draft, never published.',
    });
    expect(baseSentence({ hasDraft: false, base: null, current: v(8) })).toEqual({
      text: 'v8 runs now. Your first change opens a draft.',
    });
  });

  test('says how saving goes, with the retry countdown', () => {
    const now = Date.parse('2026-10-24T14:40:00Z');
    const at = '2026-10-24T14:32:00Z';
    const base = { savedAt: at, retryAt: null, error: null, now };
    expect(saveSentence({ ...base, status: 'saved' })).toMatch(/^Autosaved \d\d:\d\d\.$/);
    expect(saveSentence({ ...base, status: 'saved', savedAt: null })).toBeNull();
    expect(saveSentence({ ...base, status: 'saving' })).toBe('Saving…');
    expect(saveSentence({ ...base, status: 'offline', retryAt: now + 4200 })).toBe(
      'Not saved: goald can’t be reached. Retrying in 5 s.',
    );
    expect(saveSentence({ ...base, status: 'retrying', retryAt: now + 900 })).toBe(
      'Not saved: goald failed. Retrying in 1 s.',
    );
    expect(saveSentence({ ...base, status: 'conflict' })).toBe(
      'Not saved: the same field changed in another tab or the CLI.',
    );
    expect(saveSentence({ ...base, status: 'held' })).toBe(
      'Not saved: factory.yaml doesn’t parse. Visual changes wait until it does.',
    );
    expect(saveSentence({ ...base, status: 'failed', error: { detail: 'name must be a slug', title: 'Bad' } })).toBe(
      'Not saved: name must be a slug.',
    );
  });
});

describe('the conflict dialog', () => {
  test('names each field both sides changed, with both values', () => {
    const base = { stations: [{ id: 'build', kind: 'agent', wip: 3 }], flow: { constraint: 'build' } };
    const theirs = { stations: [{ id: 'build', kind: 'agent', wip: 4 }], flow: { constraint: 'review' } };
    const ours = { stations: [{ id: 'build', kind: 'agent', wip: 2 }], flow: { constraint: 'build' } };
    const merged = merge3(base, theirs, ours);

    expect(conflictRows('line', merged.conflicts)).toEqual([
      { pointer: '/stations/0/wip', label: 'Build’s WIP limit', theirs: '4', ours: '2' },
    ]);
  });

  test('labels basics fields and removed values', () => {
    const merged = merge3(
      { title: 'Goal', work_types: ['feature'] },
      { title: 'Goal platform', work_types: ['feature', 'fix'] },
      { work_types: ['feature', 'docs'] },
    );

    expect(conflictRows('basics', merged.conflicts)).toEqual(
      expect.arrayContaining([
        { pointer: '/title', label: 'Title', theirs: 'Goal platform', ours: 'removed' },
        { pointer: '/work_types', label: 'Work types', theirs: 'feature, fix', ours: 'feature, docs' },
      ]),
    );
  });
});

describe('the problem link', () => {
  const model = {
    basics: {},
    line: { stations: [{ id: 'inbox', kind: 'queue' }] },
    agents: { agents: [{ name: 'architect' }, { name: 'frontend-engineer' }] },
    connections: [],
    review: {},
  } as Model;
  const problem = (section: string, pointer?: string): DefinitionProblem => ({
    code: 'x',
    severity: 'warning',
    check: 'x',
    section,
    ...(pointer ? { pointer } : {}),
    line: 1,
    column: 1,
    end_line: 1,
    end_column: 1,
    message: 'x',
    fixes: [],
  });

  test('opens the section and the entity the problem is at', () => {
    expect(problemPlace(problem('agents', '/agents/agents/1/builtins/web'), model)).toEqual({
      section: 'agents',
      agent: 'frontend-engineer',
    });
    expect(problemPlace(problem('line', '/line/stations/0/wip'), model)).toEqual({ section: 'line', station: 'inbox' });
  });

  test('falls back to its section, and files to Review', () => {
    expect(problemPlace(problem('basics'), model)).toEqual({ section: 'basics' });
    expect(problemPlace(problem('files'), model)).toEqual({ section: 'review' });
  });
});
