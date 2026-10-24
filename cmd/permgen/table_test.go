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
  - {name: member:manage, level: org, org_roles: [owner, admin], fresh: true, better_auth: {member: [update, delete], invitation: [create]}}
  - {name: org:delete, level: org, org_roles: [owner], fresh: true, better_auth: {organization: [delete]}}
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
		{name: "fresh permission for tokens", permissions: "  - {name: public, level: org, org_roles: [owner], fresh: true, pat: true}", want: "personal tokens never hold a fresh permission"},
		{name: "platform permission for tokens", permissions: "  - {name: public, level: install, platform_admin: true, pat: true}", want: "never hold a platform admin's"},
		{name: "install permission for tokens", permissions: "  - {name: public, level: install, pat: true}", want: "no install permission but authenticated"},
		{name: "listed twice", permissions: "  - {name: org:read, level: install}", want: "listed twice"},
		{name: "malformed name", permissions: "  - {name: Public, level: install}", want: "lowercase"},
		{name: "better_auth outside an org permission", permissions: "  - {name: public, level: install, better_auth: {member: [update]}}", want: "better_auth is for org permissions"},
		{name: "better_auth resource without actions", permissions: "  - {name: public, level: org, org_roles: [owner], better_auth: {member: []}}", want: "at least one action"},
		{name: "malformed better_auth action", permissions: "  - {name: public, level: org, org_roles: [owner], better_auth: {member: [\"up date\"]}}", want: "a lowercase name"},
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

func TestLoad_RefusesAMalformedRoleName(t *testing.T) {
	t.Parallel()

	perms := strings.Replace(testPermissions, "org_roles: [owner, admin, member]", "org_roles: [owner, \"site admin\", member]", 1)

	if _, err := load([]byte(perms), []byte(testOpenAPI)); err == nil || !strings.Contains(err.Error(), "not a lowercase name") {
		t.Errorf("load: %v, want a malformed role refused", err)
	}
}

// Better Auth's roles hold the better_auth actions of the permissions each organization role
// holds: owners those of member:manage and org:delete, admins those of member:manage only,
// members none.
func TestRenderTS_EachRoleHoldsTheActionsOfItsPermissions(t *testing.T) {
	t.Parallel()

	tb, err := load([]byte(testPermissions), []byte(testOpenAPI))
	if err != nil {
		t.Fatal(err)
	}

	out, err := renderTS(tb)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"  invitation: ['create'],\n  member: ['delete', 'update'],\n  organization: ['delete'],\n} as const;",
		"  owner: ac.newRole({ invitation: ['create'], member: ['delete', 'update'], organization: ['delete'] }),",
		"  admin: ac.newRole({ invitation: ['create'], member: ['delete', 'update'] }),",
		"  member: ac.newRole({}),",
		"export const factoryRoles = ['viewer', 'approver', 'editor'] as const;",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("access.gen.ts lacks %q:\n%s", want, out)
		}
	}
}
