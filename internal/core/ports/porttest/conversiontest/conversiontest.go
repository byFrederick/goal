// Package conversiontest is the conformance suite for ports.FactoryConversions (ADR 0037
// decision 19). The in-memory fake in porttest and the PostgreSQL adapter run it, so tests on the
// fake hold for the database's rules: one conversion per factory, kept, a factory the
// organization has, a refusal replaced by a later try and a conversion kept for good,
// and organizations kept apart.
package conversiontest

import (
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Harness is one FactoryConversions under test.
type Harness struct {
	Conversions ports.FactoryConversions
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
	// NewFactory returns a new factory of org.
	NewFactory func(t *testing.T, org domain.OrgID) domain.FactoryID
}

// The versions of the suite's conversions.
const (
	v1 = domain.DefinitionVersionID("0b6f1c9e-0000-7000-8000-000000000101")
	v2 = domain.DefinitionVersionID("0b6f1c9e-0000-7000-8000-000000000102")
)

var at = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)

// Run runs the suite; each test calls newHarness once.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	for _, tc := range []struct {
		name string
		test func(t *testing.T, h Harness)
	}{
		{"Record_OncePerFactory", testOnce},
		{"Record_RefusedWithProblems", testRefused},
		{"Record_RefusedReplaced", testRefusedReplaced},
		{"Record_InvalidRefused", testInvalid},
		{"Record_UnknownFactoryNotFound", testUnknownFactory},
		{"OtherOrg_SeesNothing", testIsolation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.test(t, newHarness(t))
		})
	}
}

func testOnce(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	f := h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	first := ports.FactoryConversion{Factory: f, From: v1, To: v2, Names: map[string]map[string]string{"github": {"token": "github"}}, At: at}
	if ok, err := h.Conversions.Record(ctx, first); err != nil || !ok {
		t.Fatalf("Record = %v, %v; want recorded", ok, err)
	}

	again := ports.FactoryConversion{Factory: f, From: v2, Problems: []string{"later"}, At: at.Add(time.Hour)}
	if ok, err := h.Conversions.Record(ctx, again); err != nil || ok {
		t.Fatalf("second Record = %v, %v; want false: the first stays", ok, err)
	}

	got, err := h.Conversions.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].From != v1 || got[0].To != v2 || len(got[0].Problems) != 0 || !got[0].At.Equal(at) ||
		!maps.Equal(got[0].Names["github"], first.Names["github"]) {
		t.Errorf("List = %+v; want the first conversion only", got)
	}
}

func testRefused(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	f := h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	problems := []string{"connection aws-prod uses the aws kind", "connection models uses the bedrock kind"}
	if ok, err := h.Conversions.Record(ctx, ports.FactoryConversion{Factory: f, From: v1, Problems: problems, At: at}); err != nil || !ok {
		t.Fatalf("Record = %v, %v", ok, err)
	}

	got, err := h.Conversions.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || !got[0].Refused() || !slices.Equal(got[0].Problems, problems) || len(got[0].Names) != 0 {
		t.Errorf("List = %+v; want the refusal with its problems", got)
	}
}

func testRefusedReplaced(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	f := h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	for i, c := range []ports.FactoryConversion{
		{Factory: f, From: v1, Problems: []string{"the token can't push"}, At: at},
		{Factory: f, From: v1, Problems: []string{"MCP server docs did not answer"}, At: at.Add(time.Hour)},
		{Factory: f, From: v1, To: v2, Names: map[string]map[string]string{"gitlab": {"token": "gitlab"}}, At: at.Add(2 * time.Hour)},
	} {
		if ok, err := h.Conversions.Record(ctx, c); err != nil || !ok {
			t.Fatalf("Record %d = %v, %v; want it to replace the refusal", i, ok, err)
		}
	}

	if ok, err := h.Conversions.Record(ctx, ports.FactoryConversion{Factory: f, From: v2, Problems: []string{"later"}, At: at.Add(3 * time.Hour)}); err != nil || ok {
		t.Fatalf("Record after the conversion = %v, %v; want false: a conversion is kept", ok, err)
	}

	got, err := h.Conversions.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].Refused() || got[0].To != v2 || len(got[0].Problems) != 0 || !got[0].At.Equal(at.Add(2*time.Hour)) ||
		got[0].Names["gitlab"]["token"] != "gitlab" {
		t.Errorf("List = %+v; want the conversion that replaced the refusals", got)
	}
}

func testInvalid(t *testing.T, h Harness) {
	t.Helper()

	org := h.NewOrg(t)
	f := h.NewFactory(t, org)
	ctx := domain.WithOrg(t.Context(), org)

	for _, c := range []ports.FactoryConversion{
		{Factory: f, From: v1, At: at},                                     // refused without a problem
		{Factory: f, From: v1, To: v2, Problems: []string{"both"}, At: at}, // converted with one
	} {
		if _, err := h.Conversions.Record(ctx, c); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Record(%+v) = %v; want ErrInvalid", c, err)
		}
	}
}

func testUnknownFactory(t *testing.T, h Harness) {
	t.Helper()

	org, other := h.NewOrg(t), h.NewOrg(t)
	f := h.NewFactory(t, other)

	_, err := h.Conversions.Record(domain.WithOrg(t.Context(), org), ports.FactoryConversion{Factory: f, From: v1, To: v2, At: at})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Record of another organization's factory = %v; want ErrNotFound", err)
	}
}

func testIsolation(t *testing.T, h Harness) {
	t.Helper()

	org, other := h.NewOrg(t), h.NewOrg(t)
	f := h.NewFactory(t, org)

	if _, err := h.Conversions.Record(domain.WithOrg(t.Context(), org), ports.FactoryConversion{Factory: f, From: v1, To: v2, At: at}); err != nil {
		t.Fatal(err)
	}

	got, err := h.Conversions.List(domain.WithOrg(t.Context(), other))
	if err != nil || len(got) != 0 {
		t.Errorf("the other organization's List = %+v, %v; want none", got, err)
	}

	if _, err := h.Conversions.List(t.Context()); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("List without an organization = %v; want ErrNoOrg", err)
	}
}
