import { describe, expect, it } from 'vitest';
import { describeSchedule, describeWhen } from './cron-words';

describe('describeSchedule', () => {
  it.each([
    ['0 8 * * *', 'UTC', 'Every day at 08:00 UTC'],
    ['30 7 * * 1-5', 'Europe/Paris', 'Weekdays at 07:30 Europe/Paris'],
    ['0 7 * * MON-FRI', 'UTC', 'Weekdays at 07:00 UTC'],
    ['0 9 * * 1', 'UTC', 'Mondays at 09:00 UTC'],
    ['0 9 * * MON', 'UTC', 'Mondays at 09:00 UTC'],
    ['0 9 * * 7', 'UTC', 'Sundays at 09:00 UTC'],
    ['0 */6 * * *', 'UTC', 'Every 6 hours'],
    ['0 * * * *', 'UTC', 'Every hour'],
    ['0 */1 * * *', 'UTC', 'Every hour'],
  ])('says %s in %s as "%s"', (cron, tz, words) => {
    expect(describeSchedule(cron, tz)).toBe(words);
  });

  it.each(['*/15 * * * *', '0 9 1 * *', '0 9 * 6 *', '0 9 * * 1,3', '0 25 * * *', '0 9 * *'])(
    'returns %s as written',
    (cron) => {
      expect(describeSchedule(cron, 'UTC')).toBe(cron);
    },
  );
});

describe('describeWhen', () => {
  it('describes a schedule', () => expect(describeWhen('0 7 * * 1-5, UTC')).toBe('Weekdays at 07:00 UTC'));
  it('leaves other text as written', () => {
    expect(describeWhen('github.issues.labeled')).toBe('github.issues.labeled');
    expect(describeWhen('*/15 * * * *, UTC')).toBe('*/15 * * * *, UTC');
  });
});
