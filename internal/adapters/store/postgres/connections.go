package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Connections = (*Connections)(nil)

// Connections implements ports.Connections over 00051's connection_kinds and connections, as
// goal_app under row-level security: every statement sees the organization in ctx and the
// platform's kinds, and every change appends its audit entry and notifies its stream event
// (connection, read back by Notices) in its transaction. Secret values are sealed with key under
// domain.ConnectionAAD and opened only by Resolve, Open and an update that replaces some of them. Without a master key (the zero masterkey.Keys) everything without
// secrets works and the rest fails with ports.ErrNoMasterKey.
type Connections struct {
	s   *Store
	key ports.MasterKey
}

// NewConnections returns the connections on s's pool, sealed with key.
func NewConnections(s *Store, key ports.MasterKey) *Connections { return &Connections{s: s, key: key} }

// connectionNameKey keeps a connection's name unique in its organization.
const connectionNameKey = "connections_org_id_name_key"

// maxTestError bounds a stored test error (00051's check).
const maxTestError = 500

// The audit data keys of connections.
const (
	auditKind        = "kind"
	auditFields      = "fields"
	auditSecrets     = "secrets"
	auditRenamedFrom = "from"
	auditTools       = "tools"
)

// The changes of a connection event (spec 4 §8.3), and the audit actions each is read back from.
const (
	changeCreated = "created"
	changeUpdated = "updated"
	changeDeleted = "deleted"
	changeTested  = "tested"
)

var connectionChangeActions = map[string][]string{
	changeCreated: {domain.AuditConnectionCreated},
	changeUpdated: {domain.AuditConnectionUpdated, domain.AuditConnectionSecretReplaced},
	changeDeleted: {domain.AuditConnectionDeleted},
	changeTested:  {domain.AuditConnectionTested},
}

// notifyConnection sends the stream event of a change of connection id, now named name, when q's
// transaction commits.
func notifyConnection(ctx context.Context, q *db.Queries, id uuid.UUID, name, change string) error {
	return q.NotifyConnection(ctx, db.NotifyConnectionParams{ID: id, Name: name, Change: change})
}

// storedKind is a kind as connection_kinds.spec holds it.
type storedKind struct {
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	Spec        domain.KindSpec `json:"spec"`
}

// encodeKind is k's spec column and its hash.
func encodeKind(k domain.ConnectionKind) ([]byte, string, error) {
	raw, err := json.Marshal(storedKind{Title: k.Title, Description: k.Description, Spec: k.Spec})
	if err != nil {
		return nil, "", fmt.Errorf("encode kind %s: %w", k.Name, err)
	}

	sum := sha256.Sum256(raw)

	return raw, hex.EncodeToString(sum[:]), nil
}

func decodeKind(name string, org *uuid.UUID, raw []byte) (domain.ConnectionKind, error) {
	var sk storedKind
	if err := json.Unmarshal(raw, &sk); err != nil {
		return domain.ConnectionKind{}, fmt.Errorf("decode kind %s: %w", name, err)
	}

	return domain.ConnectionKind{Name: name, Title: sk.Title, Description: sk.Description, Platform: org == nil, Spec: sk.Spec}, nil
}

// Kinds implements ports.Connections.
func (c *Connections) Kinds(ctx context.Context) ([]domain.ConnectionKind, error) {
	rows, err := fetch(ctx, c.s.read, func(q *db.Queries) ([]db.ConnectionKindsRow, error) { return q.ConnectionKinds(ctx) })
	if err != nil {
		return nil, fmt.Errorf("connection kinds: %w", err)
	}

	out := make([]domain.ConnectionKind, 0, len(rows))

	for _, r := range rows {
		// Each name's own kind comes first and hides the platform's.
		if len(out) > 0 && out[len(out)-1].Name == r.Name {
			continue
		}

		k, err := decodeKind(r.Name, r.OrgID, r.Spec)
		if err != nil {
			return nil, err
		}

		out = append(out, k)
	}

	return out, nil
}

// Kind implements ports.Connections.
func (c *Connections) Kind(ctx context.Context, name string) (domain.ConnectionKind, error) {
	r, err := fetch(ctx, c.s.read, func(q *db.Queries) (db.ConnectionKindRow, error) { return q.ConnectionKind(ctx, name) })
	if notFound(err) {
		return domain.ConnectionKind{}, fmt.Errorf("connection kind %q: %w", name, ports.ErrNotFound)
	}

	if err != nil {
		return domain.ConnectionKind{}, fmt.Errorf("connection kind %q: %w", name, err)
	}

	return decodeKind(r.Name, r.OrgID, r.Spec)
}

// PutKind implements ports.Connections.
func (c *Connections) PutKind(ctx context.Context, k domain.ConnectionKind, by domain.Actor) error {
	if err := k.Check(); err != nil {
		return err
	}

	raw, sum, err := encodeKind(k)
	if err != nil {
		return err
	}

	err = c.s.inTx(ctx, func(q *db.Queries) error {
		existing, err := q.OrgConnectionKind(ctx, k.Name)

		switch {
		case notFound(err):
			return insertKind(ctx, q, k.Name, raw, sum, by)
		case err != nil:
			return err
		case existing.SpecSha256 == sum:
			return nil
		}

		if err := checkHeldPlain(ctx, q, k); err != nil {
			return err
		}

		if err := q.UpdateConnectionKind(ctx, db.UpdateConnectionKindParams{Spec: raw, SpecSha256: sum, ID: existing.ID}); err != nil {
			return err
		}

		return kindAudit(ctx, q, by, domain.AuditConnectionKindUpdated, existing.ID, k.Name)
	})
	if err != nil {
		return fmt.Errorf("put connection kind %s: %w", k.Name, ruleError(err, ""))
	}

	return nil
}

// checkHeldPlain refuses k as the organization kind's change while one of its connections holds
// a field k makes secret as a plain value (domain.ConnectionKind.CheckHeldPlain).
func checkHeldPlain(ctx context.Context, q *db.Queries, k domain.ConnectionKind) error {
	rows, err := q.Connections(ctx)
	if err != nil {
		return err
	}

	var ofKind []domain.Connection

	for _, r := range rows {
		if r.Kind != k.Name {
			continue
		}

		c, err := connectionFrom(db.ConnectionRow(r))
		if err != nil {
			return err
		}

		ofKind = append(ofKind, c)
	}

	if err := k.CheckHeldPlain(ofKind); err != nil {
		return fmt.Errorf("%w: %w", err, ports.ErrConflict)
	}

	return nil
}

// insertKind adds the organization kind name, unless the platform has a kind of that name.
func insertKind(ctx context.Context, q *db.Queries, name string, raw []byte, sum string, by domain.Actor) error {
	taken, err := q.PlatformKindNamed(ctx, name)
	if err != nil {
		return err
	}

	if taken {
		return fmt.Errorf("a platform kind is named %s: %w", name, ports.ErrNameTaken)
	}

	id, err := newID()
	if err != nil {
		return err
	}

	err = q.InsertConnectionKind(ctx, db.InsertConnectionKindParams{ID: id, Name: name, Spec: raw, SpecSha256: sum, CreatedBy: by.Ref()})
	if violates(err, "connection_kinds_org_id_name_key") {
		return ports.ErrNameTaken
	}

	if err != nil {
		return err
	}

	return kindAudit(ctx, q, by, domain.AuditConnectionKindCreated, id, name)
}

func kindAudit(ctx context.Context, q *db.Queries, by domain.Actor, action string, id uuid.UUID, name string) error {
	return appendAudit(ctx, q, domain.AuditEntry{
		Actor: by, Action: action, TargetKind: domain.AuditTargetConnectionKind, TargetID: id.String(),
		Data: map[string]any{auditName: name},
	})
}

// DeleteKind implements ports.Connections.
func (c *Connections) DeleteKind(ctx context.Context, name string, by domain.Actor) error {
	err := c.s.inTx(ctx, func(q *db.Queries) error {
		id, err := q.DeleteConnectionKind(ctx, name)

		switch {
		case notFound(err):
			return ports.ErrNotFound
		case isForeignKeyViolation(err):
			return fmt.Errorf("connections use it: %w", ports.ErrConflict)
		case err != nil:
			return err
		}

		return kindAudit(ctx, q, by, domain.AuditConnectionKindDeleted, id, name)
	})
	if err != nil {
		return fmt.Errorf("delete connection kind %s: %w", name, err)
	}

	return nil
}

// platformKindRow is one entry of goal_sync_platform_kinds' argument.
type platformKindRow struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Spec       json.RawMessage `json:"spec"`
	SpecSha256 string          `json:"spec_sha256"`
}

// SyncPlatformKinds implements ports.Connections through goal_sync_platform_kinds. An id is used
// only for a kind that is new.
func (c *Connections) SyncPlatformKinds(ctx context.Context, kinds []domain.ConnectionKind) error {
	rows := make([]platformKindRow, 0, len(kinds))

	for _, k := range kinds {
		if err := k.Check(); err != nil {
			return fmt.Errorf("sync platform kinds: %w", err)
		}

		raw, sum, err := encodeKind(k)
		if err != nil {
			return err
		}

		id, err := newID()
		if err != nil {
			return err
		}

		rows = append(rows, platformKindRow{ID: id.String(), Name: k.Name, Spec: raw, SpecSha256: sum})
	}

	arg, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("sync platform kinds: %w", err)
	}

	if err := c.s.systemTx(ctx, func(q *db.Queries) error { return q.SyncPlatformKinds(ctx, arg) }); err != nil {
		return fmt.Errorf("sync platform kinds: %w", err)
	}

	return nil
}

// List implements ports.Connections.
func (c *Connections) List(ctx context.Context) ([]domain.Connection, error) {
	rows, err := fetch(ctx, c.s.read, func(q *db.Queries) ([]db.ConnectionsRow, error) { return q.Connections(ctx) })
	if err != nil {
		return nil, fmt.Errorf("connections: %w", err)
	}

	return convertAll(rows, func(r db.ConnectionsRow) (domain.Connection, error) { return connectionFrom(db.ConnectionRow(r)) })
}

// Get implements ports.Connections.
func (c *Connections) Get(ctx context.Context, name string) (domain.Connection, error) {
	r, err := fetch(ctx, c.s.read, func(q *db.Queries) (db.ConnectionRow, error) { return q.Connection(ctx, name) })
	if notFound(err) {
		return domain.Connection{}, fmt.Errorf("connection %q: %w", name, ports.ErrNotFound)
	}

	if err != nil {
		return domain.Connection{}, fmt.Errorf("connection %q: %w", name, err)
	}

	return connectionFrom(r)
}

// Create implements ports.Connections.
func (c *Connections) Create(ctx context.Context, n domain.NewConnection, by domain.Actor) (domain.Connection, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Connection{}, domain.ErrNoOrg
	}

	if err := errors.Join(domain.CheckConnectionName(n.Name), domain.CheckConnectionDescription(n.Description)); err != nil {
		return domain.Connection{}, fmt.Errorf("create connection: %w", err)
	}

	id, err := newID()
	if err != nil {
		return domain.Connection{}, err
	}

	toSeal, refs, err := domain.ConnectionSecrets(n.Fields, n.Secrets)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("create connection: %w", err)
	}

	fields, secrets := setValues(n.Fields), setValues(toSeal)
	out := domain.Connection{
		ID: domain.ConnectionID(id.String()), Name: n.Name, Kind: n.Kind, Description: n.Description, Fields: fields,
		SecretFields: secretNames(secrets, refs), SecretRefs: refs, CreatedBy: by, UpdatedBy: by,
		ToolModes: map[string]domain.ConnectionToolMode{},
	}

	err = c.s.inTx(ctx, func(q *db.Queries) error {
		kindID, kind, err := kindNamed(ctx, q, n.Kind)
		if err != nil {
			return err
		}

		if err := kind.CheckValues(fields, out.SecretFields); err != nil {
			return err
		}

		if err := shareReferenced(ctx, q, refs); err != nil {
			return err
		}

		s, err := c.seal(org, out.ID, secrets)
		if err != nil {
			return err
		}

		raw, rawRefs, err := encodeValues(fields, refs)
		if err != nil {
			return err
		}

		out.CreatedAt, err = q.InsertConnection(ctx, db.InsertConnectionParams{
			ID: id, Name: n.Name, KindID: kindID, Description: n.Description, Fields: raw, SecretFields: out.SecretFields,
			SecretRefs: rawRefs, KeyID: s.keyID, Nonce: s.nonce, Ciphertext: s.ciphertext, CreatedBy: by.Ref(),
		})
		if violates(err, connectionNameKey) {
			return fmt.Errorf("connection %q: %w", n.Name, ports.ErrNameTaken)
		}

		if err != nil {
			return err
		}

		return created(ctx, q, by, id, n, out)
	})
	if err != nil {
		return domain.Connection{}, fmt.Errorf("create connection %s: %w", n.Name, ruleError(err, ""))
	}

	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.CreatedAt

	return out, nil
}

// kindNamed is the kind name as the organization sees it, with its id; ErrNotFound when there is
// none.
func kindNamed(ctx context.Context, q *db.Queries, name string) (uuid.UUID, domain.ConnectionKind, error) {
	kr, err := q.ConnectionKind(ctx, name)
	if notFound(err) {
		return uuid.UUID{}, domain.ConnectionKind{}, fmt.Errorf("connection kind %q: %w", name, ports.ErrNotFound)
	}

	if err != nil {
		return uuid.UUID{}, domain.ConnectionKind{}, err
	}

	kind, err := decodeKind(kr.Name, kr.OrgID, kr.Spec)

	return kr.ID, kind, err
}

// encodeValues are a connection's fields and secret references as their jsonb columns hold them.
func encodeValues(fields, refs map[string]string) (rawFields, rawRefs []byte, err error) {
	if rawFields, err = json.Marshal(fields); err != nil {
		return nil, nil, err
	}

	if rawRefs, err = json.Marshal(refs); err != nil {
		return nil, nil, err
	}

	return rawFields, rawRefs, nil
}

// created audits and notifies the creation of connection id, out, from n.
func created(ctx context.Context, q *db.Queries, by domain.Actor, id uuid.UUID, n domain.NewConnection, out domain.Connection) error {
	err := connectionAudit(ctx, q, by, domain.AuditConnectionCreated, id, map[string]any{
		auditName: n.Name, auditKind: n.Kind, auditFields: valueNames(out.Fields), auditSecrets: out.SecretFields,
	})
	if err != nil {
		return err
	}

	return notifyConnection(ctx, q, id, n.Name, changeCreated)
}

// Update implements ports.Connections.
func (c *Connections) Update(ctx context.Context, name string, p domain.ConnectionPatch, by domain.Actor) (domain.Connection, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Connection{}, domain.ErrNoOrg
	}

	if p.Empty() {
		return c.Get(ctx, name)
	}

	if err := checkPatch(p); err != nil {
		return domain.Connection{}, fmt.Errorf("update connection %s: %w", name, err)
	}

	toSeal, refs, err := domain.ConnectionSecrets(p.Fields, p.Secrets)
	if err != nil {
		return domain.Connection{}, fmt.Errorf("update connection %s: %w", name, err)
	}

	var out domain.Connection

	err = c.s.inTx(ctx, func(q *db.Queries) error {
		var err error

		out, err = c.update(ctx, q, org, name, p, secretPatch{toSeal: toSeal, refs: refs}, by)

		return err
	})
	if err != nil {
		return domain.Connection{}, fmt.Errorf("update connection %s: %w", name, ruleError(err, ""))
	}

	return out, nil
}

// secretPatch is a patch's secrets split by domain.ConnectionSecrets: the values to seal (an
// empty one clearing its field) and the references to credentials.
type secretPatch struct {
	toSeal, refs map[string]string
}

// update applies p, its secrets split in sp, to the connection name of org in q's transaction and
// audits it.
func (c *Connections) update(ctx context.Context, q *db.Queries, org domain.OrgID, name string, p domain.ConnectionPatch,
	sp secretPatch, by domain.Actor,
) (domain.Connection, error) {
	r, kind, err := c.sealedOne(ctx, q, name)
	if err != nil {
		return domain.Connection{}, err
	}

	out, err := connectionFrom(sealedConnection(r))
	if err != nil {
		return domain.Connection{}, err
	}

	if p.Name != nil {
		out.Name = *p.Name
	}

	if p.Description != nil {
		out.Description = *p.Description
	}

	if p.Fields != nil {
		out.Fields = setValues(p.Fields)
	}

	if p.ToolModes != nil {
		out.ToolModes = maps.Clone(p.ToolModes)
	}

	s := sealed{keyID: r.KeyID, nonce: r.Nonce, ciphertext: r.Ciphertext}

	if len(p.Secrets) > 0 {
		if s, err = c.replaceSecrets(ctx, q, org, r, &out, sp); err != nil {
			return domain.Connection{}, err
		}
	}

	if err := kind.CheckValues(out.Fields, out.SecretFields); err != nil {
		return domain.Connection{}, err
	}

	raw, rawRefs, err := encodeValues(out.Fields, out.SecretRefs)
	if err != nil {
		return domain.Connection{}, err
	}

	modes, err := json.Marshal(out.ToolModes)
	if err != nil {
		return domain.Connection{}, err
	}

	at, err := q.UpdateConnection(ctx, db.UpdateConnectionParams{
		Name: out.Name, Description: out.Description, Fields: raw, SecretFields: out.SecretFields, SecretRefs: rawRefs, KeyID: s.keyID, Nonce: s.nonce,
		Ciphertext: s.ciphertext, ToolModes: modes, UpdatedBy: by.Ref(), ID: r.ID,
	})
	if violates(err, connectionNameKey) {
		return domain.Connection{}, fmt.Errorf("connection %q: %w", out.Name, ports.ErrNameTaken)
	}

	if err != nil {
		return domain.Connection{}, err
	}

	out.UpdatedAt, out.UpdatedBy = at.UTC(), by

	if err := updateAudits(ctx, q, by, r.ID, name, out.Name, p); err != nil {
		return domain.Connection{}, err
	}

	return out, notifyConnection(ctx, q, r.ID, out.Name, changeUpdated)
}

// checkPatch refuses a patch's malformed name, description or tool modes.
func checkPatch(p domain.ConnectionPatch) error {
	var errs []error

	if p.Name != nil {
		errs = append(errs, domain.CheckConnectionName(*p.Name))
	}

	if p.Description != nil {
		errs = append(errs, domain.CheckConnectionDescription(*p.Description))
	}

	return errors.Join(append(errs, domain.CheckToolModes(p.ToolModes))...)
}

// replaceSecrets opens r's secrets, replaces those of sp (an empty value clears one, a reference
// replaces a sealed value and a sealed value a reference) and seals what is left to seal, none
// leaving no ciphertext. It sets out's SecretFields and SecretRefs, locks the credentials its new
// references name (shareReferenced) and returns the new sealed value.
func (c *Connections) replaceSecrets(ctx context.Context, q *db.Queries, org domain.OrgID, r db.SealedConnectionsRow,
	out *domain.Connection, sp secretPatch,
) (sealed, error) {
	if err := shareReferenced(ctx, q, sp.refs); err != nil {
		return sealed{}, err
	}

	secrets, err := c.open(org, out.ID, r)
	if err != nil {
		return sealed{}, err
	}

	refs := maps.Clone(out.SecretRefs)

	for name, v := range sp.toSeal {
		delete(refs, name)

		if v == "" {
			delete(secrets, name)
		} else {
			secrets[name] = v
		}
	}

	for name, cred := range sp.refs {
		delete(secrets, name)
		refs[name] = cred
	}

	out.SecretFields, out.SecretRefs = secretNames(secrets, refs), refs

	return c.seal(org, out.ID, secrets)
}

// shareReferenced locks the credentials refs refer to against their deletion until q's
// transaction commits (ShareReferencedCredentials).
func shareReferenced(ctx context.Context, q *db.Queries, refs map[string]string) error {
	if len(refs) == 0 {
		return nil
	}

	return q.ShareReferencedCredentials(ctx, slices.Sorted(maps.Values(refs)))
}

// secretNames are the names of a connection's sealed secrets and references, sorted; never nil.
func secretNames(sealed, refs map[string]string) []string {
	names := append(valueNames(sealed), slices.Collect(maps.Keys(refs))...)
	slices.Sort(names)

	return slices.Compact(names)
}

// updateAudits records an update of the connection from, now named name: connection.updated for
// its name, description, fields or tool modes (the tools with one), connection.secret_replaced for
// its secrets (names only).
func updateAudits(ctx context.Context, q *db.Queries, by domain.Actor, id uuid.UUID, from, name string, p domain.ConnectionPatch) error {
	if p.Name != nil || p.Description != nil || p.Fields != nil || p.ToolModes != nil {
		data := map[string]any{auditName: name}
		if p.Fields != nil {
			data[auditFields] = valueNames(setValues(p.Fields))
		}

		if p.ToolModes != nil {
			data[auditTools] = append([]string{}, slices.Sorted(maps.Keys(p.ToolModes))...)
		}

		if name != from {
			data[auditRenamedFrom] = from
		}

		if err := connectionAudit(ctx, q, by, domain.AuditConnectionUpdated, id, data); err != nil {
			return err
		}
	}

	if len(p.Secrets) == 0 {
		return nil
	}

	return connectionAudit(ctx, q, by, domain.AuditConnectionSecretReplaced, id,
		map[string]any{auditName: name, auditSecrets: valueNames(p.Secrets)})
}

// Delete implements ports.Connections. The connection's active standing rules are revoked in its
// transaction.
func (c *Connections) Delete(ctx context.Context, name string, by domain.Actor) error {
	err := c.s.inTx(ctx, func(q *db.Queries) error {
		r, err := q.DeleteConnection(ctx, name)

		switch {
		case notFound(err):
			return ports.ErrNotFound
		case isForeignKeyViolation(err):
			return fmt.Errorf("knowledge sources use it: %w", ports.ErrConflict)
		case err != nil:
			return err
		}

		if err := connectionAudit(ctx, q, by, domain.AuditConnectionDeleted, r.ID, map[string]any{auditName: name, auditKind: r.Kind}); err != nil {
			return err
		}

		if err := revokeConnectionRules(ctx, q, r.ID, name, by); err != nil {
			return err
		}

		return notifyConnection(ctx, q, r.ID, name, changeDeleted)
	})
	if err != nil {
		return fmt.Errorf("delete connection %s: %w", name, err)
	}

	return nil
}

// RecordTest implements ports.Connections. A zero r.At is the database's now; a long error is cut;
// the tools are stored sorted without repeats, NULL for none.
func (c *Connections) RecordTest(ctx context.Context, name string, r domain.ConnectionTest, by domain.Actor) error {
	msg := r.Error
	if utf8.RuneCountInString(msg) > maxTestError {
		msg = string([]rune(msg)[:maxTestError])
	}

	err := c.s.inTx(ctx, func(q *db.Queries) error {
		id, err := q.RecordConnectionTest(ctx, db.RecordConnectionTestParams{
			At: optionalTime(r.At), Ok: r.OK, Error: optional(msg), Tools: toolNames(r.Tools), Name: name,
		})
		if notFound(err) {
			return ports.ErrNotFound
		}

		if err != nil {
			return err
		}

		if err := connectionAudit(ctx, q, by, domain.AuditConnectionTested, id, map[string]any{auditName: name, "ok": r.OK, "error": msg}); err != nil {
			return err
		}

		return notifyConnection(ctx, q, id, name, changeTested)
	})
	if err != nil {
		return fmt.Errorf("record test of connection %s: %w", name, err)
	}

	return nil
}

// Resolve implements ports.Connections.
func (c *Connections) Resolve(ctx context.Context, names []string, use domain.ConnectionUse) ([]domain.ResolvedConnection, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	names = uniqueNames(names)
	out := make([]domain.ResolvedConnection, 0, len(names))

	err := c.s.inTx(ctx, func(q *db.Queries) error {
		rows, err := q.SealedConnections(ctx, names)
		if err != nil {
			return err
		}

		byName := make(map[string]db.SealedConnectionsRow, len(rows))
		for _, r := range rows {
			byName[r.Name] = r
		}

		ids := make([]uuid.UUID, 0, len(names))

		for _, name := range names {
			r, ok := byName[name]
			if !ok {
				return fmt.Errorf("connection %q: %w", name, ports.ErrNotFound)
			}

			rc, err := c.resolved(org, r)
			if err != nil {
				return err
			}

			out = append(out, rc)
			ids = append(ids, r.ID)

			if err := connectionAudit(ctx, q, use.Actor(), domain.AuditConnectionUsed, r.ID, use.AuditData(name)); err != nil {
				return err
			}
		}

		return q.MarkConnectionsUsed(ctx, ids)
	})
	if err != nil {
		return nil, fmt.Errorf("resolve connections: %w", err)
	}

	return out, nil
}

// Open implements ports.Connections.
func (c *Connections) Open(ctx context.Context, name string) (domain.ResolvedConnection, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ResolvedConnection{}, domain.ErrNoOrg
	}

	var out domain.ResolvedConnection

	err := c.s.inTx(ctx, func(q *db.Queries) error {
		r, _, err := c.sealedOne(ctx, q, name)
		if err != nil {
			return err
		}

		out, err = c.resolved(org, r)

		return err
	})
	if err != nil {
		return domain.ResolvedConnection{}, fmt.Errorf("open connection %s: %w", name, err)
	}

	return out, nil
}

// resolved opens one connection for a run or a test.
func (c *Connections) resolved(org domain.OrgID, r db.SealedConnectionsRow) (domain.ResolvedConnection, error) {
	conn, err := connectionFrom(sealedConnection(r))
	if err != nil {
		return domain.ResolvedConnection{}, err
	}

	kind, err := decodeKind(r.Kind, r.KindOrgID, r.KindSpec)
	if err != nil {
		return domain.ResolvedConnection{}, err
	}

	values, err := c.open(org, conn.ID, r)
	if err != nil {
		return domain.ResolvedConnection{}, err
	}

	maps.Copy(values, conn.Fields)

	return domain.ResolvedConnection{Connection: conn, Kind: kind, Values: values, SecretRefs: maps.Clone(conn.SecretRefs)}, nil
}

// sealedOne is the connection name, locked, with its kind.
func (c *Connections) sealedOne(ctx context.Context, q *db.Queries, name string) (db.SealedConnectionsRow, domain.ConnectionKind, error) {
	rows, err := q.SealedConnections(ctx, []string{name})
	if err != nil {
		return db.SealedConnectionsRow{}, domain.ConnectionKind{}, err
	}

	if len(rows) == 0 {
		return db.SealedConnectionsRow{}, domain.ConnectionKind{}, ports.ErrNotFound
	}

	kind, err := decodeKind(rows[0].Kind, rows[0].KindOrgID, rows[0].KindSpec)

	return rows[0], kind, err
}

// sealed is a connection's sealed secrets; all nil without any.
type sealed struct {
	keyID             *string
	nonce, ciphertext []byte
}

// seal seals secrets for connection id of org under the active key; nothing for no secrets.
func (c *Connections) seal(org domain.OrgID, id domain.ConnectionID, secrets map[string]string) (sealed, error) {
	if len(secrets) == 0 {
		return sealed{}, nil
	}

	plain, err := json.Marshal(secrets)
	if err != nil {
		return sealed{}, err
	}

	active := c.key.Active()

	keyID, nonce, ct, err := c.key.Seal(plain, domain.ConnectionAAD(org, id, active))
	if err != nil {
		return sealed{}, err
	}

	if keyID != active {
		return sealed{}, fmt.Errorf("master key sealed under %q, not the active %q", keyID, active)
	}

	return sealed{keyID: &keyID, nonce: nonce, ciphertext: ct}, nil
}

// open opens r's secrets; an empty map when it has none.
func (c *Connections) open(org domain.OrgID, id domain.ConnectionID, r db.SealedConnectionsRow) (map[string]string, error) {
	out := map[string]string{}
	if r.KeyID == nil {
		return out, nil
	}

	plain, err := c.key.Open(*r.KeyID, r.Nonce, r.Ciphertext, domain.ConnectionAAD(org, id, *r.KeyID))
	if err != nil {
		return nil, fmt.Errorf("connection %s: %w", r.Name, err)
	}

	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("connection %s: its secrets do not decode", r.Name)
	}

	return out, nil
}

func connectionAudit(ctx context.Context, q *db.Queries, by domain.Actor, action string, id uuid.UUID, data map[string]any) error {
	return appendAudit(ctx, q, domain.AuditEntry{
		Actor: by, Action: action, TargetKind: domain.AuditTargetConnection, TargetID: id.String(), Data: data,
	})
}

func connectionFrom(r db.ConnectionRow) (domain.Connection, error) {
	fields := map[string]string{}
	if err := json.Unmarshal(r.Fields, &fields); err != nil {
		return domain.Connection{}, fmt.Errorf("connection %s: fields: %w", r.Name, err)
	}

	refs := map[string]string{}
	if err := json.Unmarshal(r.SecretRefs, &refs); err != nil {
		return domain.Connection{}, fmt.Errorf("connection %s: secret references: %w", r.Name, err)
	}

	modes := map[string]domain.ConnectionToolMode{}
	if err := json.Unmarshal(r.ToolModes, &modes); err != nil {
		return domain.Connection{}, fmt.Errorf("connection %s: tool modes: %w", r.Name, err)
	}

	out := domain.Connection{
		ID: domain.ConnectionID(r.ID.String()), Name: r.Name, Kind: r.Kind, Description: r.Description, Fields: fields, ToolModes: modes,
		SecretFields: slices.Clone(r.SecretFields), SecretRefs: refs, CreatedBy: namedActor(r.CreatedBy, ""), CreatedAt: r.CreatedAt.UTC(),
		UpdatedBy: namedActor(r.UpdatedBy, ""), UpdatedAt: r.UpdatedAt.UTC(), LastUsedAt: utc(r.LastUsedAt),
	}

	if r.LastTestAt != nil && r.LastTestOk != nil {
		out.LastTest = &domain.ConnectionTest{At: r.LastTestAt.UTC(), OK: *r.LastTestOk, Error: deref(r.LastTestError), Tools: toolNames(r.LastTestTools)}
	}

	return out, nil
}

// sealedConnection is r's connection columns.
func sealedConnection(r db.SealedConnectionsRow) db.ConnectionRow {
	return db.ConnectionRow{
		ID: r.ID, Name: r.Name, Kind: r.Kind, Description: r.Description, Fields: r.Fields, SecretFields: r.SecretFields,
		SecretRefs: r.SecretRefs, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedBy: r.UpdatedBy, UpdatedAt: r.UpdatedAt, LastUsedAt: r.LastUsedAt,
		LastTestAt: r.LastTestAt, LastTestOk: r.LastTestOk, LastTestError: r.LastTestError, LastTestTools: r.LastTestTools, ToolModes: r.ToolModes,
	}
}

// setValues are the values of m that are set (not empty); never nil.
func setValues(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))

	for k, v := range m {
		if v != "" {
			out[k] = v
		}
	}

	return out
}

// valueNames are m's keys, sorted; never nil, as text[] NOT NULL needs.
func valueNames(m map[string]string) []string {
	return append([]string{}, slices.Sorted(maps.Keys(m))...)
}

// uniqueNames are names without repeats, in their first order.
func uniqueNames(names []string) []string {
	out := make([]string, 0, len(names))

	for _, n := range names {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}

	return out
}

func isForeignKeyViolation(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)

	return ok && pgErr.Code == foreignKeyViolation
}

// ConnectionKeyIDs are the master key ids some connection, credential or webhook endpoint of any
// organization is sealed under (goal_connection_key_ids, as 00071 replaced it): goald refuses to
// start without each of them.
func ConnectionKeyIDs(ctx context.Context, s *Store) ([]string, error) {
	var ids []string

	err := s.systemTx(ctx, func(q *db.Queries) error {
		var err error

		ids, err = q.ConnectionKeyIDs(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("connection key ids: %w", err)
	}

	return ids, nil
}

// rekeyBatch is how many connections, credentials or webhook secrets one transaction of Rekey
// reseals.
const rekeyBatch = 100

// Rekey reseals every connection, credential and webhook signing secret of every organization
// that is sealed under another master key than key's active one (`goal db rekey`, spec 4 §5.2,
// ADR 0017, ADR 0026), rekeyBatch per transaction, and returns how many it resealed. s's pool
// connects as goal_owner, which row-level security does not bind: rekeying is the install's, not
// an organization's. Values never leave this function; a row that does not open stops it with an error naming its id.
func Rekey(ctx context.Context, s *Store, key ports.MasterKey) (int, error) {
	if key.Active() == "" {
		return 0, ports.ErrNoMasterKey
	}

	total := 0

	for _, batch := range []func(context.Context, *db.Queries, ports.MasterKey) (int, error){resealConnections, resealCredentials, resealWebhookSecrets} {
		for {
			var n int

			err := s.systemTx(ctx, func(q *db.Queries) error {
				var err error

				n, err = batch(ctx, q, key)

				return err
			})
			if err != nil {
				return total, fmt.Errorf("rekey: %w", err)
			}

			total += n

			if n < rekeyBatch {
				break
			}
		}
	}

	return total, nil
}

// resealConnections reseals a batch of connections of every organization sealed under another
// key than key's active one, and returns how many.
func resealConnections(ctx context.Context, q *db.Queries, key ports.MasterKey) (int, error) {
	rows, err := q.StaleSealedConnections(ctx, db.StaleSealedConnectionsParams{Active: key.Active(), Batch: rekeyBatch})
	if err != nil {
		return 0, err
	}

	for _, r := range rows {
		if err := reseal(ctx, q, key, r); err != nil {
			return 0, err
		}
	}

	return len(rows), nil
}

func reseal(ctx context.Context, q *db.Queries, key ports.MasterKey, r db.StaleSealedConnectionsRow) error {
	org, id := domain.OrgID(r.OrgID.String()), domain.ConnectionID(r.ID.String())

	plain, err := key.Open(*r.KeyID, r.Nonce, r.Ciphertext, domain.ConnectionAAD(org, id, *r.KeyID))
	if err != nil {
		return fmt.Errorf("connection %s: %w", id, err)
	}

	active := key.Active()

	keyID, nonce, ct, err := key.Seal(plain, domain.ConnectionAAD(org, id, active))
	if err != nil {
		return fmt.Errorf("connection %s: %w", id, err)
	}

	return q.ResealConnection(ctx, db.ResealConnectionParams{KeyID: &keyID, Nonce: nonce, Ciphertext: ct, ID: r.ID})
}
