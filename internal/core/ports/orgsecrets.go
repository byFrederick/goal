package ports

import (
	"context"
	"errors"

	"github.com/byFrederick/goal/internal/core/domain"
)

// ErrSecretNotAllowed means a secret's scope leaves out the factory of the run that would open it
// (ADR 0037 decision 1). Errors that wrap it name the secret and the factory, never the value.
var ErrSecretNotAllowed = errors.New("the secret does not allow this factory")

// OrgSecrets are an organization's secrets (ADR 0037), for the organization in ctx: the only
// place a value lives (Secrets is the platform's own, resolved at the edge). A value is sealed
// with MasterKey under domain.SecretAAD (organization, secret id and key id) and is write-only:
// only Resolve returns it, no error holds it, and replacing it is its only write. Every change and
// use is audited in its transaction (secret.created, .updated, .value_replaced, .deleted, .used).
type OrgSecrets interface {
	// List is the organization's secrets, by name.
	List(ctx context.Context) ([]domain.Secret, error)
	// Get is the secret name. ErrNotFound when there is none.
	Get(ctx context.Context, name string) (domain.Secret, error)
	// Create creates a secret (secret.created). n must pass Check (domain.ErrInvalid);
	// ErrNameTaken for a name in use, ErrNoMasterKey without a master key.
	Create(ctx context.Context, n domain.NewSecret, by domain.Actor) (domain.Secret, error)
	// Patch changes a secret's metadata (secret.updated), the result checked with
	// domain.CheckSecret, and with Replace its value (secret.value_replaced): runs that start
	// afterwards open the new one. ErrNotFound when there is none, ErrNameTaken when another secret
	// has the new name, ErrNoMasterKey for Replace without a master key.
	Patch(ctx context.Context, name string, p domain.SecretPatch, by domain.Actor) (domain.Secret, error)
	// Delete deletes a secret (secret.deleted). ErrNotFound when there is none; ErrConflict,
	// deleting nothing, while a connection refers to it as {{name}} or a knowledge source reads
	// with it, naming them.
	Delete(ctx context.Context, name string, by domain.Actor) error
	// Resolve opens the named secrets for use's run, audits each as secret.used with the factory,
	// agent and run, and sets last_used_at. ErrNotFound names the first unknown one, and
	// ErrSecretNotAllowed the first whose scope leaves out use.Factory; then nothing is recorded.
	Resolve(ctx context.Context, names []string, use domain.SecretUse) ([]domain.ResolvedSecret, error)
}
