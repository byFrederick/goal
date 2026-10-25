package authoring

import (
	"context"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
)

// Credentials checks what a definition signs in with, in the organization of ctx: the connections
// check (CheckConnections), which goald wires as ConnectionsCheck. Its problems carry Check =
// CheckConnections; an error is a failure to check, never a finding.
type Credentials interface {
	Check(ctx context.Context, f *config.Factory) ([]domain.DefinitionProblem, error)
}
