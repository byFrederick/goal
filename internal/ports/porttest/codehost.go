package porttest

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.CodeHost = (*CodeHost)(nil)

// CodeHost is an in-memory code host holding the pull requests a test adds. It merges like a
// careful host: only open, mergeable pull requests whose head has not moved.
type CodeHost struct {
	mu       sync.Mutex
	prs      map[string]*ports.PullRequest
	merges   []ports.Merge
	branches []string
}

// NewCodeHost returns a host with no pull requests.
func NewCodeHost() *CodeHost { return &CodeHost{prs: map[string]*ports.PullRequest{}} }

// Add puts a pull request on the host, keyed by its URL.
func (c *CodeHost) Add(pr ports.PullRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.prs[pr.URL] = &pr
}

// Merges returns the merges asked for, in order.
func (c *CodeHost) Merges() []ports.Merge {
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
func (c *CodeHost) PullRequest(_ context.Context, _ domain.Repository, url string) (ports.PullRequest, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	pr, ok := c.prs[url]
	if !ok {
		return ports.PullRequest{}, fmt.Errorf("pull request %s: %w", url, ports.ErrNotFound)
	}

	return *pr, nil
}

// Merge implements ports.CodeHost.
func (c *CodeHost) Merge(_ context.Context, _ domain.Repository, url string, m ports.Merge) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.merges = append(c.merges, m)

	pr, ok := c.prs[url]
	switch {
	case !ok:
		return "", fmt.Errorf("pull request %s: %w", url, ports.ErrNotFound)
	case !pr.Open || !pr.Mergeable:
		return "", fmt.Errorf("pull request %s: %w", url, ports.ErrNotMergeable)
	case pr.HeadSHA != m.HeadSHA:
		return "", fmt.Errorf("pull request %s: head moved: %w", url, ports.ErrNotMergeable)
	}

	pr.Open, pr.Merged, pr.Mergeable, pr.MergeSHA = false, true, false, "merge-"+pr.HeadSHA

	return pr.MergeSHA, nil
}

// DeleteBranch implements ports.CodeHost.
func (c *CodeHost) DeleteBranch(_ context.Context, _ domain.Repository, branch string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.branches = append(c.branches, branch)

	return nil
}
