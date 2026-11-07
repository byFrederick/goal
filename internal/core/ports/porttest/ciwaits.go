package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ciPollID keys a poll by organization, as ci_polls' primary key does.
type ciPollID struct {
	org domain.OrgID
	key domain.CIPollKey
}

// ciWaitRecord is one CI wait of an organization.
type ciWaitRecord struct {
	org  domain.OrgID
	wait domain.CIWaitRecord
}

// ciRows are the store's CI waits, in the order they were opened, and their polls.
type ciRows struct {
	waits []ciWaitRecord
	polls map[ciPollID]*domain.CIPoll
}

// purge removes the waits of gone items and, when org is set, every wait and poll of org.
// Callers hold s.mu.
func (c *ciRows) purge(org domain.OrgID, gone map[domain.ItemID]bool) {
	c.waits = slices.DeleteFunc(c.waits, func(r ciWaitRecord) bool { return gone[r.wait.Item] || (org != "" && r.org == org) })

	if org != "" {
		maps.DeleteFunc(c.polls, func(id ciPollID, _ *domain.CIPoll) bool { return id.org == org })
	}
}

// openCIWait opens n, creating its poll when org has none. Callers hold s.mu and checked n.
func (s *Store) openCIWait(org domain.OrgID, n domain.NewCIWait) (domain.CIWaitRecord, error) {
	for _, r := range s.ci.waits {
		w := r.wait
		if r.org == org && w.Open() && w.Item == n.Item && w.Station == n.Station && w.Visit == n.Visit && w.Repo == n.Repo {
			return domain.CIWaitRecord{}, fmt.Errorf("open ci wait of item %s at %s on %s: %w", n.Item, n.Station, n.Repo, ports.ErrConflict)
		}
	}

	id := ciPollID{org: org, key: n.PollKey()}
	if _, ok := s.ci.polls[id]; !ok {
		s.ci.polls[id] = &domain.CIPoll{CIPollKey: id.key, NextPollAt: n.StartedAt, Interval: domain.CIPollMinInterval}
	}

	w := domain.CIWaitRecord{ID: domain.CIWaitID(s.newID()), NewCIWait: n}
	w.Held = slices.Clone(n.Held)
	s.ci.waits = append(s.ci.waits, ciWaitRecord{org: org, wait: w})

	return w, nil
}

// OpenCIWait implements ports.Store.
func (s *Store) OpenCIWait(ctx context.Context, n domain.NewCIWait) (domain.CIWaitRecord, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.CIWaitRecord{}, err
	}

	if err := n.Check(); err != nil {
		return domain.CIWaitRecord{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.visible(n.Item, org); !ok {
		return domain.CIWaitRecord{}, fmt.Errorf("item %s: %w", n.Item, ports.ErrNotFound)
	}

	return s.openCIWait(org, n)
}

// ReplaceCIWaitSHA implements ports.Store.
func (s *Store) ReplaceCIWaitSHA(ctx context.Context, id domain.CIWaitID, sha, prURL string, at time.Time) (domain.CIWaitRecord, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.CIWaitRecord{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i := slices.IndexFunc(s.ci.waits, func(r ciWaitRecord) bool { return r.org == org && r.wait.ID == id && r.wait.Open() })
	if i < 0 {
		return domain.CIWaitRecord{}, fmt.Errorf("open ci wait %s: %w", id, ports.ErrNotFound)
	}

	n := s.ci.waits[i].wait.NewCIWait
	n.SHA, n.StartedAt = sha, at

	if prURL != "" {
		n.PRURL = prURL
	}

	if err := n.Check(); err != nil {
		return domain.CIWaitRecord{}, err
	}

	s.ci.waits[i].wait.EndedAt, s.ci.waits[i].wait.Outcome = at, domain.CISuperseded

	return s.openCIWait(org, n)
}

// hasOpenWait reports whether a wait of org on key is open. Callers hold s.mu.
func (c *ciRows) hasOpenWait(org domain.OrgID, key domain.CIPollKey) bool {
	return slices.ContainsFunc(c.waits, func(r ciWaitRecord) bool { return r.org == org && r.wait.Open() && r.wait.PollKey() == key })
}

// LeaseDueCIPolls implements ports.Store.
func (s *Store) LeaseDueCIPolls(ctx context.Context, now time.Time, owner string, lease time.Duration, limit int) ([]domain.CIPoll, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	if owner == "" || lease <= 0 {
		return nil, fmt.Errorf("lease ci polls: an owner and a lease are required: %w", domain.ErrInvalid)
	}

	limit = max(limit, 0)

	s.mu.Lock()
	defer s.mu.Unlock()

	var due []*domain.CIPoll

	for id, p := range s.ci.polls {
		live := p.LeaseOwner != "" && p.LeaseUntil.After(now)
		if id.org == org && !p.NextPollAt.After(now) && !live && s.ci.hasOpenWait(org, id.key) {
			due = append(due, p)
		}
	}

	slices.SortFunc(due, func(a, b *domain.CIPoll) int {
		return cmp.Or(a.NextPollAt.Compare(b.NextPollAt), cmp.Compare(a.Host, b.Host), cmp.Compare(a.Repo, b.Repo), cmp.Compare(a.SHA, b.SHA))
	})

	out := make([]domain.CIPoll, 0, min(len(due), limit))

	for _, p := range due[:min(len(due), limit)] {
		p.LeaseOwner, p.LeaseUntil = owner, now.Add(lease)
		out = append(out, *p)
	}

	return out, nil
}

// RecordCIPoll implements ports.Store.
func (s *Store) RecordCIPoll(ctx context.Context, key domain.CIPollKey, owner string, r domain.CIPollResult) error {
	org, err := orgOf(ctx)
	if err != nil {
		return err
	}

	if err := r.Check(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.ci.polls[ciPollID{org: org, key: key}]
	if !ok || owner == "" || p.LeaseOwner != owner {
		return fmt.Errorf("ci poll %s@%s: %w", key.Repo, key.SHA, ports.ErrLeaseLost)
	}

	if r.Checks != nil {
		p.Checks = slices.Clone(r.Checks)
	}

	if r.ETag != "" {
		p.ETag = r.ETag
	}

	if p.FirstCheckSeenAt.IsZero() {
		p.FirstCheckSeenAt = r.FirstCheckSeenAt
	}

	p.LastError, p.NextPollAt, p.Interval = r.Error, r.NextPollAt, r.Interval
	p.LeaseOwner, p.LeaseUntil = "", time.Time{}

	return nil
}

// OpenCIWaits implements ports.Store.
func (s *Store) OpenCIWaits(ctx context.Context, item domain.ItemID) ([]domain.CIWaitRecord, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var out []domain.CIWaitRecord

	for _, r := range s.ci.waits {
		if r.org == org && r.wait.Item == item && r.wait.Open() {
			w := r.wait
			w.Poll = *s.ci.polls[ciPollID{org: org, key: w.PollKey()}]
			out = append(out, w)
		}
	}

	slices.SortStableFunc(out, func(a, b domain.CIWaitRecord) int {
		return cmp.Or(a.StartedAt.Compare(b.StartedAt), cmp.Compare(a.Station, b.Station), cmp.Compare(a.Visit, b.Visit), cmp.Compare(a.Repo, b.Repo))
	})

	return out, nil
}

// CIWaits implements ports.Store.
func (s *Store) CIWaits(ctx context.Context, item domain.ItemID, station string) ([]domain.CIWaitRecord, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var out []domain.CIWaitRecord

	for _, r := range s.ci.waits {
		if r.org == org && r.wait.Item == item && r.wait.Station == station {
			w := r.wait
			w.Held = slices.Clone(w.Held)
			w.Poll = *s.ci.polls[ciPollID{org: org, key: w.PollKey()}]
			w.Poll.Checks = slices.Clone(w.Poll.Checks)
			out = append(out, w)
		}
	}

	slices.SortStableFunc(out, func(a, b domain.CIWaitRecord) int {
		return cmp.Or(a.StartedAt.Compare(b.StartedAt), cmp.Compare(a.Visit, b.Visit), cmp.Compare(a.Repo, b.Repo), cmp.Compare(a.ID, b.ID))
	})

	return out, nil
}

// heldByCI reports whether it has an open CI wait at its station, whatever its phase, which the
// claim skips (ADR 0044 decisions 2 and 3). Callers hold s.mu.
func (c *ciRows) heldByCI(org domain.OrgID, it *domain.Item) bool {
	return slices.ContainsFunc(c.waits, func(r ciWaitRecord) bool {
		return r.org == org && r.wait.Open() && r.wait.Item == it.ID && r.wait.Station == it.Station
	})
}

// OpenCIWaitsOnPoll implements ports.Store.
func (s *Store) OpenCIWaitsOnPoll(ctx context.Context, key domain.CIPollKey) ([]domain.CIWaitRecord, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var out []domain.CIWaitRecord

	for _, r := range s.ci.waits {
		if r.org == org && r.wait.Open() && r.wait.PollKey() == key {
			w := r.wait
			w.Held = slices.Clone(w.Held)
			out = append(out, w)
		}
	}

	slices.SortStableFunc(out, func(a, b domain.CIWaitRecord) int {
		return cmp.Or(a.StartedAt.Compare(b.StartedAt), cmp.Compare(a.Item, b.Item), cmp.Compare(a.Station, b.Station), cmp.Compare(a.Visit, b.Visit))
	})

	return out, nil
}

// EndCIWaits implements ports.Store.
func (s *Store) EndCIWaits(ctx context.Context, ids []domain.CIWaitID, outcome domain.CIOutcome, at time.Time) (int, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return 0, err
	}

	if !outcome.Valid() {
		return 0, fmt.Errorf("ci wait outcome %q: %w", outcome, domain.ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0

	for i, r := range s.ci.waits {
		if r.org == org && r.wait.Open() && slices.Contains(ids, r.wait.ID) {
			s.ci.waits[i].wait.EndedAt, s.ci.waits[i].wait.Outcome = at, outcome
			n++
		}
	}

	return n, nil
}

// CIRounds implements ports.Store.
func (s *Store) CIRounds(ctx context.Context, item domain.ItemID, station, visit string) (int, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rounds := map[int]bool{}

	for _, r := range s.ci.waits {
		if r.org == org && r.wait.Item == item && r.wait.Station == station && r.wait.Visit == visit {
			rounds[r.wait.Round] = true
		}
	}

	return len(rounds), nil
}
