package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// GET of one item carries how it flows: its spans, the lead-time target of its class, where a
// send-back goes, the task limits at its station and the run in progress with its turns.
func TestAPI_GetItemFlow(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	working := h.seed(t, stBuild, domain.StateQueued, t0)
	claimedAt := t0.Add(2 * time.Minute)

	if _, _, err := h.store.ClaimNext(inOrg(t), ports.Claim{
		FactoryID: lineID, Station: build, Slot: 1, Now: claimedAt, ExpiresAt: claimedAt.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	run, err := h.store.StartRun(inOrg(t), ports.NewRun{
		FactoryID: lineID, ItemID: working.ID, Station: stBuild, Agent: agBackend, Model: "claude-sonnet-5-5", StartedAt: claimedAt,
	})
	if err != nil {
		t.Fatal(err)
	}

	usage := domain.Event{FactoryID: lineID, ItemID: working.ID, RunID: run, At: claimedAt, Kind: domain.EventUsage, Station: stBuild, Actor: agBackend}
	if err := h.store.AppendEvents(inOrg(t), usage, usage, usage); err != nil {
		t.Fatal(err)
	}

	waiting := h.seed(t, stMerge, domain.StateWaitingHuman, t0)

	tests := []struct {
		name string
		item domain.ItemID
		want string
	}{
		{
			name: "in progress",
			item: working.ID,
			want: `{"spans":[
				{"station":"build","state":"queued","rework_round":0,"rework_by":null,"entered_at":"2026-10-23T09:00:00Z","left_at":"2026-10-23T09:02:00Z"},
				{"station":"build","state":"in_progress","rework_round":0,"rework_by":null,"entered_at":"2026-10-23T09:02:00Z","left_at":null}],
				"target_seconds":21600,"rework_target":null,
				"budget":{"per_item_usd":4,"max_turns_per_task":80},
				"current_run":{"id":"` + string(run) + `","agent":"backend","slot":2,"model":"claude-sonnet-5-5","turn":3,
					"started_at":"2026-10-23T09:02:00Z","output_contract":"change","checks":["schema","2+ evidence links","task check"]}}`,
		},
		{
			name: "waiting for a person",
			item: waiting.ID,
			want: `{"spans":[
				{"station":"merge","state":"waiting_human","rework_round":0,"rework_by":null,"entered_at":"2026-10-23T09:00:00Z","left_at":null}],
				"target_seconds":21600,"rework_target":"build",
				"budget":{"per_item_usd":4,"max_turns_per_task":80},"current_run":null}`,
		},
	}

	for _, tt := range tests {
		var got gen.Item
		if code := h.call(t, http.MethodGet, "/v1/orgs/acme/items/"+string(tt.item), "", &got); code != http.StatusOK || got.Flow == nil {
			t.Fatalf("%s: get item = %d, flow %v", tt.name, code, got.Flow)
		}

		if g, w := normalize(t, got.Flow), normalize(t, json.RawMessage(tt.want)); g != w {
			t.Errorf("%s: flow =\n  %s\nwant\n  %s", tt.name, g, w)
		}
	}
}

// normalize re-encodes v through a generic value, so spacing and key order do not matter.
func normalize(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatal(err)
	}

	if b, err = json.Marshal(generic); err != nil {
		t.Fatal(err)
	}

	return string(b)
}
