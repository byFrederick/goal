package factorymodel

import (
	"strconv"
	"strings"
)

// Edited lists what a draft changed in one section against its base, for the editor's
// "Edited · Undo" markers: section-relative pointers into the draft's value ("/stations/3/checks")
// mapped to the base's value there, nil where the base has none. Items of identified lists are
// matched by identity, so a moved station is not edited; a new item is one pointer. A field the
// draft removed is listed at the pointer it had in the base's item.
func Edited(base, draft Model, s Section) map[string]any {
	out := map[string]any{}

	if s == Connections {
		editedList(base.Connections, draft.Connections, connectionsShape, "", out)

		return out
	}

	bm, _ := base.Section(s).(map[string]any)
	dm, _ := draft.Section(s).(map[string]any)

	for _, bind := range sections[s] {
		p := "/" + bind.key
		bv, dv := bm[bind.key], dm[bind.key]

		bl, bIsList := bv.([]any)
		dl, dIsList := dv.([]any)

		if bind.shape.identity != nil && bIsList && dIsList {
			editedList(bl, dl, bind.shape, p, out)

			continue
		}

		editedValue(bv, dv, bind.shape, p, out)
	}

	return out
}

func editedList(base, draft []any, s *shape, p string, out map[string]any) {
	id := func(v any) string { return s.identityOf(normalize(v, s.elem)) }

	for j, d := range draft {
		ip := p + "/" + strconv.Itoa(j)
		i := indexOfID(base, id(d), id)

		if i < 0 {
			out[ip] = nil

			continue
		}

		editedValue(base[i], d, s.elem, ip, out)
	}
}

func indexOfID(items []any, want string, id func(any) string) int {
	if want == "" {
		return -1
	}

	for i, it := range items {
		if id(it) == want {
			return i
		}
	}

	return -1
}

// editedValue records p when base and draft differ there: inside objects it descends to the
// fields that differ; lists and scalars are recorded whole.
func editedValue(base, draft any, s *shape, p string, out map[string]any) {
	if equal(normalize(base, s), normalize(draft, s)) {
		return
	}

	bm, bok := base.(map[string]any)
	dm, dok := draft.(map[string]any)

	if !bok || !dok || s.kind == promptRef || s.kind == runMode {
		out[p] = base

		return
	}

	for _, k := range unionKeys(bm, dm) {
		if readOnlyKey(s, k) {
			continue
		}

		editedValue(bm[k], dm[k], s.child(s.yamlKey(k)), p+"/"+escapeToken(k), out)
	}
}

// escapeToken escapes a key for a JSON pointer.
func escapeToken(k string) string {
	return strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
}
