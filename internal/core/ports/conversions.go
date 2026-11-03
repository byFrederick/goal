package ports

import (
	"context"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// FactoryConversion is how the one-time conversion to secrets and environment files (ADR 0037
// decision 19) went for one factory: converted, From to To, or refused with Problems, each naming
// what to fix. Names maps each connection From used to its secret fields' secret names. It holds
// no value.
type FactoryConversion struct {
	Factory  domain.FactoryID
	From     domain.DefinitionVersionID
	To       domain.DefinitionVersionID
	Problems []string
	Names    map[string]map[string]string
	At       time.Time
}

// Refused reports whether the factory kept its version.
func (c FactoryConversion) Refused() bool { return c.To == "" }

// FactoryConversions keep each factory's conversion, for the organization in ctx: one per
// factory, kept for good, so the conversion runs once.
type FactoryConversions interface {
	// Record keeps c as its factory's conversion and reports whether it did: false, changing
	// nothing, when the factory has one already. A converted c has no problems, a refused one at
	// least one (domain.ErrInvalid otherwise); ErrNotFound for a factory or version the
	// organization does not have.
	Record(ctx context.Context, c FactoryConversion) (bool, error)
	// List is the organization's conversions, by factory.
	List(ctx context.Context) ([]FactoryConversion, error)
	// WithLock runs fn while this instance holds the install's conversion lock, and reports
	// whether it ran: false, without running fn, while another instance holds it. It needs no
	// organization.
	WithLock(ctx context.Context, fn func(ctx context.Context) error) (bool, error)
}
