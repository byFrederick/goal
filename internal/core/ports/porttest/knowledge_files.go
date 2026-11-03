package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// readKey is a read count's key: one per source, path, factory and day.
type readKey struct {
	source  domain.KnowledgeSourceID
	path    string
	factory domain.FactoryID
	day     time.Time
}

// PutSnapshot implements ports.Knowledge.
func (k *Knowledge) PutSnapshot(ctx context.Context, l domain.SourceLease, snap domain.KnowledgeSnapshot, files []domain.KnowledgeFile) (domain.KnowledgeSnapshot, error) {
	if err := snap.Check(files); err != nil {
		return domain.KnowledgeSnapshot{}, fmt.Errorf("put knowledge snapshot: %w", err)
	}

	var out domain.KnowledgeSnapshot

	err := k.locked(ctx, func(org domain.OrgID, o *knowledgeOrg) error {
		s, err := o.leased(l)
		if err != nil {
			return err
		}

		if snap.Source != s.src.ID {
			return fmt.Errorf("snapshot of source %s under the lease of %s: %w", snap.Source, l.Source, domain.ErrInvalid)
		}

		snap.Key = domain.KnowledgeSnapshotKey(org, snap.Source, snap.ID)
		snap.CreatedAt = snap.CreatedAt.UTC()
		snap.Changes.Failed = slices.Clone(snap.Changes.Failed)

		s.files = make(map[string]domain.KnowledgeFile, len(files))
		for _, f := range files {
			f.Source = s.src.ID
			s.files[f.Path] = f
		}

		s.snapshots = append(s.snapshots, snap)
		s.src.CurrentSnapshot = snap.ID
		out = snap

		return nil
	})
	if err != nil {
		return domain.KnowledgeSnapshot{}, fmt.Errorf("put knowledge snapshot %s: %w", snap.ID, err)
	}

	return out, nil
}

// Snapshots implements ports.Knowledge.
func (k *Knowledge) Snapshots(ctx context.Context, source string) ([]domain.KnowledgeSnapshot, error) {
	var out []domain.KnowledgeSnapshot

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[source]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", source, ports.ErrNotFound)
		}

		out = slices.Clone(s.snapshots)
		slices.SortStableFunc(out, func(a, b domain.KnowledgeSnapshot) int {
			return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), strings.Compare(string(b.ID), string(a.ID)))
		})

		return nil
	})

	return out, err
}

// Files implements ports.Knowledge.
func (k *Knowledge) Files(ctx context.Context, q domain.KnowledgeFileQuery) ([]domain.KnowledgeFile, error) {
	limit := min(max(q.Limit, 0), domain.MaxFilePage)
	out := []domain.KnowledgeFile{}

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[q.Source]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", q.Source, ports.ErrNotFound)
		}

		prefix := strings.ToLower(q.Prefix)

		for _, p := range slices.Sorted(maps.Keys(s.files)) {
			if len(out) == limit {
				break
			}

			f := s.files[p]

			if p > q.After && (strings.HasPrefix(p, q.Prefix) || strings.HasPrefix(strings.ToLower(f.Title), prefix)) {
				out = append(out, f)
			}
		}

		return nil
	})

	return out, err
}

// AddReads implements ports.Knowledge.
func (k *Knowledge) AddReads(ctx context.Context, reads []domain.KnowledgeFileReads) error {
	for _, r := range reads {
		if err := r.Check(); err != nil {
			return fmt.Errorf("add knowledge reads: %w", err)
		}
	}

	return k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		for _, r := range reads {
			if o.byID(r.Source) == nil {
				return fmt.Errorf("add knowledge reads: knowledge source %s: %w", r.Source, ports.ErrNotFound)
			}
		}

		for _, r := range reads {
			s := o.byID(r.Source)
			if s.reads == nil {
				s.reads = map[readKey]domain.KnowledgeFileReads{}
			}

			r.Day = domain.KnowledgeDay(r.Day)
			key := readKey{source: r.Source, path: r.Path, factory: r.Factory, day: r.Day}

			sum := s.reads[key]
			r.Reads += sum.Reads
			r.Searches += sum.Searches
			s.reads[key] = r
		}

		return nil
	})
}

// FileReads implements ports.Knowledge.
func (k *Knowledge) FileReads(ctx context.Context, source string, since time.Time) ([]domain.KnowledgeFileReads, error) {
	from := domain.KnowledgeDay(since)

	var out []domain.KnowledgeFileReads

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[source]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", source, ports.ErrNotFound)
		}

		for _, r := range s.reads {
			if !r.Day.Before(from) {
				out = append(out, r)
			}
		}

		slices.SortFunc(out, func(a, b domain.KnowledgeFileReads) int {
			return cmp.Or(a.Day.Compare(b.Day), strings.Compare(a.Path, b.Path), strings.Compare(string(a.Factory), string(b.Factory)))
		})

		return nil
	})

	return out, err
}
