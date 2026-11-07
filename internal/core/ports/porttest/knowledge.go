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

var _ ports.Knowledge = (*Knowledge)(nil)

// auditKind is the audit data key of a source's kind.
const auditKind = "kind"

// Knowledge keeps knowledge bases in memory with the database's rules: names unique per
// organization, leases claimed by
// time, an audit entry for every change people make, and the files uploaded to upload sources.
type Knowledge struct {
	audit ports.Audit
	// secrets are the ones sources read with, once ReadSecrets set them; until then any name is one.
	secrets *OrgSecrets

	mu   sync.Mutex
	orgs map[domain.OrgID]*knowledgeOrg
}

type knowledgeOrg struct {
	sources map[string]*fakeSource // by name
	// pinned are the snapshots running agent runs pinned (Pin).
	pinned map[domain.KnowledgeSnapshotID]bool
}

type fakeSource struct {
	src   domain.KnowledgeSource
	token string // the lease's, "" when none holds it
	// snapshots (oldest first), the current files by path and the read counts go with the
	// source, as 00090's cascade takes them.
	snapshots []domain.KnowledgeSnapshot
	files     map[string]domain.KnowledgeFile
	reads     map[readKey]domain.KnowledgeFileReads
	// uploads are an upload source's files by name.
	uploads map[string]domain.KnowledgeUpload
}

// NewKnowledge returns an empty knowledge base audited through audit (a Tenancy that knows the
// organizations).
func NewKnowledge(audit ports.Audit) *Knowledge {
	return &Knowledge{audit: audit, orgs: map[domain.OrgID]*knowledgeOrg{}}
}

// ReadSecrets makes secrets the ones sources read with, as the database's foreign key does: a
// source names an existing secret, follows its renames, and the secret cannot be deleted while a
// source reads with it.
func (k *Knowledge) ReadSecrets(secrets *OrgSecrets) {
	k.mu.Lock()
	k.secrets = secrets
	k.mu.Unlock()

	secrets.d.mu.Lock()
	defer secrets.d.mu.Unlock()

	secrets.d.readBy = func(org domain.OrgID, name string) []string {
		k.mu.Lock()
		defer k.mu.Unlock()

		var out []string

		for _, n := range slices.Sorted(maps.Keys(k.org(org).sources)) {
			if k.org(org).sources[n].src.Secret == name {
				out = append(out, n)
			}
		}

		return out
	}
	secrets.d.renamed = func(org domain.OrgID, from, to string) {
		k.mu.Lock()
		defer k.mu.Unlock()

		for _, s := range k.org(org).sources {
			if s.src.Secret == from {
				s.src.Secret = to
			}
		}
	}
}

// secret refuses a secret name that is none of ReadSecrets' (ErrNotFound). k.mu is not held.
func (k *Knowledge) secret(ctx context.Context, name string) error {
	k.mu.Lock()
	secrets := k.secrets
	k.mu.Unlock()

	if name == "" || secrets == nil {
		return nil
	}

	if _, err := secrets.Get(ctx, name); err != nil {
		return fmt.Errorf("secret %q: %w", name, err)
	}

	return nil
}

// org is org's knowledge base, made on first use. k.mu is held.
func (k *Knowledge) org(org domain.OrgID) *knowledgeOrg {
	o, ok := k.orgs[org]
	if !ok {
		o = &knowledgeOrg{sources: map[string]*fakeSource{}, pinned: map[domain.KnowledgeSnapshotID]bool{}}
		k.orgs[org] = o
	}

	return o
}

// locked runs fn on the organization in ctx's knowledge base under k.mu.
func (k *Knowledge) locked(ctx context.Context, fn func(org domain.OrgID, o *knowledgeOrg) error) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	return fn(org, k.org(org))
}

// Sources implements ports.Knowledge.
func (k *Knowledge) Sources(ctx context.Context) ([]domain.KnowledgeSource, error) {
	var out []domain.KnowledgeSource

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		for _, name := range slices.Sorted(maps.Keys(o.sources)) {
			out = append(out, o.view(o.sources[name]))
		}

		return nil
	})

	return out, err
}

// view is s as it is read: a copy.
func (o *knowledgeOrg) view(s *fakeSource) domain.KnowledgeSource {
	out := s.src
	out.Config = maps.Clone(s.src.Config)

	return out
}

// Source implements ports.Knowledge.
func (k *Knowledge) Source(ctx context.Context, name string) (domain.KnowledgeSource, error) {
	var out domain.KnowledgeSource

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[name]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
		}

		out = o.view(s)

		return nil
	})

	return out, err
}

// CreateSource implements ports.Knowledge.
func (k *Knowledge) CreateSource(ctx context.Context, n domain.NewKnowledgeSource, by domain.Actor) (domain.KnowledgeSource, error) {
	if err := n.Check(); err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("create knowledge source: %w", err)
	}

	if err := k.secret(ctx, n.Secret); err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("create knowledge source: %w", err)
	}

	var out domain.KnowledgeSource

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		if _, taken := o.sources[n.Name]; taken {
			return fmt.Errorf("knowledge source %q: %w", n.Name, ports.ErrNameTaken)
		}

		now := time.Now().UTC()
		s := domain.KnowledgeSource{
			ID: domain.KnowledgeSourceID(newV7()), Name: n.Name, Title: n.Title, Kind: n.Kind, Secret: n.Secret,
			Config: maps.Clone(n.Config), ReadWhen: n.ReadWhen, SyncEvery: n.Every(), Status: domain.SyncIdle,
			NextSyncAt: &now, CreatedBy: by, CreatedAt: now, UpdatedAt: now,
		}

		if s.Config == nil {
			s.Config = map[string]string{}
		}

		if err := k.audit.Append(ctx, sourceEntry(by, domain.AuditKnowledgeSourceCreated, s.ID,
			sourceAudit(n))); err != nil {
			return err
		}

		o.sources[n.Name] = &fakeSource{src: s}
		out = o.view(o.sources[n.Name])

		return nil
	})
	if err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("create knowledge source: %w", err)
	}

	return out, nil
}

// UpdateSource implements ports.Knowledge.
func (k *Knowledge) UpdateSource(ctx context.Context, name string, p domain.KnowledgeSourcePatch, by domain.Actor) (domain.KnowledgeSource, error) {
	if err := p.Check(); err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("update knowledge source: %w", err)
	}

	if p.Secret != nil {
		if err := k.secret(ctx, *p.Secret); err != nil {
			return domain.KnowledgeSource{}, fmt.Errorf("update knowledge source: %w", err)
		}
	}

	var out domain.KnowledgeSource

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[name]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
		}

		if err := p.CheckOn(s.src); err != nil {
			return err
		}

		if err := k.audit.Append(ctx, sourceEntry(by, domain.AuditKnowledgeSourceUpdated, s.src.ID, map[string]any{auditName: name})); err != nil {
			return err
		}

		if p.Title != nil {
			s.src.Title = *p.Title
		}

		if p.Config != nil {
			s.src.Config = maps.Clone(p.Config)
		}

		if p.SyncEvery != nil {
			s.src.SyncEvery = *p.SyncEvery
			s.src.NextSyncAt = patchedNextSync(s.src, time.Now().UTC())
		}

		now := time.Now().UTC()
		newReadWhen(&s.src, p.ReadWhen, now)

		if p.Secret != nil {
			s.src.Secret = *p.Secret
		}

		s.src.UpdatedAt = now
		out = o.view(s)

		return nil
	})

	return out, err
}

// newReadWhen sets src's read-when line to line when it differs, and then makes a source that
// writes snapshots (upload, repository, Confluence) due at now.
func newReadWhen(src *domain.KnowledgeSource, line *string, now time.Time) {
	if line == nil || *line == src.ReadWhen {
		return
	}

	src.ReadWhen = *line

	switch src.Kind {
	case domain.SourceUpload, domain.SourceRepository, domain.SourceConfluence:
		src.NextSyncAt = &now
	case domain.SourceSynced:
	}
}

// ConvertSource implements ports.Knowledge.
func (k *Knowledge) ConvertSource(ctx context.Context, name string, kind domain.KnowledgeSourceKind, by domain.Actor) (domain.KnowledgeSource, error) {
	var out domain.KnowledgeSource

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[name]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
		}

		if err := s.src.CheckConvert(kind); err != nil {
			return err
		}

		if err := k.audit.Append(ctx, sourceEntry(by, domain.AuditKnowledgeSourceUpdated, s.src.ID,
			map[string]any{auditName: name, auditKind: string(kind)})); err != nil {
			return err
		}

		now := time.Now().UTC()
		s.src.Kind = kind
		s.src.Config = s.src.ConvertedConfig()
		s.src.NextSyncAt, s.src.UpdatedAt = &now, now
		out = o.view(s)

		return nil
	})
	if err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("convert knowledge source: %w", err)
	}

	return out, nil
}

// sourceAudit is the data of a new source's created entry: its name, kind and what it reads.
func sourceAudit(n domain.NewKnowledgeSource) map[string]any {
	out := map[string]any{auditName: n.Name, auditKind: string(n.Kind)}
	if n.Secret != "" {
		out["secret"] = n.Secret
	}

	return out
}

// DeleteSource implements ports.Knowledge; drop runs before anything changes, so its error
// changes nothing.
func (k *Knowledge) DeleteSource(ctx context.Context, name string, by domain.Actor, drop ports.DropBlobs) error {
	return k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[name]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
		}

		var blobs []domain.BlobHash

		for _, u := range s.uploads {
			blobs = append(blobs, u.Blob)
		}

		var orphans []domain.BlobHash

		for _, b := range slices.Compact(slices.Sorted(slices.Values(blobs))) {
			if !o.holds(b, s.src.ID, "") {
				orphans = append(orphans, b)
			}
		}

		if err := dropped(ctx, drop, orphans); err != nil {
			return err
		}

		if err := k.audit.Append(ctx, sourceEntry(by, domain.AuditKnowledgeSourceDeleted, s.src.ID,
			map[string]any{auditName: name, auditKind: string(s.src.Kind)})); err != nil {
			return err
		}

		delete(o.sources, name)

		return nil
	})
}

// dropped calls drop with unheld unless either is empty.
func dropped(ctx context.Context, drop ports.DropBlobs, unheld []domain.BlobHash) error {
	if drop == nil || len(unheld) == 0 {
		return nil
	}

	return drop(ctx, unheld)
}

// SyncNow implements ports.Knowledge.
func (k *Knowledge) SyncNow(ctx context.Context, name string) error {
	return k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[name]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
		}

		now := time.Now().UTC()
		s.src.NextSyncAt = &now

		return nil
	})
}

// patchedNextSync is src's next sync once its interval changed to src.SyncEvery: none on demand, now
// for one that waited for Sync, else as it was.
func patchedNextSync(src domain.KnowledgeSource, now time.Time) *time.Time {
	switch {
	case src.SyncEvery == domain.SyncOnDemand:
		return nil
	case src.NextSyncAt == nil:
		return &now
	default:
		return src.NextSyncAt
	}
}

// ClaimSource implements ports.Knowledge: the source due first of every organization's.
func (k *Knowledge) ClaimSource(_ context.Context, now, until time.Time) (*domain.SourceClaim, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	var (
		best    *fakeSource
		bestOrg domain.OrgID
	)

	for org, o := range k.orgs {
		for _, s := range o.sources {
			if !claimable(s.src, now) {
				continue
			}

			if best == nil || s.src.NextSyncAt.Before(*best.src.NextSyncAt) ||
				(s.src.NextSyncAt.Equal(*best.src.NextSyncAt) && s.src.ID < best.src.ID) {
				best, bestOrg = s, org
			}
		}
	}

	if best == nil {
		return nil, nil //nolint:nilnil // nothing is due
	}

	u := until.UTC()
	best.src.LeaseUntil, best.src.Status, best.token = &u, domain.SyncSyncing, uuid.NewString()

	return &domain.SourceClaim{
		Org: bestOrg, Source: k.orgs[bestOrg].view(best), Lease: domain.SourceLease{Source: best.src.ID, Token: best.token},
	}, nil
}

// claimable is the claim's rule: its lease ended, and due; an on-demand source waiting
// for Sync never is.
func claimable(s domain.KnowledgeSource, now time.Time) bool {
	if s.NextSyncAt == nil || (s.LeaseUntil != nil && s.LeaseUntil.After(now)) {
		return false
	}

	return !s.NextSyncAt.After(now)
}

// RenewSource implements ports.Knowledge.
func (k *Knowledge) RenewSource(ctx context.Context, l domain.SourceLease, now, until time.Time) error {
	return k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, err := o.leased(l)
		if err != nil {
			return err
		}

		if s.src.Status != domain.SyncSyncing || s.src.LeaseUntil == nil || !s.src.LeaseUntil.After(now) {
			return fmt.Errorf("knowledge source %s: %w", l.Source, ports.ErrLeaseLost)
		}

		u := until.UTC()
		s.src.LeaseUntil = &u

		return nil
	})
}

// leased is the source l holds; ErrLeaseLost when another claim took it or none holds it.
func (o *knowledgeOrg) leased(l domain.SourceLease) (*fakeSource, error) {
	s := o.byID(l.Source)
	if s == nil || s.token == "" || s.token != l.Token {
		return nil, fmt.Errorf("knowledge source %s: %w", l.Source, ports.ErrLeaseLost)
	}

	return s, nil
}

func (o *knowledgeOrg) byID(id domain.KnowledgeSourceID) *fakeSource {
	for _, s := range o.sources {
		if s.src.ID == id {
			return s
		}
	}

	return nil
}

// FinishSync implements ports.Knowledge.
func (k *Knowledge) FinishSync(ctx context.Context, l domain.SourceLease, r domain.SyncResult) error {
	return k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, err := o.leased(l)
		if err != nil {
			return err
		}

		at := r.At.UTC()
		s.src.LeaseUntil, s.token = nil, ""

		if r.Error == "" {
			s.src.Status, s.src.LastError, s.src.LastSyncedAt, s.src.SyncFailures = domain.SyncIdle, "", &at, 0
		} else {
			s.src.Status, s.src.LastError = domain.SyncFailed, truncateRunes(r.Error, domain.MaxKnowledgeError)
			s.src.SyncFailures++
		}

		switch {
		case s.src.NextSyncAt == nil || !s.src.NextSyncAt.Equal(r.Due):
			// Made on demand, or due again, since the claim: that stands.
		case s.src.SyncEvery == domain.SyncOnDemand:
			s.src.NextSyncAt = nil
		default:
			next := at.Add(s.src.SyncEvery)
			s.src.NextSyncAt = &next
		}

		s.src.UpdatedAt = time.Now().UTC()

		return nil
	})
}

// candidate is a chunk a search may rank.
func sourceEntry(by domain.Actor, action string, id domain.KnowledgeSourceID, data map[string]any) domain.AuditEntry {
	return domain.AuditEntry{Actor: by, Action: action, TargetKind: domain.AuditTargetKnowledgeSource, TargetID: string(id), Data: data}
}

// newV7 is a time-ordered id, as the postgres adapter's.
func newV7() string { return uuid.Must(uuid.NewV7()).String() }

// truncateRunes is s, valid UTF-8 as the database keeps it, cut to at most n characters.
func truncateRunes(s string, n int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}

	return s
}

// holds reports whether an upload of o, other than those of the source without and the file
// named except in the source with that id, has the content b.
func (o *knowledgeOrg) holds(b domain.BlobHash, without domain.KnowledgeSourceID, except string) bool {
	for _, s := range o.sources {
		if s.src.ID == without {
			continue
		}

		for name, u := range s.uploads {
			if u.Blob == b && name != except {
				return true
			}
		}
	}

	return false
}

// upload is the upload source named name, locked by the caller.
func (o *knowledgeOrg) upload(name string) (*fakeSource, error) {
	s, ok := o.sources[name]
	if !ok {
		return nil, fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
	}

	if s.src.Kind != domain.SourceUpload {
		return nil, fmt.Errorf("knowledge source %q: %w", name, domain.ErrNotUploadSource)
	}

	return s, nil
}

// heldElsewhere reports whether another upload of o than the file named name of s has content b.
func (o *knowledgeOrg) heldElsewhere(b domain.BlobHash, s *fakeSource, name string) bool {
	for n, u := range s.uploads {
		if u.Blob == b && n != name {
			return true
		}
	}

	return o.holds(b, s.src.ID, "")
}

// AddUpload implements ports.Knowledge.
func (k *Knowledge) AddUpload(ctx context.Context, source string, u domain.KnowledgeUpload, by domain.Actor, drop ports.DropBlobs) (domain.KnowledgeUpload, error) {
	if !u.Blob.Valid() {
		return domain.KnowledgeUpload{}, fmt.Errorf("upload %q: its content hash %q: %w", u.Name, u.Blob, domain.ErrInvalid)
	}

	var out domain.KnowledgeUpload

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, err := o.upload(source)
		if err != nil {
			return err
		}

		if existing, ok := s.uploads[u.Name]; ok && existing.Blob == u.Blob {
			out = existing

			return nil
		}

		if old, ok := s.uploads[u.Name]; ok && !o.heldElsewhere(old.Blob, s, u.Name) {
			if err := dropped(ctx, drop, []domain.BlobHash{old.Blob}); err != nil {
				return err
			}
		}

		f := domain.KnowledgeUpload{
			ID: domain.KnowledgeUploadID(newV7()), Source: s.src.ID, Name: u.Name, MIME: u.MIME, Bytes: u.Bytes, Blob: u.Blob,
			CreatedBy: by, CreatedAt: time.Now().UTC(),
		}

		if err := k.audit.Append(ctx, domain.AuditEntry{
			Actor: by, Action: domain.AuditKnowledgeDocumentUploaded, TargetKind: domain.AuditTargetKnowledgeDocument,
			TargetID: string(f.ID), Data: map[string]any{"source": source, "title": u.Name, "bytes": u.Bytes},
		}); err != nil {
			return err
		}

		if s.uploads == nil {
			s.uploads = map[string]domain.KnowledgeUpload{}
		}

		s.uploads[u.Name] = f
		out = f

		return nil
	})

	return out, err
}

// DeleteUpload implements ports.Knowledge; drop runs before anything changes, so its error
// changes nothing.
func (k *Knowledge) DeleteUpload(ctx context.Context, source, name string, by domain.Actor, drop ports.DropBlobs) error {
	return k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, err := o.upload(source)
		if err != nil {
			return err
		}

		u, ok := s.uploads[name]
		if !ok {
			return fmt.Errorf("knowledge source %q has no upload %q: %w", source, name, ports.ErrNotFound)
		}

		if !o.heldElsewhere(u.Blob, s, name) {
			if err := dropped(ctx, drop, []domain.BlobHash{u.Blob}); err != nil {
				return err
			}
		}

		if err := k.audit.Append(ctx, domain.AuditEntry{
			Actor: by, Action: domain.AuditKnowledgeDocumentDeleted, TargetKind: domain.AuditTargetKnowledgeDocument,
			TargetID: string(u.ID), Data: map[string]any{"source": source, "title": name},
		}); err != nil {
			return err
		}

		delete(s.uploads, name)

		return nil
	})
}

// Uploads implements ports.Knowledge.
func (k *Knowledge) Uploads(ctx context.Context, source string) ([]domain.KnowledgeUpload, error) {
	var out []domain.KnowledgeUpload

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, err := o.upload(source)
		if err != nil {
			return err
		}

		for _, name := range slices.Sorted(maps.Keys(s.uploads)) {
			out = append(out, s.uploads[name])
		}

		return nil
	})

	return out, err
}
