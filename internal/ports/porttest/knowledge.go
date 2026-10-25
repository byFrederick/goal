package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Knowledge = (*Knowledge)(nil)

// auditKind is the audit data key of a source's kind.
const auditKind = "kind"

// Knowledge keeps knowledge bases in memory with the database's rules: names unique per
// organization, sources reading their organization's connections (a synced one only through a
// kind that syncs; a connection a source reads cannot be deleted), leases claimed by time, an
// audit entry for every change people make, and the same search: the nearest chunks of the
// query's model by cosine distance and the best by keywords (any of the query's words, each
// occurrence counting), the 50 best of each over the named sources only, fused by
// domain.FuseRanks.
type Knowledge struct {
	conns *Connections
	audit ports.Audit

	mu      sync.Mutex
	orgs    map[domain.OrgID]*knowledgeOrg
	chunkID int64
}

type knowledgeOrg struct {
	sources map[string]*fakeSource // by name
	docs    map[domain.KnowledgeDocumentID]*fakeDocument
}

type fakeSource struct {
	src    domain.KnowledgeSource
	connID domain.ConnectionID
	token  string // the lease's, "" when none holds it
}

type fakeDocument struct {
	doc    domain.KnowledgeDocument
	chunks []fakeChunk
}

type fakeChunk struct {
	id    int64
	chunk domain.Chunk
	model string
}

// NewKnowledge returns an empty knowledge base whose sources read conns' connections, audited
// through audit (a Tenancy that knows the organizations).
func NewKnowledge(conns *Connections, audit ports.Audit) *Knowledge {
	k := &Knowledge{conns: conns, audit: audit, orgs: map[domain.OrgID]*knowledgeOrg{}}

	conns.d.mu.Lock()
	conns.d.readBy = k.readsConnection
	conns.d.mu.Unlock()

	return k
}

// readsConnection reports whether one of org's sources reads the connection id.
func (k *Knowledge) readsConnection(org domain.OrgID, id domain.ConnectionID) bool {
	k.mu.Lock()
	defer k.mu.Unlock()

	for _, s := range k.org(org).sources {
		if s.connID == id {
			return true
		}
	}

	return false
}

// org is org's knowledge base, made on first use. k.mu is held.
func (k *Knowledge) org(org domain.OrgID) *knowledgeOrg {
	o, ok := k.orgs[org]
	if !ok {
		o = &knowledgeOrg{sources: map[string]*fakeSource{}, docs: map[domain.KnowledgeDocumentID]*fakeDocument{}}
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

// view is s as it is read: a copy with its document count.
func (o *knowledgeOrg) view(s *fakeSource) domain.KnowledgeSource {
	out := s.src
	out.Config = maps.Clone(s.src.Config)

	for _, d := range o.docs {
		if d.doc.Source == s.src.ID {
			out.Documents++
		}
	}

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

	connID, err := k.connection(ctx, n)
	if err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("create knowledge source: %w", err)
	}

	var out domain.KnowledgeSource

	err = k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		if _, taken := o.sources[n.Name]; taken {
			return fmt.Errorf("knowledge source %q: %w", n.Name, ports.ErrNameTaken)
		}

		now := time.Now().UTC()
		s := domain.KnowledgeSource{
			ID: domain.KnowledgeSourceID(newV7()), Name: n.Name, Title: n.Title, Kind: n.Kind, Connection: n.Connection,
			Config: maps.Clone(n.Config), SyncEvery: cmp.Or(n.SyncEvery, domain.DefaultSyncEvery), Status: domain.SyncIdle,
			CreatedBy: by, CreatedAt: now, UpdatedAt: now,
		}

		if s.Config == nil {
			s.Config = map[string]string{}
		}

		if n.Kind != domain.SourceLive {
			s.NextSyncAt = &now
		}

		if err := k.audit.Append(ctx, sourceEntry(by, domain.AuditKnowledgeSourceCreated, s.ID,
			map[string]any{auditName: n.Name, auditKind: string(n.Kind), "connection": n.Connection})); err != nil {
			return err
		}

		o.sources[n.Name] = &fakeSource{src: s, connID: connID}
		out = o.view(o.sources[n.Name])

		return nil
	})
	if err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("create knowledge source: %w", err)
	}

	return out, nil
}

// connection is the connection a new source reads ("" for uploads), checked as the database does.
func (k *Knowledge) connection(ctx context.Context, n domain.NewKnowledgeSource) (domain.ConnectionID, error) {
	if n.Kind == domain.SourceUpload {
		return "", nil
	}

	c, err := k.conns.Get(ctx, n.Connection)
	if err != nil {
		return "", err
	}

	kind, err := k.conns.Kind(ctx, c.Kind)
	if err != nil {
		return "", err
	}

	if n.Kind == domain.SourceSynced && kind.Spec.Sync == "" {
		return "", fmt.Errorf("connection %q: %w", n.Connection, domain.ErrConnectionCannotSync)
	}

	return c.ID, nil
}

// UpdateSource implements ports.Knowledge.
func (k *Knowledge) UpdateSource(ctx context.Context, name string, p domain.KnowledgeSourcePatch, by domain.Actor) (domain.KnowledgeSource, error) {
	if err := p.Check(); err != nil {
		return domain.KnowledgeSource{}, fmt.Errorf("update knowledge source: %w", err)
	}

	var out domain.KnowledgeSource

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[name]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
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
		}

		s.src.UpdatedAt = time.Now().UTC()
		out = o.view(s)

		return nil
	})

	return out, err
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

		for _, d := range o.docs {
			if d.doc.Source == s.src.ID && d.doc.Blob != "" {
				blobs = append(blobs, d.doc.Blob)
			}
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

		for id, d := range o.docs {
			if d.doc.Source == s.src.ID {
				delete(o.docs, id)
			}
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

// holds reports whether a document of o, other than those of the source without and the
// document except, has the content b.
func (o *knowledgeOrg) holds(b domain.BlobHash, without domain.KnowledgeSourceID, except domain.KnowledgeDocumentID) bool {
	for id, d := range o.docs {
		if d.doc.Blob == b && d.doc.Source != without && id != except {
			return true
		}
	}

	return false
}

// SyncNow implements ports.Knowledge.
func (k *Knowledge) SyncNow(ctx context.Context, name string) error {
	return k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[name]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", name, ports.ErrNotFound)
		}

		if s.src.Kind == domain.SourceLive {
			return fmt.Errorf("knowledge source %q is live and never syncs: %w", name, domain.ErrInvalid)
		}

		now := time.Now().UTC()
		s.src.NextSyncAt = &now

		return nil
	})
}

// ClaimSource implements ports.Knowledge: the source due first of every organization's.
func (k *Knowledge) ClaimSource(_ context.Context, now, until time.Time, model string) (*domain.SourceClaim, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	var (
		best    *fakeSource
		bestOrg domain.OrgID
	)

	for org, o := range k.orgs {
		for _, s := range o.sources {
			if !claimable(s.src, now, model) {
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

// claimable is the claim's rule: not live, its lease ended, and due or of another model.
func claimable(s domain.KnowledgeSource, now time.Time, model string) bool {
	if s.Kind == domain.SourceLive || (s.LeaseUntil != nil && s.LeaseUntil.After(now)) {
		return false
	}

	return !s.NextSyncAt.After(now) || (model != "" && s.EmbedModel != model && s.Status != domain.SyncFailed)
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
			s.src.Status, s.src.LastError, s.src.LastSyncedAt, s.src.EmbedModel = domain.SyncIdle, "", &at, r.EmbedModel
		} else {
			s.src.Status, s.src.LastError = domain.SyncFailed, truncateRunes(r.Error, domain.MaxKnowledgeError)
		}

		if s.src.NextSyncAt.Equal(r.Due) {
			next := at.Add(s.src.SyncEvery)
			s.src.NextSyncAt = &next
		}

		s.src.UpdatedAt = time.Now().UTC()

		return nil
	})
}

// Versions implements ports.Knowledge.
func (k *Knowledge) Versions(ctx context.Context, source domain.KnowledgeSourceID) (map[string]string, error) {
	out := map[string]string{}

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		for _, d := range o.docs {
			if d.doc.Source == source && d.doc.Status != domain.DocumentPending {
				out[d.doc.ExternalID] = d.doc.Version
			}
		}

		return nil
	})

	return out, err
}

// PutDocument implements ports.Knowledge.
func (k *Knowledge) PutDocument(ctx context.Context, d domain.KnowledgeDocument, chunks []domain.Chunk) error {
	if d.ExternalID == "" || len(d.ExternalID) > domain.MaxExternalID {
		return fmt.Errorf("put knowledge document: its external id has %d bytes: %w", len(d.ExternalID), domain.ErrInvalid)
	}

	for _, c := range chunks {
		if c.Embedding != nil && (d.EmbedModel == "" || len(c.Embedding) != domain.EmbeddingDimensions) {
			return fmt.Errorf("put knowledge document %q: chunk %d's embedding: %w", d.ExternalID, c.Ord, domain.ErrInvalid)
		}

		if len([]rune(c.Text)) > domain.MaxChunkText {
			return fmt.Errorf("put knowledge document %q: chunk %d is too long: %w", d.ExternalID, c.Ord, domain.ErrInvalid)
		}
	}

	return k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		if o.byID(d.Source) == nil {
			return fmt.Errorf("knowledge source %s: %w", d.Source, ports.ErrNotFound)
		}

		existing := o.document(d.Source, d.ExternalID)
		if existing == nil {
			d.ID = domain.KnowledgeDocumentID(newV7())
			existing = &fakeDocument{}
			o.docs[d.ID] = existing
		} else {
			d.ID, d.CreatedBy = existing.doc.ID, existing.doc.CreatedBy
			d.Blob = cmp.Or(d.Blob, existing.doc.Blob)
		}

		d.Error = truncateRunes(d.Error, domain.MaxKnowledgeError)
		d.SeenAt = later(existing.doc.SeenAt, d.SeenAt.UTC())
		existing.doc, existing.chunks = d, nil

		for _, c := range chunks {
			k.chunkID++
			c.Embedding = halve(c.Embedding)
			existing.chunks = append(existing.chunks, fakeChunk{id: k.chunkID, chunk: c, model: modelOf(c, d.EmbedModel)})
		}

		return nil
	})
}

func modelOf(c domain.Chunk, model string) string {
	if c.Embedding == nil {
		return ""
	}

	return model
}

// halve rounds v as the database's halfvec keeps it: to the nearest float16.
func halve(v []float32) []float32 {
	if v == nil {
		return nil
	}

	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = toFloat16(x)
	}

	return out
}

// toFloat16 rounds x to the nearest value a float16 holds (normal range; tiny values to zero).
func toFloat16(x float32) float32 {
	if x == 0 || math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
		return x
	}

	frac, exp := math.Frexp(float64(x))
	if exp < -13 {
		return 0
	}

	const mantissa = 1 << 11

	return float32(math.Ldexp(math.Round(frac*mantissa)/mantissa, exp))
}

// document is the document of source with external id ext, or nil.
func (o *knowledgeOrg) document(source domain.KnowledgeSourceID, ext string) *fakeDocument {
	for _, d := range o.docs {
		if d.doc.Source == source && d.doc.ExternalID == ext {
			return d
		}
	}

	return nil
}

// later is the later of a and b.
func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}

// MarkSeen implements ports.Knowledge; no external ids is nothing to do.
func (k *Knowledge) MarkSeen(ctx context.Context, l domain.SourceLease, externalIDs []string, at time.Time) error {
	if len(externalIDs) == 0 {
		return nil
	}

	return k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		if _, err := o.leased(l); err != nil {
			return err
		}

		for _, d := range o.docs {
			if d.doc.Source == l.Source && slices.Contains(externalIDs, d.doc.ExternalID) {
				d.doc.SeenAt = later(d.doc.SeenAt, at.UTC())
			}
		}

		return nil
	})
}

// DeleteUnseen implements ports.Knowledge.
func (k *Knowledge) DeleteUnseen(ctx context.Context, l domain.SourceLease, before time.Time) (int, error) {
	n := 0

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		if _, err := o.leased(l); err != nil {
			return err
		}

		for id, d := range o.docs {
			if d.doc.Source == l.Source && d.doc.SeenAt.Before(before) {
				delete(o.docs, id)

				n++
			}
		}

		return nil
	})

	return n, err
}

// AddUpload implements ports.Knowledge.
func (k *Knowledge) AddUpload(ctx context.Context, source string, d domain.KnowledgeDocument, by domain.Actor) (domain.KnowledgeDocument, error) {
	if !d.Blob.Valid() {
		return domain.KnowledgeDocument{}, fmt.Errorf("upload %q: its content hash %q: %w", d.Title, d.Blob, domain.ErrInvalid)
	}

	var out domain.KnowledgeDocument

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[source]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", source, ports.ErrNotFound)
		}

		if s.src.Kind != domain.SourceUpload {
			return fmt.Errorf("knowledge source %q: %w", source, domain.ErrNotUploadSource)
		}

		if existing := o.document(s.src.ID, string(d.Blob)); existing != nil {
			out = existing.doc

			return nil
		}

		now := time.Now().UTC()
		doc := domain.KnowledgeDocument{
			ID: domain.KnowledgeDocumentID(newV7()), Source: s.src.ID, ExternalID: string(d.Blob), Version: string(d.Blob),
			Title: d.Title, MIME: d.MIME, Bytes: d.Bytes, Blob: d.Blob, Status: domain.DocumentPending, SeenAt: now, CreatedBy: by,
		}

		if err := k.audit.Append(ctx, domain.AuditEntry{
			Actor: by, Action: domain.AuditKnowledgeDocumentUploaded, TargetKind: domain.AuditTargetKnowledgeDocument,
			TargetID: string(doc.ID), Data: map[string]any{"source": source, "title": d.Title, "bytes": d.Bytes},
		}); err != nil {
			return err
		}

		o.docs[doc.ID] = &fakeDocument{doc: doc}
		out = doc

		return nil
	})

	return out, err
}

// DeleteDocument implements ports.Knowledge; drop runs before anything changes, so its error
// changes nothing.
func (k *Knowledge) DeleteDocument(ctx context.Context, id domain.KnowledgeDocumentID, by domain.Actor, drop ports.DropBlobs) error {
	return k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		d, ok := o.docs[id]
		if !ok {
			return fmt.Errorf("knowledge document %s: %w", id, ports.ErrNotFound)
		}

		src := o.byID(d.doc.Source)
		if src.src.Kind != domain.SourceUpload {
			return fmt.Errorf("knowledge document %s: %w", id, domain.ErrNotUploadSource)
		}

		if d.doc.Blob != "" && !o.holds(d.doc.Blob, "", id) {
			if err := dropped(ctx, drop, []domain.BlobHash{d.doc.Blob}); err != nil {
				return err
			}
		}

		if err := k.audit.Append(ctx, domain.AuditEntry{
			Actor: by, Action: domain.AuditKnowledgeDocumentDeleted, TargetKind: domain.AuditTargetKnowledgeDocument,
			TargetID: string(id), Data: map[string]any{"source": src.src.Name, "title": d.doc.Title},
		}); err != nil {
			return err
		}

		delete(o.docs, id)

		return nil
	})
}

// Documents implements ports.Knowledge.
func (k *Knowledge) Documents(ctx context.Context, source string, after domain.KnowledgeDocumentID, limit int) ([]domain.KnowledgeDocument, error) {
	var out []domain.KnowledgeDocument

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[source]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", source, ports.ErrNotFound)
		}

		for _, id := range slices.Sorted(maps.Keys(o.docs)) {
			if d := o.docs[id]; d.doc.Source == s.src.ID && (after == "" || id > after) && len(out) < limit {
				out = append(out, d.doc)
			}
		}

		return nil
	})

	return out, err
}

// Document implements ports.Knowledge.
func (k *Knowledge) Document(ctx context.Context, id domain.KnowledgeDocumentID) (domain.KnowledgeDocument, string, error) {
	var (
		out  domain.KnowledgeDocument
		text string
	)

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		d, ok := o.docs[id]
		if !ok {
			return fmt.Errorf("knowledge document %s: %w", id, ports.ErrNotFound)
		}

		texts := make([]string, 0, len(d.chunks))
		for _, c := range d.chunks {
			texts = append(texts, c.chunk.Text)
		}

		out, text = d.doc, strings.Join(texts, "\n\n")

		return nil
	})

	return out, text, err
}

// Search implements ports.Knowledge.
func (k *Knowledge) Search(ctx context.Context, q domain.KnowledgeQuery) ([]domain.KnowledgeHit, error) {
	if len(q.Sources) == 0 || q.Limit <= 0 {
		return nil, nil
	}

	if q.Embedding != nil && (q.Model == "" || len(q.Embedding) != domain.EmbeddingDimensions) {
		return nil, fmt.Errorf("search knowledge: the query's embedding: %w", domain.ErrInvalid)
	}

	var out []domain.KnowledgeHit

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		hits := o.candidates(q.Sources)
		fused := domain.FuseRanks(nearest(hits, q), byWords(hits, q.Text))

		for _, f := range fused[:min(len(fused), q.Limit, domain.SearchCandidates)] {
			h := hits[f.Chunk]
			h.Score = f.Score
			out = append(out, h.KnowledgeHit)
		}

		return nil
	})

	return out, err
}

// candidate is a chunk a search may rank.
type candidate struct {
	domain.KnowledgeHit

	embedding []float32
	model     string
}

// candidates are the chunks of the named sources, by id.
func (o *knowledgeOrg) candidates(sources []string) map[int64]candidate {
	ids := map[domain.KnowledgeSourceID]string{}

	for _, name := range sources {
		if s, ok := o.sources[name]; ok {
			ids[s.src.ID] = name
		}
	}

	out := map[int64]candidate{}

	for _, d := range o.docs {
		name, ok := ids[d.doc.Source]
		if !ok {
			continue
		}

		for _, c := range d.chunks {
			out[c.id] = candidate{
				KnowledgeHit: domain.KnowledgeHit{
					Chunk: c.id, Document: d.doc.ID, Source: name, Title: d.doc.Title, URL: d.doc.URL, Heading: c.chunk.Heading, Text: c.chunk.Text,
				},
				embedding: c.chunk.Embedding, model: c.model,
			}
		}
	}

	return out
}

// nearest ranks the chunks of q.Model by cosine distance to q.Embedding, at most
// domain.SearchCandidates.
func nearest(hits map[int64]candidate, q domain.KnowledgeQuery) []int64 {
	if q.Embedding == nil {
		return nil
	}

	type scored struct {
		id int64
		d  float64
	}

	var all []scored

	query := halve(q.Embedding)

	for id, h := range hits {
		if h.embedding != nil && h.model == q.Model {
			all = append(all, scored{id, cosineDistance(query, h.embedding)})
		}
	}

	slices.SortFunc(all, func(a, b scored) int { return cmp.Or(cmp.Compare(a.d, b.d), cmp.Compare(a.id, b.id)) })

	out := make([]int64, 0, min(len(all), domain.SearchCandidates))
	for _, s := range all[:min(len(all), domain.SearchCandidates)] {
		out = append(out, s.id)
	}

	return out
}

func cosineDistance(a, b []float32) float64 {
	var dot, na, nb float64

	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}

	if na == 0 || nb == 0 {
		return math.NaN()
	}

	return 1 - dot/math.Sqrt(na*nb)
}

// byWords ranks the chunks holding any of text's words by how often they hold them over 1 + the
// log of their length in words (ts_rank's normalization 1), at most domain.SearchCandidates.
func byWords(hits map[int64]candidate, text string) []int64 {
	words := tokens(text)
	if len(words) == 0 {
		return nil
	}

	type scored struct {
		id   int64
		rank float64
	}

	var all []scored

	for id, h := range hits {
		n, chunk := 0, tokens(h.Heading+" "+h.Text)

		for _, t := range chunk {
			if slices.Contains(words, t) {
				n++
			}
		}

		if n > 0 {
			all = append(all, scored{id, float64(n) / (1 + math.Log(float64(len(chunk))))})
		}
	}

	slices.SortFunc(all, func(a, b scored) int { return cmp.Or(cmp.Compare(b.rank, a.rank), cmp.Compare(a.id, b.id)) })

	out := make([]int64, 0, min(len(all), domain.SearchCandidates))
	for _, s := range all[:min(len(all), domain.SearchCandidates)] {
		out = append(out, s.id)
	}

	return out
}

// tokens are s's words lowercased, as PostgreSQL's simple configuration splits them.
func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func sourceEntry(by domain.Actor, action string, id domain.KnowledgeSourceID, data map[string]any) domain.AuditEntry {
	return domain.AuditEntry{Actor: by, Action: action, TargetKind: domain.AuditTargetKnowledgeSource, TargetID: string(id), Data: data}
}

// newV7 is a time-ordered id, as the postgres adapter's.
func newV7() string { return uuid.Must(uuid.NewV7()).String() }

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}

	return s
}
