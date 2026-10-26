package domain

import (
	"reflect"
	"testing"
)

func TestWaitingFor(t *testing.T) {
	t.Parallel()

	planned := OpenNeed{Kind: EdgePlanned, Item: "a", Number: 1}
	linked := OpenNeed{Kind: EdgeLinked, Item: "b", Number: 2}
	overSQL := OpenNeed{Kind: EdgeOverlap, Item: "c", Number: 3, Paths: []string{"db/queries/*.sql"}}
	overWeb := OpenNeed{Kind: EdgeOverlap, Item: "d", Number: 4, Paths: []string{"web/**", "db/queries/items.sql"}}
	titled := OpenNeed{Kind: EdgeLinked, Item: "e", Number: 5, Title: "Move terraform", Paths: []string{"terraform/**"}}
	titledOver := OpenNeed{Kind: EdgeOverlap, Item: "f", Number: 6, Title: "The API", Paths: []string{"internal/api/board.go"}}

	queued := Item{
		State: StateQueued,
		Paths: []string{"web/src/**", "db/queries/items.sql", "internal/api/**", "db/queries/*.sql"},
	}

	tests := []struct {
		name  string
		item  Item
		needs []OpenNeed
		want  Waiting
		ok    bool
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
				Files: []string{"web/src/**", "db/queries/items.sql", "db/queries/*.sql"},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := WaitingFor(tt.item, tt.needs)
			if ok != tt.ok || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WaitingFor = %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
