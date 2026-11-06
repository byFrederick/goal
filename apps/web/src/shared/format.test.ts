import { describe, expect, it } from 'vitest';
import {
  formatBytes,
  formatCpuSeconds,
  formatDuration,
  formatDurationCompact,
  formatHours,
  formatUsdCost,
  ordinal,
} from './format';

describe('formatHours', () => {
  it('says a duration in hours adaptively, as the Item page does', () => {
    expect(formatHours(114 / 3600)).toBe('1 min'); // a p85 of 114 s, not "0.0 h"
    expect(formatHours(30 / 3600)).toBe('30 s');
    expect(formatHours(1.5)).toBe('1 h 30 min');
    expect(formatHours(6)).toBe('6 h');
    expect(formatHours(50)).toBe('2 d 2 h');
  });
});

describe('ordinal', () => {
  it('says a number as its ordinal, teens included', () => {
    expect(ordinal(1)).toBe('1st');
    expect(ordinal(2)).toBe('2nd');
    expect(ordinal(3)).toBe('3rd');
    expect(ordinal(4)).toBe('4th');
    expect(ordinal(7)).toBe('7th');
    expect(ordinal(11)).toBe('11th');
    expect(ordinal(12)).toBe('12th');
    expect(ordinal(13)).toBe('13th');
    expect(ordinal(21)).toBe('21st');
    expect(ordinal(112)).toBe('112th');
  });
});

describe('formatDurationCompact', () => {
  it('says a duration as the Teardown canvas draws it', () => {
    expect(formatDurationCompact(0)).toBe('0 s');
    expect(formatDurationCompact(45_000)).toBe('45 s');
    expect(formatDurationCompact(38 * 60_000)).toBe('38 m');
    expect(formatDurationCompact((3 * 60 + 10) * 60_000)).toBe('3 h 10 m');
    expect(formatDurationCompact(3 * 3_600_000)).toBe('3 h');
    expect(formatDurationCompact(52 * 3_600_000)).toBe('2 d 4 h');
    expect(formatDurationCompact(48 * 3_600_000)).toBe('2 d');
  });

  it('leaves formatDuration as it was', () => {
    expect(formatDuration(38 * 60_000)).toBe('38 min');
  });
});

describe('formatUsdCost', () => {
  it('shows a cost under a cent as <$0.01 and zero as $0.00', () => {
    expect(formatUsdCost(0)).toBe('$0.00');
    expect(formatUsdCost(0.005)).toBe('<$0.01');
    expect(formatUsdCost(0.0001)).toBe('<$0.01');
    expect(formatUsdCost(0.01)).toBe('$0.01');
    expect(formatUsdCost(2.4)).toBe('$2.40');
  });
});

describe('formatCpuSeconds', () => {
  it('says seconds under a minute, minutes from a minute up, and — for null', () => {
    expect(formatCpuSeconds(42)).toBe('42 s');
    expect(formatCpuSeconds(186)).toBe('3.1 min');
    expect(formatCpuSeconds(60)).toBe('1 min');
    expect(formatCpuSeconds(null)).toBe('—');
  });
});

describe('formatBytes', () => {
  it('scales by 1024 and shows — for null', () => {
    expect(formatBytes(512 * 1024 ** 2)).toBe('512 MB');
    expect(formatBytes(Math.round(1.2 * 1024 ** 3))).toBe('1.2 GB');
    expect(formatBytes(1024 ** 3)).toBe('1 GB');
    expect(formatBytes(null)).toBe('—');
  });
});
