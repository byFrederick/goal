package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

func TestParseInterspersed_FlagsAnywhere(t *testing.T) {
	t.Parallel()

	fs := newFlags("run")
	typ := fs.String("type", "feature", "")

	pos, err := parseInterspersed(fs, []string{"goal", "--type", "fix", "Add a version command", "--type=infra"})
	if err != nil || !slices.Equal(pos, []string{"goal", "Add a version command"}) || *typ != "infra" {
		t.Errorf("parse = %v, %v, type %s; want both positionals and the last --type", pos, err, *typ)
	}

	if _, err := parseInterspersed(newFlags("run"), []string{"goal", "--nope"}); !errors.Is(err, errUsage) {
		t.Errorf("unknown flag = %v, want a usage error", err)
	}
}

func TestDecide_RejectNeedsAReasonAndApproveNone(t *testing.T) {
	t.Parallel()

	env := testEnv(t, "http://127.0.0.1:1", nil) // never reached

	for _, args := range [][]string{
		{"reject", item1}, {"approve", item1, "--reason", "x"}, {"approve"}, {"approve", item1, "--by", "pat"},
	} {
		if err := decide(t.Context(), &printer{w: &bytes.Buffer{}}, env, args[0], args[1:]); !errors.Is(err, errUsage) {
			t.Errorf("goal %v = %v, want a usage error", args, err)
		}
	}
}

// The items the note test leaves notes on: one open, one done.
const (
	notedRef  = "goal#1"
	closedRef = "goal#2"
)

func TestNote_SendsTheTextToTheItem(t *testing.T) {
	t.Parallel()

	var (
		gotItem string
		gotNote gen.NewNote
	)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orgs/acme/items/{item}/notes", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("item") == closedRef {
			detail := "item is done or cancelled"

			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(gen.Problem{Title: "Conflict", Status: http.StatusConflict, Detail: &detail})

			return
		}

		gotItem = r.PathValue("item")
		if err := json.NewDecoder(r.Body).Decode(&gotNote); err != nil {
			t.Error(err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(gen.NoteReceipt{EventId: 42, Delivery: gen.NextRun})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	env := testEnv(t, srv.URL, map[string]string{envOrg: orgAcme})

	var buf bytes.Buffer
	if err := run(t.Context(), []string{verbNote, notedRef, "Check the connection pool."}, &printer{w: &buf}, env, nil); err != nil {
		t.Fatal(err)
	}

	if gotItem != notedRef || gotNote.Text != "Check the connection pool." {
		t.Errorf("goald got a note on %q: %+v", gotItem, gotNote)
	}

	if got := buf.String(); got != "✓ note left on "+notedRef+"; the next run reads it\n" {
		t.Errorf("output %q", got)
	}

	err := run(t.Context(), []string{verbNote, closedRef, "late"}, &printer{w: &bytes.Buffer{}}, env, nil)
	if err == nil || !strings.Contains(err.Error(), "item is done or cancelled") {
		t.Errorf("note on a done item = %v, want goald's refusal", err)
	}

	for _, args := range [][]string{{verbNote}, {verbNote, notedRef}, {verbNote, notedRef, "  "}, {verbNote, notedRef, "a", "b"}} {
		if err := run(t.Context(), args, &printer{w: &bytes.Buffer{}}, env, nil); !errors.Is(err, errUsage) {
			t.Errorf("goal %v = %v, want a usage error", args, err)
		}
	}
}

// item1 is the id of the item fakeGoald plays.
const item1 = "item-1"

// fakeGoald plays an item that reaches the approve station after a few polls.
func fakeGoald(t *testing.T) *httptest.Server {
	t.Helper()

	var polls atomic.Int64

	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	station := func(s string) *string { return &s }
	events := []gen.Event{
		{Id: 1, Kind: "created", Station: station("inbox"), Data: map[string]any{}, At: at},
		{
			Id: 2, Kind: "run_finished", Station: station("spec"), Actor: "architect", At: at,
			Data: map[string]any{"status": "succeeded", "cost_usd": 0.42, "turns": 7},
		},
		{Id: 3, Kind: "usage", Station: station("spec"), Data: map[string]any{"input_tokens": 10}, At: at},
		{Id: 4, Kind: "moved", Station: station("spec"), Data: map[string]any{"from": "spec", "to": "approve-spec"}, At: at},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orgs/acme/items/{id}", func(w http.ResponseWriter, _ *http.Request) {
		state := gen.ItemState("in_progress")
		if polls.Add(1) >= 3 {
			state = "waiting_human"
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gen.Item{Id: item1, Ref: "goal#1", Number: 1, State: state, Station: "approve-spec", CostUsd: 0.42})
	})
	mux.HandleFunc("GET /v1/orgs/acme/items/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		// One new event per poll, as if they were happening.
		visible := min(int(polls.Load())+1, len(events))
		page := gen.EventPage{Events: []gen.Event{}, NextAfter: after}

		for _, e := range events[:visible] {
			if e.Id > after {
				page.Events = append(page.Events, e)
				page.NextAfter = e.Id
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(page)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestFollow_PrintsEachEventOnceUntilAHumanMustAct(t *testing.T) {
	t.Parallel()

	srv := fakeGoald(t)

	c, err := connect(t.Context(), testEnv(t, srv.URL, map[string]string{envOrg: orgAcme}), "")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := follow(t.Context(), &printer{w: &buf}, c, item1, 0, time.Millisecond); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	for _, want := range []string{"created", "run succeeded ($0.42, 7 turns)", "moved spec → approve-spec", "goal#1 is waiting_human at approve-spec", "goal approve goal#1"} {
		if strings.Count(got, want) != 1 {
			t.Errorf("output has %q %d times, want once:\n%s", want, strings.Count(got, want), got)
		}
	}

	if strings.Contains(got, "usage") {
		t.Errorf("output shows token usage events:\n%s", got)
	}
}

// Stations the control tests move items to; goald refuses a move to a done station.
const (
	stationSpec = "spec"
	stationDone = "done"
)

// controlGoald records the move and pause requests it answers, refusing a move to a done station.
type controlGoald struct {
	mu     sync.Mutex
	called []string
	bodies []map[string]any
}

func (g *controlGoald) handler(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any

		_ = json.NewDecoder(r.Body).Decode(&b)

		g.mu.Lock()
		g.called, g.bodies = append(g.called, action+" "+r.PathValue("item")), append(g.bodies, b)
		g.mu.Unlock()

		if b["to"] == stationDone {
			detail := "an item reaches a done station by approval or merge, not by a move"

			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(gen.Problem{Title: "Conflict", Status: http.StatusConflict, Detail: &detail})

			return
		}

		it := gen.Item{Ref: notedRef, State: gen.ItemStateQueued, Station: stationSpec}
		if action == verbPause {
			it.State, it.Station = gen.ItemStateBlocked, "build"
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(it)
	}
}

func TestControl_MoveAndPauseSendTheirRequests(t *testing.T) {
	t.Parallel()

	g := &controlGoald{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orgs/acme/items/{item}/move", g.handler(verbMove))
	mux.HandleFunc("POST /v1/orgs/acme/items/{item}/pause", g.handler(verbPause))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	env := testEnv(t, srv.URL, map[string]string{envOrg: orgAcme})

	var out bytes.Buffer
	for _, args := range [][]string{
		{verbMove, notedRef, stationSpec, "--reason", "needs a spec"},
		{verbPause, notedRef},
		{verbPause, notedRef, "--reason", "vendor outage"},
	} {
		if err := run(t.Context(), args, &printer{w: &out}, env, nil); err != nil {
			t.Fatalf("goal %v: %v", args, err)
		}
	}

	paused := "✓ paused goal#1; now blocked at build (goal retry goal#1 resumes it)\n"
	if want := "✓ moved goal#1; now queued at spec\n" + paused + paused; out.String() != want {
		t.Errorf("output %q; want %q", out.String(), want)
	}

	if !slices.Equal(g.called, []string{"move goal#1", "pause goal#1", "pause goal#1"}) || g.bodies[0]["to"] != stationSpec ||
		g.bodies[0]["reason"] != "needs a spec" || g.bodies[1]["reason"] != nil || g.bodies[2]["reason"] != "vendor outage" {
		t.Errorf("goald got %v with %v", g.called, g.bodies)
	}

	if err := run(t.Context(), []string{verbMove, notedRef, stationDone}, &printer{w: &bytes.Buffer{}}, env, nil); err == nil ||
		!strings.Contains(err.Error(), "by approval or merge") {
		t.Errorf("a refused move = %v; want goald's refusal", err)
	}
}

func TestControl_UsageErrors(t *testing.T) {
	t.Parallel()

	env := testEnv(t, "http://127.0.0.1:1", nil) // never reached

	for _, args := range [][]string{{verbMove}, {verbMove, notedRef}, {verbMove, notedRef, "a", "b"}, {verbPause}, {verbPause, notedRef, "x"}} {
		if err := run(t.Context(), args, &printer{w: &bytes.Buffer{}}, env, nil); !errors.Is(err, errUsage) {
			t.Errorf("goal %v = %v; want a usage error", args, err)
		}
	}
}
