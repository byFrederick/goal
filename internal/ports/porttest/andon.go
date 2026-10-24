package porttest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// PullAndon implements ports.Store. The audit entry goes to the linked Tenancy, if any, after the
// pull: the fake has no transaction spanning both.
func (s *Store) PullAndon(ctx context.Context, p domain.AndonPull, by domain.Actor) (domain.AndonPull, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.AndonPull{}, err
	}

	got, audit, err := s.pullAndon(org, p, by)
	if err != nil || audit == nil {
		return got, err
	}

	err = audit.Append(ctx, domain.AuditEntry{
		At: got.PulledAt, Actor: by, Action: domain.AuditAndonPulled, TargetKind: domain.AuditTargetFactory,
		TargetID: string(got.FactoryID), FactoryID: got.FactoryID, Data: andonAudit(got),
	})
	if err != nil {
		return domain.AndonPull{}, fmt.Errorf("audit andon pull: %w", err)
	}

	return got, nil
}

func (s *Store) pullAndon(org domain.OrgID, p domain.AndonPull, by domain.Actor) (domain.AndonPull, ports.Audit, error) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	if s.factories[p.FactoryID] != org {
		return domain.AndonPull{}, nil, fmt.Errorf("andon of factory %s: %w", p.FactoryID, ports.ErrNotFound)
	}

	if s.openAndon(org, p.FactoryID) != nil {
		return domain.AndonPull{}, nil, fmt.Errorf("andon of factory %s is already pulled: %w", p.FactoryID, ports.ErrConflict)
	}

	s.ids++
	p.ID, p.PulledBy, p.ClearedBy, p.ClearedAt, p.Note = int64(s.ids), by.Ref(), "", time.Time{}, ""

	p.Data = maps.Clone(p.Data)
	if p.Data == nil {
		p.Data = map[string]any{}
	}

	s.andon = append(s.andon, andonRecord{org: org, pull: p})

	return clonePull(p), s.audit, nil
}

// ClearAndon implements ports.Store.
func (s *Store) ClearAndon(ctx context.Context, factory domain.FactoryID, by domain.Actor, note string, at time.Time) (domain.AndonPull, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.AndonPull{}, err
	}

	got, audit, err := s.clearAndon(org, factory, by, note, at)
	if err != nil || audit == nil {
		return got, err
	}

	err = audit.Append(ctx, domain.AuditEntry{
		At: at, Actor: by, Action: domain.AuditAndonCleared, TargetKind: domain.AuditTargetFactory,
		TargetID: string(factory), FactoryID: factory, Data: andonAudit(got),
	})
	if err != nil {
		return domain.AndonPull{}, fmt.Errorf("audit andon clear: %w", err)
	}

	return got, nil
}

func (s *Store) clearAndon(org domain.OrgID, factory domain.FactoryID, by domain.Actor, note string, at time.Time) (domain.AndonPull, ports.Audit, error) {
	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	for i, r := range s.andon {
		if r.org == org && r.pull.FactoryID == factory && r.pull.Open() {
			p := &s.andon[i].pull
			p.ClearedBy, p.ClearedAt, p.Note = by.Ref(), at, note

			return clonePull(*p), s.audit, nil
		}
	}

	return domain.AndonPull{}, nil, fmt.Errorf("andon of factory %s is not pulled: %w", factory, ports.ErrNotFound)
}

// OpenAndon implements ports.Store.
func (s *Store) OpenAndon(ctx context.Context, factory domain.FactoryID) (*domain.AndonPull, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.openAndon(org, factory), nil
}

// AndonPulls implements ports.Store.
func (s *Store) AndonPulls(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.AndonPull, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.AndonPull{}

	for _, r := range slices.Backward(s.andon) {
		if len(out) == limit {
			break
		}

		if r.org == org && r.pull.FactoryID == factory {
			out = append(out, clonePull(r.pull))
		}
	}

	return out, nil
}

// openAndon is the factory's open pull, or nil. Callers hold s.mu.
func (s *Store) openAndon(org domain.OrgID, factory domain.FactoryID) *domain.AndonPull {
	for _, r := range s.andon {
		if r.org == org && r.pull.FactoryID == factory && r.pull.Open() {
			p := clonePull(r.pull)
			return &p
		}
	}

	return nil
}

// GateDecisions implements ports.FlowStats.
func (s *Store) GateDecisions(ctx context.Context, factory domain.FactoryID, station string, after time.Time, n int) (domain.GateTally, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.GateTally{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var verdicts []domain.GateVerdict

	for _, e := range slices.Backward(s.events) {
		if len(verdicts) == n {
			break
		}

		it, ok := s.items[e.ItemID]
		isVerdict := e.Kind == domain.EventGateApproved || e.Kind == domain.EventGateRejected

		if !ok || it.OrgID != org || e.FactoryID != factory || e.Station != station || !isVerdict {
			continue
		}

		verdicts = append(verdicts, gateVerdict(e))
	}

	// Of the last n verdicts, those after the clear, as the PostgreSQL query reads them.
	verdicts = slices.DeleteFunc(verdicts, func(v domain.GateVerdict) bool { return !v.At.After(after) })

	return domain.TallyGate(station, verdicts), nil
}

func gateVerdict(e domain.Event) domain.GateVerdict {
	category, _ := e.Data["category"].(string)
	reason, _ := e.Data["reason"].(string)

	return domain.GateVerdict{Rejected: e.Kind == domain.EventGateRejected, Category: category, Reason: reason, At: e.At}
}

// andonAudit is the data of an andon audit entry, as the PostgreSQL store writes it.
func andonAudit(p domain.AndonPull) map[string]any {
	data := map[string]any{"pull": strconv.FormatInt(p.ID, 10), "kind": string(p.Kind), "reason": p.Reason}
	if !p.Open() {
		data["note"] = p.Note
	}

	return data
}

func clonePull(p domain.AndonPull) domain.AndonPull {
	p.Data = maps.Clone(p.Data)
	return p
}
