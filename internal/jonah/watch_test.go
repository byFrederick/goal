package jonah_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/jonah"
)

// history is a WatchDefinitions of one factory whose versions are given.
type history struct {
	versions []domain.VersionReach
}

func (h *history) Factories(context.Context) ([]ports.FactoryRecord, error) {
	return []ports.FactoryRecord{{ID: factory, Current: &ports.VersionRef{ID: h.versions[len(h.versions)-1].ID}}}, nil
}

func (h *history) Reach(context.Context, domain.FactoryID, int) ([]domain.VersionReach, error) {
	return h.versions, nil
}

type watchFixture struct {
	store   *porttest.Store
	clock   *porttest.Clock
	watcher *jonah.Watcher
	history *history
	org     domain.OrgID
}

// ctx is the fixture's organization, as a person.
func (f *watchFixture) ctx(t *testing.T) context.Context {
	t.Helper()

	return domain.WithActor(domain.WithOrg(t.Context(), f.org), domain.LocalUser)
}

const day = 24 * time.Hour

// newWatchFixture is a factory with v1 at t0, v2 a day later reaching build, and v3 two days
// later reaching review only.
func newWatchFixture(t *testing.T) *watchFixture {
	t.Helper()

	clock := porttest.NewClock(t0.Add(5 * day))
	store := porttest.NewStore()
	tn := porttest.NewTenancy(clock.Now).WithStore(store)

	o, err := tn.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}

	store.AddFactory(o.ID, factory)

	h := &history{versions: []domain.VersionReach{
		{ID: versionID(1), Number: 1, PublishedAt: t0, First: true},
		{ID: versionID(2), Number: 2, PublishedAt: t0.Add(day), Stations: []string{stBuild}},
		{ID: versionID(3), Number: 3, PublishedAt: t0.Add(2 * day), Stations: []string{"review"}},
	}}

	return &watchFixture{
		store: store, clock: clock, history: h, org: o.ID,
		watcher: jonah.NewWatcher(jonah.WatchDeps{Orgs: tn, Definitions: h, Proposals: store, Watches: store, Clock: clock}),
	}
}

// outcomes are n outcomes on version, each sent back when sent is, costing usd.
func outcomes(version domain.DefinitionVersionID, n int, sent bool, usd float64) []domain.StationOutcome {
	out := make([]domain.StationOutcome, n)
	for i := range out {
		out[i] = domain.StationOutcome{
			VersionID: version, ItemID: domain.ItemID(string(version) + "-" + string(rune('a'+i%26)) + string(rune('a'+i/26))),
			At: t0, SentBack: sent && i%2 == 0, CostUSD: usd + float64(i%3)/100, LeadTime: time.Hour,
		}
	}

	return out
}

func (f *watchFixture) watches(t *testing.T) map[string]domain.Watch {
	t.Helper()

	got, err := f.store.VersionWatches(f.ctx(t), factory, []domain.DefinitionVersionID{versionID(1), versionID(2), versionID(3)})
	if err != nil {
		t.Fatal(err)
	}

	out := map[string]domain.Watch{}
	for _, w := range got {
		out[string(w.VersionID)+"/"+w.Station+"/"+string(w.Metric)] = w
	}

	return out
}

// A person's publish is watched on the send-back rate and the cost per item at each station it
// reaches. At build, before is v1 and after is v2 and v3, as v3 does not reach build; outcomes
// are counted on the version of their run.
func TestWatch_PersonsPublishAtEachStationItReaches(t *testing.T) {
	t.Parallel()

	f := newWatchFixture(t)

	build := append(outcomes(versionID(1), 40, false, 1), outcomes(versionID(2), 30, true, 2)...)
	build = append(build, outcomes(versionID(3), 30, true, 2)...)
	f.store.SetStationOutcomes(factory, stBuild, build)
	f.store.SetStationOutcomes(factory, "review", outcomes(versionID(3), 5, false, 1))

	if err := f.watcher.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}

	wantPersonsWatches(t, f.watches(t))

	// A second sweep changes nothing: the sweep is idempotent.
	if err := f.watcher.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}

	if again := f.watches(t); len(again) != 4 || again["v2/build/sent_back_rate"].After.N != 60 {
		t.Errorf("after a second sweep = %v, want the same four watches", again)
	}
}

// wantPersonsWatches checks the watches of TestWatch_PersonsPublishAtEachStationItReaches.
func wantPersonsWatches(t *testing.T, got map[string]domain.Watch) {
	t.Helper()

	if len(got) != 4 {
		t.Fatalf("watches = %v, want sent_back_rate and cost_per_item of v2 at build and of v3 at review", got)
	}

	rate := got["v2/build/sent_back_rate"]
	want := domain.Watch{
		FactoryID: factory, VersionID: versionID(2), Station: stBuild, Metric: domain.MetricSentBackRate,
		Before: domain.WatchSide{N: 40}, After: domain.WatchSide{N: 60, Value: 0.5}, Verdict: domain.WatchWorse,
		OpenedAt: t0.Add(day), UpdatedAt: t0.Add(5 * day),
	}
	interval := rate.Interval
	rate.Interval = nil

	if interval == nil || !reflect.DeepEqual(rate, want) {
		t.Errorf("v2's rate at build = %+v (interval %v), want %+v with an interval", rate, interval, want)
	}

	if cost := got["v2/build/cost_per_item"]; cost.Verdict != domain.WatchWorse || cost.After.N != 60 {
		t.Errorf("v2's cost at build = %+v, want worse over 60 after", cost)
	}

	if review := got["v3/review/sent_back_rate"]; review.Before.N != 0 || review.After.N != 5 || review.Verdict != domain.WatchTooEarly {
		t.Errorf("v3's rate at review = %+v, want too early with 0 before and 5 after", review)
	}
}

// A Jonah proposal's publish is watched on its own number only, at its station, and the watch
// names the proposal.
func TestWatch_ProposalWatchesItsOwnNumber(t *testing.T) {
	t.Parallel()

	f := newWatchFixture(t)
	p := publishedProposal(f.ctx(t), t, f.store, versionID(2), domain.ProposalEstimate{Metric: domain.MetricLeadTimeP85, Station: stBuild})
	f.store.SetStationOutcomes(factory, stBuild, append(outcomes(versionID(1), 10, false, 1), outcomes(versionID(2), 10, false, 1)...))

	if err := f.watcher.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}

	got := f.watches(t)
	own, ok := got["v2/build/lead_time_p85"]

	if !ok || own.ProposalID != p.ID || own.Before.N != 10 || own.After.N != 10 || own.Verdict != domain.WatchNoChange {
		t.Errorf("watches = %v, want proposal %s's lead time at build, no change over 10 a side", got, p.ID)
	}

	if _, ok := got["v2/build/sent_back_rate"]; ok {
		t.Errorf("watches = %v, want no person's-publish watch of a proposal's version", got)
	}
}

// A watch still too early at 14 days closes as no clear change, and a later sweep leaves it.
func TestWatch_ClosesAfterFourteenDays(t *testing.T) {
	t.Parallel()

	f := newWatchFixture(t)
	f.store.SetStationOutcomes(factory, stBuild, append(outcomes(versionID(1), 3, false, 1), outcomes(versionID(2), 3, true, 1)...))
	f.clock.Advance(domain.WatchMaxAge - 4*day) // to v2's fourteenth day

	if err := f.watcher.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}

	rate := f.watches(t)["v2/build/sent_back_rate"]
	if rate.Verdict != domain.WatchNoClearChange || rate.ClosedAt.IsZero() {
		t.Fatalf("rate at 14 days = %+v, want no clear change and closed", rate)
	}

	f.store.SetStationOutcomes(factory, stBuild, append(outcomes(versionID(1), 50, false, 1), outcomes(versionID(2), 50, true, 1)...))
	f.clock.Advance(time.Hour)

	if err := f.watcher.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}

	if after := f.watches(t)["v2/build/sent_back_rate"]; after.Verdict != domain.WatchNoClearChange || after.After.N != 3 {
		t.Errorf("closed watch after another sweep = %+v, want it unchanged", after)
	}
}

// publishedProposal stores a proposal made against v1 with estimate and records it published as
// version.
func publishedProposal(ctx context.Context, t *testing.T, store *porttest.Store, version domain.DefinitionVersionID, estimate domain.ProposalEstimate) domain.Proposal {
	t.Helper()

	if _, err := store.QueueJonahReview(ctx, domain.NewJonahReview{
		FactoryID: factory, VersionID: versionID(1), Trigger: domain.JonahManual, WindowStart: t0.Add(-time.Hour), WindowEnd: t0,
		By: domain.LocalUser, At: t0,
	}); err != nil {
		t.Fatal(err)
	}

	claimed, err := store.ClaimJonahReview(ctx, t0, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	_, made, err := store.FinishJonahReview(ctx, claimed, domain.JonahReviewResult{
		State: domain.JonahDone, At: t0,
		Proposals: []domain.NewProposal{{
			Title: "Ask for a screenshot", Estimate: estimate,
			Edits: []domain.ProposalEdit{{Path: agentPath, Content: "# writer\n"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	p, err := store.DecideProposal(ctx, factory, made[0].Number, domain.ProposalDecision{
		State: domain.ProposalPublished, PublishedVersionID: version, By: domain.LocalUser, At: t0,
	})
	if err != nil {
		t.Fatal(err)
	}

	return p
}
