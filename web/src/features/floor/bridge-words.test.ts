import { describe, expect, it } from 'vitest';
import type { BridgeStep, Factory } from '../../api/types';
import { bridgeInsight, shareWords, stepLabel } from './bridge-words';

const stations: Factory['stations'] = [
  { id: 'inbox', kind: 'queue' },
  { id: 'triage', kind: 'queue' },
  { id: 'investigate', kind: 'agent' },
  { id: 'review', kind: 'agent' },
  { id: 'approve', kind: 'human' },
  { id: 'done', kind: 'done' },
];

const step = (component: BridgeStep['component'], station: string | null, share = 0.5): BridgeStep => ({
  component,
  station,
  hours: 1,
  share,
  items: 2,
});

describe('stepLabel', () => {
  it('names each kind of time as the Floor says it', () => {
    expect(stepLabel(step('waiting', 'investigate'), stations)).toBe('Waiting for Investigate');
    expect(stepLabel(step('waiting', 'triage'), stations)).toBe('Triage queue');
    expect(stepLabel(step('rework', 'review'), stations)).toBe('Review rework');
    expect(stepLabel(step('waiting_on_you', null), stations)).toBe('Waiting on you');
    expect(stepLabel(step('blocked', 'investigate'), stations)).toBe('Blocked at Investigate');
    expect(stepLabel(step('working', 'investigate'), stations)).toBe('Working at Investigate');
    expect(stepLabel(step('other', null), stations)).toBe('Other');
  });
});

describe('shareWords', () => {
  it('says common shares as words, the rest as a percentage', () => {
    expect(shareWords(0.66)).toBe('two thirds');
    expect(shareWords(0.5)).toBe('half');
    expect(shareWords(0.97)).toBe('nearly all');
    expect(shareWords(0.26)).toBe('a quarter');
    expect(shareWords(0.41)).toBe('41%');
  });
});

describe('bridgeInsight', () => {
  it('names the largest step and, when it is there, the constraint at the same station', () => {
    expect(
      bridgeInsight(step('waiting', 'investigate', 0.66), { station: 'investigate', busy_ratio: 0.94 }, stations),
    ).toEqual({
      lead: 'Waiting in front of Investigate explains two thirds of the gap.',
      constraint: 'It is the constraint: busy 94% of the time.',
    });
  });

  it('names the constraint when the largest step is elsewhere, without a busy share before any history', () => {
    expect(
      bridgeInsight(step('waiting_on_you', null, 0.5), { station: 'investigate', busy_ratio: null }, stations),
    ).toEqual({
      lead: 'Waiting on you explains half of the gap.',
      constraint: 'Investigate is the constraint.',
    });
    expect(bridgeInsight(step('rework', 'review', 0.3), null, stations).constraint).toBeNull();
  });
});
