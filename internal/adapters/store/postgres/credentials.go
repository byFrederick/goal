package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Credentials = (*Credentials)(nil)

// Credentials implements ports.Credentials over 00065's credentials, as goal_app under row-level
// security: every statement sees the organization in ctx, and every change and use appends its
// audit entry in its transaction. Each value is sealed with key under domain.CredentialAAD
// (organization, credential id and key id) and opened only by Resolve; a rename keeps the sealed
// value, which is bound to the id. Without a master key (the zero masterkey.Keys) metadata works
// and Create, a Replace and Resolve fail with ports.ErrNoMasterKey. Connections refer to a
// credential as {{name}} in 00066's secret_refs: UsedBy and ImportPreview count them, and Delete
// locks the credential and refuses while one does, as a connection's write locks the credentials it
// refers to (ShareReferencedCredentials), so neither can slip past the other.
type Credentials struct {
	s   *Store
	key ports.MasterKey
}

// NewCredentials returns the credentials on s's pool, sealed with key.
func NewCredentials(s *Store, key ports.MasterKey) *Credentials { return &Credentials{s: s, key: key} }

// credentialNameKey keeps a credential's name unique in its organization.
const credentialNameKey = "credentials_org_id_name_key"

// The audit data keys of credentials, beside auditName and auditRenamedFrom.
const auditType = "type"

// List implements ports.Credentials.
func (c *Credentials) List(ctx context.Context) ([]domain.Credential, error) {
	rows, err := fetch(ctx, c.s.read, func(q *db.Queries) ([]db.CredentialsRow, error) { return q.Credentials(ctx) })
	if err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}

	return convertAll(rows, func(r db.CredentialsRow) (domain.Credential, error) { return credentialFrom(db.CredentialRow(r)) })
}

// Get implements ports.Credentials.
func (c *Credentials) Get(ctx context.Context, name string) (domain.Credential, error) {
	r, err := fetch(ctx, c.s.read, func(q *db.Queries) (db.CredentialRow, error) { return q.Credential(ctx, name) })

	switch {
	case notFound(err):
		return domain.Credential{}, fmt.Errorf("credential %q: %w", name, ports.ErrNotFound)
	case err != nil:
		return domain.Credential{}, fmt.Errorf("credential %q: %w", name, err)
	default:
		return credentialFrom(r)
	}
}

// Create implements ports.Credentials.
func (c *Credentials) Create(ctx context.Context, n domain.NewCredential, by domain.Actor) (domain.Credential, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Credential{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.Credential{}, fmt.Errorf("create credential: %w", err)
	}

	id, err := newID()
	if err != nil {
		return domain.Credential{}, err
	}

	out := n.Credential()
	out.ID, out.CreatedBy, out.UpdatedBy = domain.CredentialID(id.String()), by, by

	cols, err := credentialColumns(out)
	if err != nil {
		return domain.Credential{}, fmt.Errorf("create credential %s: %w", n.Name, err)
	}

	err = c.s.inTx(ctx, func(q *db.Queries) error {
		s, err := c.seal(org, out.ID, n.Value)
		if err != nil {
			return err
		}

		out.CreatedAt, err = q.InsertCredential(ctx, db.InsertCredentialParams{
			ID: id, Name: n.Name, Type: string(n.Type), Description: n.Description, AllFactories: n.Scope.All,
			FactoryIds: cols.factories, Agents: cols.agents, KeyID: s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext,
			ExpiresAt: n.ExpiresAt, RemindDays: cols.remindDays, CreatedBy: by.Ref(),
		})
		if violates(err, credentialNameKey) {
			return fmt.Errorf("credential %q: %w", n.Name, ports.ErrNameTaken)
		}

		if err != nil {
			return err
		}

		return credentialAudit(ctx, q, by, domain.AuditCredentialCreated, id, map[string]any{auditName: n.Name, auditType: string(n.Type)})
	})
	if err != nil {
		return domain.Credential{}, fmt.Errorf("create credential %s: %w", n.Name, ruleError(err, ""))
	}

	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.CreatedAt

	return out, nil
}

// Patch implements ports.Credentials.
func (c *Credentials) Patch(ctx context.Context, name string, p domain.CredentialPatch, by domain.Actor) (domain.Credential, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Credential{}, domain.ErrNoOrg
	}

	if err := p.CheckValue(); err != nil {
		return domain.Credential{}, fmt.Errorf("patch credential %s: %w", name, err)
	}

	if p.Empty() {
		return c.Get(ctx, name)
	}

	var out domain.Credential

	err := c.s.inTx(ctx, func(q *db.Queries) error {
		var err error

		out, err = c.patch(ctx, q, org, name, p, by)

		return err
	})
	if err != nil {
		return domain.Credential{}, fmt.Errorf("patch credential %s: %w", name, ruleError(err, ""))
	}

	return out, nil
}

// patch applies p to the credential name of org in q's transaction and audits it.
func (c *Credentials) patch(ctx context.Context, q *db.Queries, org domain.OrgID, name string, p domain.CredentialPatch,
	by domain.Actor,
) (domain.Credential, error) {
	r, err := sealedCredential(ctx, q, name)
	if err != nil {
		return domain.Credential{}, err
	}

	cur, err := credentialFrom(sealedCredentialRow(r))
	if err != nil {
		return domain.Credential{}, err
	}

	out := p.Apply(cur)
	if err := domain.CheckCredential(out); err != nil {
		return domain.Credential{}, err
	}

	cols, err := credentialColumns(out)
	if err != nil {
		return domain.Credential{}, err
	}

	s := sealedValue{keyID: r.KeyID, nonce: r.Nonce, ciphertext: r.Ciphertext}

	if p.Replace != nil {
		if s, err = c.seal(org, out.ID, *p.Replace); err != nil {
			return domain.Credential{}, err
		}
	}

	at, err := q.UpdateCredential(ctx, db.UpdateCredentialParams{
		Name: out.Name, Type: string(out.Type), Description: out.Description, AllFactories: out.Scope.All,
		FactoryIds: cols.factories, Agents: cols.agents, KeyID: s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext,
		ExpiresAt: out.ExpiresAt, RemindDays: cols.remindDays, UpdatedBy: by.Ref(), ID: r.ID,
	})
	if violates(err, credentialNameKey) {
		return domain.Credential{}, fmt.Errorf("credential %q: %w", out.Name, ports.ErrNameTaken)
	}

	if err != nil {
		return domain.Credential{}, err
	}

	out.UpdatedAt, out.UpdatedBy = at.UTC(), by

	return out, patchAudits(ctx, q, by, r.ID, name, out.Name, p)
}

// patchAudits records p's change of the credential from, now named name: credential.updated for
// its metadata, credential.value_replaced for its value.
func patchAudits(ctx context.Context, q *db.Queries, by domain.Actor, id uuid.UUID, from, name string, p domain.CredentialPatch) error {
	if p.ChangesMetadata() {
		data := map[string]any{auditName: name}
		if name != from {
			data[auditRenamedFrom] = from
		}

		if err := credentialAudit(ctx, q, by, domain.AuditCredentialUpdated, id, data); err != nil {
			return err
		}
	}

	if p.Replace == nil {
		return nil
	}

	return credentialAudit(ctx, q, by, domain.AuditCredentialValueReplaced, id, map[string]any{auditName: name})
}

// Delete implements ports.Credentials.
func (c *Credentials) Delete(ctx context.Context, name string, by domain.Actor) error {
	err := c.s.inTx(ctx, func(q *db.Queries) error {
		_, err := q.LockCredential(ctx, name)
		if notFound(err) {
			return fmt.Errorf("credential %q: %w", name, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}

		users, err := q.CredentialUsers(ctx, name)
		if err != nil {
			return err
		}

		if len(users) > 0 {
			return fmt.Errorf("credential %q is used by connections %s: %w", name, strings.Join(users, ", "), ports.ErrConflict)
		}

		id, err := q.DeleteCredential(ctx, name)
		if err != nil {
			return err
		}

		return credentialAudit(ctx, q, by, domain.AuditCredentialDeleted, id, map[string]any{auditName: name})
	})
	if err != nil {
		return fmt.Errorf("delete credential %s: %w", name, err)
	}

	return nil
}

// UsedBy implements ports.Credentials.
func (c *Credentials) UsedBy(ctx context.Context, name string) ([]string, error) {
	users, err := fetch(ctx, c.s.read, func(q *db.Queries) ([]string, error) { return q.CredentialUsers(ctx, name) })
	if err != nil {
		return nil, fmt.Errorf("credential %s: its connections: %w", name, err)
	}

	return append([]string{}, users...), nil
}

// Resolve implements ports.Credentials.
func (c *Credentials) Resolve(ctx context.Context, names []string, use domain.CredentialUse) ([]domain.ResolvedCredential, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	names = uniqueNames(names)
	out := make([]domain.ResolvedCredential, 0, len(names))

	err := c.s.inTx(ctx, func(q *db.Queries) error {
		rows, err := q.SealedCredentials(ctx, names)
		if err != nil {
			return err
		}

		byName := make(map[string]db.SealedCredentialsRow, len(rows))
		for _, r := range rows {
			byName[r.Name] = r
		}

		ids := make([]uuid.UUID, 0, len(names))

		for _, name := range names {
			rc, err := c.resolved(org, byName, name, use.Factory)
			if err != nil {
				return err
			}

			out = append(out, rc)
			ids = append(ids, byName[name].ID)
		}

		for i, name := range names {
			if err := credentialAudit(ctx, q, domain.EngineActor, domain.AuditCredentialUsed, ids[i], use.AuditData(name)); err != nil {
				return err
			}
		}

		if len(ids) == 0 {
			return nil
		}

		used, err := q.MarkCredentialsUsed(ctx, ids)
		if err != nil {
			return err
		}

		for i := range out {
			out[i].Credential.LastUsedAt = utc(used[0])
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("resolve credentials: %w", err)
	}

	return out, nil
}

// resolved opens the credential name of byName for a run of factory: ErrNotFound when there is
// none, ErrCredentialNotAllowed when its scope leaves factory out.
func (c *Credentials) resolved(org domain.OrgID, byName map[string]db.SealedCredentialsRow, name string,
	factory domain.FactoryID,
) (domain.ResolvedCredential, error) {
	r, ok := byName[name]
	if !ok {
		return domain.ResolvedCredential{}, fmt.Errorf("credential %q: %w", name, ports.ErrNotFound)
	}

	cred, err := credentialFrom(sealedCredentialRow(r))
	if err != nil {
		return domain.ResolvedCredential{}, err
	}

	if !cred.Scope.Allows(factory) {
		return domain.ResolvedCredential{}, fmt.Errorf("credential %q, factory %s: %w", name, factory, ports.ErrCredentialNotAllowed)
	}

	plain, err := c.key.Open(r.KeyID, r.Nonce, r.Ciphertext, domain.CredentialAAD(org, cred.ID, r.KeyID))
	if err != nil {
		return domain.ResolvedCredential{}, fmt.Errorf("credential %s: %w", name, err)
	}

	return domain.ResolvedCredential{Credential: cred, Value: string(plain)}, nil
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

	var (
		existing []string
		used     []db.CredentialUserCountsRow
	)

	err := c.s.read(ctx, func(q *db.Queries) error {
		var err error

		if existing, err = q.CredentialNames(ctx, names); err != nil {
			return err
		}

		used, err = q.CredentialUserCounts(ctx, names)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("import preview: %w", err)
	}

	counts := make(map[string]int, len(used))
	for _, u := range used {
		counts[u.Name] = int(u.Connections)
	}

	out := make([]domain.CredentialImportPreview, 0, len(names))
	for _, name := range names {
		out = append(out, domain.CredentialImportPreview{Name: name, Exists: slices.Contains(existing, name), Connections: counts[name]})
	}

	return out, nil
}

// sealedCredential is the credential name, locked, with its sealed value.
func sealedCredential(ctx context.Context, q *db.Queries, name string) (db.SealedCredentialsRow, error) {
	rows, err := q.SealedCredentials(ctx, []string{name})
	if err != nil {
		return db.SealedCredentialsRow{}, err
	}

	if len(rows) == 0 {
		return db.SealedCredentialsRow{}, fmt.Errorf("credential %q: %w", name, ports.ErrNotFound)
	}

	return rows[0], nil
}

// sealedValue is a credential's sealed value.
type sealedValue struct {
	keyID             string
	nonce, ciphertext []byte
}

// seal seals value for credential id of org under the active key.
func (c *Credentials) seal(org domain.OrgID, id domain.CredentialID, value string) (sealedValue, error) {
	active := c.key.Active()

	keyID, nonce, ct, err := c.key.Seal([]byte(value), domain.CredentialAAD(org, id, active))
	if err != nil {
		return sealedValue{}, err
	}

	if keyID != active {
		return sealedValue{}, fmt.Errorf("master key sealed under %q, not the active %q", keyID, active)
	}

	return sealedValue{keyID: keyID, nonce: nonce, ciphertext: ct}, nil
}

// columns are a credential's scope, agents and reminder as the credentials table holds them.
type columns struct {
	factories  []uuid.UUID
	agents     []byte
	remindDays int32
}

// credentialColumns are c's columns; a factory id that is not a UUID, or a reminder too long
// for its column, is domain.ErrInvalid.
func credentialColumns(c domain.Credential) (columns, error) {
	factories := make([]uuid.UUID, 0, len(c.Scope.Factories))

	for _, f := range c.Scope.Factories {
		id, err := uuid.Parse(string(f))
		if err != nil {
			return columns{}, fmt.Errorf("a scope's factory id %q is not a UUID: %w", f, domain.ErrInvalid)
		}

		factories = append(factories, id)
	}

	agents := make(map[string][]string, len(c.Agents))

	for f, names := range c.Agents {
		if _, err := uuid.Parse(string(f)); err != nil {
			return columns{}, fmt.Errorf("agents' factory id %q is not a UUID: %w", f, domain.ErrInvalid)
		}

		agents[string(f)] = append([]string{}, names...)
	}

	raw, err := json.Marshal(agents)
	if err != nil {
		return columns{}, fmt.Errorf("encode agents: %w", err)
	}

	if c.RemindDays < 0 || c.RemindDays > math.MaxInt32 {
		return columns{}, fmt.Errorf("a rotation reminder of %d days is too long: %w", c.RemindDays, domain.ErrInvalid)
	}

	return columns{factories: factories, agents: raw, remindDays: int32(c.RemindDays)}, nil
}

func credentialAudit(ctx context.Context, q *db.Queries, by domain.Actor, action string, id uuid.UUID, data map[string]any) error {
	return appendAudit(ctx, q, domain.AuditEntry{
		Actor: by, Action: action, TargetKind: domain.AuditTargetCredential, TargetID: id.String(), Data: data,
	})
}

func credentialFrom(r db.CredentialRow) (domain.Credential, error) {
	var agents map[domain.FactoryID][]string
	if err := json.Unmarshal(r.Agents, &agents); err != nil {
		return domain.Credential{}, fmt.Errorf("credential %s: agents: %w", r.Name, err)
	}

	if len(agents) == 0 {
		agents = nil
	}

	var factories []domain.FactoryID
	for _, f := range r.FactoryIds {
		factories = append(factories, domain.FactoryID(f.String()))
	}

	return domain.Credential{
		ID: domain.CredentialID(r.ID.String()), Name: r.Name, Type: domain.CredentialType(r.Type), Description: r.Description,
		Scope: domain.CredentialScope{All: r.AllFactories, Factories: factories}, Agents: agents,
		ExpiresAt: utc(r.ExpiresAt), RemindDays: int(r.RemindDays), LastUsedAt: utc(r.LastUsedAt),
		CreatedBy: namedActor(r.CreatedBy, r.CreatedByName), CreatedAt: r.CreatedAt.UTC(), UpdatedBy: namedActor(r.UpdatedBy, r.UpdatedByName),
		UpdatedAt: r.UpdatedAt.UTC(),
	}, nil
}

// sealedCredentialRow is r's metadata columns.
func sealedCredentialRow(r db.SealedCredentialsRow) db.CredentialRow {
	return db.CredentialRow{
		ID: r.ID, Name: r.Name, Type: r.Type, Description: r.Description, AllFactories: r.AllFactories, FactoryIds: r.FactoryIds,
		Agents: r.Agents, ExpiresAt: r.ExpiresAt, RemindDays: r.RemindDays, LastUsedAt: r.LastUsedAt, CreatedBy: r.CreatedBy,
		CreatedAt: r.CreatedAt, UpdatedBy: r.UpdatedBy, UpdatedAt: r.UpdatedAt, CreatedByName: r.CreatedByName, UpdatedByName: r.UpdatedByName,
	}
}

// resealCredentials reseals a batch of credentials of every organization sealed under another
// key than key's active one, and returns how many.
func resealCredentials(ctx context.Context, q *db.Queries, key ports.MasterKey) (int, error) {
	rows, err := q.StaleSealedCredentials(ctx, db.StaleSealedCredentialsParams{Active: key.Active(), Batch: rekeyBatch})
	if err != nil {
		return 0, err
	}

	return resealRows(key, "credential", rows, credentialAAD, func(id uuid.UUID, s sealedValue) error {
		return q.ResealCredential(ctx, db.ResealCredentialParams{KeyID: s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext, ID: id})
	})
}

func credentialAAD(org domain.OrgID, id uuid.UUID, keyID string) []byte {
	return domain.CredentialAAD(org, domain.CredentialID(id.String()), keyID)
}

// resealRows opens each of rows (rows of kind; another table's stale rows convert to this type)
// under its key and seals it again under key's active one, each bound to aad, and saves it; it
// returns how many. A row that does not open stops it with an error naming the row's id and
// never its value.
func resealRows(key ports.MasterKey, kind string, rows []db.StaleSealedCredentialsRow,
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
