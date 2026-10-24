package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestItem_BriefFromTheLatestOutput(t *testing.T) {
	t.Parallel()

	l := Line{Stations: []Station{{ID: "probe"}, {ID: "vet"}, {ID: "sign"}, {ID: "closed"}}}
	long := strings.Repeat("x", 121)

	it := Item{
		CostUSD: 0.86,
		Outputs: map[string]json.RawMessage{
			"probe": json.RawMessage(`{"summary": "older", "pull_request": "https://github.com/acme/app/pull/7", "cause": "old"}`),
			"vet": json.RawMessage(`{"summary": " Raise the idle timeout. ", "risk": "Low", "change": "1 line of Terraform",
				"evidence": 3, "reversible": true, "notes": "` + long + `", "links": ["a"], "decision": "pass",
				"reason": "fine", "category": "ok", "lane": "infra", "blank": "  ", "zeta": "last"}`),
		},
	}

	want := ApprovalBrief{
		Summary: "Raise the idle timeout.",
		// Sorted by key; long, blank and non-scalar fields and the verdict's own are left out; four at most.
		Facts:       []Fact{{Key: "change", Value: "1 line of Terraform"}, {Key: "evidence", Value: "3"}, {Key: "reversible", Value: "yes"}, {Key: "risk", Value: "Low"}},
		PullRequest: "https://github.com/acme/app/pull/7",
		SpendUSD:    0.86,
	}

	if got := it.Brief(l); !reflect.DeepEqual(got, want) {
		t.Errorf("Brief = %+v\nwant  %+v", got, want)
	}

	if got := (Item{}).Brief(l); got.Summary != "" || len(got.Facts) != 0 || got.Facts == nil {
		t.Errorf("Brief of an item without outputs = %+v; want nothing, with an empty list of facts", got)
	}
}
