package domain

import (
	"reflect"
	"testing"
	"time"
)

const (
	h            = time.Hour
	stationBuild = "build"
)

func waiting(station string) Component {
	return Component{Kind: ComponentWaiting, Station: station}
}

func working(station string) Component {
	return Component{Kind: ComponentWorking, Station: station}
}

var (
	rework  = Component{Kind: ComponentRework, Station: "review"}
	onYou   = Component{Kind: ComponentWaitingOnYou}
	blocked = Component{Kind: ComponentBlocked, Station: stationBuild}
	other   = Component{Kind: ComponentOther}
	inbox   = waiting("intake")
	build   = working(stationBuild)
	atBuild = waiting(stationBuild)
	target  = 6 * h
)

// item is a done item whose lead time is the sum of its times.
func item(times map[Component]time.Duration) ItemTime {
	var lead time.Duration
	for _, d := range times {
		lead += d
	}

	return ItemTime{LeadTime: lead, Times: times}
}

func sum(b Bridge) time.Duration {
	var total time.Duration
	for _, s := range b.Steps {
		total += s.Duration
	}

	return total
}

func TestBridgeLeadTime_StepsExplainTheGapByExcessOverFastItems(t *testing.T) {
	t.Parallel()

	items := []ItemTime{
		// Fast: on target.
		item(map[Component]time.Duration{atBuild: h, build: 2 * h}),
		item(map[Component]time.Duration{atBuild: h, build: 3 * h}),
		// Slow: they wait in front of build, are sent back by review, wait on a person.
		item(map[Component]time.Duration{atBuild: 5 * h, build: 2 * h, rework: 2 * h}),
		item(map[Component]time.Duration{atBuild: 7 * h, build: 3 * h, onYou: 2 * h}),
		item(map[Component]time.Duration{atBuild: 6 * h, build: 2 * h, rework: 1 * h, onYou: h}),
	}

	// Excess over the fast items' means: build wait 6 - 1 = 5 h, rework 1 h, on you 1 h, working
	// 2.33 - 2.5 < 0 (dropped): 7 h in all, so a 3.5 h gap is split 2.5 / 0.5 / 0.5.
	b := BridgeLeadTime(items, target, target+3*h+30*time.Minute)

	want := []BridgeStep{
		{Component: atBuild, Duration: 2*h + 30*time.Minute, Share: 5.0 / 7, Items: 3},
		{Component: rework, Duration: 30 * time.Minute, Share: 1.0 / 7, Items: 2},
		{Component: onYou, Duration: 30 * time.Minute, Share: 1.0 / 7, Items: 2},
	}
	if !sameSteps(b.Steps, want) {
		t.Errorf("steps = %+v, want %+v", b.Steps, want)
	}

	if got := sum(b); got != 3*h+30*time.Minute {
		t.Errorf("steps sum to %v, want the gap 3h30m", got)
	}
}

func TestBridgeLeadTime_StepsSumExactlyToAnUnevenGap(t *testing.T) {
	t.Parallel()

	items := []ItemTime{
		item(map[Component]time.Duration{inbox: h}),
		item(map[Component]time.Duration{inbox: 7 * h, atBuild: 7 * h, blocked: 7 * h}),
		item(map[Component]time.Duration{inbox: 7 * h, atBuild: 7 * h, blocked: 7 * h}),
		item(map[Component]time.Duration{inbox: 7 * h, atBuild: 7 * h, blocked: 7 * h}),
		item(map[Component]time.Duration{inbox: 7 * h, atBuild: 7 * h, blocked: 7 * h}),
	}

	gap := 10*h + 7*time.Second + 1 // not a multiple of three
	b := BridgeLeadTime(items, target, target+gap)

	if got := sum(b); got != gap || len(b.Steps) != 3 {
		t.Errorf("steps %+v sum to %v, want 3 steps summing to %v", b.Steps, got, gap)
	}
}

func TestBridgeLeadTime_NoStepsOnTargetOrWithFewItems(t *testing.T) {
	t.Parallel()

	five := make([]ItemTime, 5)
	for i := range five {
		five[i] = item(map[Component]time.Duration{inbox: time.Duration(i+5) * h})
	}

	tests := []struct {
		name   string
		items  []ItemTime
		actual time.Duration
	}{
		{name: "on target", items: five, actual: target},
		{name: "under target", items: five, actual: 5 * h},
		{name: "four items", items: five[:4], actual: 8 * h},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if b := BridgeLeadTime(tt.items, target, tt.actual); len(b.Steps) != 0 {
				t.Errorf("steps = %+v, want none", b.Steps)
			}
		})
	}
}

func TestBridgeLeadTime_SmallStepsMergeIntoOther(t *testing.T) {
	t.Parallel()

	items := []ItemTime{
		item(map[Component]time.Duration{atBuild: h}),
		item(map[Component]time.Duration{atBuild: 20 * h, inbox: 20 * time.Minute, blocked: 10 * time.Minute}),
		item(map[Component]time.Duration{atBuild: 20 * h}),
		item(map[Component]time.Duration{atBuild: 20 * h}),
		item(map[Component]time.Duration{atBuild: 20 * h, inbox: 10 * time.Minute}),
	}

	// Excess: build wait 19 h, inbox 7.5 min, blocked 2.5 min. The two small ones are each under
	// 3% of the gap: one other step with the two slow items that had either.
	b := BridgeLeadTime(items, target, 18*h)

	if len(b.Steps) != 2 || b.Steps[0].Component != atBuild || b.Steps[1].Component != other || b.Steps[1].Items != 2 {
		t.Fatalf("steps = %+v, want the build wait then other (2 items)", b.Steps)
	}

	if got := sum(b); got != 12*h {
		t.Errorf("steps sum to %v, want the gap 12h", got)
	}
}

func TestBridgeLeadTime_WithoutFastItemsDelaysCountWhole(t *testing.T) {
	t.Parallel()

	items := make([]ItemTime, 5)
	for i := range items {
		items[i] = item(map[Component]time.Duration{atBuild: 4 * h, build: 6 * h, onYou: 4 * h})
	}

	// Every item is slow: delays count their whole mean (4 h each), working counts nothing.
	b := BridgeLeadTime(items, target, 14*h)

	want := []BridgeStep{
		{Component: atBuild, Duration: 4 * h, Share: 0.5, Items: 5},
		{Component: onYou, Duration: 4 * h, Share: 0.5, Items: 5},
	}
	if !sameSteps(b.Steps, want) {
		t.Errorf("steps = %+v, want %+v", b.Steps, want)
	}
}

func TestComponentOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		span Span
		want Component
	}{
		{Span{Station: "intake", State: StateQueued}, inbox},
		{Span{Station: stationBuild, State: StateReady}, atBuild},
		{Span{Station: stationBuild, State: StateInProgress}, build},
		{Span{Station: stDecide, State: StateWaitingHuman}, onYou},
		{Span{Station: stationBuild, State: StateBlocked}, blocked},
		{Span{Station: stationBuild, State: StateInProgress, ReworkRound: 1, ReworkBy: "review"}, rework},
	}
	for _, tt := range tests {
		if got := ComponentOf(tt.span); got != tt.want {
			t.Errorf("ComponentOf(%+v) = %+v, want %+v", tt.span, got, tt.want)
		}
	}
}

func TestPercentile_InterpolatesBetweenRanks(t *testing.T) {
	t.Parallel()

	ds := []time.Duration{10 * h, h, 4 * h, 2 * h}
	if got := Percentile(ds, 0.85); got != 4*h+198*time.Minute {
		t.Errorf("p85 = %v, want 7h18m as percentile_cont gives", got)
	}

	if got := Percentile(nil, 0.85); got != 0 {
		t.Errorf("p85 of nothing = %v, want 0", got)
	}
}

// sameSteps compares steps with shares to 1e-9.
func sameSteps(got, want []BridgeStep) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		g, w := got[i], want[i]
		if d := g.Share - w.Share; d > 1e-9 || d < -1e-9 {
			return false
		}

		g.Share = w.Share
		if !reflect.DeepEqual(g, w) {
			return false
		}
	}

	return true
}
