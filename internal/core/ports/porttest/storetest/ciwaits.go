package storetest

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ciWaitsTests are the cases of CI waits and their shared commit polls (ADR 0044 decision 9).
var ciWaitsTests = []struct {
	name string
	test func(f *fixture)
}{
	{"CIWaits_TwoWaitsShareOnePoll", testCIWaitsShareOnePoll},
	{"CIWaits_LeaseHeldUntilExpiry", testCIWaitsLeaseHeldUntilExpiry},
	{"CIWaits_SupersededOnNewSHA", testCIWaitsSupersededOnNewSHA},
	{"CIWaits_CountRounds", testCIWaitsCountRounds},
	{"CIWaits_OneOpenPerVisitAndRepository", testCIWaitsOneOpen},
	{"CIWaits_EndedWaitsLeaveThePollUnleased", testCIWaitsEnded},
}

// The repository, commits and stations the CI wait cases use.
const (
	ciRepo, ciHost         = "github.com/acme/app", "github.com"
	ciSHA, ciNewSHA        = "c0ffee30", "c0ffee31"
	ciStation, ciVisit     = "review", "visit-1"
	ciOwner, ciOtherOwner  = "goald-a", "goald-b"
	ciLease                = time.Minute
	ciChecks               = `{"checks": [{"name": "test", "state": "finished", "result": "passed"}]}`
)

// ciWait is a new before-run wait of item on ciRepo at sha, round 1 of ciVisit, started at t1.
func ciWait(item domain.ItemID, sha string) domain.NewCIWait {
	return domain.NewCIWait{
		Item: item, Station: ciStation, Visit: ciVisit, Repo: ciRepo, SHA: sha, Phase: domain.CIPhaseBefore, Round: 1, StartedAt: t1,
	}
}

// openCIWait opens n or fails the test.
func (f *fixture) openCIWait(t *testing.T, n domain.NewCIWait) domain.CIWait {
	t.Helper()

	w, err := f.Store.OpenCIWait(f.ctx(t), n)
	if err != nil {
		t.Fatalf("OpenCIWait(%s, %s) = %v", n.Item, n.SHA, err)
	}

	return w
}

// leaseCIPolls leases the due polls at now for owner or fails the test.
func (f *fixture) leaseCIPolls(t *testing.T, now time.Time, owner string) []domain.CIPoll {
	t.Helper()

	polls, err := f.Store.LeaseDueCIPolls(f.ctx(t), now, owner, ciLease, 10)
	if err != nil {
		t.Fatalf("LeaseDueCIPolls(%s) = %v", owner, err)
	}

	return polls
}

// openCIWaits reads the item's open waits or fails the test.
func (f *fixture) openCIWaits(t *testing.T, item domain.ItemID) []domain.CIWait {
	t.Helper()

	ws, err := f.Store.OpenCIWaits(f.ctx(t), item)
	if err != nil {
		t.Fatalf("OpenCIWaits(%s) = %v", item, err)
	}

	return ws
}

// Two items waiting on one commit share one poll: one lease gets it once, and the checks it
// records show on both items' waits.
func testCIWaitsShareOnePoll(f *fixture) {
	t := f.t

	a, b := f.create(t, domain.NewItem{}), f.create(t, domain.NewItem{})
	wa := f.openCIWait(t, ciWait(a.ID, ciSHA))
	f.openCIWait(t, ciWait(b.ID, ciSHA))

	if wa.ID == "" || !wa.Open() || wa.Repo != ciRepo || wa.SHA != ciSHA || wa.Round != 1 {
		t.Fatalf("OpenCIWait = %+v; want an open wait with an id on %s at %s, round 1", wa, ciRepo, ciSHA)
	}

	polls := f.leaseCIPolls(t, t1, ciOwner)
	want := domain.CIPollKey{Host: ciHost, Repo: ciRepo, SHA: ciSHA}

	if len(polls) != 1 || polls[0].CIPollKey != want {
		t.Fatalf("LeaseDueCIPolls = %+v; want the one poll %+v", polls, want)
	}

	if p := polls[0]; p.LeaseOwner != ciOwner || !p.LeaseUntil.Equal(t1.Add(ciLease)) || p.Interval != domain.CIPollMinInterval {
		t.Errorf("leased poll = %+v; want owner %s until %s every %s", p, ciOwner, t1.Add(ciLease), domain.CIPollMinInterval)
	}

	seen, next := t1.Add(5*time.Second), t1.Add(15*time.Second)
	r := domain.CIPollResult{
		Checks: json.RawMessage(ciChecks), ETag: `"v1"`, FirstCheckSeenAt: seen, NextPollAt: next, Interval: domain.CIPollMinInterval,
	}

	if err := f.Store.RecordCIPoll(f.ctx(t), want, ciOwner, r); err != nil {
		t.Fatalf("RecordCIPoll = %v", err)
	}

	for _, it := range []domain.Item{a, b} {
		ws := f.openCIWaits(t, it.ID)
		if len(ws) != 1 {
			t.Fatalf("OpenCIWaits(%s) = %d waits; want 1", it.ID, len(ws))
		}

		p := ws[0].Poll
		if !jsonEqual(p.Checks, json.RawMessage(ciChecks)) || p.ETag != `"v1"` || !p.FirstCheckSeenAt.Equal(seen) || !p.NextPollAt.Equal(next) {
			t.Errorf("OpenCIWaits(%s) poll = %+v; want the recorded checks, etag, first check at %s and next poll at %s", it.ID, p, seen, next)
		}

		if p.LeaseOwner != "" || !p.LeaseUntil.IsZero() {
			t.Errorf("OpenCIWaits(%s) poll lease = %s until %s; want released after RecordCIPoll", it.ID, p.LeaseOwner, p.LeaseUntil)
		}
	}

	if got := f.leaseCIPolls(t, next.Add(-time.Second), ciOwner); len(got) != 0 {
		t.Errorf("LeaseDueCIPolls before the next poll = %+v; want none", got)
	}
}

// A poll leased by one owner is not leased by another until its lease ends, and a record by an
// owner that lost it is ErrLeaseLost.
func testCIWaitsLeaseHeldUntilExpiry(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	f.openCIWait(t, ciWait(it.ID, ciSHA))

	if got := f.leaseCIPolls(t, t1, ciOwner); len(got) != 1 {
		t.Fatalf("first LeaseDueCIPolls = %d polls; want 1", len(got))
	}

	if got := f.leaseCIPolls(t, t1.Add(30*time.Second), ciOtherOwner); len(got) != 0 {
		t.Fatalf("LeaseDueCIPolls while leased = %+v; want none", got)
	}

	after := t1.Add(ciLease + time.Second)

	got := f.leaseCIPolls(t, after, ciOtherOwner)
	if len(got) != 1 || got[0].LeaseOwner != ciOtherOwner || !got[0].LeaseUntil.Equal(after.Add(ciLease)) {
		t.Fatalf("LeaseDueCIPolls after the lease ended = %+v; want the poll leased to %s", got, ciOtherOwner)
	}

	r := domain.CIPollResult{NextPollAt: after.Add(time.Minute), Interval: 30 * time.Second}
	if err := f.Store.RecordCIPoll(f.ctx(t), got[0].CIPollKey, ciOwner, r); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("RecordCIPoll by the old owner = %v; want ErrLeaseLost", err)
	}

	if err := f.Store.RecordCIPoll(f.ctx(t), got[0].CIPollKey, ciOtherOwner, r); err != nil {
		t.Errorf("RecordCIPoll by the owner = %v; want nil", err)
	}
}

// ReplaceCIWaitSHA ends the open wait superseded and opens one on the new sha in the same visit
// and round; OpenCIWaits lists only the new one.
func testCIWaitsSupersededOnNewSHA(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	old := f.openCIWait(t, ciWait(it.ID, ciSHA))
	moved := t1.Add(time.Minute)

	w, err := f.Store.ReplaceCIWaitSHA(f.ctx(t), old.ID, ciNewSHA, "https://github.com/acme/app/pull/7", moved)
	if err != nil {
		t.Fatalf("ReplaceCIWaitSHA = %v", err)
	}

	if w.ID == old.ID || w.SHA != ciNewSHA || w.Visit != ciVisit || w.Round != 1 || !w.StartedAt.Equal(moved) || !w.Open() {
		t.Errorf("ReplaceCIWaitSHA = %+v; want a new open wait on %s in %s, round 1, started at %s", w, ciNewSHA, ciVisit, moved)
	}

	ws := f.openCIWaits(t, it.ID)
	if len(ws) != 1 || ws[0].ID != w.ID || ws[0].SHA != ciNewSHA || ws[0].PRURL != "https://github.com/acme/app/pull/7" {
		t.Fatalf("OpenCIWaits = %+v; want only the wait on %s", ws, ciNewSHA)
	}

	if _, err := f.Store.ReplaceCIWaitSHA(f.ctx(t), old.ID, "c0ffee32", "", moved); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("ReplaceCIWaitSHA of the superseded wait = %v; want ErrNotFound", err)
	}

	// The superseded wait ended: ending it again ends nothing.
	if n, err := f.Store.EndCIWaits(f.ctx(t), []domain.CIWaitID{old.ID}, domain.CIPassed, moved); err != nil || n != 0 {
		t.Errorf("EndCIWaits of the superseded wait = %d, %v; want 0, nil", n, err)
	}

	polls := f.leaseCIPolls(t, moved, ciOwner)
	if len(polls) != 1 || polls[0].SHA != ciNewSHA {
		t.Errorf("LeaseDueCIPolls = %+v; want only the new sha's poll", polls)
	}
}

// CIRounds counts the distinct rounds of one item, station and visit, open or ended, and not
// another visit's.
func testCIWaitsCountRounds(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	ctx := f.ctx(t)

	if n, err := f.Store.CIRounds(ctx, it.ID, ciStation, ciVisit); err != nil || n != 0 {
		t.Fatalf("CIRounds before any wait = %d, %v; want 0, nil", n, err)
	}

	first := f.openCIWait(t, ciWait(it.ID, ciSHA))

	other := ciWait(it.ID, ciSHA)
	other.Repo = "github.com/acme/api"
	f.openCIWait(t, other) // a second repository in the same round

	if _, err := f.Store.ReplaceCIWaitSHA(ctx, first.ID, ciNewSHA, "", t1.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	open := f.openCIWaits(t, it.ID)
	ids := make([]domain.CIWaitID, 0, len(open))

	for _, w := range open {
		ids = append(ids, w.ID)
	}

	if n, err := f.Store.EndCIWaits(ctx, ids, domain.CIFailed, t1.Add(2*time.Minute)); err != nil || n != len(ids) {
		t.Fatalf("EndCIWaits = %d, %v; want %d, nil", n, err, len(ids))
	}

	second := ciWait(it.ID, "c0ffee33")
	second.Phase, second.Round, second.Held = domain.CIPhaseAfter, 2, json.RawMessage(`{"verdict": "approve"}`)
	f.openCIWait(t, second)

	nextVisit := ciWait(it.ID, ciSHA)
	nextVisit.Visit = "visit-2"
	f.openCIWait(t, nextVisit)

	if n, err := f.Store.CIRounds(ctx, it.ID, ciStation, ciVisit); err != nil || n != 2 {
		t.Errorf("CIRounds(%s) = %d, %v; want 2, nil", ciVisit, n, err)
	}

	if n, err := f.Store.CIRounds(ctx, it.ID, ciStation, "visit-2"); err != nil || n != 1 {
		t.Errorf("CIRounds(visit-2) = %d, %v; want 1, nil", n, err)
	}
}

// An item has at most one open wait per station visit and repository; an unknown item has none
// to open.
func testCIWaitsOneOpen(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	f.openCIWait(t, ciWait(it.ID, ciSHA))

	if _, err := f.Store.OpenCIWait(f.ctx(t), ciWait(it.ID, ciNewSHA)); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("second open wait = %v; want ErrConflict", err)
	}

	if _, err := f.Store.OpenCIWait(f.ctx(t), ciWait(missingItem, ciSHA)); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("wait of an unknown item = %v; want ErrNotFound", err)
	}

	bad := ciWait(it.ID, ciSHA)
	bad.Round = 0

	if _, err := f.Store.OpenCIWait(f.ctx(t), bad); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("wait of round 0 = %v; want ErrInvalid", err)
	}
}

// Once every wait on a commit ended, its poll is no longer leased.
func testCIWaitsEnded(f *fixture) {
	t := f.t

	it := f.create(t, domain.NewItem{})
	w := f.openCIWait(t, ciWait(it.ID, ciSHA))

	if n, err := f.Store.EndCIWaits(f.ctx(t), []domain.CIWaitID{w.ID}, domain.CIPassed, t1); err != nil || n != 1 {
		t.Fatalf("EndCIWaits = %d, %v; want 1, nil", n, err)
	}

	if got := f.leaseCIPolls(t, t1.Add(time.Hour), ciOwner); len(got) != 0 {
		t.Errorf("LeaseDueCIPolls with no open wait = %+v; want none", got)
	}

	if ws := f.openCIWaits(t, it.ID); len(ws) != 0 {
		t.Errorf("OpenCIWaits after the end = %+v; want none", ws)
	}
}

// jsonEqual reports whether a and b hold the same JSON value, as jsonb normalizes it.
func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}

	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)

	return string(xa) == string(ya)
}
