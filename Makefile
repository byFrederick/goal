# Every target here is what CI runs (.github/workflows/ci.yml). `make check` before calling a change done.
GO       ?= go
GOLANGCI ?= golangci-lint
PG_IMAGE ?= postgres:18

.PHONY: help build test test-integration lint fmt validate tf-check migrate-check check clean

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

build: ## Build every binary in cmd/ into bin/
	$(GO) build -trimpath -o bin/ ./cmd/...

test: ## Unit tests with the race detector
	$(GO) test -race ./...

test-integration: ## Integration tests against PostgreSQL 18 from compose.yaml
	docker compose up -d --wait postgres
	GOAL_TEST_DATABASE_URL=postgres://goal:$${GOAL_PG_PASSWORD:-goal-dev-only}@localhost:5432/goal?sslmode=disable \
		$(GO) test -race -tags integration ./...

lint: ## golangci-lint v2 with the repo's rules (.golangci.yml)
	$(GOLANGCI) run ./...

fmt: ## Format Go code
	$(GOLANGCI) fmt ./...

validate: ## Validate every factory definition
	$(GO) run ./cmd/goal validate

tf-check: ## terraform fmt and validate (no AWS calls)
	cd terraform && terraform fmt -check -recursive && terraform init -backend=false -input=false >/dev/null && terraform validate

migrate-check: ## Apply all migrations to a throwaway PostgreSQL 18
	@docker rm -f goal-migrate-check >/dev/null 2>&1 || true
	docker run -d --rm --name goal-migrate-check -e POSTGRES_PASSWORD=check -e POSTGRES_DB=goal $(PG_IMAGE) >/dev/null
	@until docker exec goal-migrate-check pg_isready -h 127.0.0.1 -U postgres -d goal >/dev/null 2>&1; do sleep 1; done
	@for f in migrations/*.sql; do \
		docker exec -i goal-migrate-check psql -U postgres -d goal -v ON_ERROR_STOP=1 -q < $$f || { docker stop goal-migrate-check >/dev/null; exit 1; }; \
		echo "applied $$f"; \
	done
	@docker stop goal-migrate-check >/dev/null

check: lint test validate tf-check ## Everything CI checks except integration tests

clean: ## Remove build output
	rm -rf bin/
