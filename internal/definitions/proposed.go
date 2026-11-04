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

// Proposed is a published version with a proposal's files written over its own: what a try of
// the proposal on a past item runs (ADR 0039 decision 9). It keeps the version's id, organization
// and number, so the try's run records the version the proposal was made against, and it is
// never cached. It serves the gateway's reads of that one version (gateway.Definitions).
type Proposed struct {
	c *Cache
	v *version
}

// Proposed is version id of the organization in ctx with files written over its files, loaded and
// checked as a version is: an error wrapping ports.ErrNotFound when the organization has no such
// version, and any other when the files do not load as a definition.
func (c *Cache) Proposed(ctx context.Context, id domain.DefinitionVersionID, files map[string][]byte) (*Proposed, error) {
	base, err := c.version(ctx, id)
	if err != nil {
		return nil, err
	}

	v := &version{id: base.id, org: base.org, number: base.number, manifest: maps.Clone(base.manifest), files: maps.Clone(base.files)}
	for p, b := range files {
		v.files[p], v.manifest[p] = bytes.Clone(b), domain.HashBlob(b)
	}

	if v.factory, err = config.Load(v.files); err != nil {
		return nil, fmt.Errorf("definition version %s with the proposal's files: %w", id, err)
	}

	if v.factory.Metadata.ID != base.factory.Metadata.ID {
		return nil, fmt.Errorf("definition version %s with the proposal's files: metadata.id changed", id)
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
