package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ItemByNumber implements ports.Store.
func (s *Store) ItemByNumber(ctx context.Context, factory domain.FactoryID, number int64) (domain.Item, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Item{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, it := range s.items {
		if it.OrgID == org && it.FactoryID == factory && it.Number == number {
			return clone(it), nil
		}
	}

	return domain.Item{}, fmt.Errorf("item %d of factory %s: %w", number, factory, ports.ErrNotFound)
}

// MergedItems implements ports.Store.
func (s *Store) MergedItems(ctx context.Context, factory domain.FactoryID, shas []string) (map[string]domain.Item, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := map[string]domain.Item{}

	for _, e := range s.events {
		sha, _ := e.Data["sha"].(string)
		if e.Kind != domain.EventMerged || e.FactoryID != factory || !slices.Contains(shas, sha) {
			continue
		}

		if it, ok := s.visible(e.ItemID, org); ok {
			out[sha] = clone(it)
		}
	}

	return out, nil
}

// ListItems implements ports.Store.
func (s *Store) ListItems(ctx context.Context, q ports.ItemQuery) ([]domain.Item, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var match []*domain.Item

	for _, it := range s.items {
		if it.OrgID == org && it.FactoryID == q.FactoryID && (len(q.States) == 0 || slices.Contains(q.States, it.State)) &&
			(q.Station == "" || it.Station == q.Station) && (q.Before <= 0 || it.Number < q.Before) {
			match = append(match, it)
		}
	}

	slices.SortFunc(match, func(a, b *domain.Item) int { return cmp.Compare(b.Number, a.Number) })

	out := make([]domain.Item, 0, min(q.Limit, len(match)))
	for _, it := range match[:min(q.Limit, len(match))] {
		out = append(out, clone(it))
	}

	return out, nil
}

// SearchItems implements ports.Store.
func (s *Store) SearchItems(ctx context.Context, q ports.ItemSearch) ([]domain.Item, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	title := strings.ToLower(q.Title)

	var match []*domain.Item

	for _, it := range s.items {
		byTitle := title != "" && strings.Contains(strings.ToLower(it.Title), title)
		if it.OrgID == org && slices.Contains(q.Factories, it.FactoryID) && (byTitle || (q.Number > 0 && it.Number == q.Number)) {
			match = append(match, it)
		}
	}

	exact := func(it *domain.Item) int {
		if it.Number == q.Number {
			return 0
		}

		return 1
	}

	slices.SortFunc(match, func(a, b *domain.Item) int {
		return cmp.Or(cmp.Compare(exact(a), exact(b)), cmp.Compare(b.Number, a.Number), cmp.Compare(a.FactoryID, b.FactoryID))
	})

	out := make([]domain.Item, 0, min(q.Limit, len(match)))
	for _, it := range match[:min(q.Limit, len(match))] {
		out = append(out, clone(it))
	}

	return out, nil
}

// OrgRuns implements ports.Store.
func (s *Store) OrgRuns(ctx context.Context, q ports.RunQuery) ([]domain.ListedRun, domain.RunSummary, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, domain.RunSummary{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		match = []domain.ListedRun{}
		sum   domain.RunSummary
		spend = map[domain.FactoryID]float64{}
	)

	for _, r := range s.runs {
		it, ok := s.visible(r.ItemID, org)
		if !ok || !slices.Contains(q.Factories, r.FactoryID) || r.StartedAt.Before(q.Since) || !r.StartedAt.Before(q.Until) {
			continue
		}

		countRun(&sum, r)
		spend[r.FactoryID] += r.Result.CostUSD

		if runMatches(q, r, it) {
			match = append(match, domain.ListedRun{Run: *r, ItemNumber: it.Number, ItemTitle: it.Title})
		}
	}

	for _, f := range slices.Sorted(maps.Keys(spend)) {
		sum.ByFactory = append(sum.ByFactory, domain.FactorySpend{FactoryID: f, USD: spend[f]})
	}

	slices.SortFunc(match, func(a, b domain.ListedRun) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
	})

	return match[:min(q.Limit, len(match))], sum, nil
}

// countRun adds r to a run summary.
func countRun(sum *domain.RunSummary, r *domain.Run) {
	sum.Total++
	sum.SpendUSD += r.Result.CostUSD

	switch r.Result.Status {
	case domain.RunRunning:
		sum.Running++
	case domain.RunFailed:
		sum.Failed++
	case domain.RunSucceeded, domain.RunCancelled:
		sum.Finished++
	}
}

// runMatches reports whether r, a run of it, passes q's status, text and cursor.
func runMatches(q ports.RunQuery, r *domain.Run, it *domain.Item) bool {
	if q.Status != "" && r.Result.Status != q.Status {
		return false
	}

	if q.Text != "" && !containsFold(it.Title, q.Text) && !containsFold(r.Agent, q.Text) && !containsFold(r.Model, q.Text) &&
		!numbered(q, it) {
		return false
	}

	return q.After == nil || r.StartedAt.Before(q.After.StartedAt) || (r.StartedAt.Equal(q.After.StartedAt) && r.ID < q.After.ID)
}

// numbered reports whether it is the item q's number names.
func numbered(q ports.RunQuery, it *domain.Item) bool {
	return q.Number > 0 && it.Number == q.Number && (q.NumberOf == "" || it.FactoryID == q.NumberOf)
}

// containsFold reports whether s contains sub, case-insensitively.
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// StationLoads implements ports.Store.
func (s *Store) StationLoads(ctx context.Context, factory domain.FactoryID) ([]domain.StationLoad, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	loads := map[string]*domain.StationLoad{}

	for _, it := range s.items {
		if it.OrgID != org || it.FactoryID != factory || it.State == domain.StateDone || it.State == domain.StateCancelled {
			continue
		}

		ld, ok := loads[it.Station]
		if !ok {
			ld = &domain.StationLoad{Station: it.Station}
			loads[it.Station] = ld
		}

		switch {
		case it.State.Waiting():
			ld.Waiting++
		case it.State == domain.StateInProgress:
			ld.InProgress++
		case it.State == domain.StateWaitingHuman:
			ld.WaitingHuman++
		}
	}

	out := make([]domain.StationLoad, 0, len(loads))
	for _, ld := range loads {
		out = append(out, *ld)
	}

	return out, nil
}

// SpendSince implements ports.Store.
func (s *Store) SpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (float64, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.spend(org, factory, since, time.Time{}, ""), nil
}

// spend adds the cost of an organization's factory's runs started in [since, until), or at or
// after since when until is zero, on items of typ ("" for every type). Callers hold s.mu.
func (s *Store) spend(org domain.OrgID, factory domain.FactoryID, since, until time.Time, typ string) float64 {
	var usd float64

	for _, r := range s.runs {
		it, ok := s.visible(r.ItemID, org)
		if !ok || r.FactoryID != factory || !ofType(it, typ) {
			continue
		}

		if (until.IsZero() && !r.StartedAt.Before(since)) || inWindow(r.StartedAt, since, until) {
			usd += r.Result.CostUSD
		}
	}

	return usd
}

// inWindow reports whether t, when set, is in [since, until).
func inWindow(t, since, until time.Time) bool {
	return !t.IsZero() && !t.Before(since) && t.Before(until)
}

// percentile interpolates linearly between the closest ranks of sorted seconds, as
// PostgreSQL's percentile_cont does. No values give zero.
func percentile(sorted []float64, p float64) time.Duration {
	return time.Duration(math.Round(percentileOf(sorted, p) * float64(time.Second)))
}

// percentileOf interpolates linearly between the closest ranks of sorted values, as
// PostgreSQL's percentile_cont does. No values give zero.
func percentileOf(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}

	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, len(sorted)-1)

	return sorted[lo] + (pos-float64(lo))*(sorted[hi]-sorted[lo])
}

// Attention implements ports.Store.
func (s *Store) Attention(ctx context.Context) ([]domain.Attention, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.Attention{}

	for _, id := range slices.Sorted(maps.Keys(s.factories)) {
		if s.factories[id] == org {
			out = append(out, s.attention(org, id))
		}
	}

	return out, nil
}

// attention counts one factory's decisions, blocked items and open andon pull. Callers hold s.mu.
func (s *Store) attention(org domain.OrgID, factory domain.FactoryID) domain.Attention {
	a := domain.Attention{Factory: factory}
	byType := map[string]int{}

	for _, it := range s.items {
		if it.OrgID != org || it.FactoryID != factory {
			continue
		}

		if it.State == domain.StateWaitingHuman {
			a.WaitingHuman++
			byType[it.Type]++
		}

		if it.State == domain.StateBlocked {
			a.Blocked++
		}
	}

	for _, typ := range slices.Sorted(maps.Keys(byType)) {
		a.WaitingByType = append(a.WaitingByType, domain.TypeCount{Type: typ, Count: byType[typ]})
	}

	a.Andon = s.openAndon(org, factory)

	return a
}

// InboxItems implements ports.FlowStats.
func (s *Store) InboxItems(ctx context.Context, factories []domain.FactoryID, limit int) ([]domain.AttentionItem, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var match []*domain.Item

	for _, it := range s.items {
		if it.OrgID == org && needsPerson(it) && slices.Contains(factories, it.FactoryID) {
			match = append(match, it)
		}
	}

	slices.SortFunc(match, func(a, b *domain.Item) int {
		return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
	})

	out := make([]domain.AttentionItem, 0, min(limit, len(match)))
	for _, it := range match[:min(limit, len(match))] {
		a := domain.AttentionItem{Item: clone(it)}
		if it.State == domain.StateBlocked {
			a.BlockedReason, a.Paused = s.blocker(it.ID)
		}

		out = append(out, a)
	}

	return out, nil
}

// needsPerson reports whether an item belongs in the inbox: waiting for a decision, blocked, or
// open in the expedite lane.
func needsPerson(it *domain.Item) bool {
	open := it.State != domain.StateDone && it.State != domain.StateCancelled

	return it.State == domain.StateWaitingHuman || it.State == domain.StateBlocked || (it.Class == domain.ClassExpedite && open)
}

// blocker is the reason of an item's latest blocked or paused event and whether it was a pause.
// Callers hold s.mu.
func (s *Store) blocker(id domain.ItemID) (string, bool) {
	for _, e := range slices.Backward(s.events) {
		if e.ItemID == id && (e.Kind == domain.EventBlocked || e.Kind == domain.EventPaused) {
			reason, _ := e.Data["reason"].(string)

			return reason, e.Kind == domain.EventPaused
		}
	}

	return "", false
}

// Assemblies implements ports.Store.
func (s *Store) Assemblies(ctx context.Context, factory domain.FactoryID, ids []domain.ItemID) (map[domain.ItemID]domain.Assembly, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := map[domain.ItemID]domain.Assembly{}

	for _, id := range ids {
		a, ok := s.items[id]
		if !ok || a.OrgID != org || a.FactoryID != factory {
			continue
		}

		var parts []domain.Part

		for _, it := range s.items {
			if it.OrgID == org && it.ParentID == id && it.PartKey != "" {
				parts = append(parts, domain.Part{ID: it.ID, Number: it.Number, Station: it.Station, State: it.State})
			}
		}

		if len(parts) == 0 {
			continue
		}

		slices.SortFunc(parts, func(a, b domain.Part) int { return cmp.Compare(a.Number, b.Number) })
		out[id] = domain.Assembly{ID: a.ID, Number: a.Number, Title: a.Title, Parts: parts}
	}

	return out, nil
}
