// The organization's connections in words (spec 4 §10), for the Connections pages, the builder's
// Basics and Connections sections and the shell's "1 failing": a status never said by colour
// alone, when it was last tested and used, the variables a run gets (the kind's env names with
// their {{field}} filled in, never a value) and how runs use a kind.
import { useQuery } from '@tanstack/react-query';
import { connectionsQuery } from '../api/queries';
import type { Connection, ConnectionKind } from '../api/types';
import { formatAge } from './format';
import { useOrgHolds } from './org';

export type Tone = 'ok' | 'rework' | 'muted';

export interface StatusLine {
  tone: Tone;
  text: string;
}

/** The text colour of each tone (the words say it too). */
export const toneText: Record<Tone, string> = {
  ok: 'text-ok-ink',
  rework: 'text-rework-ink',
  muted: 'text-muted',
};

/**
 * "OK", "Test failed: 401", "Never tested" (or "No test" for a kind without one), "Missing
 * secret": what a connection's status means, in a few words.
 */
export function statusLine(c: Connection, kind?: ConnectionKind | undefined): StatusLine {
  switch (c.status) {
    case 'ok':
      return { tone: 'ok', text: 'OK' };
    case 'failing':
      return { tone: 'rework', text: c.last_test?.status ? `Test failed: ${c.last_test.status}` : 'Test failed' };
    case 'missing-secret':
      return { tone: 'rework', text: 'Missing secret' };
    default:
      return { tone: 'muted', text: kind && !kind.testable ? 'No test' : 'Never tested' };
  }
}

/** "Just now", "4 min ago", "2 h ago", "3 d ago". */
export function since(iso: string, now: number): string {
  const age = formatAge(Math.max(0, (now - Date.parse(iso)) / 1000));
  return age === 'now' ? 'Just now' : `${age} ago`;
}

/** "Tested 2 h ago", "Not tested". */
export function testedText(c: Connection, now: number): string {
  return c.last_test ? `Tested ${since(c.last_test.at, now).toLowerCase()}` : 'Not tested';
}

/** "Used 4 min ago", "Never used". */
export function usedText(c: Connection, now: number): string {
  return c.last_used_at ? `Used ${since(c.last_used_at, now).toLowerCase()}` : 'Never used';
}

/** "goal, sre and support"; "" for nothing. */
export function namesText(names: readonly string[]): string {
  if (names.length <= 1) return names[0] ?? '';
  return `${names.slice(0, -1).join(', ')} and ${names.at(-1)}`;
}

/** What uses it: "goal and sre", or "Nothing yet". */
export function usedByText(c: Connection): string {
  const names = [...c.used_by.factories.map((f) => f.name), ...c.used_by.knowledge_sources];
  return names.length > 0 ? namesText(names) : 'Nothing yet';
}

/**
 * The variables a run gets from a connection: its kind's env names with each {{field}} filled in
 * from the connection's fields or the field's default. Names whose field has no value are left
 * out, as goald leaves them out of a run.
 */
export function runEnv(kind: ConnectionKind | undefined, c: Pick<Connection, 'fields'>): string[] {
  if (!kind) return [];
  const value = (name: string) => c.fields[name] || kind.fields.find((f) => f.name === name)?.default || '';
  return kind.env
    .map((e) => e.replace(/\{\{([a-z0-9_]+)\}\}/g, (all, name: string) => value(name) || all))
    .filter((e) => !e.includes('{{'));
}

/** How runs use a kind, as its badges say: env, CLI, MCP, git. */
export function modeLabels(kind: ConnectionKind | undefined): string[] {
  const labels = { env: 'env', cli: 'CLI', mcp: 'MCP', git: 'git' } as const;
  return (kind?.modes ?? []).map((m) => labels[m]);
}

/**
 * Two letters for a kind's square, from its title: a word's inner capitals ("GitHub" gh,
 * "PagerDuty" pd), else the first two words' initials ("Claude subscription" cs) unless the second
 * is an acronym ("Anthropic API key" an), else its first two letters ("Jira" ji).
 */
export function monogram(title: string): string {
  const words = title.split(/[\s_-]+/).filter(Boolean);
  const first = words[0] ?? '';
  const caps = first.match(/[A-Z]/g) ?? [];
  if (caps.length >= 2 && caps.length < first.length) return caps.slice(0, 2).join('').toLowerCase();
  const second = words[1];
  if (second && second !== second.toUpperCase()) return `${first[0]}${second[0]}`.toLowerCase();
  return first.slice(0, 2).toLowerCase();
}

/** The name pattern goald enforces (ConnectionName): 3 to 40 lowercase letters, digits and inner dashes. */
export const connectionName = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;

/**
 * How many of the organization's connections failed their last test, for the shell's dot and its
 * "1 failing"; 0 for whoever may not read connections.
 */
export function useConnectionTrouble(org: string): number {
  const read = useOrgHolds('connection:read');
  const { data } = useQuery({ ...connectionsQuery(org), enabled: read });
  return data?.filter((c) => c.status === 'failing').length ?? 0;
}
