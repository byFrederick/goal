// How the Connections section describes an entry before phase 4 (D §10.8): its kind, how it
// signs in (env: references only, never a value), whether the gateway serves it yet, and its
// status, taken from B's problems at its pointer rather than from a test call.
import type { DefinitionProblem } from '../../../api/types';
import type { Model, ModelConnection } from '../../../shared/definition/model';

const setting = (c: ModelConnection, key: string): unknown => c.settings?.[key];

/** An entry naming one of the organization's connections (spec 4 §4.4), not an inline form. */
const isOrgConnection = (c: ModelConnection): boolean => !c.connector && !c.type;

/**
 * "Organization connection", or for the removed inline forms "Remote MCP server", "MCP server
 * image", "Command", "Built-in", "AWS role" or "<connector> connector".
 */
export function connectionKind(c: ModelConnection): string {
  if (isOrgConnection(c)) return 'Organization connection';
  if (c.connector) return `${c.connector.split('@')[0]} connector`;
  if (c.type === 'builtin') return 'Built-in';
  if (c.type === 'aws') return 'AWS role';
  if (setting(c, 'transport') === 'http') return 'Remote MCP server';
  return setting(c, 'image') ? 'MCP server image' : 'Command';
}

const envRef = /^env:[A-Za-z_][A-Za-z0-9_]*$/;

/** The env: references of a legacy entry: in its headers, its env and its command words. */
function envRefs(c: ModelConnection): string[] {
  const values: unknown[] = [];
  for (const key of ['headers', 'env']) {
    const map = setting(c, key);
    if (map && typeof map === 'object') values.push(...Object.values(map));
  }
  const command = setting(c, 'command');
  if (Array.isArray(command)) values.push(...command);
  return [...new Set(values.filter((v): v is string => typeof v === 'string' && envRef.test(v)))];
}

/** How the entry signs in: "Uses env:NAME" (a platform secret), its credential, or "No sign-in". */
export function signIn(c: ModelConnection): string {
  if (isOrgConnection(c)) return 'Uses the organization’s connection';
  if (c.credential) return `Uses the credential ${c.credential}`;
  const refs = envRefs(c);
  return refs.length > 0 ? `Uses ${refs.join(', ')}` : 'No sign-in';
}

/** The gateway serves remote MCP servers and MCP server images today (connectors from phase 4). */
export function served(c: ModelConnection): boolean {
  if (c.connector) return true;
  if (c.type !== 'mcp') return false;
  return setting(c, 'transport') === 'http' || Boolean(setting(c, 'image'));
}

export interface ConnectionStatus {
  tone: 'ok' | 'rework' | 'muted';
  text: string;
}

/** The text colour of each status (the words say it too). */
export const statusTone: Record<ConnectionStatus['tone'], string> = {
  ok: 'text-ok-ink',
  rework: 'text-rework-ink',
  muted: 'text-muted',
};

/** The problems at the connection's pointer or inside it. */
export function problemsAt(index: number, problems: readonly DefinitionProblem[]): DefinitionProblem[] {
  const at = `/connections/${index}`;
  return problems.filter((p) => p.pointer === at || p.pointer?.startsWith(`${at}/`));
}

export function connectionStatus(
  c: ModelConnection,
  index: number,
  problems: readonly DefinitionProblem[],
): ConnectionStatus {
  const mine = problemsAt(index, problems).filter((p) => p.severity !== 'info');
  if (mine.some((p) => p.code === 'secret-env-unset')) return { tone: 'rework', text: 'Variable not set' };
  if (mine.some((p) => p.code === 'credentials-unavailable')) {
    return { tone: 'rework', text: 'Credentials arrive with phase 4' };
  }
  if (mine.length > 0) return { tone: 'rework', text: mine.length === 1 ? '1 problem' : `${mine.length} problems` };
  if (!served(c)) return { tone: 'muted', text: 'Not served until a later phase' };
  return { tone: 'ok', text: 'No problems' };
}

/** Who may use the connection: a connector-form entry's `agents`; for any other, each agent's list. */
export function agentsWithAccess(model: Model, c: ModelConnection): string[] {
  if (c.connector) return c.agents ?? [];
  return (model.agents.agents ?? []).filter((a) => a.connections?.includes(c.name)).map((a) => a.name);
}
