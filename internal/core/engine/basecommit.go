package engine

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// repositoryHost is the line's code host connection for its repository (runtime.repository),
// opened for the engine's use on the item, with the code host that serves it.
func (e *Engine) repositoryHost(ctx context.Context, l domain.Line, it domain.Item) (mergeTarget, error) {
	targets, err := e.mergeTargets(ctx, l, []string{l.Runtime.Repository.URL}, domain.ConnectionUse{By: domain.EngineActor, Item: it.ID})
	if err != nil {
		return mergeTarget{}, err
	}

	return targets[0], nil
}

// baseCommit is the head of the default branch of the line's repository, recorded as the item's
// base commit when a station that preloads domain.ContextInFlight finishes it and the line's
// flow.assemblies.changedSince is on (ADR 0015 decision 5). It is empty otherwise, when the line
// has no repository, and when the code host cannot say: that is logged, and finishing never
// waits on it.
func (e *Engine) baseCommit(ctx context.Context, log *slog.Logger, c claimed) string {
	l := c.line
	if !l.Flow.Assemblies.ChangedSince || !c.station.Preloads(domain.ContextInFlight) || l.Runtime.Repository.URL == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gateMergeTimeout)
	defer cancel()

	sha, err := e.branchHead(ctx, l, c.item)
	if err != nil {
		log.WarnContext(ctx, "base commit not recorded", slog.String("repository", l.Runtime.Repository.URL), slog.Any("error", err))

		return ""
	}

	return sha
}

// branchHead reads the head of the default branch of the line's repository.
func (e *Engine) branchHead(ctx context.Context, l domain.Line, it domain.Item) (string, error) {
	t, err := e.repositoryHost(ctx, l, it)
	if err != nil {
		return "", err
	}

	sha, err := t.host.BranchHead(ctx, t.conn, t.url, "")
	if err != nil {
		return "", fmt.Errorf("read the default branch's head: %w", err)
	}

	return sha, nil
}

// changedSinceNote is the `## Changed since your plan` section (ADR 0015 decision 5) for a claim
// of an item with a base commit, while the line's flow.assemblies.changedSince is on: the
// commits on the repository's default branch since the base commit that changed files the
// item's paths match. A commit an item of the factory merged is named by that item. It is empty
// without such commits, and when the code host or the store cannot say, which is logged: a
// claim never waits on it.
func (e *Engine) changedSinceNote(ctx context.Context, l domain.Line, it domain.Item) string {
	paths := it.Paths
	if len(paths) == 0 {
		paths = it.PartPaths
	}

	if !l.Flow.Assemblies.ChangedSince || it.BaseCommit == "" || l.Runtime.Repository.URL == "" || len(paths) == 0 {
		return ""
	}

	note, err := e.changedSince(ctx, l, it, paths)
	if err != nil {
		e.log.WarnContext(ctx, "changed since your plan left out", slog.String("item", string(it.ID)),
			slog.String("base_commit", it.BaseCommit), slog.Any("error", err))

		return ""
	}

	return note
}

// changedSince compares the item's base commit with the default branch's head and renders the
// commits whose changed files overlap paths.
func (e *Engine) changedSince(ctx context.Context, l domain.Line, it domain.Item, paths []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gateMergeTimeout)
	defer cancel()

	t, err := e.repositoryHost(ctx, l, it)
	if err != nil {
		return "", err
	}

	head, err := t.host.BranchHead(ctx, t.conn, t.url, "")
	if err != nil {
		return "", fmt.Errorf("read the default branch's head: %w", err)
	}

	if head == it.BaseCommit {
		return "", nil
	}

	cmp, err := t.host.Compare(ctx, t.conn, t.url, it.BaseCommit, head)
	if err != nil {
		return "", fmt.Errorf("compare %s with the default branch: %w", it.BaseCommit, err)
	}

	entries := overlapping(cmp.Commits, paths)
	if len(entries) == 0 {
		return "", nil
	}

	shas := make([]string, 0, len(entries))
	for _, en := range entries {
		shas = append(shas, en.SHA)
	}

	merged, err := e.store.MergedItems(ctx, it.FactoryID, slices.Compact(shas))
	if err != nil {
		return "", fmt.Errorf("read the items that merged the commits: %w", err)
	}

	for i, en := range entries {
		if m, ok := merged[en.SHA]; ok {
			entries[i].Ref, entries[i].Title = domain.ItemRef{Factory: l.Name, Number: m.Number}.String(), m.Title
		}
	}

	return domain.ChangedSinceSection(it.BaseCommit, entries, cmp.Truncated), nil
}

// overlapping are the entries of the commits that changed files paths match, each with only
// those files.
func overlapping(commits []ports.Commit, paths []string) []domain.ChangedSinceEntry {
	var entries []domain.ChangedSinceEntry

	for _, cm := range commits {
		var changed []string

		for _, f := range cm.Files {
			if len(domain.PathsOverlap([]string{f}, paths)) > 0 {
				changed = append(changed, f)
			}
		}

		if len(changed) > 0 {
			entries = append(entries, domain.ChangedSinceEntry{SHA: cm.SHA, Subject: cm.Subject, Paths: changed})
		}
	}

	return entries
}
