// Package notificationsettingtest is the conformance suite for ports.NotificationSettings (ADR
// 0037 decision 14). The in-memory fake in porttest and the PostgreSQL adapter run it, so tests on
// the fake hold for the database's rules: Slack off until a setting names a secret, a name that is
// no secret refused, one audit entry per change holding the name and never the value, the setting
// following the secret's renames, the secret not deleted while the setting uses it, and
// organizations kept apart.
package notificationsettingtest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Harness is one NotificationSettings under test.
type Harness struct {
	Settings ports.NotificationSettings
	// Secrets are the secrets the settings name, in the same store.
	Secrets ports.OrgSecrets
	Audit   ports.Audit
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
}

// Run runs the suite. Tests run in parallel and call newHarness once each; harnesses must be
// isolated from each other.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(f *fixture)
	}{
		{"Get_NothingSavedIsOff", testDefault},
		{"Set_ThenGetRoundTripsAndClears", testRoundTrip},
		{"Set_UnknownSecretNotFound", testUnknown},
		{"Set_AuditedOncePerChange", testAudit},
		{"Rename_KeepsTheSetting", testRename},
		{"Delete_UsedSecretIsAConflict", testDelete},
		{"OtherOrg_IsOff", testIsolation},
		{"Methods_NoOrgIsAnError", testNoOrg},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			f := &fixture{Harness: h, t: t}
			f.org = h.NewOrg(t)
			tc.test(f)
		})
	}
}

// The secret the suite names, and its value, which no error or audit entry may hold.
const (
	slackBot = "slack-bot"
	value    = "xoxb-notification-setting-value-5e1d"
)

var admin = domain.Actor{Kind: domain.ActorUser, ID: "u-admin", Name: "Ada Admin"}

type fixture struct {
	Harness

	t   *testing.T
	org domain.OrgID
}

func (f *fixture) in(org domain.OrgID) context.Context { return domain.WithOrg(f.t.Context(), org) }

func (f *fixture) secret(org domain.OrgID, name string) {
	f.t.Helper()

	n := domain.NewSecret{Name: name, Description: "Slack bot token", Scope: domain.SecretScope{All: true}, Value: value}
	if _, err := f.Secrets.Create(f.in(org), n, admin); err != nil {
		f.t.Fatalf("Create secret %s: %v", name, err)
	}
}

func (f *fixture) get(org domain.OrgID) domain.NotificationSettings {
	f.t.Helper()

	s, err := f.Settings.NotificationSettings(f.in(org))
	if err != nil {
		f.t.Fatalf("NotificationSettings: %v", err)
	}

	return s
}

func (f *fixture) set(slack string) {
	f.t.Helper()

	if err := f.Settings.SetNotificationSettings(f.in(f.org), domain.NotificationSettings{SlackSecret: slack}, admin); err != nil {
		f.t.Fatalf("SetNotificationSettings(%q): %v", slack, err)
	}
}

func (f *fixture) audit(org domain.OrgID) []domain.AuditEntry {
	f.t.Helper()

	got, err := f.Audit.AuditLog(f.in(org), ports.AuditQuery{Limit: 100, Action: domain.AuditOrgNotificationsUpdated})
	if err != nil {
		f.t.Fatal(err)
	}

	return got
}

func (f *fixture) expect(ok bool, format string, args ...any) {
	f.t.Helper()

	if !ok {
		f.t.Errorf(format, args...)
	}
}

// refused fails unless err is want, and when err holds the secret's value.
func (f *fixture) refused(what string, err, want error) {
	f.t.Helper()

	if !errors.Is(err, want) {
		f.t.Errorf("%s = %v; want %v", what, err, want)
	}

	if err != nil && strings.Contains(err.Error(), value) {
		f.t.Errorf("%s's error holds the secret's value", what)
	}
}

func testDefault(f *fixture) {
	f.expect(f.get(f.org) == domain.NotificationSettings{}, "an organization without settings has Slack on: %+v", f.get(f.org))
}

func testRoundTrip(f *fixture) {
	f.secret(f.org, slackBot)
	f.set(slackBot)

	if got := f.get(f.org); got.SlackSecret != slackBot {
		f.t.Fatalf("settings = %+v; want Slack with %s", got, slackBot)
	}

	f.set("")
	f.expect(f.get(f.org) == domain.NotificationSettings{}, "cleared settings = %+v; want Slack off", f.get(f.org))
}

func testUnknown(f *fixture) {
	f.secret(f.org, slackBot)
	f.set(slackBot)

	err := f.Settings.SetNotificationSettings(f.in(f.org), domain.NotificationSettings{SlackSecret: "no-such-secret"}, admin)
	f.refused("SetNotificationSettings of no secret", err, ports.ErrNotFound)
	f.expect(f.get(f.org).SlackSecret == slackBot, "a refused set changed the settings: %+v", f.get(f.org))
	f.expect(len(f.audit(f.org)) == 1, "a refused set was audited: %+v", f.audit(f.org))
}

func testAudit(f *fixture) {
	f.secret(f.org, slackBot)
	f.set(slackBot)
	f.set(slackBot)
	f.set("")
	f.set("")

	got := f.audit(f.org)
	if len(got) != 2 {
		f.t.Fatalf("audit = %+v; want one entry for the set and one for the clear, none for the repeats", got)
	}

	// Newest first.
	for i, want := range []string{"", slackBot} {
		e := got[i]
		f.expect(e.Actor.Ref() == admin.Ref() && e.TargetKind == domain.AuditTargetOrg && e.TargetID == string(f.org),
			"entry %d = %+v; want the admin's, on the organization", i, e)
		f.expect(e.Data[domain.AuditSlackSecret] == want, "entry %d's Slack secret = %v; want %q", i, e.Data[domain.AuditSlackSecret], want)
		f.expect(!strings.Contains(fmt.Sprint(e.Data), value), "entry %d holds the secret's value", i)
	}
}

func testRename(f *fixture) {
	f.secret(f.org, slackBot)
	f.set(slackBot)

	renamed := "slack-bot-2"
	if _, err := f.Secrets.Patch(f.in(f.org), slackBot, domain.SecretPatch{Name: &renamed}, admin); err != nil {
		f.t.Fatalf("rename the secret: %v", err)
	}

	f.expect(f.get(f.org).SlackSecret == renamed, "settings after the rename = %+v; want %s", f.get(f.org), renamed)
}

func testDelete(f *fixture) {
	f.secret(f.org, slackBot)
	f.set(slackBot)

	err := f.Secrets.Delete(f.in(f.org), slackBot, admin)
	f.refused("Delete of the Slack secret", err, ports.ErrConflict)
	f.expect(err != nil && strings.Contains(err.Error(), "the Slack notification setting"),
		"the conflict does not name the Slack notification setting: %v", err)

	_, err = f.Secrets.Get(f.in(f.org), slackBot)
	f.expect(err == nil, "a refused Delete deleted the secret: %v", err)
	f.expect(f.get(f.org).SlackSecret == slackBot, "a refused Delete changed the settings: %+v", f.get(f.org))

	f.set("")
	f.expect(f.Secrets.Delete(f.in(f.org), slackBot, admin) == nil, "Delete of a secret the setting no longer uses was refused")
}

func testIsolation(f *fixture) {
	labs := f.NewOrg(f.t)
	f.secret(f.org, slackBot)
	f.set(slackBot)

	f.expect(f.get(labs) == domain.NotificationSettings{}, "another org's settings = %+v; want Slack off", f.get(labs))
	f.expect(len(f.audit(labs)) == 0, "another org sees the audit entry")

	err := f.Settings.SetNotificationSettings(f.in(labs), domain.NotificationSettings{SlackSecret: slackBot}, admin)
	f.refused("another org's set of this org's secret", err, ports.ErrNotFound)
}

func testNoOrg(f *fixture) {
	ctx := f.t.Context()

	_, err := f.Settings.NotificationSettings(ctx)
	f.refused("NotificationSettings without an org", err, domain.ErrNoOrg)
	f.refused("SetNotificationSettings without an org", f.Settings.SetNotificationSettings(ctx, domain.NotificationSettings{}, admin), domain.ErrNoOrg)
}
