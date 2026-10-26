import { describe, expect, it } from 'vitest';
import type { ItemWaiting } from '../api/types';
import { pathSummary, waitingLines } from './waiting';

type ItemWaitingNeed = ItemWaiting['needs'][number];

const need = (over: Partial<ItemWaitingNeed> = {}): ItemWaitingNeed => ({
  ref: 'software#93',
  title: 'Move the state bucket',
  paths: ['terraform/**'],
  ...over,
});

const waiting = (kind: ItemWaiting['kind'], needs: ItemWaitingNeed[]): ItemWaiting => ({
  kind,
  items: needs.map((n) => n.ref),
  files: [],
  needs,
});

describe('pathSummary', () => {
  it('is the directory the globs share, cut before the first wildcard', () => {
    expect(pathSummary('Title', ['terraform/**', 'terraform/main.tf'])).toBe('terraform/');
  });

  it('does not keep a wildcard in the prefix: terraform/** and terraform/*.tf share terraform/', () => {
    expect(pathSummary('Title', ['terraform/**', 'terraform/*.tf'])).toBe('terraform/');
  });

  it('shares only whole directories: web/src/ab.ts and web/src/ac.ts give web/src/, web/src/a and web/lib/x give web/', () => {
    expect(pathSummary('Title', ['web/src/ab.ts', 'web/src/ac.ts'])).toBe('web/src/');
    expect(pathSummary('Title', ['web/src/a.ts', 'web/lib/x.ts'])).toBe('web/');
  });

  it('gives the longest shared directory of several globs', () => {
    expect(pathSummary('Title', ['internal/knowledge/**', 'internal/metrics/**'])).toBe('internal/');
  });

  it('lists the globs when they share no directory: at most two in full', () => {
    expect(pathSummary('Title', ['*.tf'])).toBe('*.tf');
    expect(pathSummary('Title', ['a.tf', 'b.tf'])).toBe('a.tf, b.tf');
  });

  it('lists the first two globs and the count of the rest when there are more than two', () => {
    expect(pathSummary('Title', ['a/x.go', 'b/y.go', 'c/z.go', 'd/w.go'])).toBe('a/x.go, b/y.go, +2 more');
  });

  it('is the title when the need names no paths', () => {
    expect(pathSummary('Move the state bucket', [])).toBe('Move the state bucket');
  });
});

describe('waitingLines', () => {
  it('reads one "moves" line per need for kind after', () => {
    expect(waitingLines(waiting('after', [need()]))).toEqual([
      { ref: 'software#93', text: 'waits for software#93 (moves terraform/)' },
    ]);
  });

  it('reads one "same files" line per need for kind files', () => {
    expect(waitingLines(waiting('files', [need()]))).toEqual([
      { ref: 'software#93', text: 'waits for the same files as software#93 (terraform/)' },
    ]);
  });

  it('falls back to the needed item title when it has no paths, for both kinds', () => {
    const untitled = need({ paths: [] });
    expect(waitingLines(waiting('after', [untitled]))[0]?.text).toBe(
      'waits for software#93 (moves Move the state bucket)',
    );
    expect(waitingLines(waiting('files', [untitled]))[0]?.text).toBe(
      'waits for the same files as software#93 (Move the state bucket)',
    );
  });

  it('gives a line per open need, in the order given', () => {
    const lines = waitingLines(
      waiting('after', [need(), need({ ref: 'goal#16', title: 'Exit on empty bucket', paths: [] })]),
    );
    expect(lines.map((l) => l.ref)).toEqual(['software#93', 'goal#16']);
  });

  it('is empty with no waiting item', () => {
    expect(waitingLines(null)).toEqual([]);
    expect(waitingLines(undefined)).toEqual([]);
  });
});
