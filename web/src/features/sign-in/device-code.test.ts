import { describe, expect, it } from 'vitest';
import { formatUserCode, isWholeCode, requestTimes } from './device-code';

describe('formatUserCode', () => {
  it('shows what was typed as ABCD-EFGH: letters and digits, upper case, eight at most', () => {
    expect(formatUserCode('abcd')).toBe('ABCD');
    expect(formatUserCode('abcde')).toBe('ABCD-E');
    expect(formatUserCode('abcd efgh')).toBe('ABCD-EFGH');
    expect(formatUserCode(' ABCD-EFGH ')).toBe('ABCD-EFGH');
    expect(formatUserCode('ABCD-EFGH-JK')).toBe('ABCD-EFGH');
    expect(formatUserCode('?user_code=AB')).toBe('USER-CODE');
  });

  it('knows a whole code', () => {
    expect(isWholeCode('ABCD-EFGH')).toBe(true);
    expect(isWholeCode('ABCD-EFG')).toBe(false);
  });
});

describe('requestTimes', () => {
  const now = Date.parse('2026-10-24T12:00:00Z');

  it('says when the CLI asked and until when the code works', () => {
    expect(requestTimes('2026-10-24T11:59:00Z', '2026-10-24T12:09:00Z', now)).toBe(
      '1 minute ago, expires in 9 minutes',
    );
    expect(requestTimes('2026-10-24T11:59:40Z', '2026-10-24T12:00:30Z', now)).toBe(
      'just now, expires in less than a minute',
    );
    expect(requestTimes(undefined, '2026-10-24T12:01:00Z', now)).toBe('Expires in 1 minute');
  });
});
