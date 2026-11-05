package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.RepositoryTests = (*RepositoryTests)(nil)

// RepositoryTests keeps the latest repository tests in memory with the database's rules: one per
// factory and URL, organizations kept apart. It knows only the factories AddFactory registered,
// and RemoveFactory takes a factory's tests with it, as the database's cascade does.
type RepositoryTests struct {
	mu        sync.Mutex
	factories map[domain.FactoryID]domain.OrgID
	rows      map[domain.OrgID]map[[2]string]domain.RepositoryTest
}

// NewRepositoryTests returns no tests and no factories.
func NewRepositoryTests() *RepositoryTests {
	return &RepositoryTests{factories: map[domain.FactoryID]domain.OrgID{}, rows: map[domain.OrgID]map[[2]string]domain.RepositoryTest{}}
}

// AddFactory registers factory as org's.
func (r *RepositoryTests) AddFactory(org domain.OrgID, factory domain.FactoryID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.factories[factory] = org
}

// RemoveFactory forgets factory and its tests.
func (r *RepositoryTests) RemoveFactory(factory domain.FactoryID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	org := r.factories[factory]
	delete(r.factories, factory)

	maps.DeleteFunc(r.rows[org], func(k [2]string, _ domain.RepositoryTest) bool { return k[0] == string(factory) })
}

// Record implements ports.RepositoryTests.
func (r *RepositoryTests) Record(ctx context.Context, t domain.RepositoryTest) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	if err := t.Check(); err != nil {
		return fmt.Errorf("record repository test: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if got, ok := r.factories[t.Factory]; !ok || got != org {
		return fmt.Errorf("record repository test of factory %s: %w", t.Factory, ports.ErrNotFound)
	}

	if r.rows[org] == nil {
		r.rows[org] = map[[2]string]domain.RepositoryTest{}
	}

	t.At = t.At.UTC()
	r.rows[org][[2]string{string(t.Factory), t.URL}] = t

	return nil
}

// List implements ports.RepositoryTests.
func (r *RepositoryTests) List(ctx context.Context, factory domain.FactoryID) ([]domain.RepositoryTest, error) {
	return r.list(ctx, func(t domain.RepositoryTest) bool { return t.Factory == factory })
}

// BySecret implements ports.RepositoryTests.
func (r *RepositoryTests) BySecret(ctx context.Context, secret string) ([]domain.RepositoryTest, error) {
	return r.list(ctx, func(t domain.RepositoryTest) bool { return t.Secret == secret })
}

// list is the organization's tests keep takes, by factory and URL.
func (r *RepositoryTests) list(ctx context.Context, keep func(domain.RepositoryTest) bool) ([]domain.RepositoryTest, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	out := []domain.RepositoryTest{}

	for _, key := range slices.SortedFunc(maps.Keys(r.rows[org]), func(a, b [2]string) int {
		return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]))
	}) {
		if t := r.rows[org][key]; keep(t) {
			out = append(out, t)
		}
	}

	return out, nil
}
