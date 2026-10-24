package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// ErrNotMergeStep means a merge was asked for an item that is not waiting at a human station
// whose action is merge.
var ErrNotMergeStep = errors.New("item is not waiting at a merge station")

// MergeSquash is the merge method goald uses unless told otherwise.
const MergeSquash = "squash"

// Merge merges the pull request of an item waiting at a merge station, then approves the
// item on by's behalf. The pull request is the one the item's outputs name, on the factory's
// repository. Merge never forces: a pull request that is not open and mergeable, or whose head
// moves meanwhile, is refused with an error wrapping ports.ErrNotMergeable and nothing changes.
// After a successful merge the pull request's head branch is deleted, and nothing else.
// A pull request that is already merged (a merge whose approval failed) is approved again.
func (e *Engine) Merge(ctx context.Context, id domain.ItemID, by domain.Actor, method string) (domain.Item, error) {
	it, l, err := e.waitingHuman(ctx, id)
	if err != nil {
		return domain.Item{}, err
	}

	if st, _ := l.Station(it.Station); st.Action != domain.ActionMerge || l.Runtime.Repository.URL == "" {
		return domain.Item{}, fmt.Errorf("merge %s: station %s does not merge: %w", id, it.Station, ErrNotMergeStep)
	}

	url := it.PullRequest(l)
	if url == "" {
		return domain.Item{}, fmt.Errorf("merge %s: no output names a pull_request: %w", id, ports.ErrNotMergeable)
	}

	if method == "" {
		method = MergeSquash
	}

	repo := l.Runtime.Repository

	pr, err := e.merge(ctx, repo, url, method)
	if err != nil {
		return domain.Item{}, fmt.Errorf("merge %s: %w", id, err)
	}

	merged := map[string]any{"pull_request": url, "sha": pr.MergeSHA, "method": method, "branch_deleted": false}

	// Only the pull request's own branch, never a fork's or the branch it merged into.
	if pr.SameRepository && pr.HeadBranch != "" && pr.HeadBranch != pr.BaseBranch {
		if err := e.codehost.DeleteBranch(ctx, repo, pr.HeadBranch); err != nil {
			e.log.WarnContext(ctx, "merged, but the head branch was not deleted", slog.String("item", string(id)),
				slog.String("branch", pr.HeadBranch), slog.Any("error", err))
		} else {
			merged["branch_deleted"] = true
		}
	}

	now := e.clock.Now()
	t := ports.Transition{
		ItemID: it.ID, From: it.Position(), At: now,
		Events: []domain.Event{decision(it, domain.EventMerged, now, by, merged), decision(it, domain.EventApproved, now, by, nil)},
	}

	if err := e.advance(l, it, by.Name, &t); err != nil {
		return domain.Item{}, fmt.Errorf("merge %s: %w", id, err)
	}

	return e.decide(ctx, "merge", t)
}

// merge merges the pull request unless it already is, and returns it as merged.
func (e *Engine) merge(ctx context.Context, repo domain.Repository, url, method string) (ports.PullRequest, error) {
	pr, err := e.codehost.PullRequest(ctx, repo, url)
	if err != nil {
		return ports.PullRequest{}, err
	}

	if pr.Merged {
		return pr, nil
	}

	if !pr.Open {
		return ports.PullRequest{}, fmt.Errorf("%s is closed: %w", url, ports.ErrNotMergeable)
	}

	if !pr.Mergeable {
		return ports.PullRequest{}, fmt.Errorf("%s has conflicts, failing required checks, or its mergeability is still being computed: %w",
			url, ports.ErrNotMergeable)
	}

	if _, err := e.codehost.Merge(ctx, repo, url, ports.Merge{Method: method, HeadSHA: pr.HeadSHA}); err != nil {
		return ports.PullRequest{}, err
	}

	// Trust the host's record, not the merge call's answer.
	if pr, err = e.codehost.PullRequest(ctx, repo, url); err != nil {
		return ports.PullRequest{}, fmt.Errorf("verify the merge: %w", err)
	}

	if !pr.Merged {
		return ports.PullRequest{}, fmt.Errorf("%s reports not merged after merging", url)
	}

	return pr, nil
}
