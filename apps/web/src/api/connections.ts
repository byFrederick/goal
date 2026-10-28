// The organization's connections and connection kinds (spec 4 §8.2) over client.ts's goald client,
// and the audit entries about them (the Connections page's recent activity, a connection's recent
// use). Every change and test needs a sign-in from the last 10 minutes: a 403 reauth-required
// opens the re-auth dialog and the request is sent once more (session.ts). Secret values go out
// on create and replace only; goald never answers one.
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type {
  AuditPage,
  Connection,
  ConnectionKind,
  ConnectionKindFile,
  ConnectionPatch,
  ConnectionTestResult,
  NewConnection,
  RepositoryList,
} from './types';

/** The platform's kinds and the organization's own, by name. */
export async function listConnectionKinds(org: string, signal?: AbortSignal): Promise<ConnectionKind[]> {
  const body = await unwrap(goal.GET(orgPath('/connection-kinds'), { params: { path: { org } }, ...cancel(signal) }));
  return body.kinds;
}

/** Creates or replaces an organization kind; 400 connection-kind-invalid, 409 connection-kind-name-taken. */
export function putConnectionKind(org: string, kind: string, file: ConnectionKindFile): Promise<ConnectionKind> {
  return unwrap(goal.PUT(orgPath('/connection-kinds/{kind}'), { params: { path: { org, kind } }, body: file }));
}

export interface ConnectionFilter {
  /** Only the connections this factory's current version names (its id or name). */
  factory?: string;
}

/** The organization's connections, by name, never a secret value. */
export async function listConnections(
  org: string,
  filter: ConnectionFilter = {},
  signal?: AbortSignal,
): Promise<Connection[]> {
  const body = await unwrap(
    goal.GET(orgPath('/connections'), {
      params: { path: { org }, query: filter.factory ? { factory: filter.factory } : {} },
      ...cancel(signal),
    }),
  );
  return body.connections;
}

/** One connection with the env names a run gets and its kind's docs; 404 connection-not-found. */
export function getConnection(org: string, connection: string, signal?: AbortSignal): Promise<Connection> {
  return unwrap(
    goal.GET(orgPath('/connections/{connection}'), { params: { path: { org, connection } }, ...cancel(signal) }),
  );
}

/** The owners a code host connection reaches and the repositories of one (connection:browse); `owner` absent is the connection's own account. */
export function listConnectionRepositories(
  org: string,
  connection: string,
  owner: string | undefined,
  signal?: AbortSignal,
): Promise<RepositoryList> {
  return unwrap(
    goal.GET(orgPath('/connections/{connection}/repositories'), {
      params: { path: { org, connection }, query: owner ? { owner } : {} },
      ...cancel(signal),
    }),
  );
}

/** 400 field-required, field-unknown; 404 connection-kind-not-found; 409 connection-name-taken; 503 connections-unavailable. */
export function createConnection(org: string, connection: NewConnection): Promise<Connection> {
  return unwrap(goal.POST(orgPath('/connections'), { params: { path: { org } }, body: connection }));
}

/** A rename, a description, every field, or the named secrets; 409 connection-in-use for a rename while used. */
export function updateConnection(org: string, connection: string, patch: ConnectionPatch): Promise<Connection> {
  return unwrap(
    goal.PATCH(orgPath('/connections/{connection}'), { params: { path: { org, connection } }, body: patch }),
  );
}

/** 409 connection-in-use, naming what uses it. */
export async function deleteConnection(org: string, connection: string): Promise<void> {
  await unwrap(goal.DELETE(orgPath('/connections/{connection}'), { params: { path: { org, connection } } }));
}

/** The kind's test from goald; a failed test is ok false, 409 connection-not-testable without one. */
export function testConnection(org: string, connection: string): Promise<ConnectionTestResult> {
  return unwrap(goal.POST(orgPath('/connections/{connection}/test'), { params: { path: { org, connection } } }));
}

/** The newest audit entries about connections, or about one connection (by its id). */
export function listConnectionAudit(
  org: string,
  { connection, limit }: { connection?: string | undefined; limit: number },
  signal?: AbortSignal,
): Promise<AuditPage> {
  return unwrap(
    goal.GET(orgPath('/audit'), {
      params: {
        path: { org },
        query: { target_kind: 'connection', limit, ...(connection ? { target_id: connection } : {}) },
      },
      ...cancel(signal),
    }),
  );
}
