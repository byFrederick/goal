package gateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Tries reads a try of a past item and what its original run recorded, for the organization in
// ctx; ports.PastTries satisfies it.
type Tries interface {
	PastTry(ctx context.Context, id domain.PastTryID) (domain.PastTry, error)
	PastTryInput(ctx context.Context, run domain.RunID) (domain.PastTryInput, error)
}

// ProposedDefinitions loads definition version id with a proposal's edits applied to it: what a try of
// a proposal runs on. cmd/goald adapts *definitions.Cache.Proposed to it.
type ProposedDefinitions interface {
	Proposed(ctx context.Context, id domain.DefinitionVersionID, edits []domain.ProposalEdit) (Definitions, error)
}

// errNoTries answers a try token's bundle when the gateway has no Deps.Tries, Deps.Proposed or
// Deps.Jonah: 503.
var errNoTries = errors.New("the gateway has no tries store")

// tryBundle is a try of a past item's bundle (ADR 0039 decision 9): the station's bundle for the
// try's agent on the token's version with the proposal's edits applied to it (a publish's try's
// own edits, on its own version), whose brief is
// the one the original run recorded and whose inputs are the item's outputs from the runs that
// ended before it started. Its repository is the original run's commit, fetched by SHA, with the
// login that clones it, which prepare drops before the agent starts. Its environment has no files
// or MCP servers and its env is for setup alone; it has no connection, granted call or knowledge
// source, and it records nothing on the past item. A try of another factory, station or agent, or
// of a run with no recorded commit, wraps errForbidden.
func (g *Gateway) tryBundle(ctx context.Context, c domain.TaskClaims) (runBundle, error) {
	if g.tries == nil || g.proposed == nil || g.jonah == nil {
		return runBundle{}, errNoTries
	}

	try, err := g.tries.PastTry(ctx, c.TryOf)
	if err != nil {
		return runBundle{}, fmt.Errorf("try %s: %w", c.TryOf, err)
	}

	in, err := g.tries.PastTryInput(ctx, try.OriginalRunID)
	if err != nil {
		return runBundle{}, fmt.Errorf("try %s: original run %s: %w", try.ID, try.OriginalRunID, err)
	}

	orig := in.Run
	if err := tryOfToken(c, try, orig); err != nil {
		return runBundle{}, err
	}

	// A proposal's try runs on its edits; a publish's on its own, the draft's (ADR 0039 decision 12).
	edits := try.Edits

	if try.ProposalID == "" && try.VersionID != c.VersionID {
		return runBundle{}, fmt.Errorf("%w: try %s runs on version %s, not %s", errForbidden, try.ID, try.VersionID, c.VersionID)
	}

	if try.ProposalID != "" {
		p, err := g.jonah.Proposal(ctx, c.FactoryID, try.ProposalNumber)
		if err != nil {
			return runBundle{}, fmt.Errorf("try %s: proposal %d: %w", try.ID, try.ProposalNumber, err)
		}

		edits = p.Edits
	}

	defs, err := g.proposed.Proposed(ctx, c.VersionID, edits)
	if err != nil {
		return runBundle{}, fmt.Errorf("try %s: version %s with its edits: %w", try.ID, c.VersionID, err)
	}

	pg := *g
	pg.defs = defs

	// The token names no item; the bundle reads the past item's as the original run did.
	ic := c
	ic.ItemID = orig.ItemID

	return pg.tryOf(ctx, ic, in)
}

// tryOfToken refuses a try, or its original run, that is not of c's factory, station and agent,
// and a run that recorded no commit, wrapping errForbidden.
func tryOfToken(c domain.TaskClaims, try domain.PastTry, orig domain.Run) error {
	switch {
	case try.FactoryID != c.FactoryID || orig.FactoryID != c.FactoryID:
		return fmt.Errorf("%w: try %s is not of factory %s", errForbidden, try.ID, c.FactoryID)
	case try.Station != c.Station || orig.Station != c.Station || orig.Agent != c.Agent:
		return fmt.Errorf("%w: try %s is not of agent %s at %s", errForbidden, try.ID, c.Agent, c.Station)
	case orig.Input.BranchHead == "":
		return fmt.Errorf("%w: run %s recorded no commit", errForbidden, orig.ID)
	}

	return nil
}

// tryOf builds a try's bundle on g's definitions, c naming the past item.
func (g *Gateway) tryOf(ctx context.Context, c domain.TaskClaims, in domain.PastTryInput) (runBundle, error) {
	run, err := g.run(ctx, c)
	if err != nil {
		return runBundle{}, err
	}

	line, it := run.line, run.item

	b, err := g.agentBundle(ctx, c, run, in.Run.Input.Brief, in.Outputs)
	if err != nil {
		return runBundle{}, err
	}

	if line.OnSecrets() {
		if err := g.addEnvironment(ctx, c, line, run.agent, &b); err != nil {
			return runBundle{}, err
		}
	}

	if err := g.addRepository(ctx, line.Runtime.Repository, it.Branch(), "", line.Flow.Assemblies.FreshBase, &b); err != nil {
		return runBundle{}, err
	}

	if b.Repository == nil {
		return runBundle{}, fmt.Errorf("%w: version %s has no repository to try in", errForbidden, c.VersionID)
	}

	b.Repository.Commit, b.Repository.BaseCommit = in.Run.Input.BranchHead, in.Run.Input.BaseHead

	if err := g.addRules(ctx, c, it, &b); err != nil {
		return runBundle{}, err
	}

	return b, nil
}
