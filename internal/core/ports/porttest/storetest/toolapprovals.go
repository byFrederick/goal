package storetest

import (
	"reflect"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// testOpenToolApprovals counts the open tool approvals of the organization by connection, as
// domain.OpenToolApproval defines them: the latest request on a waiting_human item with no
// decision and no withdrawal after it.
func testOpenToolApprovals(f *fixture) {
	t := f.t

	// Two open approvals on github and one on docs.
	f.toolApproval(t, f.create(t, domain.NewItem{State: domain.StateWaitingHuman}), "github")
	f.toolApproval(t, f.create(t, domain.NewItem{State: domain.StateWaitingHuman}), "github")
	f.toolApproval(t, f.create(t, domain.NewItem{State: domain.StateWaitingHuman}), "docs")

	// A granted, a denied and a withdrawn approval are no longer open.
	for _, after := range []domain.EventKind{domain.EventToolApprovalGranted, domain.EventToolApprovalDenied, domain.EventMoved} {
		it := f.create(t, domain.NewItem{State: domain.StateWaitingHuman})
		f.toolApproval(t, it, "github")
		f.event(t, domain.Event{FactoryID: it.FactoryID, ItemID: it.ID, At: t1, Kind: after, Station: stWork, Actor: domain.LocalUser.Ref()})
	}

	// A request on an item that no longer waits for a person counts for nothing.
	f.toolApproval(t, f.create(t, domain.NewItem{State: domain.StateQueued}), "github")

	// A request asked again after a denial is open again.
	again := f.create(t, domain.NewItem{State: domain.StateWaitingHuman})
	f.toolApproval(t, again, "docs")
	f.event(t, domain.Event{FactoryID: again.FactoryID, ItemID: again.ID, At: t1, Kind: domain.EventToolApprovalDenied, Station: stWork, Actor: domain.LocalUser.Ref()})
	f.toolApproval(t, again, "docs")

	// Another organization's open approval is never counted.
	other := f.otherOrg(t)
	other.toolApproval(t, other.create(t, domain.NewItem{State: domain.StateWaitingHuman}), "github")

	got, err := f.Store.OpenToolApprovals(f.ctx(t))
	if err != nil {
		t.Fatal(err)
	}

	if want := map[string]int{"github": 2, "docs": 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("OpenToolApprovals = %v; want %v", got, want)
	}

	if got, err := other.Store.OpenToolApprovals(other.ctx(t)); err != nil || !reflect.DeepEqual(got, map[string]int{"github": 1}) {
		t.Errorf("the other organization's OpenToolApprovals = %v, %v; want map[github:1]", got, err)
	}
}

// toolApproval has the worker ask a person about a call of connection's create_comment on it.
func (f *fixture) toolApproval(t *testing.T, it domain.Item, connection string) {
	t.Helper()

	f.event(t, domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, At: t1, Kind: domain.EventToolApprovalRequested, Station: stWork, Actor: agWorker,
		Data: map[string]any{domain.KeyConnection: connection, domain.KeyTool: "create_comment", "input": map[string]any{"body": "hi"}},
	})
}
