package storetest

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// fileTests cover filed items (ADR 0040 decisions 1 and 2).
var fileTests = []struct {
	name string
	test func(f *fixture)
}{
	{"CreateItem_RecordsOriginAndActor", testCreateItemRecordsOrigin},
	{"Assemble_PartsInheritGenerationAndUntrusted", testAssemblePartsInherit},
}

// A filed item keeps its origin, via, filer, generation and untrusted, and its created event and
// item.filed audit entry name the real actor.
func testCreateItemRecordsOrigin(f *fixture) {
	t := f.t

	ada := domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}
	it := f.create(t, domain.NewItem{
		Title: "a", Origin: domain.OriginPerson, Via: domain.ViaCLI, CreatedBy: ada, Source: domain.ItemFromCLI,
	})
	later := f.create(t, domain.NewItem{Title: "b", Origin: domain.OriginFollowUp, Generation: 2, Untrusted: true})

	type filed struct {
		Origin    domain.Origin
		Via       domain.Via
		CreatedBy string
		Source    domain.ItemSource
	}

	got := f.item(t, it.ID)
	if g, want := (filed{got.Origin, got.Via, got.CreatedBy, got.Source}), (filed{domain.OriginPerson, domain.ViaCLI, ada.Ref(), domain.ItemFromCLI}); g != want {
		t.Errorf("item = %+v; want %+v", g, want)
	}

	if got := f.item(t, later.ID); got.Origin != domain.OriginFollowUp || got.Generation != 2 || !got.Untrusted || got.CreatedBy != "" {
		t.Errorf("follow-up = origin %q generation %d untrusted %t by %q; want follow-up, 2, true and no filer",
			got.Origin, got.Generation, got.Untrusted, got.CreatedBy)
	}

	sameEvents(t, f.Events(t, it.ID), []domain.Event{{
		FactoryID: f.factory, ItemID: it.ID, At: t0, Kind: domain.EventCreated, Station: stWork, Actor: ada.Name,
		Data: map[string]any{dataActor: ada.Ref()},
	}})

	e := f.filedEntry(t, it.ID)
	if e.Actor.Ref() != ada.Ref() || e.TargetKind != domain.AuditTargetItem || e.FactoryID != f.factory ||
		e.Data["origin"] != string(domain.OriginPerson) || e.Data["filed_via"] != string(domain.ViaCLI) {
		t.Errorf("item.filed = %+v; want by %s, origin person, filed via cli", e, ada.Ref())
	}
}

// Parts are filed by the planning agent with origin plan, and inherit their assembly's
// generation and untrusted, each with an item.filed entry.
func testAssemblePartsInherit(f *fixture) {
	t := f.t

	it, l := f.planning(t, domain.NewItem{Title: titleBig, Origin: domain.OriginFollowUp, Generation: 1, Untrusted: true})

	agent := domain.Actor{Kind: domain.ActorAgent, ID: agWorker, Name: agWorker}
	a := assembly(it, l, plan())
	a.By = agent

	_, parts := f.assemble(t, a)

	for _, p := range parts.Items {
		got := f.item(t, p.ID)
		if got.Origin != domain.OriginPlan || got.Generation != 1 || !got.Untrusted || got.CreatedBy != agent.Ref() {
			t.Errorf("part %s = origin %q generation %d untrusted %t by %q; want plan, 1, true, %s",
				p.PartKey, got.Origin, got.Generation, got.Untrusted, got.CreatedBy, agent.Ref())
		}

		if events := f.Events(t, p.ID); len(events) == 0 || events[0].Kind != domain.EventPartCreated || events[0].Actor != agWorker ||
			events[0].Data[dataActor] != agent.Ref() {
			t.Errorf("part %s events = %+v; want part_created by %s first", p.PartKey, events, agent.Ref())
		}

		if e := f.filedEntry(t, p.ID); e.Actor.Ref() != agent.Ref() || e.Data["origin"] != string(domain.OriginPlan) {
			t.Errorf("part %s item.filed = %+v; want by %s with origin plan", p.PartKey, e, agent.Ref())
		}
	}
}

// filedEntry is the one item.filed audit entry of item id.
func (f *fixture) filedEntry(t *testing.T, id domain.ItemID) domain.AuditEntry {
	t.Helper()

	entries, err := f.Audit.AuditLog(f.ctx(t), ports.AuditQuery{Action: domain.AuditItemFiled, TargetID: string(id), Limit: 10})
	if err != nil || len(entries) != 1 {
		t.Fatalf("audit log of %s for %s = %+v, %v; want one entry", domain.AuditItemFiled, id, entries, err)
	}

	return entries[0]
}
