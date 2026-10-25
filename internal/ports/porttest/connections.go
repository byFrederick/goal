package porttest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// MasterKeys stand in for the platform's master keys without cryptography: Seal returns random
// bytes and remembers what they seal, Open gives it back only for the same key id and associated
// data, as AES-GCM would. Keys made by one MasterKeys share what they sealed.
type MasterKeys struct {
	mu     sync.Mutex
	sealed map[string]sealedValue // by nonce and ciphertext
}

type sealedValue struct {
	keyID      string
	aad, plain []byte
}

// NewMasterKeys returns master keys that sealed nothing yet.
func NewMasterKeys() *MasterKeys { return &MasterKeys{sealed: map[string]sealedValue{}} }

// Key returns the keys ids, the first active; with none, Seal fails with ports.ErrNoMasterKey.
func (m *MasterKeys) Key(ids ...string) ports.MasterKey {
	return &fakeKey{m: m, ids: slices.Clone(ids)}
}

type fakeKey struct {
	m   *MasterKeys
	ids []string
}

func (k *fakeKey) Active() string {
	if len(k.ids) == 0 {
		return ""
	}

	return k.ids[0]
}

func (k *fakeKey) Seal(plaintext, aad []byte) (keyID string, nonce, ciphertext []byte, err error) {
	if len(k.ids) == 0 {
		return "", nil, nil, ports.ErrNoMasterKey
	}

	nonce, ciphertext = make([]byte, 12), make([]byte, 16+len(plaintext))
	_, _ = rand.Read(nonce)
	_, _ = rand.Read(ciphertext)

	k.m.mu.Lock()
	defer k.m.mu.Unlock()

	k.m.sealed[string(nonce)+string(ciphertext)] = sealedValue{keyID: k.ids[0], aad: bytes.Clone(aad), plain: bytes.Clone(plaintext)}

	return k.ids[0], nonce, ciphertext, nil
}

func (k *fakeKey) Open(keyID string, nonce, ciphertext, aad []byte) ([]byte, error) {
	if !slices.Contains(k.ids, keyID) {
		if len(k.ids) == 0 {
			return nil, ports.ErrNoMasterKey
		}

		return nil, fmt.Errorf("master key %q is not configured", keyID)
	}

	k.m.mu.Lock()
	defer k.m.mu.Unlock()

	v, ok := k.m.sealed[string(nonce)+string(ciphertext)]
	if !ok || v.keyID != keyID || !bytes.Equal(v.aad, aad) {
		return nil, errors.New("the sealed value does not open")
	}

	return bytes.Clone(v.plain), nil
}

var _ ports.Connections = (*Connections)(nil)

// Connections keeps connection kinds and connections in memory with the database's rules: the
// platform's kinds and each organization's, names unique per organization, secrets sealed with
// the master key under domain.ConnectionAAD and never returned but by Resolve and Open, and an audit entry
// through audit for every change and use. Views made with WithKey share the data, as goalds
// with different keys share one database.
type Connections struct {
	d     *connectionData
	key   ports.MasterKey
	audit ports.Audit
}

type connectionData struct {
	mu       sync.Mutex
	platform map[string]domain.ConnectionKind
	orgKinds map[domain.OrgID]map[string]domain.ConnectionKind
	conns    map[domain.OrgID]map[string]*connectionRow
	// readBy reports whether a knowledge source reads a connection (NewKnowledge sets it), which
	// then cannot be deleted, as the database's foreign key refuses.
	readBy func(domain.OrgID, domain.ConnectionID) bool
}

type connectionRow struct {
	conn domain.Connection
	// own: the kind is the organization's, not the platform's.
	own               bool
	keyID             string
	nonce, ciphertext []byte
}

// NewConnections returns connections sealed with key and audited through audit (a Tenancy that
// knows the organizations).
func NewConnections(key ports.MasterKey, audit ports.Audit) *Connections {
	return &Connections{key: key, audit: audit, d: &connectionData{
		platform: map[string]domain.ConnectionKind{},
		orgKinds: map[domain.OrgID]map[string]domain.ConnectionKind{},
		conns:    map[domain.OrgID]map[string]*connectionRow{},
	}}
}

// WithKey returns a view of c's data sealed with key.
func (c *Connections) WithKey(key ports.MasterKey) *Connections {
	return &Connections{d: c.d, key: key, audit: c.audit}
}

// Kinds implements ports.Connections.
func (c *Connections) Kinds(ctx context.Context) ([]domain.ConnectionKind, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	all := maps.Clone(c.d.platform)
	maps.Copy(all, c.d.orgKinds[org])

	out := slices.Collect(maps.Values(all))
	slices.SortFunc(out, func(a, b domain.ConnectionKind) int { return strings.Compare(a.Name, b.Name) })

	return out, nil
}

// Kind implements ports.Connections.
func (c *Connections) Kind(ctx context.Context, name string) (domain.ConnectionKind, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ConnectionKind{}, domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	k, _, ok := c.d.kindLocked(org, name)
	if !ok {
		return domain.ConnectionKind{}, fmt.Errorf("connection kind %q: %w", name, ports.ErrNotFound)
	}

	return k, nil
}

// kindLocked is the kind name as org sees it, and whether it is org's own.
func (d *connectionData) kindLocked(org domain.OrgID, name string) (domain.ConnectionKind, bool, bool) {
	if k, ok := d.orgKinds[org][name]; ok {
		return k, true, true
	}

	k, ok := d.platform[name]

	return k, false, ok
}

// PutKind implements ports.Connections.
func (c *Connections) PutKind(ctx context.Context, k domain.ConnectionKind, by domain.Actor) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	if err := k.Check(); err != nil {
		return err
	}

	k.Platform = false

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	old, exists := c.d.orgKinds[org][k.Name]

	switch {
	case exists && equalKinds(old, k):
		return nil
	case !exists:
		if _, taken := c.d.platform[k.Name]; taken {
			return fmt.Errorf("a platform kind is named %s: %w", k.Name, ports.ErrNameTaken)
		}
	}

	action := domain.AuditConnectionKindCreated
	if exists {
		action = domain.AuditConnectionKindUpdated
	}

	if err := c.appendAudit(ctx, by, action, domain.AuditTargetConnectionKind, k.Name, map[string]any{auditName: k.Name}); err != nil {
		return err
	}

	if c.d.orgKinds[org] == nil {
		c.d.orgKinds[org] = map[string]domain.ConnectionKind{}
	}

	c.d.orgKinds[org][k.Name] = cloneKind(k)

	return nil
}

// DeleteKind implements ports.Connections.
func (c *Connections) DeleteKind(ctx context.Context, name string, by domain.Actor) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	if _, ok := c.d.orgKinds[org][name]; !ok {
		return fmt.Errorf("connection kind %q: %w", name, ports.ErrNotFound)
	}

	for _, r := range c.d.conns[org] {
		if r.own && r.conn.Kind == name {
			return fmt.Errorf("connections use kind %s: %w", name, ports.ErrConflict)
		}
	}

	if err := c.appendAudit(ctx, by, domain.AuditConnectionKindDeleted, domain.AuditTargetConnectionKind, name, map[string]any{auditName: name}); err != nil {
		return err
	}

	delete(c.d.orgKinds[org], name)

	return nil
}

// SyncPlatformKinds implements ports.Connections.
func (c *Connections) SyncPlatformKinds(_ context.Context, kinds []domain.ConnectionKind) error {
	for _, k := range kinds {
		if err := k.Check(); err != nil {
			return fmt.Errorf("sync platform kinds: %w", err)
		}
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	listed := map[string]bool{}

	for _, k := range kinds {
		k.Platform = true
		c.d.platform[k.Name] = cloneKind(k)
		listed[k.Name] = true
	}

	for name := range c.d.platform {
		if !listed[name] && !c.d.platformKindUsedLocked(name) {
			delete(c.d.platform, name)
		}
	}

	return nil
}

func (d *connectionData) platformKindUsedLocked(name string) bool {
	for _, rows := range d.conns {
		for _, r := range rows {
			if !r.own && r.conn.Kind == name {
				return true
			}
		}
	}

	return false
}

// List implements ports.Connections.
func (c *Connections) List(ctx context.Context) ([]domain.Connection, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	out := make([]domain.Connection, 0, len(c.d.conns[org]))
	for _, name := range slices.Sorted(maps.Keys(c.d.conns[org])) {
		out = append(out, cloneConnection(c.d.conns[org][name].conn))
	}

	return out, nil
}

// Get implements ports.Connections.
func (c *Connections) Get(ctx context.Context, name string) (domain.Connection, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Connection{}, domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	r, ok := c.d.conns[org][name]
	if !ok {
		return domain.Connection{}, fmt.Errorf("connection %q: %w", name, ports.ErrNotFound)
	}

	return cloneConnection(r.conn), nil
}

// Create implements ports.Connections.
func (c *Connections) Create(ctx context.Context, n domain.NewConnection, by domain.Actor) (domain.Connection, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Connection{}, domain.ErrNoOrg
	}

	if err := errors.Join(domain.CheckConnectionName(n.Name), domain.CheckConnectionDescription(n.Description)); err != nil {
		return domain.Connection{}, err
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	kind, own, ok := c.d.kindLocked(org, n.Kind)
	if !ok {
		return domain.Connection{}, fmt.Errorf("connection kind %q: %w", n.Kind, ports.ErrNotFound)
	}

	fields, secrets := setOnly(n.Fields), setOnly(n.Secrets)
	secretNames := slices.Sorted(maps.Keys(secrets))

	if err := kind.CheckValues(fields, secretNames); err != nil {
		return domain.Connection{}, err
	}

	if _, taken := c.d.conns[org][n.Name]; taken {
		return domain.Connection{}, fmt.Errorf("connection %q: %w", n.Name, ports.ErrNameTaken)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return domain.Connection{}, err
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	r := &connectionRow{own: own, conn: domain.Connection{
		ID: domain.ConnectionID(id.String()), Name: n.Name, Kind: n.Kind, Description: n.Description, Fields: fields,
		SecretFields: secretNames, CreatedBy: by, CreatedAt: now, UpdatedBy: by, UpdatedAt: now,
	}}

	if err := c.sealLocked(org, r, secrets); err != nil {
		return domain.Connection{}, err
	}

	err = c.appendAudit(ctx, by, domain.AuditConnectionCreated, domain.AuditTargetConnection, string(r.conn.ID), map[string]any{
		auditName: n.Name, "kind": n.Kind, "fields": slices.Sorted(maps.Keys(fields)), "secrets": secretNames,
	})
	if err != nil {
		return domain.Connection{}, err
	}

	if c.d.conns[org] == nil {
		c.d.conns[org] = map[string]*connectionRow{}
	}

	c.d.conns[org][n.Name] = r

	return cloneConnection(r.conn), nil
}

// Update implements ports.Connections.
func (c *Connections) Update(ctx context.Context, name string, p domain.ConnectionPatch, by domain.Actor) (domain.Connection, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.Connection{}, domain.ErrNoOrg
	}

	if err := checkPatch(p); err != nil {
		return domain.Connection{}, err
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	old, ok := c.d.conns[org][name]
	if !ok {
		return domain.Connection{}, fmt.Errorf("connection %q: %w", name, ports.ErrNotFound)
	}

	if p.Empty() {
		return cloneConnection(old.conn), nil
	}

	r := &connectionRow{own: old.own, conn: cloneConnection(old.conn), keyID: old.keyID, nonce: old.nonce, ciphertext: old.ciphertext}

	if p.Name != nil && *p.Name != name {
		if _, taken := c.d.conns[org][*p.Name]; taken {
			return domain.Connection{}, fmt.Errorf("connection %q: %w", *p.Name, ports.ErrNameTaken)
		}

		r.conn.Name = *p.Name
	}

	if p.Description != nil {
		r.conn.Description = *p.Description
	}

	if p.Fields != nil {
		r.conn.Fields = setOnly(p.Fields)
	}

	if err := c.replaceLocked(org, r, p.Secrets); err != nil {
		return domain.Connection{}, err
	}

	kind, _, _ := c.d.kindLocked(org, r.conn.Kind)
	if err := kind.CheckValues(r.conn.Fields, r.conn.SecretFields); err != nil {
		return domain.Connection{}, err
	}

	if err := c.updateAudits(ctx, by, name, r.conn, p); err != nil {
		return domain.Connection{}, err
	}

	r.conn.UpdatedBy, r.conn.UpdatedAt = by, time.Now().UTC().Truncate(time.Microsecond)

	delete(c.d.conns[org], name)
	c.d.conns[org][r.conn.Name] = r

	return cloneConnection(r.conn), nil
}

// checkPatch refuses a patch's malformed name or description.
func checkPatch(p domain.ConnectionPatch) error {
	var errs []error

	if p.Name != nil {
		errs = append(errs, domain.CheckConnectionName(*p.Name))
	}

	if p.Description != nil {
		errs = append(errs, domain.CheckConnectionDescription(*p.Description))
	}

	return errors.Join(errs...)
}

// replaceLocked opens r's secrets, replaces those of with (an empty value clears one) and reseals.
func (c *Connections) replaceLocked(org domain.OrgID, r *connectionRow, with map[string]string) error {
	if len(with) == 0 {
		return nil
	}

	secrets, err := c.openLocked(org, r)
	if err != nil {
		return err
	}

	for k, v := range with {
		if v == "" {
			delete(secrets, k)
		} else {
			secrets[k] = v
		}
	}

	r.conn.SecretFields = slices.Sorted(maps.Keys(secrets))

	return c.sealLocked(org, r, secrets)
}

func (c *Connections) updateAudits(ctx context.Context, by domain.Actor, from string, conn domain.Connection, p domain.ConnectionPatch) error {
	if p.Name != nil || p.Description != nil || p.Fields != nil {
		data := map[string]any{auditName: conn.Name}
		if p.Fields != nil {
			data["fields"] = slices.Sorted(maps.Keys(setOnly(p.Fields)))
		}

		if conn.Name != from {
			data["from"] = from
		}

		if err := c.appendAudit(ctx, by, domain.AuditConnectionUpdated, domain.AuditTargetConnection, string(conn.ID), data); err != nil {
			return err
		}
	}

	if len(p.Secrets) == 0 {
		return nil
	}

	return c.appendAudit(ctx, by, domain.AuditConnectionSecretReplaced, domain.AuditTargetConnection, string(conn.ID),
		map[string]any{auditName: conn.Name, "secrets": slices.Sorted(maps.Keys(p.Secrets))})
}

// Delete implements ports.Connections.
func (c *Connections) Delete(ctx context.Context, name string, by domain.Actor) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	r, ok := c.d.conns[org][name]
	if !ok {
		return fmt.Errorf("connection %q: %w", name, ports.ErrNotFound)
	}

	if c.d.readBy != nil && c.d.readBy(org, r.conn.ID) {
		return fmt.Errorf("knowledge sources use it: %w", ports.ErrConflict)
	}

	err := c.appendAudit(ctx, by, domain.AuditConnectionDeleted, domain.AuditTargetConnection, string(r.conn.ID),
		map[string]any{auditName: name, "kind": r.conn.Kind})
	if err != nil {
		return err
	}

	delete(c.d.conns[org], name)

	return nil
}

// RecordTest implements ports.Connections.
func (c *Connections) RecordTest(ctx context.Context, name string, t domain.ConnectionTest, by domain.Actor) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	if utf8.RuneCountInString(t.Error) > 500 {
		t.Error = string([]rune(t.Error)[:500])
	}

	if t.At.IsZero() {
		t.At = time.Now()
	}

	t.At = t.At.UTC().Truncate(time.Microsecond)

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	r, ok := c.d.conns[org][name]
	if !ok {
		return fmt.Errorf("connection %q: %w", name, ports.ErrNotFound)
	}

	err := c.appendAudit(ctx, by, domain.AuditConnectionTested, domain.AuditTargetConnection, string(r.conn.ID),
		map[string]any{auditName: name, "ok": t.OK, "error": t.Error})
	if err != nil {
		return err
	}

	r.conn.LastTest = &t

	return nil
}

// Resolve implements ports.Connections.
func (c *Connections) Resolve(ctx context.Context, names []string, run domain.RunID) ([]domain.ResolvedConnection, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	var (
		out  []domain.ResolvedConnection
		rows []*connectionRow
	)

	for _, name := range names {
		r, ok := c.d.conns[org][name]
		if !ok {
			return nil, fmt.Errorf("connection %q: %w", name, ports.ErrNotFound)
		}

		if slices.Contains(rows, r) {
			continue
		}

		rc, err := c.resolvedLocked(org, r)
		if err != nil {
			return nil, err
		}

		rows = append(rows, r)
		out = append(out, rc)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)

	for _, r := range rows {
		if err := c.appendAudit(ctx, domain.EngineActor, domain.AuditConnectionUsed, domain.AuditTargetConnection, string(r.conn.ID),
			map[string]any{auditName: r.conn.Name, "run_id": string(run)}); err != nil {
			return nil, err
		}

		r.conn.LastUsedAt = &now
	}

	for i := range out {
		out[i].Connection.LastUsedAt = &now
	}

	return out, nil
}

// Open implements ports.Connections.
func (c *Connections) Open(ctx context.Context, name string) (domain.ResolvedConnection, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ResolvedConnection{}, domain.ErrNoOrg
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	r, ok := c.d.conns[org][name]
	if !ok {
		return domain.ResolvedConnection{}, fmt.Errorf("connection %q: %w", name, ports.ErrNotFound)
	}

	return c.resolvedLocked(org, r)
}

// resolvedLocked opens r with its kind and every value.
func (c *Connections) resolvedLocked(org domain.OrgID, r *connectionRow) (domain.ResolvedConnection, error) {
	values, err := c.openLocked(org, r)
	if err != nil {
		return domain.ResolvedConnection{}, err
	}

	maps.Copy(values, r.conn.Fields)

	kind, _, _ := c.d.kindLocked(org, r.conn.Kind)
	if !r.own {
		kind = c.d.platform[r.conn.Kind]
	}

	return domain.ResolvedConnection{Connection: cloneConnection(r.conn), Kind: cloneKind(kind), Values: values}, nil
}

// Rekey reseals every organization's connections sealed under another key than key's active one
// under it, as goal db rekey does, and returns how many it resealed.
func (c *Connections) Rekey(key ports.MasterKey) (int, error) {
	if key.Active() == "" {
		return 0, ports.ErrNoMasterKey
	}

	c.d.mu.Lock()
	defer c.d.mu.Unlock()

	v := c.WithKey(key)
	n := 0

	for org, rows := range c.d.conns {
		for _, r := range rows {
			if r.keyID == "" || r.keyID == key.Active() {
				continue
			}

			secrets, err := v.openLocked(org, r)
			if err != nil {
				return n, err
			}

			if err := v.sealLocked(org, r, secrets); err != nil {
				return n, err
			}

			n++
		}
	}

	return n, nil
}

func (c *Connections) sealLocked(org domain.OrgID, r *connectionRow, secrets map[string]string) error {
	if len(secrets) == 0 {
		r.keyID, r.nonce, r.ciphertext = "", nil, nil

		return nil
	}

	plain, err := json.Marshal(secrets)
	if err != nil {
		return err
	}

	keyID, nonce, ct, err := c.key.Seal(plain, domain.ConnectionAAD(org, r.conn.ID, c.key.Active()))
	if err != nil {
		return err
	}

	r.keyID, r.nonce, r.ciphertext = keyID, nonce, ct

	return nil
}

func (c *Connections) openLocked(org domain.OrgID, r *connectionRow) (map[string]string, error) {
	out := map[string]string{}
	if r.keyID == "" {
		return out, nil
	}

	plain, err := c.key.Open(r.keyID, r.nonce, r.ciphertext, domain.ConnectionAAD(org, r.conn.ID, r.keyID))
	if err != nil {
		return nil, fmt.Errorf("connection %s: %w", r.conn.Name, err)
	}

	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, err
	}

	return out, nil
}

func (c *Connections) appendAudit(ctx context.Context, by domain.Actor, action, kind, id string, data map[string]any) error {
	return c.audit.Append(ctx, domain.AuditEntry{Actor: by, Action: action, TargetKind: kind, TargetID: id, Data: data})
}

// setOnly are m's values that are set; never nil.
func setOnly(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))

	for k, v := range m {
		if v != "" {
			out[k] = v
		}
	}

	return out
}

func cloneConnection(c domain.Connection) domain.Connection {
	c.Fields = maps.Clone(c.Fields)
	c.SecretFields = slices.Clone(c.SecretFields)

	if c.LastUsedAt != nil {
		at := *c.LastUsedAt
		c.LastUsedAt = &at
	}

	if c.LastTest != nil {
		t := *c.LastTest
		c.LastTest = &t
	}

	return c
}

// cloneKind copies k through JSON, as the database stores it.
func cloneKind(k domain.ConnectionKind) domain.ConnectionKind {
	raw, _ := json.Marshal(k.Spec) // a KindSpec always marshals

	var spec domain.KindSpec
	if json.Unmarshal(raw, &spec) == nil {
		k.Spec = spec
	}

	return k
}

func equalKinds(a, b domain.ConnectionKind) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)

	return bytes.Equal(ra, rb)
}
