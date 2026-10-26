package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// ErrNotMergeStep means a merge was asked for an item that is not waiting for a person at a
// station whose action is merge.
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
// never forces: each pull request's branch is first brought up to date with its base, and one
// that is not open and mergeable then, or whose head moves meanwhile, stops the merge with an
// error wrapping ports.ErrNotMergeable, and the item does not move. A branch that conflicts with
// its base sends the item back to be built again instead (conflictRework), unless the line's
// flow.assemblies.onConflict is human; the refusal then also wraps ports.ErrMergeConflict. The
// ones merged before it stay merged and are skipped when the merge is asked again, so a retry
// after a partial failure is safe. After each merge the pull request's head branch is deleted,
// and nothing else. The approval carries one merged event per pull request.
func (e *Engine) Merge(ctx context.Context, id domain.ItemID, by domain.Actor, method string) (domain.Item, error) {
	it, l, err := e.waitingHuman(ctx, id)
	if err != nil {
		return domain.Item{}, err
	}

	if st, _ := l.Station(it.Station); st.Action != domain.ActionMerge {
		return domain.Item{}, fmt.Errorf("merge %s: station %s does not merge: %w", id, it.Station, ErrNotMergeStep)
	}

	if method == "" {
		method = MergeSquash
	}

	now := e.clock.Now()
	t := ports.Transition{ItemID: it.ID, From: it.Position(), At: now}

	merged, err := e.mergeAll(ctx, l, it, by, method, now)
	if err != nil {
		if e.conflictRework(l, it, err, &t) {
			return e.decide(ctx, "merge", t)
		}

		return domain.Item{}, fmt.Errorf("merge %s: %w", id, err)
	}

	t.Events = append(t.Events, merged...)

	// A merge at a merge station is a decision the line requires (spec 4 §4.9).
	t.Events = append(t.Events, decision(it, domain.EventApproved, now, by, map[string]any{domain.KeyPlanned: true}))

	if err := e.advance(l, it, by.Name, &t); err != nil {
		return domain.Item{}, fmt.Errorf("merge %s: %w", id, err)
	}

	return e.decide(ctx, "merge", t)
}

// gateMergeTimeout bounds a gate's merge, which runs after the run, while the lease is held,
// and goes on through a shutdown so the item does not stay in progress with some pull requests
// merged.
const gateMergeTimeout = 2 * time.Minute

// mergeOnApproval merges the item's pull requests when the gate that approved it, an agent
// station, has action merge (spec 4 §4.7), and adds the merged events to t. It is Merge's merge
// with the engine as actor: the connections are opened for the engine's use on the item, so it
// is no person's touch. The pull requests are those of the item's outputs and of the gate's own
// output, which only t holds yet. A refusal is returned, so the item blocks at the gate (or,
// for a conflict, goes back to be built: conflictRework) and never moves on unmerged.
func (e *Engine) mergeOnApproval(ctx context.Context, c claimed, l domain.Line, t *ports.Transition) error {
	if c.station.Action != domain.ActionMerge {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gateMergeTimeout)
	defer cancel()

	it := c.item
	it.Outputs = maps.Clone(it.Outputs)

	if it.Outputs == nil {
		it.Outputs = map[string]json.RawMessage{}
	}

	it.Outputs[c.station.ID] = t.Output

	merged, err := e.mergeAll(ctx, l, it, domain.EngineActor, MergeSquash, t.At)
	if err != nil {
		return fmt.Errorf("merge on the gate's approval: %w", err)
	}

	t.Events = append(t.Events, merged...)

	return nil
}

// mergeAll merges every pull request the item's outputs name (domain.Item.PullRequests) through
// the line's code host connections, opened for by's merge of it, and returns one merged event
// by that actor per pull request. It stops at the first refusal; the ones merged before it stay merged
// and are skipped when asked again.
func (e *Engine) mergeAll(ctx context.Context, l domain.Line, it domain.Item, by domain.Actor, method string, at time.Time) ([]domain.Event, error) {
	urls := it.PullRequests(l)
	if len(urls) == 0 {
		return nil, fmt.Errorf("no output names a pull_request or pull_requests: %w", ports.ErrNotMergeable)
	}

	targets, err := e.mergeTargets(ctx, l, urls, domain.ConnectionUse{By: by, Item: it.ID})
	if err != nil {
		return nil, err
	}

	events := make([]domain.Event, 0, len(targets))

	for _, t := range targets {
		data, err := e.mergeOne(ctx, it.ID, t, method)
		if err != nil {
			return nil, err
		}

		events = append(events, decision(it, domain.EventMerged, at, by, data))
	}

	return events, nil
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

// merge merges the pull request unless it already is, and returns it as merged. An open one's
// branch is first brought up to date with its base at the head just read (ports.CodeHost.
// UpdateBranch), and merged at the head the next read returns; a conflict is a *mergeConflict.
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

	if err := t.host.UpdateBranch(ctx, t.conn, t.url, pr.HeadSHA); err != nil {
		var mc *ports.MergeConflictError
		if errors.As(err, &mc) {
			return ports.PullRequest{}, &mergeConflict{url: t.url, base: pr.BaseBranch, files: mc.Files, err: err}
		}

		return ports.PullRequest{}, fmt.Errorf("update the branch of %s: %w", t.url, err)
	}

	// The read waits while the host computes the updated branch's mergeability.
	if pr, err = t.host.PullRequest(ctx, t.conn, t.url); err != nil {
		return ports.PullRequest{}, fmt.Errorf("read %s after updating its branch: %w", t.url, err)
	}

	if pr.MergeabilityPending {
		return ports.PullRequest{}, fmt.Errorf("%s: the host is still computing whether it can be merged, try again shortly: %w",
			t.url, ports.ErrNotMergeable)
	}

	if !pr.Mergeable {
		return ports.PullRequest{}, fmt.Errorf("%s has conflicts or failing required checks: %w", t.url, ports.ErrNotMergeable)
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

// mergeConflict is a merge refused because a pull request's branch conflicts with its base. Its
// message is the reason the item is sent back with: the pull request, its base, the files when
// the host named them, and what to do.
type mergeConflict struct {
	url, base string
	files     []string
	err       error // the code host's, wrapping ports.ErrMergeConflict
}

func (c *mergeConflict) Error() string {
	base := c.base
	if base == "" {
		base = "main"
	}

	in := ""
	if len(c.files) > 0 {
		in = " in " + strings.Join(c.files, ", ")
	}

	return fmt.Sprintf("%s conflicts with %s%s: merge origin/%s and resolve", c.url, base, in, base)
}

// Unwrap makes a conflict both ports.ErrMergeConflict and ports.ErrNotMergeable, so a person's
// merge refused for one answers 409 like any other refusal.
func (c *mergeConflict) Unwrap() []error { return []error{c.err, ports.ErrNotMergeable} }

// conflictRework points t back at the line's rework target for it (the gate's onReject, else the
// nearest agent station upstream) when err is a merge conflict and the line's
// flow.assemblies.onConflict is rework, with a merge_conflict event giving the reason, and
// reports whether it did. It is no rejection: t.Rework stays false, so the item's rework count
// and its gate's maxRework are untouched.
func (e *Engine) conflictRework(l domain.Line, it domain.Item, err error, t *ports.Transition) bool {
	var mc *mergeConflict
	if !errors.As(err, &mc) || l.Flow.Assemblies.OnConflict == domain.ConflictHuman {
		return false
	}

	target, terr := l.ReworkTarget(it.Station)
	if terr != nil {
		return false
	}

	t.Events = append(t.Events, event(it, domain.EventMergeConflict, t.At, domain.ActorEngine,
		map[string]any{keyReason: mc.Error(), "pull_request": mc.url}))

	pos, events, doneAt := e.arrive(it, target, domain.ActorEngine)
	t.To, t.DoneAt = pos, doneAt
	t.Events = append(t.Events, events...)

	return true
}
