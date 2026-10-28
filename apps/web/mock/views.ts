// A factory's saved views in the mock API (ADR 0020, contracts/api/openapi.yaml, tag views): GET and POST
// /v1/orgs/{org}/factories/{factory}/views, and PATCH and DELETE …/views/{view}. The shared views come
// first, then the caller's personal ones, each by name (case ignored); a person never sees another's
// personal view. A shared view needs factory:manage (403 forbidden), a name is 1 to 60 characters
// (400 invalid-view), and a name a view of the same kind already uses is 409 view-name-taken. The mock
// starts with no views, so the Items page renders as its picture does.
import { randomUUID } from 'node:crypto';
import type { IncomingMessage, ServerResponse } from 'node:http';
import type { components, Org } from '@goal/api-client';

type SavedView = components['schemas']['SavedView'];
type SavedViewFilters = components['schemas']['SavedViewFilters'];

export interface ViewsDeps {
  clock: () => number;
  /** The caller's user id: the signed-in person's, else the local user's. */
  caller: (req: IncomingMessage) => string;
  /** A factory of the organization by id or name, with the permissions the caller holds on it. */
  factory: (org: string, key: string) => { id: string; can: readonly string[] } | undefined;
}

export interface MockViews {
  /** Starts over with no views. */
  reset: () => void;
  /** Serves a view route; false when rest is none of them. */
  handle: (
    org: Org,
    rest: string,
    method: string,
    req: IncomingMessage,
    res: ServerResponse,
    body: () => Promise<Record<string, unknown>>,
  ) => Promise<boolean>;
}

/** A view as the mock keeps it: the API's shape, and the user it belongs to. */
interface StoredView extends SavedView {
  owner: string;
}

const maxName = 60;
const maxFilterKeys = 20;
const maxFilterValues = 20;
const maxFilterValueLength = 200;

const send = (res: ServerResponse, status: number, body?: unknown) => {
  res.statusCode = status;
  if (status === 204 || body === undefined) return res.end();
  res.setHeader('Content-Type', status >= 400 ? 'application/problem+json' : 'application/json');
  res.end(JSON.stringify(body));
};

const refuse = (res: ServerResponse, status: number, code: string, detail: string) =>
  send(res, status, {
    title: status === 409 ? 'Conflict' : status === 403 ? 'Forbidden' : 'Not Found',
    status,
    detail,
    code,
  });

/** A name as the API takes it: trimmed, 1 to 60 characters. */
const cleanName = (value: unknown): string | undefined => {
  if (typeof value !== 'string') return undefined;
  const name = value.trim();
  return name.length >= 1 && name.length <= maxName ? name : undefined;
};

/** The filters as the API takes them: at most 20 keys, 20 values each, 200 characters a value. */
function cleanFilters(value: unknown): SavedViewFilters | undefined {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return undefined;
  const entries = Object.entries(value);
  if (entries.length > maxFilterKeys) return undefined;
  const filters: SavedViewFilters = {};
  for (const [key, values] of entries) {
    if (!Array.isArray(values) || values.length > maxFilterValues) return undefined;
    if (!values.every((v) => typeof v === 'string' && v.length <= maxFilterValueLength)) return undefined;
    filters[key] = values as string[];
  }
  return filters;
}

const byName = (a: StoredView, b: StoredView) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });

export function createViews({ clock, caller, factory }: ViewsDeps): MockViews {
  // The views of each factory, by factory id.
  let views = new Map<string, StoredView[]>();

  /** The API's shape of a view for this caller: `mine` is whether the caller made it. */
  const shaped = (view: StoredView, user: string): SavedView => {
    const { owner: _owner, ...rest } = view;
    return { ...rest, mine: view.owner === user };
  };

  /** The views the caller sees, shared first, each group by name. */
  const visibleTo = (factoryId: string, user: string): SavedView[] => {
    const all = views.get(factoryId) ?? [];
    const shared = all.filter((v) => v.shared).sort(byName);
    const own = all.filter((v) => !v.shared && v.owner === user).sort(byName);
    return [...shared, ...own].map((v) => shaped(v, user));
  };

  /** Another view of the same kind with this name: shared ones among shared, the owner's personal ones among theirs. */
  const taken = (factoryId: string, name: string, shared: boolean, owner: string, except?: string) =>
    (views.get(factoryId) ?? []).some(
      (v) =>
        v.id !== except &&
        v.name.toLowerCase() === name.toLowerCase() &&
        (shared ? v.shared : !v.shared && v.owner === owner),
    );

  return {
    reset: () => {
      views = new Map();
    },

    handle: async (org, rest, method, req, res, body) => {
      const match = rest.match(/^\/factories\/([^/]+)\/views(?:\/([^/]+))?$/);
      if (!match) return false;
      const [, factoryKey = '', viewId] = match;
      const found = factory(org.id, decodeURIComponent(factoryKey));
      if (!found) {
        refuse(res, 404, 'factory-not-found', 'The factory is not in this organization');
        return true;
      }
      const user = caller(req);
      const canManage = found.can.includes('factory:manage');
      const now = new Date(clock()).toISOString();

      if (!viewId && method === 'GET') {
        send(res, 200, { views: visibleTo(found.id, user) });
        return true;
      }

      if (!viewId && method === 'POST') {
        const input = await body();
        const name = cleanName(input.name);
        const filters = cleanFilters(input.filters);
        if (!name || !filters || typeof input.shared !== 'boolean') {
          refuse(res, 400, 'invalid-view', 'A view needs a name of 1 to 60 characters and its filters');
          return true;
        }
        if (input.shared && !canManage) {
          refuse(res, 403, 'forbidden', 'Sharing a view needs factory:manage');
          return true;
        }
        if (taken(found.id, name, input.shared, user)) {
          refuse(res, 409, 'view-name-taken', 'A view with this name already exists');
          return true;
        }
        const view: StoredView = {
          id: randomUUID(),
          name,
          shared: input.shared,
          filters,
          mine: true,
          created_at: now,
          updated_at: now,
          owner: user,
        };
        views.set(found.id, [...(views.get(found.id) ?? []), view]);
        send(res, 201, shaped(view, user));
        return true;
      }

      // Every other route names one view, which the caller must see: another person's personal view is not found.
      const stored = (views.get(found.id) ?? []).find((v) => v.id === viewId && (v.shared || v.owner === user));
      if (!stored) {
        refuse(res, 404, 'view-not-found', 'No view with this id');
        return true;
      }
      if (stored.shared && !canManage) {
        refuse(res, 403, 'forbidden', 'Changing a shared view needs factory:manage');
        return true;
      }

      if (method === 'DELETE') {
        views.set(
          found.id,
          (views.get(found.id) ?? []).filter((v) => v.id !== stored.id),
        );
        send(res, 204);
        return true;
      }

      if (method === 'PATCH') {
        const input = await body();
        const name = cleanName(input.name);
        const filters = cleanFilters(input.filters);
        if (!name || !filters) {
          refuse(res, 400, 'invalid-view', 'A view needs a name of 1 to 60 characters and its filters');
          return true;
        }
        if (taken(found.id, name, stored.shared, stored.owner, stored.id)) {
          refuse(res, 409, 'view-name-taken', 'A view with this name already exists');
          return true;
        }
        stored.name = name;
        stored.filters = filters;
        stored.updated_at = now;
        send(res, 200, shaped(stored, user));
        return true;
      }

      return false;
    },
  };
}
