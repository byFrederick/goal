import { describe, expect, it } from 'vitest';
import type { Change, DefinitionProblem } from '../../../api/types';
import {
  blockedSentence,
  defaultMessage,
  hunksOf,
  mergeMoveNeeds,
  movesFromProblems,
  parseCompare,
  publishExplainer,
  publishedLine,
  reviewCompare,
  sourceText,
} from './review';

const change = (over: Partial<Change>): Change => ({
  section: 'line',
  op: 'changed',
  target: { kind: 'station', id: 'build' },
  summary: 'Build also runs pnpm test',
  from_lines: [0, 0],
  to_lines: [0, 0],
  ...over,
});

describe('the comparison', () => {
  it('is the one asked for, else the draft against its base, or its source in the wizard', () => {
    expect(reviewCompare('7..draft', { place: 'definition', hasDraft: true })).toEqual({ from: '7', to: 'draft' });
    expect(reviewCompare(undefined, { place: 'definition', hasDraft: true })).toEqual({ from: 'base', to: 'draft' });
    expect(reviewCompare(undefined, { place: 'wizard', hasDraft: true })).toEqual({ from: 'source', to: 'draft' });
    expect(reviewCompare(undefined, { place: 'definition', hasDraft: false })).toBeUndefined();
    expect(parseCompare('7..draft..x')).toBeUndefined();
  });
});

describe('hunksOf', () => {
  const before = ['stations:', '  - id: build', '    wip: 3', '    checks: [schema]', '  - id: merge', ''].join('\n');
  const after = [
    'stations:',
    '  - id: build',
    '    wip: 3',
    '    checks: [schema, pnpm test]',
    '  - id: merge',
    '',
  ].join('\n');

  it('numbers new and context lines, not removed ones, and heads a hunk with the changes it touches', () => {
    const [hunk, ...rest] = hunksOf(before, after, [
      change({ to_lines: [2, 4], from_lines: [2, 4] }),
      change({ target: { kind: 'station', id: 'merge' }, to_lines: [5, 5], from_lines: [5, 5] }),
      change({ section: 'connections', target: { kind: 'connection', id: 'browser' }, to_lines: [20, 22] }),
    ]);
    expect(rest).toEqual([]);
    expect(hunk?.header).toBe('Line: build station, Line: merge station');
    expect(hunk?.lines).toEqual([
      { kind: 'context', text: '  - id: build', number: 2 },
      { kind: 'context', text: '    wip: 3', number: 3 },
      { kind: 'removed', text: '    checks: [schema]' },
      { kind: 'added', text: '    checks: [schema, pnpm test]', number: 4 },
      { kind: 'context', text: '  - id: merge', number: 5 },
    ]);
  });

  it('shows nothing for equal texts, and a whole new file as added', () => {
    expect(hunksOf(after, after)).toEqual([]);
    expect(hunksOf('', '# x\n')[0]?.lines).toEqual([{ kind: 'added', text: '# x', number: 1 }]);
  });
});

describe('publishing', () => {
  it('starts the version note from the first two changes', () => {
    expect(
      defaultMessage([change({}), change({ summary: 'Merge becomes a merge step' }), change({ summary: 'x' })]),
    ).toBe('Build also runs pnpm test; Merge becomes a merge step');
    expect(defaultMessage([change({ summary: 'y'.repeat(300) })])).toHaveLength(200);
  });

  it('reads the station moves from the publish fixes, and keeps a row once it is mapped', () => {
    const problem: DefinitionProblem = {
      code: 'items-at-removed-station',
      severity: 'error',
      check: 'items',
      section: 'line',
      line: 0,
      column: 0,
      end_line: 0,
      end_column: 0,
      message: 'the draft removes station qa',
      fixes: ['review', 'done'].map((to) => ({
        label: `Move them to ${to}`,
        section: 'publish',
        patch: [{ op: 'add' as const, path: '/station_moves/qa', value: to }],
      })),
    };
    const needs = movesFromProblems([problem]);
    expect(needs).toEqual([{ station: 'qa', options: ['review', 'done'] }]);
    expect(mergeMoveNeeds(needs, [])).toEqual(needs);
    expect(mergeMoveNeeds(needs, [{ station: 'lint', options: ['build'] }]).map((n) => n.station)).toEqual([
      'qa',
      'lint',
    ]);
  });

  it('says what stands in the way and what publishing changes', () => {
    expect(blockedSentence(1)).toBe('Fix 1 problem first. Your draft stays saved.');
    expect(publishExplainer(8, 9)).toBe(
      'v8 keeps running until you publish. Runs already started finish on v8; new runs start on v9.',
    );
    expect(publishExplainer(null, 1)).toMatch(/^Publishing saves version 1\./);
  });
});

describe('version rows', () => {
  it('say when and by whom, and where a draft started', () => {
    expect(publishedLine('2026-10-21T14:32:00Z', { kind: 'user', id: 'local', name: 'Local user' })).toBe(
      'Oct 21 by Local user',
    );
    expect(sourceText({ kind: 'template', template: 'cost-review@2' }, 'Cost review')).toBe('Template Cost review v2');
    expect(sourceText({ kind: 'clone', factory: 'sre', version: '3' })).toBe('Clone of sre v3');
  });
});
