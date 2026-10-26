import { describe, expect, it } from 'vitest';
import type { LineSection } from '../../../shared/definition/model';
import {
  bufferSentence,
  checkLabel,
  intakeSummary,
  noMergeHint,
  previewDetail,
  reworkSentence,
  upstreamOf,
} from './labels';

const line: LineSection = {
  stations: [
    { id: 'inbox', name: 'Inbox', kind: 'queue' },
    { id: 'spec', name: 'Spec', kind: 'agent', agent: 'architect', agents: 2, wip: 2, buffer: 2 },
    { id: 'build', name: 'Build', kind: 'agent', route: { backend: 'be', frontend: 'fe', infra: 'ops' } },
    { id: 'review', name: 'Review', kind: 'agent', agent: 'reviewer', gate: { on_reject: 'build', max_rework: 2 } },
    { id: 'merge', name: 'Merge', kind: 'human', action: 'merge' },
    { id: 'done', name: 'Done', kind: 'done' },
  ],
  librarian: 'librarian',
};
const at = (id: string) => line.stations?.find((s) => s.id === id) ?? { id, kind: 'queue' as const };

describe('line labels', () => {
  it('summarizes the intake, manual when it names no source', () => {
    expect(intakeSummary(undefined)).toBe('Manual (people and the API)');
    expect(intakeSummary([{ source: 'github-issues', labels: ['goal'] }])).toBe('GitHub issues labelled goal');
    expect(intakeSummary([{ source: 'cron', cron: '0 9 * * 1' }])).toBe('On a schedule');
  });

  it('says who works at each station in the preview', () => {
    expect(previewDetail(at('inbox'), line)).toBe('queue');
    expect(previewDetail(at('spec'), line)).toBe('architect × 2');
    expect(previewDetail(at('build'), line)).toBe('3 by lane');
    expect(previewDetail(at('review'), line)).toBe('reviewer, gate');
    expect(previewDetail({ ...at('review'), action: 'merge' }, line)).toBe('reviewer, gate, merge');
    expect(previewDetail(at('merge'), line)).toBe('you, merge');
    expect(previewDetail(at('done'), line)).toBe('librarian');
    expect(previewDetail(at('done'), { ...line, librarian: null })).toBe('items close');
  });

  it('says where a gate sends work back, and how often', () => {
    expect(reworkSentence(at('review'), line.stations ?? [])).toBe('Review sends work back to Build, at most twice.');
    expect(reworkSentence(at('spec'), line.stations ?? [])).toBeUndefined();
  });

  it('names the buffer a queue releases into only under drum-buffer-rope', () => {
    expect(bufferSentence(line, 'inbox')).toBeUndefined();
    expect(bufferSentence({ ...line, flow: { release: 'drum-buffer-rope' } }, 'inbox')).toBe(
      'New work enters when Spec has fewer than 2 waiting',
    );
  });

  it('names a schema check by its contract and a command by itself', () => {
    expect(checkLabel({ kind: 'schema' }, 'spec')).toBe('spec contract');
    expect(checkLabel('pnpm test', undefined)).toBe('pnpm test');
  });

  it('hints that pull requests stay open when a factory with a repository merges nothing', () => {
    const open = { ...line, stations: (line.stations ?? []).filter((s) => s.action !== 'merge') };
    expect(noMergeHint(open, true)).toMatch(/pull requests stay open/);
    expect(noMergeHint(open, false)).toBeUndefined();
    expect(noMergeHint(line, true)).toBeUndefined();
  });

  it('offers only stations before a gate as where rework goes', () => {
    expect(upstreamOf(line.stations ?? [], 3).map((s) => s.id)).toEqual(['inbox', 'spec', 'build']);
  });
});
