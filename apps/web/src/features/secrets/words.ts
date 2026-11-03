// What the Secrets screens say and count, as pure functions (words.test.ts): a secret's expiry and
// its reminder, where it is used, when it was last used, and what the audit log says about it. Times
// come in as `now` so a test sets the clock.
import type { AuditEntry, Secret, SecretUse } from '../../api/types';
import { since } from '../../shared/connections';
import { formatDay, plural } from '../../shared/format';

const DAY = 86_400_000;

/** A secret's name as the contract's SecretName pattern reads it (contracts/api/openapi.yaml). */
export const secretNamePattern = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;

/** The name's problem in words, or undefined when it is a secret name (New secret's Name field). */
export function nameProblem(name: string): string | undefined {
  if (name === '') return 'Give the secret a name.';
  if (!secretNamePattern.test(name)) return 'Use 3 to 40 lowercase letters, digits and dashes. Start with a letter.';
  return undefined;
}

/**
 * When a secret expires, and what the screen says about it (MASTER.md's danger colour):
 * - never: no expiry;
 * - set: an expiry outside the reminder window;
 * - reminder: inside it (expires_at minus remind_days has come), with the days left;
 * - expired: past its expiry.
 */
export type Expiry =
  | { state: 'never' }
  | { state: 'set'; date: string }
  | { state: 'reminder'; date: string; days: number }
  | { state: 'expired'; date: string };

/** "Oct 30, 2027": the day with its year, for an expiry that can be far off. */
export function formatFullDay(iso: string): string {
  return new Date(iso).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    timeZone: 'UTC',
  });
}

export function expiryOf(c: Pick<Secret, 'expires_at' | 'remind_days'>, now: number): Expiry {
  if (!c.expires_at) return { state: 'never' };
  const expires = Date.parse(c.expires_at);
  const date = formatFullDay(c.expires_at);
  if (expires <= now) return { state: 'expired', date };
  if (expires - c.remind_days * DAY <= now) {
    return { state: 'reminder', date, days: Math.ceil((expires - now) / DAY) };
  }
  return { state: 'set', date };
}

/** The secrets inside their reminder window or past expiry: the ones the list's banner and its Expiring filter name. */
export function expiringSoon(list: readonly Secret[], now: number): Secret[] {
  return list.filter((s) => {
    const e = expiryOf(s, now);
    return e.state === 'reminder' || e.state === 'expired';
  });
}

/** "cloudflare-deploy expires in 3 days", "cloudflare-deploy expired Oct 30, 2026". */
export function expiringHeadline(s: Secret, now: number): string {
  const e = expiryOf(s, now);
  if (e.state === 'expired') return `${s.name} expired ${e.date}`;
  if (e.state === 'reminder') return `${s.name} expires in ${plural(e.days, 'day')}`;
  return s.name;
}

/** "2 min ago" within a day, then the day: "Oct 20"; "Never used" for none. */
export function lastUsedText(c: Pick<Secret, 'last_used_at'>, now: number): string {
  if (!c.last_used_at) return 'Never used';
  return now - Date.parse(c.last_used_at) < DAY ? since(c.last_used_at, now) : formatDay(c.last_used_at);
}

/** What kind of place uses a secret, as a short label. */
export const useKindLabels: Record<SecretUse['kind'], string> = {
  definition: 'Factory definition',
  knowledge_source: 'Knowledge source',
  webhook: 'Webhook',
  slack: 'Slack',
  connection: 'Connection',
};

/** "software · definition": the name of one use with its kind, for the list's Used in column. */
export function usedAtLabel(u: SecretUse): string {
  return u.kind === 'slack' ? u.name : `${u.name} · ${useKindLabels[u.kind].toLowerCase()}`;
}

/** "in 5 places", "in 1 place", "nowhere": the phone list's count. */
export function placesText(n: number): string {
  return n === 0 ? 'nowhere' : `in ${plural(n, 'place')}`;
}

/** "5 places", "1 place": the Where it's used header's count. */
export function placesCount(n: number): string {
  return plural(n, 'place');
}

/** The Delete card's reason while something uses the secret. */
export function deleteReason(n: number): string {
  return `Remove it from the ${n} places above first. Until then it can’t be deleted.`;
}

/** The reminder's options: days before the expiry, or 0 for none (the contract's remind_days). */
export const remindOptions: readonly { days: number; label: string }[] = [
  { days: 14, label: '14 days before' },
  { days: 7, label: '7 days before' },
  { days: 30, label: '30 days before' },
  { days: 0, label: 'Never' },
];

/** The reminder in words: "14 days before, to owners and admins", "None". */
export function reminderText(s: Pick<Secret, 'expires_at' | 'remind_days'>): string {
  if (!s.expires_at || s.remind_days === 0) return 'None';
  return `${plural(s.remind_days, 'day')} before, to owners and admins`;
}

/** The .env or YAML format of a pasted text: YAML when it has the top-level secrets: map, else .env. */
export function importFormat(text: string): 'env' | 'yaml' {
  return /^secrets:\s*$/m.test(text) ? 'yaml' : 'env';
}

/** How many names an import writes: the new ones, and the existing ones too when they are replaced. */
export function importCount(found: readonly { exists: boolean }[], onConflict: 'replace' | 'skip'): number {
  return found.filter((c) => !c.exists || onConflict === 'replace').length;
}

/**
 * The names a pasted text writes, as the file spells them: SENTRY_TOKEN for the secret sentry-token. A .env's keys
 * become lowercase with dashes (the contract's rule); a YAML secrets: map's keys already are secret names.
 */
export function fileNames(text: string): Map<string, string> {
  const names = new Map<string, string>();
  const lines = text.split('\n');
  const top = lines.findIndex((l) => /^secrets:\s*$/.test(l));
  const keys =
    top >= 0
      ? lines.slice(top + 1).flatMap((l) => /^\s+([^\s:#]+):/.exec(l)?.[1] ?? [])
      : lines.flatMap((l) => /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=/.exec(l)?.[1] ?? []);
  for (const key of keys) names.set(key.toLowerCase().replace(/_/g, '-'), key);
  return names;
}

/** "Used in 1 place", "Used in 2 places". */
export function usedInText(n: number): string {
  return `Used in ${plural(n, 'place')}`;
}

const data = (e: AuditEntry, key: string): unknown => (e.data as Record<string, unknown>)[key];

/** What the audit log says happened to a secret: "Replaced by Freddy Cruz", "Used by run #147, frontend-engineer". */
export function activityText(e: AuditEntry): string {
  switch (e.action) {
    case 'secret.created':
      return `Created by ${e.actor.name}`;
    case 'secret.updated':
      return `Changed by ${e.actor.name}`;
    case 'secret.value_replaced':
      return `Replaced by ${e.actor.name}`;
    case 'secret.deleted':
      return `Deleted by ${e.actor.name}`;
    case 'secret.used': {
      const run = data(e, 'run');
      const role = data(e, 'role');
      if (typeof run === 'number' || typeof run === 'string') {
        return `Used by run #${run}${typeof role === 'string' && role ? `, ${role}` : ''}`;
      }
      return `Used by ${e.actor.name}`;
    }
    default:
      return e.action;
  }
}
