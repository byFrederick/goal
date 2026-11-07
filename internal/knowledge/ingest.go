// Package knowledge indexes and searches the organizations' knowledge bases (spec 4 §4.8): the
// ingestion loop that syncs due sources under leases (Ingestor), the text extraction and the
// chunking it uses, uploads kept in the bucket (Uploads) and keyword search (Searcher). It reaches
// the database, the bucket and the sources only through ports.
package knowledge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Secrets read and open the secrets synced sources read with: ports.OrgSecrets.
type Secrets interface {
	Get(ctx context.Context, name string) (domain.Secret, error)
	Resolve(ctx context.Context, names []string, use domain.SecretUse) ([]domain.ResolvedSecret, error)
}

// Hosts list the code hosts the organization in ctx added (ADR 0041 decision 1):
// ports.CodeHosts. The built-in ones are listed too.
type Hosts interface {
	CodeHosts(ctx context.Context) ([]domain.CodeHost, error)
}

// Listings name the factories that list a knowledge source: the current, unarchived factories
// of the organization in ctx whose spec.knowledge names it (definitions.Cache).
type Listings interface {
	FactoriesListing(ctx context.Context, source string) []domain.FactoryID
}

// Defaults of the ingestion loop.
const (
	// DefaultWorkers is how many sources one goald syncs at once (GOAL_KB_WORKERS).
	DefaultWorkers = 2
	// Lease is how long a claim holds a source; it is renewed every third of it while a sync runs.
	Lease = 10 * time.Minute
	// Tick is how often an idle worker looks for a due source.
	Tick = 15 * time.Second
)

// IngestorConfig wires an Ingestor. Readers are the source
// readers by name: the reader a synced source on a secret names, gitdocs for repository and confluence for Confluence sources; Uploads
// reads upload sources. Objects stores the snapshots upload, repository and Confluence sources
// sync into, written first in TempDir ("" is the system's); nil writes none, and repository and
// Confluence sources then fail. Converter turns what is neither text nor HTML into Markdown; nil
// refuses it. Keep is how long a replaced snapshot stays (ADR 0038 decision 3): the longest a
// claim that may have pinned it can last; 0 deletes none. Hosts are the organizations' code
// hosts: a source gitdocs reads with a secret syncs only from a listed host (nil lists the
// built-in ones only). Listings name the factories that list each source: a source's secret
// opens only when each of them is in its scope (nil lists none, so only a secret open to all
// opens).
type IngestorConfig struct {
	Knowledge ports.Knowledge
	Secrets   Secrets
	Hosts     Hosts
	Listings  Listings
	Uploads   ports.SourceReader
	Readers   map[string]ports.SourceReader
	Objects   ports.ObjectStore
	Converter ports.Converter
	TempDir   string
	Clock     ports.Clock
	Log       *slog.Logger
	// Workers, Lease and Tick default to DefaultWorkers, Lease and Tick.
	Workers int
	Lease   time.Duration
	Tick    time.Duration
	Keep    time.Duration
}

// Ingestor syncs the sources that are due, in every live organization in one order (the source
// due first, whichever its organization): it claims one with a lease (renewed while it works
// and fencing its writes), lists its documents, fetches, extracts, chunks and embeds the
// new and changed ones, replaces their chunks, deletes those its source no longer lists and
// schedules the next sync. Upload, repository and Confluence sources sync into a snapshot too
// (ADR 0038 decision 3, snapshot.go); upload sources keep their chunks until the cut-over. Any goald runs one; a goald that dies mid-sync leaves its source to
// the next claim once the lease ends, and every step is safe to repeat.
type Ingestor struct {
	cfg    IngestorConfig
	limits byteLimits
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

	return &Ingestor{cfg: cfg, limits: defaultLimits}
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

// SyncDue syncs, one after the other and the one due first first, every source due in any
// organization that no other worker holds, and returns how many it synced (failed syncs
// included). With nothing due it is one claim. A source's failure is recorded on it, not
// returned; the error is the claim's or the database's.
func (in *Ingestor) SyncDue(ctx context.Context) (int, error) {
	n := 0

	var errs []error

	for ctx.Err() == nil {
		now := in.cfg.Clock.Now()

		c, err := in.cfg.Knowledge.ClaimSource(ctx, now, now.Add(in.cfg.Lease))
		if err != nil {
			errs = append(errs, fmt.Errorf("knowledge: claim: %w", err))

			break
		}

		if c == nil {
			break
		}

		n++

		if err := in.sync(domain.WithOrg(ctx, c.Org), *c); err != nil {
			errs = append(errs, err)
		}
	}

	return n, errors.Join(errs...)
}

// sync runs one claimed source's sync, renewing its lease, and records how it ended. A sync cut
// short (goald stopping, the lease lost or taken over) records nothing: the lease ends, or
// another claim holds it, and that sync takes the source over.
func (in *Ingestor) sync(ctx context.Context, c domain.SourceClaim) error {
	src := c.Source

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	renewed := make(chan struct{})

	go func() {
		defer close(renewed)

		in.renew(ctx, cancel, c.Lease)
	}()

	started := in.cfg.Clock.Now()
	stats, err := in.pass(ctx, c)

	lost := context.Cause(ctx)
	if lost == nil && errors.Is(err, ports.ErrLeaseLost) {
		lost = err
	}

	cancel(nil)
	<-renewed

	if lost != nil {
		in.cutShort(ctx, src, lost)

		return nil
	}

	r := domain.SyncResult{At: in.cfg.Clock.Now(), Due: *src.NextSyncAt}
	if err != nil {
		r.Error = err.Error()
	}

	in.cfg.Log.InfoContext(ctx, "knowledge sync", slog.String("source", src.Name), slog.String("source_id", string(src.ID)),
		slog.Int("files", stats.snapshot.files), slog.Int("files_fetched", stats.snapshot.fetched),
		slog.Int("files_archived", stats.snapshot.archived), slog.Int("files_failed", stats.snapshot.failed), slog.Bool("snapshot", stats.snapshot.written),
		slog.Float64("seconds", r.At.Sub(started).Seconds()), slog.String("error", r.Error))

	err = in.cfg.Knowledge.FinishSync(context.WithoutCancel(ctx), c.Lease, r)
	if errors.Is(err, ports.ErrLeaseLost) {
		in.cutShort(ctx, src, err)

		return nil
	}

	if err != nil {
		return fmt.Errorf("knowledge: finish %s: %w", src.Name, err)
	}

	return nil
}

// cutShort logs a sync that records nothing, and why.
func (in *Ingestor) cutShort(ctx context.Context, src domain.KnowledgeSource, cause error) {
	in.cfg.Log.WarnContext(ctx, "knowledge sync cut short", slog.String("source", src.Name), slog.Any("cause", cause))
}

// errLeaseLost cuts a sync short when its lease could not be renewed.
var errLeaseLost = errors.New("the lease on the source was lost")

// renew extends the lease every third of it until ctx ends; a renewal that fails cuts the sync
// short.
func (in *Ingestor) renew(ctx context.Context, cancel context.CancelCauseFunc, l domain.SourceLease) {
	tick := in.cfg.Clock.NewTicker(in.cfg.Lease / 3)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C():
			now := in.cfg.Clock.Now()
			if err := in.cfg.Knowledge.RenewSource(ctx, l, now, now.Add(in.cfg.Lease)); err != nil && ctx.Err() == nil {
				cancel(fmt.Errorf("%w: %w", errLeaseLost, err))

				return
			}
		}
	}
}

type syncStats struct {
	snapshot snapshotStats
}

// pass is one sync of src: for an upload or synced source every listed document whose version
// changed indexed, the others marked seen,
// and for a synced source the ones it no longer lists deleted; then for an upload, repository or
// Confluence source its snapshot.
func (in *Ingestor) pass(ctx context.Context, c domain.SourceClaim) (syncStats, error) {
	var stats syncStats

	src := c.Source

	spec, reader, err := in.reader(ctx, src)
	if err != nil {
		return stats, err
	}

	if !writesSnapshots(src.Kind) {
		return stats, nil
	}

	if in.cfg.Objects == nil {
		if src.Kind == domain.SourceUpload {
			return stats, nil
		}

		return stats, fmt.Errorf("source %s syncs into snapshots, and this goald stores none", src.Name)
	}

	stats.snapshot, err = in.snapshot(ctx, c, spec, reader)
	if !errors.Is(err, ports.ErrLeaseLost) && ctx.Err() == nil {
		in.retain(ctx, c)
	}

	return stats, err
}

// retain deletes the source's snapshots retention no longer keeps (ADR 0038 decision 3): those
// not current, pinned by no running run and replaced cfg.Keep ago or longer. Each zip goes before
// its row, so a crash between the two leaves a row whose zip is gone, which reads as not synced
// and the next sync deletes, never a zip no row names. A failure is logged and fails no sync.
func (in *Ingestor) retain(ctx context.Context, c domain.SourceClaim) {
	if in.cfg.Keep <= 0 {
		return
	}

	log := in.cfg.Log.With(slog.String("source", c.Source.Name), slog.String("source_id", string(c.Source.ID)))

	expired, err := in.cfg.Knowledge.ExpiredSnapshots(ctx, c.Lease, in.cfg.Clock.Now().Add(-in.cfg.Keep))
	if err != nil {
		log.WarnContext(ctx, "knowledge retention", slog.Any("error", err))

		return
	}

	var (
		gone []domain.KnowledgeSnapshotID
		errs []error
	)

	for _, s := range expired {
		if err := in.cfg.Objects.Delete(ctx, s.Key); err != nil {
			errs = append(errs, fmt.Errorf("delete %s: %w", s.Key, err))

			continue
		}

		gone = append(gone, s.ID)
	}

	if len(gone) > 0 {
		if err := in.cfg.Knowledge.DeleteSnapshots(ctx, c.Lease, gone); err != nil {
			errs = append(errs, err)
			gone = nil
		}
	}

	if len(gone) > 0 || len(errs) > 0 {
		log.InfoContext(ctx, "knowledge retention", slog.Int("snapshots_deleted", len(gone)),
			slog.Int("snapshots_expired", len(expired)), slog.Any("error", errors.Join(errs...)))
	}
}

// reader is what reads src: the uploads; a repository's or Confluence source's kind's reader,
// with its secret opened when it has one; or a synced source's reader its config names, with its
// secret opened. A synced source without a secret, one still on a connection (ADR 0037), has none.
func (in *Ingestor) reader(ctx context.Context, src domain.KnowledgeSource) (domain.SyncSpec, ports.SourceReader, error) {
	spec := domain.SyncSpec{Source: src}

	if src.Kind == domain.SourceUpload {
		return spec, in.cfg.Uploads, nil
	}

	if name, ok := kindReaders[src.Kind]; ok {
		if src.Secret == "" {
			reader, ok := in.cfg.Readers[name]
			if !ok {
				return spec, nil, fmt.Errorf("source %s reads with %q, which this goald cannot read", src.Name, name)
			}

			return spec, reader, nil
		}

		return in.secretReader(ctx, spec, name)
	}

	if src.Secret != "" {
		return in.secretReader(ctx, spec, src.Config[domain.SourceReaderKey])
	}

	if src.Connection != "" {
		return spec, nil, fmt.Errorf("source %s reads with connection %s, which goald no longer reads: give it a secret", src.Name, src.Connection)
	}

	return spec, nil, fmt.Errorf("source %s names no secret to read with: give it one", src.Name)
}

// secretReader opens the secret spec's source reads with, now (audited as secret.used, for the
// factory secretUse names), so a value replaced since is the one read, and picks the reader name.
func (in *Ingestor) secretReader(ctx context.Context, spec domain.SyncSpec, name string) (domain.SyncSpec, ports.SourceReader, error) {
	src := spec.Source

	if in.cfg.Secrets == nil {
		return spec, nil, fmt.Errorf("source %s reads with secret %s, and this goald opens no secrets", src.Name, src.Secret)
	}

	reader, ok := in.cfg.Readers[name]
	if !ok {
		return spec, nil, fmt.Errorf("source %s names reader %q, which this goald cannot read", src.Name, name)
	}

	if name == kindReaders[domain.SourceRepository] {
		listed, err := in.listedRepository(ctx, src)
		if err != nil {
			return spec, nil, err
		}

		spec.Source = listed
	}

	use, err := in.secretUse(ctx, src)
	if err != nil {
		return spec, nil, err
	}

	opened, err := in.cfg.Secrets.Resolve(ctx, []string{src.Secret}, use)
	if err != nil {
		return spec, nil, fmt.Errorf("secret %s: %w", src.Secret, err)
	}

	spec.Secret = &opened[0]

	return spec, reader, nil
}

// secretUse is what src's secret is opened for (ADR 0041 decision 16): no factory when the
// secret is open to all; else the first of the factories listing src, when every one of them is
// in its scope. A secret open to some factories only refuses a source a factory outside its scope
// lists, naming the factory, and a source no factory lists.
func (in *Ingestor) secretUse(ctx context.Context, src domain.KnowledgeSource) (domain.SecretUse, error) {
	secret, err := in.cfg.Secrets.Get(ctx, src.Secret)
	if err != nil {
		return domain.SecretUse{}, fmt.Errorf("secret %s: %w", src.Secret, err)
	}

	if secret.Scope.All {
		return domain.SecretUse{}, nil
	}

	var listing []domain.FactoryID
	if in.cfg.Listings != nil {
		listing = in.cfg.Listings.FactoriesListing(ctx, src.Name)
	}

	if len(listing) == 0 {
		return domain.SecretUse{}, fmt.Errorf("secret %s is open to some factories only, and no factory lists source %s; "+
			"a source no factory lists reads with a secret open to all: %w", src.Secret, src.Name, ports.ErrSecretNotAllowed)
	}

	for _, f := range listing {
		if !secret.Scope.Allows(f) {
			return domain.SecretUse{}, fmt.Errorf("secret %s is not open to factory %s, which lists source %s: %w",
				src.Secret, f, src.Name, ports.ErrSecretNotAllowed)
		}
	}

	return domain.SecretUse{Factory: listing[0]}, nil
}

// listedRepository is src, a source gitdocs reads with a secret, with its code host's kind set
// to the one its repository's host is listed as among the organization's code hosts (ADR 0041
// decision 1): the listed kind wins over the config's. A host the organization doesn't list is
// refused before the secret is opened, so the secret goes to no host nobody listed.
func (in *Ingestor) listedRepository(ctx context.Context, src domain.KnowledgeSource) (domain.KnowledgeSource, error) {
	u, err := url.Parse(strings.TrimSpace(src.Config[domain.SourceRepositoryKey]))
	if err != nil || u.Host == "" {
		return src, fmt.Errorf("source %s: the config names no repository URL: %w", src.Name, domain.ErrInvalid)
	}

	hosts := domain.BuiltInCodeHosts()

	if in.cfg.Hosts != nil {
		added, err := in.cfg.Hosts.CodeHosts(ctx)
		if err != nil {
			return src, fmt.Errorf("source %s: list code hosts: %w", src.Name, err)
		}

		hosts = append(hosts, added...)
	}

	host := strings.ToLower(u.Host)

	listed, ok := domain.ListedCodeHost(hosts, host)
	if !ok {
		return src, fmt.Errorf("source %s: %s is not one of the organization's code hosts, so goald sends it no secret; "+
			"an owner can add it under Code hosts in the organization's settings: %w", src.Name, host, domain.ErrInvalid)
	}

	src.Config = maps.Clone(src.Config)
	src.Config[domain.SourceCodeHostKey] = string(listed.Kind)

	return src, nil
}

// errFetch marks a document its source could not hand over.
var errFetch = errors.New("fetch")
