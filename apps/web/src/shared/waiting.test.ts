import { describe, expect, it } from 'vitest';
import type { ItemWaiting } from '../api/types';
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
