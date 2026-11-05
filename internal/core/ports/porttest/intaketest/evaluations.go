package intaketest

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Values the cases share.
const (
	firstDelivery = "d-1"
	typeFix       = "fix"
)

// evaluationTests are the suite's cases for delivery evaluations and the locked firing (ADR 0040
// decisions 13 and 16).
var evaluationTests = []struct {
	name string
	test func(f *fixture)
}{
	{"ReceiverAddsEvaluationsPerFactory", testReceiverAddsEvaluations},
	{"ClaimEvaluationsSkipLocked", testClaimEvaluationsSkipLocked},
	{"FireHoldsLimitsAcrossInstances", testFireHoldsLimits},
	{"EvaluationsOfAPageByDelivery", testEvaluationsOf},
}

// receiveEvaluated records an accepted delivery id to w at t0 with evaluations es.
func (f *fixture) receiveEvaluated(w domain.WebhookEndpointID, id string, es ...domain.NewDeliveryEvaluation) domain.Received {
	f.t.Helper()

	r, err := f.in.Receive(f.ctx(), domain.NewReceipt{
		WebhookID: w, ReceivedAt: t0, Receipt: domain.ReceiptAccepted, DeliveryID: id, Event: pushEvent, Named: true,
		Body: []byte(`{"ref":"main"}`), Evaluations: es,
	})
	if err != nil {
		f.t.Fatalf("Receive %s: %v", id, err)
	}

	return r
}

// A kept delivery adds its evaluations with its row: pending, or done and skipped at once with
// the reason. Its duplicate adds none, and another organization sees none.
func testReceiverAddsEvaluations(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)
	other := f.NewFactory(t, f.org)
	skip := "acme/legacy is not one of shop's repositories"

	r := f.receiveEvaluated(w.ID, firstDelivery,
		domain.NewDeliveryEvaluation{FactoryID: f.factory, Triggers: []string{flaky}},
		domain.NewDeliveryEvaluation{FactoryID: other, Triggers: []string{flaky, nightly}, Skip: skip})
	f.receiveEvaluated(w.ID, firstDelivery, domain.NewDeliveryEvaluation{FactoryID: f.factory, Triggers: []string{flaky}})

	es, err := f.in.DeliveryEvaluations(f.ctx(), r.Row)
	if err != nil || len(es) != 2 {
		t.Fatalf("DeliveryEvaluations = %+v, %v; want two", es, err)
	}

	checkReceived(t, f, w.ID, other, skip, es)

	checkEvaluationsIsolated(t, f, r.Row)
}

// checkReceived checks the pending and the skipped evaluation.
func checkReceived(t *testing.T, f *fixture, w domain.WebhookEndpointID, other domain.FactoryID, skip string, es []domain.DeliveryEvaluation) {
	t.Helper()

	for _, e := range es {
		switch e.FactoryID {
		case f.factory:
			if e.State != domain.EvaluationPending || e.Outcome != "" || e.Attempts != 0 || e.Event != pushEvent || e.DeliveryID != firstDelivery ||
				e.WebhookID != w || !e.NextAt.Equal(t0) {
				t.Errorf("pending evaluation = %+v", e)
			}
		case other:
			checkSkippedAtOnce(t, e, skip)
		}
	}
}

// checkEvaluationsIsolated checks another organization sees and claims none of row's evaluations.
func checkEvaluationsIsolated(t *testing.T, f *fixture, row domain.WebhookDeliveryID) {
	t.Helper()

	theirs := &fixture{Harness: f.Harness, t: t, in: f.in, org: f.NewOrg(t)}
	if got, err := f.in.DeliveryEvaluations(theirs.ctx(), row); err != nil || len(got) != 0 {
		t.Errorf("another organization's DeliveryEvaluations = %+v, %v; want none", got, err)
	}

	if got, err := f.in.ClaimEvaluations(theirs.ctx(), "goald-b", t0, time.Minute, 10); err != nil || len(got) != 0 {
		t.Errorf("another organization's claim = %+v, %v; want none", got, err)
	}
}

// Claims at once take each due row once; a live lease keeps a row from another claim, a lapsed one
// lets it go again, and only the claim that holds a row may finish it.
func testClaimEvaluationsSkipLocked(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)

	for i := range 6 {
		f.receiveEvaluated(w.ID, fmt.Sprintf("d-%d", i), domain.NewDeliveryEvaluation{FactoryID: f.factory, Triggers: []string{flaky}})
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = map[string]string{}
	)

	for i := range 4 {
		wg.Go(func() {
			holder := fmt.Sprintf("goald-%d", i)

			got, err := f.in.ClaimEvaluations(f.ctx(), holder, t0, time.Minute, 2)
			if err != nil {
				t.Error(err)
			}

			mu.Lock()
			defer mu.Unlock()

			for _, c := range got {
				if prev, ok := seen[c.ID]; ok {
					t.Errorf("row %s claimed by %s and %s", c.ID, prev, holder)
				}

				seen[c.ID] = holder

				if c.Attempts != 1 || c.LeaseHolder != holder || string(c.Body) != `{"ref":"main"}` {
					t.Errorf("claimed = %+v; want attempt 1 with its holder and body", c)
				}
			}
		})
	}

	wg.Wait()

	if len(seen) != 6 {
		t.Fatalf("%d rows claimed; want all 6", len(seen))
	}

	checkLeases(t, f, seen)
}

// checkLeases checks live leases keep rows, lapsed ones free them, and only the holder finishes.
func checkLeases(t *testing.T, f *fixture, seen map[string]string) {
	t.Helper()

	if got, _ := f.in.ClaimEvaluations(f.ctx(), "goald-x", t0.Add(30*time.Second), time.Minute, 10); len(got) != 0 {
		t.Errorf("a claim under live leases = %+v; want none", got)
	}

	again, err := f.in.ClaimEvaluations(f.ctx(), "goald-x", t0.Add(2*time.Minute), time.Minute, 1)
	if err != nil || len(again) != 1 || again[0].Attempts != 2 {
		t.Fatalf("a claim after the leases = %+v, %v; want one row on its second attempt", again, err)
	}

	checkFinish(t, f, seen, again[0])
}

// Firings of one trigger at once hold perDay and perHour, as the trigger's lock serializes them:
// of twelve events, perDay create items and the rest are skipped with its reason. A replay
// returns the first firing and files nothing.
func testFireHoldsLimits(f *fixture) {
	t := f.t
	limits := domain.FireLimits{PerDay: 3, PerHour: 5}

	fire := func(key string) (domain.TriggerFiring, bool) {
		item := domain.NewItem{
			FactoryID: f.factory, Type: typeFix, Title: "Fix " + key, Station: station, State: domain.StateQueued, Class: domain.ClassStandard,
			CreatedAt: t0, Source: domain.ItemFromGitHub, Trigger: flaky, Origin: domain.OriginTrigger, Untrusted: true,
		}

		got, fresh, err := f.in.Fire(f.ctx(), domain.TriggerFiring{
			FactoryID: f.factory, Trigger: flaky, Source: domain.TriggerGitHub, EventKey: "github.check_run", IdempotencyKey: key,
			Outcome: domain.FiringCreated, At: t0,
		}, &item, limits)
		if err != nil {
			t.Errorf("Fire %s: %v", key, err)
		}

		return got, fresh
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		skipped int
	)

	for i := range 12 {
		wg.Go(func() {
			got, _ := fire(fmt.Sprintf("e%d", i))

			mu.Lock()
			defer mu.Unlock()

			switch {
			case got.Outcome == domain.FiringCreated && got.ItemID != "":
				created++
			case got.Outcome == domain.FiringSkipped && strings.HasPrefix(got.Reason, domain.StepPerDay+": "):
				skipped++
			default:
				t.Errorf("firing = %+v; want created or skipped by perDay", got)
			}
		})
	}

	wg.Wait()

	if created != 3 || skipped != 9 {
		t.Fatalf("%d created and %d skipped; want 3 and 9", created, skipped)
	}

	checkReplay(t, f, fire)
}

// checkReplay checks a replay of e0 returns its first firing, whose item is untrusted.
func checkReplay(t *testing.T, f *fixture, fire func(string) (domain.TriggerFiring, bool)) {
	t.Helper()

	first, ok, err := f.in.Firing(f.ctx(), f.factory, flaky, "e0")
	if err != nil || !ok {
		t.Fatalf("Firing e0 = %v, %v", ok, err)
	}

	if got, fresh := fire("e0"); fresh || got.ID != first.ID {
		t.Errorf("replay = %+v, fresh %v; want the first firing", got, fresh)
	}

	if first.ItemID != "" {
		if it, err := f.Store.Item(f.ctx(), first.ItemID); err != nil || !it.Untrusted || it.Origin != domain.OriginTrigger {
			t.Errorf("the firing's item = %+v, %v; want it filed untrusted by the trigger", it, err)
		}
	}
}

// checkFinish checks only the claim that holds c may finish it, once.
func checkFinish(t *testing.T, f *fixture, seen map[string]string, c domain.ClaimedEvaluation) {
	t.Helper()

	stale := domain.EvaluationFinish{ID: c.ID, Holder: seen[c.ID], Attempts: 1, State: domain.EvaluationDone, Outcome: domain.EvaluationSkipped, At: t0}

	if ok, err := f.in.FinishEvaluation(f.ctx(), stale); err != nil || ok {
		t.Errorf("the first holder's finish = %v, %v; want refused", ok, err)
	}

	done := stale
	done.Holder, done.Attempts, done.Reason = "goald-x", 2, domain.ReasonNoTriggerMatches

	if ok, err := f.in.FinishEvaluation(f.ctx(), done); err != nil || !ok {
		t.Fatalf("the holder's finish = %v, %v; want it done", ok, err)
	}

	es, _ := f.in.DeliveryEvaluations(f.ctx(), c.Delivery)
	if len(es) != 1 || es[0].State != domain.EvaluationDone || es[0].LeaseHolder != "" || es[0].LeaseUntil != nil || es[0].Reason != done.Reason {
		t.Errorf("finished = %+v; want done without a lease", es)
	}

	retry := domain.EvaluationFinish{ID: c.ID, Holder: "goald-x", Attempts: 2, State: domain.EvaluationPending, At: t0}
	if ok, _ := f.in.FinishEvaluation(f.ctx(), retry); ok {
		t.Error("a done row finished again; want refused")
	}
}

// checkSkippedAtOnce checks e is done and skipped with skip, naming both triggers.
func checkSkippedAtOnce(t *testing.T, e domain.DeliveryEvaluation, skip string) {
	t.Helper()

	if e.State != domain.EvaluationDone || e.Outcome != domain.EvaluationSkipped || e.Reason != skip || !slices.Equal(e.Triggers, []string{flaky, nightly}) {
		t.Errorf("skipped evaluation = %+v", e)
	}
}

// The evaluations of a page of deliveries come by delivery then factory: a created one with its
// item's number, a skipped one with its reason; a delivery not asked for and another
// organization's rows never come back.
func testEvaluationsOf(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)
	other := f.NewFactory(t, f.org)
	skip := "acme/legacy is not one of shop's repositories"

	created := f.receiveEvaluated(w.ID, "d-created", domain.NewDeliveryEvaluation{FactoryID: f.factory, Triggers: []string{flaky}})
	skipped := f.receiveEvaluated(w.ID, "d-skipped",
		domain.NewDeliveryEvaluation{FactoryID: f.factory, Triggers: []string{flaky}, Skip: skip},
		domain.NewDeliveryEvaluation{FactoryID: other, Triggers: []string{nightly}, Skip: skip})
	f.receiveEvaluated(w.ID, "d-unasked", domain.NewDeliveryEvaluation{FactoryID: f.factory, Triggers: []string{flaky}, Skip: skip})

	it := f.item(domain.ItemFromGitHub, flaky, t0)

	claimed, err := f.in.ClaimEvaluations(f.ctx(), "goald-a", t0, time.Minute, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimEvaluations = %+v, %v; want the one pending row", claimed, err)
	}

	c := claimed[0]
	if ok, err := f.in.FinishEvaluation(f.ctx(), domain.EvaluationFinish{
		ID: c.ID, Holder: c.LeaseHolder, Attempts: c.Attempts, State: domain.EvaluationDone, Outcome: domain.EvaluationCreated, ItemID: it.ID, At: t0,
	}); err != nil || !ok {
		t.Fatalf("FinishEvaluation = %v, %v", ok, err)
	}

	got, err := f.in.DeliveryEvaluationsOf(f.ctx(), []domain.WebhookDeliveryID{created.Row, skipped.Row})
	if err != nil || len(got) != 3 {
		t.Fatalf("DeliveryEvaluationsOf = %+v, %v; want three", got, err)
	}

	checkEvaluationsOf(t, f, got, created.Row, skipped.Row, it, skip)

	theirs := &fixture{Harness: f.Harness, t: t, in: f.in, org: f.NewOrg(t)}
	if got, err := f.in.DeliveryEvaluationsOf(theirs.ctx(), []domain.WebhookDeliveryID{created.Row, skipped.Row}); err != nil || len(got) != 0 {
		t.Errorf("another organization's DeliveryEvaluationsOf = %+v, %v; want none", got, err)
	}

	if got, err := f.in.DeliveryEvaluationsOf(f.ctx(), nil); err != nil || len(got) != 0 {
		t.Errorf("DeliveryEvaluationsOf no rows = %+v, %v; want none", got, err)
	}
}

// checkEvaluationsOf checks the created and the two skipped evaluations, each with its delivery.
func checkEvaluationsOf(t *testing.T, f *fixture, got []domain.DeliveryEvaluation, created, skipped domain.WebhookDeliveryID,
	it domain.Item, skip string,
) {
	t.Helper()

	seen := map[domain.WebhookDeliveryID]int{}

	for _, e := range got {
		seen[e.Delivery]++

		switch e.Delivery {
		case created:
			checkCreatedOf(t, f, e, it)
		case skipped:
			if e.State != domain.EvaluationDone || e.Outcome != domain.EvaluationSkipped || e.Reason != skip || e.ItemID != "" || e.ItemNumber != 0 {
				t.Errorf("skipped evaluation = %+v; want skipped with %q", e, skip)
			}
		default:
			t.Errorf("evaluation of delivery %s not asked for", e.Delivery)
		}
	}

	if seen[created] != 1 || seen[skipped] != 2 {
		t.Errorf("evaluations by delivery = %v; want one created and two skipped", seen)
	}
}

// checkCreatedOf checks e is the fixture factory's, done and created with it and its number.
func checkCreatedOf(t *testing.T, f *fixture, e domain.DeliveryEvaluation, it domain.Item) {
	t.Helper()

	if e.State != domain.EvaluationDone || e.Outcome != domain.EvaluationCreated || e.ItemID != it.ID || e.ItemNumber != it.Number ||
		e.FactoryID != f.factory || e.DeliveryID != "d-created" {
		t.Errorf("created evaluation = %+v; want item %s #%d", e, it.ID, it.Number)
	}
}
