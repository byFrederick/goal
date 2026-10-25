package definitionstest

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// checkVersion fails unless got is want apart from its times, which must be set as got's
// state requires, and its id when want leaves it empty.
func checkVersion(t *testing.T, what string, got, want domain.DefinitionVersion) {
	t.Helper()

	if want.ID == "" {
		want.ID = got.ID
	}

	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() || got.PublishedAt.IsZero() == (got.State == domain.VersionPublished) {
		t.Errorf("%s times: created %v, updated %v, published %v; want them set as a %s version requires",
			what, got.CreatedAt, got.UpdatedAt, got.PublishedAt, got.State)
	}

	if g, w := timeless(got), timeless(want); !reflect.DeepEqual(g, w) {
		t.Errorf("%s = %+v; want %+v", what, g, w)
	}
}

// timeless is v without its times and with empty maps as nil, so every store compares equal.
func timeless(v domain.DefinitionVersion) domain.DefinitionVersion {
	v.CreatedAt, v.UpdatedAt, v.PublishedAt = time.Time{}, time.Time{}, time.Time{}
	v.Manifest = orNil(v.Manifest)
	v.Source.Manifest = orNil(v.Source.Manifest)

	return v
}

// checkRecord fails unless got is want apart from its times: CreatedAt and each version
// reference's At must be set, and PausedAt and ArchivedAt set exactly when want's are.
func checkRecord(t *testing.T, what string, got, want ports.FactoryRecord) {
	t.Helper()

	if got.CreatedAt.IsZero() || got.Paused() != want.Paused() || got.Archived() != want.Archived() ||
		unstamped(got.Current) || unstamped(got.Draft) {
		t.Errorf("%s times: created %v, paused %v, archived %v, current %+v, draft %+v; want them set (paused: %v, archived: %v)",
			what, got.CreatedAt, got.PausedAt, got.ArchivedAt, got.Current, got.Draft, want.Paused(), want.Archived())
	}

	if g, w := untimed(got), untimed(want); !reflect.DeepEqual(g, w) {
		t.Errorf("%s = %s; want %s", what, g, w)
	}
}

func unstamped(r *ports.VersionRef) bool { return r != nil && r.At.IsZero() }

// recordView is a record without its times, its version references by value so it prints.
type recordView struct {
	ID                domain.FactoryID
	OrgID             domain.OrgID
	Name, Title       string
	Current, Draft    *ports.VersionRef
	LastVersionNumber int
	PausedBy, Creator domain.Actor
	ArchivedBy        domain.Actor
}

func untimed(r ports.FactoryRecord) recordView {
	return recordView{
		ID: r.ID, OrgID: r.OrgID, Name: r.Name, Title: r.Title, Current: atless(r.Current), Draft: atless(r.Draft),
		LastVersionNumber: r.LastVersionNumber, PausedBy: r.PausedBy, Creator: r.CreatedBy, ArchivedBy: r.ArchivedBy,
	}
}

func atless(r *ports.VersionRef) *ports.VersionRef {
	if r == nil {
		return nil
	}

	c := *r
	c.At = time.Time{}

	return &c
}

func (v recordView) String() string {
	return fmt.Sprintf("{%s org %s %q %q current %+v draft %+v last v%d paused by %+v created by %+v archived by %+v}",
		v.ID, v.OrgID, v.Name, v.Title, v.Current, v.Draft, v.LastVersionNumber, v.PausedBy, v.Creator, v.ArchivedBy)
}

func orNil[M ~map[K]V, K comparable, V any](m M) M {
	if len(m) == 0 {
		return nil
	}

	return m
}

// paused and running are the PausedAt of a paused and a running factory in a want, and
// archivedSince the ArchivedAt of an archived one; any time that is set will do for them.
var (
	paused        = t0
	running       time.Time
	archivedSince = t0
)

// records returns the organization's factories.
func (f *fixture) records(t *testing.T) []ports.FactoryRecord {
	t.Helper()

	list, err := f.Definitions.FactoryRecords(f.ctx(t))
	if err != nil {
		t.Fatalf("FactoryRecords: %v", err)
	}

	return list
}

// names lists the names of records in order.
func names(records []ports.FactoryRecord) []string {
	out := []string{}
	for _, r := range records {
		out = append(out, r.Name)
	}

	return out
}

// ref is the reference of a version as records list it (At is not compared).
func ref(v domain.DefinitionVersion) *ports.VersionRef {
	if v.State == domain.VersionPublished {
		return &ports.VersionRef{ID: v.ID, Number: v.Number, Revision: v.Revision, By: v.PublishedBy}
	}

	return &ports.VersionRef{ID: v.ID, Revision: v.Revision, By: v.UpdatedBy}
}
