package authoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// SetAgentModel sets agents[].model of agent in the factory's draft to model: a bake-off's
// "Apply to the definition" (ADR 0029 decision 7). It opens a draft from the current version when
// none is open, and never publishes. base is the version the bake-off ran; the write is refused
// with ErrDraftConflict, which then wraps ports.ErrConflict too, when the draft has no such agent
// or the agent's entry under agents (its prompt file's hash included) differs from base's in
// anything but its model.
func (s *Service) SetAgentModel(ctx context.Context, f domain.FactoryID, base domain.DefinitionVersionID, agent, model string) error {
	rec, err := s.record(ctx, f)
	if err != nil {
		return err
	}

	v, err := s.store.Version(ctx, base)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && v.FactoryID != f) {
		return fmt.Errorf("version %s of %s: %w", base, f, ErrVersionNotFound)
	}

	if err != nil {
		return fmt.Errorf("read version %s of %s: %w", base, f, err)
	}

	baseModel, _, err := s.versionModel(s.tree(ctx, v.Manifest))
	if err != nil {
		return err
	}

	_, want := agentEntry(baseModel, agent)
	if want == nil {
		return fmt.Errorf("version %s of %s has no agent %q: %w", base, f, agent, ErrInvalidRequest)
	}

	if rec.Draft == nil {
		if _, _, err := s.OpenDraft(ctx, f, OpenDraftRequest{}); err != nil {
			return err
		}
	}

	value := func(draft Model) (json.RawMessage, error) {
		i, got := agentEntry(draft, agent)
		if got == nil || !sameButModel(got, want) {
			return nil, fmt.Errorf("agent %s of %s changed since version %s: %w", agent, f, base, ErrDraftConflict)
		}

		entry := maps.Clone(got)
		entry["model"] = model

		list, _ := draft.Agents["agents"].([]any)
		list = slices.Clone(list)
		list[i] = entry

		section := maps.Clone(draft.Agents)
		section["agents"] = list

		return json.Marshal(section)
	}

	_, err = s.saveSection(ctx, f, SectionSave{Section: Agents}, value)
	if errors.Is(err, ErrDraftConflict) {
		return fmt.Errorf("set the model of agent %s: %w: %w", agent, err, ports.ErrConflict)
	}

	return err
}

// agentEntry is agent's entry under agents in m and its index; nil when m has none.
func agentEntry(m Model, agent string) (int, map[string]any) {
	list, _ := m.Agents["agents"].([]any)

	for i, a := range list {
		if e, ok := a.(map[string]any); ok && e["name"] == agent {
			return i, e
		}
	}

	return -1, nil
}

// sameButModel reports whether two agent entries are the same but for their model.
func sameButModel(a, b map[string]any) bool {
	a, b = maps.Clone(a), maps.Clone(b)
	delete(a, "model")
	delete(b, "model")

	return reflect.DeepEqual(a, b)
}
