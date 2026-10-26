package api_test

import (
	"net/http"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

const acmeItem1 = "/v1/orgs/acme/items/acme%231"

// The actions of a person's control of an item.
const (
	actMove  = "move"
	actPause = "pause"
	actRetry = "retry"
)

func TestAPI_MoveAndPause(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.submit(t) // acme#1, queued at the inbox

	steps := []struct {
		name   string
		action string
		body   string
		status int
		code   string
		state  gen.ItemState
	}{
		{name: "an unknown station", action: actMove, body: `{"to":"nowhere"}`, status: http.StatusBadRequest, code: "unknown-station"},
		{name: "its own station", action: actMove, body: `{"to":"inbox"}`, status: http.StatusConflict, code: "same-station"},
		{name: "a done station", action: actMove, body: `{"to":"done"}`, status: http.StatusConflict, code: "done-station"},
		{name: "a send-back of a queued item", action: "reject", body: `{"reason":"no"}`, status: http.StatusConflict, code: "cannot-send-back"},
		{name: "downstream", action: actMove, body: `{"to":"approve"}`, status: http.StatusOK, state: gen.ItemStateWaitingHuman},
		{name: "upstream without a reason", action: actMove, body: `{"to":"inbox"}`, status: http.StatusBadRequest, code: "reason-required"},
		{name: "upstream with a reason", action: actMove, body: `{"to":"inbox","reason":"needs triage"}`, status: http.StatusOK, state: gen.ItemStateQueued},
		{name: "pause without a body", action: actPause, status: http.StatusOK, state: gen.ItemStateBlocked},
		{name: "pause again", action: actPause, body: `{"reason":"still"}`, status: http.StatusConflict, code: "already-blocked"},
		{name: "retry resumes", action: actRetry, status: http.StatusOK, state: gen.ItemStateQueued},
	}

	for _, s := range steps {
		var (
			it   gen.Item
			prob gen.Problem
			out  any = &it
		)

		if s.status != http.StatusOK {
			out = &prob
		}

		if got := h.call(t, http.MethodPost, acmeItem1+"/"+s.action, s.body, out); got != s.status {
			t.Fatalf("%s: %s = %d; want %d", s.name, s.action, got, s.status)
		}

		if s.status != http.StatusOK && deref(prob.Code) != s.code {
			t.Errorf("%s: problem %+v; want code %s", s.name, prob, s.code)
		}

		if s.status == http.StatusOK && it.State != s.state {
			t.Errorf("%s: item %s at %s; want %s", s.name, it.State, it.Station, s.state)
		}
	}

	var it gen.Item
	if h.call(t, http.MethodGet, acmeItem1, "", &it); it.ReworkCount != 1 {
		t.Errorf("rework count %d after one upstream move; want 1", it.ReworkCount)
	}
}

func TestAPI_MoveOfAClosedItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.submit(t)

	if code := h.call(t, http.MethodPost, acmeItem1+"/move", `{"to":"approve"}`, nil); code != http.StatusOK {
		t.Fatalf("move = %d", code)
	}

	if code := h.call(t, http.MethodPost, acmeItem1+"/approve", ``, nil); code != http.StatusOK {
		t.Fatalf("approve = %d", code)
	}

	var p gen.Problem
	if code := h.call(t, http.MethodPost, acmeItem1+"/move", `{"to":"approve"}`, &p); code != http.StatusConflict || deref(p.Code) != "item-closed" {
		t.Errorf("move of a done item = %d %+v; want 409 item-closed", code, p)
	}

	if code := h.call(t, http.MethodPost, acmeItem1+"/pause", ``, &p); code != http.StatusConflict || deref(p.Code) != "item-closed" {
		t.Errorf("pause of a done item = %d %+v; want 409 item-closed", code, p)
	}
}
