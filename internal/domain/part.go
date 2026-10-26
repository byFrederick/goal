package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// NewPart is one slice of a plan: a part of the assembly the planning item becomes. Key names
// it within the plan (retries match parts by it), Brief is the part's body, Paths the files it
// plans to change and Needs the keys of the parts it waits for (planned edges).
type NewPart struct {
	Key   string
	Title string
	Lane  string
	Brief string
	Paths []string
	Needs []string
}

// ErrBadPlan means a station's output has a plan that cannot become an assembly: not the
// platform's shape, or refused by ValidatePlan.
var ErrBadPlan = errors.New("bad plan")

// PlanFrom reads the "plan" a station's output declares, the platform-defined field that makes
// the item an assembly: [{key, title, lane, brief, paths, needs}]. It returns nil, nil when the
// output has no plan (absent, null or empty) and ErrBadPlan, wrapping ValidatePlan's error when
// it refused the plan, when the plan is malformed.
func PlanFrom(output json.RawMessage) ([]NewPart, error) {
	var v struct {
		Plan []struct {
			Key   string   `json:"key"`
			Title string   `json:"title"`
			Lane  string   `json:"lane"`
			Brief string   `json:"brief"`
			Paths []string `json:"paths"`
			Needs []string `json:"needs"`
		} `json:"plan"`
	}

	if err := json.Unmarshal(output, &v); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadPlan, err)
	}

	if len(v.Plan) == 0 {
		return nil, nil
	}

	parts := make([]NewPart, len(v.Plan))
	for i, p := range v.Plan {
		parts[i] = NewPart{Key: p.Key, Title: p.Title, Lane: p.Lane, Brief: p.Brief, Paths: p.Paths, Needs: p.Needs}
	}

	if err := ValidatePlan(parts); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadPlan, err)
	}

	return parts, nil
}

// Parts are an assembly's parts in plan order, with how many are done of how many.
type Parts struct {
	Items []Item
	Done  int
	Total int
}

// ValidatePlan checks a plan before any part is created: at least one part, every part with a
// key and a title, keys unique, each need naming another part's key once. It returns ErrInvalid
// for those and ErrCycle when the needs make a part wait for itself, directly or through others.
func ValidatePlan(parts []NewPart) error {
	if len(parts) == 0 {
		return fmt.Errorf("plan has no parts: %w", ErrInvalid)
	}

	keys := make(map[string]bool, len(parts))

	for i, p := range parts {
		switch {
		case strings.TrimSpace(p.Key) == "":
			return fmt.Errorf("part %d has no key: %w", i+1, ErrInvalid)
		case strings.TrimSpace(p.Title) == "":
			return fmt.Errorf("part %q has no title: %w", p.Key, ErrInvalid)
		case keys[p.Key]:
			return fmt.Errorf("part key %q twice: %w", p.Key, ErrInvalid)
		}

		keys[p.Key] = true
	}

	for _, p := range parts {
		seen := make(map[string]bool, len(p.Needs))

		for _, n := range p.Needs {
			switch {
			case !keys[n]:
				return fmt.Errorf("part %q needs unknown part %q: %w", p.Key, n, ErrInvalid)
			case seen[n]:
				return fmt.Errorf("part %q needs %q twice: %w", p.Key, n, ErrInvalid)
			}

			seen[n] = true
		}
	}

	return planCycle(parts)
}

// planCycle returns ErrCycle unless the parts can be ordered so each comes after what it needs
// (Kahn's algorithm: repeatedly take the parts whose needs are all taken).
func planCycle(parts []NewPart) error {
	waiting := make(map[string]int, len(parts))
	neededBy := make(map[string][]string, len(parts))

	var ready []string

	for _, p := range parts {
		waiting[p.Key] = len(p.Needs)
		for _, n := range p.Needs {
			neededBy[n] = append(neededBy[n], p.Key)
		}

		if len(p.Needs) == 0 {
			ready = append(ready, p.Key)
		}
	}

	taken := 0

	for len(ready) > 0 {
		k := ready[len(ready)-1]
		ready = ready[:len(ready)-1]
		taken++

		for _, d := range neededBy[k] {
			if waiting[d]--; waiting[d] == 0 {
				ready = append(ready, d)
			}
		}
	}

	if taken < len(parts) {
		return fmt.Errorf("plan's needs: %w", ErrCycle)
	}

	return nil
}

// PartWaves gives each part of an assembly its wave in the plan: 0 when it needs no other part,
// otherwise one more than the largest wave among the parts it needs. needs maps each part to
// the parts it needs; needs outside the map (items that are not parts) are ignored, and a
// cycle, which a validated plan never has, is cut where it closes.
func PartWaves(needs map[string][]string) map[string]int {
	waves := make(map[string]int, len(needs))
	visiting := make(map[string]bool, len(needs))

	var wave func(string) int

	wave = func(k string) int {
		if w, ok := waves[k]; ok {
			return w
		}

		visiting[k] = true
		w := 0

		for _, n := range needs[k] {
			if _, part := needs[n]; !part || visiting[n] {
				continue
			}

			w = max(w, wave(n)+1)
		}

		visiting[k] = false
		waves[k] = w

		return w
	}

	for k := range needs {
		wave(k)
	}

	return waves
}
