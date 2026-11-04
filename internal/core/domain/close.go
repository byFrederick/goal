package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MinCloseReason and MaxCloseReason bound, in characters, the trimmed close an output gives to
// close its item (ADR 0040 decision 11).
const (
	MinCloseReason = 10
	MaxCloseReason = 500
)

// ErrBadClose is an output's close that goald refuses: it fails the output's check.
var ErrBadClose = errors.New("bad close")

// askInstead ends every refusal of a close, so the agent knows what to do instead (ADR 0018).
const askInstead = "only an item no person filed (from a trigger or a follow-up) that is not a part can be closed; " +
	"if this item needs nothing, ask a person instead (ADR 0018)"

// CloseFrom reads the close of an accepted output from its field (board.flow.outputs.close),
// trimmed: "" when the line maps no field, the output has none, or it is null or only whitespace.
// A close that is not a string, or is shorter than MinCloseReason or longer than MaxCloseReason
// characters, is ErrBadClose.
func CloseFrom(output json.RawMessage, field string) (string, error) {
	if field == "" {
		return "", nil
	}

	var v map[string]json.RawMessage
	if json.Unmarshal(output, &v) != nil {
		return "", nil
	}

	raw, ok := v[field]
	if !ok || string(raw) == "null" {
		return "", nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("%s is not a string; %s: %w", field, askInstead, ErrBadClose)
	}

	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); s != "" && (n < MinCloseReason || n > MaxCloseReason) {
		return "", fmt.Errorf("%s has %d characters, not %d to %d; %s: %w", field, n, MinCloseReason, MaxCloseReason, askInstead, ErrBadClose)
	}

	return s, nil
}

// CheckClose reports whether it may be closed by its agent's output, which has a plan when plan:
// only an item whose origin is trigger or follow-up, never a part, and never together with a plan.
func CheckClose(it Item, plan bool) error {
	switch {
	case it.ParentID != "":
		return fmt.Errorf("a part cannot be closed; %s: %w", askInstead, ErrBadClose)
	case it.Origin != OriginTrigger && it.Origin != OriginFollowUp:
		return fmt.Errorf("an item whose origin is %q cannot be closed; %s: %w", it.Origin, askInstead, ErrBadClose)
	case plan:
		return fmt.Errorf("an output cannot both close its item and plan parts; %s: %w", askInstead, ErrBadClose)
	}

	return nil
}
