package definitions

import (
	"bytes"
	"context"
	"fmt"
	"maps"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Proposed is a published version with a proposal's edits applied to its files: what a try of
// the proposal on a past item runs (ADR 0039 decision 9). It keeps the version's id, organization
// and number, so the try's run records the version the proposal was made against, and it is
// never cached. It serves the gateway's reads of that one version (gateway.Definitions).
type Proposed struct {
	c *Cache
	v *version
}

// Proposed is version id of the organization in ctx with edits applied to its files, in order,
// loaded and checked as a version is: an error wrapping ports.ErrNotFound when the organization
// has no such version, domain.ErrEditDoesNotApply when an edit does not apply to it, and any
// other when the edited files do not load as a definition.
func (c *Cache) Proposed(ctx context.Context, id domain.DefinitionVersionID, edits []domain.ProposalEdit) (*Proposed, error) {
	base, err := c.version(ctx, id)
	if err != nil {
		return nil, err
	}

	v := &version{id: base.id, org: base.org, number: base.number, manifest: maps.Clone(base.manifest), files: maps.Clone(base.files)}
	for _, e := range edits {
		content, ok := v.files[e.Path]

		edited, err := e.Apply(content, ok)
		if err != nil {
			return nil, fmt.Errorf("definition version %s with the proposal's edits: %w", id, err)
		}

		b := []byte(edited)
		v.files[e.Path], v.manifest[e.Path] = b, domain.HashBlob(b)
	}

	if v.factory, err = config.Load(v.files); err != nil {
		return nil, fmt.Errorf("definition version %s with the proposal's edits: %w", id, err)
	}

	if v.factory.Metadata.ID != base.factory.Metadata.ID {
		return nil, fmt.Errorf("definition version %s with the proposal's edits: metadata.id changed", id)
	}

	return &Proposed{c: c, v: v}, nil
}

// Line is the proposed version's line.
func (p *Proposed) Line() domain.Line { return current{version: p.v}.line() }

// Version returns the proposed version's factory; ErrNotFound for another id.
func (p *Proposed) Version(_ context.Context, id domain.DefinitionVersionID) (*config.Factory, error) {
	if err := p.is(id); err != nil {
		return nil, err
	}

	return p.v.factory, nil
}

// AgentFiles is Cache.AgentFiles of the proposed version.
func (p *Proposed) AgentFiles(ctx context.Context, id domain.DefinitionVersionID, agent domain.AgentSpec, output string) (map[string][]byte, error) {
	if err := p.is(id); err != nil {
		return nil, err
	}

	return p.c.agentFiles(ctx, p.v, agent, output)
}

// Rules is Cache.Rules of the proposed version.
func (p *Proposed) Rules(_ context.Context, id domain.DefinitionVersionID, run domain.RuleTarget) ([]domain.RunRule, error) {
	if err := p.is(id); err != nil {
		return nil, err
	}

	return rulesOf(p.v, run)
}

// VersionFiles is Cache.VersionFiles of the proposed version.
func (p *Proposed) VersionFiles(_ context.Context, id domain.DefinitionVersionID) (map[string][]byte, error) {
	if err := p.is(id); err != nil {
		return nil, err
	}

	out := make(map[string][]byte, len(p.v.files))
	for path, b := range p.v.files {
		out[path] = bytes.Clone(b)
	}

	return out, nil
}

func (p *Proposed) is(id domain.DefinitionVersionID) error {
	if id != p.v.id {
		return fmt.Errorf("definition version %s is not the proposed %s: %w", id, p.v.id, ports.ErrNotFound)
	}

	return nil
}
