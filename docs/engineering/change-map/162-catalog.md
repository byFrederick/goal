## When you change

The tool templates: a template file (`internal/catalog/*.yaml`), the catalog's reader (`internal/catalog/catalog.go`) or `domain.ToolTemplate` and its checks (`internal/core/domain/catalog.go`)

## Also change

`GET /v1/catalog` in `contracts/api/openapi.yaml` (`ToolTemplate` and its parts) and `toolTemplateOut` (`internal/api/catalog.go`) when a template gains a field; the template test the environment test route sends (`sendTemplateTest` in `internal/api/environmenttest.go`) when the test's form changes; the count and names in `TestTemplates_LoadAll` and `TestCatalog_List` when a template is added or removed

## Run

`go test ./internal/catalog/... ./internal/core/domain/...`, `task api:gen` after a contract change, `go test -run TestCatalog_ ./internal/api`, `go test -run TestEnvironmentTest_ ./internal/api`
