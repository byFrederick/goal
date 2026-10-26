package domain_test

import (
	"reflect"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
)

func TestWaitingFor(t *testing.T) {
	t.Parallel()

	planned := domain.OpenNeed{Kind: domain.EdgePlanned, Item: "a", Number: 1}
	linked := domain.OpenNeed{Kind: domain.EdgeLinked, Item: "b", Number: 2}
	overSQL := domain.OpenNeed{Kind: domain.EdgeOverlap, Item: "c", Number: 3, Paths: []string{"queries/*.sql"}}
	overWeb := domain.OpenNeed{Kind: domain.EdgeOverlap, Item: "d", Number: 4, Paths: []string{"web/**", "queries/items.sql"}}

	queued := domain.Item{
		State: domain.StateQueued,
		Paths: []string{"web/src/**", "queries/items.sql", "internal/api/**", "queries/*.sql"},
	}

	tests := []struct {
		name  string
		item  domain.Item
		needs []domain.OpenNeed
		want  domain.Waiting
		ok    bool
	}{
		{name: "not waiting", item: domain.Item{State: domain.StateInProgress}, needs: []domain.OpenNeed{planned}},
		{name: "no open needs", item: queued},
		{
			name: "after only", item: domain.Item{State: domain.StateReady}, needs: []domain.OpenNeed{linked, planned},
			want: domain.Waiting{Kind: domain.WaitingAfter, Items: []domain.OpenNeed{linked, planned}}, ok: true,
		},
		{
			name: "files only", item: queued, needs: []domain.OpenNeed{overWeb, overSQL},
			want: domain.Waiting{
				Kind: domain.WaitingFiles, Items: []domain.OpenNeed{overWeb, overSQL},
				Files: []string{"web/src/**", "queries/items.sql", "queries/*.sql"},
			},
			ok: true,
		},
		{
			name: "after wins over files", item: queued, needs: []domain.OpenNeed{overSQL, planned, overWeb, linked},
			want: domain.Waiting{Kind: domain.WaitingAfter, Items: []domain.OpenNeed{planned, linked}}, ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := domain.WaitingFor(tt.item, tt.needs)
			if ok != tt.ok || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WaitingFor = %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
