package porttest

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Credentials = (*Credentials)(nil)

// Credentials keeps credentials in memory with the database's rules: names unique per
// organization, each value sealed with the master key under domain.CredentialAAD and returned only
// by Resolve, the factory scope enforced there, and an audit entry through audit for every change
// and use. Views made with WithKey share the data, as goalds with different keys share one
// database. No connection refers to a credential yet, so ImportPreview counts none.
type Credentials struct {
	d     *credentialData
	key   ports.MasterKey
	audit ports.Audit
}

type credentialData struct {
	mu   sync.Mutex
	rows map[domain.OrgID]map[string]*credentialRow
}

type credentialRow struct {
	cred              domain.Credential
	keyID             string
	nonce, ciphertext []byte
}

// NewCredentials returns credentials sealed with key and audited through audit (a Tenancy that
// knows the organizations).
func NewCredentials(key ports.MasterKey, audit ports.Audit) *Credentials {
	return &Credentials{key: key, audit: audit, d: &credentialData{rows: map[domain.OrgID]map[string]*credentialRow{}}}
}

// WithKey returns a view of c's data sealed with key.
func (c *Credentials) WithKey(key ports.MasterKey) *Credentials {
	return &Credentials{d: c.d, key: key, audit: c.audit}
}

// List implements ports.Credentials.
func (c *Credentials) List(ctx context.Context) ([]domain.Credential, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	out := []domain.Credential{}
	for _, name := range slices.Sorted(maps.Keys(c.d.rows[org])) {
		out = append(out, cloneCredential(c.d.rows[org][name].cred))
	}

	return out, nil
}

// Get implements ports.Credentials.
func (c *Credentials) Get(ctx context.Context, name string) (domain.Credential, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Credential{}, domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	r, ok := c.d.rows[org][name]
	if !ok {
		return domain.Credential{}, fmt.Errorf("credential %q: %w", name, ports.ErrNotFound)
	}

	return cloneCredential(r.cred), nil
}

// Create implements ports.Credentials.
func (c *Credentials) Create(ctx context.Context, n domain.NewCredential, by domain.Actor) (domain.Credential, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Credential{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.Credential{}, err
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	if _, taken := c.d.rows[org][n.Name]; taken {
		return domain.Credential{}, fmt.Errorf("credential %q: %w", n.Name, ports.ErrNameTaken)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return domain.Credential{}, err
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	cred := cloneCredential(n.Credential())
	cred.ID, cred.CreatedBy, cred.CreatedAt, cred.UpdatedBy, cred.UpdatedAt = domain.CredentialID(id.String()), by, now, by, now

	r := &credentialRow{cred: cred}
	if err := c.sealLocked(org, r, n.Value); err != nil {
		return domain.Credential{}, err
	}

	err = c.appendAudit(ctx, by, domain.AuditCredentialCreated, r.cred, map[string]any{auditName: n.Name, "type": string(n.Type)})
	if err != nil {
		return domain.Credential{}, err
	}

	if c.d.rows[org] == nil {
		c.d.rows[org] = map[string]*credentialRow{}
	}

	c.d.rows[org][n.Name] = r

	return cloneCredential(r.cred), nil
}

// Patch implements ports.Credentials.
func (c *Credentials) Patch(ctx context.Context, name string, p domain.CredentialPatch, by domain.Actor) (domain.Credential, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Credential{}, domain.ErrNoOrg
	}

	if err := p.CheckValue(); err != nil {
		return domain.Credential{}, err
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	r, ok := c.d.rows[org][name]
	if !ok {
		return domain.Credential{}, fmt.Errorf("credential %q: %w", name, ports.ErrNotFound)
	}

	next := cloneCredential(p.Apply(r.cred))
	if err := domain.CheckCredential(next); err != nil {
		return domain.Credential{}, err
	}

	if next.Name != name {
		if _, taken := c.d.rows[org][next.Name]; taken {
			return domain.Credential{}, fmt.Errorf("credential %q: %w", next.Name, ports.ErrNameTaken)
		}
	}

	sealed := &credentialRow{cred: next, keyID: r.keyID, nonce: r.nonce, ciphertext: r.ciphertext}
	if p.Replace != nil {
		if err := c.sealLocked(org, sealed, *p.Replace); err != nil {
			return domain.Credential{}, err
		}
	}

	sealed.cred.UpdatedBy, sealed.cred.UpdatedAt = by, time.Now().UTC().Truncate(time.Microsecond)

	if p.ChangesMetadata() {
		data := map[string]any{auditName: next.Name}
		if next.Name != name {
			data["from"] = name
		}

		if err := c.appendAudit(ctx, by, domain.AuditCredentialUpdated, next, data); err != nil {
			return domain.Credential{}, err
		}
	}

	if p.Replace != nil {
		if err := c.appendAudit(ctx, by, domain.AuditCredentialValueReplaced, next, map[string]any{auditName: next.Name}); err != nil {
			return domain.Credential{}, err
		}
	}

	*r = *sealed
	delete(c.d.rows[org], name)
	c.d.rows[org][next.Name] = r

	return cloneCredential(r.cred), nil
}

// Delete implements ports.Credentials.
func (c *Credentials) Delete(ctx context.Context, name string, by domain.Actor) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	r, ok := c.d.rows[org][name]
	if !ok {
		return fmt.Errorf("credential %q: %w", name, ports.ErrNotFound)
	}

	if err := c.appendAudit(ctx, by, domain.AuditCredentialDeleted, r.cred, map[string]any{auditName: name}); err != nil {
		return err
	}

	delete(c.d.rows[org], name)

	return nil
}

// Resolve implements ports.Credentials.
func (c *Credentials) Resolve(ctx context.Context, names []string, use domain.CredentialUse) ([]domain.ResolvedCredential, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	var (
		out  []domain.ResolvedCredential
		rows []*credentialRow
	)

	for _, name := range names {
		r, ok := c.d.rows[org][name]
		if !ok {
			return nil, fmt.Errorf("credential %q: %w", name, ports.ErrNotFound)
		}

		if slices.Contains(rows, r) {
			continue
		}

		if !r.cred.Scope.Allows(use.Factory) {
			return nil, fmt.Errorf("credential %q, factory %s: %w", name, use.Factory, ports.ErrCredentialNotAllowed)
		}

		value, err := c.openLocked(org, r)
		if err != nil {
			return nil, err
		}

		rows = append(rows, r)
		out = append(out, domain.ResolvedCredential{Credential: cloneCredential(r.cred), Value: value})
	}

	now := time.Now().UTC().Truncate(time.Microsecond)

	for _, r := range rows {
		if err := c.appendAudit(ctx, domain.EngineActor, domain.AuditCredentialUsed, r.cred, use.AuditData(r.cred.Name)); err != nil {
			return nil, err
		}

		r.cred.LastUsedAt = &now
	}

	for i := range out {
		out[i].Credential.LastUsedAt = &now
	}

	return out, nil
}

// ImportPreview implements ports.Credentials.
func (c *Credentials) ImportPreview(ctx context.Context, names []string) ([]domain.CredentialImportPreview, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	var errs []error
	for _, name := range names {
		if _, err := domain.CredentialEnvName(name); err != nil {
			errs = append(errs, err)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	out := make([]domain.CredentialImportPreview, 0, len(names))
	for _, name := range names {
		_, exists := c.d.rows[org][name]
		out = append(out, domain.CredentialImportPreview{Name: name, Exists: exists})
	}

	return out, nil
}

func (c *Credentials) sealLocked(org domain.OrgID, r *credentialRow, value string) error {
	keyID, nonce, ct, err := c.key.Seal([]byte(value), domain.CredentialAAD(org, r.cred.ID, c.key.Active()))
	if err != nil {
		return err
	}

	r.keyID, r.nonce, r.ciphertext = keyID, nonce, ct

	return nil
}

func (c *Credentials) openLocked(org domain.OrgID, r *credentialRow) (string, error) {
	plain, err := c.key.Open(r.keyID, r.nonce, r.ciphertext, domain.CredentialAAD(org, r.cred.ID, r.keyID))
	if err != nil {
		return "", fmt.Errorf("credential %s: %w", r.cred.Name, err)
	}

	return string(plain), nil
}

func (c *Credentials) appendAudit(ctx context.Context, by domain.Actor, action string, cred domain.Credential, data map[string]any) error {
	return c.audit.Append(ctx, domain.AuditEntry{
		Actor: by, Action: action, TargetKind: domain.AuditTargetCredential, TargetID: string(cred.ID), Data: data,
	})
}

// cloneCredential is a deep copy of c.
func cloneCredential(c domain.Credential) domain.Credential {
	c.Scope.Factories = slices.Clone(c.Scope.Factories)

	if c.Agents != nil {
		agents := make(map[domain.FactoryID][]string, len(c.Agents))
		for f, names := range c.Agents {
			agents[f] = slices.Clone(names)
		}

		c.Agents = agents
	}

	for _, t := range []**time.Time{&c.ExpiresAt, &c.LastUsedAt} {
		if *t != nil {
			v := **t
			*t = &v
		}
	}

	return c
}
