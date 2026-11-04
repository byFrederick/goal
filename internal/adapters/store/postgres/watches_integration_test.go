//go:build integration

package postgres

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/jonah"
)

var watchAt = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

// watchStation is the station the tests' runs work at.
const watchStation = "build"

// watchOrgs serves one organization to the watch sweep.
type watchOrgs struct{ org domain.OrgID }

func (o watchOrgs) ActiveOrgs(context.Context) ([]domain.OrgID, error) {
	return []domain.OrgID{o.org}, nil
}

// watchHistory serves one factory's versions to the watch sweep.
type watchHistory struct {
	factory  domain.FactoryID
	versions []domain.VersionReach
}

func (h watchHistory) Factories(context.Context) ([]ports.FactoryRecord, error) {
	return []ports.FactoryRecord{{ID: h.factory, Current: &ports.VersionRef{ID: h.versions[len(h.versions)-1].ID}}}, nil
}

func (h watchHistory) Reach(context.Context, domain.FactoryID, int) ([]domain.VersionReach, error) {
	return h.versions, nil
}

// watchLine writes an item's runs, sent-back touches and spans at a station straight to the
// tables the outcomes are read from.
type watchLine struct {
	t       *testing.T
	s       *Store
	org     domain.OrgID
	factory domain.FactoryID
}

func (l watchLine) item() domain.ItemID {
	l.t.Helper()

	it, err := l.s.CreateItem(domain.WithOrg(l.t.Context(), l.org), domain.NewItem{
		FactoryID: l.factory, Type: typeTask, Title: "x", Station: watchStation, State: domain.StateQueued, Class: domain.ClassStandard, CreatedAt: watchAt,
	})
	if err != nil {
		l.t.Fatal(err)
	}

	return it.ID
}

func (l watchLine) exec(sql string, args ...any) {
	l.t.Helper()

	if err := inOrgTx(l.t, l.s.pool, l.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(l.t.Context(), sql, args...)

		return err
	}); err != nil {
		l.t.Fatal(err)
	}
}

// run records a run of item at build on version from at, running or finished an hour later.
func (l watchLine) run(item domain.ItemID, version domain.DefinitionVersionID, at time.Time, usd float64, running bool) {
	status, ended := "succeeded", any(at.Add(time.Hour))
	if running {
		status, ended = "running", nil
	}

	l.exec(`INSERT INTO agent_runs (factory_id, item_id, station, agent, model, status, cost_usd, started_at, ended_at, definition_version_id)
		VALUES ($1, $2, 'build', 'engineer', 'claude-sonnet-5-5', $3, $4, $5, $6, $7)`,
		string(l.factory), string(item), status, usd, at, ended, string(version))
}

// sentBack records a person's gate rejection of item at build at at.
func (l watchLine) sentBack(item domain.ItemID, at time.Time) {
	l.exec(`INSERT INTO item_events (factory_id, item_id, at, kind, station, actor, data)
		VALUES ($1, $2, $3, 'gate_rejected', 'build', 'human', '{"actor": "user:local", "reason": "no tests"}')`,
		string(l.factory), string(item), at)
}

// left records item's span at build from entered to left.
func (l watchLine) left(item domain.ItemID, entered, left time.Time) {
	l.exec(`INSERT INTO item_spans (factory_id, item_id, type, class_of_service, station, state, rework_round, entered_at, left_at)
		VALUES ($1, $2, 'task', 'standard', 'build', 'in_progress', 0, $3, $4)`,
		string(l.factory), string(item), entered, left)
}

// Each item's outcome at a station is credited to the version of its run there, even a run that
// started on v1 after v2 was published; a rework run on another version is a separate outcome;
// a running item counts nowhere yet. The sweep's watch of v2 at build then counts v3's outcomes
// after it, as v3 reached review only.
func TestWatches_CreditsRunVersionAndSpansUnrelatedPublishes(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))
	org := newOrgID(t, s)
	ctx := domain.WithOrg(t.Context(), org)
	factory := saveFactory(ctx, t, s)
	v1, v2, v3 := newPublishedVersion(t, s.pool, org, factory), newPublishedVersion(t, s.pool, org, factory), newPublishedVersion(t, s.pool, org, factory)
	l := watchLine{t: t, s: s, org: org, factory: factory}
	h := func(n int) time.Time { return watchAt.Add(time.Duration(n) * time.Hour) }

	a, b, late, c, running := l.item(), l.item(), l.item(), l.item(), l.item()

	l.run(a, v1, h(0), 1, false) // sent back on v1
	l.sentBack(a, h(2))
	l.left(a, h(0), h(2))
	l.run(b, v1, h(1), 1, false)
	l.left(b, h(1), h(3))
	l.run(late, v1, h(30), 1, false) // pinned to v1, started after v2's publish
	l.left(late, h(30), h(32))
	l.run(c, v2, h(40), 2, false) // sent back on v2, reworked on v3
	l.sentBack(c, h(42))
	l.left(c, h(40), h(42))
	l.run(c, v3, h(60), 3, false)
	l.left(c, h(60), h(62))
	l.run(running, v2, h(41), 2, true)

	wantOutcomes(ctx, t, s, factory, []domain.DefinitionVersionID{v1, v2, v3}, []domain.ItemID{a, b, late, c})

	clock := porttest.NewClock(watchAt.Add(5 * 24 * time.Hour))
	history := watchHistory{factory: factory, versions: []domain.VersionReach{
		{ID: v1, Number: 1, PublishedAt: watchAt.Add(-time.Hour), First: true},
		{ID: v2, Number: 2, PublishedAt: watchAt.Add(24 * time.Hour), Stations: []string{watchStation}},
		{ID: v3, Number: 3, PublishedAt: watchAt.Add(48 * time.Hour), Stations: []string{"review"}},
	}}
	w := jonah.NewWatcher(jonah.WatchDeps{Orgs: watchOrgs{org}, Definitions: history, Proposals: s, Watches: s, Clock: clock})

	if err := w.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}

	wantSweptRate(ctx, t, s, factory, []domain.DefinitionVersionID{v1, v2, v3})

	// Another organization sees none of them.
	other := domain.WithOrg(t.Context(), newOrgID(t, s))
	if theirs, err := s.VersionWatches(other, factory, []domain.DefinitionVersionID{v1, v2, v3}); err != nil || len(theirs) != 0 {
		t.Errorf("another organization's watches = %+v, %v; want none", theirs, err)
	}
}

// wantOutcomes checks the outcomes at build of TestWatches_CreditsRunVersionAndSpansUnrelatedPublishes:
// items a, b and late on v1, c on v2 (sent back) and again on v3.
func wantOutcomes(ctx context.Context, t *testing.T, s *Store, factory domain.FactoryID, versions []domain.DefinitionVersionID, items []domain.ItemID) {
	t.Helper()

	got, err := s.StationOutcomes(ctx, factory, watchStation, versions)
	if err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		version  domain.DefinitionVersionID
		item     domain.ItemID
		sentBack bool
		cost     float64
	}

	var short []outcome
	for _, o := range got {
		short = append(short, outcome{o.VersionID, o.ItemID, o.SentBack, o.CostUSD})
	}

	want := []outcome{
		{versions[0], items[0], true, 1},
		{versions[0], items[1], false, 1},
		{versions[0], items[2], false, 1},
		{versions[1], items[3], true, 2},
		{versions[2], items[3], false, 3},
	}
	if !slices.Equal(short, want) {
		t.Fatalf("outcomes = %+v, want %+v", short, want)
	}

	if got[0].LeadTime != 2*time.Hour {
		t.Errorf("a's lead time = %s, want 2h", got[0].LeadTime)
	}
}

// wantSweptRate checks the sweep's watch of v2's send-back rate at build: v1's three items
// before, v2's and v3's after.
func wantSweptRate(ctx context.Context, t *testing.T, s *Store, factory domain.FactoryID, versions []domain.DefinitionVersionID) {
	t.Helper()

	watches, err := s.VersionWatches(ctx, factory, versions)
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(watches, func(w domain.Watch) bool {
		return w.VersionID == versions[1] && w.Station == watchStation && w.Metric == domain.MetricSentBackRate
	})
	if i < 0 {
		t.Fatalf("watches = %+v, want v2's send-back rate at build", watches)
	}

	if rate := watches[i]; rate.Before.N != 3 || rate.After.N != 2 || rate.After.Value != 0.5 || rate.Interval == nil ||
		rate.Verdict != domain.WatchTooEarly || !rate.ClosedAt.IsZero() {
		t.Errorf("v2's rate at build = %+v, want 3 before, 2 after (v2 and v3) at 0.5, too early and open", rate)
	}

	if len(watches) != 4 {
		t.Errorf("watches = %d, want v2's two at build and v3's two at review", len(watches))
	}
}

// A watch of a proposal's own number that is worse comes to the inbox's read until an undo of
// the proposal is written; a closed watch is never written again.
func TestWatches_WorseUntilUndone(t *testing.T) {
	t.Parallel()

	s := New(newTestPool(t))
	org := newOrgID(t, s)
	ctx := domain.WithOrg(t.Context(), org)
	factory, _ := seedJonah(ctx, t, s)

	p, err := s.Proposal(ctx, factory, 1)
	if err != nil {
		t.Fatal(err)
	}

	worse := domain.Watch{
		FactoryID: factory, VersionID: p.VersionID, ProposalID: p.ID, Station: watchStation, Metric: domain.MetricSentBackRate,
		Before: domain.WatchSide{N: 50, Value: 0.1}, After: domain.WatchSide{N: 50, Value: 0.4},
		Interval: &domain.WatchInterval{Low: 0.15, High: 0.45}, Verdict: domain.WatchWorse, OpenedAt: watchAt, UpdatedAt: watchAt,
		ClosedAt: watchAt,
	}

	if saved, err := s.SaveWatch(ctx, worse); err != nil || !saved {
		t.Fatalf("SaveWatch = %v, %v", saved, err)
	}

	worse.Verdict = domain.WatchHelped
	if saved, err := s.SaveWatch(ctx, worse); err != nil || saved {
		t.Errorf("SaveWatch of a closed watch = %v, %v; want not saved", saved, err)
	}

	if list, err := s.WorseProposalWatches(ctx, 10); err != nil || len(list) != 1 || list[0].ProposalNumber != 1 ||
		list[0].Verdict != domain.WatchWorse {
		t.Fatalf("worse watches = %+v, %v; want proposal 1's", list, err)
	}

	if _, err := s.UndoProposal(ctx, factory, p.ID, p.VersionID, domain.NewProposal{
		Title: "Undo proposal 1: a", Edits: []domain.ProposalEdit{{Path: ruleA, Replacements: []domain.Replacement{{Old: "x", New: ""}}}},
	}, watchAt); err != nil {
		t.Fatal(err)
	}

	if list, err := s.WorseProposalWatches(ctx, 10); err != nil || len(list) != 0 {
		t.Errorf("worse watches after the undo = %+v, %v; want none", list, err)
	}
}
