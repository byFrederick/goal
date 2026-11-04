package engine_test

import (
	"errors"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// Submit files a person's item through domain.File: the line's first type by default, at the
// first station, recording how and by whom it was filed, with a created event by that person.
func TestSubmit_FilesAPersonsItem(t *testing.T) {
	t.Parallel()

	l := lineOf(agentStation(stWork, agWorker, 1, 1))
	l.WorkTypes = []string{typeFeature, "fix"}
	h := newHarness(t, l, porttest.Succeed(`{}`))

	ada := domain.Actor{Kind: domain.ActorUser, ID: "ada", Name: "Ada"}

	it, err := h.e.Submit(inOrg(t), domain.FileRequest{
		FactoryID: factoryID, Origin: domain.OriginPerson, Via: domain.ViaCLI, By: ada, Title: "  " + anItem + " ",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := h.item(t, it.ID)
	if got.Type != typeFeature || got.Title != anItem || got.Station != l.Stations[0].ID || got.Origin != domain.OriginPerson ||
		got.Via != domain.ViaCLI || got.CreatedBy != ada.Ref() || got.CreatedAt.IsZero() {
		t.Errorf("item = %+v; want a feature at %s filed by %s through the CLI", got, l.Stations[0].ID, ada.Ref())
	}

	created := h.eventsOf(it.ID, domain.EventCreated)
	if len(created) != 1 || created[0].Actor != ada.Name || created[0].Data["actor"] != ada.Ref() {
		t.Errorf("created events = %+v; want one by %s", created, ada.Ref())
	}
}

// What the line does not take is refused with domain.ErrInvalid, and nothing is filed.
func TestSubmit_RefusesWhatTheLineDoesNotTake(t *testing.T) {
	t.Parallel()

	l := lineOf(agentStation(stWork, agWorker, 1, 1))
	l.WorkTypes = []string{typeFeature}
	h := newHarness(t, l, porttest.Succeed(`{}`))

	for name, r := range map[string]domain.FileRequest{
		"an unknown type":   {Origin: domain.OriginPerson, Title: anItem, Type: "chore"},
		"a blank title":     {Origin: domain.OriginPerson, Title: "  "},
		"no origin":         {Title: anItem},
		"via on a trigger":  {Origin: domain.OriginTrigger, Via: domain.ViaWeb, Title: anItem},
		"a trigger station": {Origin: domain.OriginTrigger, Title: anItem, Station: "nowhere"},
	} {
		r.FactoryID = factoryID
		if _, err := h.e.Submit(inOrg(t), r); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("submit %s: %v, want domain.ErrInvalid", name, err)
		}
	}

	if items, err := h.store.Waiting(inOrg(t), factoryID, stWork, 10); err != nil || len(items) != 0 {
		t.Errorf("waiting items = %+v, %v; want none filed", items, err)
	}
}
