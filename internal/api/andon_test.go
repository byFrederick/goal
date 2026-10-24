package api_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// acmeAndon is acme's andon cord.
const acmeAndon = "/v1/orgs/acme/factories/acme/andon"

func (h *harness) andon(t *testing.T, factory string) gen.AndonState {
	t.Helper()

	return decode[gen.AndonState](t, h.request(t, http.MethodGet, "/v1/orgs/acme/factories/"+factory+"/andon", "", nil), http.StatusOK)
}

func TestAPI_AndonPullAndClear(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	if got := h.andon(t, "acme"); got.Open != nil || len(got.Recent) != 0 || got.Policy != nil || len(got.Gates) != 0 {
		t.Errorf("andon of a running line without gates = %+v; want nothing open, no history, no policy, no gates", got)
	}

	pulled := h.expectPull(t)

	refused(t, "a second pull", h.request(t, http.MethodPost, acmeAndon, `{"reason":"again"}`, nil), http.StatusConflict, "andon-pulled")

	h.clock.Advance(time.Minute)
	h.expectClear(t, pulled)

	refused(t, "a clear with nothing pulled", h.request(t, http.MethodPost, acmeAndon+"/clear", "", nil), http.StatusConflict, "andon-not-pulled")

	if got := actions(h.audit(t, acmeSlug, "?factory=acme").Entries); !reflect.DeepEqual(got, []string{domain.AuditAndonCleared, domain.AuditAndonPulled}) {
		t.Errorf("acme's audit = %v; want the clear after the pull", got)
	}
}

// expectPull pulls acme's cord at t0 and checks the pull, the andon and the attention after it.
func (h *harness) expectPull(t *testing.T) gen.AndonPull {
	t.Helper()

	pulled := decode[gen.AndonPull](t, h.request(t, http.MethodPost, acmeAndon, `{"reason":"  Bad answers reach customers "}`, nil), http.StatusCreated)
	want := gen.AndonPull{
		Id: pulled.Id, Kind: gen.AndonKindManual, Reason: "Bad answers reach customers", PulledBy: domain.LocalUser.Ref(), PulledAt: t0,
		Data: map[string]any{},
	}

	if !reflect.DeepEqual(pulled, want) {
		t.Errorf("pull = %+v; want %+v", pulled, want)
	}

	if got := h.andon(t, acmeID); got.Open == nil || got.Open.Id != pulled.Id || len(got.Recent) != 1 {
		t.Errorf("andon after the pull = %+v; want the pull open and listed", got)
	}

	if a := h.attention(t, "acme"); a[acmeID] == nil || a[acmeID].Reason != want.Reason {
		t.Errorf("attention of acme = %+v; want its open pull", a[acmeID])
	}

	if line := h.boardLine(t); line.Status != gen.BoardLineStatusStopped || line.Andon == nil || line.Andon.Id != pulled.Id {
		t.Errorf("board line = %s with andon %+v; want stopped by the pull", line.Status, line.Andon)
	}

	return pulled
}

// boardLine is acme's board line.
func (h *harness) boardLine(t *testing.T) gen.BoardLine {
	t.Helper()

	return decode[gen.Board](t, h.request(t, http.MethodGet, "/v1/orgs/acme/factories/acme/board", "", nil), http.StatusOK).Line
}

// expectClear clears acme's cord now, a minute after t0, and checks the pull cleared, the andon
// and the attention after it.
func (h *harness) expectClear(t *testing.T, pulled gen.AndonPull) {
	t.Helper()

	cleared := decode[gen.AndonPull](t, h.request(t, http.MethodPost, acmeAndon+"/clear", `{"note":"Fixed the prompt"}`, nil), http.StatusOK)

	by, note := valueOf(cleared.ClearedBy), valueOf(cleared.Note)
	if cleared.Id != pulled.Id || by != domain.LocalUser.Ref() || note != "Fixed the prompt" || cleared.ClearedAt == nil || !cleared.ClearedAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("clear = %+v; want the pull cleared by the local user a minute later, with the note", cleared)
	}

	if got := h.andon(t, "acme"); got.Open != nil || len(got.Recent) != 1 || got.Recent[0].ClearedAt == nil {
		t.Errorf("andon after the clear = %+v; want nothing open and the cleared pull in the history", got)
	}

	if a := h.attention(t, "acme"); a[acmeID] != nil {
		t.Errorf("attention of acme after the clear = %+v; want no andon", a[acmeID])
	}

	if line := h.boardLine(t); line.Status != gen.BoardLineStatusRunning || line.Andon != nil {
		t.Errorf("board line after the clear = %s with andon %+v; want running", line.Status, line.Andon)
	}
}

func TestAPI_AndonRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	tests := []struct {
		name, path, body string
		want             int
	}{
		{name: "blank reason", path: acmeAndon, body: `{"reason":"   "}`, want: http.StatusBadRequest},
		{name: "reason too long", path: acmeAndon, body: `{"reason":"` + strings.Repeat("x", 501) + `"}`, want: http.StatusBadRequest},
		{name: "no body", path: acmeAndon, want: http.StatusBadRequest},
		{name: "unknown factory", path: "/v1/orgs/acme/factories/nope/andon", body: `{"reason":"x"}`, want: http.StatusNotFound},
		{name: "another organization's factory", path: "/v1/orgs/example-labs/factories/acme/andon", body: `{"reason":"x"}`, want: http.StatusNotFound},
		{name: "note too long", path: acmeAndon + "/clear", body: `{"note":"` + strings.Repeat("y", 2001) + `"}`, want: http.StatusBadRequest},
	}

	for _, tt := range tests {
		if res := h.request(t, http.MethodPost, tt.path, tt.body, nil); res.status != tt.want {
			t.Errorf("%s: %d %s; want %d", tt.name, res.status, res.body, tt.want)
		}
	}

	for _, q := range []string{"?limit=-1", "?limit=51"} {
		if res := h.request(t, http.MethodGet, acmeAndon+q, "", nil); res.status != http.StatusBadRequest {
			t.Errorf("GET andon%s = %d; want 400", q, res.status)
		}
	}
}

// review is inbox → write → check (a gate sending work back to write) → done, with an andon
// policy of 2 rejections in the last 5 verdicts.
const (
	stCheck  = "check"
	reviewID = "5e0c3d2b-1a4f-4b6e-8c7d-9e0f1a2b3c4d"
	review   = `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + reviewID + `, name: review }
spec:
  runtime: { runner: docker, image: ghcr.io/acme/agent:1.0.0, auth: anthropic-api-key }
  knowledgeBase: { collections: [notes] }
  agents:
    - { name: writer, prompt: ./agents/writer.md }
  board:
    workTypes: [feature]
    stations:
      - { id: inbox, kind: queue }
      - { id: write, kind: agent, agent: writer }
      - { id: check, kind: agent, agent: writer, gate: { onReject: write, maxRework: 2 } }
      - { id: done, kind: done }
    flow:
      andon: { reworkRate: 0.4, window: 5 }
`
)

// publishReview imports and publishes the review factory through the API.
func (h *harness) publishReview(t *testing.T) {
	t.Helper()

	files := textFiles(map[string]string{yamlFile: review, writerFile: writerPrompt})
	decode[gen.Factory](t, h.request(t, http.MethodPost, factoriesPath, body(t, map[string]any{
		"start": map[string]any{"kind": authoring.StartImport, "keep_id": true, filesKey: files},
	}), nil), http.StatusCreated)

	d := decode[gen.Draft](t, h.request(t, http.MethodGet, factoriesPath+"/review/draft", "", nil), http.StatusOK)
	decode[gen.Version](t, h.request(t, http.MethodPost, factoriesPath+"/review/draft/publish", body(t, map[string]any{
		revisionKey: d.Revision, expectedKey: nil,
	}), nil), http.StatusCreated)
}

// verdicts appends gate verdicts at check on an item of review at `at`: true rejects.
func (h *harness) verdicts(t *testing.T, at time.Time, reasons map[bool][]string) {
	t.Helper()

	ctx := inOrg(t)

	it, err := h.store.CreateItem(ctx, domain.NewItem{
		FactoryID: reviewID, Type: typeFeature, Title: "checked", Station: stCheck, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}

	var events []domain.Event

	for rejected, list := range reasons {
		for _, reason := range list {
			e := domain.Event{FactoryID: reviewID, ItemID: it.ID, At: at, Kind: domain.EventGateApproved, Station: stCheck, Actor: "writer"}
			if rejected {
				e.Kind, e.Data = domain.EventGateRejected, map[string]any{"reason": reason}
			}

			events = append(events, e)
		}
	}

	if err := h.store.AppendEvents(ctx, events...); err != nil {
		t.Fatal(err)
	}
}

// Gate tallies count the last window verdicts after the last clear, and warn one rejection
// before the policy's rate.
func TestAPI_AndonGateTalliesSinceTheLastClear(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.publishReview(t)
	h.verdicts(t, t0.Add(-time.Hour), map[bool][]string{true: {"old", "old", "old"}})

	got := h.andon(t, "review")
	if got.Policy == nil || got.Policy.ReworkRate != 0.4 || got.Policy.Window != 5 {
		t.Errorf("policy = %+v; want 0.4 over 5", got.Policy)
	}

	if want := []gen.GateTally{{Station: stCheck, Verdicts: 3, Rejected: 3, TopReason: new("old"), Warning: true}}; !reflect.DeepEqual(got.Gates, want) {
		t.Errorf("gates before any clear = %+v; want every verdict", got.Gates)
	}

	reviewAndon := "/v1/orgs/acme/factories/review/andon"
	decode[gen.AndonPull](t, h.request(t, http.MethodPost, reviewAndon, `{"reason":"Too much rework"}`, nil), http.StatusCreated)
	h.clock.Advance(time.Minute)
	decode[gen.AndonPull](t, h.request(t, http.MethodPost, reviewAndon+"/clear", "", nil), http.StatusOK)

	if got := h.andon(t, "review").Gates; len(got) != 1 || got[0].Verdicts != 0 || got[0].Warning {
		t.Errorf("gates right after the clear = %+v; want nothing counted yet", got)
	}

	h.verdicts(t, t0.Add(2*time.Minute), map[bool][]string{false: {""}, true: {"No evidence "}})

	want := []gen.GateTally{{Station: stCheck, Verdicts: 2, Rejected: 1, TopReason: new("no evidence"), Warning: true}}
	if got := h.andon(t, "review").Gates; !reflect.DeepEqual(got, want) {
		t.Errorf("gates after the clear = %+v; want %+v", got, want)
	}
}

// A factory event from the store's notices reaches the stream as `event: factory`, with its data
// on one line.
func TestAPI_StreamSendsFactoryEvents(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, _, events := h.stream(t, "?factory=acme")
	events.next(t) // retry

	data := `{"org_id":"` + string(testOrg) + `","factory_id":"` + acmeID + `","kind":"andon_pulled","andon_id":7,"at":"2026-10-23T09:00:00+00:00"}`
	h.notices.Publish(
		ports.ItemNotice{OrgID: testOrg, FactoryID: lineID, Org: &ports.OrgEvent{Name: "factory", FactoryID: lineID, Data: json.RawMessage(`{}`)}},
		ports.ItemNotice{OrgID: testOrg, FactoryID: acmeID, Org: &ports.OrgEvent{Name: "factory", FactoryID: acmeID, Data: json.RawMessage(data)}},
	)

	got := events.next(t)
	if len(got) != 2 || got[0] != "event: factory" || !strings.HasPrefix(got[1], "data: ") {
		t.Fatalf("event %q; want acme's factory event (and not line's)", got)
	}

	var e gen.FactoryEvent
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got[1], "data: ")), &e); err != nil {
		t.Fatal(err)
	}

	if e.FactoryId != acmeID || e.Kind != "andon_pulled" || valueOf(e.AndonId) != 7 || !e.At.Equal(t0) {
		t.Errorf("factory event = %+v; want acme's andon_pulled of pull 7 at %s", e, t0)
	}
}

func actions(entries []gen.AuditEntry) []string {
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Action)
	}

	return out
}

// attention is the org's attention by factory id: its open andon pull, or nil.
func (h *harness) attention(t *testing.T, org string) map[string]*gen.AndonPull {
	t.Helper()

	got := decode[gen.OrgAttention](t, h.request(t, http.MethodGet, "/v1/orgs/"+org+"/attention", "", nil), http.StatusOK)

	out := map[string]*gen.AndonPull{}
	for _, f := range got.Factories {
		out[f.FactoryId] = f.Andon
	}

	return out
}
