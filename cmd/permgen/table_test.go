package main

import (
	"strings"
	"testing"
)

const testPermissions = `version: 1
org_roles: [owner, admin, member]
factory_roles: [viewer, approver, editor]
permissions:
  - {name: public, level: install}
  - {name: org:read, level: org, org_roles: [owner, admin, member], keys: true}
  - {name: item:decide, level: factory, factory_role: approver, keys: true}
`

const testOpenAPI = `paths:
  /v1/health:
    get: {operationId: getHealth, x-goal-scope: public}
  /v1/orgs/{org}:
    parameters: [{name: org, in: path}]
    get: {operationId: getOrg, x-goal-scope: "org:read"}
  /v1/orgs/{org}/items/{item}/approve:
    post: {operationId: approveItem, x-goal-scope: "item:decide"}
`

func TestLoad_ReadsBothSources(t *testing.T) {
	t.Parallel()

	tb, err := load([]byte(testPermissions), []byte(testOpenAPI))
	if err != nil {
		t.Fatal(err)
	}

	if got := tb.Scopes["approveItem"]; got != "item:decide" {
		t.Errorf("approveItem scope %q, want item:decide", got)
	}

	if len(tb.Scopes) != 3 {
		t.Errorf("scopes %v, want the three operations", tb.Scopes)
	}

	if tb.Permissions[0].Name != "item:decide" {
		t.Errorf("permissions not sorted by name: %v", tb.Permissions)
	}
}

func TestLoad_RefusesATableThatDoesNotHoldTogether(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		permissions string // replaces the first permission line, "" keeps it
		openapi     string // "" keeps testOpenAPI
		want        string
	}{
		{name: "operation without a scope", openapi: "paths:\n  /v1/health:\n    get: {operationId: getHealth}\n", want: "getHealth has no x-goal-scope"},
		{name: "unknown scope", openapi: "paths:\n  /v1/health:\n    get: {operationId: getHealth, x-goal-scope: everything}\n", want: `"everything" is not in api/permissions.yaml`},
		{name: "operation without an id", openapi: "paths:\n  /v1/health:\n    get: {x-goal-scope: public}\n", want: "no operationId"},
		{name: "operationId twice", openapi: "paths:\n  /a:\n    get: {operationId: x, x-goal-scope: public}\n  /b:\n    get: {operationId: x, x-goal-scope: public}\n", want: "used twice"},
		{name: "misspelt field", permissions: "  - {name: public, level: install, freshh: true}", want: "freshh"},
		{name: "unknown level", permissions: "  - {name: public, level: planet}", want: "unknown level"},
		{name: "unknown org role", permissions: "  - {name: public, level: org, org_roles: [boss]}", want: `unknown org role "boss"`},
		{name: "org permission without roles", permissions: "  - {name: public, level: org}", want: "names its org_roles"},
		{name: "factory permission with an unknown role", permissions: "  - {name: public, level: factory, factory_role: boss}", want: "one of factory_roles"},
		{name: "factory permission with org roles", permissions: "  - {name: public, level: factory, factory_role: viewer, org_roles: [owner]}", want: "no org_roles"},
		{name: "install permission with roles", permissions: "  - {name: public, level: install, org_roles: [owner]}", want: "no org_roles or factory_role"},
		{name: "platform admin outside install", permissions: "  - {name: public, level: org, org_roles: [owner], platform_admin: true}", want: "platform_admin"},
		{name: "fresh permission for keys", permissions: "  - {name: public, level: org, org_roles: [owner], fresh: true, keys: true}", want: "keys never hold a fresh permission"},
		{name: "listed twice", permissions: "  - {name: org:read, level: install}", want: "listed twice"},
		{name: "malformed name", permissions: "  - {name: Public, level: install}", want: "lowercase"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			perms, openapi := testPermissions, testOpenAPI
			if tt.permissions != "" {
				perms = strings.Replace(perms, "  - {name: public, level: install}", tt.permissions, 1)
			}

			if tt.openapi != "" {
				openapi = tt.openapi
			}

			_, err := load([]byte(perms), []byte(openapi))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("load: %v, want an error containing %q", err, tt.want)
			}
		})
	}
}
