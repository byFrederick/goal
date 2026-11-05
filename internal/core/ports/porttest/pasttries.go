package porttest

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.PastTries = (*Store)(nil)

// QueuePastTries implements ports.PastTries.
func (s *Store) QueuePastTries(ctx context.Context, factory domain.FactoryID, proposal domain.ProposalID, tries []domain.NewPastTry,
	at time.Time,
) ([]domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	for _, n := range tries {
		if err := n.Check(); err != nil {
			return nil, fmt.Errorf("queue past tries: %w", err)
		}
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	number := s.proposalNumber(org, factory, proposal)
	if number == 0 {
		return nil, fmt.Errorf("queue past tries: proposal %s: %w", proposal, ports.ErrNotFound)
	}

	if slices.ContainsFunc(s.jonah.tries, func(r jonahRow[domain.PastTry]) bool { return r.org == org && r.row.ProposalID == proposal }) {
		return nil, fmt.Errorf("queue past tries: proposal %s has tries: %w", proposal, ports.ErrConflict)
	}

	out, err := s.newPastTries(org, factory, proposal, number, tries, at)
	if err != nil {
		return nil, err
	}

	for _, t := range out {
		s.jonah.tries = append(s.jonah.tries, jonahRow[domain.PastTry]{org: org, row: t})
	}

	return out, nil
}

// AddPastTries implements ports.PastTries.
func (s *Store) AddPastTries(ctx context.Context, factory domain.FactoryID, proposal domain.ProposalID, adds []domain.PastTryAdd,
	at time.Time,
) ([]domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	if err := domain.CheckPastTryAdds(adds); err != nil {
		return nil, fmt.Errorf("add past tries: %w", err)
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	i := slices.IndexFunc(s.jonah.proposals, func(r jonahRow[domain.Proposal]) bool {
		return r.org == org && r.row.FactoryID == factory && r.row.ID == proposal
	})
	if i < 0 {
		return nil, fmt.Errorf("add past tries: proposal %s: %w", proposal, ports.ErrNotFound)
	}

	p := s.jonah.proposals[i].row
	if p.State != domain.ProposalProposed {
		return nil, fmt.Errorf("add past tries: proposal %s is %s: %w", proposal, p.State, ports.ErrProposalDecided)
	}

	var out []domain.PastTry

	for _, a := range adds {
		latest := s.latestTry(org, proposal, a.ItemID)
		if latest.ID == "" {
			return nil, fmt.Errorf("add past tries: proposal %s has no try of item %s: %w", proposal, a.ItemID, ports.ErrNotFound)
		}

		for k := 1; k <= a.Tries; k++ {
			out = append(out, domain.PastTry{
				ID: domain.PastTryID(s.newID()), FactoryID: factory, ProposalID: proposal, ProposalNumber: p.Number, ItemID: a.ItemID,
				Attempt: latest.Attempt + k, Station: latest.Station, OriginalRunID: latest.OriginalRunID, Model: latest.Model,
				State: domain.PastTryQueued, CreatedAt: at,
			})
		}
	}

	for _, t := range out {
		s.jonah.tries = append(s.jonah.tries, jonahRow[domain.PastTry]{org: org, row: t})
	}

	return out, nil
}

// latestTry is org's proposal's try of item with the highest attempt, zero when it has none.
// Callers hold s.mu.
func (s *Store) latestTry(org domain.OrgID, proposal domain.ProposalID, item domain.ItemID) domain.PastTry {
	var latest domain.PastTry

	for _, r := range s.jonah.tries {
		if r.org == org && r.row.ProposalID == proposal && r.row.ItemID == item && r.row.KeptTestID == "" && r.row.Attempt > latest.Attempt {
			latest = r.row
		}
	}

	return latest
}

// proposalNumber is the number of org's factory's proposal, 0 when there is none. Callers hold s.mu.
func (s *Store) proposalNumber(org domain.OrgID, factory domain.FactoryID, proposal domain.ProposalID) int64 {
	for _, p := range s.jonah.proposals {
		if p.org == org && p.row.FactoryID == factory && p.row.ID == proposal {
			return p.row.Number
		}
	}

	return 0
}

// newPastTries are the queued tries of tries, checked against org's items and runs. Callers hold s.mu.
func (s *Store) newPastTries(org domain.OrgID, factory domain.FactoryID, proposal domain.ProposalID, number int64,
	tries []domain.NewPastTry, at time.Time,
) ([]domain.PastTry, error) {
	out := make([]domain.PastTry, 0, len(tries))

	for _, n := range tries {
		it, ok := s.visible(n.ItemID, org)
		run, rok := s.runs[n.OriginalRunID]

		if !ok || it.FactoryID != factory || !rok || !s.runVisible(run, org) {
			return nil, fmt.Errorf("queue past tries: item %s or run %s: %w", n.ItemID, n.OriginalRunID, ports.ErrNotFound)
		}

		if n.KeptTestID != "" && !s.keptTestOf(org, factory, n.KeptTestID) {
			return nil, fmt.Errorf("queue past tries: kept test %s: %w", n.KeptTestID, ports.ErrNotFound)
		}

		if slices.ContainsFunc(out, func(t domain.PastTry) bool {
			return t.ItemID == n.ItemID && t.KeptTestID == n.KeptTestID && t.Attempt == n.TryAttempt()
		}) {
			return nil, fmt.Errorf("queue past tries: item %s attempt %d twice: %w", n.ItemID, n.TryAttempt(), ports.ErrConflict)
		}

		out = append(out, domain.PastTry{
			ID: domain.PastTryID(s.newID()), FactoryID: factory, ProposalID: proposal, ProposalNumber: number, KeptTestID: n.KeptTestID,
			ContentKey: n.ContentKey, ItemID: n.ItemID, Attempt: n.TryAttempt(), Station: n.Station, OriginalRunID: n.OriginalRunID, Model: n.Model,
			State: domain.PastTryQueued, CreatedAt: at,
		})
	}

	return out, nil
}

// keptTestOf reports whether org's factory has kept test id. Callers hold s.mu.
func (s *Store) keptTestOf(org domain.OrgID, factory domain.FactoryID, id domain.KeptTestID) bool {
	return slices.ContainsFunc(s.jonah.keptTests, func(r jonahRow[domain.KeptTest]) bool {
		return r.org == org && r.row.FactoryID == factory && r.row.ID == id
	})
}

// QueuePublishTries implements ports.PastTries.
func (s *Store) QueuePublishTries(ctx context.Context, factory domain.FactoryID, version domain.DefinitionVersionID, edits []domain.ProposalEdit,
	tries []domain.NewPastTry, at time.Time,
) ([]domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	for _, n := range tries {
		if err := n.CheckPublish(); err != nil {
			return nil, fmt.Errorf("queue publish tries: %w", err)
		}
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	out, err := s.newPastTries(org, factory, "", 0, s.duePublishTries(org, tries), at)
	if err != nil {
		return nil, err
	}

	for i := range out {
		out[i].VersionID, out[i].Edits = version, slices.Clone(edits)
		s.jonah.tries = append(s.jonah.tries, jonahRow[domain.PastTry]{org: org, row: out[i]})
	}

	return out, nil
}

// duePublishTries are those of tries whose kept test, content key and attempt no publish's try of
// org has, each once, as tries_publish_key keeps them. Callers hold s.mu.
func (s *Store) duePublishTries(org domain.OrgID, tries []domain.NewPastTry) []domain.NewPastTry {
	var due []domain.NewPastTry

	same := func(n domain.NewPastTry, kept domain.KeptTestID, key string, attempt int) bool {
		return n.KeptTestID == kept && n.ContentKey == key && n.TryAttempt() == attempt
	}

	for _, n := range tries {
		had := slices.ContainsFunc(s.jonah.tries, func(r jonahRow[domain.PastTry]) bool {
			return r.org == org && r.row.ProposalID == "" && same(n, r.row.KeptTestID, r.row.ContentKey, r.row.Attempt)
		})

		if !had && !slices.ContainsFunc(due, func(d domain.NewPastTry) bool { return same(n, d.KeptTestID, d.ContentKey, d.TryAttempt()) }) {
			due = append(due, n)
		}
	}

	return due
}

// KeptTestTries implements ports.PastTries.
func (s *Store) KeptTestTries(ctx context.Context, factory domain.FactoryID, key string) ([]domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.PastTry{}

	for _, r := range s.jonah.tries {
		if r.org == org && r.row.FactoryID == factory && r.row.KeptTestID != "" && r.row.ContentKey == key {
			out = append(out, r.row)
		}
	}

	return out, nil
}

// ProposalPastTries implements ports.PastTries.
func (s *Store) ProposalPastTries(ctx context.Context, proposal domain.ProposalID) ([]domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.PastTry{}

	for _, r := range s.jonah.tries {
		if r.org == org && r.row.ProposalID == proposal && proposal != "" {
			out = append(out, r.row)
		}
	}

	return out, nil
}

// PastTry implements ports.PastTries.
func (s *Store) PastTry(ctx context.Context, id domain.PastTryID) (domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.PastTry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.jonah.tries {
		if r.org == org && r.row.ID == id {
			return r.row, nil
		}
	}

	return domain.PastTry{}, fmt.Errorf("past try %s: %w", id, ports.ErrNotFound)
}

// ClaimPastTry implements ports.PastTries.
func (s *Store) ClaimPastTry(ctx context.Context, factory domain.FactoryID, now time.Time, lease time.Duration, limit int) (domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.PastTry{}, err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	running, next := s.nextPastTry(org, factory, now)
	if next < 0 || running >= limit {
		return domain.PastTry{}, fmt.Errorf("claim past try of factory %s: %w", factory, ports.ErrNotFound)
	}

	t := &s.jonah.tries[next].row
	t.State, t.LeaseUntil, t.Attempts = domain.PastTryRunning, now.Add(lease), t.Attempts+1

	return *t, nil
}

// nextPastTry is how many of org's factory's tries run under a live lease at now, and the index of
// the one a claim takes: the oldest due publish's try, else the oldest due try; -1 for none.
// Callers hold s.mu.
func (s *Store) nextPastTry(org domain.OrgID, factory domain.FactoryID, now time.Time) (running, next int) {
	next, publish := -1, -1

	for i, r := range s.jonah.tries {
		if r.org != org || r.row.FactoryID != factory {
			continue
		}

		live := r.row.State == domain.PastTryRunning && r.row.LeaseUntil.After(now)
		if live {
			running++
		}

		if r.row.State != domain.PastTryQueued && (r.row.State != domain.PastTryRunning || live) {
			continue
		}

		if next < 0 {
			next = i
		}

		if publish < 0 && r.row.ProposalID == "" {
			publish = i
		}
	}

	if publish >= 0 {
		next = publish
	}

	return running, next
}

// FinishPastTry implements ports.PastTries.
func (s *Store) FinishPastTry(ctx context.Context, claimed domain.PastTry, r domain.PastTryResult) (domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.PastTry{}, err
	}

	if err := r.Check(); err != nil {
		return domain.PastTry{}, fmt.Errorf("finish past try %s: %w", claimed.ID, err)
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	for i := range s.jonah.tries {
		t := &s.jonah.tries[i].row
		if s.jonah.tries[i].org != org || t.ID != claimed.ID {
			continue
		}

		if t.State != domain.PastTryRunning || !t.LeaseUntil.Equal(claimed.LeaseUntil) {
			return domain.PastTry{}, fmt.Errorf("finish past try %s: %w", claimed.ID, ports.ErrLeaseLost)
		}

		t.State, t.LeaseUntil, t.FinishedAt = domain.PastTryDone, time.Time{}, r.At
		t.Verdict, t.Error, t.Reason, t.Diff, t.CostUSD, t.TryRunID = r.Verdict, r.Error, r.Reason, r.Diff, r.CostUSD, r.TryRunID

		return *t, nil
	}

	return domain.PastTry{}, fmt.Errorf("finish past try %s: %w", claimed.ID, ports.ErrLeaseLost)
}

// RecordPastTryRun implements ports.PastTries.
func (s *Store) RecordPastTryRun(ctx context.Context, claimed domain.PastTry, run domain.RunID, diff string, cost float64) (domain.PastTry, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.PastTry{}, err
	}

	if err := domain.CheckPastTryRun(run, diff, cost); err != nil {
		return domain.PastTry{}, fmt.Errorf("record past try %s's run: %w", claimed.ID, err)
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	for i := range s.jonah.tries {
		t := &s.jonah.tries[i].row
		if s.jonah.tries[i].org != org || t.ID != claimed.ID {
			continue
		}

		if t.State != domain.PastTryRunning || !t.LeaseUntil.Equal(claimed.LeaseUntil) {
			break
		}

		t.TryRunID, t.Diff, t.CostUSD = run, diff, cost

		return *t, nil
	}

	return domain.PastTry{}, fmt.Errorf("record past try %s's run: %w", claimed.ID, ports.ErrLeaseLost)
}

// PastTryTarget implements ports.PastTries.
func (s *Store) PastTryTarget(ctx context.Context, item domain.ItemID, station string) (domain.Run, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.Run{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var best *domain.Run

	for id, r := range s.runs {
		if !s.firstRunAt(org, id, r, item, station) {
			continue
		}

		if best == nil || r.StartedAt.After(best.StartedAt) || (r.StartedAt.Equal(best.StartedAt) && r.ID > best.ID) {
			best = r
		}
	}

	if best == nil {
		return domain.Run{}, fmt.Errorf("past try target of item %s at %s: %w", item, station, ports.ErrNotFound)
	}

	return *best, nil
}

// firstRunAt reports whether run id, r, is a finished station run of org's item at station that
// was a claim's first: one of its repositories has a branch head. Callers hold s.mu.
func (s *Store) firstRunAt(org domain.OrgID, id domain.RunID, r *domain.Run, item domain.ItemID, station string) bool {
	return r.ItemID == item && r.Station == station && s.runVisible(r, org) && s.jonah.runKinds[id] == domain.RunKindStation &&
		r.Result.Status != domain.RunRunning && hasTryRepository(r.Input)
}

// AgentDoneItems implements ports.PastTries.
func (s *Store) AgentDoneItems(ctx context.Context, factory domain.FactoryID, agent string, limit int) ([]domain.AgentItem, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := []domain.AgentItem{}

	for id, it := range s.items {
		if it.OrgID != org || it.FactoryID != factory || it.State != domain.StateDone {
			continue
		}

		best := s.newestFirstRun(org, id, agent)
		if best == nil {
			continue
		}

		out = append(out, domain.AgentItem{
			ItemID: id, ItemNumber: it.Number, ItemTitle: it.Title, Run: *best, SentBackAt: s.sentBackAt(id), CostUSD: it.CostUSD, DoneAt: it.DoneAt,
		})
	}

	slices.SortFunc(out, func(a, b domain.AgentItem) int {
		return cmp.Or(b.DoneAt.Compare(a.DoneAt), cmp.Compare(a.ItemID, b.ItemID))
	})

	return out[:min(len(out), max(limit, 0))], nil
}

// newestFirstRun is org's item's newest finished station run of agent that was a claim's first;
// nil when it has none. Callers hold s.mu.
func (s *Store) newestFirstRun(org domain.OrgID, item domain.ItemID, agent string) *domain.Run {
	var best *domain.Run

	for id, r := range s.runs {
		if r.Agent != agent || !s.firstRunAt(org, id, r, item, r.Station) {
			continue
		}

		if best == nil || r.StartedAt.After(best.StartedAt) || (r.StartedAt.Equal(best.StartedAt) && r.ID > best.ID) {
			best = r
		}
	}

	return best
}

// sentBackAt are the stations of item's rejected and gate_rejected events, in append order.
// Callers hold s.mu.
func (s *Store) sentBackAt(item domain.ItemID) []string {
	var out []string

	for _, e := range s.events {
		if e.ItemID == item && (e.Kind == domain.EventRejected || e.Kind == domain.EventGateRejected) {
			out = append(out, e.Station)
		}
	}

	return out
}

// PastTryInput implements ports.PastTries.
func (s *Store) PastTryInput(ctx context.Context, run domain.RunID) (domain.PastTryInput, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.PastTryInput{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.runs[run]
	if !ok || !s.runVisible(r, org) || r.ItemID == "" {
		return domain.PastTryInput{}, fmt.Errorf("run %s: %w", run, ports.ErrNotFound)
	}

	return domain.PastTryInput{Run: *r, Outputs: s.outputsBefore(r.ItemID, r.StartedAt)}, nil
}

// outputsBefore is item's latest output of each station among its runs that ended before before.
// Callers hold s.mu.
func (s *Store) outputsBefore(item domain.ItemID, before time.Time) map[string]json.RawMessage {
	latest := map[string]*domain.Run{}

	for _, o := range s.runs {
		if o.ItemID != item || o.EndedAt.IsZero() || !o.EndedAt.Before(before) || len(o.Result.Output) == 0 {
			continue
		}

		if l := latest[o.Station]; l == nil || o.EndedAt.After(l.EndedAt) || (o.EndedAt.Equal(l.EndedAt) && o.ID > l.ID) {
			latest[o.Station] = o
		}
	}

	outputs := make(map[string]json.RawMessage, len(latest))
	for st, o := range latest {
		outputs[st] = slices.Clone(o.Result.Output)
	}

	return outputs
}

// hasTryRepository reports whether in has a repository a try can start from.
func hasTryRepository(in domain.RunInput) bool {
	_, ok := in.TryRepository()
	return ok
}
