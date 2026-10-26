import { describe, expect, test, vi } from 'vitest';
import { safeNext, withSession } from './session';

const problem = (status: number, code: string) =>
  new Response(JSON.stringify({ title: 'No', status, code }), {
    status,
    headers: { 'Content-Type': 'application/problem+json' },
  });

/** goald behind a fake fetch: it answers each request with the next response, recording the bodies. */
function goald(...responses: Response[]) {
  const bodies: string[] = [];
  const fetch = vi.fn(async (request: Request) => {
    bodies.push(await request.text());
    const next = responses.shift();
    if (!next) throw new Error('no answer left');
    return next;
  });
  return { fetch, bodies };
}

const post = () =>
  new Request('http://goal.example.com/v1/orgs/acme', { method: 'DELETE', body: JSON.stringify({ confirm: 'Acme' }) });

describe('safeNext', () => {
  test.each([
    ['/o/acme/f/goal/board?station=review', '/o/acme/f/goal/board?station=review'],
    ['/', '/'],
  ])('keeps the same-origin path %s', (next, want) => {
    expect(safeNext(next)).toBe(want);
  });

  test.each([
    ['https://evil.example.com/o/acme'],
    ['//evil.example.com'],
    ['/\\evil.example.com'],
    ['o/acme'],
    ['javascript:alert(1)'],
    ['/sign-in?next=/o/acme'],
    [''],
    [undefined],
    [42],
  ])('refuses %s', (next) => {
    expect(safeNext(next)).toBeUndefined();
  });
});

describe('withSession', () => {
  test('a 401 tells the app to sign in again and is answered as it is', async () => {
    const { fetch } = goald(problem(401, 'unauthenticated'));
    const unauthenticated = vi.fn();
    const reauth = vi.fn(async () => true);

    const res = await withSession(fetch, { unauthenticated, reauth })(new Request('http://goal.example.com/v1/orgs'));

    expect(res.status).toBe(401);
    expect(unauthenticated).toHaveBeenCalledOnce();
    expect(reauth).not.toHaveBeenCalled();
  });

  test('reauth-required asks the person to confirm, then sends the same request once more', async () => {
    const { fetch, bodies } = goald(problem(403, 'reauth-required'), Response.json({ purge_after: 'later' }));
    const reauth = vi.fn(async () => true);

    const res = await withSession(fetch, { unauthenticated: vi.fn(), reauth })(post());

    expect(res.status).toBe(200);
    expect(reauth).toHaveBeenCalledOnce();
    expect(bodies).toEqual(['{"confirm":"Acme"}', '{"confirm":"Acme"}']);
  });

  test('a cancelled confirmation answers the refusal; nothing is sent again', async () => {
    const { fetch } = goald(problem(403, 'reauth-required'));
    const reauth = vi.fn(async () => false);

    const res = await withSession(fetch, { unauthenticated: vi.fn(), reauth })(post());

    expect(res.status).toBe(403);
    expect(await res.json()).toMatchObject({ code: 'reauth-required' });
    expect(fetch).toHaveBeenCalledOnce();
  });

  test('the retry is the only one: a second reauth-required is answered as it is', async () => {
    const { fetch } = goald(problem(403, 'reauth-required'), problem(403, 'reauth-required'));
    const reauth = vi.fn(async () => true);

    const res = await withSession(fetch, { unauthenticated: vi.fn(), reauth })(post());

    expect(res.status).toBe(403);
    expect(reauth).toHaveBeenCalledOnce();
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  test('a 403 forbidden is not a question of signing in: the app reads what the caller may do again', async () => {
    const { fetch } = goald(problem(403, 'forbidden'));
    const reauth = vi.fn(async () => true);
    const unauthenticated = vi.fn();
    const forbidden = vi.fn();

    const res = await withSession(fetch, { unauthenticated, reauth, forbidden })(post());

    expect(res.status).toBe(403);
    expect(await res.json()).toMatchObject({ code: 'forbidden' });
    expect(forbidden).toHaveBeenCalledOnce();
    expect(reauth).not.toHaveBeenCalled();
    expect(unauthenticated).not.toHaveBeenCalled();
    expect(fetch).toHaveBeenCalledOnce();
  });

  test('other refusals leave what the caller may do alone', async () => {
    const { fetch } = goald(problem(403, 'cross-origin'), problem(404, 'factory-not-found'));
    const forbidden = vi.fn();
    const send = withSession(fetch, { unauthenticated: vi.fn(), reauth: vi.fn(async () => true), forbidden });

    expect((await send(post())).status).toBe(403);
    expect((await send(post())).status).toBe(404);
    expect(forbidden).not.toHaveBeenCalled();
  });
});
