package factorymodel

import (
	"fmt"
	"slices"
)

// checkValue refuses a section value of the wrong type and object keys the model cannot have:
// keys the section does not own, and in a renamed position keys that do not read back as
// themselves (snake_case).
func checkValue(s Section, v any) error {
	if err := checkSectionType(s, v); err != nil {
		return err
	}

	if m, ok := v.(map[string]any); ok {
		for k := range m {
			if !slices.ContainsFunc(sections[s], func(b binding) bool { return b.key == k }) {
				return fmt.Errorf("%w: %s has no %s", ErrModelInvalid, s, k)
			}
		}
	}

	for _, b := range sections[s] {
		val, _ := part(v, b)
		if err := checkKeys(val, b.shape, b.key); err != nil {
			return err
		}
	}

	return nil
}

func checkSectionType(s Section, v any) error {
	switch s {
	case Connections:
		if _, ok := v.([]any); !ok {
			return fmt.Errorf("%w: connections is a list", ErrModelInvalid)
		}
	case Basics, Line, Agents, Review:
		if !isMap(v) {
			return fmt.Errorf("%w: %s is an object", ErrModelInvalid, s)
		}
	default:
		return fmt.Errorf("%w: unknown section %q", ErrModelInvalid, s)
	}

	return nil
}

func checkKeys(v any, s *shape, at string) error {
	if v != nil && !specialFormOK(v, s) {
		return fmt.Errorf("%w: %s has the wrong form", ErrModelInvalid, at)
	}

	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			if !s.verbatim && s.kind != connectItem && snake(camel(k)) != k {
				return fmt.Errorf("%w: %s.%s is not a model key", ErrModelInvalid, at, k)
			}

			if err := checkKeys(c, s.child(s.yamlKey(k)), at+"."+k); err != nil {
				return err
			}
		}
	case []any:
		for _, c := range x {
			if err := checkKeys(c, s.child(""), at); err != nil {
				return err
			}
		}
	}

	return nil
}

// specialFormOK checks the positions whose model form is not the YAML's: a prompt is a path or
// {path, sha256, words}, a run mode a string or {mode, commands}, a connection has only its own
// keys and an object of settings that are not among them.
func specialFormOK(v any, s *shape) bool {
	switch s.kind {
	case promptRef:
		_, str := v.(string)

		return str || isPromptRef(v)
	case runMode:
		_, str := v.(string)
		m, _ := v.(map[string]any)
		_, mode := m["mode"].(string)

		return str || mode
	case connectItem:
		return connectionFormOK(v)
	case generic, agentItem:
	}

	return true
}

func connectionFormOK(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return true // a value the schema rejects is written as it is
	}

	for k := range m {
		if !slices.Contains(connectionKeys, k) && k != keySettings && k != keyLegacy {
			return false
		}
	}

	settings, ok := m[keySettings]
	if !ok || settings == nil {
		return true
	}

	sm, ok := settings.(map[string]any)

	return ok && !slices.ContainsFunc(connectionKeys, func(k string) bool {
		_, clash := sm[k]

		return clash
	})
}

func isMap(v any) bool {
	_, ok := v.(map[string]any)

	return ok
}

// isPromptRef reports whether v is a prompt as the model writes it: {path, sha256?, words?}.
func isPromptRef(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}

	if _, path := m["path"].(string); !path {
		return false
	}

	for k := range m {
		if k != "path" && k != "sha256" && k != "words" {
			return false
		}
	}

	return true
}
