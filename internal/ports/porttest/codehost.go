package porttest

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.CodeHost = (*CodeHost)(nil)

// CodeHost is an in-memory code host holding the pull requests a test adds. Like the adapters, it
// serves only pull requests on the host of the connection's git login, and it merges like a
// careful host: only open, mergeable pull requests whose head has not moved.
type CodeHost struct {
	mu       sync.Mutex
	prs      map[string]*ports.PullRequest
	moves    map[string]string // url → the head a push moves it to once it is next read
	merges   []MergeCall
	branches []string
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
	}

	pr.Open, pr.Merged, pr.Mergeable, pr.MergeSHA = false, true, false, "merge-"+pr.HeadSHA

	return pr.MergeSHA, nil
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

// onHost refuses a pull request URL that is not on the host of the connection's git login.
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
