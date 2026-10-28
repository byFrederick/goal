package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// saved_views' unique name indexes (migration 00074): a personal view's name per owner, a shared
// view's per factory.
const (
	personalViewNameKey = "saved_views_personal_name_key"
	sharedViewNameKey   = "saved_views_shared_name_key"
)

// ListSavedViews implements ports.Store. A factory or viewer that is not a UUID names nothing.
func (s *Store) ListSavedViews(ctx context.Context, factory domain.FactoryID, viewer domain.UserID) ([]domain.SavedView, error) {
	fid, err := uuid.Parse(string(factory))
	if err != nil {
		fid = uuid.Nil
	}

	var who *uuid.UUID
	if u, err := uuid.Parse(string(viewer)); err == nil {
		who = &u
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.SavedView, error) {
		return q.ListSavedViews(ctx, db.ListSavedViewsParams{FactoryID: fid, Viewer: who})
	})
	if err != nil {
		return nil, fmt.Errorf("saved views of factory %s: %w", factory, err)
	}

	out := make([]domain.SavedView, 0, len(rows))

	for _, r := range rows {
		v, err := savedViewFrom(r)
		if err != nil {
			return nil, err
		}

		out = append(out, v)
	}

	return out, nil
}

// CreateSavedView implements ports.Store.
func (s *Store) CreateSavedView(ctx context.Context, v domain.SavedView) (domain.SavedView, error) {
	if err := v.Validate(); err != nil {
		return domain.SavedView{}, err
	}

	fid, err := parseID("factory", string(v.Factory))
	if err != nil {
		return domain.SavedView{}, err
	}

	owner, err := uuid.Parse(string(v.Owner))
	if err != nil {
		return domain.SavedView{}, fmt.Errorf("saved view owner %q: %w", v.Owner, domain.ErrInvalid)
	}

	id, err := newID()
	if err != nil {
		return domain.SavedView{}, err
	}

	filters, err := json.Marshal(v.Filters)
	if err != nil {
		return domain.SavedView{}, fmt.Errorf("saved view filters: %w", err)
	}

	row, err := fetch(ctx, s.inTx, func(q *db.Queries) (db.SavedView, error) {
		return q.InsertSavedView(ctx, db.InsertSavedViewParams{
			ID: id, FactoryID: fid, OwnerID: owner, Name: v.Name, Shared: v.Shared, Filters: filters, CreatedAt: v.CreatedAt,
		})
	})
	if err != nil {
		return domain.SavedView{}, fmt.Errorf("create saved view %q: %w", v.Name, savedViewError(err))
	}

	return savedViewFrom(row)
}

// UpdateSavedView implements ports.Store.
func (s *Store) UpdateSavedView(ctx context.Context, factory domain.FactoryID, id domain.SavedViewID, name string,
	filters map[string][]string, at time.Time,
) (domain.SavedView, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return domain.SavedView{}, err
	}

	vid, err := parseID("saved view", string(id))
	if err != nil {
		return domain.SavedView{}, err
	}

	v := domain.SavedView{Name: name, Filters: filters}
	if err := v.Validate(); err != nil {
		return domain.SavedView{}, err
	}

	data, err := json.Marshal(v.Filters)
	if err != nil {
		return domain.SavedView{}, fmt.Errorf("saved view filters: %w", err)
	}

	row, err := fetch(ctx, s.inTx, func(q *db.Queries) (db.SavedView, error) {
		return q.UpdateSavedView(ctx, db.UpdateSavedViewParams{Name: v.Name, Filters: data, UpdatedAt: at, ID: vid, FactoryID: fid})
	})
	if notFound(err) {
		return domain.SavedView{}, fmt.Errorf("saved view %s: %w", id, ports.ErrNotFound)
	}

	if err != nil {
		return domain.SavedView{}, fmt.Errorf("update saved view %s: %w", id, savedViewError(err))
	}

	return savedViewFrom(row)
}

// DeleteSavedView implements ports.Store.
func (s *Store) DeleteSavedView(ctx context.Context, factory domain.FactoryID, id domain.SavedViewID) error {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return err
	}

	vid, err := parseID("saved view", string(id))
	if err != nil {
		return err
	}

	n, err := fetch(ctx, s.inTx, func(q *db.Queries) (int64, error) {
		return q.DeleteSavedView(ctx, db.DeleteSavedViewParams{ID: vid, FactoryID: fid})
	})
	if err != nil {
		return fmt.Errorf("delete saved view %s: %w", id, err)
	}

	if n == 0 {
		return fmt.Errorf("saved view %s: %w", id, ports.ErrNotFound)
	}

	return nil
}

// savedViewError names the rule a write broke: either name index, or a factory of no such id.
// Any other error is returned as it is.
func savedViewError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch {
	case pgErr.Code == uniqueViolation && (pgErr.ConstraintName == personalViewNameKey || pgErr.ConstraintName == sharedViewNameKey):
		return domain.ErrSavedViewNameTaken
	case pgErr.Code == foreignKeyViolation:
		return fmt.Errorf("%s: %w", pgErr.ConstraintName, ports.ErrNotFound)
	default:
		return err
	}
}

func savedViewFrom(r db.SavedView) (domain.SavedView, error) {
	filters := map[string][]string{}
	if err := json.Unmarshal(r.Filters, &filters); err != nil {
		return domain.SavedView{}, fmt.Errorf("saved view %s: filters: %w", r.ID, err)
	}

	return domain.SavedView{
		ID: domain.SavedViewID(r.ID.String()), Factory: domain.FactoryID(r.FactoryID.String()), Owner: domain.UserID(r.OwnerID.String()),
		Name: r.Name, Shared: r.Shared, Filters: filters, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}
