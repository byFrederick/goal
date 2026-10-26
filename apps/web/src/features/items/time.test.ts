import { describe, expect, it } from 'vitest';
import type { ItemSpan } from '../../api/types';
import { formatTarget, timeBar } from './time';

const t = (min: number) => new Date(Date.UTC(2026, 9, 23, 14, 5) + min * 60_000).toISOString();
const span = (station: string, state: ItemSpan['state'], from: number, to: number | null, round = 0): ItemSpan => ({
  station,
  state,
  rework_round: round,
  rework_by: round > 0 ? 'review' : null,
  entered_at: t(from),
  left_at: to === null ? null : t(to),
});

const minutes = (ms: number) => ms / 60_000;

describe('timeBar', () => {
  // The canvas: 1 min at triage, 37 at investigate (running now), an expedite target of 60 min.
  const spans = [
    span('inbox', 'queued', 0, 0),
    span('triage', 'queued', 0, 0.5),
    span('triage', 'in_progress', 0.5, 1),
    span('investigate', 'queued', 1, 1),
    span('investigate', 'in_progress', 1, null),
  ];

  it('segments by station and state, and totals each station in the order the item reached it', () => {
    const bar = timeBar(spans, { createdAt: t(0), end: t(38), targetSeconds: 3600 });

    expect(bar.segments.map((s) => [s.station, s.tone, minutes(s.ms), s.current])).toEqual([
      ['triage', 'target', 0.5, false],
      ['triage', 'run', 0.5, false],
      ['investigate', 'run', 37, true],
    ]);
    expect(bar.stations.map((s) => [s.station, minutes(s.ms)])).toEqual([
      ['triage', 1],
      ['investigate', 37],
    ]);
    expect(bar.tones).toEqual(['run', 'target']);
  });

  it('scales to the target while under it, with the time left', () => {
    const bar = timeBar(spans, { createdAt: t(0), end: t(38), targetSeconds: 3600 });

    expect(minutes(bar.elapsedMs)).toBe(38);
    expect(minutes(bar.scaleMs)).toBe(60);
    expect(bar.leftMs === null ? null : minutes(bar.leftMs)).toBe(22);
    expect(bar.overMs).toBeNull();
  });

  it('scales to the elapsed time once over the target, and says by how much', () => {
    const bar = timeBar(spans, { createdAt: t(0), end: t(75), targetSeconds: 3600 });

    expect(minutes(bar.scaleMs)).toBe(75);
    expect(bar.leftMs).toBeNull();
    expect(bar.overMs === null ? null : minutes(bar.overMs)).toBe(15);
  });

  it('measures closed spans to where they ended and has no target without one', () => {
    const done = [span('build', 'in_progress', 0, 10), span('merge', 'waiting_human', 10, 30, 1)];
    const bar = timeBar(done, { createdAt: t(0), end: t(30), targetSeconds: null });

    expect(bar.segments.map((s) => [s.tone, minutes(s.ms)])).toEqual([
      ['run', 10],
      ['you', 20],
    ]);
    expect(minutes(bar.scaleMs)).toBe(30);
    expect(bar.targetMs).toBeNull();
    expect(bar.leftMs).toBeNull();
    expect(bar.overMs).toBeNull();
  });
});

describe('formatTarget', () => {
  it.each([
    [3600, '60 minutes'],
    [5400, '90 minutes'],
    [21_600, '6 hours'],
    [5_400 * 4, '6 hours'],
    [9000, '2.5 hours'],
    [60, '1 minute'],
  ])('%d s reads %s', (seconds, text) => {
    expect(formatTarget(seconds)).toBe(text);
  });

  it.each([
    [3600, '60 min'],
    [21_600, '6 h'],
    [9000, '2.5 h'],
  ])('%d s reads %s when short', (seconds, text) => {
    expect(formatTarget(seconds, 'short')).toBe(text);
  });
});
