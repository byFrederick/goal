// What the Connections pages say about kinds, goald's refusals and the audit log, as pure
// functions (words.test.ts): a kind's short summary and how runs use each of its modes, a
// refusal's sentence, and an audit entry about a connection in a few words.
import { ApiError } from '../../api/client';
import type { AuditEntry, ConnectionKind } from '../../api/types';
import { since } from '../../shared/connections';
import { formatDay } from '../../shared/format';

const lower = (s: string) => (/^[A-Z][a-z]/.test(s) ? s.charAt(0).toLowerCase() + s.slice(1) : s);

/** "Token and host", "Access key ID and secret access key", "URL or command": what a kind asks for. */
export function kindSummary(kind: ConnectionKind): string {
  const required = kind.fields.filter((f) => f.required);
  const shown = (required.length > 0 ? required : kind.fields).map((f, i) => (i === 0 ? f.title : lower(f.title)));
  if (shown.length <= 1) return shown[0] ?? '';
  const joiner = required.length > 0 ? 'and' : 'or';
  return `${shown.slice(0, -1).join(', ')} ${joiner} ${shown.at(-1)}`;
}

/** How runs use one mode of a kind, in a sentence. */
export function modeSentence(mode: ConnectionKind['modes'][number], kind: ConnectionKind, env: string[]): string {
  switch (mode) {
    case 'env':
      return env.length > 0
        ? `APIs and CLIs read ${env.length === 1 ? env[0] : 'these variables'}.`
        : 'APIs and CLIs read the variables it sets.';
    case 'cli':
      return 'Its CLI is installed in the box when a run starts.';
    case 'mcp':
      return 'An MCP server the agent calls straight from its box.';
    case 'git':
      return `git over https signs in with it${kind.code_host ? ', and the merge station merges its pull requests' : ''}.`;
    default:
      return '';
  }
}

/** The sentence for a refusal of a connection change (goald's Problem detail, or why it could not ask). */
export function refusalText(err: unknown): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Try again once it is running.';
  if (err.code === 'connections-unavailable') return unavailableText;
  return err.detail ?? err.title;
}

/** goald has no master key (503 connections-unavailable). */
export const unavailableText =
  'goald has no master key, so it can’t seal or open secrets: connections with secrets can’t be created, tested or changed. Whoever runs goald sets GOAL_MASTER_KEYS and restarts it.';

const data = (e: AuditEntry, key: string): unknown => (e.data as Record<string, unknown>)[key];

/** What happened, in a few words: "created", "token replaced", "test failed: 401", "used by a run". */
export function activityText(e: AuditEntry): string {
  switch (e.action) {
    case 'connection.created':
      return 'created';
    case 'connection.updated': {
      const from = data(e, 'from');
      return typeof from === 'string' ? `renamed from ${from}` : 'changed';
    }
    case 'connection.secret_replaced': {
      const secrets = data(e, 'secrets');
      const names = Array.isArray(secrets) ? secrets.map((s) => String(s).replaceAll('_', ' ')) : [];
      return names.length > 0 ? `${names.join(' and ')} replaced` : 'secret replaced';
    }
    case 'connection.tested': {
      if (data(e, 'ok') === true) return 'tested, passed';
      const error = data(e, 'error');
      return typeof error === 'string' && error ? `test failed: ${error}` : 'test failed';
    }
    case 'connection.used':
      return 'used by a run';
    case 'connection.deleted':
      return 'deleted';
    default:
      return e.action;
  }
}

/** The connection an entry is about, by the name it had then. */
export function activityName(e: AuditEntry): string {
  const name = data(e, 'name');
  return typeof name === 'string' ? name : e.target.id;
}

/** "Just now", "14 min ago" within a day, else "Oct 22". */
export function activityWhen(iso: string, now: number): string {
  return now - Date.parse(iso) < 86_400_000 ? since(iso, now) : formatDay(iso);
}
