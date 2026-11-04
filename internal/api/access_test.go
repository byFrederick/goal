package api_test

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
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

// slackRow is the name of the organization-wide row of the Slack notifications' secret.
const slackRow = "Slack notifications"

// slackAccess is h's Access answer to query once acme publishes accessEnvYAML and the Slack
// notifications use acme-token.
func slackAccess(t *testing.T) (*harness, func(query string) gen.Access) {
	t.Helper()

	h := newHarness(t)
	h.publishAccess(t)

	if err := h.notifySettings.SetNotificationSettings(inOrg(t), domain.NotificationSettings{SlackSecret: tokenName}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	return h, func(query string) gen.Access {
		t.Helper()

		return decode[gen.Access](t, h.request(t, http.MethodGet, accessPath+query, "", nil), http.StatusOK)
	}
}

// lastRow is the last group's last row, where the slack row sorts.
func lastRow(t *testing.T, a gen.Access) gen.AccessRow {
	t.Helper()

	if len(a.Groups) == 0 {
		t.Fatal("no groups")
	}

	rows := a.Groups[len(a.Groups)-1].Rows

	return rows[len(rows)-1]
}

// Without a Slack secret, Access has no slack row.
func TestAccess_SlackNotificationsUnset(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishAccess(t)

	got := decode[gen.Access](t, h.request(t, http.MethodGet, accessPath, "", nil), http.StatusOK)
	if g := groupsOf(got); g != "mcp=docs,linear,playwright env=ACME_KEY,OLD_KEY file=~/.acme/key" || got.Reached != 6 {
		t.Errorf("unset = %s (%d reached); want no slack row", g, got.Reached)
	}
}

// The organization's Slack notifications show as one row of kind slack, holding the setting's
// secret and no factory use: in its own by=reach group and under its secret by=secret, counted
// in reached and need_attention.
func TestAccess_SlackNotifications(t *testing.T) {
	t.Parallel()

	h, get := slackAccess(t)

	if raw := h.request(t, http.MethodGet, accessPath, "", nil); strings.Contains(string(raw.body), secretValue) {
		t.Errorf("access = %s; want no secret value", raw.body)
	}

	got := get("")
	if head := fmt.Sprintf("%d %d %s", got.Reached, got.NeedAttention, groupsOf(got)); head != "7 2 mcp=docs,linear,playwright env=ACME_KEY,OLD_KEY file=~/.acme/key slack="+slackRow {
		t.Errorf("reached, need_attention, groups = %s", head)
	}

	expiry := time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)
	if r := lastRow(t, got); r.Kind != gen.AccessRowKindSlack || strings.Join(r.Secrets, ",") != tokenName ||
		r.Uses == nil || len(r.Uses) != 0 || r.Status != gen.AccessStatusOk || r.ExpiresAt == nil || !r.ExpiresAt.Equal(expiry) {
		t.Errorf("slack row = %s", body(t, r))
	}

	if g := groupsOf(get("?by=secret")); g != tokenName+"=linear,ACME_KEY,~/.acme/key,"+slackRow+" gone-key=OLD_KEY" {
		t.Errorf("by secret = %s", g)
	}
}

// The slack row belongs to no factory: no by=factory group and no one factory's Access holds it.
func TestAccess_SlackNotificationsNotByFactory(t *testing.T) {
	t.Parallel()

	_, get := slackAccess(t)

	if g := groupsOf(get("?by=factory")); strings.Contains(g, slackRow) {
		t.Errorf("by factory = %s; want no slack row", g)
	}

	for _, query := range []string{"?factory=acme", "?factory=acme&by=secret"} {
		if one := get(query); strings.Contains(groupsOf(one), slackRow) || one.Reached != 6 {
			t.Errorf("%s = %s (%d reached); want no slack row", query, groupsOf(one), one.Reached)
		}
	}
}

// The slack row's status comes from its secret alone: expiring within the secret's reminder
// days, then expired.
func TestAccess_SlackNotificationsExpiry(t *testing.T) {
	t.Parallel()

	h, get := slackAccess(t)

	// acme-token expires on 2027-01-31 with a reminder 14 days before.
	h.clock.Advance(time.Date(2027, 1, 20, 0, 0, 0, 0, time.UTC).Sub(h.clock.Now()))

	if r := lastRow(t, get("")); r.Status != gen.AccessStatusExpiring {
		t.Errorf("slack 11 days before its secret's expiry = %s; want expiring", body(t, r))
	}

	h.clock.Advance(12 * 24 * time.Hour)

	if r := lastRow(t, get("")); r.Status != gen.AccessStatusExpired {
		t.Errorf("slack after its secret's expiry = %s; want expired", body(t, r))
	}
}

// A setting that names a secret the organization no longer holds shows its row as missing. The
// secrets refuse to delete the setting's secret, so the setting here keeps its own secrets.
func TestAccess_SlackNotificationsMissingSecret(t *testing.T) {
	t.Parallel()

	stale := porttest.NewOrgSecrets(porttest.NewMasterKeys().Key("k1"), newTenancy(t), nil)
	settings := porttest.NewNotificationSettings(stale, newTenancy(t))

	if _, err := stale.Create(inOrg(t), domain.NewSecret{Name: "slack-bot", Value: "xoxb-test-token"}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if err := settings.SetNotificationSettings(inOrg(t), domain.NotificationSettings{SlackSecret: "slack-bot"}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	h := newHarnessLogged(t, nil, nil, func(_ *harness, d *api.Deps) { d.NotificationSettings = settings })

	got := decode[gen.Access](t, h.request(t, http.MethodGet, accessPath, "", nil), http.StatusOK)
	if g := groupsOf(got); g != "slack="+slackRow {
		t.Fatalf("groups = %s", g)
	}

	if r := got.Groups[0].Rows[0]; r.Status != gen.AccessStatusMissing || got.NeedAttention != 1 || strings.Join(r.Secrets, ",") != "slack-bot" {
		t.Errorf("slack = %s (%d need attention); want missing slack-bot", body(t, r), got.NeedAttention)
	}
}
