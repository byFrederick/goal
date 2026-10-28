package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/byFrederick/goal/internal/core/domain"
)

// integrate decides, once at the claim's start, the branch its runs work on and the base that
// branch is cut from and its pull request goes into (domain.RunBase, ADR 0033). A part of an
// assembly whose line is in integration mode branch first sets the assembly's integration branch
// when none is set yet, the assembly has at least minParts parts and the part names no pull
// request; the store sets it once, so a part that lost that race reads the assembly again. A part
// that names a pull request keeps the base the pull request was opened against, read from its code
// host: a failed read fails the claim rather than guess the default branch. A landing root
// (domain.Item.Landing) works on its integration branch with the default branch as its base, at
// every station.
func (e *Engine) integrate(ctx context.Context, log *slog.Logger, c *claimed) error {
	it := c.item
	if it.Landing() {
		c.branch, c.base = domain.RunBase(it, it, "")

		return nil
	}

	if it.ParentID == "" {
		return nil
	}

	root, err := e.store.Item(ctx, it.ParentID)
	if err != nil {
		return fmt.Errorf("read the assembly: %w", err)
	}

	pr := it.PullRequest(c.line)

	if root.IntegrationBranch == "" && pr == "" && c.line.Flow.Assemblies.Integration.Branches() {
		if root, err = e.setIntegrationBranch(ctx, log, c.line, root); err != nil {
			return err
		}
	}

	prBase := ""
	if root.IntegrationBranch != "" && pr != "" {
		if prBase, err = e.pullRequestBase(ctx, c.line, it, pr); err != nil {
			return err
		}
	}

	// The default branch leaves both empty, so a trunk run's task token is as before.
	if branch, base := domain.RunBase(it, root, prBase); base != "" {
		c.branch, c.base = branch, base
	}

	return nil
}

// setIntegrationBranch sets root's integration branch and landing station when the assembly has
// at least the policy's minParts parts and the line has a landing station, and returns root as
// stored after it.
func (e *Engine) setIntegrationBranch(ctx context.Context, log *slog.Logger, l domain.Line, root domain.Item) (domain.Item, error) {
	parts, err := e.store.Parts(ctx, root.ID)
	if err != nil {
		return root, fmt.Errorf("read the assembly's parts: %w", err)
	}

	if parts.Total < l.Flow.Assemblies.Integration.MinParts {
		return root, nil
	}

	land, ok := l.LandingStation(root.Station)
	if !ok {
		// The definition checks only that the line has a merge station: one that comes before the
		// planning station leaves nowhere to land, so the assembly stays on the default branch.
		log.WarnContext(ctx, "integration branch not set", slog.String("assembly", string(root.ID)),
			slog.String("reason", "no merge station after "+root.Station))

		return root, nil
	}

	branch := domain.IntegrationBranch(root)

	set, err := e.store.SetIntegrationBranch(ctx, root.ID, branch, land.ID, e.clock.Now())
	if err != nil {
		return root, fmt.Errorf("set the assembly's integration branch: %w", err)
	}

	if !set {
		// Another instance set it first, maybe to other values: read what it stored.
		if root, err = e.store.Item(ctx, root.ID); err != nil {
			return root, fmt.Errorf("read the assembly: %w", err)
		}

		return root, nil
	}

	log.InfoContext(ctx, "integration branch set", slog.String("assembly", string(root.ID)),
		slog.String("branch", branch), slog.String("land_at", land.ID))

	root.IntegrationBranch, root.IntegrationLandAt = branch, land.ID

	return root, nil
}

// pullRequestBase reads the branch the pull request at url was opened against, through the
// line's code host connection for its host (mergeTargets).
func (e *Engine) pullRequestBase(ctx context.Context, l domain.Line, it domain.Item, url string) (string, error) {
	targets, err := e.mergeTargets(ctx, l, []string{url}, domain.ConnectionUse{By: domain.EngineActor, Item: it.ID})
	if err != nil {
		return "", fmt.Errorf("the base of pull request %s: %w", url, err)
	}

	pr, err := targets[0].host.PullRequest(ctx, targets[0].conn, url)
	if err != nil {
		return "", fmt.Errorf("read pull request %s: %w", url, err)
	}

	return pr.BaseBranch, nil
}
