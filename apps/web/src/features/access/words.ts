// What the Access page says and counts, as pure functions (words.test.ts). Times come in as `now` so a
// test sets the clock.
import type { Access, AccessRow, AccessUse } from '../../api/types';
import { since } from '../../shared/connections';
import { formatDay, plural } from '../../shared/format';

const DAY = 86_400_000;

/** "Variable", "File", "MCP server": what a row is, in words. */
export const rowKindLabels: Record<AccessRow['kind'], string> = {
  mcp: 'MCP server',
  env: 'Variable',
  file: 'File',
  slack: 'Slack notifications',
};

/** "linear" is one row however many groups list it: rows counted by kind and name. */
export function uniqueRows(access: Access): AccessRow[] {
  const seen = new Map<string, AccessRow>();
  for (const g of access.groups) {
    for (const r of g.rows) if (!seen.has(`${r.kind}:${r.name}`)) seen.set(`${r.kind}:${r.name}`, r);
  }
  return [...seen.values()];
}

/** The environment as a column or chip reads it: "software · deploy". */
export const placeLabel = (u: AccessUse): string => `${u.factory.name} · ${u.environment}`;

/** Every environment found in `uses`, in the order first met, once each. */
export function places(rows: readonly AccessRow[]): AccessUse[] {
  const seen = new Map<string, AccessUse>();
  for (const r of rows) for (const u of r.uses) if (!seen.has(placeLabel(u))) seen.set(placeLabel(u), u);
  return [...seen.values()];
}

/** "4 MCP servers and 5 keys, in 4 environments". */
export function reachedDetail(access: Access): string {
  const rows = uniqueRows(access);
  const mcp = rows.filter((r) => r.kind === 'mcp').length;
  return `${plural(mcp, 'MCP server')} and ${plural(rows.length - mcp, 'key')}, in ${plural(places(rows).length, 'environment')}`;
}

/** "A key expires, a test fails": what needs attention; "Everything is in order" for nothing. */
export function attentionDetail(access: Access): string {
  const statuses = new Set(uniqueRows(access).map((r) => r.status));
  const parts = [
    statuses.has('missing') ? 'a secret is missing' : '',
    statuses.has('expired') || statuses.has('expiring') ? 'a key expires' : '',
    statuses.has('failed') ? 'a test fails' : '',
  ].filter(Boolean);
  if (parts.length === 0) return 'Everything is in order';
  const text = parts.join(', ');
  return `${text.charAt(0).toUpperCase()}${text.slice(1)}`;
}

/** "2 min ago" within a day, then the day: "Oct 30". */
function whenText(iso: string, now: number): string {
  return now - Date.parse(iso) < DAY ? since(iso, now).toLowerCase() : formatDay(iso);
}

/** Whole days until `iso`: 0 for today, negative once past. */
export const daysUntil = (iso: string, now: number): number => Math.ceil((Date.parse(iso) - now) / DAY);

export type StatusTone = 'ok' | 'bad' | 'muted';

/** The row's status in words and the tone that colours its dot: "Tested 2 min ago", "Expires in 3 days", "Test failed: 401". */
export function statusOf(row: AccessRow, now: number): { text: string; tone: StatusTone } {
  switch (row.status) {
    case 'missing':
      return { text: 'Secret missing', tone: 'bad' };
    case 'failed':
      return { text: row.test_error ? `Test failed: ${row.test_error}` : 'Test failed', tone: 'bad' };
    case 'expired':
    case 'expiring': {
      if (!row.expires_at) return { text: row.status === 'expired' ? 'Expired' : 'Expires soon', tone: 'bad' };
      const days = daysUntil(row.expires_at, now);
      if (days < 0) return { text: `Expired ${plural(-days, 'day')} ago`, tone: 'bad' };
      return { text: days === 0 ? 'Expires today' : `Expires in ${plural(days, 'day')}`, tone: 'bad' };
    }
    case 'untested':
      return { text: 'Not tested', tone: 'muted' };
    case 'tested':
      return row.tested_at
        ? { text: `Tested ${whenText(row.tested_at, now)}`, tone: 'ok' }
        : { text: 'Not tested', tone: 'muted' };
    case 'ok':
      return row.last_used_at
        ? { text: `Used ${whenText(row.last_used_at, now)}`, tone: 'ok' }
        : { text: 'Not used yet', tone: 'muted' };
  }
}

/** The row that expires or expired first, with the use that says where; undefined when none does. */
export function soonestExpiring(access: Access): { row: AccessRow; use: AccessUse } | undefined {
  const due = uniqueRows(access)
    .filter((r) => (r.status === 'expired' || r.status === 'expiring') && r.expires_at)
    .sort((a, b) => Date.parse(a.expires_at ?? '') - Date.parse(b.expires_at ?? ''));
  const row = due[0];
  if (!row) return undefined;
  const use = row.uses.find((u) => u.status === 'expired' || u.status === 'expiring') ?? row.uses[0];
  return use ? { row, use } : undefined;
}

/** "reads it as GH_TOKEN", "mounts it as ~/.kube/config", "sends it to linear". */
export function readsAs(row: AccessRow): string {
  switch (row.kind) {
    case 'env':
      return `reads it as ${row.name}`;
    case 'file':
      return `mounts it as ${row.name}`;
    case 'mcp':
      return `sends it to ${row.name}`;
    case 'slack':
      return 'sends Slack notifications with it';
  }
}

/** Where a row opens in the definition: the environment's file in the YAML view (the Environments section replaces it). */
export const environmentFile = (u: AccessUse): string => `environments/${u.environment}.yaml`;

/** Whether text is in the row's name, its secrets, or a factory or environment it reaches. */
export function rowMatches(row: AccessRow, q: string): boolean {
  return [row.name, ...row.secrets, ...row.uses.flatMap((u) => [u.factory.name, u.environment])].some((t) =>
    t.toLowerCase().includes(q),
  );
}
