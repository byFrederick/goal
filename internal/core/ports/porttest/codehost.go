package porttest

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.CodeHost = (*CodeHost)(nil)

// CodeHost is an in-memory code host holding the pull requests, branch heads and commits a test
// adds. Like the adapters, it serves only pull requests and repositories on the host of the
// connection's git login, and it merges and updates branches like a careful host: only open pull
// requests whose head has not moved.
type CodeHost struct {
	mu       sync.Mutex
	prs      map[string]*ports.PullRequest
	moves    map[string]string // url → the head a push moves it to once it is next read
	pending  map[string]int    // url → how many more reads report its mergeability pending
	reads    map[string]int    // url → how many times it was read
	refuse   map[string]int    // url → how many more merges are refused though it reads mergeable
	merges   []MergeCall
	branches []string
	updated  []string                        // urls whose branches were updated, in order
	conflict map[string][]string             // url → the files its branch conflicts with its base in
	current  map[string]bool                 // urls whose branches already hold their base
	heads    map[string]map[string]string    // repo → branch ("" the default) → head SHA
	history  map[string][]ports.Commit       // repo → its linear history, oldest first
	repos    map[string]ports.RepositoryList // owner → what Repositories answers for it
	login    string                          // the owner an empty one means
}

// SetRepositories makes list what Repositories answers for list.Owner; the first list set is
// the signed-in account's, which an empty owner means. A CodeHost given none cannot list
// repositories: Repositories answers errors.ErrUnsupported, as GitLab does.
func (c *CodeHost) SetRepositories(list ports.RepositoryList) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.repos == nil {
		c.repos, c.login = map[string]ports.RepositoryList{}, list.Owner
	}

	list.Owners = slices.Clone(list.Owners)
	list.Repositories = slices.Clone(list.Repositories)
	c.repos[list.Owner] = list
}

// SetBranchHead makes sha the head of branch in the repository at repo; the empty branch is the
// default one.
func (c *CodeHost) SetBranchHead(repo, branch, sha string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.heads == nil {
		c.heads = map[string]map[string]string{}
	}

	if c.heads[repo] == nil {
		c.heads[repo] = map[string]string{}
	}

	c.heads[repo][branch] = sha
}

// AddCommits appends commits to the linear history of the repository at repo, oldest first.
func (c *CodeHost) AddCommits(repo string, commits ...ports.Commit) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.history == nil {
		c.history = map[string][]ports.Commit{}
	}

	for _, cm := range commits {
		cm.Files = slices.Clone(cm.Files)
		c.history[repo] = append(c.history[repo], cm)
	}
}

// UpToDate makes the branch of the pull request at url already hold its base: updating it
// succeeds and leaves its head where it is.
func (c *CodeHost) UpToDate(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.current == nil {
		c.current = map[string]bool{}
	}

	c.current[url] = true
}

// Conflict makes updating the branch of the pull request at url conflict with its base in files.
func (c *CodeHost) Conflict(url string, files ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conflict == nil {
		c.conflict = map[string][]string{}
	}

	c.conflict[url] = slices.Clone(files)
}

// Resolve undoes Conflict: the branch of the pull request at url updates cleanly again.
func (c *CodeHost) Resolve(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.conflict, url)
}

// UpdatedBranches returns the pull requests whose branches were updated, in order.
func (c *CodeHost) UpdatedBranches() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.updated)
}

// MoveHeadAfterRead pushes sha to the pull request at url right after it is next read, as a push
// landing between a merger's read and its merge.
func (c *CodeHost) MoveHeadAfterRead(url, sha string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.moves == nil {
		c.moves = map[string]string{}
	}

	c.moves[url] = sha
}

// PendingReads makes the next n reads of the pull request at url report its mergeability as
// still being computed (MergeabilityPending), as a host does for a while after its branch moved;
// the reads after them report what the pull request is.
func (c *CodeHost) PendingReads(url string, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pending == nil {
		c.pending = map[string]int{}
	}

	c.pending[url] = n
}

// RefuseMerges makes the next n merges of the pull request at url refused with
// ports.ErrNotMergeable although it reads mergeable, as GitHub refuses one whose required checks
// have not run on its head yet.
func (c *CodeHost) RefuseMerges(url string, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.refuse == nil {
		c.refuse = map[string]int{}
	}

	c.refuse[url] = n
}

// Reads returns how many times the pull request at url was read.
func (c *CodeHost) Reads(url string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.reads[url]
}

// MergeCall is one merge asked of the fake: the pull request, the connection it came with and how.
type MergeCall struct {
	URL        string
	Connection string
	ports.Merge
}

// NewCodeHost returns a host with no pull requests.
func NewCodeHost() *CodeHost { return &CodeHost{prs: map[string]*ports.PullRequest{}} }

// Add puts a pull request on the host, keyed by its URL, replacing one with the same URL.
func (c *CodeHost) Add(pr ports.PullRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.prs[pr.URL] = &pr
}

// Merges returns the merges asked for, in order.
func (c *CodeHost) Merges() []MergeCall {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.merges)
}

// DeletedBranches returns the branches deleted, in order.
func (c *CodeHost) DeletedBranches() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.branches)
}

// PullRequest implements ports.CodeHost.
func (c *CodeHost) PullRequest(_ context.Context, conn domain.ResolvedConnection, url string) (ports.PullRequest, error) {
	if err := onHost(conn, url); err != nil {
		return ports.PullRequest{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	pr, ok := c.prs[url]
	if !ok {
		return ports.PullRequest{}, fmt.Errorf("pull request %s does not exist: %w", url, ports.ErrNotMergeable)
	}

	read := *pr

	if c.reads == nil {
		c.reads = map[string]int{}
	}

	c.reads[url]++

	if c.pending[url] > 0 {
		read.MergeabilityPending = true
		c.pending[url]--
	}

	if sha, ok := c.moves[url]; ok {
		pr.HeadSHA = sha

		delete(c.moves, url)
	}

	return read, nil
}

// Merge implements ports.CodeHost.
func (c *CodeHost) Merge(_ context.Context, conn domain.ResolvedConnection, url string, m ports.Merge) (string, error) {
	if err := onHost(conn, url); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.merges = append(c.merges, MergeCall{URL: url, Connection: conn.Connection.Name, Merge: m})

	pr, ok := c.prs[url]
	switch {
	case !ok:
		return "", fmt.Errorf("pull request %s does not exist: %w", url, ports.ErrNotMergeable)
	case !pr.Open || !pr.Mergeable || pr.MergeabilityPending:
		return "", fmt.Errorf("pull request %s: %w", url, ports.ErrNotMergeable)
	case pr.HeadSHA != m.HeadSHA:
		return "", fmt.Errorf("pull request %s: head moved: %w", url, ports.ErrNotMergeable)
	case c.refuse[url] > 0:
		c.refuse[url]--
		return "", fmt.Errorf("pull request %s: required checks are expected: %w", url, ports.ErrNotMergeable)
	}

	pr.Open, pr.Merged, pr.Mergeable, pr.MergeSHA = false, true, false, "merge-"+pr.HeadSHA

	return pr.MergeSHA, nil
}

// UpdateBranch implements ports.CodeHost. An update moves the head to "updated-" and the head
// it was given; an up-to-date branch (UpToDate) is left as it is.
func (c *CodeHost) UpdateBranch(_ context.Context, conn domain.ResolvedConnection, url, headSHA string) error {
	if err := onHost(conn, url); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	pr, ok := c.prs[url]
	switch {
	case !ok:
		return fmt.Errorf("pull request %s does not exist: %w", url, ports.ErrNotMergeable)
	case !pr.Open:
		return fmt.Errorf("pull request %s is closed: %w", url, ports.ErrNotMergeable)
	case pr.HeadSHA != headSHA:
		return fmt.Errorf("pull request %s: head moved: %w", url, ports.ErrNotMergeable)
	}

	if files, ok := c.conflict[url]; ok {
		return &ports.MergeConflictError{URL: url, Files: slices.Clone(files)}
	}

	if c.current[url] {
		return nil
	}

	c.updated = append(c.updated, url)
	pr.HeadSHA = "updated-" + headSHA

	return nil
}

// DeleteBranch implements ports.CodeHost.
func (c *CodeHost) DeleteBranch(_ context.Context, conn domain.ResolvedConnection, url, branch string) error {
	if err := onHost(conn, url); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.branches = append(c.branches, branch)

	return nil
}

// BranchHead implements ports.CodeHost from SetBranchHead.
func (c *CodeHost) BranchHead(_ context.Context, conn domain.ResolvedConnection, repo, branch string) (string, error) {
	if err := onHost(conn, repo); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	sha, ok := c.heads[repo][branch]
	if !ok {
		return "", fmt.Errorf("branch %q of %s: %w", branch, repo, ports.ErrNotFound)
	}

	return sha, nil
}

// Compare implements ports.CodeHost from AddCommits: the commits after base up to head, none
// when head is not after base.
func (c *CodeHost) Compare(_ context.Context, conn domain.ResolvedConnection, repo, base, head string) (ports.Comparison, error) {
	if err := onHost(conn, repo); err != nil {
		return ports.Comparison{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	history, ok := c.history[repo]
	if !ok {
		return ports.Comparison{}, fmt.Errorf("repository %s: %w", repo, ports.ErrNotFound)
	}

	index := func(sha string) int {
		return slices.IndexFunc(history, func(cm ports.Commit) bool { return cm.SHA == sha })
	}

	from, to := index(base), index(head)
	if from < 0 || to < 0 {
		return ports.Comparison{}, fmt.Errorf("compare %s...%s in %s: %w", base, head, repo, ports.ErrNotFound)
	}

	var out ports.Comparison

	for i := from + 1; i <= to; i++ {
		if len(out.Commits) == ports.MaxCompareCommits {
			out.Truncated = true

			break
		}

		cm := history[i]
		cm.Files = slices.Clone(cm.Files)
		out.Commits = append(out.Commits, cm)
	}

	return out, nil
}

// Repositories implements ports.CodeHost from SetRepositories.
func (c *CodeHost) Repositories(_ context.Context, conn domain.ResolvedConnection, owner string) (ports.RepositoryList, error) {
	if _, _, err := conn.GitLogin(); err != nil {
		return ports.RepositoryList{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.repos == nil {
		return ports.RepositoryList{}, fmt.Errorf("repositories: %w", errors.ErrUnsupported)
	}

	if owner == "" {
		owner = c.login
	}

	list, ok := c.repos[owner]
	if !ok {
		return ports.RepositoryList{}, fmt.Errorf("repositories of %s: %w", owner, ports.ErrNotFound)
	}

	list.Owners = slices.Clone(list.Owners)
	list.Repositories = slices.Clone(list.Repositories)

	return list, nil
}

// onHost refuses a pull request or repository URL that is not on the host of the connection's git login.
func onHost(conn domain.ResolvedConnection, raw string) error {
	host, _, err := conn.GitLogin()
	if err != nil {
		return err
	}

	if u, err := url.Parse(raw); err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, host) {
		return fmt.Errorf("%s is not on %s: %w", raw, host, ports.ErrNotMergeable)
	}

	return nil
}
