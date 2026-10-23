package domain

import (
	"strconv"
	"strings"
)

// ItemRef names an item the way people do: its factory (name or id) and its per-factory
// number, written <factory>#<number>, as in acme#12.
type ItemRef struct {
	Factory string
	Number  int64
}

// ParseItemRef parses <factory>#<number>. It reports false for anything else, such as an
// item id.
func ParseItemRef(s string) (ItemRef, bool) {
	factory, num, ok := strings.Cut(s, "#")
	if !ok || factory == "" || strings.TrimSpace(factory) != factory {
		return ItemRef{}, false
	}

	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != num {
		return ItemRef{}, false
	}

	return ItemRef{Factory: factory, Number: n}, true
}

// String writes the reference as <factory>#<number>.
func (r ItemRef) String() string { return r.Factory + "#" + strconv.FormatInt(r.Number, 10) }
