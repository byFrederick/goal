// Package repositorytesttest is the conformance suite for ports.RepositoryTests (ADR 0041
// decision 8). The in-memory fake in porttest and the PostgreSQL adapter run it, so tests on the
// fake hold for the database's rules: one latest test per factory and URL, a factory the
// organization has, organizations kept apart and tests that go with their factory.
package repositorytesttest

import (
	"errors"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Harness is one RepositoryTests under test.
type Harness struct {
	Tests ports.RepositoryTests
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
	// NewFactory returns a new factory of org.
	NewFactory func(t *testing.T, org domain.OrgID) domain.FactoryID
	// DeleteFactory deletes factory of org, as goal_delete_factory does.
	DeleteFactory func(t *testing.T, org domain.OrgID, factory domain.FactoryID)
}

// The repositories and the secret most of the suite's tests are of.
const (
	app    = "github.com/acme/app"
	owner  = "github.com/acme/*"
	secret = "repo-token"
)

// at is the time of the suite's tests.
var at = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

// Run runs the suite; each test calls newHarness once.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	for _, tc := range []struct {
		name string
		test func(t *testing.T, h Harness)
	}{
		{"Record_LatestReplaces", testLatestReplaces},
		{"Record_KeepsOwnerCounts", testOwnerCounts},
		{"Record_KeepsCICheck", testCICheck},
		{"Record_InvalidRefused", testInvalid},
		{"Record_UnknownFactoryNotFound", testUnknownFactory},
		{"List_ByFactory", testListByFactory},
		{"BySecret_AcrossFactories", testBySecret},
		{"DeleteFactory_TakesItsTests", testDeleteFactory},
		{"OtherOrg_SeesNothing", testIsolation},
		{"Methods_NoOrgIsAnError", testNoOrg},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.test(t, newHarness(t))
		})
	}
}

// passed is a passing read test of url for factory.
func passed(factory domain.FactoryID, url string) domain.RepositoryTest {
	return domain.RepositoryTest{
		Factory: factory, URL: url, Access: domain.RepositoryTestRead, Secret: secret, At: at, Tested: true, OK: true,
		Summary: "Can read", ReadOK: true, ReadReason: "GitHub shows acme/app to the token", PushReason: "no push",
	}
}

func testLatestReplaces(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	f := h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	if err := h.Tests.Record(ctx, passed(f, app)); err != nil {
		t.Fatal(err)
	}

	second := domain.RepositoryTest{
		Factory: f, URL: app, Access: domain.RepositoryTestWrite, Secret: "other-token", At: at.Add(time.Hour), Tested: true,
		Summary: "Can read but can't push", ReadOK: true, ReadReason: "shown", PushReason: "no push",
	}
	if err := h.Tests.Record(ctx, second); err != nil {
		t.Fatal(err)
	}

	got, err := h.Tests.List(ctx, f)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0] != second {
		t.Errorf("List = %+v; want only %+v", got, second)
	}
}

func testOwnerCounts(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	f := h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	want := domain.RepositoryTest{
		Factory: f, URL: owner, Access: domain.RepositoryTestRead, Secret: secret, At: at, Tested: true, OK: true,
		Summary: "Can read at least 1000 repositories in acme", Warning: "Goal won't push, but agents could. Use a read-only token.",
		ReadOK: true, ReadReason: "listed", PushOK: true, PushReason: "listed", Readable: 1000, Pushable: 3, Truncated: true,
	}
	if err := h.Tests.Record(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, err := h.Tests.List(ctx, f)
	if err != nil || len(got) != 1 || got[0] != want {
		t.Errorf("List = %+v, %v; want %+v", got, err, want)
	}
}

func testCICheck(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	f := h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	failing := passed(f, app)
	failing.CITested, failing.CIReason = true, "the token has no read access to Checks and Commit statuses"

	passing := passed(f, "github.com/acme/web")
	passing.CITested, passing.CIOK, passing.CIReason = true, true, "GitHub shows the checks of acme/web"

	// A test without a CI check reads back without one, not as a failed one.
	absent := passed(f, owner)

	for _, r := range []domain.RepositoryTest{failing, passing, absent} {
		if err := h.Tests.Record(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	got, err := h.Tests.List(ctx, f)
	if err != nil {
		t.Fatal(err)
	}

	if want := []domain.RepositoryTest{absent, failing, passing}; len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("List = %+v; want %+v", got, want)
	}
}

func testInvalid(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	f := h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	untestedOK := passed(f, app)
	untestedOK.Tested = false

	badAccess := passed(f, app)
	badAccess.Access = "admin"

	noTime := passed(f, app)
	noTime.At = time.Time{}

	negative := passed(f, owner)
	negative.Readable = -1

	untestedCI := passed(f, app)
	untestedCI.CIOK = true

	for _, bad := range []domain.RepositoryTest{untestedOK, badAccess, noTime, negative, untestedCI, passed(f, "")} {
		if err := h.Tests.Record(ctx, bad); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Record(%+v) = %v; want ErrInvalid", bad, err)
		}
	}
}

func testUnknownFactory(t *testing.T, h Harness) {
	t.Helper()

	org, other := h.NewOrg(t), h.NewOrg(t)
	theirs := h.NewFactory(t, other)

	if err := h.Tests.Record(domain.WithOrg(t.Context(), org), passed(theirs, app)); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Record of another organization's factory = %v; want ErrNotFound", err)
	}
}

func testListByFactory(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	a, b := h.NewFactory(t, org), h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	for _, r := range []domain.RepositoryTest{passed(a, owner), passed(a, app), passed(b, "github.com/acme/web")} {
		if err := h.Tests.Record(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	got, err := h.Tests.List(ctx, a)
	if err != nil {
		t.Fatal(err)
	}

	// By url in byte order: * sorts before letters.
	if len(got) != 2 || got[0].URL != owner || got[1].URL != app {
		t.Errorf("List(a) = %+v; want %s then %s", got, owner, app)
	}
}

func testBySecret(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	a, b := h.NewFactory(t, org), h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	other := passed(a, owner)
	other.Secret = "other-token"

	for _, r := range []domain.RepositoryTest{passed(a, app), other, passed(b, app)} {
		if err := h.Tests.Record(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	got, err := h.Tests.BySecret(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].URL != app || got[1].URL != app || got[0].Factory == got[1].Factory {
		t.Errorf("BySecret = %+v; want %s of both factories", got, app)
	}
}

func testDeleteFactory(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	f, kept := h.NewFactory(t, org), h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	for _, r := range []domain.RepositoryTest{passed(f, app), passed(kept, app)} {
		if err := h.Tests.Record(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	h.DeleteFactory(t, org, f)

	if got, err := h.Tests.BySecret(ctx, secret); err != nil || len(got) != 1 || got[0].Factory != kept {
		t.Errorf("BySecret after deleting a factory = %+v, %v; want only the other factory's", got, err)
	}
}

func testIsolation(t *testing.T, h Harness) {
	t.Helper()

	org, other := h.NewOrg(t), h.NewOrg(t)
	f := h.NewFactory(t, org)

	if err := h.Tests.Record(domain.WithOrg(t.Context(), org), passed(f, app)); err != nil {
		t.Fatal(err)
	}

	octx := domain.WithOrg(t.Context(), other)

	if got, err := h.Tests.List(octx, f); err != nil || len(got) != 0 {
		t.Errorf("List of another organization = %+v, %v; want none", got, err)
	}

	if got, err := h.Tests.BySecret(octx, secret); err != nil || len(got) != 0 {
		t.Errorf("BySecret of another organization = %+v, %v; want none", got, err)
	}
}

func testNoOrg(t *testing.T, h Harness) {
	t.Helper()

	if _, err := h.Tests.List(t.Context(), ""); err == nil {
		t.Error("List without an organization succeeded")
	}

	if _, err := h.Tests.BySecret(t.Context(), secret); err == nil {
		t.Error("BySecret without an organization succeeded")
	}

	if err := h.Tests.Record(t.Context(), domain.RepositoryTest{}); err == nil {
		t.Error("Record without an organization succeeded")
	}
}
