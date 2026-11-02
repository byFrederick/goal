// The Worker's answer when goald cannot answer a public request: a 502 instead of Cloudflare's
// 1101. A page load gets a page in the app's design (docs/design/MASTER.md), self-contained
// because goald, which serves the app and its fonts, is the part that is down; an API call gets
// the problem details every goald error has (contracts/api/openapi.yaml, Problem).

const title = 'Goal is not answering';
const detail = 'Goal did not answer this request, usually because it is restarting. Reload in a few seconds.';

// MASTER.md's colour tokens, light and dark, and its type scale. The Reload link's empty href is
// the page's own URL.
const page = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>${title}</title>
<style>
:root {
  --color-shell: #ffffff; --color-ink: #171717; --color-ink-soft: #4a4a47;
  --color-action: #171717; --color-action-ink: #ffffff; --color-run: #2b5bd7;
}
@media (prefers-color-scheme: dark) {
  :root {
    --color-shell: #0f0f0f; --color-ink: #ededec; --color-ink-soft: #b4b4b0;
    --color-action: #ededec; --color-action-ink: #111111; --color-run: #3d6be0;
  }
}
* { box-sizing: border-box; }
body {
  margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 24px 16px;
  background: var(--color-shell); color: var(--color-ink);
  font: 400 14px/1.5 "IBM Plex Sans", system-ui, sans-serif; font-variant-numeric: tabular-nums;
}
main { max-width: 420px; }
h1 { margin: 0 0 8px; font-size: 20px; font-weight: 600; letter-spacing: -0.01em; }
p { margin: 0 0 16px; color: var(--color-ink-soft); }
.primary {
  display: inline-flex; align-items: center; height: 32px; padding: 0 12px; border-radius: 6px;
  background: var(--color-action); color: var(--color-action-ink);
  font-size: 13px; font-weight: 500; text-decoration: none;
}
.primary:focus-visible { outline: 2px solid var(--color-run); outline-offset: 2px; }
</style>
</head>
<body>
<main>
<h1>${title}</h1>
<p>${detail}</p>
<a class="primary" href="">Reload</a>
</main>
</body>
</html>
`;

/** isNavigation says whether request loads a page: a GET or HEAD for HTML outside /v1 and /api. */
function isNavigation(request: Request): boolean {
  const { pathname } = new URL(request.url);
  const api = ['/v1', '/api'].some((p) => pathname === p || pathname.startsWith(`${p}/`));
  return (
    !api &&
    (request.method === 'GET' || request.method === 'HEAD') &&
    (request.headers.get('accept') ?? '').includes('text/html')
  );
}

/** unavailable answers request with a 502 that no cache keeps. */
export function unavailable(request: Request): Response {
  if (isNavigation(request)) {
    return new Response(page, {
      status: 502,
      headers: { 'content-type': 'text/html; charset=utf-8', 'cache-control': 'no-store' },
    });
  }
  return new Response(JSON.stringify({ title, status: 502, detail, code: 'goald-unavailable' }), {
    status: 502,
    headers: { 'content-type': 'application/problem+json', 'cache-control': 'no-store' },
  });
}
