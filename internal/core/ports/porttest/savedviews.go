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

// savedViewRecord is one saved view of an organization.
type savedViewRecord struct {
	org  domain.OrgID
	view domain.SavedView
}

// ListSavedViews implements ports.Store.
func (s *Store) ListSavedViews(ctx context.Context, factory domain.FactoryID, viewer domain.UserID) ([]domain.SavedView, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.SavedView{}

	for _, r := range s.views {
		if r.org == org && r.view.Factory == factory && (r.view.Shared || (viewer != "" && r.view.Owner == viewer)) {
			out = append(out, cloneView(r.view))
		}
	}

	slices.SortFunc(out, func(a, b domain.SavedView) int {
		if a.Shared != b.Shared {
			if a.Shared {
				return -1
			}

			return 1
		}

		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), strings.Compare(string(a.ID), string(b.ID)))
	})

	return out, nil
}

// CreateSavedView implements ports.Store.
func (s *Store) CreateSavedView(ctx context.Context, v domain.SavedView) (domain.SavedView, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.SavedView{}, err
	}

	if err := v.Validate(); err != nil {
		return domain.SavedView{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.factories[v.Factory] != org {
		return domain.SavedView{}, fmt.Errorf("factory %s: %w", v.Factory, ports.ErrNotFound)
	}

	v.ID = domain.SavedViewID(s.newID())
	v.UpdatedAt = v.CreatedAt

	if s.viewNameTaken(org, v) {
		return domain.SavedView{}, fmt.Errorf("saved view %q: %w", v.Name, domain.ErrSavedViewNameTaken)
	}

	s.views[v.ID] = savedViewRecord{org: org, view: cloneView(v)}

	return v, nil
}

// UpdateSavedView implements ports.Store.
func (s *Store) UpdateSavedView(ctx context.Context, factory domain.FactoryID, id domain.SavedViewID, name string,
	filters map[string][]string, at time.Time,
) (domain.SavedView, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.SavedView{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.views[id]
	if !ok || r.org != org || r.view.Factory != factory {
		return domain.SavedView{}, fmt.Errorf("saved view %s: %w", id, ports.ErrNotFound)
	}

	v := r.view
	v.Name, v.Filters, v.UpdatedAt = name, filters, at

	if err := v.Validate(); err != nil {
		return domain.SavedView{}, err
	}

	if s.viewNameTaken(org, v) {
		return domain.SavedView{}, fmt.Errorf("saved view %q: %w", v.Name, domain.ErrSavedViewNameTaken)
	}

	s.views[id] = savedViewRecord{org: org, view: cloneView(v)}

	return v, nil
}

// DeleteSavedView implements ports.Store.
func (s *Store) DeleteSavedView(ctx context.Context, factory domain.FactoryID, id domain.SavedViewID) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.views[id]
	if !ok || r.org != org || r.view.Factory != factory {
		return fmt.Errorf("saved view %s: %w", id, ports.ErrNotFound)
	}

	delete(s.views, id)

	return nil
}

// viewNameTaken reports whether another view clashes with v's name as saved_views' unique
// indexes say: per owner for personal views, per factory for shared ones. Callers hold s.mu.
func (s *Store) viewNameTaken(org domain.OrgID, v domain.SavedView) bool {
	for _, r := range s.views {
		o := r.view
		if r.org != org || o.ID == v.ID || o.Factory != v.Factory || o.Shared != v.Shared || !strings.EqualFold(o.Name, v.Name) {
			continue
		}

		if v.Shared || o.Owner == v.Owner {
			return true
		}
	}

	return false
}

// cloneView copies v's filters, so callers never share them with the store.
func cloneView(v domain.SavedView) domain.SavedView {
	filters := make(map[string][]string, len(v.Filters))
	for k, values := range maps.All(v.Filters) {
		filters[k] = slices.Clone(values)
	}

	v.Filters = filters

	return v
}
