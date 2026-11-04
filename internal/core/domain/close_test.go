package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fieldClose is the output field the close tests map.
const fieldClose = "close"

func TestCloseFrom(t *testing.T) {
	t.Parallel()

	reason := "Nothing to do: the index already exists."

	tests := []struct {
		name, field, output, want string
		bad                       bool
	}{
		{name: "closes", field: fieldClose, output: `{"close":"  ` + reason + ` "}`, want: reason},
		{name: "no field mapped", output: `{"close":"` + reason + `"}`},
		{name: "absent", field: fieldClose, output: `{"spec":"x"}`},
		{name: "null", field: fieldClose, output: `{"close":null}`},
		{name: "empty", field: fieldClose, output: `{"close":""}`},
		{name: "whitespace", field: fieldClose, output: `{"close":" \n\t "}`},
		{name: "not an object", field: fieldClose, output: `["close"]`},
		{name: "shortest", field: fieldClose, output: `{"close":"ten chars."}`, want: "ten chars."},
		{name: "longest", field: fieldClose, output: `{"close":"` + strings.Repeat("é", MaxCloseReason) + `"}`, want: strings.Repeat("é", MaxCloseReason)},
		{name: "too short", field: fieldClose, output: `{"close":"no need"}`, bad: true},
		{name: "too long", field: fieldClose, output: `{"close":"` + strings.Repeat("a", MaxCloseReason+1) + `"}`, bad: true},
		{name: "not a string", field: fieldClose, output: `{"close":true}`, bad: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := CloseFrom(json.RawMessage(tt.output), tt.field)
			if tt.bad {
				if !errors.Is(err, ErrBadClose) || !strings.Contains(err.Error(), "ask a person instead") {
					t.Fatalf("CloseFrom = %q, %v; want ErrBadClose telling the agent to ask a person", got, err)
				}

				return
			}

			if err != nil || got != tt.want {
				t.Fatalf("CloseFrom = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestCheckClose(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		item Item
		plan bool
		ok   bool
	}{
		{name: "trigger", item: Item{Origin: OriginTrigger}, ok: true},
		{name: "follow-up", item: Item{Origin: OriginFollowUp}, ok: true},
		{name: "person", item: Item{Origin: OriginPerson}},
		{name: "plan", item: Item{Origin: OriginPlan, ParentID: "a"}},
		{name: "part", item: Item{Origin: OriginFollowUp, ParentID: "a"}},
		{name: "with a plan", item: Item{Origin: OriginTrigger}, plan: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := CheckClose(tt.item, tt.plan)
			if tt.ok {
				if err != nil {
					t.Fatalf("CheckClose = %v; want nil", err)
				}

				return
			}

			if !errors.Is(err, ErrBadClose) || !strings.Contains(err.Error(), "only an item no person filed") ||
				!strings.Contains(err.Error(), "ask a person instead") {
				t.Fatalf("CheckClose = %v; want ErrBadClose naming who may close and to ask a person", err)
			}
		})
	}
}
