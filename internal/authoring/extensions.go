package authoring

import (
	"context"
	"encoding/json"
	"io/fs"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
)

// Credentials checks what a definition signs in with, in the organization of ctx: the connections
// check (CheckConnections), which goald wires as ConnectionsCheck. Its problems carry Check =
// CheckConnections; an error is a failure to check, never a finding.
type Credentials interface {
	Check(ctx context.Context, f *config.Factory) ([]domain.DefinitionProblem, error)
}

// Check is a validation step another phase registers (phase 4's connector checker). Its
// problems carry Check = ID().
type Check interface {
	ID() string
	Check(ctx context.Context, in CheckInput) []domain.DefinitionProblem
}

// CheckInput is the definition a Check validates.
type CheckInput struct {
	// Factory is nil when factory.yaml does not parse or fails the schema.
	Factory *config.Factory
	Model   Model
	Ranges  Ranges
	Files   fs.FS
}

// Resolver turns a definition's validated connections into what runs on the version, at
// publish (phase 4.2). A failure refuses the publish.
type Resolver interface {
	Resolve(ctx context.Context, f *config.Factory) (Resolution, error)
}

// Resolution is a Resolver's result. Phase 4.2 adds the connection bindings, and the
// Resolved and Bindings fields of ports.Publish that store it in the publish transaction.
type Resolution struct {
	Resolved json.RawMessage
}
