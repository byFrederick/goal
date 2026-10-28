package storetest

import (
	"reflect"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// People who own views, and ids that name no view or factory.
const (
	viewerA domain.UserID = "00000000-0000-4000-8000-0000000000a1"
	viewerB domain.UserID = "00000000-0000-4000-8000-0000000000b2"

	missingView    domain.SavedViewID = "00000000-0000-7000-8000-fffffffffffd"
	missingFactory domain.FactoryID   = "00000000-0000-7000-8000-fffffffffffc"
)

// View names the tests use more than once.
const (
	viewAlpha = "Alpha"
	viewOmega = "omega"
	viewMine  = "mine"
	// filterNew is a filter value of the views.
	filterNew = "new-this-week"
)

// saveView creates a view of the fixture's factory unless v names another, at t0 unless it says.
func (f *fixture) saveView(t *testing.T, v domain.SavedView) domain.SavedView {
	t.Helper()

	if v.Factory == "" {
		v.Factory = f.factory
	}

	if v.CreatedAt.IsZero() {
		v.CreatedAt = t0
	}

	got, err := f.Store.CreateSavedView(f.ctx(t), v)
	if err != nil {
		t.Fatalf("CreateSavedView %q: %v", v.Name, err)
	}

	return got
}

// viewNames lists the names of the views viewer sees on factory, in the store's order.
func (f *fixture) viewNames(t *testing.T, factory domain.FactoryID, viewer domain.UserID) []string {
	t.Helper()

	views, err := f.Store.ListSavedViews(f.ctx(t), factory, viewer)
	if err != nil {
		t.Fatalf("ListSavedViews: %v", err)
	}

	names := []string{}
	for _, v := range views {
		names = append(names, v.Name)
	}

	return names
}

func sameView(t *testing.T, what string, got, want domain.SavedView) {
	t.Helper()

	got.CreatedAt, got.UpdatedAt = got.CreatedAt.UTC(), got.UpdatedAt.UTC()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s =\n  %+v\nwant\n  %+v", what, got, want)
	}
}

// A viewer sees the factory's shared views, then their own personal ones, each by name with
// case ignored; never another person's personal views or another factory's.
func testSavedViewsListOrderAndVisibility(f *fixture) {
	t := f.t

	mine := f.saveView(t, domain.SavedView{Owner: viewerA, Name: "  zeta ", Filters: map[string][]string{"state": {filterNew, "blocked"}, "q": nil}})
	f.saveView(t, domain.SavedView{Owner: viewerA, Name: "Beta"})
	f.saveView(t, domain.SavedView{Owner: viewerA, Name: viewOmega, Shared: true})
	f.saveView(t, domain.SavedView{Owner: viewerB, Name: viewAlpha, Shared: true})
	f.saveView(t, domain.SavedView{Owner: viewerB, Name: "aaa"})
	f.saveView(t, domain.SavedView{Factory: f.newFactory(t), Owner: viewerA, Name: "elsewhere", Shared: true})

	want := domain.SavedView{
		ID: mine.ID, Factory: f.factory, Owner: viewerA, Name: "zeta",
		Filters: map[string][]string{"state": {filterNew, "blocked"}, "q": {}}, CreatedAt: t0, UpdatedAt: t0,
	}
	if mine.ID == "" {
		t.Error("CreateSavedView returned no id")
	}

	sameView(t, "CreateSavedView", mine, want)

	views, err := f.Store.ListSavedViews(f.ctx(t), f.factory, viewerA)
	if err != nil {
		t.Fatal(err)
	}

	if len(views) != 4 {
		t.Fatalf("ListSavedViews(A) = %+v; want 4 views", views)
	}

	sameView(t, "ListSavedViews(A)[3]", views[3], want)

	for _, tc := range []struct {
		viewer domain.UserID
		want   []string
	}{
		{viewerA, []string{viewAlpha, viewOmega, "Beta", "zeta"}},
		{viewerB, []string{viewAlpha, viewOmega, "aaa"}},
		{"", []string{viewAlpha, viewOmega}},
	} {
		if got := f.viewNames(t, f.factory, tc.viewer); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ListSavedViews(%q) = %q; want %q", tc.viewer, got, tc.want)
		}
	}

	if got := f.viewNames(t, missingFactory, viewerA); len(got) != 0 {
		t.Errorf("ListSavedViews(unknown factory) = %q; want none", got)
	}

	if got := f.viewNames(t, "not-a-uuid", viewerA); len(got) != 0 {
		t.Errorf("ListSavedViews(malformed factory) = %q; want none", got)
	}
}

// A personal view's name is unique for its owner and a shared view's within its factory, case
// ignored, on create and on rename; a personal and a shared view may share a name.
func testSavedViewsNameTaken(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	f.saveView(t, domain.SavedView{Owner: viewerA, Name: "Mine"})
	f.saveView(t, domain.SavedView{Owner: viewerB, Name: "MINE"})
	f.saveView(t, domain.SavedView{Owner: viewerA, Name: viewMine, Shared: true})
	f.saveView(t, domain.SavedView{Factory: f.newFactory(t), Owner: viewerA, Name: viewMine})
	f.saveView(t, domain.SavedView{Factory: f.newFactory(t), Owner: viewerA, Name: viewMine, Shared: true})

	_, err := f.Store.CreateSavedView(ctx, domain.SavedView{Factory: f.factory, Owner: viewerA, Name: "mINE", CreatedAt: t0})
	expect(t, "personal name of the same owner", err, domain.ErrSavedViewNameTaken)

	_, err = f.Store.CreateSavedView(ctx, domain.SavedView{Factory: f.factory, Owner: viewerB, Name: " Mine ", Shared: true, CreatedAt: t0})
	expect(t, "shared name of the same factory", err, domain.ErrSavedViewNameTaken)

	other := f.saveView(t, domain.SavedView{Owner: viewerA, Name: "spare"})
	_, err = f.Store.UpdateSavedView(ctx, f.factory, other.ID, "MINE", nil, t1)
	expect(t, "rename to a personal name taken", err, domain.ErrSavedViewNameTaken)

	shared := f.saveView(t, domain.SavedView{Owner: viewerB, Name: "team", Shared: true})
	_, err = f.Store.UpdateSavedView(ctx, f.factory, shared.ID, "Mine", nil, t1)
	expect(t, "rename to a shared name taken", err, domain.ErrSavedViewNameTaken)

	if _, err := f.Store.UpdateSavedView(ctx, f.factory, shared.ID, "TEAM", nil, t1); err != nil {
		t.Errorf("renaming a view to its own name in another case: %v", err)
	}

	if got, want := f.viewNames(t, f.factory, viewerA), []string{viewMine, "TEAM", "Mine", "spare"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ListSavedViews(A) = %q; want %q", got, want)
	}
}

// An update renames a view and replaces its filters; a delete removes it; unknown ids, views of
// another factory and invalid views are refused and change nothing.
func testSavedViewsUpdateAndDelete(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	v := f.saveView(t, domain.SavedView{Owner: viewerA, Name: viewMine, Filters: map[string][]string{"state": {filterNew}}})

	got, err := f.Store.UpdateSavedView(ctx, f.factory, v.ID, " renamed ", map[string][]string{"lane": {"backend"}}, t1)
	if err != nil {
		t.Fatal(err)
	}

	want := domain.SavedView{
		ID: v.ID, Factory: f.factory, Owner: viewerA, Name: "renamed",
		Filters: map[string][]string{"lane": {"backend"}}, CreatedAt: t0, UpdatedAt: t1,
	}
	sameView(t, "UpdateSavedView", got, want)

	views, err := f.Store.ListSavedViews(ctx, f.factory, viewerA)
	if err != nil {
		t.Fatal(err)
	}

	if len(views) != 1 {
		t.Fatalf("ListSavedViews = %+v; want the renamed view", views)
	}

	sameView(t, "ListSavedViews after the update", views[0], want)

	_, err = f.Store.UpdateSavedView(ctx, f.factory, v.ID, "", nil, t1)
	expect(t, "update to an empty name", err, domain.ErrInvalid)

	_, err = f.Store.UpdateSavedView(ctx, f.factory, missingView, "x", nil, t1)
	expect(t, "update of an unknown view", err, ports.ErrNotFound)

	_, err = f.Store.UpdateSavedView(ctx, f.factory, "not-a-uuid", "x", nil, t1)
	expect(t, "update of a malformed id", err, ports.ErrNotFound)

	elsewhere := f.newFactory(t)
	_, err = f.Store.UpdateSavedView(ctx, elsewhere, v.ID, "x", nil, t1)
	expect(t, "update through another factory", err, ports.ErrNotFound)
	expect(t, "delete through another factory", f.Store.DeleteSavedView(ctx, elsewhere, v.ID), ports.ErrNotFound)

	_, err = f.Store.CreateSavedView(ctx, domain.SavedView{Factory: f.factory, Owner: viewerA, Name: " ", CreatedAt: t0})
	expect(t, "create with a blank name", err, domain.ErrInvalid)

	_, err = f.Store.CreateSavedView(ctx, domain.SavedView{Factory: missingFactory, Owner: viewerA, Name: "x", CreatedAt: t0})
	expect(t, "create on an unknown factory", err, ports.ErrNotFound)

	if err := f.Store.DeleteSavedView(ctx, f.factory, v.ID); err != nil {
		t.Fatal(err)
	}

	if got := f.viewNames(t, f.factory, viewerA); len(got) != 0 {
		t.Errorf("ListSavedViews after the delete = %q; want none", got)
	}

	expect(t, "delete of a deleted view", f.Store.DeleteSavedView(ctx, f.factory, v.ID), ports.ErrNotFound)
	expect(t, "delete of a malformed id", f.Store.DeleteSavedView(ctx, f.factory, "not-a-uuid"), ports.ErrNotFound)
}

// Another organization neither sees nor changes a view, nor creates one on the factory.
func testSavedViewsOtherOrgSeesNothing(f *fixture) {
	t := f.t
	v := f.saveView(t, domain.SavedView{Owner: viewerA, Name: "team", Shared: true})
	other := f.otherOrg(t)
	ctx := other.ctx(t)

	if got := other.viewNames(t, f.factory, viewerA); len(got) != 0 {
		t.Errorf("another organization's ListSavedViews = %q; want none", got)
	}

	_, err := f.Store.UpdateSavedView(ctx, f.factory, v.ID, "x", nil, t1)
	expect(t, "another organization's update", err, ports.ErrNotFound)
	expect(t, "another organization's delete", f.Store.DeleteSavedView(ctx, f.factory, v.ID), ports.ErrNotFound)

	_, err = f.Store.CreateSavedView(ctx, domain.SavedView{Factory: f.factory, Owner: viewerA, Name: "x", CreatedAt: t0})
	expect(t, "another organization's create", err, ports.ErrNotFound)

	_, err = f.Store.ListSavedViews(t.Context(), f.factory, viewerA)
	expect(t, "ListSavedViews without an organization", err, domain.ErrNoOrg)

	if got := f.viewNames(t, f.factory, viewerA); len(got) != 1 {
		t.Errorf("ListSavedViews = %q; want the view unchanged", got)
	}
}
