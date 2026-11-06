package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ErrNotMergeStep means a merge was asked for an item that is not waiting for a person at a
// station whose action is merge.
var ErrNotMergeStep = errors.New("item is not waiting at a merge station")

// ErrNoCodeHost means a pull request is on a host nothing of the factory's version serves. On
// connections, none of its connections has a git login for that host and a kind naming one of
// goald's code hosts. On secrets (ADR 0037 decision 7), the host is not runtime.repository.url's,
// or the repository names no code host goald has (runtime.repository.forge) or no
// runtime.repository.token.
var ErrNoCodeHost = errors.New("no code host serves the pull request's host")

// ErrNoWriteEntry means a pull request is in a repository that only a read entry of
// runtime.repositories covers, or none does: goald never updates a branch, merges or deletes a
// branch there (ADR 0041 decision 3).
var ErrNoWriteEntry = errors.New("no repository entry that writes covers the pull request's repository")

// ErrNotTheItems means a pull request's head is not the item's branch, or its assembly's
// integration branch, in its own repository: no station checked it, so goald does not merge it
// (ADR 0041 decision 5).
var ErrNotTheItems = errors.New("the pull request is not the item's")

// MergeSquash is the merge method goald uses unless told otherwise.
const MergeSquash = "squash"

// Merge merges every pull request of an item waiting at a merge station, then approves the item
// on by's behalf (spec 4 §4.7). The pull requests are the ones the item's outputs name
// (domain.Item.PullRequests), in the order they name them, each in its own repository through the
// code host and token mergeTargets picks for it: the token of the runtime.repositories entry that
// covers it, else the factory's connection that serves its host, opened for by's merge of the item
// (connection.used names both), or runtime.repository's token. A host nothing serves is refused
// (ErrNoCodeHost), and a repository no entry that writes covers (ErrNoWriteEntry), before anything
// is opened or merged; a pull request whose head is not the item's branch is refused
// (ErrNotTheItems) before its branch is touched. Merge
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

	merged, err := e.mergeAll(ctx, l, it, by, method, now, nil)
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
// output, which only t holds yet. Unlike a person's merge, it reads a pull request whose
// mergeability is still being computed again for up to Config.MergeWait, heartbeating the lease.
// A refusal is returned, so the item blocks at the gate, goes back to be built for a conflict
// (conflictRework), or goes back to the gate's queue when the pull request is only not mergeable
// yet (deferMerge); it never moves on unmerged. A landing
// root whose gate output says pull_requests: [] (domain.NoPullRequests), and whose outputs name
// none, moves on with nothing merged: its integration branch had nothing to land.
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

	if it.Landing() && domain.NoPullRequests(t.Output) && len(it.PullRequests(l)) == 0 {
		e.log.InfoContext(ctx, "empty landing", slog.String("item", string(it.ID)), slog.String("branch", it.IntegrationBranch))

		return nil
	}

	merged, err := e.mergeAll(ctx, l, it, domain.EngineActor, MergeSquash, t.At, &c.lease)
	if err != nil {
		return fmt.Errorf("merge on the gate's approval: %w", err)
	}

	t.Events = append(t.Events, merged...)

	return nil
}

// mergeAll merges every pull request the item's outputs name (domain.Item.PullRequests) through
// the line's code host connections, opened for by's merge of it, and returns one merged event
// by that actor per pull request. It stops at the first refusal; the ones merged before it stay merged
// and are skipped when asked again. With a lease (a gate's merge), a pull request whose
// mergeability is pending is read again while the lease is heartbeated (settled).
func (e *Engine) mergeAll(ctx context.Context, l domain.Line, it domain.Item, by domain.Actor, method string, at time.Time,
	lease *domain.Lease,
) ([]domain.Event, error) {
	urls := it.PullRequests(l)
	if len(urls) == 0 {
		return nil, fmt.Errorf("no output names a pull_request or pull_requests: %w", ports.ErrNotMergeable)
	}

	targets, err := e.mergeTargets(ctx, l, urls)
	if err != nil {
		return nil, err
	}

	heads, err := e.itemHeads(ctx, it)
	if err != nil {
		return nil, err
	}

	events := make([]domain.Event, 0, len(targets))

	for _, t := range targets {
		data, err := e.mergeOne(ctx, it.ID, t, heads(t.repo.Key()), method, lease)
		if err != nil {
			return nil, err
		}

		events = append(events, decision(it, domain.EventMerged, at, by, data))
	}

	return events, nil
}

// mergeTarget is one pull request to merge, or a repository to read, with its repository on one
// of the organization's code hosts, the token it is called with (an entry's, or
// runtime.repository.token) and the code host adapter of the host's kind.
type mergeTarget struct {
	url   string
	repo  domain.CodeRepository
	token string
	host  ports.CodeHost
}

// listedCodeHosts are the organization's code hosts (domain.ListedCodeHosts): the built-in ones,
// and the ones it added when the engine has Hosts.
func (e *Engine) listedCodeHosts(ctx context.Context) ([]domain.CodeHost, error) {
	if e.hosts == nil {
		return domain.BuiltInCodeHosts(), nil
	}

	added, err := e.hosts.CodeHosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the organization's code hosts: %w", err)
	}

	return domain.ListedCodeHosts(added), nil
}

// codeRepositories are the repositories of urls (pull requests', or a repository's own) on the
// organization's code hosts (ADR 0041 decision 1), each with the adapter of its host's kind. A
// URL on a host the organization doesn't list, or whose kind this goald has no adapter for, is a
// refusal wrapping ErrNoCodeHost, before anything is opened or sent; a URL that names no
// repository wraps ports.ErrNotMergeable.
func (e *Engine) codeRepositories(ctx context.Context, l domain.Line, urls []string) ([]mergeTarget, error) {
	hosts, err := e.listedCodeHosts(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]mergeTarget, 0, len(urls))

	for _, raw := range urls {
		repo, err := domain.ParseCodeRepository(hosts, raw)

		switch {
		case errors.Is(err, domain.ErrCodeHostUnlisted):
			return nil, refusal(l, fmt.Errorf("%s: %w: %w", raw, err, ErrNoCodeHost))
		case err != nil:
			return nil, fmt.Errorf("%w: %w", err, ports.ErrNotMergeable)
		}

		ch, ok := e.codehosts[string(repo.Host.Kind)]
		if !ok {
			return nil, refusal(l, fmt.Errorf("%s: %s is a %s host, and this goald has no %s code host: %w", raw, repo.Host.Host, repo.Host.Kind, repo.Host.Kind, ErrNoCodeHost))
		}

		out = append(out, mergeTarget{url: raw, repo: repo, host: ch})
	}

	return out, nil
}

// mergeTargets are the pull requests at urls as a merge writes them (ADR 0041 decisions 3 and 5).
// On a line with runtime.repositories, each repository must be covered by an entry that writes:
// the most specific entry that covers it (domain.TokenEntry, reads false) is a write entry, or the
// merge is refused, naming the repository, before anything is opened (ErrNoWriteEntry). That
// entry's token signs in, opened once per entry for the line's factory through OrgSecrets; an
// entry without one signs in with runtime.repository.token (repositoryTargets). A line without
// runtime.repositories is repositoryTargets' alone.
func (e *Engine) mergeTargets(ctx context.Context, l domain.Line, urls []string) ([]mergeTarget, error) {
	if len(l.Runtime.Repositories) == 0 {
		return e.repositoryTargets(ctx, l, urls)
	}

	targets, err := e.codeRepositories(ctx, l, urls)
	if err != nil {
		return nil, err
	}

	picked, err := writeEntries(l, targets)
	if err != nil {
		return nil, err
	}

	tokens := map[string]string{}

	var logins []int // the targets whose entry has no token

	for i, entry := range picked {
		if entry.Token == "" {
			logins = append(logins, i)

			continue
		}

		if _, ok := tokens[entry.Name]; !ok {
			if tokens[entry.Name], err = e.entryToken(ctx, l, entry); err != nil {
				return nil, err
			}
		}

		targets[i].token = tokens[entry.Name]
	}

	if len(logins) == 0 {
		return targets, nil
	}

	rest := make([]string, len(logins))
	for j, i := range logins {
		rest[j] = targets[i].url
	}

	signed, err := e.repositoryTargets(ctx, l, rest)
	if err != nil {
		return nil, err
	}

	for j, i := range logins {
		targets[i] = signed[j]
	}

	return targets, nil
}

// writeEntries are the entries that write targets' repositories, one per target: the most
// specific entry that covers each, which must be a write entry. The shorthand's
// runtime.repository.forge, when set, must agree with the kind its host is listed as.
func writeEntries(l domain.Line, targets []mergeTarget) ([]domain.RepositoryEntry, error) {
	picked := make([]domain.RepositoryEntry, len(targets))

	for i, t := range targets {
		entry, ok := domain.TokenEntry(l.Runtime.Repositories, t.repo.Key(), false)
		if !ok || entry.Access != domain.AccessWrite {
			return nil, refusal(l, noWriteEntry(t, entry, ok))
		}

		if f := l.Runtime.Repository.Forge; f != "" && entry.URL == l.Runtime.Repository.URL && f != string(t.repo.Host.Kind) {
			return nil, refusal(l, fmt.Errorf("the repository %s names runtime.repository.forge %s, but the organization lists %s as a %s host: %w",
				entry.URL, f, t.repo.Host.Host, t.repo.Host.Kind, ErrNoCodeHost))
		}

		picked[i] = entry
	}

	return picked, nil
}

// noWriteEntry is the refusal of the pull request of t, whose repository entry, the most specific
// that covers it, only reads, or that no entry covers (covered false).
func noWriteEntry(t mergeTarget, entry domain.RepositoryEntry, covered bool) error {
	if !covered {
		return fmt.Errorf("%s: no entry of runtime.repositories covers %s; an entry with access write must cover it for goald to merge there: %w",
			t.url, t.repo, ErrNoWriteEntry)
	}

	return fmt.Errorf("%s: %s is covered by entry %s, which only reads; an entry with access write must cover it for goald to merge there: %w",
		t.url, t.repo, entry.Name, ErrNoWriteEntry)
}

// entryToken is the token of entry, opened for the line's factory through OrgSecrets, which checks
// its scope, audits it as secret.used and sets its last use.
func (e *Engine) entryToken(ctx context.Context, l domain.Line, entry domain.RepositoryEntry) (string, error) {
	values, err := e.openSecrets(ctx, domain.SecretNames(entry.Token), domain.SecretUse{Factory: l.FactoryID}, "",
		"runtime.repositories "+entry.Name+" token")
	if err != nil {
		return "", err
	}

	return domain.FillSecrets(entry.Token, values), nil
}

// repositoryLogin names the token of a version on secrets, in errors and logs.
const repositoryLogin = "runtime.repository.token"

// repositoryTargets are the targets signed in with runtime.repository.token (ADR 0037 decision 7): every pull
// request must be on the host of runtime.repository.url, one of the organization's code hosts,
// whose kind picks the adapter; runtime.repository.forge, when set, must agree with it. The
// runtime.repository.token signs in. The token is opened once for the line's factory through
// OrgSecrets, which checks its scope, audits it as secret.used and sets its last use, as at run
// start. A refusal opens nothing; each wraps ErrNoCodeHost and names the repository key to fix.
func (e *Engine) repositoryTargets(ctx context.Context, l domain.Line, urls []string) ([]mergeTarget, error) {
	repo := l.Runtime.Repository

	own, err := e.codeRepositories(ctx, l, []string{repo.URL})
	if err != nil {
		return nil, fmt.Errorf("runtime.repository.url: %w", err)
	}

	host := own[0].repo.Host

	targets, err := e.codeRepositories(ctx, l, urls)
	if err != nil {
		return nil, err
	}

	for _, t := range targets {
		if t.repo.Host.Host != host.Host {
			return nil, refusal(l, fmt.Errorf("%s is not on the host of the repository %q (runtime.repository.url), the only one a version on secrets merges on: %w",
				t.url, repo.URL, ErrNoCodeHost))
		}
	}

	if repo.Forge != "" && repo.Forge != string(host.Kind) {
		return nil, refusal(l, fmt.Errorf("the repository %s names runtime.repository.forge %s, but the organization lists %s as a %s host: %w",
			repo.URL, repo.Forge, host.Host, host.Kind, ErrNoCodeHost))
	}

	names := domain.SecretNames(repo.Token)
	if len(names) == 0 {
		return nil, refusal(l, fmt.Errorf("the repository %s names no runtime.repository.token, which merges its pull requests: %w", repo.URL, ErrNoCodeHost))
	}

	values, err := e.openSecrets(ctx, names, domain.SecretUse{Factory: l.FactoryID}, "", repositoryLogin)
	if err != nil {
		return nil, err
	}

	token := domain.FillSecrets(repo.Token, values)
	for i := range targets {
		targets[i].token = token
	}

	return targets, nil
}

// refusal adds to err why no run of l can start, when one cannot: a merge refused for a version
// that does not run says how to fix the version.
func refusal(l domain.Line, err error) error {
	if reason := l.Refusal(); reason != "" {
		return fmt.Errorf("%w (%s)", err, reason)
	}

	return err
}

// itemHeads are, for each repository key, the branches a pull request of it may come from (ADR 0041
// decision 5): its own branch (domain.Item.Branch) and, for a landing root, its integration
// branch in that repository, from its repository row, else work_items.integration_branch.
func (e *Engine) itemHeads(ctx context.Context, it domain.Item) (func(key string) []string, error) {
	own := it.Branch()
	if !it.Landing() {
		return func(string) []string { return []string{own} }, nil
	}

	rows, err := e.store.ItemRepositories(ctx, it.ID)
	if err != nil {
		return nil, fmt.Errorf("repositories of %s: %w", it.ID, err)
	}

	return func(key string) []string {
		branch := it.IntegrationBranch
		if i := slices.IndexFunc(rows, func(r domain.ItemRepository) bool { return r.Repo == key && r.IntegrationBranch != "" }); i >= 0 {
			branch = rows[i].IntegrationBranch
		}

		return []string{own, branch}
	}, nil
}

// theItems refuses pr, the pull request at url, unless its head is one of heads in its own
// repository: a fork's, or another branch's, is no station's work (ErrNotTheItems).
func theItems(url string, pr ports.PullRequest, heads []string) error {
	if !pr.SameRepository {
		return fmt.Errorf("%s comes from a fork (branch %s), not from the item's branch %s in that repository: %w: %w",
			url, pr.HeadBranch, heads[0], ErrNotTheItems, ports.ErrNotMergeable)
	}

	if !slices.Contains(heads, pr.HeadBranch) {
		return fmt.Errorf("%s comes from branch %s, not the item's branch (%s): no station checked it: %w: %w",
			url, pr.HeadBranch, strings.Join(slices.Compact(heads), " or "), ErrNotTheItems, ports.ErrNotMergeable)
	}

	return nil
}

// mergeOne merges one pull request unless it already is, deletes its head branch, and returns
// the data of its merged event. A pull request not from heads is refused (theItems) first.
func (e *Engine) mergeOne(ctx context.Context, id domain.ItemID, t mergeTarget, heads []string, method string, lease *domain.Lease) (map[string]any, error) {
	pr, err := e.merge(ctx, t, heads, method, lease)
	if err != nil {
		return nil, err
	}

	data := map[string]any{"pull_request": t.url, "sha": pr.MergeSHA, "method": method, "branch_deleted": false, domain.KeyPlanned: true}

	// Only the pull request's own branch, never a fork's or the branch it merged into.
	if pr.SameRepository && pr.HeadBranch != "" && pr.HeadBranch != pr.BaseBranch {
		if err := t.host.DeleteBranch(ctx, t.repo, t.token, t.url, pr.HeadBranch); err != nil {
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
// UpdateBranch), and merged at the head the next read returns (settled); a conflict is a
// *mergeConflictError. A refusal of an open pull request with no conflict (the update or the
// merge refused, its mergeability still pending, not mergeable after a clean update) is a
// *notMergeableYetError: after a clean update the host knows of no conflict, and the port
// cannot tell required checks that have not run yet on the new head from ones that fail.
func (e *Engine) merge(ctx context.Context, t mergeTarget, heads []string, method string, lease *domain.Lease) (ports.PullRequest, error) {
	pr, err := t.host.PullRequest(ctx, t.repo, t.token, t.url)
	if err != nil {
		return ports.PullRequest{}, err
	}

	if err := theItems(t.url, pr, heads); err != nil {
		return ports.PullRequest{}, err
	}

	if pr.Merged {
		return pr, nil
	}

	if !pr.Open {
		return ports.PullRequest{}, fmt.Errorf("%s is closed: %w", t.url, ports.ErrNotMergeable)
	}

	if err := t.host.UpdateBranch(ctx, t.repo, t.token, t.url, pr.HeadSHA); err != nil {
		if mc, ok := errors.AsType[*ports.MergeConflictError](err); ok {
			return ports.PullRequest{}, &mergeConflictError{url: t.url, base: pr.BaseBranch, files: mc.Files, err: err}
		}

		return ports.PullRequest{}, yet(t.url, fmt.Errorf("update the branch of %s: %w", t.url, err))
	}

	if pr, err = e.settled(ctx, t, lease); err != nil {
		return ports.PullRequest{}, err
	}

	if pr.MergeabilityPending {
		return ports.PullRequest{}, yet(t.url, fmt.Errorf("%s: the host is still computing whether it can be merged, try again shortly: %w",
			t.url, ports.ErrNotMergeable))
	}

	if !pr.Mergeable {
		return ports.PullRequest{}, yet(t.url, fmt.Errorf("%s is not mergeable yet: the host reports conflicts or required checks not passed on its head: %w",
			t.url, ports.ErrNotMergeable))
	}

	if _, err := t.host.Merge(ctx, t.repo, t.token, t.url, ports.Merge{Method: method, HeadSHA: pr.HeadSHA}); err != nil {
		return ports.PullRequest{}, yet(t.url, err)
	}

	// Trust the host's record, not the merge call's answer.
	if pr, err = t.host.PullRequest(ctx, t.repo, t.token, t.url); err != nil {
		return ports.PullRequest{}, fmt.Errorf("verify the merge of %s: %w", t.url, err)
	}

	if !pr.Merged {
		return ports.PullRequest{}, fmt.Errorf("%s reports not merged after merging", t.url)
	}

	return pr, nil
}

// mergePoll is the first wait before a gate's merge reads a pull request whose mergeability is
// pending again; each wait after it is twice as long, up to what is left of Config.MergeWait.
const mergePoll = 2 * time.Second

// settled reads the pull request after its branch was updated. Without a lease (a person's
// merge, an HTTP request) that is one read. With one, a pull request whose mergeability the host
// is still computing is read again after a growing wait on the engine clock, heartbeating the
// lease (backoff), until it is known or Config.MergeWait has passed.
func (e *Engine) settled(ctx context.Context, t mergeTarget, lease *domain.Lease) (ports.PullRequest, error) {
	pr, err := t.host.PullRequest(ctx, t.repo, t.token, t.url)

	for waited, wait := time.Duration(0), mergePoll; err == nil && lease != nil && pr.MergeabilityPending && waited < e.cfg.MergeWait; wait *= 2 {
		d := min(wait, e.cfg.MergeWait-waited)
		if err := e.backoff(ctx, *lease, d); err != nil {
			return ports.PullRequest{}, fmt.Errorf("wait for the mergeability of %s: %w", t.url, err)
		}

		waited += d
		pr, err = t.host.PullRequest(ctx, t.repo, t.token, t.url)
	}

	if err != nil {
		return ports.PullRequest{}, fmt.Errorf("read %s after updating its branch: %w", t.url, err)
	}

	return pr, nil
}

// notMergeableYetError is a refusal to merge an open pull request that is no conflict and may pass
// with time alone: its mergeability still pending, required checks not yet passed on the head
// its branch update made, its head moved meanwhile. A gate's merge refused for one goes back to
// the gate's queue (deferMerge) instead of blocking.
type notMergeableYetError struct {
	url string
	err error // wraps ports.ErrNotMergeable
}

func (n *notMergeableYetError) Error() string { return n.err.Error() }

func (n *notMergeableYetError) Unwrap() error { return n.err }

// yet marks err a *notMergeableYetError when it is a refusal (ports.ErrNotMergeable); any other
// error, such as the host being unreachable, is returned as it is.
func yet(url string, err error) error {
	if !errors.Is(err, ports.ErrNotMergeable) {
		return err
	}

	return &notMergeableYetError{url: url, err: err}
}

// deferMerge sends a claimed gate item whose merge was refused with a *notMergeableYetError
// back to the gate's arrival state, so its agent runs again (waiting for the checks on the new
// head) and approves again, with a merge_deferred event counting the deferrals in a row, and
// returns nil.
// Any other refusal, and the one after Config.MergeDeferrals deferrals in a row, is returned,
// naming the count, for the item to block.
func (e *Engine) deferMerge(ctx context.Context, log *slog.Logger, c claimed, err error, t *ports.Transition) error {
	ny, ok := errors.AsType[*notMergeableYetError](err)
	if !ok {
		return err
	}

	events, rerr := e.itemEvents(ctx, c.item.ID)
	if rerr != nil {
		return fmt.Errorf("%w (its deferrals could not be counted: %w)", err, rerr)
	}

	n := deferralsInARow(events)
	if n >= e.cfg.MergeDeferrals {
		return fmt.Errorf("not merged after %d deferrals in a row: %w", n, err)
	}

	t.To, t.Outcome = domain.Position{Station: c.station.ID, State: c.station.Arrival()}, domain.LeaseReleased
	t.Events = append(t.Events, event(c.item, domain.EventMergeDeferred, t.At, domain.ActorEngine,
		map[string]any{keyReason: err.Error(), "pull_request": ny.url, "deferrals": n + 1}))

	log.InfoContext(ctx, "merge deferred", slog.String("pull_request", ny.url), slog.Int("deferrals", n+1))

	return nil
}

// deferralsInARow counts the merge_deferred events since the item's latest move or retry, so a
// person's retry, and any move of the item, starts the count again.
func deferralsInARow(events []domain.Event) int {
	n := 0

	for _, ev := range slices.Backward(events) {
		if ev.Kind == domain.EventMoved || ev.Kind == domain.EventRetried {
			return n
		}

		if ev.Kind == domain.EventMergeDeferred {
			n++
		}
	}

	return n
}

// mergeConflictError is a merge refused because a pull request's branch conflicts with its base. Its
// message is the reason the item is sent back with: the pull request, its base, the files when
// the host named them, and what to do.
type mergeConflictError struct {
	url, base string
	files     []string
	err       error // the code host's, wrapping ports.ErrMergeConflict
}

func (c *mergeConflictError) Error() string {
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
func (c *mergeConflictError) Unwrap() []error { return []error{c.err, ports.ErrNotMergeable} }

// conflictRework points t back at the line's rework target for it (the gate's onReject, else the
// nearest agent station upstream) when err is a merge conflict and the line's
// flow.assemblies.onConflict is rework, with a merge_conflict event giving the reason, and
// reports whether it did. It is no rejection: t.Rework stays false, so the item's rework count
// and its gate's maxRework are untouched.
func (e *Engine) conflictRework(l domain.Line, it domain.Item, err error, t *ports.Transition) bool {
	mc, ok := errors.AsType[*mergeConflictError](err)
	if !ok || l.Flow.Assemblies.OnConflict == domain.ConflictHuman {
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
