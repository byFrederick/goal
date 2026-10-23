package domain

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseVerdict_RejectionNeedsAReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   Decision
		bad    bool
	}{
		{name: "approve", output: `{"decision":"approve"}`, want: Approve},
		{name: "reject with reason", output: `{"decision":"reject","reason":"no evidence"}`, want: Reject},
		{name: "reject without reason", output: `{"decision":"reject"}`, bad: true},
		{name: "unknown decision", output: `{"decision":"maybe"}`, bad: true},
		{name: "not json", output: `ok`, bad: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v, err := ParseVerdict(json.RawMessage(tt.output))
			if tt.bad {
				if !errors.Is(err, ErrBadVerdict) {
					t.Fatalf("err = %v, want ErrBadVerdict", err)
				}

				return
			}

			if err != nil || v.Decision != tt.want {
				t.Fatalf("ParseVerdict = %+v, %v; want %s", v, err, tt.want)
			}
		})
	}
}
