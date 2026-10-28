package config

import "slices"

// Starter is factory.yaml's starter section (ADR 0032).
type Starter struct {
	Item StarterItem `json:"item"`
}

// StarterItem is the first item onboarding submits, once, to a factory that has no items yet.
type StarterItem struct {
	Title string `json:"title"`
	Type  string `json:"type,omitempty"`
	Body  string `json:"body,omitempty"`
}

// starter checks that starter.item.type is one of the definition's work types. The schema has
// already checked the item's shape and its title.
func (c *checker) starter() {
	s := c.f.Starter
	if s == nil || s.Item.Type == "" || slices.Contains(c.f.Spec.Board.WorkTypes, s.Item.Type) {
		return
	}

	c.errorf(checkReferences, codeUnknownWorkType, []string{"starter", "item", "type"},
		"starter.item.type %q is not one of board.workTypes", s.Item.Type)
}
