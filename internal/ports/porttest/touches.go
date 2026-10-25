package porttest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// touchLabel is the reason of one touch event, as item_touch_labels keeps it: every touch event
// gets one when it is appended, required_gate when planned.
type touchLabel struct {
	reason domain.TouchReason
	by     string
}

// labelEvent adds e's label row when e is a touch event. Callers hold s.mu.
func (s *Store) labelEvent(e domain.Event) {
	if domain.IsTouchEvent(e) {
		s.labels[e.ID] = touchLabel{reason: domain.DefaultReason(e)}
	}
}

// itemTouches is the touches of an item with their reasons. Callers hold s.mu.
func (s *Store) itemTouches(id domain.ItemID) []domain.Touch {
	var events []domain.Event

	for _, e := range s.events {
		if e.ItemID == id {
			events = append(events, e)
		}
	}

	touches := domain.TouchesOf(events)
	for i, t := range touches {
		l := s.labels[t.EventID]
		touches[i].Reason, touches[i].ReasonBy = l.reason, l.by
	}

	return touches
}

// ItemTouches implements ports.FlowStats.
func (s *Store) ItemTouches(ctx context.Context, item domain.ItemID) ([]domain.Touch, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.visible(item, org); !ok {
		return []domain.Touch{}, nil
	}

	return s.itemTouches(item), nil
}

// TouchCounts implements ports.FlowStats.
func (s *Store) TouchCounts(ctx context.Context, items []domain.ItemID) (map[domain.ItemID]int, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := map[domain.ItemID]int{}

	for _, id := range items {
		if _, ok := s.visible(id, org); ok {
			if n := len(s.itemTouches(id)); n > 0 {
				out[id] = n
			}
		}
	}

	return out, nil
}

// Touches implements ports.FlowStats.
func (s *Store) Touches(ctx context.Context, q ports.FlowQuery) (domain.TouchStats, error) {
	total, _, err := s.OrgTouches(ctx, []domain.FactoryID{q.FactoryID}, q)

	return total, err
}

// OrgTouches implements ports.FlowStats.
func (s *Store) OrgTouches(ctx context.Context, factories []domain.FactoryID, q ports.FlowQuery) (domain.TouchStats, []domain.FactoryTouchStats, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.TouchStats{}, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var all []domain.FinishedItem

	each := make([]domain.FactoryTouchStats, 0, len(factories))

	for _, f := range factories {
		items := s.finished(org, f, q, q.Since)
		all = append(all, items...)

		st := domain.TallyTouches(items)
		st.LineStops = s.lineStops(org, f, q.Since, q.Until)
		each = append(each, domain.FactoryTouchStats{FactoryID: f, TouchStats: st})
	}

	total := domain.TallyTouches(all)
	for _, f := range each {
		total.LineStops += f.LineStops
	}

	return total, each, nil
}

// TouchesByBucket implements ports.FlowStats.
func (s *Store) TouchesByBucket(ctx context.Context, q ports.FlowQuery, d time.Duration, n int) ([]domain.TouchStats, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	start := q.Until.Add(-time.Duration(n) * d)

	return domain.BucketTouches(s.finished(org, q.FactoryID, q, start), start, d, n), nil
}

// finished is factory's items of q.Type (and q.VersionID) done in [since, q.Until) with their
// touches, by when they were done. Callers hold s.mu.
func (s *Store) finished(org domain.OrgID, factory domain.FactoryID, q ports.FlowQuery, since time.Time) []domain.FinishedItem {
	var out []domain.FinishedItem

	for _, it := range s.items {
		if it.OrgID != org || it.FactoryID != factory || !ofType(it, q.Type) || !inWindow(it.DoneAt, since, q.Until) {
			continue
		}

		v := s.lastVersion(it.ID)
		if q.VersionID != "" && v != q.VersionID {
			continue
		}

		out = append(out, domain.FinishedItem{FactoryID: factory, ItemID: it.ID, VersionID: v, DoneAt: it.DoneAt, Touches: s.itemTouches(it.ID)})
	}

	slices.SortFunc(out, func(a, b domain.FinishedItem) int {
		return cmp.Or(a.DoneAt.Compare(b.DoneAt), cmp.Compare(a.ItemID, b.ItemID))
	})

	return out
}

// lastVersion is the definition version of the item's last agent run; "" without one. Callers
// hold s.mu.
func (s *Store) lastVersion(id domain.ItemID) domain.DefinitionVersionID {
	var last *domain.Run

	for _, r := range s.runs {
		if r.ItemID == id && (last == nil || r.StartedAt.After(last.StartedAt) || (r.StartedAt.Equal(last.StartedAt) && r.ID > last.ID)) {
			last = r
		}
	}

	if last == nil {
		return ""
	}

	return last.DefinitionVersionID
}

// lineStops counts the andon pulls people made on factory in [since, until). Callers hold s.mu.
func (s *Store) lineStops(org domain.OrgID, factory domain.FactoryID, since, until time.Time) int {
	n := 0

	for _, a := range s.andon {
		if a.org == org && a.pull.FactoryID == factory && strings.HasPrefix(a.pull.PulledBy, string(domain.ActorUser)+":") &&
			inWindow(a.pull.PulledAt, since, until) {
			n++
		}
	}

	return n
}

// LabelTouch implements ports.Store. The audit entry goes to the linked Tenancy, if any, after
// the change: the fake has no transaction spanning both.
func (s *Store) LabelTouch(ctx context.Context, event int64, reason domain.TouchReason, by domain.Actor) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	if !reason.Valid() {
		return fmt.Errorf("touch reason %q: %w", reason, domain.ErrInvalid)
	}

	t, audit, err := s.labelTouch(org, event, reason, by)
	if err != nil || audit == nil {
		return err
	}

	if err := audit.Append(ctx, domain.AuditEntry{
		Actor: by, Action: domain.AuditTouchLabelled, TargetKind: domain.AuditTargetItem, TargetID: string(t.ItemID),
		FactoryID: t.FactoryID, Data: map[string]any{"event_id": event, "reason": string(reason)},
	}); err != nil {
		return fmt.Errorf("audit touch label: %w", err)
	}

	return nil
}

func (s *Store) labelTouch(org domain.OrgID, event int64, reason domain.TouchReason, by domain.Actor) (domain.Touch, ports.Audit, error) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	i := slices.IndexFunc(s.events, func(e domain.Event) bool { return e.ID == event })
	if i < 0 {
		return domain.Touch{}, nil, fmt.Errorf("touch %d: %w", event, ports.ErrNotFound)
	}

	item := s.events[i].ItemID
	if _, ok := s.visible(item, org); ok {
		for _, t := range s.itemTouches(item) {
			if t.EventID == event {
				s.labels[event] = touchLabel{reason: reason, by: by.Ref()}

				return t, s.audit, nil
			}
		}
	}

	return domain.Touch{}, nil, fmt.Errorf("touch %d: %w", event, ports.ErrNotFound)
}
