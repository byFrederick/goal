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
// adds, each keyed by its repository's URL. Like the adapters, it refuses a call without a token
// or a repository, and a pull request URL that is not the repository's, and it merges and
// updates branches like a careful host: only open pull requests whose head has not moved.
type CodeHost struct {
	mu       sync.Mutex
	prs      map[string]*ports.PullRequest
	moves    map[string]string // url → the head a push moves it to once it is next read
	pending  map[string]int    // url → how many more reads report its mergeability pending
	reads    map[string]int    // url → how many times it was read
	refuse   map[string]int    // url → how many more merges are refused though it reads mergeable
	merges   []MergeCall
	branches []string
	updated  []string                          // urls whose branches were updated, in order
	conflict map[string][]string               // url → the files its branch conflicts with its base in
	current  map[string]bool                   // urls whose branches already hold their base
	heads    map[string]map[string]string      // repo → branch ("" the default) → head SHA
	history  map[string][]ports.Commit         // repo → its linear history, oldest first
	repos    map[string]ports.RepositoryList   // owner → what Repositories answers for it
	login    string                            // the owner an empty one means
	access   map[string]ports.RepositoryAccess // repo + " " + token → what RepositoryAccess answers
	stall    bool                              // RepositoryAccess waits for its context to end
	tokens   []string                          // the tokens RepositoryAccess was given, in order
	owners   map[string]ports.OwnerAccess      // host/owner + " " + token → what OwnerAccess answers
	logins   []string                          // the tokens of the other calls, in order
}

// SetAccess makes a what RepositoryAccess answers for token in the repository at repo. Any other
// repository or token reads as one the host does not show: read and push failed.
func (c *CodeHost) SetAccess(repo, token string, a ports.RepositoryAccess) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.access == nil {
		c.access = map[string]ports.RepositoryAccess{}
	}

	c.access[repoKey(repo)+" "+token] = a
}

// SetOwnerAccess makes a what OwnerAccess answers for token in the repositories of owner on the
// host named host (ignoring case). Any other host, owner or token reads as one the host shows no
// repositories of.
func (c *CodeHost) SetOwnerAccess(host, owner, token string, a ports.OwnerAccess) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.owners == nil {
		c.owners = map[string]ports.OwnerAccess{}
	}

	c.owners[ownerKey(host, owner)+" "+token] = a
}

// StallAccess makes RepositoryAccess give no answer until its context ends, as a host that does
// not answer.
func (c *CodeHost) StallAccess() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stall = true
}

// AccessTokens are the tokens RepositoryAccess was given, in order.
func (c *CodeHost) AccessTokens() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.tokens)
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

	key := repoKey(repo)
	if c.heads[key] == nil {
		c.heads[key] = map[string]string{}
	}

	c.heads[key][branch] = sha
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
		c.history[repoKey(repo)] = append(c.history[repoKey(repo)], cm)
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

// MergeCall is one merge asked of the fake: the pull request, the token it came with and how.
type MergeCall struct {
	URL   string
	Token string
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
func (c *CodeHost) PullRequest(_ context.Context, repo domain.CodeRepository, token, url string) (ports.PullRequest, error) {
	if err := c.inRepository(repo, token, url); err != nil {
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
func (c *CodeHost) Merge(_ context.Context, repo domain.CodeRepository, token, url string, m ports.Merge) (string, error) {
	if err := c.inRepository(repo, token, url); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.merges = append(c.merges, MergeCall{URL: url, Token: token, Merge: m})

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
func (c *CodeHost) UpdateBranch(_ context.Context, repo domain.CodeRepository, token, url, headSHA string) error {
	if err := c.inRepository(repo, token, url); err != nil {
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
func (c *CodeHost) DeleteBranch(_ context.Context, repo domain.CodeRepository, token, url, branch string) error {
	if err := c.inRepository(repo, token, url); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.branches = append(c.branches, branch)

	return nil
}

// BranchHead implements ports.CodeHost from SetBranchHead.
func (c *CodeHost) BranchHead(_ context.Context, repo domain.CodeRepository, token, branch string) (string, error) {
	if err := c.inRepository(repo, token, ""); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	sha, ok := c.heads[repo.Key()][branch]
	if !ok {
		return "", fmt.Errorf("branch %q of %s: %w", branch, repo, ports.ErrNotFound)
	}

	return sha, nil
}

// Compare implements ports.CodeHost from AddCommits: the commits after base up to head, none
// when head is not after base.
func (c *CodeHost) Compare(_ context.Context, repo domain.CodeRepository, token, base, head string) (ports.Comparison, error) {
	if err := c.inRepository(repo, token, ""); err != nil {
		return ports.Comparison{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	history, ok := c.history[repo.Key()]
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
func (c *CodeHost) Repositories(_ context.Context, host domain.CodeHost, token, owner string) (ports.RepositoryList, error) {
	if host.Host == "" || token == "" {
		return ports.RepositoryList{}, fmt.Errorf("repositories: no host or no token: %w", domain.ErrInvalid)
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

// RepositoryAccess implements ports.CodeHost from SetAccess and StallAccess.
func (c *CodeHost) RepositoryAccess(ctx context.Context, repo domain.CodeRepository, token string) (ports.RepositoryAccess, error) {
	if repo.Host.Host == "" || repo.Name == "" {
		return ports.RepositoryAccess{}, fmt.Errorf("repository access of %s: no repository: %w", repo, domain.ErrInvalid)
	}

	c.mu.Lock()
	c.tokens = append(c.tokens, token)
	stall := c.stall
	a, ok := c.access[repo.Key()+" "+token]
	c.mu.Unlock()

	if stall {
		<-ctx.Done()

		return ports.RepositoryAccess{}, fmt.Errorf("repository access of %s: %w", repo, ctx.Err())
	}

	if !ok {
		reason := "the host answered 404: Not Found"

		return ports.RepositoryAccess{Read: ports.AccessCheck{Reason: reason}, Push: ports.AccessCheck{Reason: reason}}, nil
	}

	return a, nil
}

// OwnerAccess implements ports.CodeHost from SetOwnerAccess.
func (c *CodeHost) OwnerAccess(_ context.Context, host domain.CodeHost, token, owner string) (ports.OwnerAccess, error) {
	if host.Host == "" || token == "" || owner == "" {
		return ports.OwnerAccess{}, fmt.Errorf("owner access of %s: no host, token or owner: %w", owner, domain.ErrInvalid)
	}

	c.mu.Lock()
	a, ok := c.owners[ownerKey(host.Host, owner)+" "+token]
	c.mu.Unlock()

	if !ok {
		return ports.OwnerAccess{Reason: "the host shows the token no repositories of " + owner}, nil
	}

	return a, nil
}

// ownerKey is the lower-case host/owner of an owner's repositories.
func ownerKey(host, owner string) string {
	return strings.ToLower(host + "/" + owner)
}

// LoginTokens are the tokens the pull request and repository calls were made with, in order,
// refused ones included.
func (c *CodeHost) LoginTokens() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.logins)
}

// inRepository records token and refuses a call without a token or a repository, or for a pull
// request URL (when raw is set) that is not one of repo's.
func (c *CodeHost) inRepository(repo domain.CodeRepository, token, raw string) error {
	c.mu.Lock()
	c.logins = append(c.logins, token)
	c.mu.Unlock()

	if token == "" || repo.Host.Host == "" || repo.Owner == "" || repo.Name == "" {
		return fmt.Errorf("%s: no repository or no token: %w", repo, domain.ErrInvalid)
	}

	if raw == "" {
		return nil
	}

	if got, err := domain.ParseCodeRepository([]domain.CodeHost{repo.Host}, raw); err != nil || got.Key() != repo.Key() {
		return fmt.Errorf("%s is not a pull request of %s: %w", raw, repo, ports.ErrNotMergeable)
	}

	return nil
}

// repoKey is the lower-case host/owner/name of the repository URL raw, as domain.CodeRepository's
// Key, whatever its host.
func repoKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	return strings.ToLower(u.Host + "/" + strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"))
}
