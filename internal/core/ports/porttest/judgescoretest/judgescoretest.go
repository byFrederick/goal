// Package judgescoretest is the conformance suite for ports.JudgeScores (ADR 0025 decision 2).
// The in-memory fake in porttest runs it, and the PostgreSQL adapter runs it with the integration
// tag, so tests on the fake hold for the database's rules: a run scored at most once per judge,
// claims that skip held scores and take back ended leases, records fenced by the lease's token,
// the recent list with each item's number and title, the 30-day summary per judge, and
// organizations kept apart.
package judgescoretest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Harness is one JudgeScores under test.
type Harness struct {
	JudgeScores ports.JudgeScores
	// Store holds the items and runs the scores name.
	Store ports.Store
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
	// NewFactory returns a factory of org the store accepts items for.
	NewFactory func(t *testing.T, org domain.OrgID) domain.FactoryID
	// NewVersion returns a published definition version of factory.
	NewVersion func(t *testing.T, org domain.OrgID, factory domain.FactoryID) domain.DefinitionVersionID
	// Hold holds score id of org, as another goald's claim transaction would, until release.
	Hold func(t *testing.T, org domain.OrgID, id domain.JudgeScoreID) (release func())
}

// Run runs the suite. Tests run in parallel and call newHarness once each; harnesses must be
// isolated from each other.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(t *testing.T, f *fixture)
	}{
		{"Enqueue_Queued", testEnqueue},
		{"Enqueue_SameJudgeAndRunIsOneScore", testEnqueueTwice},
		{"Enqueue_RulesRefused", testEnqueueRules},
		{"Claim_OldestDueFirst", testClaimOrder},
		{"Claim_TakesBackAnEndedLease", testClaimExpired},
		{"Claim_SkipsAHeldScore", testClaimSkipsHeld},
		{"Record_ScoredAndError", testRecord},
		{"Record_RefusedOnceTheLeaseIsLost", testRecordLeaseLost},
		{"Record_RulesRefused", testRecordRules},
		{"Recent_NewestFirstWithTheItem", testRecent},
		{"Summary_CountsPassRateCostAndDays", testSummary},
		{"OtherOrg_SeesAndClaimsNothing", testIsolation},
		{"Methods_NoOrgIsAnError", testNoOrg},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			org := h.NewOrg(t)
			factory := h.NewFactory(t, org)
			tc.test(t, &fixture{Harness: h, org: org, factory: factory, version: h.NewVersion(t, org, factory)})
		})
	}
}

// Names and times the suite uses. Times are whole seconds: stores may keep microseconds only.
const (
	judgeA  = "spec-fidelity"
	judgeB  = "test-quality"
	station = "work"
	agent   = "backend"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// fixture is one test's organization, factory and version.
type fixture struct {
	Harness

	org     domain.OrgID
	factory domain.FactoryID
	version domain.DefinitionVersionID
}

func (f *fixture) ctx(t *testing.T) context.Context {
	t.Helper()

	return domain.WithOrg(t.Context(), f.org)
}

// item creates an item of factory titled title.
func (f *fixture) item(t *testing.T, factory domain.FactoryID, title string) domain.Item {
	t.Helper()

	it, err := f.Store.CreateItem(f.ctx(t), domain.NewItem{
		FactoryID: factory, Type: "task", Title: title, Station: station, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: t0.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	return it
}

// run starts a run of it.
func (f *fixture) run(t *testing.T, it domain.Item) domain.RunID {
	t.Helper()

	id, err := f.Store.StartRun(f.ctx(t), ports.NewRun{
		FactoryID: it.FactoryID, DefinitionVersionID: f.version, ItemID: it.ID, Station: station, Agent: agent,
		Model: "claude-test", StartedAt: t0.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	return id
}

// newScore is judge's score of a new run of a new item of the fixture's factory, queued at at.
func (f *fixture) newScore(t *testing.T, judge string, at time.Time) domain.NewJudgeScore {
	t.Helper()

	it := f.item(t, f.factory, "item for "+judge)

	return domain.NewJudgeScore{FactoryID: f.factory, VersionID: f.version, Judge: judge, RunID: f.run(t, it), ItemID: it.ID, At: at}
}

func (f *fixture) enqueue(t *testing.T, n domain.NewJudgeScore) domain.JudgeScore {
	t.Helper()

	s, err := f.JudgeScores.Enqueue(f.ctx(t), n)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	return s
}

func (f *fixture) claim(t *testing.T, now, until time.Time) *domain.JudgeScoreClaim {
	t.Helper()

	c, err := f.JudgeScores.Claim(f.ctx(t), now, until)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	return c
}

// claimID claims at now for a minute and fails unless it got score want.
func (f *fixture) claimID(t *testing.T, now time.Time, want domain.JudgeScoreID) domain.JudgeScoreClaim {
	t.Helper()

	c := f.claim(t, now, now.Add(time.Minute))
	if c == nil || c.Score.ID != want {
		t.Fatalf("Claim at %v = %+v; want score %s", now, c, want)
	}

	return *c
}

func (f *fixture) record(t *testing.T, l domain.JudgeScoreLease, r domain.JudgeResult) domain.JudgeScore {
	t.Helper()

	s, err := f.JudgeScores.Record(f.ctx(t), l, r)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	return s
}

// scored is a scored result at at.
func scored(at time.Time, score float64, passed bool, cost float64) domain.JudgeResult {
	outcome := "faithful"
	if !passed {
		outcome = "drifted"
	}

	return domain.JudgeResult{
		State: domain.JudgeScoreScored, Outcome: outcome, Score: score, Passed: passed, Reason: "One sentence.",
		CostUSD: cost, JudgeRunID: "00000000-0000-7000-8000-0000000000d1", At: at,
	}
}

// score takes n to scored with r: enqueued, claimed at r.At less a second and recorded.
func (f *fixture) score(t *testing.T, n domain.NewJudgeScore, r domain.JudgeResult) domain.JudgeScore {
	t.Helper()

	s := f.enqueue(t, n)
	c := f.claimID(t, r.At.Add(-time.Second), s.ID)

	return f.record(t, c.Lease, r)
}

func testEnqueue(t *testing.T, f *fixture) {
	n := f.newScore(t, judgeA, t0)
	s := f.enqueue(t, n)

	if s.ID == "" || s.OrgID != f.org || s.FactoryID != f.factory || s.VersionID != f.version || s.Judge != judgeA ||
		s.RunID != n.RunID || s.ItemID != n.ItemID || s.State != domain.JudgeScoreQueued || !s.CreatedAt.Equal(t0) ||
		!s.ScoredAt.IsZero() || !s.LeaseUntil.IsZero() {
		t.Errorf("Enqueue = %+v; want a queued score of %+v in org %s", s, n, f.org)
	}

	try := f.enqueue(t, domain.NewJudgeScore{FactoryID: f.factory, VersionID: f.version, Judge: judgeA, At: t0})
	if try.RunID != "" || try.ItemID != "" || try.State != domain.JudgeScoreQueued {
		t.Errorf("Enqueue of a try = %+v; want a queued score with no run or item", try)
	}
}

func testEnqueueTwice(t *testing.T, f *fixture) {
	n := f.newScore(t, judgeA, t0)
	first := f.enqueue(t, n)

	again := n
	again.At = t0.Add(time.Hour)

	if s := f.enqueue(t, again); s.ID != first.ID || !s.CreatedAt.Equal(t0) {
		t.Errorf("second Enqueue = %+v; want the first score %s, queued at %v", s, first.ID, t0)
	}

	c := f.claimID(t, t0, first.ID)
	f.record(t, c.Lease, scored(t0.Add(time.Second), 1, true, 0.25))

	if s := f.enqueue(t, again); s.ID != first.ID || s.State != domain.JudgeScoreScored {
		t.Errorf("Enqueue after the score = %+v; want the scored score %s", s, first.ID)
	}

	other := n
	other.Judge = judgeB

	if s := f.enqueue(t, other); s.ID == first.ID {
		t.Errorf("Enqueue by another judge = the first score; want a score of its own")
	}

	try := domain.NewJudgeScore{FactoryID: f.factory, VersionID: f.version, Judge: judgeA, ItemID: n.ItemID, At: t0}
	if a, b := f.enqueue(t, try), f.enqueue(t, try); a.ID == b.ID {
		t.Errorf("two tries = one score %s; want two", a.ID)
	}
}

func testEnqueueRules(t *testing.T, f *fixture) {
	n := f.newScore(t, judgeA, t0)

	bad := n
	bad.Judge = "Not A Slug"

	if _, err := f.JudgeScores.Enqueue(f.ctx(t), bad); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("Enqueue with a bad slug = %v; want ErrInvalid", err)
	}

	unknown := n
	unknown.ItemID = "00000000-0000-7000-8000-00000000ffff"
	unknown.RunID = ""

	if _, err := f.JudgeScores.Enqueue(f.ctx(t), unknown); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Enqueue of an unknown item = %v; want ErrNotFound", err)
	}

	unknown = n
	unknown.RunID = "00000000-0000-7000-8000-00000000fffe"

	if _, err := f.JudgeScores.Enqueue(f.ctx(t), unknown); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Enqueue of an unknown run = %v; want ErrNotFound", err)
	}
}

func testClaimOrder(t *testing.T, f *fixture) {
	b := f.enqueue(t, f.newScore(t, judgeA, t0.Add(time.Minute)))
	a := f.enqueue(t, f.newScore(t, judgeB, t0))

	until := t0.Add(5 * time.Minute)

	c := f.claim(t, t0, until)
	if c == nil || c.Score.ID != a.ID || c.Score.State != domain.JudgeScoreRunning || !c.Score.LeaseUntil.Equal(until) ||
		c.Lease.ID != a.ID || c.Lease.Token == "" || !c.Lease.Until.Equal(until) {
		t.Fatalf("first Claim = %+v; want the oldest score %s running until %v", c, a.ID, until)
	}

	if c.Score.Judge != judgeB || c.Score.RunID != a.RunID || c.Score.VersionID != f.version {
		t.Errorf("claimed score = %+v; want the fields Enqueue stored", c.Score)
	}

	f.claimID(t, t0, b.ID)

	if c := f.claim(t, t0, until); c != nil {
		t.Errorf("third Claim = %+v; want nil, nothing is due", c)
	}
}

func testClaimExpired(t *testing.T, f *fixture) {
	s := f.enqueue(t, f.newScore(t, judgeA, t0))
	first := f.claimID(t, t0, s.ID)

	if c := f.claim(t, first.Lease.Until.Add(-time.Second), first.Lease.Until.Add(time.Minute)); c != nil {
		t.Errorf("Claim before the lease ends = %+v; want nil", c)
	}

	second := f.claimID(t, first.Lease.Until, s.ID)
	if second.Lease.Token == first.Lease.Token {
		t.Errorf("the claim after the lease ended kept token %s; want a new one", first.Lease.Token)
	}

	at := first.Lease.Until.Add(time.Second)
	if _, err := f.JudgeScores.Record(f.ctx(t), first.Lease, scored(at, 1, true, 0.25)); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("Record under the first lease = %v; want ErrLeaseLost", err)
	}

	if got := f.record(t, second.Lease, scored(at, 1, true, 0.25)); got.State != domain.JudgeScoreScored {
		t.Errorf("Record under the second lease = %+v; want scored", got)
	}
}

func testClaimSkipsHeld(t *testing.T, f *fixture) {
	a := f.enqueue(t, f.newScore(t, judgeA, t0))
	b := f.enqueue(t, f.newScore(t, judgeA, t0.Add(time.Minute)))

	release := f.Hold(t, f.org, a.ID)
	f.claimID(t, t0, b.ID)
	release()
	f.claimID(t, t0, a.ID)
}

func testRecord(t *testing.T, f *fixture) {
	n := f.newScore(t, judgeA, t0)
	s := f.enqueue(t, n)
	c := f.claimID(t, t0, s.ID)

	r := scored(t0.Add(30*time.Second), 0.5, false, 0.25)
	got := f.record(t, c.Lease, r)

	if got.ID != s.ID || got.State != domain.JudgeScoreScored || got.Outcome != r.Outcome || got.Score != r.Score ||
		got.Passed || got.Reason != r.Reason || got.CostUSD != r.CostUSD || got.JudgeRunID != r.JudgeRunID ||
		!got.ScoredAt.Equal(r.At) || !got.LeaseUntil.IsZero() || got.RunID != n.RunID || !got.CreatedAt.Equal(t0) {
		t.Errorf("Record = %+v; want %+v on score %s with its lease released", got, r, s.ID)
	}

	failed := f.enqueue(t, f.newScore(t, judgeA, t0))
	c = f.claimID(t, t0, failed.ID)
	e := domain.JudgeResult{State: domain.JudgeScoreError, Reason: "The box did not start.", CostUSD: 0.5, At: t0.Add(time.Second)}

	if got := f.record(t, c.Lease, e); got.State != domain.JudgeScoreError || got.Outcome != "" || got.Score != 0 || got.Passed ||
		got.Reason != e.Reason || got.CostUSD != e.CostUSD || !got.ScoredAt.Equal(e.At) {
		t.Errorf("Record of an error = %+v; want %+v", got, e)
	}
}

func testRecordLeaseLost(t *testing.T, f *fixture) {
	s := f.enqueue(t, f.newScore(t, judgeA, t0))
	c := f.claimID(t, t0, s.ID)

	if _, err := f.JudgeScores.Record(f.ctx(t), c.Lease, scored(c.Lease.Until, 1, true, 0)); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("Record when the lease ended = %v; want ErrLeaseLost", err)
	}

	forged := c.Lease
	forged.Token = "00000000-0000-4000-8000-000000000000"

	if _, err := f.JudgeScores.Record(f.ctx(t), forged, scored(t0.Add(time.Second), 1, true, 0)); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("Record under another token = %v; want ErrLeaseLost", err)
	}

	f.record(t, c.Lease, scored(t0.Add(time.Second), 1, true, 0))

	if _, err := f.JudgeScores.Record(f.ctx(t), c.Lease, scored(t0.Add(2*time.Second), 0, false, 0)); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("second Record = %v; want ErrLeaseLost, the score is no longer running", err)
	}

	queued := f.enqueue(t, f.newScore(t, judgeA, t0))
	never := domain.JudgeScoreLease{ID: queued.ID, Token: c.Lease.Token, Until: c.Lease.Until}

	if _, err := f.JudgeScores.Record(f.ctx(t), never, scored(t0.Add(time.Second), 1, true, 0)); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("Record of a queued score = %v; want ErrLeaseLost", err)
	}
}

func testRecordRules(t *testing.T, f *fixture) {
	s := f.enqueue(t, f.newScore(t, judgeA, t0))
	c := f.claimID(t, t0, s.ID)

	if _, err := f.JudgeScores.Record(f.ctx(t), c.Lease, scored(t0.Add(time.Second), 1.5, true, 0)); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("Record of score 1.5 = %v; want ErrInvalid", err)
	}

	if got := f.record(t, c.Lease, scored(t0.Add(time.Second), 1, true, 0)); got.State != domain.JudgeScoreScored {
		t.Errorf("Record after a refused one = %+v; want scored, the refusal changed nothing", got)
	}
}

func testRecent(t *testing.T, f *fixture) {
	var want []domain.JudgeScore

	for i := range 3 {
		it := f.item(t, f.factory, fmt.Sprintf("item %d", i))
		s := f.enqueue(t, domain.NewJudgeScore{
			FactoryID: f.factory, VersionID: f.version, Judge: judgeA, RunID: f.run(t, it), ItemID: it.ID, At: t0.Add(time.Duration(i) * time.Minute),
		})
		s.ItemNumber, s.ItemTitle = it.Number, it.Title
		want = append([]domain.JudgeScore{s}, want...)
	}

	c := f.claimID(t, t0.Add(time.Hour), want[2].ID)
	number, title := want[2].ItemNumber, want[2].ItemTitle
	want[2] = f.record(t, c.Lease, scored(t0.Add(time.Hour+time.Second), 1, true, 0.25))
	want[2].ItemNumber, want[2].ItemTitle = number, title

	f.enqueue(t, f.newScore(t, judgeB, t0.Add(time.Hour)))
	f.enqueue(t, domain.NewJudgeScore{FactoryID: f.factory, VersionID: f.version, Judge: judgeA, At: t0.Add(time.Hour)})

	other := f.NewFactory(t, f.org)
	oit := f.item(t, other, "elsewhere")
	f.enqueue(t, domain.NewJudgeScore{FactoryID: other, VersionID: f.version, Judge: judgeA, RunID: f.run(t, oit), ItemID: oit.ID, At: t0.Add(time.Hour)})

	got, err := f.JudgeScores.Recent(f.ctx(t), f.factory, judgeA, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != len(want) {
		t.Fatalf("Recent = %d scores %+v; want %d, the judge's production scores in the factory", len(got), got, len(want))
	}

	for i := range want {
		if g, w := got[i], want[i]; g.ID != w.ID || g.ItemNumber != w.ItemNumber || g.ItemTitle != w.ItemTitle || g.State != w.State ||
			!g.CreatedAt.Equal(w.CreatedAt) || g.Score != w.Score || g.Outcome != w.Outcome {
			t.Errorf("Recent[%d] = %+v; want %+v", i, g, w)
		}
	}

	if got, err := f.JudgeScores.Recent(f.ctx(t), f.factory, judgeA, 2); err != nil || len(got) != 2 || got[0].ID != want[0].ID {
		t.Errorf("Recent with limit 2 = %+v, %v; want the newest 2", got, err)
	}
}

func testSummary(t *testing.T, f *fixture) {
	now := t0.Add(10 * 24 * time.Hour)
	day1, day2 := t0, t0.Add(24*time.Hour)

	f.score(t, f.newScore(t, judgeA, day1), scored(day1.Add(time.Minute), 1, true, 0.25))
	f.score(t, f.newScore(t, judgeA, day1.Add(time.Hour)), scored(day1.Add(2*time.Hour), 0, false, 0.25))
	f.score(t, f.newScore(t, judgeA, day2), scored(day2.Add(time.Minute), 1, true, 0.5))

	// An error on a third day costs but is no point of the series.
	failed := f.enqueue(t, f.newScore(t, judgeA, day2.Add(24*time.Hour)))
	c := f.claimID(t, day2.Add(24*time.Hour), failed.ID)
	f.record(t, c.Lease, domain.JudgeResult{State: domain.JudgeScoreError, Reason: "No box.", CostUSD: 0.5, At: day2.Add(24*time.Hour + 30*time.Second)})

	old := now.Add(-31 * 24 * time.Hour)
	f.score(t, f.newScore(t, judgeA, old), scored(old.Add(time.Minute), 1, true, 4)) // too old
	f.score(t, f.newScore(t, judgeB, day2), scored(day2.Add(time.Minute), 0, false, 0.25))

	tryN := domain.NewJudgeScore{FactoryID: f.factory, VersionID: f.version, Judge: judgeA, At: day1}
	f.score(t, tryN, scored(day1.Add(time.Minute), 0, false, 8)) // a try is no production score

	other := f.NewFactory(t, f.org)
	oit := f.item(t, other, "elsewhere")
	f.score(t, domain.NewJudgeScore{FactoryID: other, VersionID: f.version, Judge: judgeA, RunID: f.run(t, oit), ItemID: oit.ID, At: day1},
		scored(day1.Add(time.Minute), 0, false, 16))

	// Last, so the claims above never take it: still queued, it counts for nothing.
	f.enqueue(t, f.newScore(t, judgeA, day2))

	got, err := f.JudgeScores.Summary(f.ctx(t), f.factory, now)
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.JudgeSummary{
		{Judge: judgeA, Scored: 3, Passed: 2, PassRate: 2.0 / 3, CostUSD: 1.5, Days: []domain.JudgeDay{
			{Day: day1.Truncate(24 * time.Hour), Scored: 2, Passed: 1, PassRate: 0.5},
			{Day: day2.Truncate(24 * time.Hour), Scored: 1, Passed: 1, PassRate: 1},
		}},
		{Judge: judgeB, Scored: 1, Passed: 0, PassRate: 0, CostUSD: 0.25, Days: []domain.JudgeDay{
			{Day: day2.Truncate(24 * time.Hour), Scored: 1, Passed: 0, PassRate: 0},
		}},
	}

	if len(got) != len(want) {
		t.Fatalf("Summary = %+v; want %+v", got, want)
	}

	for i := range want {
		g, w := got[i], want[i]
		if g.Judge != w.Judge || g.Scored != w.Scored || g.Passed != w.Passed || g.PassRate != w.PassRate || g.CostUSD != w.CostUSD ||
			len(g.Days) != len(w.Days) {
			t.Errorf("Summary[%d] = %+v; want %+v", i, g, w)

			continue
		}

		for j := range w.Days {
			if gd, wd := g.Days[j], w.Days[j]; !gd.Day.Equal(wd.Day) || gd.Scored != wd.Scored || gd.Passed != wd.Passed || gd.PassRate != wd.PassRate {
				t.Errorf("Summary[%d].Days[%d] = %+v; want %+v", i, j, gd, wd)
			}
		}
	}
}

func testIsolation(t *testing.T, f *fixture) {
	n := f.newScore(t, judgeA, t0)
	s := f.enqueue(t, n)
	c := f.claimID(t, t0, s.ID)
	f.record(t, c.Lease, scored(t0.Add(time.Second), 1, true, 0.25))
	queued := f.enqueue(t, f.newScore(t, judgeA, t0))

	other := domain.WithOrg(t.Context(), f.NewOrg(t))

	if got, err := f.JudgeScores.Claim(other, t0, t0.Add(time.Minute)); err != nil || got != nil {
		t.Errorf("another org's Claim = %+v, %v; want nil, nil", got, err)
	}

	if got, err := f.JudgeScores.Recent(other, f.factory, judgeA, 10); err != nil || len(got) != 0 {
		t.Errorf("another org's Recent = %+v, %v; want none", got, err)
	}

	if got, err := f.JudgeScores.Summary(other, f.factory, t0.Add(time.Hour)); err != nil || len(got) != 0 {
		t.Errorf("another org's Summary = %+v, %v; want none", got, err)
	}

	if _, err := f.JudgeScores.Enqueue(other, n); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("another org's Enqueue of this org's run = %v; want ErrNotFound", err)
	}

	if _, err := f.JudgeScores.Record(other, domain.JudgeScoreLease{ID: queued.ID, Token: c.Lease.Token, Until: c.Lease.Until},
		scored(t0.Add(time.Second), 1, true, 0)); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("another org's Record = %v; want ErrLeaseLost", err)
	}

	f.claimID(t, t0, queued.ID)
}

func testNoOrg(t *testing.T, f *fixture) {
	ctx := t.Context()
	n := f.newScore(t, judgeA, t0)

	_, err := f.JudgeScores.Enqueue(ctx, n)
	errs := []error{err}
	_, err = f.JudgeScores.Claim(ctx, t0, t0.Add(time.Minute))
	errs = append(errs, err)
	_, err = f.JudgeScores.Record(ctx, domain.JudgeScoreLease{ID: "x"}, scored(t0, 1, true, 0))
	errs = append(errs, err)
	_, err = f.JudgeScores.Recent(ctx, f.factory, judgeA, 1)
	errs = append(errs, err)
	_, err = f.JudgeScores.Summary(ctx, f.factory, t0)
	errs = append(errs, err)

	for i, err := range errs {
		if !errors.Is(err, domain.ErrNoOrg) {
			t.Errorf("method %d without an org = %v; want ErrNoOrg", i, err)
		}
	}
}
