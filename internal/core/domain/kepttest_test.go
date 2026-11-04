package domain

import (
	"errors"
	"testing"
	"time"
)

func TestPassedFirstTime(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	claimed := func(station string) Event { return Event{At: at, Kind: EventClaimed, Station: station} }
	moved := func(from, to string) Event {
		return Event{At: at, Kind: EventMoved, Station: from, Data: map[string]any{"from": from, "to": to}}
	}
	rejected := func(station string) Event { return Event{At: at, Kind: EventGateRejected, Station: station} }
	created := Event{At: at, Kind: EventCreated, Station: "intake"}

	tests := []struct {
		name   string
		events []Event
		want   bool
	}{
		{name: "claimed once and moved on", events: []Event{created, claimed("work"), moved("work", "review")}, want: true},
		{name: "moved on, then passed later stations", events: []Event{
			created, claimed("work"), moved("work", "review"), claimed("review"), rejected("review"), claimed("review"), moved("review", "done"),
		}, want: true},
		{name: "a move with no data, at the station it left", events: []Event{
			claimed("work"), {At: at, Kind: EventMoved, Station: "work"},
		}, want: true},
		{name: "never claimed there", events: []Event{created, claimed("review"), moved("review", "done")}},
		{name: "claimed but not moved on yet", events: []Event{created, claimed("work")}},
		{name: "a gate rejection at the station", events: []Event{claimed("work"), rejected("work"), moved("work", "review")}},
		{name: "sent back and claimed there again", events: []Event{
			claimed("work"), moved("work", "review"), claimed("review"), moved("review", "work"), claimed("work"), moved("work", "review"),
		}},
		{name: "moved on from another station only", events: []Event{claimed("work"), moved("intake", "review")}},
		{name: "no events"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := PassedFirstTime(tc.events, "work"); got != tc.want {
				t.Errorf("PassedFirstTime = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewKeptTest_Check(t *testing.T) {
	t.Parallel()

	ok := NewKeptTest{FactoryID: "f", ItemID: "i", Station: "work", KeptBy: LocalUser, KeptAt: time.Unix(1, 0)}
	if err := ok.Check(); err != nil {
		t.Errorf("Check of a whole kept test = %v", err)
	}

	for name, n := range map[string]NewKeptTest{
		"no item":       {FactoryID: "f", Station: "work", KeptBy: LocalUser, KeptAt: time.Unix(1, 0)},
		"no station":    {FactoryID: "f", ItemID: "i", KeptBy: LocalUser, KeptAt: time.Unix(1, 0)},
		"no keeper":     {FactoryID: "f", ItemID: "i", Station: "work", KeptAt: time.Unix(1, 0)},
		"no time":       {FactoryID: "f", ItemID: "i", Station: "work", KeptBy: LocalUser},
		"long station":  {FactoryID: "f", ItemID: "i", Station: string(make([]byte, 101)), KeptBy: LocalUser, KeptAt: time.Unix(1, 0)},
		"no factory id": {ItemID: "i", Station: "work", KeptBy: LocalUser, KeptAt: time.Unix(1, 0)},
	} {
		if err := n.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("Check with %s = %v, want ErrInvalid", name, err)
		}
	}
}
