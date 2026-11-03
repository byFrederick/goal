package postgres

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// dayLayout is how days travel to and from the date column.
const dayLayout = time.DateOnly

// PutSnapshot implements ports.Knowledge: under the lease's hold (HoldKnowledgeLease) it inserts
// the snapshot, replaces the source's files and makes the snapshot current, in one transaction.
func (k *Knowledge) PutSnapshot(ctx context.Context, l domain.SourceLease, s domain.KnowledgeSnapshot, files []domain.KnowledgeFile) (domain.KnowledgeSnapshot, error) {
	if err := s.Check(files); err != nil {
		return domain.KnowledgeSnapshot{}, fmt.Errorf("put knowledge snapshot: %w", err)
	}

	sid, token, err := lease(l)
	if err != nil {
		return domain.KnowledgeSnapshot{}, err
	}

	if s.Source != l.Source {
		return domain.KnowledgeSnapshot{}, fmt.Errorf("snapshot of source %s under the lease of %s: %w", s.Source, l.Source, domain.ErrInvalid)
	}

	id, err := parseID("knowledge snapshot", string(s.ID))
	if err != nil {
		return domain.KnowledgeSnapshot{}, fmt.Errorf("put knowledge snapshot: %w", domain.ErrInvalid)
	}

	changes, err := json.Marshal(s.Changes)
	if err != nil {
		return domain.KnowledgeSnapshot{}, fmt.Errorf("put knowledge snapshot: changes: %w", err)
	}

	params := db.InsertKnowledgeFilesParams{
		SourceID: sid, Paths: make([]string, 0, len(files)), Titles: make([]string, 0, len(files)),
		Sizes: make([]int64, 0, len(files)), Versions: make([]string, 0, len(files)),
	}
	for _, f := range files {
		params.Paths = append(params.Paths, f.Path)
		params.Titles = append(params.Titles, f.Title)
		params.Sizes = append(params.Sizes, f.Bytes)
		params.Versions = append(params.Versions, f.Version)
	}

	err = k.s.inTx(ctx, func(q *db.Queries) error {
		if err := held(ctx, q, l, sid, token); err != nil {
			return err
		}

		key, err := q.InsertKnowledgeSnapshot(ctx, db.InsertKnowledgeSnapshotParams{
			ID: id, SourceID: sid, Files: int32(s.Files), Failed: int32(s.Failed), //nolint:gosec // bounded by KnowledgeSnapshot.Check
			Bytes: s.Bytes, ZipBytes: s.ZipBytes, Changes: changes, ReadWhen: s.ReadWhen, CreatedAt: s.CreatedAt,
		})
		if err != nil {
			return ruleError(err, "")
		}

		s.Key = key

		if err := q.DeleteKnowledgeFiles(ctx, sid); err != nil {
			return err
		}

		if len(files) > 0 {
			if err := q.InsertKnowledgeFiles(ctx, params); err != nil {
				return ruleError(err, "")
			}
		}

		return q.SetKnowledgeSnapshot(ctx, db.SetKnowledgeSnapshotParams{SnapshotID: &id, ID: sid})
	})
	if err != nil {
		return domain.KnowledgeSnapshot{}, fmt.Errorf("put knowledge snapshot %s: %w", s.ID, err)
	}

	s.CreatedAt = s.CreatedAt.UTC()

	return s, nil
}

// Snapshots implements ports.Knowledge.
func (k *Knowledge) Snapshots(ctx context.Context, source string) ([]domain.KnowledgeSnapshot, error) {
	var out []domain.KnowledgeSnapshot

	err := k.s.read(ctx, func(q *db.Queries) error {
		sid, err := sourceIDNamed(ctx, q, source)
		if err != nil {
			return err
		}

		rows, err := q.KnowledgeSnapshots(ctx, sid)
		if err != nil {
			return err
		}

		out = make([]domain.KnowledgeSnapshot, 0, len(rows))

		for _, r := range rows {
			s, err := snapshotFrom(r)
			if err != nil {
				return err
			}

			out = append(out, s)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge snapshots: %w", err)
	}

	return out, nil
}

// Files implements ports.Knowledge.
func (k *Knowledge) Files(ctx context.Context, fq domain.KnowledgeFileQuery) ([]domain.KnowledgeFile, error) {
	limit := min(max(fq.Limit, 0), domain.MaxFilePage)
	out := []domain.KnowledgeFile{}

	err := k.s.read(ctx, func(q *db.Queries) error {
		sid, err := sourceIDNamed(ctx, q, fq.Source)
		if err != nil || limit == 0 {
			return err
		}

		rows, err := q.KnowledgeFiles(ctx, db.KnowledgeFilesParams{
			SourceID: sid, After: fq.After, Prefix: fq.Prefix, MaxRows: int32(limit), //nolint:gosec // bounded by MaxFilePage
		})
		if err != nil {
			return err
		}

		for _, r := range rows {
			out = append(out, domain.KnowledgeFile{
				Source: domain.KnowledgeSourceID(r.SourceID.String()), Path: r.Path, Title: r.Title, Bytes: r.Bytes, Version: r.Version,
			})
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge files: %w", err)
	}

	return out, nil
}

// AddReads implements ports.Knowledge. Counts of one key are merged first, since one statement
// updates a row only once; a source unknown to the organization fails its foreign key, which
// takes the organization with it.
func (k *Knowledge) AddReads(ctx context.Context, reads []domain.KnowledgeFileReads) error {
	params, err := readParams(reads)
	if err != nil {
		return fmt.Errorf("add knowledge reads: %w", err)
	}

	if len(params.SourceIds) == 0 {
		_, ok := domain.OrgFrom(ctx)
		if !ok {
			return domain.ErrNoOrg
		}

		return nil
	}

	err = k.s.inTx(ctx, func(q *db.Queries) error {
		err := q.AddKnowledgeReads(ctx, params)
		if isForeignKeyViolation(err) {
			return fmt.Errorf("knowledge source: %w", ports.ErrNotFound)
		}

		return ruleError(err, "")
	})
	if err != nil {
		return fmt.Errorf("add knowledge reads: %w", err)
	}

	return nil
}

// readParams checks reads and merges those of one source, path, factory and day, in key order.
func readParams(reads []domain.KnowledgeFileReads) (db.AddKnowledgeReadsParams, error) {
	type key struct {
		source, factory uuid.UUID
		path, day       string
	}

	merged := map[key]*domain.KnowledgeFileReads{}

	var keys []key

	for _, r := range reads {
		if err := r.Check(); err != nil {
			return db.AddKnowledgeReadsParams{}, err
		}

		source, err := parseID("knowledge source", string(r.Source))
		if err != nil {
			return db.AddKnowledgeReadsParams{}, err
		}

		factory, err := uuid.Parse(string(r.Factory))
		if err != nil {
			return db.AddKnowledgeReadsParams{}, fmt.Errorf("factory %q: %w", r.Factory, domain.ErrInvalid)
		}

		k := key{source: source, factory: factory, path: r.Path, day: domain.KnowledgeDay(r.Day).Format(dayLayout)}
		if m, ok := merged[k]; ok {
			m.Reads += r.Reads
			m.Searches += r.Searches

			continue
		}

		merged[k] = &r
		keys = append(keys, k)
	}

	slices.SortFunc(keys, func(a, b key) int {
		return cmp.Or(strings.Compare(a.source.String(), b.source.String()), strings.Compare(a.path, b.path),
			strings.Compare(a.factory.String(), b.factory.String()), strings.Compare(a.day, b.day))
	})

	var p db.AddKnowledgeReadsParams

	for _, k := range keys {
		m := merged[k]
		p.SourceIds = append(p.SourceIds, k.source)
		p.Paths = append(p.Paths, k.path)
		p.FactoryIds = append(p.FactoryIds, k.factory)
		p.Days = append(p.Days, k.day)
		p.Reads = append(p.Reads, int32(m.Reads))          //nolint:gosec // a day's reads of one file fit
		p.Searches = append(p.Searches, int32(m.Searches)) //nolint:gosec // a day's searches of one file fit
	}

	return p, nil
}

// FileReads implements ports.Knowledge.
func (k *Knowledge) FileReads(ctx context.Context, source string, since time.Time) ([]domain.KnowledgeFileReads, error) {
	var out []domain.KnowledgeFileReads

	err := k.s.read(ctx, func(q *db.Queries) error {
		sid, err := sourceIDNamed(ctx, q, source)
		if err != nil {
			return err
		}

		rows, err := q.KnowledgeFileReads(ctx, db.KnowledgeFileReadsParams{SourceID: sid, Since: domain.KnowledgeDay(since).Format(dayLayout)})
		if err != nil {
			return err
		}

		for _, r := range rows {
			day, err := time.Parse(dayLayout, r.Day)
			if err != nil {
				return fmt.Errorf("day %q: %w", r.Day, err)
			}

			out = append(out, domain.KnowledgeFileReads{
				Source: domain.KnowledgeSourceID(r.SourceID.String()), Path: r.Path, Factory: domain.FactoryID(r.FactoryID.String()),
				Day: day, Reads: int(r.Reads), Searches: int(r.Searches),
			})
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge file reads: %w", err)
	}

	return out, nil
}

// sourceIDNamed is the id of the source name; ErrNotFound when there is none.
func sourceIDNamed(ctx context.Context, q *db.Queries, name string) (uuid.UUID, error) {
	src, err := sourceNamed(ctx, q, name)
	if err != nil {
		return uuid.UUID{}, err
	}

	return uuid.Parse(string(src.ID))
}

func snapshotFrom(r db.KnowledgeSnapshotsRow) (domain.KnowledgeSnapshot, error) {
	var changes domain.SnapshotChanges
	if err := json.Unmarshal(r.Changes, &changes); err != nil {
		return domain.KnowledgeSnapshot{}, fmt.Errorf("knowledge snapshot %s: changes: %w", r.ID, err)
	}

	return domain.KnowledgeSnapshot{
		ID: domain.KnowledgeSnapshotID(r.ID.String()), Source: domain.KnowledgeSourceID(r.SourceID.String()), Files: int(r.Files),
		Failed: int(r.Failed), Bytes: r.Bytes, ZipBytes: r.ZipBytes, Changes: changes, ReadWhen: r.ReadWhen, Key: r.ZipKey,
		CreatedAt: r.CreatedAt.UTC(),
	}, nil
}

func snapshotID(id *uuid.UUID) domain.KnowledgeSnapshotID {
	if id == nil {
		return ""
	}

	return domain.KnowledgeSnapshotID(id.String())
}
