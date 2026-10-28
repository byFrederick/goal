// What the Credentials screen says and counts, as pure functions (words.test.ts): a type's name,
// a credential's expiry and its reminder, what uses it, and when it was last used. Times come in
// as `now` so a test sets the clock.
import type { Credential, CredentialType } from '../../api/types';
import { since } from '../../shared/connections';
import { formatDay, plural } from '../../shared/format';

const DAY = 86_400_000;

/** The type's name as a label: "API key", "OAuth token". */
export const typeLabels: Record<CredentialType, string> = {
  api_key: 'API key',
  oauth_token: 'OAuth token',
  ssh_key: 'SSH key',
  cloud_role: 'Cloud role',
  app_key: 'App key',
};

/** The filter tabs' names, plural, in the picture's order. */
export const typeTabLabels: Record<CredentialType, string> = {
  api_key: 'API keys',
  oauth_token: 'OAuth tokens',
  ssh_key: 'SSH keys',
  cloud_role: 'Cloud roles',
  app_key: 'App keys',
};

/** Every type, in the order the filter tabs show them. */
export const credentialTypes: readonly CredentialType[] = [
  'api_key',
  'oauth_token',
  'ssh_key',
  'cloud_role',
  'app_key',
];

/** How many credentials of each type there are. */
export function countByType(list: readonly Credential[]): Record<CredentialType, number> {
  const counts = Object.fromEntries(credentialTypes.map((t) => [t, 0])) as Record<CredentialType, number>;
  for (const c of list) counts[c.type] += 1;
  return counts;
}

/**
 * When a credential expires, and what the screen says about it (MASTER.md's danger colour):
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

export function expiryOf(c: Pick<Credential, 'expires_at' | 'remind_days'>, now: number): Expiry {
  if (!c.expires_at) return { state: 'never' };
  const expires = Date.parse(c.expires_at);
  const date = formatDay(c.expires_at);
  if (expires <= now) return { state: 'expired', date };
  if (expires - c.remind_days * DAY <= now) {
    return { state: 'reminder', date, days: Math.ceil((expires - now) / DAY) };
  }
  return { state: 'set', date };
}

/** How many credentials expire within the next seven days, the expired ones left out. */
export function expiringThisWeek(list: readonly Credential[], now: number): number {
  return list.filter((c) => {
    if (!c.expires_at) return false;
    const expires = Date.parse(c.expires_at);
    return expires > now && expires <= now + 7 * DAY;
  }).length;
}

/** "1 expires this week", "2 expire this week". */
export function expiringText(n: number): string {
  return n === 1 ? '1 expires this week' : `${n} expire this week`;
}

/** "2 min ago" within a day, then the day: "Oct 20"; "Never used" for none. */
export function lastUsedText(c: Pick<Credential, 'last_used_at'>, now: number): string {
  if (!c.last_used_at) return 'Never used';
  return now - Date.parse(c.last_used_at) < DAY ? since(c.last_used_at, now) : formatDay(c.last_used_at);
}

/** What uses the credential: each connection as "Connection github", then the agents by name. */
export function usedByLabels(c: Pick<Credential, 'used_by'>): string[] {
  const connections = c.used_by.connections.map((name) => `Connection ${name}`);
  const agents = [...new Set(c.used_by.agents.map((a) => a.agent))];
  return [...connections, ...agents];
}

/** A credential's name as the contract's CredentialName pattern reads it (contracts/api/openapi.yaml). */
export const credentialNamePattern = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;

/** The name's problem in words, or undefined when it is a credential name (New credential's Name field). */
export function nameProblem(name: string): string | undefined {
  if (name === '') return 'Give the credential a name.';
  if (!credentialNamePattern.test(name)) {
    return 'Use 3 to 40 lowercase letters, digits and dashes. Start with a letter.';
  }
  return undefined;
}

/**
 * The existing credential a new name is similar to: the same name before a trailing -suffix, either
 * way round ("cloudflare-token" and "cloudflare-token-2027"). That is when the footer says a new
 * credential replaces nothing. An identical name is not similar: the API refuses it as taken.
 */
export function similarName(name: string, existing: readonly string[]): string | undefined {
  return existing.find((other) => other !== name && (name.startsWith(`${other}-`) || other.startsWith(`${name}-`)));
}

/** The rotation reminder's options: days before the expiry, or 0 for none (the contract's remind_days). */
export const remindOptions: readonly { days: number; label: string }[] = [
  { days: 14, label: '14 days before it expires' },
  { days: 7, label: '7 days before it expires' },
  { days: 30, label: '30 days before it expires' },
  { days: 0, label: 'Never' },
];

/** The .env or YAML format of a pasted text: YAML when it has the top-level credentials: map, else .env. */
export function importFormat(text: string): 'env' | 'yaml' {
  return /^credentials:\s*$/m.test(text) ? 'yaml' : 'env';
}

/** How many names an import writes: the new ones, and the existing ones too when they are replaced. */
export function importCount(found: readonly { exists: boolean }[], onConflict: 'replace' | 'skip'): number {
  return found.filter((c) => !c.exists || onConflict === 'replace').length;
}

/** "Used by 2 connections", "Used by 1 connection". */
export function usedByConnections(n: number): string {
  return `Used by ${plural(n, 'connection')}`;
}
