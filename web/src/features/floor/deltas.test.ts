import { describe, expect, it } from 'vitest';
import { leadTimeDelta, longWaitSentence, reworkDelta, targetSentence, throughputDelta } from './deltas';

describe('throughputDelta', () => {
  it('is signed to one decimal, worse when fewer items finish', () => {
    expect(throughputDelta(6.4, 7.2)).toEqual({ text: '−0.8', direction: 'down', tone: 'worse' });
    expect(throughputDelta(1.24, 0.9)).toEqual({ text: '+0.3', direction: 'up', tone: 'better' });
  });

  it('says same when the change rounds to nothing', () => {
    expect(throughputDelta(1.02, 1)).toEqual({ text: 'Same', direction: 'flat', tone: 'flat' });
  });
});

describe('leadTimeDelta', () => {
  it('is worse when the p85 grows, said as the Item page says durations', () => {
    expect(leadTimeDelta(9.2, 7.8)).toEqual({ text: '+1 h 24 min', direction: 'up', tone: 'worse' });
    expect(leadTimeDelta(5, 6.3)).toEqual({ text: '−1 h 18 min', direction: 'down', tone: 'better' });
    // Minutes-long lead times move by minutes and seconds, not by "0.0 h".
    expect(leadTimeDelta(114 / 3600, 54 / 3600)).toEqual({ text: '+1 min', direction: 'up', tone: 'worse' });
    expect(leadTimeDelta(40 / 3600, 10 / 3600)).toEqual({ text: '+30 s', direction: 'up', tone: 'worse' });
  });

  it('has no delta without both ranges', () => {
    expect(leadTimeDelta(9.2, null)).toBeNull();
    expect(leadTimeDelta(null, 3)).toBeNull();
  });
});

describe('reworkDelta', () => {
  it('is in percentage points, better when fewer are sent back', () => {
    expect(reworkDelta(0.18, 0.22)).toEqual({ text: '−4 pts', direction: 'down', tone: 'better' });
    expect(reworkDelta(0.25, 0.24)).toEqual({ text: '+1 pt', direction: 'up', tone: 'worse' });
    expect(reworkDelta(0.2, 0.2)).toEqual({ text: 'Same', direction: 'flat', tone: 'flat' });
  });
});

describe('targetSentence', () => {
  it('says how far under or over the target throughput is', () => {
    expect(targetSentence(6.4, 8)).toBe('1.6 under the target of 8 a day.');
    expect(targetSentence(3.5, 3)).toBe('0.5 over the target of 3 a day.');
    expect(targetSentence(2.96, 3)).toBe('On the target of 3 a day.');
    expect(targetSentence(2, 2.5)).toBe('0.5 under the target of 2.5 a day.');
  });
});

describe('longWaitSentence', () => {
  it('names the items that waited over a day at the station', () => {
    expect(longWaitSentence({ station: 'investigate', items: 11, threshold_hours: 24 })).toBe(
      '11 items waited more than a day in front of Investigate.',
    );
    expect(longWaitSentence({ station: 'build', items: 1, threshold_hours: 24 })).toBe(
      '1 item waited more than a day in front of Build.',
    );
  });

  it('says nothing when none did or there is no constraint', () => {
    expect(longWaitSentence({ station: 'build', items: 0, threshold_hours: 24 })).toBeNull();
    expect(longWaitSentence(null)).toBeNull();
  });
});
