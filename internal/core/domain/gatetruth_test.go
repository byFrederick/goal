package domain

import (
	"strings"
	"testing"
	"time"
)

func TestGateTruth(t *testing.T) {
	t.Parallel()

	line := Line{Stations: []Station{
		{ID: "queue", Kind: KindQueue}, {ID: "build", Kind: KindAgent}, {ID: "review", Kind: KindAgent, Gate: &Gate{OnReject: "build"}},
		{ID: "approve", Kind: KindHuman}, {ID: "done", Kind: KindDone},
	}}
	start := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return start.Add(time.Duration(s) * time.Second) }
	person := map[string]any{"actor": "user:ada"}
	withData := func(d map[string]any, kv ...string) map[string]any {
		out := map[string]any{}
		for k, v := range d {
			out[k] = v
		}

		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i]] = kv[i+1]
		}

		return out
	}

	claim := func(s int, st string) Event { return Event{At: at(s), Kind: EventClaimed, Station: st} }
	gateRejected := Event{At: at(2), Kind: EventGateRejected, Station: "review", Actor: "reviewer",
		Data: map[string]any{"reason": "the gate's own words"}}
	gateApproved := Event{At: at(2), Kind: EventGateApproved, Station: "review", Actor: "reviewer"}
	movedOn := Event{At: at(2), Kind: EventMoved, Station: "review", Actor: "engine", Data: map[string]any{"from": "review", "to": "approve"}}
	merged := Event{At: at(5), Kind: EventMerged, Station: "approve", Actor: "ada", Data: person}
	personRejected := Event{At: at(4), Kind: EventRejected, Station: "approve", Actor: "ada", Data: withData(person, "reason", "the tests are thin")}
	personRejectedMove := Event{At: at(4), Kind: EventMoved, Station: "approve", Actor: "ada", Data: withData(person, "from", "approve", "to", "build")}
	personGateRejected := Event{At: at(3), Kind: EventGateRejected, Station: "review", Actor: "ada",
		Data: withData(person, "reason", "not what was asked")}
	personMovedBack := Event{At: at(6), Kind: EventMoved, Station: "done", Actor: "ada",
		Data: withData(person, "from", "done", "to", "review", "reason", "it broke main")}
	engineMovedBack := Event{At: at(3), Kind: EventMoved, Station: "review", Actor: "engine", Data: map[string]any{"from": "review", "to": "build"}}

	tests := []struct {
		name   string
		events []Event
		want   Decision
		reason string
	}{
		{name: "merged with no send-back", events: []Event{claim(0, "review"), gateApproved, movedOn, merged}, want: Approve, reason: "merged"},
		{name: "a person rejected after the gate", events: []Event{claim(0, "review"), gateApproved, movedOn, personRejected, personRejectedMove},
			want: Reject, reason: "the tests are thin"},
		{name: "a person's gate rejection", events: []Event{claim(0, "review"), personGateRejected}, want: Reject, reason: "not what was asked"},
		{name: "a person moved it back after the merge", events: []Event{claim(0, "review"), gateApproved, movedOn, merged, personMovedBack},
			want: Reject, reason: "it broke main"},
		{name: "still open", events: []Event{claim(0, "review"), gateApproved, movedOn}},
		{name: "cancelled", events: []Event{claim(0, "review"), gateApproved, movedOn, {At: at(4), Kind: EventCancelled, Station: "approve", Data: person}}},
		{name: "only the gate agent rejected it", events: []Event{claim(0, "review"), gateRejected, engineMovedBack}},
		{name: "the gate agent's approval alone decides nothing", events: []Event{claim(0, "review"), gateApproved}},
		{name: "a claim before the station ends the window", events: []Event{
			claim(0, "review"), gateRejected, engineMovedBack, claim(4, "build"), {At: at(5), Kind: EventMerged, Station: "approve"},
		}},
		{name: "a claim at the station ends the window", events: []Event{
			claim(0, "review"), gateApproved, movedOn, {At: at(3), Kind: EventMoved, Station: "approve", Data: map[string]any{"from": "approve", "to": "review"}},
			claim(4, "review"), personGateRejected,
		}},
		{name: "a claim after the station does not end it", events: []Event{claim(0, "review"), gateApproved, movedOn, claim(3, "approve"), merged},
			want: Approve, reason: "merged"},
		{name: "the original claim stamped at the run's start", events: []Event{claim(1, "review"), gateApproved, movedOn, merged},
			want: Approve, reason: "merged"},
		{name: "events before the run started are not read", events: []Event{
			{At: at(-5), Kind: EventRejected, Station: "approve", Actor: "ada", Data: withData(person, "reason", "an older version")},
			claim(0, "review"), gateApproved, movedOn, merged,
		}, want: Approve, reason: "merged"},
		{name: "a person's rejection to a station after the gate", events: []Event{
			claim(0, "review"), gateApproved, movedOn,
			{At: at(4), Kind: EventRejected, Station: "done", Actor: "ada", Data: withData(person, "reason", "retitle it")},
			{At: at(4), Kind: EventMoved, Station: "done", Actor: "ada", Data: withData(person, "from", "done", "to", "approve")}, merged,
		}, want: Approve, reason: "merged"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := GateTruthOf(tt.events, line, "review", at(1))
			if got.Decision != tt.want {
				t.Fatalf("decision = %q, want %q (reason %q)", got.Decision, tt.want, got.Reason)
			}

			if tt.want == "" {
				if got.Reason != "" {
					t.Errorf("reason = %q with no answer", got.Reason)
				}

				return
			}

			if !strings.Contains(got.Reason, tt.reason) {
				t.Errorf("reason = %q, want it to contain %q", got.Reason, tt.reason)
			}

			if strings.Contains(got.Reason, "the gate's own words") {
				t.Errorf("reason %q quotes the gate's own reason", got.Reason)
			}
		})
	}
}
