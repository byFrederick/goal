// Package knowledge indexes and searches the organizations' knowledge bases (spec 4 §4.8): the
// ingestion loop that syncs due sources under leases (Ingestor), the text extraction and the
// chunking it uses, uploads kept in the bucket (Uploads) and search with or without an embedding
// model (Searcher). It reaches the database, the bucket, the embedding model and the sources
// only through ports.
package knowledge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Orgs lists the organizations whose sources sync; ports.Tenancy satisfies it.
type Orgs interface {
	ActiveOrgs(ctx context.Context) ([]domain.OrgID, error)
}

// Connections opens a synced source's connection; ports.Connections satisfies it.
type Connections interface {
	Resolve(ctx context.Context, names []string, use domain.ConnectionUse) ([]domain.ResolvedConnection, error)
}

// Defaults of the ingestion loop.
const (
	// DefaultWorkers is how many sources one goald syncs at once (GOAL_KB_WORKERS).
	DefaultWorkers = 2
	// Lease is how long a claim holds a source; it is renewed every third of it while a sync runs.
	Lease = 10 * time.Minute
	// Tick is how often an idle worker looks for a due source.
	Tick = 15 * time.Second
	// embedBatch is how many texts one embedding request carries at most, and embedBatchTokens
	// how many tokens by estimateTokens: models bound a request's tokens (Workers AI's bge-m3 takes
	// 60,000 in all).
	embedBatch       = 64
	embedBatchTokens = 40_000
)

// IngestorConfig wires an Ingestor. Embedder nil indexes keywords only; Readers are the source
// readers of synced connections by their kind's sync name (KindSpec.Sync); Uploads reads upload
// sources.
type IngestorConfig struct {
	Knowledge   ports.Knowledge
	Orgs        Orgs
	Connections Connections
	Uploads     ports.SourceReader
	Readers     map[string]ports.SourceReader
	Embedder    ports.Embedder
	Clock       ports.Clock
	Log         *slog.Logger
	// Workers, Lease and Tick default to DefaultWorkers, Lease and Tick.
	Workers int
	Lease   time.Duration
	Tick    time.Duration
}

// Ingestor syncs the sources that are due, in every active organization: it claims one with a
// lease (renewed while it works), lists its documents, fetches, extracts, chunks and embeds the
// new and changed ones, replaces their chunks, deletes those its source no longer lists and
// schedules the next sync. Any goald runs one; a goald that dies mid-sync leaves its source to
// the next claim once the lease ends, and every step is safe to repeat.
type Ingestor struct {
	cfg IngestorConfig
}

// NewIngestor returns an ingestor for cfg.
func NewIngestor(cfg IngestorConfig) *Ingestor {
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultWorkers
	}

	if cfg.Lease <= 0 {
		cfg.Lease = Lease
	}

	if cfg.Tick <= 0 {
		cfg.Tick = Tick
	}

	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}

	return &Ingestor{cfg: cfg}
}

// Run syncs due sources with cfg.Workers workers until ctx ends; each looks every cfg.Tick.
func (in *Ingestor) Run(ctx context.Context) {
	var wg sync.WaitGroup

	for range in.cfg.Workers {
		wg.Go(func() {
			tick := in.cfg.Clock.NewTicker(in.cfg.Tick)
			defer tick.Stop()

			for {
				if _, err := in.SyncDue(ctx); err != nil && ctx.Err() == nil {
					in.cfg.Log.WarnContext(ctx, "knowledge sync", slog.Any("error", err))
				}

				select {
				case <-ctx.Done():
					return
				case <-tick.C():
				}
			}
		})
	}

	wg.Wait()
}

// SyncDue syncs, one after the other, every source due in any active organization that no
// other worker holds, and returns how many it synced (failed syncs included). A source's failure
// is recorded on it, not returned; the error is the listing's or the database's.
func (in *Ingestor) SyncDue(ctx context.Context) (int, error) {
	orgs, err := in.cfg.Orgs.ActiveOrgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("knowledge: organizations: %w", err)
	}

	n := 0

	var errs []error

	for _, org := range orgs {
		octx := domain.WithOrg(ctx, org)

		for ctx.Err() == nil {
			now := in.cfg.Clock.Now()

			src, err := in.cfg.Knowledge.ClaimSource(octx, now, now.Add(in.cfg.Lease), in.model())
			if err != nil {
				errs = append(errs, fmt.Errorf("knowledge: claim in %s: %w", org, err))

				break
			}

			if src == nil {
				break
			}

			n++

			if err := in.sync(octx, *src); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return n, errors.Join(errs...)
}

// model is the embedding model, "" without one.
func (in *Ingestor) model() string {
	if in.cfg.Embedder == nil {
		return ""
	}

	return in.cfg.Embedder.Model()
}

// sync runs one claimed source's sync, renewing its lease, and records how it ended. A sync cut
// short (goald stopping, the lease lost) records nothing: the lease ends and the next claim
// takes the source over.
func (in *Ingestor) sync(ctx context.Context, src domain.KnowledgeSource) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	renewed := make(chan struct{})

	go func() {
		defer close(renewed)

		in.renew(ctx, cancel, src.ID)
	}()

	started := in.cfg.Clock.Now()
	stats, err := in.pass(ctx, src, started)

	lost := context.Cause(ctx)

	cancel(nil)
	<-renewed

	if lost != nil {
		in.cfg.Log.WarnContext(ctx, "knowledge sync cut short", slog.String("source", src.Name), slog.Any("cause", lost))

		return nil
	}

	r := domain.SyncResult{At: in.cfg.Clock.Now(), Due: *src.NextSyncAt}
	if err != nil {
		r.Error = err.Error()
	} else {
		r.EmbedModel = in.model()
	}

	in.cfg.Log.InfoContext(ctx, "knowledge sync", slog.String("source", src.Name), slog.String("source_id", string(src.ID)),
		slog.Int("listed", stats.listed), slog.Int("fetched", stats.fetched), slog.Int("deleted", stats.deleted),
		slog.Int("chunks", stats.chunks), slog.Float64("seconds", r.At.Sub(started).Seconds()), slog.String("error", r.Error))

	if err := in.cfg.Knowledge.FinishSync(context.WithoutCancel(ctx), src.ID, r); err != nil {
		return fmt.Errorf("knowledge: finish %s: %w", src.Name, err)
	}

	return nil
}

// errLeaseLost cuts a sync short when its lease could not be renewed.
var errLeaseLost = errors.New("the lease on the source was lost")

// renew extends the lease every third of it until ctx ends; a renewal that fails cuts the sync
// short.
func (in *Ingestor) renew(ctx context.Context, cancel context.CancelCauseFunc, id domain.KnowledgeSourceID) {
	tick := in.cfg.Clock.NewTicker(in.cfg.Lease / 3)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C():
			now := in.cfg.Clock.Now()
			if err := in.cfg.Knowledge.RenewSource(ctx, id, now, now.Add(in.cfg.Lease)); err != nil && ctx.Err() == nil {
				cancel(fmt.Errorf("%w: %w", errLeaseLost, err))

				return
			}
		}
	}
}

type syncStats struct {
	listed, fetched, deleted, chunks int
}

// pass is one sync of src: every listed document whose version changed (all of them when its
// chunks are of another model) indexed, the others marked seen, and for a synced source the ones
// it no longer lists deleted.
func (in *Ingestor) pass(ctx context.Context, src domain.KnowledgeSource, started time.Time) (syncStats, error) {
	var stats syncStats

	spec, reader, err := in.reader(ctx, src)
	if err != nil {
		return stats, err
	}

	versions := map[string]string{}

	if model := in.model(); model == "" || src.EmbedModel == model {
		if versions, err = in.cfg.Knowledge.Versions(ctx, src.ID); err != nil {
			return stats, err
		}
	}

	var unchanged []string

	for item, err := range reader.List(ctx, spec) {
		if err != nil {
			return stats, fmt.Errorf("list: %w", err)
		}

		stats.listed++

		if v, ok := versions[item.ID]; ok && v == item.Version {
			unchanged = append(unchanged, item.ID)

			continue
		}

		n, err := in.index(ctx, spec, reader, item, started)
		if err != nil {
			return stats, err
		}

		stats.fetched++
		stats.chunks += n
	}

	if err := in.cfg.Knowledge.MarkSeen(ctx, src.ID, unchanged, started); err != nil {
		return stats, err
	}

	if src.Kind == domain.SourceSynced {
		if stats.deleted, err = in.cfg.Knowledge.DeleteUnseen(ctx, src.ID, started); err != nil {
			return stats, err
		}
	}

	return stats, nil
}

// reader is what reads src: the uploads, or its connection's kind's reader with the connection
// opened.
func (in *Ingestor) reader(ctx context.Context, src domain.KnowledgeSource) (domain.SyncSpec, ports.SourceReader, error) {
	spec := domain.SyncSpec{Source: src}

	if src.Kind == domain.SourceUpload {
		return spec, in.cfg.Uploads, nil
	}

	conns, err := in.cfg.Connections.Resolve(ctx, []string{src.Connection}, domain.ConnectionUse{Source: src.Name})
	if err != nil {
		return spec, nil, fmt.Errorf("connection %s: %w", src.Connection, err)
	}

	spec.Connection = &conns[0]

	sync := conns[0].Kind.Spec.Sync

	reader, ok := in.cfg.Readers[sync]
	if !ok {
		return spec, nil, fmt.Errorf("connection %s's kind syncs with %q, which this goald cannot read", src.Connection, sync)
	}

	return spec, reader, nil
}

// index fetches, extracts, chunks and embeds one document and stores it, and returns how many
// chunks it has. A document that cannot be fetched or read is stored failed with the reason (one
// that could not be fetched without a version, so the next sync tries again); only the embedding
// model failing, the database or ctx ending stop the sync.
func (in *Ingestor) index(ctx context.Context, spec domain.SyncSpec, reader ports.SourceReader, item domain.SourceItem, seen time.Time) (int, error) {
	d := domain.KnowledgeDocument{
		Source: spec.Source.ID, ExternalID: item.ID, Version: item.Version, Title: item.Title, URL: item.URL, MIME: item.MIME,
		SeenAt: seen, CreatedBy: domain.EngineActor,
	}

	pieces, err := in.read(ctx, spec, reader, &d)
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	switch {
	case err != nil:
		d.Status, d.Error = domain.DocumentFailed, err.Error()
	case len(pieces) == 0:
		d.Status = domain.DocumentSkipped
	default:
		d.Status = domain.DocumentIndexed
	}

	chunks, err := in.embed(ctx, pieces)
	if err != nil {
		return 0, err
	}

	if d.Status == domain.DocumentIndexed {
		now := in.cfg.Clock.Now()
		d.IndexedAt, d.EmbedModel = &now, in.model()
	}

	if err := in.cfg.Knowledge.PutDocument(ctx, d, chunks); err != nil {
		return 0, err
	}

	return len(chunks), nil
}

// read fetches d's content and cuts its text into pieces, filling in what the fetch tells.
func (in *Ingestor) read(ctx context.Context, spec domain.SyncSpec, reader ports.SourceReader, d *domain.KnowledgeDocument) ([]Piece, error) {
	doc, err := reader.Fetch(ctx, spec, d.ExternalID)
	if err != nil {
		d.Version = ""

		return nil, fmt.Errorf("fetch: %w", err)
	}

	if doc.MIME != "" {
		d.MIME = doc.MIME
	}

	if doc.Title != "" {
		d.Title = doc.Title
	}

	if doc.URL != "" {
		d.URL = doc.URL
	}

	d.Bytes = int64(len(doc.Body))

	text, err := Extract(d.MIME, doc.Body)
	if err != nil {
		return nil, err
	}

	return Chunk(text), nil
}

// batchEnd is where the embedding request of texts from start ends: at most embedBatch texts
// and, as servers pad a batch to its longest text, at most embedBatchTokens estimated tokens for
// that many texts of the longest's length; one text at least.
func batchEnd(texts []string, start int) int {
	end, longest := start, 0

	for end < len(texts) && end-start < embedBatch {
		longest = max(longest, estimateTokens(texts[end]))
		if end > start && (end-start+1)*longest > embedBatchTokens {
			break
		}

		end++
	}

	return end
}

// estimateTokens is a cautious count of the tokens in s: English and code run at about three
// ASCII characters a token, other scripts and symbols (box drawing, CJK) at up to two tokens a
// character.
func estimateTokens(s string) int {
	ascii, other := 0, 0

	for _, r := range s {
		if r < utf8.RuneSelf {
			ascii++
		} else {
			other++
		}
	}

	return ascii/3 + 2*other + 1
}

// embed turns pieces into chunks, with embeddings when there is a model, in batches.
func (in *Ingestor) embed(ctx context.Context, pieces []Piece) ([]domain.Chunk, error) {
	chunks := make([]domain.Chunk, len(pieces))
	for i, p := range pieces {
		chunks[i] = domain.Chunk{Ord: i, Heading: p.Heading, Text: p.Text}
	}

	if in.cfg.Embedder == nil {
		return chunks, nil
	}

	texts := make([]string, len(pieces))
	for i, p := range pieces {
		texts[i] = p.EmbedText()
	}

	for start := 0; start < len(texts); {
		end := batchEnd(texts, start)

		vecs, err := in.cfg.Embedder.Embed(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("embed: %w", err)
		}

		if len(vecs) != end-start {
			return nil, fmt.Errorf("embed: %d embeddings for %d texts", len(vecs), end-start)
		}

		for i, v := range vecs {
			chunks[start+i].Embedding = v
		}

		start = end
	}

	return chunks, nil
}
