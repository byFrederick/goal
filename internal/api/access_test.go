package api_test

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// accessPath is the Access read model's route.
const accessPath = "/v1/orgs/acme/access"

// accessEnvYAML is the environment acme publishes for the Access tests: a remote server with a
// key, one without, a stdio server, variables with a secret, a missing one and none, and a file.
const accessEnvYAML = `apiVersion: goal/v1
kind: Environment
spec:
  env:
    ACME_KEY: "{{acme-token}}"
    OLD_KEY: "{{gone-key}}"
    ACME_URL: https://api.example.com
  files:
    ~/.acme/key: "{{acme-token}}"
  mcp:
    linear:
      url: ` + linearURL + `
      headers: {Authorization: "Bearer {{acme-token}}"}
    docs:
      url: ` + docsURL + `
    playwright:
      command: [npx, "@playwright/mcp@0.0.41"]
`

// publishAccess publishes acme with accessEnvYAML and tests its servers: linear passes, docs fails.
func (h *harness) publishAccess(t *testing.T) {
	t.Helper()

	h.createSecret(t)
	h.draftEnvironment(t, accessEnvYAML)
	h.publish(t, "Add access")

	if _, err := h.versions.Refresh(t.Context()); err != nil { // as the definitions cache does on a change
		t.Fatal(err)
	}

	h.tools.Serve(linearURL, "create_issue", "list_issues")
	h.tools.Fail(docsURL, errors.New("connection refused"))
	decode[gen.EnvironmentTestResult](t, h.request(t, http.MethodPost, envTestPath, `{"version":"current"}`, nil), http.StatusOK)
}

// rowNames are the rows' names.
func rowNames(rows []gen.AccessRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Name)
	}

	return out
}

// row is the row named name.
func row(t *testing.T, rows []gen.AccessRow, name string) gen.AccessRow {
	t.Helper()

	i := slices.IndexFunc(rows, func(r gen.AccessRow) bool { return r.Name == name })
	if i < 0 {
		t.Fatalf("no row %s in %v", name, rowNames(rows))
	}

	return rows[i]
}

// groupsOf is each group's key and row names: key=name,name key=….
func groupsOf(a gen.Access) string {
	parts := make([]string, 0, len(a.Groups))
	for _, g := range a.Groups {
		parts = append(parts, g.Key+"="+strings.Join(rowNames(g.Rows), ","))
	}

	return strings.Join(parts, " ")
}

// usesOf is a row's uses, secrets and status: factory/environment agents secrets status(tools).
func usesOf(r gen.AccessRow) string {
	parts := make([]string, 0, len(r.Uses))
	for _, u := range r.Uses {
		tools := -1
		if u.Test != nil {
			tools = u.Test.Tools
		}

		parts = append(parts, fmt.Sprintf("%s/%s %s %s %s(%d)", u.Factory.Name, u.Environment, strings.Join(u.Agents, ","),
			strings.Join(r.Secrets, ","), u.Status, tools))
	}

	return strings.Join(parts, "; ")
}

// By what agents reach, the rows are MCP servers (each), then variables and files that reference
// a secret, each with the factory, environment and agents using it and its secrets; by factory
// and by secret, the same rows group under each factory and each secret.
func TestAccess_Grouping(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishAccess(t)

	got := decode[gen.Access](t, h.request(t, http.MethodGet, accessPath, "", nil), http.StatusOK)
	if head := fmt.Sprintf("%s %d %d", got.By, got.Reached, got.NeedAttention); head != "reach 6 2" {
		t.Errorf("access = %s; want by reach, 6 reached, 2 needing attention", head)
	}

	if g := groupsOf(got); g != "mcp=docs,linear,playwright env=ACME_KEY,OLD_KEY file=~/.acme/key" {
		t.Errorf("groups = %s", g)
	}

	linear := row(t, got.Groups[0].Rows, serverLinear)
	if u := usesOf(linear); u != "acme/default writer acme-token tested(2)" || linear.TestedAt == nil {
		t.Errorf("linear = %s", body(t, linear))
	}

	byFactory := decode[gen.Access](t, h.request(t, http.MethodGet, accessPath+"?by=factory", "", nil), http.StatusOK)
	if g := groupsOf(byFactory); g != string(acmeID)+"=docs,linear,playwright,ACME_KEY,OLD_KEY,~/.acme/key" || byFactory.Groups[0].Title != acmeSlug {
		t.Errorf("by factory = %s", g)
	}

	bySecret := decode[gen.Access](t, h.request(t, http.MethodGet, accessPath+"?by=secret", "", nil), http.StatusOK)
	if g := groupsOf(bySecret); g != "acme-token=linear,ACME_KEY,~/.acme/key gone-key=OLD_KEY" {
		t.Errorf("by secret = %s", g)
	}

	refused(t, "an unknown grouping", h.request(t, http.MethodGet, accessPath+"?by=team", "", nil), http.StatusBadRequest, "")
	refused(t, "an unknown factory", h.request(t, http.MethodGet, accessPath+"?factory=nope", "", nil), http.StatusNotFound, "factory-not-found")
}

// Each row's status: a missing secret, a failed test, a tested or untested remote server, ok;
// then, as the clock nears the secret's expiry, expiring within its reminder days and expired.
func TestAccess_Status(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishAccess(t)

	status := func(group int, name string) gen.AccessStatus {
		t.Helper()

		got := decode[gen.Access](t, h.request(t, http.MethodGet, accessPath+"?factory=acme", "", nil), http.StatusOK)

		return row(t, got.Groups[group].Rows, name).Status
	}

	for _, tc := range []struct {
		group int
		name  string
		want  gen.AccessStatus
	}{
		{0, serverDocs, gen.AccessStatusFailed},
		{0, serverLinear, gen.AccessStatusTested},
		{0, "playwright", gen.AccessStatusOk},
		{1, "OLD_KEY", gen.AccessStatusMissing},
		{1, envAcmeKey, gen.AccessStatusOk},
	} {
		if got := status(tc.group, tc.name); got != tc.want {
			t.Errorf("%s = %s; want %s", tc.name, got, tc.want)
		}
	}

	got := decode[gen.Access](t, h.request(t, http.MethodGet, accessPath, "", nil), http.StatusOK)
	if docs := row(t, got.Groups[0].Rows, serverDocs); valueOf(docs.TestError) == "" {
		t.Errorf("docs = %s; want its test's error", body(t, docs))
	}

	// acme-token expires on 2027-01-31 with a reminder 14 days before.
	h.clock.Advance(time.Date(2027, 1, 20, 0, 0, 0, 0, time.UTC).Sub(h.clock.Now()))

	if got := status(1, envAcmeKey); got != gen.AccessStatusExpiring {
		t.Errorf("ACME_KEY 11 days before its expiry = %s; want expiring", got)
	}

	h.clock.Advance(12 * 24 * time.Hour)

	if got := status(1, envAcmeKey); got != gen.AccessStatusExpired {
		t.Errorf("ACME_KEY after its expiry = %s; want expired", got)
	}
}
