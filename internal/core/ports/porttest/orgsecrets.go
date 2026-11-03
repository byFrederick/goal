package porttest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.OrgSecrets = (*OrgSecrets)(nil)

// OrgSecrets keeps organizations' secrets in memory with the database's rules: names unique per
// organization, each value sealed with the master key under domain.SecretAAD and returned only by
// Resolve, the factory scope enforced there, and an audit entry through audit for every change and
// use. Views made with WithKey share the data, as goalds with different keys share one database.
// The connections given to NewOrgSecrets are the ones that refer to secrets as {{name}}: Delete
// refuses while one does.
type OrgSecrets struct {
	d     *secretData
	key   ports.MasterKey
	audit ports.Audit
	conns *Connections
}

type secretData struct {
	mu   sync.Mutex
	rows map[domain.OrgID]map[string]*secretRow
}

type secretRow struct {
	secret            domain.Secret
	keyID             string
	nonce, ciphertext []byte
}

// NewOrgSecrets returns secrets sealed with key and audited through audit (a Tenancy that knows
// the organizations), referred to by conns' connections; nil conns has none.
func NewOrgSecrets(key ports.MasterKey, audit ports.Audit, conns *Connections) *OrgSecrets {
	return &OrgSecrets{key: key, audit: audit, conns: conns, d: &secretData{rows: map[domain.OrgID]map[string]*secretRow{}}}
}

// WithKey returns a view of s's data sealed with key.
func (s *OrgSecrets) WithKey(key ports.MasterKey) *OrgSecrets {
	return &OrgSecrets{d: s.d, key: key, audit: s.audit, conns: s.conns}
}

// CopySealed copies the sealed value of secret from of fromOrg onto secret to of toOrg, as
// someone with the database's rows in hand would (secrettest's Harness.CopySealed).
func (s *OrgSecrets) CopySealed(fromOrg domain.OrgID, from string, toOrg domain.OrgID, to string) error {
	s.d.mu.Lock()
	defer s.d.mu.Unlock()

	src, dst := s.d.rows[fromOrg][from], s.d.rows[toOrg][to]
	if src == nil || dst == nil {
		return fmt.Errorf("copy the sealed value of %s to %s: %w", from, to, ports.ErrNotFound)
	}

	dst.keyID, dst.nonce, dst.ciphertext = src.keyID, slices.Clone(src.nonce), slices.Clone(src.ciphertext)

	return nil
}

// List implements ports.OrgSecrets.
func (s *OrgSecrets) List(ctx context.Context) ([]domain.Secret, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	s.d.mu.Lock()
	defer s.d.mu.Unlock()

	out := []domain.Secret{}
	for _, name := range slices.Sorted(maps.Keys(s.d.rows[org])) {
		out = append(out, cloneSecret(s.d.rows[org][name].secret))
	}

	return out, nil
}

// Get implements ports.OrgSecrets.
func (s *OrgSecrets) Get(ctx context.Context, name string) (domain.Secret, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Secret{}, domain.ErrNoOrg
	}

	s.d.mu.Lock()
	defer s.d.mu.Unlock()

	r, err := s.rowLocked(org, name)
	if err != nil {
		return domain.Secret{}, err
	}

	return cloneSecret(r.secret), nil
}

// Create implements ports.OrgSecrets.
func (s *OrgSecrets) Create(ctx context.Context, n domain.NewSecret, by domain.Actor) (domain.Secret, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Secret{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.Secret{}, err
	}

	s.d.mu.Lock()
	defer s.d.mu.Unlock()

	if _, taken := s.d.rows[org][n.Name]; taken {
		return domain.Secret{}, fmt.Errorf("secret %q: %w", n.Name, ports.ErrNameTaken)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return domain.Secret{}, err
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	sec := cloneSecret(n.Secret())
	sec.ID, sec.CreatedBy, sec.CreatedAt, sec.UpdatedBy, sec.UpdatedAt = domain.SecretID(id.String()), by, now, by, now

	r := &secretRow{secret: sec}
	if err := s.sealLocked(org, r, n.Value); err != nil {
		return domain.Secret{}, err
	}

	if err := s.appendAudit(ctx, by, domain.AuditSecretCreated, r.secret, map[string]any{auditName: n.Name}); err != nil {
		return domain.Secret{}, err
	}

	if s.d.rows[org] == nil {
		s.d.rows[org] = map[string]*secretRow{}
	}

	s.d.rows[org][n.Name] = r

	return cloneSecret(r.secret), nil
}

// Patch implements ports.OrgSecrets.
func (s *OrgSecrets) Patch(ctx context.Context, name string, p domain.SecretPatch, by domain.Actor) (domain.Secret, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Secret{}, domain.ErrNoOrg
	}

	if err := p.CheckValue(); err != nil {
		return domain.Secret{}, err
	}

	s.d.mu.Lock()
	defer s.d.mu.Unlock()

	r, err := s.rowLocked(org, name)
	if err != nil {
		return domain.Secret{}, err
	}

	if p.Empty() {
		return cloneSecret(r.secret), nil
	}

	next := cloneSecret(p.Apply(r.secret))
	if err := domain.CheckSecret(next); err != nil {
		return domain.Secret{}, err
	}

	if next.Name != name {
		if _, taken := s.d.rows[org][next.Name]; taken {
			return domain.Secret{}, fmt.Errorf("secret %q: %w", next.Name, ports.ErrNameTaken)
		}
	}

	sealed := &secretRow{secret: next, keyID: r.keyID, nonce: r.nonce, ciphertext: r.ciphertext}
	if p.Replace != nil {
		if err := s.sealLocked(org, sealed, *p.Replace); err != nil {
			return domain.Secret{}, err
		}
	}

	sealed.secret.UpdatedBy, sealed.secret.UpdatedAt = by, time.Now().UTC().Truncate(time.Microsecond)

	if err := s.patchAudits(ctx, by, name, next, p); err != nil {
		return domain.Secret{}, err
	}

	*r = *sealed

	delete(s.d.rows[org], name)
	s.d.rows[org][next.Name] = r

	return cloneSecret(r.secret), nil
}

// Delete implements ports.OrgSecrets.
func (s *OrgSecrets) Delete(ctx context.Context, name string, by domain.Actor) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	s.d.mu.Lock()
	defer s.d.mu.Unlock()

	r, err := s.rowLocked(org, name)
	if err != nil {
		return err
	}

	if s.conns != nil {
		if users := s.conns.referrers(org, name); len(users) > 0 {
			return fmt.Errorf("secret %q is used by connections %s: %w", name, strings.Join(users, ", "), ports.ErrConflict)
		}
	}

	if err := s.appendAudit(ctx, by, domain.AuditSecretDeleted, r.secret, map[string]any{auditName: name}); err != nil {
		return err
	}

	delete(s.d.rows[org], name)

	return nil
}

// Resolve implements ports.OrgSecrets.
func (s *OrgSecrets) Resolve(ctx context.Context, names []string, use domain.SecretUse) ([]domain.ResolvedSecret, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	s.d.mu.Lock()
	defer s.d.mu.Unlock()

	var (
		out  []domain.ResolvedSecret
		rows []*secretRow
	)

	for _, name := range names {
		r, ok := s.d.rows[org][name]
		if !ok {
			return nil, fmt.Errorf("secret %q: %w", name, ports.ErrNotFound)
		}

		if slices.Contains(rows, r) {
			continue
		}

		if !r.secret.Scope.Allows(use.Factory) {
			return nil, fmt.Errorf("secret %q, factory %s: %w", name, use.Factory, ports.ErrSecretNotAllowed)
		}

		plain, err := s.key.Open(r.keyID, r.nonce, r.ciphertext, domain.SecretAAD(org, r.secret.ID, r.keyID))
		if err != nil {
			return nil, fmt.Errorf("secret %s: %w", name, err)
		}

		rows = append(rows, r)
		out = append(out, domain.ResolvedSecret{Secret: cloneSecret(r.secret), Value: string(plain)})
	}

	now := time.Now().UTC().Truncate(time.Microsecond)

	for _, r := range rows {
		if err := s.appendAudit(ctx, domain.EngineActor, domain.AuditSecretUsed, r.secret, use.AuditData(r.secret.Name)); err != nil {
			return nil, err
		}

		r.secret.LastUsedAt = &now
	}

	for i := range out {
		out[i].Secret.LastUsedAt = &now
	}

	return out, nil
}

// rowLocked is org's secret name; ErrNotFound when there is none.
func (s *OrgSecrets) rowLocked(org domain.OrgID, name string) (*secretRow, error) {
	r, ok := s.d.rows[org][name]
	if !ok {
		return nil, fmt.Errorf("secret %q: %w", name, ports.ErrNotFound)
	}

	return r, nil
}

// patchAudits are the entries of p's change of secret from, now next.
func (s *OrgSecrets) patchAudits(ctx context.Context, by domain.Actor, from string, next domain.Secret, p domain.SecretPatch) error {
	if p.ChangesMetadata() {
		data := map[string]any{auditName: next.Name}
		if next.Name != from {
			data["from"] = from
		}

		if err := s.appendAudit(ctx, by, domain.AuditSecretUpdated, next, data); err != nil {
			return err
		}
	}

	if p.Replace == nil {
		return nil
	}

	return s.appendAudit(ctx, by, domain.AuditSecretValueReplaced, next, map[string]any{auditName: next.Name})
}

func (s *OrgSecrets) sealLocked(org domain.OrgID, r *secretRow, value string) error {
	keyID, nonce, ct, err := s.key.Seal([]byte(value), domain.SecretAAD(org, r.secret.ID, s.key.Active()))
	if err != nil {
		return err
	}

	r.keyID, r.nonce, r.ciphertext = keyID, nonce, ct

	return nil
}

func (s *OrgSecrets) appendAudit(ctx context.Context, by domain.Actor, action string, sec domain.Secret, data map[string]any) error {
	return s.audit.Append(ctx, domain.AuditEntry{
		Actor: by, Action: action, TargetKind: domain.AuditTargetSecret, TargetID: string(sec.ID), Data: data,
	})
}

// cloneSecret is a deep copy of s.
func cloneSecret(s domain.Secret) domain.Secret {
	s.Scope.Factories = slices.Clone(s.Scope.Factories)

	for _, t := range []**time.Time{&s.ExpiresAt, &s.LastUsedAt} {
		if *t != nil {
			v := **t
			*t = &v
		}
	}

	return s
}
