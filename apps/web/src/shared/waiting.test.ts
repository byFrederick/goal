import { describe, expect, it } from 'vitest';
import type { CICheck, ItemWaiting } from '../api/types';
import { waitingDetail, waitingShort } from './waiting';

type ItemWaitingNeed = ItemWaiting['needs'][number];

const need = (over: Partial<ItemWaitingNeed> = {}): ItemWaitingNeed => ({
  ref: 'software#93',
  title: 'Move the state bucket',
  paths: ['terraform/**'],
  ...over,
});

const waiting = (kind: ItemWaiting['kind'], needs: ItemWaitingNeed[], files: string[] = []): ItemWaiting => ({
  kind,
  items: needs.map((n) => n.ref),
  files,
  needs,
});

describe('waitingShort', () => {
  it('names the one item it waits for', () => {
    expect(waitingShort({ items: ['sre#3'] })).toBe('Waits for sre#3');
  });

  it('counts the others after the first', () => {
    expect(waitingShort({ items: ['sre#3', 'sre#4'] })).toBe('Waits for sre#3 +1');
    expect(waitingShort({ items: ['sre#3', 'sre#4', 'sre#5'] })).toBe('Waits for sre#3 +2');
  });

  it('never carries a path or a glob', () => {
    expect(waitingShort(waiting('files', [need()], ['terraform/main.tf']))).not.toMatch(/[/*]/);
  });
});

describe('waitingDetail', () => {
  it('gives each need its ref, its title and the paths it holds the wait on', () => {
    expect(waitingDetail(waiting('files', [need()], ['terraform/main.tf']))).toBe(
      'software#93 Move the state bucket (same files terraform/**)\nShared files: terraform/main.tf',
    );
  });

  it('says moves for an after wait, and gives the title alone when the need names no paths', () => {
    expect(waitingDetail(waiting('after', [need(), need({ ref: 'goal#16', paths: [] })]))).toBe(
      'software#93 Move the state bucket (moves terraform/**)\ngoal#16 Move the state bucket',
    );
  });

  it('gives a line per need, in the order given, with nothing cut short', () => {
    const long = 'internal/knowledge/sync/retry/very/long/path/that/keeps/going/**';
    const lines = waitingDetail(waiting('after', [need({ paths: [long] }), need({ ref: 'goal#16' })])).split('\n');
    expect(lines).toHaveLength(2);
    expect(lines[0]).toContain(long);
  });
});

describe('a turn wait', () => {
  const turn = (target?: string | null): ItemWaiting => ({
    ...waiting('turn', [need({ ref: 'software#763', paths: [] })]),
    ...(target === undefined ? {} : { target }),
  });

  it('says the item waits for the other to land', () => {
    expect(waitingShort(turn('goal/a7-x'))).toBe('Waits for software#763 to land');
  });

  it('names the integration branch in the detail', () => {
    expect(waitingDetail(turn('goal/a7-x'))).toBe('Waiting for software#763 to land on goal/a7-x');
  });

  it('names the default branch when the target is null or absent', () => {
    expect(waitingDetail(turn(null))).toBe('Waiting for software#763 to land on the default branch');
    expect(waitingDetail(turn())).toBe('Waiting for software#763 to land on the default branch');
  });
});

describe('a CI wait', () => {
  const check = (over: Partial<CICheck>): CICheck => ({
    repo: 'github.com/acme/app',
    name: 'build',
    state: 'running',
    failed_jobs: [],
    ...over,
  });
  const base = (ci: Partial<NonNullable<ItemWaiting['ci']>>): ItemWaiting => ({
    kind: 'ci',
    items: [],
    files: [],
    needs: [],
    ci: {
      commits: [{ repo: 'github.com/acme/app', sha: '0123456789abcdef' }],
      checks: [],
      finished: 0,
      total: 0,
      ...ci,
    },
  });

  it('counts finished checks while it runs, and says only Waiting for CI before the first poll', () => {
    expect(waitingShort(base({ finished: 9, total: 14 }))).toBe('Waiting for CI · 9 of 14');
    expect(waitingShort(base({}))).toBe('Waiting for CI');
  });

  it('names the first failed check and its first failed job after a failure', () => {
    const failure = {
      repo: 'github.com/acme/app',
      sha: 'abc',
      checks: [check({ name: 'task web:e2e', state: 'finished', result: 'failed', failed_jobs: ['3/12', '4/12'] })],
    };
    expect(waitingShort(base({ finished: 3, total: 4, failure }))).toBe('CI failed: task web:e2e (3/12)');
    failure.checks[0] = check({ name: 'lint', state: 'finished', result: 'failed' });
    expect(waitingShort(base({ failure }))).toBe('CI failed: lint');
    expect(waitingShort(base({ failure: { ...failure, checks: [] } }))).toBe('CI timed out');
  });
});
