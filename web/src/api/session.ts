// What every /v1 request does about the session (spec 3 §10.1): a 401 sends the person to sign
// in, keeping the page they were on; a 403 reauth-required asks them to confirm it's them and
// sends the same request once more; a 403 forbidden (the screen offered what the caller may no
// longer do) has the app read `can` again, so the screen stops offering it. client.ts wraps the client's fetch with withSession; the app
// (main.tsx and the re-auth dialog) says what happens through setSessionHandlers.

export interface SessionHandlers {
  /** goald answered 401: the session is gone. The app clears its cache and opens the sign-in page. */
  unauthenticated: () => void;
  /**
   * goald answered 403 reauth-required: the change needs a sign-in from the last 10 minutes.
   * Resolves true once the person signed in again (the request is sent once more), false when
   * they cancelled (the refusal is the answer).
   */
  reauth: () => Promise<boolean>;
  /** goald answered 403 forbidden: what the screens read `can` from is stale. The refusal is the answer. */
  forbidden?: () => void;
}

/** Wraps fetch with the handling of 401 and 403 reauth-required. */
export function withSession(
  fetch: (request: Request) => Promise<Response>,
  handlers: SessionHandlers,
): (request: Request) => Promise<Response> {
  return async (request) => {
    // A request can be sent once: keep a copy (body included) for the retry.
    const retry = request.clone();
    const response = await fetch(request);
    if (response.status === 401) {
      handlers.unauthenticated();
      return response;
    }
    if (response.status !== 403) return response;
    const code = await problemCode(response);
    if (code === 'forbidden') handlers.forbidden?.();
    if (code !== 'reauth-required') return response;
    if (!(await handlers.reauth())) return response;
    return fetch(retry);
  };
}

async function problemCode(response: Response): Promise<string | undefined> {
  try {
    const body: unknown = await response.clone().json();
    return typeof body === 'object' && body !== null && 'code' in body && typeof body.code === 'string'
      ? body.code
      : undefined;
  } catch {
    return undefined;
  }
}

let current: SessionHandlers = { unauthenticated: () => {}, reauth: async () => false, forbidden: () => {} };

/** The app's handlers; until the app sets them, a 401 or reauth-required is only answered. */
export function setSessionHandlers(handlers: Partial<SessionHandlers>): void {
  current = { ...current, ...handlers };
}

/** The handlers set now, read per request so the app can set them after the client exists. */
export const sessionHandlers: SessionHandlers = {
  unauthenticated: () => current.unauthenticated(),
  reauth: () => current.reauth(),
  forbidden: () => current.forbidden?.(),
};

/**
 * The page to open after signing in: a path of this origin only (starting with one "/", never
 * "//" or "/\", which browsers read as another host), and never the sign-in page itself. Anything
 * else is undefined, and the caller opens the app's start.
 */
export function safeNext(next: unknown): string | undefined {
  if (typeof next !== 'string' || !next.startsWith('/') || next.startsWith('//') || next.startsWith('/\\')) {
    return undefined;
  }
  let url: URL;
  try {
    url = new URL(next, 'http://goal.invalid');
  } catch {
    return undefined;
  }
  if (url.origin !== 'http://goal.invalid' || url.pathname.startsWith('/sign-in')) return undefined;
  return `${url.pathname}${url.search}${url.hash}`;
}
