package ports

import (
	"context"
	"errors"

	"github.com/byFrederick/goal/internal/core/domain"
)

// ErrCredentialNotAllowed means a credential's scope leaves out the factory of the run that would
// open it (ADR 0017 decisions 4 and 5). Errors that wrap it name the credential and the factory,
// never the value.
var ErrCredentialNotAllowed = errors.New("the credential does not allow this factory")

// Credentials are an organization's credentials (ADR 0017), for the organization in ctx: named
// secrets that connections refer to as {{name}} and agents get as env. A value is sealed with
// MasterKey under domain.CredentialAAD (organization, credential id and key id) and is
// write-only: only Resolve returns it, and no error holds it. Every change and use is audited in
// its transaction (credential.created, .updated, .value_replaced, .deleted, .used).
type Credentials interface {
	// List is the organization's credentials, by name.
	List(ctx context.Context) ([]domain.Credential, error)
	// Get is the credential name. ErrNotFound when there is none.
	Get(ctx context.Context, name string) (domain.Credential, error)
	// Create creates a credential (credential.created). n must pass Check (domain.ErrInvalid);
	// ErrNameTaken for a name in use, ErrNoMasterKey without a master key.
	Create(ctx context.Context, n domain.NewCredential, by domain.Actor) (domain.Credential, error)
	// Patch changes a credential's metadata (credential.updated), the result checked with
	// domain.CheckCredential, and with Replace its value (credential.value_replaced): runs that
	// start afterwards open the new one. ErrNotFound when there is none, ErrNameTaken when another
	// credential has the new name, ErrNoMasterKey for Replace without a master key.
	Patch(ctx context.Context, name string, p domain.CredentialPatch, by domain.Actor) (domain.Credential, error)
	// Delete deletes a credential (credential.deleted). ErrNotFound when there is none; the
	// connection-refs part adds ErrConflict while a connection refers to it.
	Delete(ctx context.Context, name string, by domain.Actor) error
	// Resolve opens the named credentials for use's run, audits each as credential.used with the
	// factory, agent and run, and sets last_used_at. ErrNotFound names the first unknown one, and
	// ErrCredentialNotAllowed the first whose scope leaves out use.Factory; then nothing is
	// recorded. The agent list is not checked here: a reference from a connection ignores it, and
	// the engine checks direct use with Credential.ForAgent.
	Resolve(ctx context.Context, names []string, use domain.CredentialUse) ([]domain.ResolvedCredential, error)
	// ImportPreview is an import's dry run (ADR 0017 decision 9): for each name, in order, whether
	// a credential has it and how many connections use that one. It stores nothing. A name that is
	// not a credential name is domain.ErrInvalid.
	ImportPreview(ctx context.Context, names []string) ([]domain.CredentialImportPreview, error)
}
