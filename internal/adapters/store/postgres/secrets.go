package postgres

import (
	"context"
	"fmt"
	"maps"
	"math"
	"strings"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.OrgSecrets = (*Secrets)(nil)

// Secrets implements ports.OrgSecrets over 00086's secrets, as goal_app under row-level security:
// every statement sees the organization in ctx, and every change and use appends its audit entry
// in its transaction. Each value is sealed with key under domain.SecretAAD (organization, secret
// id and key id) and opened only by Resolve; a rename keeps the sealed value, which is bound to
// the id. Without a master key (the zero masterkey.Keys) metadata works and Create, a Replace and
// Resolve fail with ports.ErrNoMasterKey. Connections refer to a secret as {{name}} in 00070's
// secret_refs: Delete locks the secret and refuses while one does, as a connection's write locks
// the secrets it refers to (ShareReferencedCredentials), so neither can slip past the other.
type Secrets struct{ t secretTable }

// NewSecrets returns the secrets on s's pool, sealed with key.
func NewSecrets(s *Store, key ports.MasterKey) *Secrets {
	return &Secrets{t: secretTable{s: s, key: key, kind: secretKind}}
}

// List implements ports.OrgSecrets.
func (s *Secrets) List(ctx context.Context) ([]domain.Secret, error) { return s.t.list(ctx) }

// Get implements ports.OrgSecrets.
func (s *Secrets) Get(ctx context.Context, name string) (domain.Secret, error) {
	return s.t.get(ctx, name)
}

// Create implements ports.OrgSecrets.
func (s *Secrets) Create(ctx context.Context, n domain.NewSecret, by domain.Actor) (domain.Secret, error) {
	if err := n.Check(); err != nil {
		return domain.Secret{}, fmt.Errorf("create secret: %w", err)
	}

	return s.t.create(ctx, n, by, nil)
}

// Patch implements ports.OrgSecrets.
func (s *Secrets) Patch(ctx context.Context, name string, p domain.SecretPatch, by domain.Actor) (domain.Secret, error) {
	return s.t.patch(ctx, name, p, by, p.ChangesMetadata(), domain.CheckSecret)
}

// Delete implements ports.OrgSecrets.
func (s *Secrets) Delete(ctx context.Context, name string, by domain.Actor) error {
	return s.t.delete(ctx, name, by)
}

// Resolve implements ports.OrgSecrets.
func (s *Secrets) Resolve(ctx context.Context, names []string, use domain.SecretUse) ([]domain.ResolvedSecret, error) {
	return s.t.resolve(ctx, names, use)
}

// secretsNameKey keeps a secret's name unique in its organization.
const secretsNameKey = "secrets_org_id_name_key" //nolint:gosec // a constraint name, not a credential

// tableKind is what a secretTable's rows are called and audited as: secrets, or the credentials
// the credentials API still serves from the same table until the secrets API replaces it.
type tableKind struct {
	// noun names a row in errors.
	noun                                      string
	target                                    string
	created, updated, replaced, deleted, used string
	// notAllowed is Resolve's error for a factory outside the scope.
	notAllowed error
}

var (
	secretKind = tableKind{
		noun: "secret", target: domain.AuditTargetSecret,
		created: domain.AuditSecretCreated, updated: domain.AuditSecretUpdated, replaced: domain.AuditSecretValueReplaced,
		deleted: domain.AuditSecretDeleted, used: domain.AuditSecretUsed, notAllowed: ports.ErrSecretNotAllowed,
	}
	credentialKind = tableKind{
		noun: "credential", target: domain.AuditTargetCredential,
		created: domain.AuditCredentialCreated, updated: domain.AuditCredentialUpdated, replaced: domain.AuditCredentialValueReplaced,
		deleted: domain.AuditCredentialDeleted, used: domain.AuditCredentialUsed, notAllowed: ports.ErrCredentialNotAllowed,
	}
)

// secretTable is the secrets table's reads and writes, audited as kind: Secrets and Credentials
// are each one.
type secretTable struct {
	s    *Store
	key  ports.MasterKey
	kind tableKind
}

func (t secretTable) list(ctx context.Context) ([]domain.Secret, error) {
	rows, err := fetch(ctx, t.s.read, func(q *db.Queries) ([]db.SecretsRow, error) { return q.Secrets(ctx) })
	if err != nil {
		return nil, fmt.Errorf("%ss: %w", t.kind.noun, err)
	}

	out := make([]domain.Secret, 0, len(rows))
	for _, r := range rows {
		out = append(out, secretFrom(db.SecretRow(r)))
	}

	return out, nil
}

func (t secretTable) get(ctx context.Context, name string) (domain.Secret, error) {
	r, err := fetch(ctx, t.s.read, func(q *db.Queries) (db.SecretRow, error) { return q.Secret(ctx, name) })

	switch {
	case notFound(err):
		return domain.Secret{}, fmt.Errorf("%s %q: %w", t.kind.noun, name, ports.ErrNotFound)
	case err != nil:
		return domain.Secret{}, fmt.Errorf("%s %q: %w", t.kind.noun, name, err)
	default:
		return secretFrom(r), nil
	}
}

// create inserts n, which the caller checked, and audits it with data beside its name.
func (t secretTable) create(ctx context.Context, n domain.NewSecret, by domain.Actor, data map[string]any) (domain.Secret, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Secret{}, domain.ErrNoOrg
	}

	id, err := newID()
	if err != nil {
		return domain.Secret{}, err
	}

	out := n.Secret()
	out.ID, out.CreatedBy, out.UpdatedBy = domain.SecretID(id.String()), by, by

	cols, err := secretColumns(out)
	if err != nil {
		return domain.Secret{}, fmt.Errorf("create %s %s: %w", t.kind.noun, n.Name, err)
	}

	err = t.s.inTx(ctx, func(q *db.Queries) error {
		s, err := t.seal(org, out.ID, n.Value)
		if err != nil {
			return err
		}

		out.CreatedAt, err = q.InsertSecret(ctx, db.InsertSecretParams{
			ID: id, Name: out.Name, Source: string(out.Source), Description: out.Description, AllFactories: out.Scope.All,
			FactoryIds: cols.factories, KeyID: s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext,
			ExpiresAt: out.ExpiresAt, RemindDays: cols.remindDays, CreatedBy: by.Ref(),
		})
		if violates(err, secretsNameKey) {
			return fmt.Errorf("%s %q: %w", t.kind.noun, n.Name, ports.ErrNameTaken)
		}

		if err != nil {
			return err
		}

		entry := map[string]any{auditName: n.Name}
		maps.Copy(entry, data)

		return t.audit(ctx, q, by, t.kind.created, id, entry)
	})
	if err != nil {
		return domain.Secret{}, fmt.Errorf("create %s %s: %w", t.kind.noun, n.Name, ruleError(err, ""))
	}

	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.CreatedAt

	return out, nil
}

// patch applies p to the secret name, the result checked with check; metadata says whether the
// caller's patch changes metadata, which is audited as updated.
func (t secretTable) patch(ctx context.Context, name string, p domain.SecretPatch, by domain.Actor, metadata bool,
	check func(domain.Secret) error,
) (domain.Secret, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Secret{}, domain.ErrNoOrg
	}

	if err := p.CheckValue(); err != nil {
		return domain.Secret{}, fmt.Errorf("patch %s %s: %w", t.kind.noun, name, err)
	}

	if !metadata && p.Replace == nil {
		return t.get(ctx, name)
	}

	var out domain.Secret

	err := t.s.inTx(ctx, func(q *db.Queries) error {
		var err error

		out, err = t.patchIn(ctx, q, org, name, p, by, metadata, check)

		return err
	})
	if err != nil {
		return domain.Secret{}, fmt.Errorf("patch %s %s: %w", t.kind.noun, name, ruleError(err, ""))
	}

	return out, nil
}

// patchIn applies p to the secret name of org in q's transaction and audits it.
func (t secretTable) patchIn(ctx context.Context, q *db.Queries, org domain.OrgID, name string, p domain.SecretPatch,
	by domain.Actor, metadata bool, check func(domain.Secret) error,
) (domain.Secret, error) {
	r, err := t.sealed(ctx, q, name)
	if err != nil {
		return domain.Secret{}, err
	}

	cur := secretFrom(sealedSecretRow(r))

	out := p.Apply(cur)
	if err := check(out); err != nil {
		return domain.Secret{}, err
	}

	cols, err := secretColumns(out)
	if err != nil {
		return domain.Secret{}, err
	}

	s := sealedValue{keyID: r.KeyID, nonce: r.Nonce, ciphertext: r.Ciphertext}

	if p.Replace != nil {
		if s, err = t.seal(org, out.ID, *p.Replace); err != nil {
			return domain.Secret{}, err
		}
	}

	at, err := q.UpdateSecret(ctx, db.UpdateSecretParams{
		Name: out.Name, Description: out.Description, AllFactories: out.Scope.All, FactoryIds: cols.factories,
		KeyID: s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext, ExpiresAt: out.ExpiresAt, RemindDays: cols.remindDays,
		UpdatedBy: by.Ref(), ID: r.ID,
	})
	if violates(err, secretsNameKey) {
		return domain.Secret{}, fmt.Errorf("%s %q: %w", t.kind.noun, out.Name, ports.ErrNameTaken)
	}

	if err != nil {
		return domain.Secret{}, err
	}

	out.UpdatedAt, out.UpdatedBy = at.UTC(), by

	return out, t.patchAudits(ctx, q, by, r.ID, name, out.Name, metadata, p.Replace != nil)
}

// patchAudits records a change of the secret from, now named name: updated for its metadata,
// value_replaced for its value.
func (t secretTable) patchAudits(ctx context.Context, q *db.Queries, by domain.Actor, id uuid.UUID, from, name string,
	metadata, replaced bool,
) error {
	if metadata {
		data := map[string]any{auditName: name}
		if name != from {
			data[auditRenamedFrom] = from
		}

		if err := t.audit(ctx, q, by, t.kind.updated, id, data); err != nil {
			return err
		}
	}

	if !replaced {
		return nil
	}

	return t.audit(ctx, q, by, t.kind.replaced, id, map[string]any{auditName: name})
}

func (t secretTable) delete(ctx context.Context, name string, by domain.Actor) error {
	err := t.s.inTx(ctx, func(q *db.Queries) error {
		_, err := q.LockSecret(ctx, name)
		if notFound(err) {
			return fmt.Errorf("%s %q: %w", t.kind.noun, name, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		users, err := q.CredentialUsers(ctx, name)
		if err != nil {
			return err
		}

		if len(users) > 0 {
			return fmt.Errorf("%s %q is used by connections %s: %w", t.kind.noun, name, strings.Join(users, ", "), ports.ErrConflict)
		}

		sources, err := q.SecretKnowledgeSources(ctx, name)
		if err != nil {
			return err
		}

		if len(sources) > 0 {
			return fmt.Errorf("%s %q is used by knowledge sources %s: %w", t.kind.noun, name, strings.Join(sources, ", "), ports.ErrConflict)
		}

		notifies, err := q.SecretNotifiesSlack(ctx, name)
		if err != nil {
			return err
		}

		if notifies {
			return fmt.Errorf("%s %q is used by the Slack notification setting: %w", t.kind.noun, name, ports.ErrConflict)
		}

		id, err := q.DeleteSecret(ctx, name)
		if err != nil {
			return err
		}

		return t.audit(ctx, q, by, t.kind.deleted, id, map[string]any{auditName: name})
	})
	if err != nil {
		return fmt.Errorf("delete %s %s: %w", t.kind.noun, name, err)
	}

	return nil
}

func (t secretTable) resolve(ctx context.Context, names []string, use domain.SecretUse) ([]domain.ResolvedSecret, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	names = uniqueNames(names)
	out := make([]domain.ResolvedSecret, 0, len(names))

	err := t.s.inTx(ctx, func(q *db.Queries) error {
		rows, err := q.SealedSecrets(ctx, names)
		if err != nil {
			return err
		}

		byName := make(map[string]db.SealedSecretsRow, len(rows))
		for _, r := range rows {
			byName[r.Name] = r
		}

		ids := make([]uuid.UUID, 0, len(names))

		for _, name := range names {
			rs, err := t.opened(org, byName, name, use.Factory)
			if err != nil {
				return err
			}

			out = append(out, rs)
			ids = append(ids, byName[name].ID)
		}

		for i, name := range names {
			if err := t.audit(ctx, q, domain.EngineActor, t.kind.used, ids[i], use.AuditData(name)); err != nil {
				return err
			}
		}

		if len(ids) == 0 {
			return nil
		}

		used, err := q.MarkSecretsUsed(ctx, ids)
		if err != nil {
			return err
		}

		for i := range out {
			out[i].Secret.LastUsedAt = utc(used[0])
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("resolve %ss: %w", t.kind.noun, err)
	}

	return out, nil
}

// opened opens the secret name of byName for a run of factory: ErrNotFound when there is none,
// the kind's notAllowed when its scope leaves factory out.
func (t secretTable) opened(org domain.OrgID, byName map[string]db.SealedSecretsRow, name string,
	factory domain.FactoryID,
) (domain.ResolvedSecret, error) {
	r, ok := byName[name]
	if !ok {
		return domain.ResolvedSecret{}, fmt.Errorf("%s %q: %w", t.kind.noun, name, ports.ErrNotFound)
	}

	sec := secretFrom(sealedSecretRow(r))
	if !sec.Scope.Allows(factory) {
		return domain.ResolvedSecret{}, fmt.Errorf("%s %q, factory %s: %w", t.kind.noun, name, factory, t.kind.notAllowed)
	}

	plain, err := t.key.Open(r.KeyID, r.Nonce, r.Ciphertext, domain.SecretAAD(org, sec.ID, r.KeyID))
	if err != nil {
		return domain.ResolvedSecret{}, fmt.Errorf("%s %s: %w", t.kind.noun, name, err)
	}

	return domain.ResolvedSecret{Secret: sec, Value: string(plain)}, nil
}

// sealed is the secret name, locked, with its sealed value.
func (t secretTable) sealed(ctx context.Context, q *db.Queries, name string) (db.SealedSecretsRow, error) {
	rows, err := q.SealedSecrets(ctx, []string{name})
	if err != nil {
		return db.SealedSecretsRow{}, err
	}

	if len(rows) == 0 {
		return db.SealedSecretsRow{}, fmt.Errorf("%s %q: %w", t.kind.noun, name, ports.ErrNotFound)
	}

	return rows[0], nil
}

// seal seals value for secret id of org under the active key.
func (t secretTable) seal(org domain.OrgID, id domain.SecretID, value string) (sealedValue, error) {
	active := t.key.Active()

	keyID, nonce, ct, err := t.key.Seal([]byte(value), domain.SecretAAD(org, id, active))
	if err != nil {
		return sealedValue{}, err
	}

	if keyID != active {
		return sealedValue{}, fmt.Errorf("master key sealed under %q, not the active %q", keyID, active)
	}

	return sealedValue{keyID: keyID, nonce: nonce, ciphertext: ct}, nil
}

func (t secretTable) audit(ctx context.Context, q *db.Queries, by domain.Actor, action string, id uuid.UUID, data map[string]any) error {
	return appendAudit(ctx, q, domain.AuditEntry{Actor: by, Action: action, TargetKind: t.kind.target, TargetID: id.String(), Data: data})
}

// sealedValue is a secret's sealed value.
type sealedValue struct {
	keyID             string
	nonce, ciphertext []byte
}

// columns are a secret's scope and reminder as the secrets table holds them.
type columns struct {
	factories  []uuid.UUID
	remindDays int32
}

// secretColumns are s's columns; a factory id that is not a UUID, or a reminder too long for its
// column, is domain.ErrInvalid.
func secretColumns(s domain.Secret) (columns, error) {
	factories := make([]uuid.UUID, 0, len(s.Scope.Factories))

	for _, f := range s.Scope.Factories {
		id, err := uuid.Parse(string(f))
		if err != nil {
			return columns{}, fmt.Errorf("a scope's factory id %q is not a UUID: %w", f, domain.ErrInvalid)
		}

		factories = append(factories, id)
	}

	if s.RemindDays < 0 || s.RemindDays > math.MaxInt32 {
		return columns{}, fmt.Errorf("a reminder of %d days is too long: %w", s.RemindDays, domain.ErrInvalid)
	}

	return columns{factories: factories, remindDays: int32(s.RemindDays)}, nil
}

func secretFrom(r db.SecretRow) domain.Secret {
	var factories []domain.FactoryID
	for _, f := range r.FactoryIds {
		factories = append(factories, domain.FactoryID(f.String()))
	}

	return domain.Secret{
		ID: domain.SecretID(r.ID.String()), Name: r.Name, Source: domain.SecretSource(r.Source), Description: r.Description,
		Scope: domain.SecretScope{All: r.AllFactories, Factories: factories}, ExpiresAt: utc(r.ExpiresAt), RemindDays: int(r.RemindDays),
		LastUsedAt: utc(r.LastUsedAt), CreatedBy: namedActor(r.CreatedBy, r.CreatedByName), CreatedAt: r.CreatedAt.UTC(),
		UpdatedBy: namedActor(r.UpdatedBy, r.UpdatedByName), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

// sealedSecretRow is r's metadata columns.
func sealedSecretRow(r db.SealedSecretsRow) db.SecretRow {
	return db.SecretRow{
		ID: r.ID, Name: r.Name, Source: r.Source, Description: r.Description, AllFactories: r.AllFactories, FactoryIds: r.FactoryIds,
		ExpiresAt: r.ExpiresAt, RemindDays: r.RemindDays, LastUsedAt: r.LastUsedAt, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
		UpdatedBy: r.UpdatedBy, UpdatedAt: r.UpdatedAt, CreatedByName: r.CreatedByName, UpdatedByName: r.UpdatedByName,
	}
}

// resealSecrets reseals a batch of secrets of every organization sealed under another key than
// key's active one, and returns how many.
func resealSecrets(ctx context.Context, q *db.Queries, key ports.MasterKey) (int, error) {
	rows, err := q.StaleSealedSecrets(ctx, db.StaleSealedSecretsParams{Active: key.Active(), Batch: rekeyBatch})
	if err != nil {
		return 0, err
	}

	return resealRows(key, "secret", rows, secretAAD, func(id uuid.UUID, s sealedValue) error {
		return q.ResealSecret(ctx, db.ResealSecretParams{KeyID: s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext, ID: id})
	})
}

func secretAAD(org domain.OrgID, id uuid.UUID, keyID string) []byte {
	return domain.SecretAAD(org, domain.SecretID(id.String()), keyID)
}

// resealRows opens each of rows (rows of kind; another table's stale rows convert to this type)
// under its key and seals it again under key's active one, each bound to aad, and saves it; it
// returns how many. A row that does not open stops it with an error naming the row's id and
// never its value.
func resealRows(key ports.MasterKey, kind string, rows []db.StaleSealedSecretsRow,
	aad func(org domain.OrgID, id uuid.UUID, keyID string) []byte, save func(id uuid.UUID, s sealedValue) error,
) (int, error) {
	active := key.Active()

	for _, r := range rows {
		org := domain.OrgID(r.OrgID.String())

		plain, err := key.Open(r.KeyID, r.Nonce, r.Ciphertext, aad(org, r.ID, r.KeyID))
		if err != nil {
			return 0, fmt.Errorf("%s %s: %w", kind, r.ID, err)
		}

		keyID, nonce, ct, err := key.Seal(plain, aad(org, r.ID, active))
		if err != nil {
			return 0, fmt.Errorf("%s %s: %w", kind, r.ID, err)
		}

		if err := save(r.ID, sealedValue{keyID: keyID, nonce: nonce, ciphertext: ct}); err != nil {
			return 0, err
		}
	}

	return len(rows), nil
}
