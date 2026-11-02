import { describe, expect, test } from 'vitest';
import type { DefinitionProblem, SectionSummary } from '../../../api/types';
import type { Model } from '../../../shared/definition/model';
import { merge3 } from '../draft/merge';
import {
  baseSentence,
  changesSentence,
  conflictRows,
  parseErrorTile,
  problemPlace,
  reviewTile,
  saveSentence,
  sectionSummary,
  sectionTile,
  viewTileText,
  yamlTileText,
} from './state';

const summary = (section: SectionSummary['section'], changes: number, problems: number): SectionSummary => ({
  section,
  changes,
  problems,
  lines: [1, 2],
});

describe('section tiles', () => {
  test('jump to their block in YAML mode, and all say so while factory.yaml does not parse', () => {
    expect(yamlTileText('line', [31, 45])).toBe('board, lines 31–45');
    expect(yamlTileText('review', [46, 46])).toBe('budget, line 46');
    const error = { line: 12, column: 3, message: 'did not find expected key' };
    expect(parseErrorTile('line', error)).toEqual({ state: 'problem', text: 'Syntax error, line 12' });
    expect(parseErrorTile('review', error)).toEqual({ state: 'problem', text: 'Fix factory.yaml first' });
  });

  const model = {
    basics: { name: 'acme', work_types: ['feature', 'bug'], knowledge_sources: ['docs', 'wiki'] },
    line: {
      stations: [
        { id: 'inbox', name: 'Inbox', kind: 'queue' },
        { id: 'build', name: 'Build', kind: 'agent' },
      ],
      flow: { constraint: 'build' },
    },
    agents: { default_model: 'm1', agents: [{ name: 'a', model: 'm2' }, { name: 'b' }, { name: 'c', model: 'm2' }] },
    connections: [{ name: 'github' }, { name: 'slack' }],
    review: {},
  } as unknown as Model;

  test('sectionSummary says what each section holds, from the model only', () => {
    expect(sectionSummary('basics', model)).toBe('acme, 2 work types');
    expect(sectionSummary('line', model)).toBe('2 stations, constraint Build');
    expect(sectionSummary('agents', model)).toBe('3 agents on 2 models');
    expect(sectionSummary('connections', model)).toBe('github, slack, 2 sources');
  });

  test('sectionSummary uses singular forms and copes with an empty model', () => {
    const one = {
      basics: { name: 'zed', work_types: ['x'], knowledge_sources: ['k'] },
      line: { stations: [{ id: 's', kind: 'queue' }] },
      agents: { agents: [{ name: 'a', model: 'q' }] },
      connections: [{ name: 'only' }],
      review: {},
    } as unknown as Model;
    expect(sectionSummary('basics', one)).toBe('zed, 1 work type');
    expect(sectionSummary('line', one)).toBe('1 station');
    expect(sectionSummary('agents', one)).toBe('1 agent on 1 model');
    expect(sectionSummary('connections', one)).toBe('only, 1 source');
    const empty = { basics: {}, line: {}, agents: {}, connections: [], review: {} } as unknown as Model;
    expect(sectionSummary('basics', empty)).toBe('0 work types');
    expect(sectionSummary('line', empty)).toBe('0 stations');
    expect(sectionSummary('agents', empty)).toBe('0 agents on 0 models');
    expect(sectionSummary('connections', empty)).toBe('0 sources');
  });

  test('sectionSummary never depends on names in the source', () => {
    const odd = {
      basics: { name: 'qzx-1' },
      line: { stations: [{ id: 'k9', name: 'Zork' }], flow: { constraint: 'k9' } },
    };
    expect(sectionSummary('basics', odd as unknown as Model)).toBe('qzx-1, 0 work types');
    expect(sectionSummary('line', odd as unknown as Model)).toBe('1 station, constraint Zork');
  });

  test('a tile keeps the summary and puts problems and changes in the badge', () => {
    expect(sectionTile(summary('connections', 1, 1), 'connections', model)).toEqual({
      state: 'problem',
      text: 'github, slack, 2 sources',
      badge: '1 change, 1 problem',
    });
    expect(sectionTile(summary('agents', 0, 2), 'agents', model)).toMatchObject({
      state: 'problem',
      badge: '2 problems',
    });
    expect(sectionTile(summary('line', 2, 0), 'line', model)).toEqual({
      state: 'edited',
      text: '2 stations, constraint Build',
      badge: '2 changes',
    });
    expect(sectionTile(summary('basics', 0, 0), 'basics', model)).toEqual({
      state: 'unchanged',
      text: 'acme, 2 work types',
    });
    expect(sectionTile(undefined, 'basics', model)).toEqual({ state: 'unchanged', text: 'acme, 2 work types' });
  });

  test('say what each section holds when the editor is view only', () => {
    const model = {
      basics: { name: 'goal' },
      line: {
        stations: [
          { id: 'inbox', kind: 'queue' },
          { id: 'done', kind: 'done' },
        ],
      },
      agents: { agents: [{ name: 'engineer' }] },
      connections: [],
      review: {},
    } as unknown as Model;
    expect(viewTileText('basics', model)).toBe('Purpose and repository');
    expect(viewTileText('line', model)).toBe('2 stations');
    expect(viewTileText('agents', model)).toBe('1 agent');
    expect(viewTileText('connections', model)).toBe('0 connections');
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
    expect(reviewTile({ problems: 0, changes: 3, hasDraft: true, base: 12 })).toEqual({
      state: 'ready',
      text: '3 changes from v12',
    });
    expect(reviewTile({ problems: 0, changes: 1, hasDraft: true, base: 12 }).text).toBe('1 change from v12');
    expect(reviewTile({ problems: 0, changes: 0, hasDraft: true })).toEqual({
      state: 'unchanged',
      text: 'Nothing to publish',
    });
    expect(reviewTile({ problems: 0, changes: 0, hasDraft: false })).toEqual({
      state: 'unchanged',
      text: 'Nothing to publish',
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
