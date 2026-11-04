package storetest

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// keptTestsTests are the KeptTests cases (ADR 0039 decision 12), registered by Run under KeptTests_.
var keptTestsTests = []struct {
	name string
	test func(f *fixture)
}{
	{"KeptTests_KeepAndListByStation", testKeptTestsList},
	{"KeptTests_DuplicateConflicts", testKeptTestsDuplicate},
	{"KeptTests_ItemOfTheFactoryOnly", testKeptTestsItem},
	{"KeptTests_Remove", testKeptTestsRemove},
	{"KeptTests_OtherOrgInvisible", testKeptTestsTenancy},
	{"KeptTests_LastResultIsNewestFinishedTry", testKeptTestsLastResult},
}

// keep keeps it at station, at minutes after t0, as the local user.
func (f *fixture) keep(t *testing.T, it domain.Item, station string, at int) domain.KeptTest {
	t.Helper()

	k, err := f.Kept.KeepTest(f.ctx(t), domain.NewKeptTest{
		FactoryID: it.FactoryID, ItemID: it.ID, Station: station, KeptBy: domain.LocalUser, KeptAt: t0.Add(time.Duration(at) * time.Minute),
	})
	if err != nil {
		t.Fatalf("KeepTest: %v", err)
	}

	return k
}

func testKeptTestsList(f *fixture) {
	t := f.t
	a := f.create(t, domain.NewItem{Title: "Mend the sign-in"})
	b := f.create(t, domain.NewItem{Title: "Add a page"})

	ka := f.keep(t, a, stWork, 1)
	kb := f.keep(t, b, stWork, 2)
	kn := f.keep(t, a, stNext, 3)

	expectKept(t, ka, a, f.factory)

	all, err := f.Kept.KeptTests(f.ctx(t), f.factory, "")
	if err != nil {
		t.Fatal(err)
	}

	if got := keptIDs(all); !slices.Equal(got, []domain.KeptTestID{kn.ID, kb.ID, ka.ID}) {
		t.Errorf("KeptTests = %v, want newest first %v", got, []domain.KeptTestID{kn.ID, kb.ID, ka.ID})
	}

	at, err := f.Kept.KeptTests(f.ctx(t), f.factory, stWork)
	if err != nil {
		t.Fatal(err)
	}

	if got := keptIDs(at); !slices.Equal(got, []domain.KeptTestID{kb.ID, ka.ID}) {
		t.Errorf("KeptTests at %s = %v, want %v", stWork, got, []domain.KeptTestID{kb.ID, ka.ID})
	}

	if at[0].ItemNumber != b.Number || at[0].ItemTitle != "Add a page" {
		t.Errorf("listed %+v, want item %d with its title", at[0], b.Number)
	}

	if got, err := f.Kept.KeptTests(f.ctx(t), f.newFactory(t), ""); err != nil || len(got) != 0 {
		t.Errorf("KeptTests of another factory = %+v, %v; want none", got, err)
	}
}

func testKeptTestsDuplicate(f *fixture) {
	t := f.t
	it := f.create(t, domain.NewItem{})
	f.keep(t, it, stWork, 1)

	_, err := f.Kept.KeepTest(f.ctx(t), domain.NewKeptTest{
		FactoryID: it.FactoryID, ItemID: it.ID, Station: stWork, KeptBy: domain.LocalUser, KeptAt: t1,
	})
	expect(t, "KeepTest of a kept item and station", err, ports.ErrConflict)

	_, err = f.Kept.KeepTest(f.ctx(t), domain.NewKeptTest{FactoryID: it.FactoryID, ItemID: it.ID, KeptBy: domain.LocalUser, KeptAt: t1})
	expect(t, "KeepTest without a station", err, domain.ErrInvalid)
}

func testKeptTestsItem(f *fixture) {
	t := f.t
	it := f.create(t, domain.NewItem{})

	_, err := f.Kept.KeepTest(f.ctx(t), domain.NewKeptTest{
		FactoryID: f.factory, ItemID: missingItem, Station: stWork, KeptBy: domain.LocalUser, KeptAt: t1,
	})
	expect(t, "KeepTest of a missing item", err, ports.ErrNotFound)

	_, err = f.Kept.KeepTest(f.ctx(t), domain.NewKeptTest{
		FactoryID: f.newFactory(t), ItemID: it.ID, Station: stWork, KeptBy: domain.LocalUser, KeptAt: t1,
	})
	expect(t, "KeepTest of an item of another factory", err, ports.ErrNotFound)
}

func testKeptTestsRemove(f *fixture) {
	t := f.t
	it := f.create(t, domain.NewItem{})
	k := f.keep(t, it, stWork, 1)
	other := f.newFactory(t)

	expect(t, "RemoveKeptTest from another factory", f.Kept.RemoveKeptTest(f.ctx(t), other, k.ID), ports.ErrNotFound)

	if err := f.Kept.RemoveKeptTest(f.ctx(t), f.factory, k.ID); err != nil {
		t.Fatalf("RemoveKeptTest: %v", err)
	}

	expect(t, "RemoveKeptTest twice", f.Kept.RemoveKeptTest(f.ctx(t), f.factory, k.ID), ports.ErrNotFound)

	if got, err := f.Kept.KeptTests(f.ctx(t), f.factory, ""); err != nil || len(got) != 0 {
		t.Errorf("KeptTests after removing = %+v, %v; want none", got, err)
	}

	// A removed test may be kept again.
	f.keep(t, it, stWork, 2)
}

func testKeptTestsTenancy(f *fixture) {
	t := f.t
	it := f.create(t, domain.NewItem{})
	k := f.keep(t, it, stWork, 1)
	other := f.otherOrg(t)

	if got, err := f.Kept.KeptTests(other.ctx(t), f.factory, ""); err != nil || len(got) != 0 {
		t.Errorf("KeptTests of another org = %+v, %v; want none", got, err)
	}

	expect(t, "RemoveKeptTest of another org's test", f.Kept.RemoveKeptTest(other.ctx(t), f.factory, k.ID), ports.ErrNotFound)

	_, err := f.Kept.KeepTest(other.ctx(t), domain.NewKeptTest{
		FactoryID: f.factory, ItemID: it.ID, Station: stNext, KeptBy: domain.LocalUser, KeptAt: t1,
	})
	expect(t, "KeepTest of another org's item", err, ports.ErrNotFound)

	if _, err := f.Kept.KeptTests(t.Context(), f.factory, ""); !errors.Is(err, domain.ErrNoOrg) {
		t.Errorf("KeptTests without an org = %v, want ErrNoOrg", err)
	}

	if got, err := f.Kept.KeptTests(f.ctx(t), f.factory, ""); err != nil || len(got) != 1 {
		t.Errorf("KeptTests after another org's calls = %+v, %v; want the one kept", got, err)
	}
}

// expectKept checks k is a's test at stWork, kept by the local user a minute after t0.
func expectKept(t *testing.T, k domain.KeptTest, a domain.Item, factory domain.FactoryID) {
	t.Helper()

	if k.ID == "" || k.ItemNumber != a.Number || k.ItemTitle != a.Title || k.Station != stWork ||
		k.KeptBy.Ref() != domain.LocalUser.Ref() || !k.KeptAt.Equal(t0.Add(time.Minute)) || k.FactoryID != factory {
		t.Errorf("KeepTest = %+v, want item %d at %s by the local user", k, a.Number, stWork)
	}
}

func keptIDs(ks []domain.KeptTest) []domain.KeptTestID {
	out := make([]domain.KeptTestID, 0, len(ks))
	for _, k := range ks {
		out = append(out, k.ID)
	}

	return out
}

// A kept test's last result is its newest finished try's, of any source: nil until one finished,
// and a queued or running try never counts.
func testKeptTestsLastResult(f *fixture) {
	t := f.t
	k := f.keep(t, f.create(t, domain.NewItem{}), stWork, 1)

	last := func() *domain.KeptTestLast {
		t.Helper()

		all, err := f.Kept.KeptTests(f.ctx(t), f.factory, "")
		if err != nil || len(all) != 1 {
			t.Fatalf("KeptTests = %+v, %v; want the one test", all, err)
		}

		return all[0].LastResult
	}

	if got := last(); got != nil {
		t.Errorf("LastResult of a test never run = %+v, want nil", got)
	}

	tries := []domain.NewPastTry{f.keptTry(t, k, "key-1", 1), f.keptTry(t, k, "key-1", 2), f.keptTry(t, k, "key-1", 3)}
	if _, err := f.Tries.QueuePublishTries(f.ctx(t), f.factory, f.NewVersion(t, f.org, f.factory), publishEdits, tries, t1); err != nil {
		t.Fatal(err)
	}

	finish := func(v domain.PastTryVerdict, at time.Time) domain.RunID {
		t.Helper()

		c, err := f.Tries.ClaimPastTry(f.ctx(t), f.factory, at, ttl, 3)
		if err != nil {
			t.Fatal(err)
		}

		run := f.pastRun(t, f.create(t, domain.NewItem{}), stNext, t1, false, "")
		if _, err := f.Tries.FinishPastTry(f.ctx(t), c, domain.PastTryResult{Verdict: v, Reason: "why", TryRunID: run, At: at}); err != nil {
			t.Fatal(err)
		}

		return run
	}

	finish(domain.PastTryFixed, t1.Add(time.Hour))
	run := finish(domain.PastTryNotFixed, t1.Add(2*time.Hour))

	got := last()
	if got == nil || got.Result != domain.KeptTestFailed || !got.At.Equal(t1.Add(2*time.Hour)) || got.RunID != run {
		t.Errorf("LastResult = %+v; want failed at %s by run %s, the newest finished", got, t1.Add(2*time.Hour), run)
	}
}
