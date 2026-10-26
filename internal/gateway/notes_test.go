package gateway

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// appendEvent records an event of kind on item it, by run when run is set, and returns its id.
func (h *harness) appendEvent(t *testing.T, it domain.Item, kind domain.EventKind, run domain.RunID, data map[string]any) int64 {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), h.claims().OrgID)
	if err := h.store.AppendEvents(ctx, domain.Event{
		FactoryID: it.FactoryID, ItemID: it.ID, RunID: run, At: h.clock.Now(), Kind: kind, Station: h.claims().Station, Actor: "ada", Data: data,
	}); err != nil {
		t.Fatal(err)
	}

	h.clock.Advance(time.Second)

	events := h.store.Events(it.ID)

	return events[len(events)-1].ID
}

// note records a person's note on item it and returns its id.
func (h *harness) note(t *testing.T, it domain.Item, text string) int64 {
	t.Helper()

	return h.appendEvent(t, it, domain.EventNote, "", map[string]any{"text": text, "actor": "user:ada"})
}

// fetchNotes asks for claims c's notes after the event id after and decodes them.
func (h *harness) fetchNotes(t *testing.T, url string, c domain.TaskClaims, after int64) []runNote {
	t.Helper()

	status, header, body := request(t, http.MethodGet, fmt.Sprintf("%s?notes_after=%d", url, after), h.token(t, c))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	if ct := header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got struct {
		Notes []runNote `json:"notes"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}

	if got.Notes == nil {
		t.Fatalf("notes = null, want a list (%s)", body)
	}

	return got.Notes
}

func texts(notes []runNote) string {
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.Text)
	}

	return strings.Join(out, ",")
}

// A run is served its item's notes left after its own run_started, oldest first, after the id it
// names: never one left before the run started, on another item, and another run's run_started
// does not move the cutoff. Serving them appends no event.
func TestServeRun_NotesSinceTheRunStarted(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	url := h.runServer(t)
	it := h.submit(t, factoryID, "Write the spec", "")
	other := h.submit(t, factoryID, "Another item", "")
	c := h.claims()
	c.ItemID = it.ID

	if got := h.fetchNotes(t, url, c, 0); len(got) != 0 {
		t.Errorf("before the run started, notes %+v; want none", got)
	}

	h.note(t, it, "before")
	h.appendEvent(t, it, domain.EventRunStarted, "run-0", nil)
	h.note(t, it, "during an earlier run")
	h.appendEvent(t, it, domain.EventRunStarted, c.RunID, nil)
	first := h.note(t, it, "first")
	h.note(t, other, "on another item")
	h.appendEvent(t, it, domain.EventRunStarted, "run-9", nil)
	second := h.note(t, it, "second")
	h.appendEvent(t, it, domain.EventAgentMessage, c.RunID, map[string]any{"text": "not a note"})
	h.note(t, it, "third")

	got := h.fetchNotes(t, url, c, 0)
	if g := texts(got); g != "first,second,third" {
		t.Fatalf("notes %q, want first,second,third", g)
	}

	n := got[0]
	if n.ID != first || n.By != "user:ada" || n.At == "" {
		t.Errorf("first note %+v; want id %d by user:ada", n, first)
	}

	if _, err := time.Parse(time.RFC3339, n.At); err != nil {
		t.Errorf("at %q is not RFC 3339: %v", n.At, err)
	}

	if g := texts(h.fetchNotes(t, url, c, first)); g != "second,third" {
		t.Errorf("after the first, notes %q; want second,third", g)
	}

	if g := texts(h.fetchNotes(t, url, c, second+100)); g != "" {
		t.Errorf("after the last, notes %q; want none", g)
	}

	before := len(h.store.Events(it.ID))
	h.fetchNotes(t, url, c, 0)

	if after := len(h.store.Events(it.ID)); after != before {
		t.Errorf("serving notes appended %d events, want none", after-before)
	}
}

// At most maxNotes are served at a time, the oldest first.
func TestServeRun_NotesAtMostAPage(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	url := h.runServer(t)
	it := h.submit(t, factoryID, "Write the spec", "")
	c := h.claims()
	c.ItemID = it.ID

	h.appendEvent(t, it, domain.EventRunStarted, c.RunID, nil)

	var ids []int64
	for i := range maxNotes + 5 {
		ids = append(ids, h.note(t, it, strconv.Itoa(i)))
	}

	got := h.fetchNotes(t, url, c, 0)
	if len(got) != maxNotes || got[0].ID != ids[0] || got[maxNotes-1].ID != ids[maxNotes-1] {
		t.Fatalf("%d notes from %d; want %d from %d", len(got), got[0].ID, maxNotes, ids[0])
	}

	if rest := h.fetchNotes(t, url, c, got[maxNotes-1].ID); len(rest) != 5 {
		t.Errorf("then %d notes, want 5", len(rest))
	}
}

// The notes answer refuses what the bundle refuses, and a notes_after that is not 0 or a positive
// integer.
func TestServeRun_NotesRefusesWhatTheTokenDoesNotName(t *testing.T) {
	t.Parallel()

	h := newHarness(t, Config{})
	url := h.runServer(t)
	it := h.submit(t, factoryID, "Write the spec", "")
	foreign := h.submit(t, otherID, "Someone else's", "")

	valid := h.claims()
	valid.ItemID = it.ID
	expired := valid
	expired.ExpiresAt = h.clock.Now().Add(-time.Second).Unix()
	stranger := valid
	stranger.Agent = "stranger"
	otherItem := valid
	otherItem.ItemID = foreign.ID
	noItem := valid
	noItem.ItemID = "item-gone"
	otherFactorys := valid
	otherFactorys.VersionID = otherVersion
	otherOrg := valid
	otherOrg.OrgID = "01900000-0000-7000-8000-00000000beef"

	tests := []struct {
		name   string
		method string
		query  string
		token  string
		status int
	}{
		{name: "no token", status: http.StatusUnauthorized},
		{name: "garbage", token: "not-a-token", status: http.StatusUnauthorized},
		{name: "expired", token: h.token(t, expired), status: http.StatusUnauthorized},
		{name: "another audience", token: h.tokenFor(t, valid, "goal-api"), status: http.StatusUnauthorized},
		{name: "agent not in the definition", token: h.token(t, stranger), status: http.StatusForbidden},
		{name: "another factory's item", token: h.token(t, otherItem), status: http.StatusForbidden},
		{name: "no such item", token: h.token(t, noItem), status: http.StatusForbidden},
		{name: "another factory's version", token: h.token(t, otherFactorys), status: http.StatusForbidden},
		{name: "another organization's item", token: h.token(t, otherOrg), status: http.StatusForbidden},
		{name: "negative", query: "-1", token: h.token(t, valid), status: http.StatusBadRequest},
		{name: "not a number", query: "x", token: h.token(t, valid), status: http.StatusBadRequest},
		{name: "empty", query: "", token: h.token(t, valid), status: http.StatusBadRequest},
		{name: "not a GET", method: http.MethodPost, token: h.token(t, valid), status: http.StatusMethodNotAllowed},
		{name: "valid", token: h.token(t, valid), status: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := tt.query
			if q == "" && tt.name != "empty" {
				q = "0"
			}

			status, _, body := request(t, cmp.Or(tt.method, http.MethodGet), url+"?notes_after="+q, tt.token)
			if status != tt.status {
				t.Fatalf("status = %d, want %d (%s)", status, tt.status, body)
			}

			if s := strings.TrimSpace(string(body)); (tt.status == http.StatusUnauthorized || tt.status == http.StatusForbidden) &&
				s != "unauthorized" && s != "forbidden" {
				t.Errorf("body = %q, want no detail", s)
			}
		})
	}
}
