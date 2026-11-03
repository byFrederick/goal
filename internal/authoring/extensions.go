package authoring

import (
	"context"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
)

// Credentials checks what a definition signs in with, in the organization of ctx: the secrets
// check (CheckSecrets), which goald wires as SecretsCheck. Its problems carry Check =
// CheckSecrets; an error is a failure to check, never a finding.
type Credentials interface {
	Check(ctx context.Context, f *config.Factory) ([]domain.DefinitionProblem, error)
}
