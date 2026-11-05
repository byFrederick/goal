package ports

import (
	"context"

	"github.com/byFrederick/goal/internal/core/domain"
)

// RepositoryTests keep the latest repository test of each factory and repository, or owner
// pattern (ADR 0041 decision 8), for the organization in ctx. Pages read them instead of asking
// the code host on every view. A test holds the host's reasons scrubbed of the token, never a
// secret value.
type RepositoryTests interface {
	// Record keeps t as the latest test of its factory and URL, replacing the one before. t must
	// pass Check (domain.ErrInvalid); ErrNotFound for a factory the organization does not have.
	Record(ctx context.Context, t domain.RepositoryTest) error
	// List is factory's latest tests, sorted by URL in byte order.
	List(ctx context.Context, factory domain.FactoryID) ([]domain.RepositoryTest, error)
	// BySecret is the organization's latest tests whose token came from secret, by factory and URL.
	BySecret(ctx context.Context, secret string) ([]domain.RepositoryTest, error)
}
