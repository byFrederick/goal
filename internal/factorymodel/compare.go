package factorymodel

import (
	"encoding/json"
	"math/big"
	"strings"
)

// normalizeSection is a section's value as compared (see normalize).
func normalizeSection(s Section, v any) any {
	if s == Connections {
		return normalize(v, connectionsShape)
	}

	m, ok := v.(map[string]any)
	if !ok {
		return v
	}

	out := make(map[string]any, len(m))

	for _, b := range sections[s] {
		if n := normalize(m[b.key], b.shape); !empty(n) {
			out[b.key] = n
		}
	}

	return out
}

// normalize is a model value at a position of shape s as compared: nulls and empty lists are
// absent; read-only parts (a prompt's sha256 and words, an agent's custom_tools, a connection's
// legacy) are left out; a prompt or run mode written as a string is its object form; a
// connection's empty settings are absent and its name defaults to its connector's.
func normalize(v any, s *shape) any {
	if str, ok := v.(string); ok {
		switch s.kind {
		case promptRef:
			v = map[string]any{"path": str}
		case runMode:
			v = map[string]any{"mode": str}
		case generic, agentItem, connectItem:
		}
	}

	switch x := v.(type) {
	case map[string]any:
		return normalizeMap(x, s)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalize(e, s.child(""))
		}

		return out
	}

	return v
}

func normalizeMap(x map[string]any, s *shape) map[string]any {
	out := make(map[string]any, len(x))

	for k, val := range x {
		if readOnlyKey(s, k) {
			continue
		}

		n := normalize(val, s.child(s.yamlKey(k)))
		if empty(n) || s.kind == connectItem && k == keySettings && isEmptyObject(n) {
			continue
		}

		out[k] = n
	}

	if s.kind == connectItem {
		if _, named := out[keyName]; !named {
			if c, ok := out[keyConnector].(string); ok {
				out[keyName], _, _ = strings.Cut(c, "@")
			}
		}
	}

	return out
}

func readOnlyKey(s *shape, k string) bool {
	switch s.kind {
	case promptRef:
		return k == "sha256" || k == "words"
	case agentItem:
		return k == "custom_tools"
	case connectItem:
		return k == keyLegacy
	case generic, runMode:
	}

	return false
}

// empty is a value that writes nothing: null or an empty list.
func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case []any:
		return len(x) == 0
	}

	return false
}

func isEmptyObject(v any) bool {
	m, ok := v.(map[string]any)

	return ok && len(m) == 0
}

// equal compares two model values; numbers compare by value.
func equal(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)

		return ok && equalMaps(x, y)
	case []any:
		y, ok := b.([]any)

		return ok && equalLists(x, y)
	case json.Number:
		y, ok := b.(json.Number)

		return ok && equalNumbers(x, y)
	}

	return a == b
}

func equalMaps(x, y map[string]any) bool {
	if len(x) != len(y) {
		return false
	}

	for k, v := range x {
		if w, ok := y[k]; !ok || !equal(v, w) {
			return false
		}
	}

	return true
}

func equalLists(x, y []any) bool {
	if len(x) != len(y) {
		return false
	}

	for i := range x {
		if !equal(x[i], y[i]) {
			return false
		}
	}

	return true
}

func equalNumbers(x, y json.Number) bool {
	rx, okx := new(big.Rat).SetString(x.String())
	ry, oky := new(big.Rat).SetString(y.String())

	return okx && oky && rx.Cmp(ry) == 0
}
