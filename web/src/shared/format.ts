// Plain-language formatting for numbers, durations and names. Intl only; no locale data shipped.

const usd = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2 });
const usdWhole = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 });
const clock = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit' });
const dayMonth = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' });

/** A coarse age for cards: "now", "6 min", "2 h", "3 d". */
export function formatAge(seconds: number): string {
  if (seconds < 60) return 'now';
  const min = Math.floor(seconds / 60);
  if (min < 60) return `${min} min`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h} h`;
  return `${Math.floor(h / 24)} d`;
}

/** A precise duration for traces: "42 s", "38 min", "2 h 10 min", "3 d 4 h". */
export function formatDuration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return `${s} s`;
  const min = Math.floor(s / 60);
  if (min < 60) return `${min} min`;
  const h = Math.floor(min / 60);
  if (h < 24) return min % 60 ? `${h} h ${min % 60} min` : `${h} h`;
  const d = Math.floor(h / 24);
  return h % 24 ? `${d} d ${h % 24} h` : `${d} d`;
}

export function formatUsd(n: number): string {
  return usd.format(n);
}

/** Whole dollars without cents, as budgets are set: "$60", "$60.50". */
export function formatUsdCompact(n: number): string {
  const cents = Math.round(n * 100);
  return cents % 100 === 0 ? usdWhole.format(cents / 100) : usd.format(n);
}

export function formatHours(h: number): string {
  return `${h < 10 ? h.toFixed(1) : Math.round(h)} h`;
}

export function formatPercent(rate: number): string {
  return `${Math.round(rate * 100)}%`;
}

/** "14:05" today, "Oct 22, 14:05" before. */
export function formatClock(iso: string, now = new Date()): string {
  const d = new Date(iso);
  const sameDay = d.toDateString() === now.toDateString();
  return sameDay ? clock.format(d) : `${dayMonth.format(d)}, ${clock.format(d)}`;
}

/** "Oct 17" from an ISO date or day ("2026-10-17"). */
export function formatDay(day: string): string {
  return dayMonth.format(new Date(day.length === 10 ? `${day}T12:00:00Z` : day));
}

/** Station and agent ids as words: "approve-spec" → "Approve spec". */
export function humanize(id: string): string {
  const words = id.replace(/[-_]+/g, ' ').trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** "1 item", "3 items". */
export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`;
}
