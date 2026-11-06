package domain

import (
	"reflect"
	"testing"
)

// The paths and the integration branch the cases of TestWaitingFor share.
const (
	queriesGlob   = "db/queries/*.sql"
	itemsQuery    = "db/queries/items.sql"
	waitingBranch = "goal/a7-x"
)

func TestWaitingFor(t *testing.T) {
	t.Parallel()

	planned := OpenNeed{Kind: EdgePlanned, Item: "a", Number: 1}
	linked := OpenNeed{Kind: EdgeLinked, Item: "b", Number: 2}
	overSQL := OpenNeed{Kind: EdgeOverlap, Item: "c", Number: 3, Paths: []string{queriesGlob}}
	overWeb := OpenNeed{Kind: EdgeOverlap, Item: "d", Number: 4, Paths: []string{"web/**", itemsQuery}}
	titled := OpenNeed{Kind: EdgeLinked, Item: "e", Number: 5, Title: "Move terraform", Paths: []string{"terraform/**"}}
	titledOver := OpenNeed{Kind: EdgeOverlap, Item: "f", Number: 6, Title: "The API", Paths: []string{"internal/api/board.go"}}
	turn := OpenNeed{Kind: EdgeTurn, Item: "g", Number: 7, Title: "The holder", Paths: []string{"cmd/goald/main.go"}}

	queued := Item{
		State: StateQueued,
		Paths: []string{"web/src/**", itemsQuery, "internal/api/**", queriesGlob},
	}

	tests := []struct {
		name   string
		item   Item
		needs  []OpenNeed
		target string
		want   Waiting
		ok     bool
	}{
		{name: "not waiting", item: Item{State: StateInProgress}, needs: []OpenNeed{planned}},
		{name: "no open needs", item: queued},
		{
			name: "after only", item: Item{State: StateReady}, needs: []OpenNeed{linked, planned},
			want: Waiting{Kind: WaitingAfter, Items: []OpenNeed{linked, planned}}, ok: true,
		},
		{
			name: "files only", item: queued, needs: []OpenNeed{overWeb, overSQL},
			want: Waiting{
				Kind: WaitingFiles, Items: []OpenNeed{overWeb, overSQL},
				Files: []string{"web/src/**", itemsQuery, queriesGlob},
			},
			ok: true,
		},
		{
			name: "after wins over files", item: queued, needs: []OpenNeed{overSQL, planned, overWeb, linked},
			want: Waiting{Kind: WaitingAfter, Items: []OpenNeed{planned, linked}}, ok: true,
		},
		{
			name: "after keeps each need's title and paths", item: queued, needs: []OpenNeed{titled, overSQL},
			want: Waiting{Kind: WaitingAfter, Items: []OpenNeed{titled}}, ok: true,
		},
		{
			name: "files keeps each need's title and paths", item: queued, needs: []OpenNeed{titledOver},
			want: Waiting{Kind: WaitingFiles, Items: []OpenNeed{titledOver}, Files: []string{"internal/api/**"}}, ok: true,
		},
		{
			name: "turn only, on an integration branch", item: queued, needs: []OpenNeed{turn}, target: waitingBranch,
			want: Waiting{Kind: WaitingTurn, Items: []OpenNeed{turn}, Target: waitingBranch}, ok: true,
		},
		{
			name: "turn only, on the default branch", item: Item{State: StateReady}, needs: []OpenNeed{turn},
			want: Waiting{Kind: WaitingTurn, Items: []OpenNeed{turn}}, ok: true,
		},
		{
			name: "after wins over turn", item: queued, needs: []OpenNeed{turn, planned}, target: waitingBranch,
			want: Waiting{Kind: WaitingAfter, Items: []OpenNeed{planned}}, ok: true,
		},
		{
			name: "files wins over turn", item: queued, needs: []OpenNeed{turn, overSQL}, target: waitingBranch,
			want: Waiting{
				Kind: WaitingFiles, Items: []OpenNeed{overSQL}, Files: []string{itemsQuery, queriesGlob},
			},
			ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := WaitingFor(tt.item, tt.needs, tt.target)
			if ok != tt.ok || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WaitingFor = %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
