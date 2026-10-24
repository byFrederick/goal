package api

import (
	"os"
	"testing"

	"go.yaml.in/yaml/v3"
)

// awaitingOperations are the permissions of api/permissions.yaml whose operations later phase 3
// PRs add (spec 3, §8.2 and §15). Remove a permission here in the PR that gives it an operation;
// the test fails while one is both listed and used.
var awaitingOperations = map[string]string{
	"device:read":     "3-13: getDeviceRequest",
	"platform:read":   "3-15: listPlatformAdmins, listAuthEvents",
	"platform:manage": "3-15: addPlatformAdmin, removePlatformAdmin",
	"org:transfer":    "3-11: transferOwnership",
	"member:read":     "3-11: listMembers",
	"invitation:read": "3-11: listInvitations",
	"member:manage":   "3-11: setFactoryRole, removeFactoryRole",
	"api_key:read":    "3-12: listApiKeys",
	"api_key:manage":  "3-12: createApiKey, revokeApiKey",
}

// contractScopes reads every operationId of api/openapi.yaml with its x-goal-scope ("" for none).
func contractScopes(t *testing.T) map[string]string {
	t.Helper()

	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}

	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	scopes := map[string]string{}

	for _, ops := range doc.Paths {
		for _, op := range ops {
			m, ok := op.(map[string]any)
			if !ok {
				continue
			}

			if id, ok := m["operationId"].(string); ok {
				scope, _ := m["x-goal-scope"].(string)
				scopes[id] = scope
			}
		}
	}

	return scopes
}

func TestPermissions_EveryOperationHasAKnownScope(t *testing.T) {
	t.Parallel()

	contract := contractScopes(t)

	for id, scope := range contract {
		switch _, known := permissionRules[scope]; {
		case scope == "":
			t.Errorf("operation %s has no x-goal-scope: give it one of api/permissions.yaml", id)
		case !known:
			t.Errorf("operation %s: x-goal-scope %q is not in api/permissions.yaml", id, scope)
		case operationScopes[id] != scope:
			t.Errorf("operation %s: generated scope %q, contract %q: run task api:gen", id, operationScopes[id], scope)
		}
	}

	for id := range operationScopes {
		if _, ok := contract[id]; !ok {
			t.Errorf("generated operation %s is not in the contract: run task api:gen", id)
		}
	}
}

func TestPermissions_EveryPermissionIsUsed(t *testing.T) {
	t.Parallel()

	used := map[string]bool{}
	for _, scope := range contractScopes(t) {
		used[scope] = true
	}

	for name := range permissionRules {
		pr, awaiting := awaitingOperations[name]

		switch {
		case used[name] && awaiting:
			t.Errorf("permission %s has an operation now: remove it from awaitingOperations (%s)", name, pr)
		case !used[name] && !awaiting:
			t.Errorf("permission %s is used by no operation: name it in an x-goal-scope or remove it", name)
		}
	}

	for name := range awaitingOperations {
		if _, ok := permissionRules[name]; !ok {
			t.Errorf("awaitingOperations names %s, which api/permissions.yaml does not have", name)
		}
	}
}
