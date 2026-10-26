package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const acmeNotes = "/v1/orgs/acme/items/acme%231/notes"

// noteBody is a note request with text.
func noteBody(t *testing.T, text string) string {
	t.Helper()

	b, err := json.Marshal(gen.NewNote{Text: text})
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func TestAPI_NoteGoesToTheNextRun(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := h.submit(t)

	var receipt gen.NoteReceipt
	if code := h.call(t, http.MethodPost, acmeNotes, noteBody(t, "  Also check the connection pool.  "), &receipt); code != http.StatusAccepted {
		t.Fatalf("note = %d, want 202", code)
	}

	if receipt.Delivery != gen.NoteReceiptDeliveryNextRun || receipt.EventId == 0 {
		t.Errorf("receipt %+v, want next_run with the event id", receipt)
	}

	var page gen.EventPage
	if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/acme%231/events", "", &page); code != http.StatusOK {
		t.Fatalf("events = %d", code)
	}

	last := page.Events[len(page.Events)-1]
	want := gen.Event{
		Id: receipt.EventId, ItemId: it.Id, At: last.At, Kind: string(domain.EventNote), Station: last.Station, Actor: domain.LocalUser.Name,
		Data: map[string]any{"text": "Also check the connection pool.", "actor": domain.LocalUser.Ref()},
	}

	if asJSON(t, last) != asJSON(t, want) {
		t.Errorf("last event %s, want the note by the local user, trimmed: %s", asJSON(t, last), asJSON(t, want))
	}

	entries, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Action: domain.AuditItemNoted, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].TargetID != it.Id || asJSON(t, entries[0].Data) != `{"length":31}` {
		t.Errorf("audit %+v, want one item.noted with the length only", entries)
	}
}

func TestAPI_NoteRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.submit(t) // acme#1, queued
	done := h.finished(t, t0, t0.Add(time.Hour), false)

	tests := []struct {
		name, path, body string
		want             int
		code             string
	}{
		{name: "blank", path: acmeNotes, body: noteBody(t, " \n "), want: http.StatusBadRequest},
		{name: "too long", path: acmeNotes, body: noteBody(t, strings.Repeat("a", 2001)), want: http.StatusBadRequest},
		{name: "no text", path: acmeNotes, body: `{}`, want: http.StatusBadRequest},
		{name: "done item", path: "/v1/orgs/acme/items/" + string(done.ID) + "/notes", body: noteBody(t, "late"), want: http.StatusConflict, code: "item-closed"},
		{name: "unknown item", path: "/v1/orgs/acme/items/acme%2399/notes", body: noteBody(t, "hi"), want: http.StatusNotFound, code: "item-not-found"},
	}

	for _, tt := range tests {
		var p gen.Problem
		if code := h.call(t, http.MethodPost, tt.path, tt.body, &p); code != tt.want || deref(p.Code) != tt.code {
			t.Errorf("%s: %d %+v, want %d %q", tt.name, code, p, tt.want, tt.code)
		}
	}
}
