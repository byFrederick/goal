package postgres

import (
	"cmp"
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// filing is one item for fileItem to write: n as domain.File checked it, with a part's key,
// planned paths and base commit, and the items it follows, linked by afterBy.
type filing struct {
	n          domain.NewItem
	factory    uuid.UUID
	parent     *uuid.UUID
	partKey    *string
	partPaths  []string
	baseCommit *string
	after      []uuid.UUID
	afterBy    domain.Actor
}

// fileItem writes a filed item in q's transaction (ADR 0040 decision 1): the row, with the
// factory's next number and still its source (00112); its created event (part_created for a
// part) by the real actor; an item.filed audit entry; and a linked edge to each of f.after with
// its edge_added event. Its branch is domain.ItemBranch of n's factory name, number and title,
// and its repositories, n.Repositories, are written joined at its creation (ADR 0041 decisions 4
// and 6). Every way an item is made goes through it: CreateItem, Assemble's parts and AddPart.
func fileItem(ctx context.Context, q *db.Queries, f filing) (db.WorkItem, error) {
	n := f.n

	id, err := newID()
	if err != nil {
		return db.WorkItem{}, err
	}

	number, err := q.NextItemNumber(ctx, f.factory)
	if notFound(err) {
		return db.WorkItem{}, fmt.Errorf("factory %s: %w", f.factory, ports.ErrNotFound)
	}

	if err != nil {
		return db.WorkItem{}, fmt.Errorf("next item number: %w", err)
	}

	paths := f.partPaths
	if paths == nil {
		paths = []string{}
	}

	by := n.FiledBy()

	var createdBy *string

	if n.CreatedBy != (domain.Actor{}) {
		ref := by.Ref()
		createdBy = &ref
	}

	row, err := q.InsertItem(ctx, db.InsertItemParams{
		ID: id, FactoryID: f.factory, Number: number, ParentID: f.parent, PartKey: f.partKey, PartPaths: paths,
		Type: n.Type, Lane: optional(n.Lane), Title: n.Title, Body: n.Body, Station: n.Station, State: string(n.State),
		ClassOfService: string(n.Class), Priority: int64(n.Priority), BaseCommit: f.baseCommit, CreatedAt: n.CreatedAt,
		Source: optional(string(n.Source)), Trigger: optional(n.Trigger),
		Origin: string(cmp.Or(n.Origin, domain.OriginPerson)), Via: optional(string(n.Via)), CreatedBy: createdBy,
		Generation: int32(min(max(n.Generation, 0), maxInt32)), Untrusted: n.Untrusted,
		StartAt: optionalTime(n.StartAt), StartedAt: optionalTime(n.StartedAt),
		Branch: domain.ItemBranch(n.FactoryName, number, n.Title),
	})
	if violates(err, partKeyIndex) {
		return db.WorkItem{}, fmt.Errorf("part key %q is taken: %w", deref(f.partKey), ports.ErrConflict)
	}

	if err != nil {
		return db.WorkItem{}, fmt.Errorf("insert item: %w", err)
	}

	it, err := itemFrom(row)
	if err != nil {
		return db.WorkItem{}, err
	}

	for i, r := range n.Repositories {
		err := q.InsertItemRepository(ctx, db.InsertItemRepositoryParams{
			ItemID: id, Repo: r.Repo, Joined: string(r.Joined), JoinedAt: n.CreatedAt, Ordinal: int32(min(i, maxInt32)),
		})
		if err != nil {
			return db.WorkItem{}, fmt.Errorf("repository %s of item %d: %w", r.Repo, number, err)
		}
	}

	created, audit := domain.FiledRecords(it, by)

	linked, err := linkAfter(ctx, q, []db.WorkItem{row}, f.after, f.afterBy, n.CreatedAt)
	if err != nil {
		return db.WorkItem{}, err
	}

	if err := appendEvents(ctx, q, append([]domain.Event{created}, linked...)); err != nil {
		return db.WorkItem{}, err
	}

	if err := appendAudit(ctx, q, audit); err != nil {
		return db.WorkItem{}, err
	}

	return row, nil
}
