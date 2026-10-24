package authoring

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// revert returns value with the base's value put back at each section-relative pointer of
// edited that pointers names (removed where the base had none). Removals go last-index first,
// so earlier list items keep their pointers.
func revert(value any, edited map[string]any, pointers []string) (json.RawMessage, error) {
	v, err := deepCopy(value)
	if err != nil {
		return nil, err
	}

	ps := slices.Clone(pointers)
	slices.SortFunc(ps, comparePointers)
	slices.Reverse(ps)

	for _, p := range slices.Compact(ps) {
		base, ok := edited[p]
		if !ok {
			continue
		}

		if p == "" {
			v = base

			continue
		}

		v = setPointer(v, splitPointer(p), base, base == nil)
	}

	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode reverted section: %w", err)
	}

	return b, nil
}

// setPointer sets (or removes) the value at toks in v and returns v; a path whose parent does
// not exist leaves v as it is.
func setPointer(v any, toks []string, val any, remove bool) any {
	if len(toks) == 0 {
		return val
	}

	switch c := v.(type) {
	case map[string]any:
		return setInMap(c, toks, val, remove)
	case []any:
		return setInList(c, toks, val, remove)
	}

	return v
}

func setInMap(c map[string]any, toks []string, val any, remove bool) map[string]any {
	tok, rest := toks[0], toks[1:]

	switch child, ok := c[tok]; {
	case len(rest) > 0 && ok:
		c[tok] = setPointer(child, rest, val, remove)
	case len(rest) > 0:
	case remove:
		delete(c, tok)
	default:
		c[tok] = val
	}

	return c
}

func setInList(c []any, toks []string, val any, remove bool) []any {
	i, err := strconv.Atoi(toks[0])
	if err != nil || i < 0 || i > len(c) {
		return c
	}

	rest := toks[1:]

	switch {
	case len(rest) > 0 && i < len(c):
		c[i] = setPointer(c[i], rest, val, remove)
	case len(rest) > 0:
	case remove && i < len(c):
		return slices.Delete(c, i, i+1)
	case !remove && i == len(c):
		return append(c, val)
	case !remove:
		c[i] = val
	}

	return c
}

// comparePointers orders pointers token by token, list indexes numerically.
func comparePointers(a, b string) int {
	ta, tb := splitPointer(a), splitPointer(b)

	for i := range min(len(ta), len(tb)) {
		if ta[i] == tb[i] {
			continue
		}

		na, ea := strconv.Atoi(ta[i])
		nb, eb := strconv.Atoi(tb[i])

		if ea == nil && eb == nil {
			return na - nb
		}

		return strings.Compare(ta[i], tb[i])
	}

	return len(ta) - len(tb)
}

func splitPointer(p string) []string {
	if p == "" {
		return nil
	}

	toks := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, t := range toks {
		toks[i] = strings.NewReplacer("~1", "/", "~0", "~").Replace(t)
	}

	return toks
}

// deepCopy copies a model value, numbers kept as json.Number.
func deepCopy(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("copy section: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()

	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("copy section: %w", err)
	}

	return out, nil
}
