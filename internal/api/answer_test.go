package api_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The run that asked, and what it asked.
const (
	askingRun = "019a4000-0000-7000-8000-0000000000a1"
	question  = "Organization or factory spend?"
)

// asked seeds a line item whose agent asked a person at build, offering choices: it waits there
// for the answer, as the engine leaves it.
func (h *harness) asked(t *testing.T, choices ...string) domain.Item {
	t.Helper()

	it := h.seed(t, stBuild, domain.StateInProgress, t0.Add(-time.Hour))

	return h.move(t, it, domain.Position{Station: stBuild, State: domain.StateWaitingHuman}, func(tr *ports.Transition) {
		tr.Events = []domain.Event{{
			FactoryID: it.FactoryID, ItemID: it.ID, At: h.clock.Now(), Kind: domain.EventQuestionAsked, Station: stBuild,
			Actor: agBackend, RunID: askingRun,
			Data: map[string]any{
				domain.KeyQuestion: question, domain.KeyChoices: choices, domain.KeyAgent: agBackend, domain.KeyRunID: askingRun,
			},
		}}
	})
}

func answerPath(it domain.Item) string {
	return "/v1/orgs/acme/items/" + url.PathEscape(string(it.ID)) + "/answer"
}

func (h *harness) answered(t *testing.T) []domain.AuditEntry {
	t.Helper()

	entries, err := h.tenancy.AuditLog(inOrg(t), ports.AuditQuery{Action: domain.AuditItemAnswered, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	return entries
}

// An answer puts the item back in its station's queue, without rework and with no question left
// open, and the audit log has its length only.
func TestAPI_AnswerPutsTheItemBackInLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, body, audit string
	}{
		{name: "a choice", body: `{"choice":1}`, audit: `{"choice_index":1,"length":7}`},
		{name: "own words", body: `{"answer":"  Factory spend, per week.  "}`, audit: `{"length":24}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			it := h.asked(t, "Organization", "Factory")

			got := decode[gen.Item](t, h.request(t, http.MethodPost, answerPath(it), tt.body, nil), http.StatusOK)
			if got.Station != stBuild || got.State != gen.ItemStateQueued || got.ReworkCount != 0 || got.Question != nil {
				t.Errorf("item %s/%s rework %d question %+v; want queued at build, no rework, no question",
					got.Station, got.State, got.ReworkCount, got.Question)
			}

			entries := h.answered(t)
			if len(entries) != 1 || entries[0].TargetID != string(it.ID) || asJSON(t, entries[0].Data) != tt.audit {
				t.Errorf("audit %+v; want one item.answered with %s, never the text", entries, tt.audit)
			}
		})
	}
}

func TestAPI_AnswerRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	open := h.asked(t, "Organization", "Factory")
	none := h.seed(t, stBuild, domain.StateWaitingHuman, t0)
	done := h.finished(t, t0, t0.Add(time.Hour), false)
	cancelled := h.asked(t)

	if _, err := h.engine.Cancel(inOrg(t), cancelled.ID, domain.LocalUser, ""); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, body string
		item       domain.Item
		want       int
		code       string
	}{
		{name: "both an answer and a choice", body: `{"answer":"Factory","choice":1}`, item: open, want: http.StatusBadRequest},
		{name: "neither", body: `{}`, item: open, want: http.StatusBadRequest},
		{name: "no body", item: open, want: http.StatusBadRequest},
		{name: "an empty answer", body: `{"answer":""}`, item: open, want: http.StatusBadRequest},
		{name: "a blank answer", body: `{"answer":"  \n "}`, item: open, want: http.StatusBadRequest},
		{name: "an answer too long", body: `{"answer":"` + strings.Repeat("é", 2001) + `"}`, item: open, want: http.StatusBadRequest},
		{name: "a choice past the choices", body: `{"choice":2}`, item: open, want: http.StatusBadRequest, code: "bad-choice"},
		{name: "a negative choice", body: `{"choice":-1}`, item: open, want: http.StatusBadRequest, code: "bad-choice"},
		{name: "no open question", body: `{"choice":0}`, item: none, want: http.StatusConflict, code: "no-open-question"},
		{name: "a done item", body: `{"answer":"yes"}`, item: done, want: http.StatusConflict, code: codeItemClosed},
		{name: "a cancelled item", body: `{"answer":"yes"}`, item: cancelled, want: http.StatusConflict, code: codeItemClosed},
	}

	for _, tt := range tests {
		res := h.request(t, http.MethodPost, answerPath(tt.item), tt.body, nil)
		if p := decode[gen.Problem](t, res, tt.want); deref(p.Code) != tt.code {
			t.Errorf("%s: code %q; want %q (%s)", tt.name, deref(p.Code), tt.code, deref(p.Detail))
		}
	}

	got, err := h.store.Item(inOrg(t), open.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.State != domain.StateWaitingHuman || len(h.answered(t)) != 0 {
		t.Errorf("item %s, audit %v; refused answers change nothing", got.State, h.answered(t))
	}
}

// Answering is a decision: a factory viewer may not.
func TestAPI_AnswerNeedsItemDecide(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)

	var it gen.Item
	if res := h.as(t, ownerToken, http.MethodPost, acmeItems, `{"title":"Plain","type":"feature"}`); res.status != http.StatusCreated || json.Unmarshal(res.body, &it) != nil {
		t.Fatalf("the owner submitting = %d %s; want 201", res.status, res.body)
	}

	path := "/v1/orgs/acme/items/" + url.PathEscape(it.Ref) + "/answer"
	if res := h.as(t, viewerToken, http.MethodPost, path, `{"choice":0}`); res.status != http.StatusForbidden {
		t.Errorf("the viewer answering = %d %s; want 403", res.status, res.body)
	}
}

// getItem carries the open question, with the asking agent's role name, until it is answered.
func TestGetItem_CarriesItsOpenQuestion(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := h.asked(t, "Organization", "Factory")
	path := "/v1/orgs/acme/items/" + url.PathEscape(string(it.ID))

	got := decode[gen.Item](t, h.request(t, http.MethodGet, path, "", nil), http.StatusOK)

	want := gen.ItemQuestion{Text: question, Choices: []string{"Organization", "Factory"}, AskedAt: h.clock.Now(), Agent: agBackend, RunId: askingRun}
	if got.Question == nil || !reflect.DeepEqual(*got.Question, want) {
		t.Errorf("question %+v; want %+v", got.Question, want)
	}

	free := decode[gen.Item](t, h.request(t, http.MethodGet, "/v1/orgs/acme/items/"+url.PathEscape(string(h.asked(t).ID)), "", nil), http.StatusOK)
	if free.Question == nil || free.Question.Choices == nil || len(free.Question.Choices) != 0 {
		t.Errorf("question without choices %+v; want an empty list of choices", free.Question)
	}

	decode[gen.Item](t, h.request(t, http.MethodPost, answerPath(it), `{"answer":"Factory"}`, nil), http.StatusOK)

	res := h.request(t, http.MethodGet, path, "", nil)
	if got := decode[gen.Item](t, res, http.StatusOK); got.Question != nil || strings.Contains(string(res.body), `"question"`) {
		t.Errorf("question after the answer %+v (%s); want none", got.Question, res.body)
	}
}
