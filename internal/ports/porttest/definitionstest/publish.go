package definitionstest

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

func testPublish(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, _ := f.create(t, acme)

	var published []domain.DefinitionVersion

	for i := range 3 {
		n := i + 1
		d := f.openDraft(t, r.ID)
		d = f.save(t, r.ID, d.Revision, map[string]string{notesPath: fmt.Sprint("version ", n)})
		p := publishing(f.record(t, r.ID), d, fmt.Sprint("Release ", n))
		p.Name, p.Title = fmt.Sprint("acme-", n), fmt.Sprint("Acme ", n)

		v, err := f.Definitions.Publish(ctx, p)
		if err != nil {
			t.Fatalf("Publish %d: %v", n, err)
		}

		want := d
		want.State, want.Number, want.Message, want.PublishedBy, want.UpdatedBy = domain.VersionPublished, n, p.Message, local, local
		checkVersion(t, fmt.Sprint("v", n), v, want)
		checkRecord(t, fmt.Sprint("factory after v", n), f.record(t, r.ID), ports.FactoryRecord{
			ID: r.ID, OrgID: f.org, Name: p.Name, Title: p.Title, Current: ref(v), LastVersionNumber: n, CreatedBy: local,
		})
		checkEntry(t, f.audited(t, r.ID, domain.AuditDefinitionPublished), local, domain.AuditTargetVersion, string(v.ID),
			map[string]any{"number": n, "message": p.Message, "moved": 0})

		published = append(published, v)
	}

	for _, v := range published {
		got, err := f.Definitions.VersionByNumber(ctx, r.ID, v.Number)
		if err != nil {
			t.Fatalf("VersionByNumber(%d): %v", v.Number, err)
		}

		checkVersion(t, fmt.Sprint("VersionByNumber ", v.Number), got, v)
	}

	_, err := f.Definitions.VersionByNumber(ctx, r.ID, 4)
	expect(t, "VersionByNumber of an unpublished number", err, ports.ErrNotFound)

	cur, err := f.Definitions.CurrentVersions(ctx)
	if err != nil || len(cur) != 1 || cur[0].ID != published[2].ID {
		t.Errorf("CurrentVersions = %+v, %v; want only v3", cur, err)
	}
}

func testPublishGuards(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, _ := f.create(t, acme)
	v1 := f.publish(t, r.ID, "first")
	f.create(t, "taken")

	rec := f.record(t, r.ID)
	d := f.openDraft(t, r.ID)
	d = f.save(t, r.ID, d.Revision, map[string]string{notesPath: "second"})
	ok := publishing(rec, d, "second")

	for _, tc := range []struct {
		what   string
		change func(p *ports.Publish)
		want   error
	}{
		{"at a stale revision", func(p *ports.Publish) { p.DraftRevision-- }, ports.ErrConflict},
		{"expecting another current version", func(p *ports.Publish) { p.ExpectedCurrent = "" }, ports.ErrBaseMoved},
		{"under another factory's name", func(p *ports.Publish) { p.Name = "TAKEN" }, ports.ErrNameTaken},
		{"of an unknown factory", func(p *ports.Publish) { p.Factory = newFactoryID() }, ports.ErrNotFound},
	} {
		p := ok
		tc.change(&p)
		_, err := f.Definitions.Publish(ctx, p)
		expect(t, "Publish "+tc.what, err, tc.want)
	}

	checkRecord(t, "factory after refused publishes", f.record(t, r.ID), ports.FactoryRecord{
		ID: r.ID, OrgID: f.org, Name: acme, Title: acmeTitle, Current: ref(v1), Draft: ref(d), LastVersionNumber: 1, CreatedBy: local,
	})

	if n := len(f.auditEntries(t, r.ID, domain.AuditDefinitionPublished)); n != 1 {
		t.Errorf("%d definition.published entries; want 1", n)
	}

	if v, err := f.Definitions.Publish(ctx, ok); err != nil || v.Number != 2 {
		t.Errorf("Publish after the refusals = %+v, %v; want v2", v, err)
	}

	checkVersion(t, "v1 after v2", f.version(t, v1.ID), v1)
}

func testPublishConcurrent(f *fixture) {
	t := f.t
	r, d := f.create(t, acme)
	rec := f.record(t, r.ID)

	const publishers = 8

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		wins   []domain.DefinitionVersion
		losses int
	)

	for range publishers {
		wg.Go(func() {
			v, err := f.Definitions.Publish(f.ctx(t), publishing(rec, d, "race"))

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil:
				wins = append(wins, v)
			case errors.Is(err, ports.ErrConflict):
				losses++
			default:
				t.Errorf("Publish: %v", err)
			}
		})
	}

	wg.Wait()

	if len(wins) != 1 || losses != publishers-1 || wins[0].Number != 1 {
		t.Fatalf("concurrent Publish: %d won %+v, %d lost; want one v1 and %d conflicts", len(wins), wins, losses, publishers-1)
	}

	if v := f.publish(t, r.ID, "next"); v.Number != 2 {
		t.Errorf("next Publish = v%d; want v2 with no gap", v.Number)
	}
}

func testPublishPaused(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, d := f.create(t, acme)
	p := publishing(r, d, "first")
	p.Start = false

	if _, err := f.Definitions.Publish(ctx, p); err != nil {
		t.Fatal(err)
	}

	rec := f.record(t, r.ID)
	if !rec.Paused() || rec.PausedBy != local || rec.Current == nil {
		t.Errorf("record = %+v; want v1 current and the factory paused", rec)
	}

	if _, err := f.Definitions.SetPaused(ctx, r.ID, false, local); err != nil {
		t.Fatal(err)
	}

	next := f.openDraft(t, r.ID)
	p = publishing(f.record(t, r.ID), next, "second")
	p.Start = false

	if _, err := f.Definitions.Publish(ctx, p); err != nil {
		t.Fatal(err)
	}

	if rec := f.record(t, r.ID); rec.Paused() {
		t.Errorf("record = %+v; want a later publish to leave the factory running", rec)
	}
}

func testPublishRemovedStations(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, _ := f.create(t, acme)
	f.publish(t, r.ID, "first")

	f.item(t, r.ID, stQA, domain.StateQueued)
	running, lease := f.claim(t, r.ID, stQA)
	queued := f.item(t, r.ID, stQA, domain.StateQueued)
	blocked := f.item(t, r.ID, stQA, domain.StateBlocked)
	waiting := f.item(t, r.ID, stQA, domain.StateWaitingHuman)
	elsewhere := f.item(t, r.ID, stBuild, domain.StateQueued)

	rec := f.record(t, r.ID)
	d := f.openDraft(t, r.ID)
	p := publishing(rec, d, "drop qa")
	p.RemovedStations = []string{stQA}

	var atRemoved *ports.ItemsAtRemovedStationError

	_, err := f.Definitions.Publish(ctx, p)
	if !errors.As(err, &atRemoved) || !errors.Is(err, ports.ErrItemsAtRemovedStation) ||
		!sameItems(atRemoved.InProgress, running) || !sameItems(atRemoved.Unmapped, queued, blocked, waiting) {
		t.Fatalf("Publish dropping a station with open items = %v (%+v); want %d in progress and 3 unmapped",
			err, atRemoved, running.Number)
	}

	if got := f.record(t, r.ID); got.LastVersionNumber != 1 || got.Draft == nil {
		t.Errorf("record after a refused publish = %+v; want v1 and the draft open", got)
	}

	// The run finishes; the other items are mapped to the review station.
	f.finish(t, running, lease)

	p.StationMoves = map[string]ports.StationMove{stQA: {To: stReview, State: domain.StateQueued}}

	v, err := f.Definitions.Publish(ctx, p)
	if err != nil {
		t.Fatalf("Publish with station moves: %v", err)
	}

	for _, it := range []domain.Item{queued, waiting, blocked} {
		want := domain.StateQueued
		if it.State == domain.StateBlocked {
			want = domain.StateBlocked
		}

		f.movedTo(t, it, stReview, want, fmt.Sprintf("definition v%d", v.Number))
	}

	if got := f.itemNow(t, elsewhere.ID); got.Station != stBuild || got.State != domain.StateQueued {
		t.Errorf("item at a kept station = %+v; want it untouched", got)
	}

	checkEntry(t, f.audited(t, r.ID, domain.AuditDefinitionPublished), local, domain.AuditTargetVersion, string(v.ID),
		map[string]any{"number": 2, "moved": 3})
}

// sameItems reports whether got lists exactly want, by id.
func sameItems(got []ports.ItemAt, want ...domain.Item) bool {
	if len(got) != len(want) {
		return false
	}

	for _, it := range want {
		if !slices.ContainsFunc(got, func(a ports.ItemAt) bool {
			return a.ID == it.ID && a.Number == it.Number && a.Station == it.Station && a.State == it.State
		}) {
			return false
		}
	}

	return true
}

// item creates an item of factory at station, in state (queued, then moved there).
func (f *fixture) item(t *testing.T, factory domain.FactoryID, station string, state domain.ItemState) domain.Item {
	t.Helper()

	it, err := f.Items.CreateItem(f.ctx(t), domain.NewItem{
		FactoryID: factory, Type: typeTask, Title: "an item", Station: station, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: t0,
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	if state == domain.StateQueued {
		return it
	}

	it, err = f.Items.Transition(f.ctx(t), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: station, State: state}, At: t0,
	})
	if err != nil {
		t.Fatalf("Transition to %s: %v", state, err)
	}

	return it
}

// claim takes the first waiting item at station, as the engine does when a run starts.
func (f *fixture) claim(t *testing.T, factory domain.FactoryID, station string) (domain.Item, domain.Lease) {
	t.Helper()

	it, l, err := f.Items.ClaimNext(f.ctx(t), ports.Claim{
		FactoryID: factory, Station: domain.Station{ID: station, Kind: domain.KindAgent, Agent: "worker", Agents: 1},
		Now: t0, ExpiresAt: t0.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}

	return it, l
}

// finish ends a claimed item's run: it moves on to the review station and its lease is released.
func (f *fixture) finish(t *testing.T, it domain.Item, l domain.Lease) {
	t.Helper()

	_, err := f.Items.Transition(f.ctx(t), ports.Transition{
		ItemID: it.ID, From: domain.Position{Station: it.Station, State: domain.StateInProgress},
		To: domain.Position{Station: stReview, State: domain.StateQueued}, Lease: &l, Outcome: domain.LeaseAdvanced, At: t0,
	})
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
}

func (f *fixture) itemNow(t *testing.T, id domain.ItemID) domain.Item {
	t.Helper()

	it, err := f.Items.Item(f.ctx(t), id)
	if err != nil {
		t.Fatalf("Item: %v", err)
	}

	return it
}

// movedTo checks it now sits at station in state, with a moved event saying why as its last.
func (f *fixture) movedTo(t *testing.T, it domain.Item, station string, state domain.ItemState, reason string) {
	t.Helper()

	got := f.itemNow(t, it.ID)
	if got.Station != station || got.State != state {
		t.Errorf("item #%d = %s at %s; want %s at %s", it.Number, got.State, got.Station, state, station)
	}

	events, err := f.Items.ItemEvents(f.ctx(t), it.ID, 0, 100)
	if err != nil || len(events) == 0 {
		t.Fatalf("ItemEvents = %v, %v", events, err)
	}

	moved := 0

	for _, e := range events {
		if e.Kind == domain.EventMoved {
			moved++
		}
	}

	e := events[len(events)-1]
	if moved != 1 || e.Kind != domain.EventMoved || e.Station != station ||
		e.Data["from"] != it.Station || e.Data["to"] != station || e.Data["reason"] != reason {
		t.Errorf("events of #%d = %+v; want one moved event last, from %s to %s for %q", it.Number, events, it.Station, station, reason)
	}
}

func testVersions(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, _ := f.create(t, acme)

	for _, note := range []string{"one", "two", "three"} {
		f.publish(t, r.ID, note)
	}

	discarded := f.openDraft(t, r.ID)
	if err := f.Definitions.DiscardDraft(ctx, r.ID, discarded.Revision, local); err != nil {
		t.Fatal(err)
	}

	f.openDraft(t, r.ID)

	numbers := func(before, limit int) []int {
		vs, err := f.Definitions.Versions(ctx, r.ID, before, limit)
		if err != nil {
			t.Fatalf("Versions: %v", err)
		}

		var out []int
		for _, v := range vs {
			out = append(out, v.Number)
		}

		return out
	}

	for _, tc := range []struct {
		before, limit int
		want          []int
	}{
		{0, 10, []int{3, 2, 1}},
		{0, 2, []int{3, 2}},
		{2, 2, []int{1}},
		{1, 10, nil},
	} {
		if got := numbers(tc.before, tc.limit); !slices.Equal(got, tc.want) {
			t.Errorf("Versions(before %d, limit %d) = %v; want %v", tc.before, tc.limit, got, tc.want)
		}
	}

	if vs, err := f.Definitions.Versions(ctx, newFactoryID(), 0, 10); err != nil || len(vs) != 0 {
		t.Errorf("Versions of an unknown factory = %+v, %v; want none", vs, err)
	}
}

func testRestore(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, _ := f.create(t, acme)
	v1 := f.publish(t, r.ID, "one")
	f.publish(t, r.ID, "two")
	v3 := f.publish(t, r.ID, "three")
	open := f.openDraft(t, r.ID)

	rec, err := f.Definitions.Restore(ctx, ports.Restore{
		Factory: r.ID, Version: v1.ID, ExpectedCurrent: v3.ID, Name: "acme-old", Title: "Acme old", By: agent,
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	want := ports.FactoryRecord{
		ID: r.ID, OrgID: f.org, Name: "acme-old", Title: "Acme old", Current: ref(v1), Draft: ref(open), LastVersionNumber: 3, CreatedBy: local,
	}
	checkRecord(t, "restored factory", rec, want)
	checkVersion(t, "v3 after the restore", f.version(t, v3.ID), v3)
	checkVersion(t, "open draft after the restore", f.draft(t, r.ID), open)
	checkEntry(t, f.audited(t, r.ID, domain.AuditDefinitionRestored), agent, domain.AuditTargetVersion, string(v1.ID),
		map[string]any{"from": 3, "to": 1})

	again, err := f.Definitions.Restore(ctx, ports.Restore{Factory: r.ID, Version: v1.ID, ExpectedCurrent: v1.ID, Name: "acme-old", By: agent})
	if err != nil {
		t.Fatalf("Restore of the current version: %v", err)
	}

	checkRecord(t, "factory restored to its current version", again, want)

	if n := len(f.auditEntries(t, r.ID, domain.AuditDefinitionRestored)); n != 1 {
		t.Errorf("%d definition.restored entries; want 1", n)
	}

	if v4, err := f.Definitions.Publish(ctx, publishing(f.record(t, r.ID), open, "four")); err != nil || v4.Number != 4 {
		t.Errorf("Publish after a restore = %+v, %v; want v4 (numbers are not reused)", v4, err)
	}
}

func testRestoreGuards(f *fixture) {
	t := f.t
	ctx := f.ctx(t)
	r, _ := f.create(t, acme)
	v1 := f.publish(t, r.ID, "one")
	v2 := f.publish(t, r.ID, "two")
	draft := f.openDraft(t, r.ID)
	other, _ := f.create(t, "zeta")
	otherV1 := f.publish(t, other.ID, "one")
	restore := ports.Restore{Factory: r.ID, Version: v1.ID, ExpectedCurrent: v2.ID, Name: acme, By: local}

	stale := restore
	stale.ExpectedCurrent = v1.ID
	_, err := f.Definitions.Restore(ctx, stale)
	expect(t, "Restore expecting another current version", err, ports.ErrBaseMoved)

	for _, id := range []domain.DefinitionVersionID{draft.ID, otherV1.ID, "00000000-0000-7000-8000-ffffffffffff"} {
		bad := restore
		bad.Version = id
		_, err := f.Definitions.Restore(ctx, bad)
		expect(t, "Restore to "+string(id)+", not a published version of the factory", err, ports.ErrNotFound)
	}

	taken := restore
	taken.Name = "zeta"
	_, err = f.Definitions.Restore(ctx, taken)
	expect(t, "Restore under another factory's name", err, ports.ErrNameTaken)

	running := f.item(t, r.ID, stBuild, domain.StateQueued)
	f.claim(t, r.ID, stBuild)

	dropping := restore
	dropping.RemovedStations = []string{stBuild}

	var atRemoved *ports.ItemsAtRemovedStationError

	_, err = f.Definitions.Restore(ctx, dropping)
	if !errors.As(err, &atRemoved) || len(atRemoved.InProgress) != 1 || atRemoved.InProgress[0].ID != running.ID {
		t.Errorf("Restore dropping a station with a run = %v; want item #%d in progress", err, running.Number)
	}

	if got := f.record(t, r.ID); got.Current == nil || got.Current.ID != v2.ID {
		t.Errorf("record after refused restores = %+v; want v2 current", got)
	}

	if n := len(f.auditEntries(t, r.ID, domain.AuditDefinitionRestored)); n != 0 {
		t.Errorf("%d definition.restored entries; want none", n)
	}
}
