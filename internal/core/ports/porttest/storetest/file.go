package storetest

import (
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// A filed item keeps its origin, via, filer, generation and untrusted, and its created event and
// item.filed audit entry name the real actor (ADR 0040 decisions 1 and 2).
func testCreateItemRecordsOrigin(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

	ada := domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}
	it := f.create(t, domain.NewItem{
		Title: "a", Origin: domain.OriginPerson, Via: domain.ViaCLI, CreatedBy: ada, Source: domain.ItemFromCLI,
	})
	later := f.create(t, domain.NewItem{Title: "b", Origin: domain.OriginFollowUp, Generation: 2, Untrusted: true})

	got := f.item(t, it.ID)
	if got.Origin != domain.OriginPerson || got.Via != domain.ViaCLI || got.CreatedBy != ada.Ref() || got.Source != domain.ItemFromCLI {
		t.Errorf("item = origin %q via %q by %q source %q; want person, cli, %s, cli", got.Origin, got.Via, got.CreatedBy, got.Source, ada.Ref())
	}

	if got := f.item(t, later.ID); got.Origin != domain.OriginFollowUp || got.Generation != 2 || !got.Untrusted || got.CreatedBy != "" {
		t.Errorf("follow-up = origin %q generation %d untrusted %t by %q; want follow-up, 2, true and no filer", got.Origin, got.Generation, got.Untrusted, got.CreatedBy)
	}

	sameEvents(t, f.Events(t, it.ID), []domain.Event{{
		FactoryID: f.factory, ItemID: it.ID, At: t0, Kind: domain.EventCreated, Station: stWork, Actor: ada.Name,
		Data: map[string]any{dataActor: ada.Ref()},
	}})

	entries, err := f.Audit.AuditLog(ctx, ports.AuditQuery{Action: domain.AuditItemFiled, TargetID: string(it.ID), Limit: 10})
	if err != nil || len(entries) != 1 {
		t.Fatalf("audit log of %s = %+v, %v; want one entry", domain.AuditItemFiled, entries, err)
	}

	if e := entries[0]; e.Actor.Ref() != ada.Ref() || e.TargetKind != domain.AuditTargetItem || e.FactoryID != f.factory ||
		e.Data["origin"] != string(domain.OriginPerson) || e.Data["via"] != string(domain.ViaCLI) {
		t.Errorf("item.filed = %+v; want by %s, origin person, via cli", e, ada.Ref())
	}
}

// Parts are filed by the planning agent with origin plan, and inherit their assembly's
// generation and untrusted, each with an item.filed entry.
func testAssemblePartsInherit(f *fixture) {
	t := f.t
	ctx := f.ctx(t)

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

		events := f.Events(t, p.ID)
		if len(events) == 0 || events[0].Kind != domain.EventPartCreated || events[0].Actor != agWorker || events[0].Data[dataActor] != agent.Ref() {
			t.Errorf("part %s events = %+v; want part_created by %s first", p.PartKey, events, agent.Ref())
		}

		entries, err := f.Audit.AuditLog(ctx, ports.AuditQuery{Action: domain.AuditItemFiled, TargetID: string(p.ID), Limit: 10})
		if err != nil || len(entries) != 1 || entries[0].Actor.Ref() != agent.Ref() || entries[0].Data["origin"] != string(domain.OriginPlan) {
			t.Errorf("part %s item.filed = %+v, %v; want one by %s with origin plan", p.PartKey, entries, err, agent.Ref())
		}
	}
}
