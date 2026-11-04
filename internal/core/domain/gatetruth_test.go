package domain

import (
	"maps"
	"strings"
	"testing"
	"time"
)

// The stations and people of the gate-truth tests.
const (
	gtWork, gtGate, gtSignoff, gtShipped = "write", "check", "signoff", "shipped"
	gtPerson, gtGateAgent                = "grace", "checker"
)

func TestGateTruth(t *testing.T) {
	t.Parallel()

	line := Line{Stations: []Station{
		{ID: "queue", Kind: KindQueue},
		{ID: gtWork, Kind: KindAgent},
		{ID: gtGate, Kind: KindAgent, Gate: &Gate{OnReject: gtWork}},
		{ID: gtSignoff, Kind: KindHuman},
		{ID: gtShipped, Kind: KindDone},
	}}
	start := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return start.Add(time.Duration(s) * time.Second) }
	person := map[string]any{evKeyActor: "user:grace"}
	withData := func(d map[string]any, kv ...string) map[string]any {
		out := map[string]any{}
		maps.Copy(out, d)

		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i]] = kv[i+1]
		}

		return out
	}

	claim := func(s int, st string) Event { return Event{At: at(s), Kind: EventClaimed, Station: st} }
	gateRejected := Event{
		At: at(2), Kind: EventGateRejected, Station: gtGate, Actor: gtGateAgent,
		Data: map[string]any{evKeyReason: "the gate's own words"},
	}
	gateApproved := Event{At: at(2), Kind: EventGateApproved, Station: gtGate, Actor: gtGateAgent}
	movedOn := Event{At: at(2), Kind: EventMoved, Station: gtGate, Actor: ActorEngine, Data: map[string]any{evKeyFrom: gtGate, evKeyTo: gtSignoff}}
	merged := Event{At: at(5), Kind: EventMerged, Station: gtSignoff, Actor: gtPerson, Data: person}
	personRejected := Event{At: at(4), Kind: EventRejected, Station: gtSignoff, Actor: gtPerson, Data: withData(person, evKeyReason, "the tests are thin")}
	personRejectedMove := Event{At: at(4), Kind: EventMoved, Station: gtSignoff, Actor: gtPerson, Data: withData(person, evKeyFrom, gtSignoff, evKeyTo, gtWork)}
	personGateRejected := Event{
		At: at(3), Kind: EventGateRejected, Station: gtGate, Actor: gtPerson,
		Data: withData(person, evKeyReason, "not what was asked"),
	}
	personMovedBack := Event{
		At: at(6), Kind: EventMoved, Station: gtShipped, Actor: gtPerson,
		Data: withData(person, evKeyFrom, gtShipped, evKeyTo, gtGate, evKeyReason, "it broke main"),
	}
	engineMovedBack := Event{At: at(3), Kind: EventMoved, Station: gtGate, Actor: ActorEngine, Data: map[string]any{evKeyFrom: gtGate, evKeyTo: gtWork}}
	personMovedForward := Event{At: at(4), Kind: EventMoved, Station: gtWork, Actor: gtPerson, Data: withData(person, evKeyFrom, gtWork, evKeyTo, gtGate, evKeyReason, "the gate was wrong")}

	tests := []struct {
		name   string
		events []Event
		want   Decision
		reason string
	}{
		{name: "merged with no send-back", events: []Event{claim(0, gtGate), gateApproved, movedOn, merged}, want: Approve, reason: string(EventMerged)},
		{
			name: "a person rejected after the gate", events: []Event{claim(0, gtGate), gateApproved, movedOn, personRejected, personRejectedMove},
			want: Reject, reason: "the tests are thin",
		},
		{name: "a person's gate rejection", events: []Event{claim(0, gtGate), personGateRejected}, want: Reject, reason: "not what was asked"},
		{
			name: "a person moved it back after the merge", events: []Event{claim(0, gtGate), gateApproved, movedOn, merged, personMovedBack},
			want: Reject, reason: "it broke main",
		},
		{name: "still open", events: []Event{claim(0, gtGate), gateApproved, movedOn}},
		{name: "cancelled", events: []Event{claim(0, gtGate), gateApproved, movedOn, {At: at(4), Kind: EventCancelled, Station: gtSignoff, Data: person}}},
		{name: "only the gate agent rejected it", events: []Event{claim(0, gtGate), gateRejected, engineMovedBack}},
		{name: "the gate agent's approval alone decides nothing", events: []Event{claim(0, gtGate), gateApproved}},
		{name: "a claim before the station ends the window", events: []Event{
			claim(0, gtGate), gateRejected, engineMovedBack, claim(4, gtWork), {At: at(5), Kind: EventMerged, Station: gtSignoff},
		}},
		{name: "a claim at the station ends the window", events: []Event{
			claim(0, gtGate), gateApproved, movedOn,
			{At: at(3), Kind: EventMoved, Station: gtSignoff, Data: map[string]any{evKeyFrom: gtSignoff, evKeyTo: gtGate}},
			claim(4, gtGate), personGateRejected,
		}},
		{
			name: "a claim after the station does not end it", events: []Event{claim(0, gtGate), gateApproved, movedOn, claim(3, gtSignoff), merged},
			want: Approve, reason: string(EventMerged),
		},
		{
			name: "the original claim stamped at the run's start", events: []Event{claim(1, gtGate), gateApproved, movedOn, merged},
			want: Approve, reason: string(EventMerged),
		},
		{name: "events before the run started are not read", events: []Event{
			{At: at(-5), Kind: EventRejected, Station: gtSignoff, Actor: gtPerson, Data: withData(person, evKeyReason, "an older version")},
			claim(0, gtGate), gateApproved, movedOn, merged,
		}, want: Approve, reason: string(EventMerged)},
		{name: "a person's rejection to a station after the gate", events: []Event{
			claim(0, gtGate), gateApproved, movedOn,
			{At: at(4), Kind: EventRejected, Station: gtShipped, Actor: gtPerson, Data: withData(person, evKeyReason, "retitle it")},
			{At: at(4), Kind: EventMoved, Station: gtShipped, Actor: gtPerson, Data: withData(person, evKeyFrom, gtShipped, evKeyTo, gtSignoff)},
			merged,
		}, want: Approve, reason: string(EventMerged)},
		{name: "a person moved it forward to the gate after the gate rejected it", events: []Event{claim(0, gtGate), gateRejected, engineMovedBack, personMovedForward, claim(5, gtGate)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := GateTruthOf(tt.events, line, gtGate, at(1))
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
