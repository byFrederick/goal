package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Credentials = (*Credentials)(nil)

// Credentials implements ports.Credentials over 00086's secrets, which 00065's credentials became
// (ADR 0037): the credentials API keeps its contract until the secrets API replaces it. It reads
// and writes the table as Secrets does (secretTable), audited as credential.*, and a credential is
// what domain.Credential.Stored says: an api_key with no agents, since a secret has neither, the
// type and agents a write sends checked and dropped. Each value is sealed under
// domain.CredentialAAD, the same bytes as domain.SecretAAD. Connections refer to a credential as
// {{name}} in 00070's secret_refs: UsedBy and ImportPreview count them, and Delete refuses while
// one does.
type Credentials struct{ t secretTable }

// NewCredentials returns the credentials on s's pool, sealed with key.
func NewCredentials(s *Store, key ports.MasterKey) *Credentials {
	return &Credentials{t: secretTable{s: s, key: key, kind: credentialKind}}
}

// List implements ports.Credentials.
func (c *Credentials) List(ctx context.Context) ([]domain.Credential, error) {
	secrets, err := c.t.list(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]domain.Credential, 0, len(secrets))
	for _, s := range secrets {
		out = append(out, credentialOf(s))
	}

	return out, nil
}

// Get implements ports.Credentials.
func (c *Credentials) Get(ctx context.Context, name string) (domain.Credential, error) {
	s, err := c.t.get(ctx, name)

	return credentialOf(s), err
}

// Create implements ports.Credentials.
func (c *Credentials) Create(ctx context.Context, n domain.NewCredential, by domain.Actor) (domain.Credential, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.Credential{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.Credential{}, fmt.Errorf("create credential: %w", err)
	}

	s, err := c.t.create(ctx, domain.NewSecret{
		Name: n.Name, Source: domain.SecretSourceValue, Description: n.Description,
		Scope: domain.SecretScope{All: n.Scope.All, Factories: n.Scope.Factories}, ExpiresAt: n.ExpiresAt, RemindDays: n.RemindDays,
		Value: n.Value,
	}, by, nil)

	return credentialOf(s), err
}

// Patch implements ports.Credentials. Its type and agents are checked with the rest and dropped.
func (c *Credentials) Patch(ctx context.Context, name string, p domain.CredentialPatch, by domain.Actor) (domain.Credential, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.Credential{}, domain.ErrNoOrg
	}

	sp := domain.SecretPatch{Name: p.Name, Description: p.Description, Replace: p.Replace}

	if p.Scope != nil {
		sp.Scope = &domain.SecretScope{All: p.Scope.All, Factories: p.Scope.Factories}
	}

	if p.Expiry != nil {
		sp.Expiry = &domain.SecretExpiry{At: p.Expiry.At, RemindDays: p.Expiry.RemindDays}
	}

	// A patch of the type or the agents alone stores nothing, but is checked and audited as the
	// change it was.
	s, err := c.t.patch(ctx, name, sp, by, p.ChangesMetadata(), func(s domain.Secret) error {
		return domain.CheckCredential(p.Apply(credentialOf(s)))
	})

	return credentialOf(s), err
}

// Delete implements ports.Credentials.
func (c *Credentials) Delete(ctx context.Context, name string, by domain.Actor) error {
	return c.t.delete(ctx, name, by)
}

// UsedBy implements ports.Credentials. Connections referred to credentials; with them gone
// (00135) nothing does, and the deprecated answers keep their shape with no users.
func (c *Credentials) UsedBy(ctx context.Context, _ string) ([]string, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	return []string{}, nil
}

// Resolve implements ports.Credentials.
func (c *Credentials) Resolve(ctx context.Context, names []string, use domain.CredentialUse) ([]domain.ResolvedCredential, error) {
	opened, err := c.t.resolve(ctx, names, domain.SecretUse(use))
	if err != nil {
		return nil, err
	}

	out := make([]domain.ResolvedCredential, 0, len(opened))
	for _, o := range opened {
		out = append(out, domain.ResolvedCredential{Credential: credentialOf(o.Secret), Value: o.Value})
	}

	return out, nil
}

// ImportPreview implements ports.Credentials.
func (c *Credentials) ImportPreview(ctx context.Context, names []string) ([]domain.CredentialImportPreview, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return nil, domain.ErrNoOrg
	}

	var errs []error

	for _, name := range names {
		if _, err := domain.CredentialEnvName(name); err != nil {
			errs = append(errs, err)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("import preview: %w", err)
	}

	existing, err := fetch(ctx, c.t.s.read, func(q *db.Queries) ([]string, error) { return q.SecretNames(ctx, names) })
	if err != nil {
		return nil, fmt.Errorf("import preview: %w", err)
	}

	// No connection refers to a name any more (00135), so every count is zero.
	out := make([]domain.CredentialImportPreview, 0, len(names))
	for _, name := range names {
		out = append(out, domain.CredentialImportPreview{Name: name, Exists: slices.Contains(existing, name)})
	}

	return out, nil
}

// credentialOf is secret s as the credentials API shows it (domain.Credential.Stored).
func credentialOf(s domain.Secret) domain.Credential {
	if s.ID == "" {
		return domain.Credential{}
	}

	return domain.Credential{
		ID: domain.CredentialID(s.ID), Name: s.Name, Description: s.Description,
		Scope:     domain.CredentialScope{All: s.Scope.All, Factories: s.Scope.Factories},
		ExpiresAt: s.ExpiresAt, RemindDays: s.RemindDays, LastUsedAt: s.LastUsedAt,
		CreatedBy: s.CreatedBy, CreatedAt: s.CreatedAt, UpdatedBy: s.UpdatedBy, UpdatedAt: s.UpdatedAt,
	}.Stored()
}
