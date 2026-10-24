package api

import (
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
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
