package cli

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
		_ = json.NewEncoder(w).Encode(gen.NoteReceipt{EventId: 42, Delivery: gen.NoteReceiptDeliveryNextRun})
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

// A not-found or a refused credential has no detail: the CLI prints its title alone.
func TestProblemText_DetailOnlyWhenThere(t *testing.T) {
	t.Parallel()

	detail := "the item is not waiting for a human"

	for _, tt := range []struct {
		p    gen.Problem
		want string
	}{
		{p: gen.Problem{Title: "Item not found", Status: http.StatusNotFound}, want: "Item not found"},
		{p: gen.Problem{Title: "Item closed", Status: http.StatusConflict, Detail: &detail}, want: "Item closed: " + detail},
	} {
		if got := problemText(&tt.p); got != tt.want {
			t.Errorf("problemText = %q, want %q", got, tt.want)
		}
	}
}

// A cheap run's cost is not printed as $0.00: below a cent it keeps four decimals.
func TestDescribe_RunCostKeepsCents(t *testing.T) {
	t.Parallel()

	for cost, want := range map[float64]string{
		0:      "run succeeded ($0.00, 1 turns)",
		0.0007: "run succeeded ($0.0007, 1 turns)",
		0.0042: "run succeeded ($0.0042, 1 turns)",
		0.01:   "run succeeded ($0.01, 1 turns)",
		0.42:   "run succeeded ($0.42, 1 turns)",
		12.345: "run succeeded ($12.35, 1 turns)",
	} {
		e := gen.Event{Kind: "run_finished", Data: map[string]any{"status": "succeeded", "cost_usd": cost, "turns": 1}}
		if got := describe(e); got != want {
			t.Errorf("describe(cost %v) = %q, want %q", cost, got, want)
		}
	}

	if got := usd(0.00004); got != "<$0.0001" {
		t.Errorf("usd(0.00004) = %q", got)
	}
}

// What the answer tests send and the question events they describe.
const (
	flagChoice    = "--choice"
	kindAsked     = "question_asked"
	keyAgent      = "agent"
	keyQuestion   = "question"
	roleArchitect = "architect"
)

// answerGoald plays an item with an open question: goal#1 takes an answer, goal#2 has no open
// question, and a choice past the first two is not one of the question's.
type answerGoald struct {
	mu     sync.Mutex
	called []string
	bodies []map[string]any
}

func (g *answerGoald) serve(t *testing.T) *httptest.Server {
	t.Helper()

	refuse := func(w http.ResponseWriter, status int, code string) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(gen.Problem{Title: "Refused", Status: status, Code: &code})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orgs/acme/items/{item}/answer", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any

		_ = json.NewDecoder(r.Body).Decode(&b)

		g.mu.Lock()
		g.called, g.bodies = append(g.called, r.PathValue("item")), append(g.bodies, b)
		g.mu.Unlock()

		switch {
		case r.PathValue("item") == closedRef:
			refuse(w, http.StatusConflict, "no-open-question")
		case b["choice"] != nil && b["choice"] != float64(0) && b["choice"] != float64(1):
			refuse(w, http.StatusBadRequest, "bad-choice")
		case b["answer"] == "forbidden":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusConflict)

			detail := "the item moved meanwhile"
			_ = json.NewEncoder(w).Encode(gen.Problem{Title: "Item changed", Status: http.StatusConflict, Detail: &detail})
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(gen.Item{Ref: notedRef, State: gen.ItemStateQueued, Station: stationSpec})
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestAnswer_SendsTheTextOrTheChoice(t *testing.T) {
	t.Parallel()

	g := &answerGoald{}
	env := testEnv(t, g.serve(t).URL, map[string]string{envOrg: orgAcme})

	var out bytes.Buffer
	for _, args := range [][]string{
		{verbAnswer, notedRef, "Use the read replica."},
		{verbAnswer, notedRef, flagChoice, "2"},
	} {
		if err := run(t.Context(), args, &printer{w: &out}, env, nil); err != nil {
			t.Fatalf("goal %v: %v", args, err)
		}
	}

	line := "✓ answered goal#1; now queued at spec\n"
	if out.String() != line+line {
		t.Errorf("output %q; want %q twice", out.String(), line)
	}

	if !slices.Equal(g.called, []string{notedRef, notedRef}) || len(g.bodies[0]) != 1 || g.bodies[0]["answer"] != "Use the read replica." ||
		len(g.bodies[1]) != 1 || g.bodies[1]["choice"] != float64(1) {
		t.Errorf("goald got %v with %v", g.called, g.bodies)
	}
}

func TestAnswer_UsageErrors(t *testing.T) {
	t.Parallel()

	env := testEnv(t, "http://127.0.0.1:1", nil) // never reached

	for _, args := range [][]string{
		{verbAnswer},
		{verbAnswer, notedRef},
		{verbAnswer, notedRef, "  "},
		{verbAnswer, notedRef, "a", "b"},
		{verbAnswer, notedRef, "a", flagChoice, "1"},
		{verbAnswer, notedRef, flagChoice, "0"},
		{verbAnswer, notedRef, flagChoice, "-1"},
		{verbAnswer, notedRef, flagChoice, "x"},
		{verbAnswer, flagChoice, "1"},
	} {
		if err := run(t.Context(), args, &printer{w: &bytes.Buffer{}}, env, nil); !errors.Is(err, errUsage) {
			t.Errorf("goal %v = %v; want a usage error", args, err)
		}
	}
}

func TestAnswer_Refusals(t *testing.T) {
	t.Parallel()

	g := &answerGoald{}
	env := testEnv(t, g.serve(t).URL, map[string]string{envOrg: orgAcme})

	for _, tt := range []struct {
		args []string
		want string
	}{
		{args: []string{verbAnswer, closedRef, "yes"}, want: closedRef + " has no open question to answer"},
		{args: []string{verbAnswer, notedRef, flagChoice, "3"}, want: "choice 3 is not one of the question's choices"},
		{args: []string{verbAnswer, notedRef, "forbidden"}, want: "Item changed: the item moved meanwhile"},
	} {
		err := run(t.Context(), tt.args, &printer{w: &bytes.Buffer{}}, env, nil)
		if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), "Refused") {
			t.Errorf("goal %v = %v; want %q", tt.args, err, tt.want)
		}
	}
}

func TestDescribe_QuestionEvents(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("why ", 100)

	for _, tt := range []struct {
		e    gen.Event
		want string
	}{
		{
			e: gen.Event{Kind: kindAsked, Data: map[string]any{
				keyAgent: roleArchitect, keyQuestion: "Which\ndatabase?", "choices": []any{"PostgreSQL", "the replica"},
			}},
			want: roleArchitect + " asked: Which database? (choices: 1. PostgreSQL, 2. the replica)",
		},
		{
			e:    gen.Event{Kind: kindAsked, Data: map[string]any{keyAgent: roleArchitect, keyQuestion: "Which database?"}},
			want: roleArchitect + " asked: Which database?",
		},
		{
			e:    gen.Event{Kind: kindAsked, Data: map[string]any{keyAgent: roleArchitect, keyQuestion: "Odd?", "choices": []any{"yes", nil, 2}}},
			want: roleArchitect + " asked: Odd? (choices: 1. yes, 2. , 3. 2)",
		},
		{
			e:    gen.Event{Kind: "question_answered", Actor: "pat", Data: map[string]any{"answer": "the  replica", "choice_index": 1}},
			want: "answer from pat: the replica",
		},
		{
			e:    gen.Event{Kind: kindAsked, Data: map[string]any{keyAgent: roleArchitect, keyQuestion: long}},
			want: cut(roleArchitect+" asked: "+oneLine(long), 300),
		},
	} {
		got := describe(tt.e)
		if got != tt.want {
			t.Errorf("describe(%s) = %q, want %q", tt.e.Kind, got, tt.want)
		}

		if strings.Contains(got, "\n") || len([]rune(strings.TrimSuffix(got, "…"))) > 300 {
			t.Errorf("describe(%s) = %q, want one line of at most 300 characters", tt.e.Kind, got)
		}
	}
}

// What the tool approval tests describe and send.
const (
	connGitHub    = "github"
	toolComment   = "create_comment"
	codeToolOpen  = "tool-approval-open"
	keyApproval   = "approval_id"
	actorPat      = "pat"
	flagAlways    = "--always-allow"
	refNotOffered = "goal#5"
	toolCalled    = "tool-approval " + notedRef
	askedLineHead = roleArchitect + " asks to call " + connGitHub + " " + toolComment + ": "
)

func TestDescribe_ToolApprovalEvents(t *testing.T) {
	t.Parallel()

	long := map[string]any{"body": strings.Repeat("a long\ncomment ", 100)}

	for _, tt := range []struct {
		e    gen.Event
		want string
	}{
		{
			e: gen.Event{Kind: "tool_approval_requested", Actor: "architect-2", Data: map[string]any{
				keyAgent: roleArchitect, "connection": connGitHub, "tool": toolComment,
				"input": map[string]any{"body": "Looks good", "issue": 7}, "always_allow_offered": true,
			}},
			want: askedLineHead + `{"body":"Looks good","issue":7} (always allow offered)`,
		},
		{
			e: gen.Event{Kind: "tool_approval_requested", Data: map[string]any{
				keyAgent: roleArchitect, "connection": connGitHub, "tool": toolComment, "input": map[string]any{}, "always_allow_offered": false,
			}},
			want: askedLineHead + `{}`,
		},
		{
			e:    gen.Event{Kind: "tool_approval_granted", Actor: actorPat, Data: map[string]any{keyApproval: 9, "always_allow": true}},
			want: "tool call approved by pat, always allow",
		},
		{
			e:    gen.Event{Kind: "tool_approval_granted", Actor: actorPat, Data: map[string]any{keyApproval: 9, "always_allow": false}},
			want: "tool call approved by pat",
		},
		{
			e:    gen.Event{Kind: "tool_approval_denied", Actor: actorPat, Data: map[string]any{keyApproval: 9, "note": "Not on\nthat issue"}},
			want: "tool call denied by pat: Not on that issue",
		},
		{
			e:    gen.Event{Kind: "tool_approval_denied", Actor: actorPat, Data: map[string]any{keyApproval: 9}},
			want: "tool call denied by pat",
		},
	} {
		if got := describe(tt.e); got != tt.want {
			t.Errorf("describe(%s) = %q, want %q", tt.e.Kind, got, tt.want)
		}
	}

	got := describe(gen.Event{Kind: "tool_approval_requested", Data: map[string]any{
		keyAgent: roleArchitect, "connection": connGitHub, "tool": toolComment, "input": long,
	}})
	if !strings.HasPrefix(got, askedLineHead) || strings.Contains(got, "\n") || len([]rune(got)) > 200 {
		t.Errorf("describe(a long input) = %q, want one cut line", got)
	}
}

func TestStopped_ShowsTheOpenToolApproval(t *testing.T) {
	t.Parallel()

	for _, offered := range []bool{true, false} {
		var buf bytes.Buffer

		it := &gen.Item{Ref: notedRef, State: "waiting_human", Station: stationSpec, ToolApproval: &gen.ToolApproval{
			Id: 9, Agent: roleArchitect, Connection: connGitHub, Tool: toolComment, RunId: "run-1",
			Input: map[string]any{"body": strings.Repeat("x", 500)}, AlwaysAllowOffered: offered,
		}}
		if err := stopped(&printer{w: &buf}, it); err != nil {
			t.Fatal(err)
		}

		got := buf.String()
		for _, want := range []string{
			"goal#1 is waiting_human at spec", "  " + askedLineHead + `{"body":"xxx`,
			"goal approve goal#1 ", `goal deny goal#1 --note "..."`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("offered %v: output has no %q:\n%s", offered, want, got)
			}
		}

		if strings.Contains(got, flagAlways) != offered {
			t.Errorf("offered %v: output offers --always-allow %v:\n%s", offered, !offered, got)
		}

		if strings.Contains(got, "reject") || strings.Contains(got, "merge") || strings.Contains(got, strings.Repeat("x", 200)) {
			t.Errorf("output has the reject or merge hints, or the whole input:\n%s", got)
		}
	}
}

// toolGoald plays items waiting on tool approvals: goal#1 has one open, goal#2 has none, goal#5
// has one that does not offer Always allow. Approving goal#1 the item's way is refused because
// its tool approval is open.
type toolGoald struct {
	mu     sync.Mutex
	called []string
	bodies []map[string]any
}

func (g *toolGoald) serve(t *testing.T) *httptest.Server {
	t.Helper()

	refuse := func(w http.ResponseWriter, status int, code string) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(gen.Problem{Title: "Refused", Status: status, Code: &code})
	}
	record := func(r *http.Request, what string) map[string]any {
		var b map[string]any

		_ = json.NewDecoder(r.Body).Decode(&b)

		g.mu.Lock()
		g.called, g.bodies = append(g.called, what+" "+r.PathValue("item")), append(g.bodies, b)
		g.mu.Unlock()

		return b
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orgs/acme/items/{item}/approve", func(w http.ResponseWriter, r *http.Request) {
		record(r, verbApprove)

		if r.PathValue("item") == notedRef {
			refuse(w, http.StatusConflict, codeToolOpen)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gen.Item{Ref: r.PathValue("item"), State: gen.ItemStateDone, Station: stationDone})
	})
	mux.HandleFunc("POST /v1/orgs/acme/items/{item}/tool-approval", func(w http.ResponseWriter, r *http.Request) {
		b := record(r, "tool-approval")

		switch {
		case r.PathValue("item") == closedRef:
			refuse(w, http.StatusConflict, "no-open-tool-approval")
		case r.PathValue("item") == refNotOffered && b["always_allow"] == true:
			refuse(w, http.StatusConflict, "always-allow-not-offered")
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(gen.Item{Ref: r.PathValue("item"), State: gen.ItemStateQueued, Station: stationSpec})
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// runLines runs each command line against env, failing the test on the first error, and returns
// what they printed.
func runLines(t *testing.T, env func(string) string, lines ...[]string) string {
	t.Helper()

	var out bytes.Buffer
	for _, args := range lines {
		if err := run(t.Context(), args, &printer{w: &out}, env, nil); err != nil {
			t.Fatalf("goal %v: %v", args, err)
		}
	}

	return out.String()
}

// wantRefusal fails unless the command line fails with exactly the error want.
func wantRefusal(t *testing.T, env func(string) string, args []string, want string) {
	t.Helper()

	if err := run(t.Context(), args, &printer{w: &bytes.Buffer{}}, env, nil); err == nil || err.Error() != want {
		t.Errorf("goal %v = %v; want %q", args, err, want)
	}
}

// wantUsage fails for each command line that is not a usage error.
func wantUsage(t *testing.T, env func(string) string, lines ...[]string) {
	t.Helper()

	for _, args := range lines {
		if err := run(t.Context(), args, &printer{w: &bytes.Buffer{}}, env, nil); !errors.Is(err, errUsage) {
			t.Errorf("goal %v = %v; want a usage error", args, err)
		}
	}
}

func TestApprove_ToolApproval(t *testing.T) {
	t.Parallel()

	g := &toolGoald{}
	env := testEnv(t, g.serve(t).URL, map[string]string{envOrg: orgAcme})

	got := runLines(t, env, []string{verbApprove, notedRef, flagAlways}, []string{verbApprove, notedRef}, []string{verbApprove, "goal#4"})

	want := "✓ approved goal#1's tool call, and always allowed it; now queued at spec\n" +
		"✓ approved goal#1's tool call; now queued at spec\n" +
		"✓ approved goal#4; now done at done\n"
	if got != want {
		t.Errorf("output %q; want %q", got, want)
	}

	wantCalled := []string{toolCalled, "approve goal#1", toolCalled, "approve goal#4"}
	if !slices.Equal(g.called, wantCalled) || g.bodies[0]["decision"] != string(gen.Approve) || g.bodies[0]["always_allow"] != true ||
		g.bodies[2]["decision"] != string(gen.Approve) || len(g.bodies[2]) != 1 {
		t.Errorf("goald got %v with %v; want %v", g.called, g.bodies, wantCalled)
	}

	wantRefusal(t, env, []string{verbApprove, closedRef, flagAlways}, closedRef+" has no tool call waiting for a decision")
	wantRefusal(t, env, []string{verbApprove, refNotOffered, flagAlways},
		"this call does not offer Always allow: its Ask first comes from the factory definition")
	wantUsage(t, env, []string{verbMerge, notedRef, flagAlways}, []string{verbRetry, notedRef, flagAlways})
}

func TestDeny_SendsTheDecision(t *testing.T) {
	t.Parallel()

	g := &toolGoald{}
	env := testEnv(t, g.serve(t).URL, map[string]string{envOrg: orgAcme})

	got := runLines(t, env, []string{verbDeny, notedRef, "--note", "Not on that issue"}, []string{verbDeny, notedRef})

	line := "✓ denied goal#1's tool call; now queued at spec\n"
	if got != line+line {
		t.Errorf("output %q; want %q twice", got, line)
	}

	if !slices.Equal(g.called, []string{toolCalled, toolCalled}) ||
		g.bodies[0]["decision"] != string(gen.Deny) || g.bodies[0]["note"] != "Not on that issue" || len(g.bodies[0]) != 2 ||
		g.bodies[1]["decision"] != string(gen.Deny) || len(g.bodies[1]) != 1 {
		t.Errorf("goald got %v with %v", g.called, g.bodies)
	}

	wantRefusal(t, env, []string{verbDeny, closedRef}, closedRef+" has no tool call waiting for a decision")
	wantUsage(t, env, []string{verbDeny}, []string{verbDeny, notedRef, "x"}, []string{verbDeny, notedRef, flagAlways},
		[]string{verbDeny, notedRef, "--note", "  "})
}
