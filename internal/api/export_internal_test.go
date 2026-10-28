package api

import "testing"

// An export needs exactly the permission of the list it exports (ADR 0020, decision 3).
func TestExportScopesAreTheLists(t *testing.T) {
	t.Parallel()

	for export, list := range map[string]string{"exportRuns": "listRuns", "exportSpend": "getOrgSpend"} {
		if got, want := operationScopes[export], operationScopes[list]; got == "" || got != want {
			t.Errorf("%s needs %q; want %q, as %s", export, got, want, list)
		}
	}
}
