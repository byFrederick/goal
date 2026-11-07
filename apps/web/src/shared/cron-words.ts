// A cron expression in words, for the common shapes only; anything else comes back as written.
const days = ['Sundays', 'Mondays', 'Tuesdays', 'Wednesdays', 'Thursdays', 'Fridays', 'Saturdays'];
const dayNames = ['SUN', 'MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT'];

const pad = (n: number) => String(n).padStart(2, '0');

/** A weekday field as an index 0-6 ("1", "MON", 7 for Sunday), or undefined. */
function weekday(field: string): number | undefined {
  const named = dayNames.indexOf(field.toUpperCase());
  if (named >= 0) return named;
  if (!/^[0-7]$/.test(field)) return undefined;
  return Number(field) % 7;
}

/** "0 7 * * 1-5" in "UTC" → "Weekdays at 07:00 UTC". Unknown shapes return the expression as written. */
export function describeSchedule(cron: string, timeZone = 'UTC'): string {
  const fields = cron.trim().split(/\s+/);
  if (fields.length !== 5) return cron;
  const [minute, hour, dom, month, dow] = fields as [string, string, string, string, string];
  if (dom !== '*' || month !== '*') return cron;

  if (/^\d+$/.test(minute) && Number(minute) < 60) {
    const everyN = /^\*\/(\d+)$/.exec(hour);
    if (minute === '0' && everyN && dow === '*') {
      const n = Number(everyN[1]);
      if (n === 1) return 'Every hour';
      if (n > 1 && n < 24) return `Every ${n} hours`;
      return cron;
    }
    if (minute === '0' && hour === '*' && dow === '*') return 'Every hour';
    if (/^\d+$/.test(hour) && Number(hour) < 24) {
      const time = `${pad(Number(hour))}:${pad(Number(minute))} ${timeZone}`;
      if (dow === '*') return `Every day at ${time}`;
      if (dow === '1-5' || dow.toUpperCase() === 'MON-FRI') return `Weekdays at ${time}`;
      const day = weekday(dow);
      if (day !== undefined) return `${days[day]} at ${time}`;
    }
  }
  return cron;
}

/** An intake trigger's `when`, "<cron>, <time zone>": in words, or as written when it isn't a known shape. */
export function describeWhen(when: string): string {
  const cut = when.lastIndexOf(', ');
  if (cut < 0) return when;
  const words = describeSchedule(when.slice(0, cut), when.slice(cut + 2));
  return words === when.slice(0, cut) ? when : words;
}
