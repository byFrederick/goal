package porttest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.FactoryConversions = (*FactoryConversions)(nil)

// FactoryConversions keeps factory conversions in memory with the database's rules: one per
// factory, kept, organizations apart. It knows only the factories AddFactory registered; its lock
// is one mutex, held by one WithLock at a time.
type FactoryConversions struct {
	mu        sync.Mutex
	lock      sync.Mutex
	factories map[domain.FactoryID]domain.OrgID
	rows      map[domain.OrgID]map[domain.FactoryID]ports.FactoryConversion
}

// NewFactoryConversions returns no conversions and no factories.
func NewFactoryConversions() *FactoryConversions {
	return &FactoryConversions{factories: map[domain.FactoryID]domain.OrgID{}, rows: map[domain.OrgID]map[domain.FactoryID]ports.FactoryConversion{}}
}

// AddFactory registers factory as org's.
func (f *FactoryConversions) AddFactory(org domain.OrgID, factory domain.FactoryID) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.factories[factory] = org
}

// Record implements ports.FactoryConversions.
func (f *FactoryConversions) Record(ctx context.Context, c ports.FactoryConversion) (bool, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return false, domain.ErrNoOrg
	}

	if err := CheckConversion(c); err != nil {
		return false, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.factories[c.Factory] != org {
		return false, fmt.Errorf("record conversion of factory %s: %w", c.Factory, ports.ErrNotFound)
	}

	if _, ok := f.rows[org][c.Factory]; ok {
		return false, nil
	}

	if f.rows[org] == nil {
		f.rows[org] = map[domain.FactoryID]ports.FactoryConversion{}
	}

	f.rows[org][c.Factory] = cloneConversion(c)

	return true, nil
}

// List implements ports.FactoryConversions.
func (f *FactoryConversions) List(ctx context.Context) ([]ports.FactoryConversion, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	out := []ports.FactoryConversion{}
	for _, id := range slices.Sorted(maps.Keys(f.rows[org])) {
		out = append(out, cloneConversion(f.rows[org][id]))
	}

	return out, nil
}

// WithLock implements ports.FactoryConversions.
func (f *FactoryConversions) WithLock(ctx context.Context, fn func(ctx context.Context) error) (bool, error) {
	if !f.lock.TryLock() {
		return false, nil
	}
	defer f.lock.Unlock()

	return true, fn(ctx)
}

// CheckConversion is the rule the database's check enforces: a converted factory has no problems,
// a refused one at least one.
func CheckConversion(c ports.FactoryConversion) error {
	if c.Refused() != (len(c.Problems) > 0) {
		return fmt.Errorf("conversion of factory %s: a converted factory has no problems and a refused one has some: %w", c.Factory, domain.ErrInvalid)
	}

	return nil
}

func cloneConversion(c ports.FactoryConversion) ports.FactoryConversion {
	c.Problems = slices.Clone(c.Problems)

	names := make(map[string]map[string]string, len(c.Names))
	for k, v := range c.Names {
		names[k] = maps.Clone(v)
	}

	c.Names, c.At = names, c.At.UTC()

	return c
}
