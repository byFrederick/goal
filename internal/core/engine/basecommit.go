package engine

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// codeRepository is one of an item's repositories as base commits and changed-since read it: its
// key (domain.RepositoryKey), the URL the code host is called for and the base commit its row
// holds, "" for none.
type codeRepository struct {
	key, url, base string
}

// itemCodeRepositories are the repositories base commits and changed-since read for item, its first
// repository first (ADR 0041 decision 5): its rows, or the default entry's for an item with none
// (domain.ResolveItemRepositories); on a line without runtime.repositories, the line's
// runtime.repository alone. None when the line has neither.
func (e *Engine) itemCodeRepositories(ctx context.Context, l domain.Line, item domain.ItemID) ([]codeRepository, error) {
	rows, err := e.store.ItemRepositories(ctx, item)
	if err != nil {
		return nil, fmt.Errorf("repositories of %s: %w", item, err)
	}

	repos := domain.ResolveItemRepositories(rows, l)

	first, ok := repos.First()
	if !ok {
		if l.Runtime.Repository.URL == "" {
			return nil, nil
		}

		key, _ := domain.RepositoryKey(l.Runtime.Repository.URL)

		return []codeRepository{{key: key, url: l.Runtime.Repository.URL}}, nil
	}

	out := []codeRepository{{key: first.Repo, url: repositoryURL(first.Repo), base: first.BaseCommit}}

	for _, r := range repos {
		if r.Repo != first.Repo {
			out = append(out, codeRepository{key: r.Repo, url: repositoryURL(r.Repo), base: r.BaseCommit})
		}
	}

	return out, nil
}

// repositoryURL is the https URL of the repository key names.
func repositoryURL(key string) string { return "https://" + key }

// repositoryHost is the repository at url on one of the organization's code hosts, opened for the
// engine's use on the item, with the code host that serves it and its token: the token of the
// entry of runtime.repositories that covers it, a read entry's first (domain.TokenEntry), opened
// through OrgSecrets for the line's factory; for a repository no entry with a token covers, the
// line's code host connection or runtime.repository.token (mergeTargets).
func (e *Engine) repositoryHost(ctx context.Context, l domain.Line, it domain.Item, url string) (mergeTarget, error) {
	entry, ok := domain.TokenEntry(l.Runtime.Repositories, url, true)
	if !ok || entry.Token == "" {
		targets, err := e.mergeTargets(ctx, l, []string{url}, domain.ConnectionUse{By: domain.EngineActor, Item: it.ID})
		if err != nil {
			return mergeTarget{}, err
		}

		return targets[0], nil
	}

	targets, err := e.codeRepositories(ctx, l, []string{url})
	if err != nil {
		return mergeTarget{}, err
	}

	values, err := e.openSecrets(ctx, domain.SecretNames(entry.Token), domain.SecretUse{Factory: l.FactoryID}, "",
		"runtime.repositories "+entry.Name+" token")
	if err != nil {
		return mergeTarget{}, err
	}

	targets[0].token = domain.FillSecrets(entry.Token, values)

	return targets[0], nil
}

// baseCommits are the heads of the default branches of the item's repositories, by repository
// key, recorded as their rows' base commits when a station that preloads domain.ContextInFlight
// finishes it and the line's flow.assemblies.changedSince is on (ADR 0015 decision 5, amended by
// ADR 0041 decision 5), and the first repository's, recorded as the item's base commit too. Each
// head is read with its repository's token; one the code host cannot say is logged and left out,
// and finishing never waits on it. Both are empty when the setting is off or the line has no
// repository.
func (e *Engine) baseCommits(ctx context.Context, log *slog.Logger, c claimed) (string, map[string]string) {
	l := c.line
	if !l.Flow.Assemblies.ChangedSince || !c.station.Preloads(domain.ContextInFlight) {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gateMergeTimeout)
	defer cancel()

	repos, err := e.itemCodeRepositories(ctx, l, c.item.ID)
	if err != nil {
		log.WarnContext(ctx, "base commit not recorded", slog.Any("error", err))

		return "", nil
	}

	if len(repos) == 0 {
		return "", nil
	}

	shas := make(map[string]string, len(repos))

	for _, r := range repos {
		sha, err := e.branchHead(ctx, l, c.item, r.url)
		if err != nil {
			log.WarnContext(ctx, "base commit not recorded", slog.String("repository", r.url), slog.Any("error", err))

			continue
		}

		shas[r.key] = sha
	}

	return shas[repos[0].key], shas
}

// branchHead reads the head of the default branch of the repository at url.
func (e *Engine) branchHead(ctx context.Context, l domain.Line, it domain.Item, url string) (string, error) {
	t, err := e.repositoryHost(ctx, l, it, url)
	if err != nil {
		return "", err
	}

	sha, err := t.host.BranchHead(ctx, t.repo, t.token, "")
	if err != nil {
		return "", fmt.Errorf("read the default branch's head: %w", err)
	}

	return sha, nil
}

// changedSinceNote is the `## Changed since your plan` section (ADR 0015 decision 5) for a claim
// while the line's flow.assemblies.changedSince is on, one per repository of the item with a base
// commit (ADR 0041 decision 5): the commits on that repository's default branch since its base
// commit that changed files the item's paths in it match. A commit an item of the factory merged
// is named by that item. The first repository falls back to the item's own base commit when its
// row has none, as an item recorded before base commits per repository has. A repository without
// such commits adds nothing, nor does one the code host or the store cannot say for, which is
// logged: a claim never waits on it.
func (e *Engine) changedSinceNote(ctx context.Context, l domain.Line, it domain.Item) string {
	paths := it.Paths
	if len(paths) == 0 {
		paths = it.PartPaths
	}

	if !l.Flow.Assemblies.ChangedSince || len(paths) == 0 {
		return ""
	}

	repos, err := e.itemCodeRepositories(ctx, l, it.ID)
	if err != nil {
		e.log.WarnContext(ctx, "changed since your plan left out", slog.String("item", string(it.ID)), slog.Any("error", err))

		return ""
	}

	notes := make([]string, 0, len(repos))

	for i, r := range repos {
		if i == 0 {
			r.base = cmp.Or(r.base, it.BaseCommit)
		}

		in := repositoryPaths(i, paths)
		if r.base == "" || len(in) == 0 {
			continue
		}

		note, err := e.changedSince(ctx, l, it, r, in)
		if err != nil {
			e.log.WarnContext(ctx, "changed since your plan left out", slog.String("item", string(it.ID)),
				slog.String("repository", r.url), slog.String("base_commit", r.base), slog.Any("error", err))

			continue
		}

		notes = append(notes, note)
	}

	return joinParts(notes...)
}

// repositoryPaths are the item's paths in its i-th repository: every path is in the first until
// paths name their repository (software#717).
func repositoryPaths(i int, paths []string) []string {
	if i > 0 {
		return nil
	}

	return paths
}

// changedSince compares r's base commit with the head of its default branch and renders the
// commits whose changed files overlap paths.
func (e *Engine) changedSince(ctx context.Context, l domain.Line, it domain.Item, r codeRepository, paths []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gateMergeTimeout)
	defer cancel()

	t, err := e.repositoryHost(ctx, l, it, r.url)
	if err != nil {
		return "", err
	}

	head, err := t.host.BranchHead(ctx, t.repo, t.token, "")
	if err != nil {
		return "", fmt.Errorf("read the default branch's head: %w", err)
	}

	if head == r.base {
		return "", nil
	}

	diff, err := t.host.Compare(ctx, t.repo, t.token, r.base, head)
	if err != nil {
		return "", fmt.Errorf("compare %s with the default branch: %w", r.base, err)
	}

	entries := overlapping(diff.Commits, paths)
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

	return domain.ChangedSinceSection(cmp.Or(r.key, r.url), r.base, entries, diff.Truncated), nil
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
