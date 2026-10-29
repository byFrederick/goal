# ADR 0035 — The docs site is served from the control Worker's static assets on Cloudflare

Status: accepted · 2026-10-29 · Builds on [ADR 0008](0008-portable-runs-connections-and-knowledge.md) (the same code on Compose and Cloudflare, Cloudflare-only code in the control Worker) and [ADR 0011](0011-production-deploys-from-main.md) (the deploy from main)

## Context

`apps/docs` is an Astro Starlight site with `base: '/docs'` and `outDir: './dist/docs'` (`apps/docs/astro.config.mjs`). `task docs:site:build` writes a static site of about 200 pages, every file under `apps/docs/dist/docs/`, including `404.html`. It must be live at `https://goal.byfrederick.work/docs`.

Every public request reaches the control Worker (`apps/control-worker/src/index.ts`) through Terraform's route `goal.byfrederick.work/*`. Until this ADR, the Worker sent each one to goald's container, and goald serves the web UI from files embedded in its binary (`apps/web/dist_webui.go`, `//go:embed all:dist`). A change to those files ships only when the deploy has built a new goald image and Cloudflare has rolled the container over, which ends the runs in flight (cloudflare-operations.md, Deploys).

There were two ways to serve the docs site:

- **Embed it in goald**, like the web UI. One serving path on every target, but each docs change rebuilds and restarts goald, goald's image grows with each page, and a missing docs page costs a request to a container that may be asleep.
- **Upload it as the control Worker's static assets** (wrangler's `assets`). The files go up with the Worker version on each `wrangler deploy` and Cloudflare serves them from its edge, without the Worker or goald. A docs change needs only a new Worker version. The cost is a second serving path: Compose has no Worker, so something else serves `/docs` there.

## Decision

1. **On Cloudflare, `/docs` is the control Worker's static assets.** `deploy/cloudflare/wrangler.jsonc` has `assets` with `binding: "ASSETS"`, `directory: "../../apps/docs/dist"`, `html_handling: "auto-trailing-slash"` and `not_found_handling: "404-page"`. Because the build puts every file under `dist/docs/`, the files serve at `/docs/...` and no file exists at any other path. `auto-trailing-slash` answers `/docs/guides/` from `guides/index.html` and redirects `/docs` to `/docs/`. goald never embeds the docs site.
2. **Every path outside `/docs` runs the Worker first.** `run_worker_first` is `["/*", "!/docs", "!/docs/*"]`. A negative pattern wins over a positive one, so `/docs` and `/docs/*` follow the asset-first routing and every other path goes to the Worker's fetch handler before any asset is looked up. An array also turns off the `Sec-Fetch-Mode: navigate` shortcut, which would otherwise send a navigation that matches no asset to `not_found_handling` instead of the Worker. Cloudflare's rule is in [Static assets, binding](https://developers.cloudflare.com/workers/static-assets/binding/).
3. **A docs path that matches no file gets the site's 404 page, never goald.** Cloudflare sends such a request to the Worker. The fetch handler answers `/docs` and `/docs/*` only with `env.ASSETS.fetch(request)`, which applies `not_found_handling` and returns `dist/docs/404.html` with status 404. A path that only starts with the same letters, such as `/docsearch`, still goes to goald.
4. **Every production deploy builds the docs first.** `.github/workflows/deploy.yml` runs `task docs:site:build` before `task deploy:worker`, in a step without a Cloudflare token. `task deploy:worker` refuses to run when `apps/docs/dist/docs/index.html` is missing, because wrangler refuses a missing assets directory. A change under `apps/docs/` counts as an app change for the deploy's `changes` job, as `contracts/` already did (the site's API reference reads `contracts/api/openapi.yaml`).
5. **Under Docker Compose, goald serves `/docs` from a directory.** Compose has no control Worker, so goald serves the same build from a directory it is given, at the same path. That is built separately, and it keeps the docs out of goald's binary on that target too.

## Consequences

- A docs change ships with the next Worker version. The deploy still builds the images, but an image whose inputs did not change keeps its digest (cloudflare-operations.md, Deploys), so goald is not restarted for the docs alone.
- `/docs` has two serving paths, the Worker's assets on Cloudflare and goald's directory on Compose. They agree as long as both serve the one build in `apps/docs/dist` at `/docs`, with its `404.html` for missing pages.
- The control Worker's tests (`apps/control-worker/test/routing.test.ts`, `describe('docs')`) serve a fixture directory, `test/assets/`, through the same `assets` settings. They prove the fetch handler's routing and the binding, not Cloudflare's edge router, so `run_worker_first` is checked by the config test in `deploy/cloudflare/scripts/images.test.mjs`, which also shows that the generated `wrangler.deploy.jsonc` keeps the `assets` block.
- A path added under `/docs` by goald would never be reached on Cloudflare. Everything under `/docs` belongs to the docs site.
- Terraform does not change: the route `goal.byfrederick.work/*` already reaches the Worker.
