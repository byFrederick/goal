## When you change

The install's starter templates: a folder under `deploy/catalog/templates/<name>/<version>/` (`template.yaml` and its `factory/`), or adding a template or a version ([ADR 0042](../adr/0042-starter-templates-are-committed-product-content.md))

## Also change

A changed template gets a new `<version>/` folder rather than an edit to one already pushed: the catalog refuses a version it holds with different content. After the merge, the operator runs `task catalog:production` (docs/engineering/cloudflare-operations.md, Deploys › Starter templates); until then production's onboarding offers the old versions

## Run

`task catalog:templates:push` against the local RustFS (`task storage:up`, `task storage:init` first), then `task catalog:production` after merge
