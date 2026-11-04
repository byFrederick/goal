package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// startGoald answers submitItem and the start routes, recording each request, and fails the
// test when anything reads events: an item with a start is not followed.
type startGoald struct {
	mu     sync.Mutex
	called []string
	bodies []map[string]any
}

var startAt = time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)

func (g *startGoald) serve(t *testing.T) string {
	t.Helper()

	record := func(action string, r *http.Request) map[string]any {
		var b map[string]any

		_ = json.NewDecoder(r.Body).Decode(&b)

		g.mu.Lock()
		g.called, g.bodies = append(g.called, action+" "+r.PathValue("item")), append(g.bodies, b)
		g.mu.Unlock()

		return b
	}

	reply := func(w http.ResponseWriter, status int, it gen.Item) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(it)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orgs/acme/factories/{factory}/items", func(w http.ResponseWriter, r *http.Request) {
		b := record("submit", r)

		it := gen.Item{Id: "id-3", Ref: "goal#3", Title: "Later", Station: "inbox", State: gen.ItemStateQueued}
		if s, ok := b["start"].(map[string]any); ok && (s["at"] != nil || s["in"] != nil) {
			it.Start = &gen.ItemStart{At: startAt}
		}

		reply(w, http.StatusCreated, it)
	})
	mux.HandleFunc("PUT /v1/orgs/acme/items/{item}/start", func(w http.ResponseWriter, r *http.Request) {
		record("put", r)
		reply(w, http.StatusOK, gen.Item{Ref: "goal#3", Start: &gen.ItemStart{At: startAt}})
	})
	mux.HandleFunc("DELETE /v1/orgs/acme/items/{item}/start", func(w http.ResponseWriter, r *http.Request) {
		record("delete", r)
		reply(w, http.StatusOK, gen.Item{Ref: "goal#3"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("goald got %s %s; want no other request (an item with a start is not followed)", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv.URL
}

// goal run with --in and --after submits the start, prints it and does not follow the item.
func TestRun_WithAStartDoesNotFollow(t *testing.T) {
	t.Parallel()

	g := &startGoald{}
	env := testEnv(t, g.serve(t), map[string]string{envOrg: orgAcme})

	var out bytes.Buffer
	if err := run(t.Context(), []string{"run", "goal", "Later", "--in", "36h", "--after", "goal#1", "--after", "goal#2"}, &printer{w: &out}, env, nil); err != nil {
		t.Fatalf("goal run: %v", err)
	}

	want := "submitted goal#3 \"Later\" (id-3) at inbox\n" +
		"starts at 2026-11-05T08:00:00Z and after goal#1, goal#2 (goal start goal#3 --now starts it now)\n"
	if out.String() != want {
		t.Errorf("output %q; want %q", out.String(), want)
	}

	start, _ := g.bodies[0]["start"].(map[string]any)
	if after, _ := start["after"].([]any); start["in"] != "36h" || start["at"] != nil || len(after) != 2 || after[1] != "goal#2" {
		t.Errorf("start sent = %v; want in 36h after goal#1 and goal#2", start)
	}

	if err := run(t.Context(), []string{"run", "goal", "Later", "--at", "2026-11-05T09:00", "--in", "1d"}, &printer{w: &bytes.Buffer{}}, env, nil); !errors.Is(err, errUsage) {
		t.Errorf("goal run --at --in = %v; want a usage error", err)
	}
}

// goal start sends --at or --in to PUT, and --now to DELETE.
func TestStart_ChangesOrClearsTheStart(t *testing.T) {
	t.Parallel()

	g := &startGoald{}
	env := testEnv(t, g.serve(t), map[string]string{envOrg: orgAcme})

	var out bytes.Buffer
	for _, args := range [][]string{
		{verbStart, "goal#3", "--at", "2026-11-05T09:00"},
		{verbStart, "goal#3", "--in", "2w"},
		{verbStart, "goal#3", "--now"},
	} {
		if err := run(t.Context(), args, &printer{w: &out}, env, nil); err != nil {
			t.Fatalf("goal %v: %v", args, err)
		}
	}

	if got := strings.Join(g.called, ", "); got != "put goal#3, put goal#3, delete goal#3" {
		t.Errorf("goald got %s", got)
	}

	if g.bodies[0]["at"] != "2026-11-05T09:00" || g.bodies[1]["in"] != "2w" {
		t.Errorf("bodies = %v", g.bodies)
	}

	if !strings.Contains(out.String(), "✓ goal#3 starts at 2026-11-05T08:00:00Z") || !strings.Contains(out.String(), "✓ goal#3 starts now") {
		t.Errorf("output %q", out.String())
	}
}

// goal start takes exactly one of --at, --in and --now.
func TestStart_UsageErrors(t *testing.T) {
	t.Parallel()

	env := testEnv(t, "http://127.0.0.1:1", nil) // never reached

	for _, args := range [][]string{
		{verbStart},
		{verbStart, "goal#3"},
		{verbStart, "goal#3", "--now", "--in", "1d"},
		{verbStart, "goal#3", "--at", "2026-11-05", "--in", "1d"},
		{verbStart, "goal#3", "goal#4", "--now"},
	} {
		if err := run(t.Context(), args, &printer{w: &bytes.Buffer{}}, env, nil); !errors.Is(err, errUsage) {
			t.Errorf("goal %v = %v; want a usage error", args, err)
		}
	}
}
