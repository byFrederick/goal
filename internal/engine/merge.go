package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// ErrNotMergeStep means a merge was asked for an item that is not waiting at a human station
// whose action is merge.
var ErrNotMergeStep = errors.New("item is not waiting at a merge station")

// ErrNoCodeHost means a pull request is on a host that none of the factory's connections serves:
// none has a git login for that host and a kind naming one of goald's code hosts.
var ErrNoCodeHost = errors.New("no code host connection serves the pull request's host")

// MergeSquash is the merge method goald uses unless told otherwise.
const MergeSquash = "squash"

// Merge merges every pull request of an item waiting at a merge station, then approves the item
// on by's behalf (spec 4 §4.7). The pull requests are the ones the item's outputs name
// (domain.Item.PullRequests), each merged through the code host of the factory's connection that
// serves its URL's host, opened for by's merge of the item (connection.used names both); a host
// no connection serves is refused (ErrNoCodeHost) before anything is opened or merged. Merge
// never forces: a pull request that is not open and mergeable, or whose head
// moves meanwhile, stops the merge with an error wrapping ports.ErrNotMergeable, and the item
// does not move. The ones merged before it stay merged and are skipped when the merge is asked
// again, so a retry after a partial failure is safe. After each merge the pull request's head
// branch is deleted, and nothing else. The approval carries one merged event per pull request.
func (e *Engine) Merge(ctx context.Context, id domain.ItemID, by domain.Actor, method string) (domain.Item, error) {
	it, l, err := e.waitingHuman(ctx, id)
	if err != nil {
		return domain.Item{}, err
	}

	if st, _ := l.Station(it.Station); st.Action != domain.ActionMerge {
		return domain.Item{}, fmt.Errorf("merge %s: station %s does not merge: %w", id, it.Station, ErrNotMergeStep)
	}

	urls := it.PullRequests(l)
	if len(urls) == 0 {
		return domain.Item{}, fmt.Errorf("merge %s: no output names a pull_request or pull_requests: %w", id, ports.ErrNotMergeable)
	}

	if method == "" {
		method = MergeSquash
	}

	targets, err := e.mergeTargets(ctx, l, urls, domain.ConnectionUse{By: by, Item: it.ID})
	if err != nil {
		return domain.Item{}, fmt.Errorf("merge %s: %w", id, err)
	}

	merged := make([]map[string]any, 0, len(targets))

	for _, t := range targets {
		data, err := e.mergeOne(ctx, id, t, method)
		if err != nil {
			return domain.Item{}, fmt.Errorf("merge %s: %w", id, err)
		}

		merged = append(merged, data)
	}

	now := e.clock.Now()
	t := ports.Transition{ItemID: it.ID, From: it.Position(), At: now}

	for _, data := range merged {
		t.Events = append(t.Events, decision(it, domain.EventMerged, now, by, data))
	}

	// A merge at a merge station is a decision the line requires (spec 4 §4.9).
	t.Events = append(t.Events, decision(it, domain.EventApproved, now, by, map[string]any{domain.KeyPlanned: true}))

	if err := e.advance(l, it, by.Name, &t); err != nil {
		return domain.Item{}, fmt.Errorf("merge %s: %w", id, err)
	}

	return e.decide(ctx, "merge", t)
}

// mergeTarget is one pull request to merge, with the connection and code host that serve it.
type mergeTarget struct {
	url  string
	conn domain.ResolvedConnection
	host ports.CodeHost
}

// codeHostConnection is one of a line's connections that can merge: its name, the host of its git
// login and the code host of its kind.
type codeHostConnection struct {
	name, host string
	ch         ports.CodeHost
}

// mergeTargets picks, for each pull request URL, the first of the line's connections
// (domain.Line.MergeConnections) whose kind names a code host goald has and whose git login is
// for the URL's host, then opens the ones it picked for use.
func (e *Engine) mergeTargets(ctx context.Context, l domain.Line, urls []string, use domain.ConnectionUse) ([]mergeTarget, error) {
	hosts, err := e.codeHostConnections(ctx, l)
	if err != nil {
		return nil, fmt.Errorf("the code host connections: %w", err)
	}

	picked := make([]codeHostConnection, 0, len(urls))

	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%q is not a URL: %w", raw, ports.ErrNotMergeable)
		}

		host := strings.ToLower(u.Host)

		i := slices.IndexFunc(hosts, func(c codeHostConnection) bool { return c.host == host })
		if i < 0 {
			err := fmt.Errorf("%s: no connection of the factory with a code host goald has is for %s: %w", raw, u.Host, ErrNoCodeHost)
			if reason := l.Refusal(); reason != "" {
				err = fmt.Errorf("%w (%s)", err, reason)
			}

			return nil, err
		}

		picked = append(picked, hosts[i])
	}

	names := make([]string, 0, len(picked))
	for _, c := range picked {
		if !slices.Contains(names, c.name) {
			names = append(names, c.name)
		}
	}

	resolved, err := e.connections.Resolve(ctx, names, use)
	if err != nil {
		return nil, fmt.Errorf("open the code host connections: %w", err)
	}

	out := make([]mergeTarget, 0, len(urls))

	for i, c := range picked {
		j := slices.IndexFunc(resolved, func(r domain.ResolvedConnection) bool { return r.Connection.Name == c.name })
		if j < 0 {
			return nil, fmt.Errorf("connection %s was not opened", c.name)
		}

		out = append(out, mergeTarget{url: urls[i], conn: resolved[j], host: c.ch})
	}

	return out, nil
}

// codeHostConnections are the line's connections that can merge, from what members may see of
// them (no secret is opened): those whose kind names a code host goald has, each with the host its
// git login is for.
func (e *Engine) codeHostConnections(ctx context.Context, l domain.Line) ([]codeHostConnection, error) {
	var out []codeHostConnection

	for _, name := range l.MergeConnections() {
		c, err := e.connections.Get(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("connection %s: %w", name, err)
		}

		k, err := e.connections.Kind(ctx, c.Kind)
		if err != nil {
			return nil, fmt.Errorf("connection %s: kind %s: %w", name, c.Kind, err)
		}

		ch, ok := e.codehosts[k.Spec.CodeHost]
		if !ok {
			continue
		}

		host := strings.ToLower(k.Outline(c).GitHost())
		if host == "" {
			e.log.WarnContext(ctx, "a code host connection signs git in to no host", slog.String("connection", name))

			continue
		}

		out = append(out, codeHostConnection{name: name, host: host, ch: ch})
	}

	return out, nil
}

// mergeOne merges one pull request unless it already is, deletes its head branch, and returns
// the data of its merged event.
func (e *Engine) mergeOne(ctx context.Context, id domain.ItemID, t mergeTarget, method string) (map[string]any, error) {
	pr, err := e.merge(ctx, t, method)
	if err != nil {
		return nil, err
	}

	data := map[string]any{"pull_request": t.url, "sha": pr.MergeSHA, "method": method, "branch_deleted": false, domain.KeyPlanned: true}

	// Only the pull request's own branch, never a fork's or the branch it merged into.
	if pr.SameRepository && pr.HeadBranch != "" && pr.HeadBranch != pr.BaseBranch {
		if err := t.host.DeleteBranch(ctx, t.conn, t.url, pr.HeadBranch); err != nil {
			e.log.WarnContext(ctx, "merged, but the head branch was not deleted", slog.String("item", string(id)),
				slog.String("pull_request", t.url), slog.String("branch", pr.HeadBranch), slog.Any("error", err))
		} else {
			data["branch_deleted"] = true
		}
	}

	return data, nil
}

// merge merges the pull request unless it already is, and returns it as merged.
func (e *Engine) merge(ctx context.Context, t mergeTarget, method string) (ports.PullRequest, error) {
	pr, err := t.host.PullRequest(ctx, t.conn, t.url)
	if err != nil {
		return ports.PullRequest{}, err
	}

	if pr.Merged {
		return pr, nil
	}

	if !pr.Open {
		return ports.PullRequest{}, fmt.Errorf("%s is closed: %w", t.url, ports.ErrNotMergeable)
	}

	if !pr.Mergeable {
		return ports.PullRequest{}, fmt.Errorf("%s has conflicts, failing required checks, or its mergeability is still being computed: %w",
			t.url, ports.ErrNotMergeable)
	}

	if _, err := t.host.Merge(ctx, t.conn, t.url, ports.Merge{Method: method, HeadSHA: pr.HeadSHA}); err != nil {
		return ports.PullRequest{}, err
	}

	// Trust the host's record, not the merge call's answer.
	if pr, err = t.host.PullRequest(ctx, t.conn, t.url); err != nil {
		return ports.PullRequest{}, fmt.Errorf("verify the merge of %s: %w", t.url, err)
	}

	if !pr.Merged {
		return ports.PullRequest{}, fmt.Errorf("%s reports not merged after merging", t.url)
	}

	return pr, nil
}
