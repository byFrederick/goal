import { describe, expect, test } from 'vitest';
import { ApiError } from '../../api/client';
import type { KnowledgeSource } from '../../api/types';
import {
  everyLabel,
  formatBytes,
  nameFor,
  refusalText,
  refusedFile,
  sourceStatus,
  syncChoices,
  syncTimes,
} from './words';

const now = Date.parse('2026-10-24T12:00:00Z');
const at = (ms: number) => new Date(now + ms).toISOString();
const min = 60_000;
const hour = 60 * min;

const source = (patch: Partial<KnowledgeSource>): KnowledgeSource => ({
  id: 's1',
  read_when: '',
  name: 'goal-docs',
  title: 'goal docs',
  kind: 'synced',
  config: { repository: 'https://github.com/acme/goal' },
  sync_every_seconds: 3600,
  status: 'idle',
  last_synced_at: at(-hour),
  next_sync_at: at(15 * min),
  latest_snapshot: null,
  used_by: { factories: [], readers: [] },
  created_by: { kind: 'user', id: 'local', name: 'Local user' },
  created_at: at(-30 * 24 * hour),
  updated_at: at(-30 * 24 * hour),
  ...patch,
});

describe('sourceStatus', () => {
  test('says what a source is in words, by its kind and its last sync', () => {
    expect(sourceStatus(source({}))).toEqual({ tone: 'ok', text: 'Synced' });
    expect(sourceStatus(source({ kind: 'upload' }))).toEqual({ tone: 'ok', text: 'Synced' });
    expect(sourceStatus(source({ status: 'syncing' }))).toEqual({ tone: 'muted', text: 'Syncing' });
    expect(sourceStatus(source({ last_synced_at: null })).text).toBe('Waiting for its first sync');
    expect(sourceStatus(source({ kind: 'upload', last_synced_at: null })).text).toBe('No files yet');
  });

  test('a failed sync says why and that what it indexed before stays searchable', () => {
    const failed = sourceStatus(
      source({ status: 'failed', last_error: 'confluence answered 429', last_synced_at: '2026-10-22T14:00:00Z' }),
    );
    expect(failed).toEqual({
      tone: 'rework',
      text: 'Sync failed',
      detail: 'Confluence answered 429. The files from Oct 22 stay as they were.',
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

  test('uploads sync when files are added', () => {
    expect(syncTimes(source({ kind: 'upload' }), now)).toEqual({ last: '1 h ago', next: 'When files are added' });
    expect(syncTimes(source({ status: 'syncing' }), now)).toEqual({ last: 'Running' });
  });
});

describe('on-demand sync', () => {
  test('0 seconds is the first schedule, worded, and shows no next sync', () => {
    expect(syncChoices[0]).toEqual({ seconds: 0, label: 'Only when someone presses Sync' });
    expect(everyLabel(0)).toBe('Only when someone presses Sync');
    expect(syncTimes(source({ sync_every_seconds: 0, next_sync_at: null }), now)).toEqual({ last: '1 h ago' });
    expect(syncTimes(source({ sync_every_seconds: 0, next_sync_at: at(hour) }), now).next).toBeUndefined();
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
