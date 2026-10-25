import { describe, expect, test } from 'vitest';
import { ApiError } from '../../api/client';
import type { KnowledgeDocument, KnowledgeSource } from '../../api/types';
import {
  documentStatus,
  everyLabel,
  excerpt,
  formatBytes,
  nameFor,
  refusalText,
  refusedFile,
  sourceStatus,
  syncTimes,
} from './words';

const now = Date.parse('2026-10-24T12:00:00Z');
const at = (ms: number) => new Date(now + ms).toISOString();
const min = 60_000;
const hour = 60 * min;

const source = (patch: Partial<KnowledgeSource>): KnowledgeSource => ({
  id: 's1',
  name: 'goal-docs',
  title: 'goal docs',
  kind: 'synced',
  connection: 'github',
  config: { repository: 'https://github.com/acme/goal' },
  sync_every_seconds: 3600,
  status: 'idle',
  last_synced_at: at(-hour),
  next_sync_at: at(15 * min),
  documents: 3,
  used_by: { factories: [] },
  created_by: { kind: 'user', id: 'local', name: 'Local user' },
  created_at: at(-30 * 24 * hour),
  updated_at: at(-30 * 24 * hour),
  ...patch,
});

const document = (patch: Partial<KnowledgeDocument>): KnowledgeDocument => ({
  id: 'd1',
  source: 'handbook',
  title: 'deploy.md',
  mime: 'text/markdown',
  bytes: 120,
  status: 'indexed',
  indexed_at: at(-hour),
  ...patch,
});

describe('sourceStatus', () => {
  test('says what a source is in words, by its kind and its last sync', () => {
    expect(sourceStatus(source({}))).toEqual({ tone: 'ok', text: 'Synced' });
    expect(sourceStatus(source({ kind: 'upload' }))).toEqual({ tone: 'ok', text: 'Indexed' });
    expect(sourceStatus(source({ status: 'syncing' }))).toEqual({ tone: 'muted', text: 'Syncing' });
    expect(sourceStatus(source({ kind: 'live', next_sync_at: null }))).toEqual({
      tone: 'muted',
      text: 'Live, not indexed',
    });
    expect(sourceStatus(source({ last_synced_at: null })).text).toBe('Waiting for its first sync');
    expect(sourceStatus(source({ kind: 'upload', last_synced_at: null, documents: 0 })).text).toBe('No documents yet');
  });

  test('a failed sync says why and that what it indexed before stays searchable', () => {
    const failed = sourceStatus(
      source({ status: 'failed', last_error: 'confluence answered 429', last_synced_at: '2026-10-22T14:00:00Z' }),
    );
    expect(failed).toEqual({
      tone: 'rework',
      text: 'Sync failed',
      detail: 'Confluence answered 429. The documents from Oct 22 stay searchable.',
    });
    expect(sourceStatus(source({ status: 'failed', last_synced_at: null })).detail).toBe('It stopped before the end.');
  });
});

describe('syncTimes', () => {
  test('a synced source: when it last synced and when it syncs next', () => {
    expect(syncTimes(source({}), now)).toEqual({ last: '1 h ago', next: 'Next in 15 min' });
    expect(syncTimes(source({ next_sync_at: at(-min) }), now).next).toBe('Due now');
    expect(syncTimes(source({ last_synced_at: '2026-10-19T09:00:00Z' }), now).last).toBe('Oct 19');
    expect(syncTimes(source({ next_sync_at: at(3 * 24 * hour) }), now).next).toBe('Next Oct 27');
    expect(syncTimes(source({ last_synced_at: null }), now).last).toBe('Not yet');
  });

  test('uploads sync when files are added; a live source is searched at question time', () => {
    expect(syncTimes(source({ kind: 'upload' }), now)).toEqual({ last: '1 h ago', next: 'When files are added' });
    expect(syncTimes(source({ kind: 'live' }), now)).toEqual({ last: 'Searched at question time' });
    expect(syncTimes(source({ status: 'syncing' }), now)).toEqual({ last: 'Running' });
  });
});

describe('documentStatus', () => {
  test('says each state, with the reason of a failed or skipped document', () => {
    expect(documentStatus(document({}), now)).toMatchObject({ tone: 'ok', text: expect.stringMatching(/^Indexed /) });
    expect(documentStatus(document({ status: 'pending', indexed_at: null }), now)).toEqual({
      tone: 'muted',
      text: 'Waiting to be indexed',
    });
    expect(documentStatus(document({ status: 'failed', error: 'Confluence answered 429' }), now)).toEqual({
      tone: 'rework',
      text: 'Failed: Confluence answered 429',
    });
    expect(documentStatus(document({ status: 'skipped' }), now).text).toBe('Skipped: it has no text');
  });
});

describe('uploads', () => {
  test('takes text, Markdown, HTML, JSON and YAML by extension, whatever its case', () => {
    for (const name of ['notes.txt', 'README.MD', 'page.html', 'a.json', 'b.yml', 'c.yaml', 'd.markdown']) {
      expect(refusedFile(name)).toBeUndefined();
    }
    expect(refusedFile('report.pdf')).toMatch(/PDF and Office files come later/);
    expect(refusedFile('Makefile')).toBeDefined();
  });

  test('sizes in binary units, as the 25 MiB limit is said', () => {
    expect(formatBytes(820)).toBe('820 B');
    expect(formatBytes(14_540)).toBe('14 KiB');
    expect(formatBytes(2_048)).toBe('2.0 KiB');
    expect(formatBytes(26_214_400)).toBe('25 MiB');
  });
});

test('nameFor makes a source name from its title, numbered when taken', () => {
  expect(nameFor('Infra docs', [])).toBe('infra-docs');
  expect(nameFor('  #incidents & #eng-help ', [])).toBe('incidents-eng-help');
  expect(nameFor('Runbooks', ['runbooks', 'runbooks-2'])).toBe('runbooks-3');
  expect(nameFor('2026 postmortems', [])).toBe('postmortems');
  expect(nameFor('Über docs', [])).toBe('uber-docs');
  expect(nameFor('!!', [])).toBe('');
  expect(nameFor('x'.repeat(80), []).length).toBeLessThanOrEqual(40);
});

test('everyLabel names the schedules people pick and any other in hours', () => {
  expect(everyLabel(3600)).toBe('1 hour');
  expect(everyLabel(900)).toBe('15 minutes');
  expect(everyLabel(7200)).toBe('2 h');
});

test("refusalText is goald's detail, or that goald is not answering", () => {
  const taken = new ApiError({
    title: 'Conflict',
    status: 409,
    code: 'knowledge-source-name-taken',
    detail: 'knowledge source "handbook" is taken',
  });
  expect(refusalText(taken)).toBe('knowledge source "handbook" is taken');
  expect(refusalText(new TypeError('Failed to fetch'))).toMatch(/not answering/);
});

describe('excerpt', () => {
  const marked = (segments: { text: string; mark: boolean }[]) =>
    segments.map((s) => (s.mark ? `[${s.text}]` : s.text)).join('');

  test("marks the query's words where they start a word, whatever their case", () => {
    expect(marked(excerpt('# Deploy\n\nDeploys go out on Tuesdays after the release review.', 'deploy tuesday'))).toBe(
      '[Deploy] [Deploy]s go out on [Tuesday]s after the release review.',
    );
    expect(marked(excerpt('Redeploy the task', 'deploy'))).toBe('Redeploy the task');
  });

  test('starts a little before the first match in a long chunk, cut at words, with ellipses', () => {
    const long = `${'lorem ipsum '.repeat(40)}the task stops with OutOfMemoryError when ${'dolor sit '.repeat(40)}`;
    const text = marked(excerpt(long, 'task memory', 120));
    expect(text.startsWith('…')).toBe(true);
    expect(text.endsWith('…')).toBe(true);
    expect(text).toContain('the [task] stops');
    expect(text.replace(/[[\]…]/g, '').length).toBeLessThanOrEqual(120);
  });

  test('a query without words marks nothing', () => {
    expect(excerpt('Some text', '!!')).toEqual([{ text: 'Some text', mark: false }]);
  });
});
