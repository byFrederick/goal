// Package environmenttesttest is the conformance suite for ports.EnvironmentTests (ADR 0037
// decision 9). The in-memory fake in porttest and the PostgreSQL adapter run it, so tests on the
// fake hold for the database's rules: one latest test per factory, environment and server, a
// factory the organization has, and organizations kept apart.
package environmenttesttest

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Harness is one EnvironmentTests under test.
type Harness struct {
	Tests ports.EnvironmentTests
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
	// NewFactory returns a new factory of org.
	NewFactory func(t *testing.T, org domain.OrgID) domain.FactoryID
}

// The environment and the server most of the suite's tests are of.
const (
	defaultEnv = "default"
	linear     = "linear"
)

// at is the time of the suite's tests.
var at = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)

// Run runs the suite; each test calls newHarness once.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	for _, tc := range []struct {
		name string
		test func(t *testing.T, h Harness)
	}{
		{"Record_LatestReplaces", testLatestReplaces},
		{"Record_KeepsReadOnly", testKeepsReadOnly},
		{"Record_InvalidRefused", testInvalid},
		{"Record_UnknownFactoryNotFound", testUnknownFactory},
		{"List_ByFactory", testListByFactory},
		{"OtherOrg_SeesNothing", testIsolation},
		{"Methods_NoOrgIsAnError", testNoOrg},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.test(t, newHarness(t))
		})
	}
}

func testLatestReplaces(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	f := h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	first := domain.EnvironmentTest{Factory: f, Environment: defaultEnv, Server: linear, At: at, OK: true, Tools: []string{"create_issue", "list_issues"}}
	if err := h.Tests.Record(ctx, first); err != nil {
		t.Fatal(err)
	}

	second := domain.EnvironmentTest{Factory: f, Environment: defaultEnv, Server: linear, At: at.Add(time.Hour), Error: "POST https://mcp.example.com/mcp answered 401"}
	if err := h.Tests.Record(ctx, second); err != nil {
		t.Fatal(err)
	}

	got, err := h.Tests.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].OK || got[0].Error != second.Error || !got[0].At.Equal(second.At) || len(got[0].Tools) != 0 {
		t.Errorf("List = %+v; want only the second test", got)
	}
}

func testKeepsReadOnly(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	f := h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	first := domain.EnvironmentTest{
		Factory: f, Environment: defaultEnv, Server: linear, At: at, OK: true,
		Tools: []string{"create_issue", "get_issue", "list_issues"}, ReadOnly: []string{"get_issue", "list_issues"},
	}
	if err := h.Tests.Record(ctx, first); err != nil {
		t.Fatal(err)
	}

	got, err := h.Tests.List(ctx, f)
	if err != nil || len(got) != 1 || !slices.Equal(got[0].ReadOnly, first.ReadOnly) {
		t.Fatalf("List = %+v, %v; want the read-only tools %v", got, err, first.ReadOnly)
	}

	second := domain.EnvironmentTest{Factory: f, Environment: defaultEnv, Server: linear, At: at.Add(time.Hour), OK: true, Tools: []string{"create_issue"}}
	if err := h.Tests.Record(ctx, second); err != nil {
		t.Fatal(err)
	}

	got, err = h.Tests.List(ctx, f)
	if err != nil || len(got) != 1 || len(got[0].ReadOnly) != 0 {
		t.Errorf("List after a test without read-only tools = %+v, %v; want none kept", got, err)
	}
}

func testInvalid(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	f := h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	for _, bad := range []domain.EnvironmentTest{
		{Factory: f, Environment: defaultEnv, Server: linear, At: at},                       // failed without an error
		{Factory: f, Environment: defaultEnv, Server: linear, At: at, OK: true, Error: "x"}, // passed with one
		{Factory: f, Environment: "Default", Server: linear, At: at, OK: true},
		{Factory: f, Environment: defaultEnv, Server: linear, OK: true},
		{Factory: f, Environment: defaultEnv, Server: linear, At: at, OK: true, Tools: []string{"a"}, ReadOnly: []string{"b"}},
	} {
		if err := h.Tests.Record(ctx, bad); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Record(%+v) = %v; want ErrInvalid", bad, err)
		}
	}
}

func testUnknownFactory(t *testing.T, h Harness) {
	t.Helper()

	org, other := h.NewOrg(t), h.NewOrg(t)
	theirs := h.NewFactory(t, other)

	err := h.Tests.Record(domain.WithOrg(t.Context(), org),
		domain.EnvironmentTest{Factory: theirs, Environment: defaultEnv, Server: linear, At: at, OK: true})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Record of another organization's factory = %v; want ErrNotFound", err)
	}
}

func testListByFactory(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	a, b := h.NewFactory(t, org), h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	for _, r := range []domain.EnvironmentTest{
		{Factory: a, Environment: defaultEnv, Server: linear, At: at, OK: true},
		{Factory: a, Environment: "review", Server: linear, At: at, OK: true},
		{Factory: b, Environment: defaultEnv, Server: "sentry", At: at, OK: true},
	} {
		if err := h.Tests.Record(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	all, err := h.Tests.List(ctx, "")
	if err != nil || len(all) != 3 {
		t.Fatalf("List = %+v, %v; want 3", all, err)
	}

	only, err := h.Tests.List(ctx, a)
	if err != nil {
		t.Fatal(err)
	}

	envs := []string{}
	for _, r := range only {
		envs = append(envs, r.Environment)
	}

	if !slices.Equal(envs, []string{defaultEnv, "review"}) {
		t.Errorf("List(a) environments = %v; want default, review", envs)
	}
}

func testIsolation(t *testing.T, h Harness) {
	t.Helper()

	org, other := h.NewOrg(t), h.NewOrg(t)
	f := h.NewFactory(t, org)

	if err := h.Tests.Record(domain.WithOrg(t.Context(), org),
		domain.EnvironmentTest{Factory: f, Environment: defaultEnv, Server: linear, At: at, OK: true}); err != nil {
		t.Fatal(err)
	}

	got, err := h.Tests.List(domain.WithOrg(t.Context(), other), "")
	if err != nil || len(got) != 0 {
		t.Errorf("List of another organization = %+v, %v; want none", got, err)
	}
}

func testNoOrg(t *testing.T, h Harness) {
	t.Helper()

	if _, err := h.Tests.List(t.Context(), ""); err == nil {
		t.Error("List without an organization succeeded")
	}

	if err := h.Tests.Record(t.Context(), domain.EnvironmentTest{}); err == nil {
		t.Error("Record without an organization succeeded")
	}
}
