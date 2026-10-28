import type { IncomingMessage, ServerResponse } from 'node:http';
import type { Org } from '@goal/api-client';
import { describe, expect, it } from 'vitest';
import { createViews } from './views.ts';

// A response that records what the handler wrote, so the routes are tested without a server.
function fakeRes() {
  const res = {
    statusCode: 200,
    body: '' as string,
    setHeader: () => {},
    end(chunk?: string) {
      res.body = chunk ?? '';
    },
  };
  return res;
}

const org = { id: 'org-1', slug: 'acme' } as Org;
const base = '/factories/factory-1/views';

/** A mock whose caller is `user`, holding factory:manage when `manage` is set. */
function setup(user: string, manage: boolean) {
  const views = createViews({
    clock: () => Date.parse('2026-10-28T09:00:00Z'),
    caller: () => user,
    factory: (_org, key) =>
      key === 'factory-1' ? { id: 'factory-1', can: manage ? ['factory:manage'] : [] } : undefined,
  });
  const call = async (method: string, rest: string, payload?: Record<string, unknown>) => {
    const res = fakeRes();
    const handled = await views.handle(
      org,
      rest,
      method,
      {} as IncomingMessage,
      res as unknown as ServerResponse,
      async () => payload ?? {},
    );
    return { handled, status: res.statusCode, body: res.body ? JSON.parse(res.body) : undefined };
  };
  return { call, reset: views.reset };
}

describe('mock saved views', () => {
  it('starts with no views', async () => {
    const { call } = setup('f-cruz', true);
    const list = await call('GET', base);
    expect(list.status).toBe(200);
    expect(list.body).toEqual({ views: [] });
  });

  it('lists shared views first, then the caller’s own, each by name ignoring case', async () => {
    const { call } = setup('f-cruz', true);
    await call('POST', base, { name: 'zeta', shared: false, filters: {} });
    await call('POST', base, { name: 'Beta', shared: true, filters: {} });
    await call('POST', base, { name: 'alpha', shared: false, filters: {} });
    await call('POST', base, { name: 'Gamma', shared: true, filters: {} });
    const { body } = await call('GET', base);
    expect(body.views.map((v: { name: string }) => v.name)).toEqual(['Beta', 'Gamma', 'alpha', 'zeta']);
  });

  it('creates a view with its filters, and the trimmed name', async () => {
    const { call } = setup('f-cruz', false);
    const created = await call('POST', base, {
      name: '  Morning  ',
      shared: false,
      filters: { state: ['waiting_human'] },
    });
    expect(created.status).toBe(201);
    expect(created.body).toMatchObject({
      name: 'Morning',
      shared: false,
      mine: true,
      filters: { state: ['waiting_human'] },
    });
  });

  it('refuses a name of 0 or over 60 characters with invalid-view', async () => {
    const { call } = setup('f-cruz', true);
    expect((await call('POST', base, { name: '   ', shared: false, filters: {} })).body.code).toBe('invalid-view');
    const long = 'x'.repeat(61);
    const over = await call('POST', base, { name: long, shared: false, filters: {} });
    expect(over.status).toBe(400);
    expect(over.body.code).toBe('invalid-view');
  });

  it('refuses a name the caller’s personal views use with view-name-taken, ignoring case', async () => {
    const { call } = setup('f-cruz', true);
    await call('POST', base, { name: 'Morning', shared: false, filters: {} });
    const taken = await call('POST', base, { name: 'morning', shared: false, filters: {} });
    expect(taken.status).toBe(409);
    expect(taken.body.code).toBe('view-name-taken');
  });

  it('refuses a shared view without factory:manage with forbidden', async () => {
    const { call } = setup('f-cruz', false);
    const shared = await call('POST', base, { name: 'Team', shared: true, filters: {} });
    expect(shared.status).toBe(403);
    expect(shared.body.code).toBe('forbidden');
  });

  it('does not show another person’s personal view, and answers view-not-found for it', async () => {
    const views = createViews({
      clock: () => 0,
      caller: (req) => (req as unknown as { user: string }).user,
      factory: () => ({ id: 'factory-1', can: ['factory:manage'] }),
    });
    const as = (user: string) => ({ user }) as unknown as IncomingMessage;
    const created = fakeRes();
    await views.handle(org, base, 'POST', as('f-cruz'), created as unknown as ServerResponse, async () => ({
      name: 'Mine',
      shared: false,
      filters: {},
    }));
    const id = JSON.parse(created.body).id as string;
    const okafor = fakeRes();
    await views.handle(org, base, 'GET', as('okafor'), okafor as unknown as ServerResponse, async () => ({}));
    expect(JSON.parse(okafor.body)).toEqual({ views: [] });
    const rename = fakeRes();
    await views.handle(org, `${base}/${id}`, 'PATCH', as('okafor'), rename as unknown as ServerResponse, async () => ({
      name: 'Stolen',
      filters: {},
    }));
    expect(rename.statusCode).toBe(404);
    expect(JSON.parse(rename.body).code).toBe('view-not-found');
  });

  it('renames a view and replaces its filters, refusing a name another of its kind holds', async () => {
    const { call } = setup('f-cruz', true);
    const a = await call('POST', base, { name: 'Morning', shared: false, filters: { state: ['waiting_human'] } });
    await call('POST', base, { name: 'Evening', shared: false, filters: {} });
    const clash = await call('PATCH', `${base}/${a.body.id}`, { name: 'evening', filters: {} });
    expect(clash.body.code).toBe('view-name-taken');
    const renamed = await call('PATCH', `${base}/${a.body.id}`, { name: 'Early', filters: { lane: ['fast'] } });
    expect(renamed.status).toBe(200);
    expect(renamed.body).toMatchObject({ name: 'Early', filters: { lane: ['fast'] } });
  });

  it('deletes a view, and answers view-not-found for one already gone', async () => {
    const { call } = setup('f-cruz', true);
    const a = await call('POST', base, { name: 'Morning', shared: false, filters: {} });
    const gone = await call('DELETE', `${base}/${a.body.id}`);
    expect(gone.status).toBe(204);
    expect((await call('GET', base)).body).toEqual({ views: [] });
    expect((await call('DELETE', `${base}/${a.body.id}`)).body.code).toBe('view-not-found');
  });

  it('leaves a route it does not know to the rest of the mock', async () => {
    const { call } = setup('f-cruz', true);
    expect((await call('GET', '/factories/factory-1/items')).handled).toBe(false);
  });

  it('answers factory-not-found for a factory it does not have', async () => {
    const { call } = setup('f-cruz', true);
    const res = await call('GET', '/factories/nope/views');
    expect(res.status).toBe(404);
    expect(res.body.code).toBe('factory-not-found');
  });
});
