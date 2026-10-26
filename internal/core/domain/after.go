package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// ErrBadAfter means a station's output names items to follow (AfterFrom) that the work cannot
// follow: not a list of item refs, or a ref that is of another factory, unknown, closed or would
// close a cycle.
var ErrBadAfter = errors.New("bad after")

// AfterFrom reads the item refs (<factory>#<number>) a station's output lists in field, the
// output property the definition maps to board.flow.dependencies.after: the open items outside
// the plan that this work must follow. It returns nil, nil when field is "" (no property mapped)
// and when the property is absent, null or empty; a ref listed twice is returned once. A value
// that is not an array of strings, or an entry that is not an item ref, is ErrBadAfter naming it.
func AfterFrom(output json.RawMessage, field string) ([]ItemRef, error) {
	if field == "" {
		return nil, nil
	}

	var v map[string]json.RawMessage
	if err := json.Unmarshal(output, &v); err != nil {
		return nil, fmt.Errorf("%w: output %s: %w", ErrBadAfter, field, err)
	}

	var raw []string
	if err := json.Unmarshal(v[field], &raw); v[field] != nil && err != nil {
		return nil, fmt.Errorf("%w: %s is not an array of item refs", ErrBadAfter, field)
	}

	var refs []ItemRef

	for _, s := range raw {
		ref, ok := ParseItemRef(s)
		if !ok {
			return nil, fmt.Errorf("%w: %s entry %q is not an item ref <factory>#<number>", ErrBadAfter, field, s)
		}

		if !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}

	return refs, nil
}
