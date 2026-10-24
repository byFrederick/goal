package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

func TestAndon_PullShowAndClear(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	pull := gen.AndonPull{Id: 7, Kind: gen.AndonKindManual, Reason: "Bad answers", PulledBy: "user:local", PulledAt: at, Data: map[string]any{}}

	var (
		gotReason string
		gotNote   *string
	)

	reply := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orgs/acme/factories/ops/andon", func(w http.ResponseWriter, r *http.Request) {
		var b gen.NewAndonPull

		_ = json.NewDecoder(r.Body).Decode(&b)
		gotReason = b.Reason

		reply(w, http.StatusCreated, pull)
	})
	mux.HandleFunc("POST /v1/orgs/acme/factories/ops/andon/clear", func(w http.ResponseWriter, r *http.Request) {
		var b gen.AndonClear

		_ = json.NewDecoder(r.Body).Decode(&b)
		gotNote = b.Note

		reply(w, http.StatusOK, pull)
	})
	mux.HandleFunc("GET /v1/orgs/acme/factories/ops/andon", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, gen.AndonState{
			Open: &pull, Recent: []gen.AndonPull{pull},
			Gates: []gen.GateTally{{Station: "review", Verdicts: 10, Rejected: 3, TopReason: new("no evidence"), Warning: true}},
		})
	})
	mux.HandleFunc("POST /v1/orgs/acme/factories/calm/andon/clear", func(w http.ResponseWriter, _ *http.Request) {
		detail := "the andon cord is not pulled"

		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(gen.Problem{Title: "Conflict", Status: http.StatusConflict, Detail: &detail})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	env := testEnv(t, srv.URL, map[string]string{envOrg: orgAcme})

	goal := func(args ...string) (string, error) {
		var buf bytes.Buffer

		err := run(t.Context(), append([]string{"andon"}, args...), &printer{w: &buf}, env, nil)

		return buf.String(), err
	}

	if out, err := goal(andonPull, "ops", "-reason", "Bad answers"); err != nil || gotReason != "Bad answers" ||
		out != "✓ andon pulled on ops: the line releases and claims nothing until it is cleared\n" {
		t.Errorf("goal andon pull = %q, %v (goald got reason %q)", out, err, gotReason)
	}

	out, err := goal("ops")
	want := "ops: line stopped since 2026-10-23 09:00 UTC by user:local: Bad answers\n" +
		"  gate review: 3 of the last 10 verdicts sent back (most often: no evidence); one more reaches the rework rate\n" +
		"recent pulls:\n  2026-10-23 09:00 UTC by user:local: Bad answers\n"

	if err != nil || out != want {
		t.Errorf("goal andon ops = %q, %v; want %q", out, err, want)
	}

	if _, err := goal(andonClear, "ops", "--note", "Fixed the prompt"); err != nil || gotNote == nil || *gotNote != "Fixed the prompt" {
		t.Errorf("goal andon clear = %v (goald got note %v)", err, gotNote)
	}

	if _, err := goal(andonClear, "calm"); err == nil || !strings.Contains(err.Error(), "not pulled") {
		t.Errorf("clear of a running line = %v; want goald's refusal", err)
	}

	for _, args := range [][]string{{}, {andonPull, "ops"}, {"ops", "-reason", "x"}, {andonPull, "ops", "--note", "x", "-reason", "y"}, {"a", "b"}} {
		if _, err := goal(args...); !errors.Is(err, errUsage) {
			t.Errorf("goal andon %v = %v; want a usage error", args, err)
		}
	}
}
