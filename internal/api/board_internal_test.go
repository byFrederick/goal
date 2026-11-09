package api

import (
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// A station with a gate shows the share of its verdicts that sent work back, and nothing before
// its first verdict; a station without a gate never does.
func TestBoardStats_SendBackShareOfGateStations(t *testing.T) {
	t.Parallel()

	gate := &domain.Gate{OnReject: "build"}
	b := boardStats{times: map[string]domain.StationTimes{
		"review": {Station: "review", Verdicts: 10, Rejected: 2},
		"check":  {Station: "check", Verdicts: 4, Rejected: 1},
	}}

	for _, tt := range []struct {
		name string
		st   domain.Station
		want *float64
	}{
		{"gate with verdicts", domain.Station{ID: "review", Kind: domain.KindAgent, Gate: gate}, new(0.2)},
		{"gate without verdicts", domain.Station{ID: "audit", Kind: domain.KindAgent, Gate: gate}, nil},
		{"no gate", domain.Station{ID: "check", Kind: domain.KindAgent}, nil},
	} {
		var bs gen.BoardStation

		b.fill(tt.st, &bs)

		if (bs.SendBackRatio == nil) != (tt.want == nil) || (tt.want != nil && *bs.SendBackRatio != *tt.want) {
			t.Errorf("%s: send_back_ratio = %v, want %v", tt.name, bs.SendBackRatio, tt.want)
		}
	}
}

// The station and agent of TestAPI_BoardLanesKeptSlots.
const (
	slotStation = "build"
	slotBackend = "backend"
)

// An agent lane's kept slots are its counted lanes' slots of SlotLanes, cut off at the capacity,
// and its kept_busy its leases in them; its shared slots are the station's uncounted ones, when it
// works a lane without a count or is the station's one agent, and shared_busy every agent's
// leases in them.
func TestAPI_BoardLanesKeptSlots(t *testing.T) {
	t.Parallel()

	routed := domain.Station{
		ID: slotStation, Kind: domain.KindAgent, Agents: 3,
		Route: map[string]string{"api": slotBackend, "web": "frontend"}, RouteAgents: map[string]int{"api": 2},
	}
	leases := []domain.Lease{
		{Station: slotStation, Agent: slotBackend, Slot: 0},
		{Station: slotStation, Agent: slotBackend, Slot: 1},
		{Station: slotStation, Agent: "frontend", Slot: 2},
		{Station: "deploy", Agent: slotBackend, Slot: 2}, // another station's
	}

	for _, tt := range []struct {
		name   string
		st     domain.Station
		agent  string
		leases []domain.Lease
		want   laneSlots
	}{
		{"counted lane", routed, slotBackend, leases, laneSlots{kept: 2, keptBusy: 2}},
		{"lane without a count", routed, "frontend", leases, laneSlots{shared: 1, sharedBusy: 1}},
		{
			"count past the capacity",
			domain.Station{ID: slotStation, Kind: domain.KindAgent, Agents: 3, WIP: 2, Route: routed.Route, RouteAgents: map[string]int{"api": 5}},
			slotBackend, nil,
			laneSlots{kept: 2},
		},
		{
			"one agent",
			domain.Station{ID: slotStation, Kind: domain.KindAgent, Agent: slotBackend, Agents: 3},
			slotBackend,
			[]domain.Lease{{Station: slotStation, Agent: slotBackend, Slot: 1}},
			laneSlots{shared: 3, sharedBusy: 1},
		},
	} {
		if got := slotsOfLane(tt.st, tt.agent, tt.leases); got != tt.want {
			t.Errorf("%s: slots = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}
