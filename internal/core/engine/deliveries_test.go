package engine_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/engine"
	"github.com/byFrederick/goal/internal/core/ports"
)

// eventTables are senders' event tables for the tests, as internal/catalog/senders gives them.
type eventTables map[string]domain.EventTable

func (e eventTables) EventTable(sender domain.WebhookSender, event string) (domain.EventTable, bool) {
	tb, ok := e[string(sender)+"."+event]

	return tb, ok
}

// checkRunTable is GitHub's check_run table, the fields the tests read.
var checkRunTable = eventTables{"github.check_run": {
	domain.FieldConclusion: {Paths: []string{"check_run.conclusion"}}, "check.name": {Paths: []string{"check_run.name"}},
	domain.FieldBranch: {Paths: []string{"check_run.check_suite.head_branch"}}, domain.FieldRepo: {Paths: []string{"repository.full_name"}},
	domain.FieldAuthor: {Paths: []string{"sender.login"}},
}}

// The webhook the delivery tests receive on, and the event they deliver.
const (
	testWebhook = "github-acme"
	evCheckRun  = "check_run"
)

// onFailedRun fires on failed check runs from every GitHub webhook open to the factory.
func onFailedRun() domain.Trigger {
	return domain.Trigger{
		Name: "red-check", Enabled: true, On: "github.check_run", Filter: domain.TriggerFilter{Conclusion: []string{"failure"}},
		Create: domain.TriggerCreate{Type: typeFeature, Station: stWork, Title: "{{check.name}} failed on {{payload.check_run.head_sha}}"},
	}
}

// deliveryHarness is an intake harness with the senders' tables and the organization's GitHub
// webhook, open to the factory.
func deliveryHarness(t *testing.T, ts ...domain.Trigger) (*harness, domain.WebhookEndpoint) {
	t.Helper()

	h := intakeHarness(t)
	h.events = checkRunTable
	h.line.Triggers = ts
	h.e = h.engine(t, h.store)
	h.intake.UseSecrets(h.orgSecrets)

	ctx := inOrg(t)
	if _, err := h.orgSecrets.Create(ctx, domain.NewSecret{Name: "webhook-" + testWebhook, Value: "test-secret"}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	w, err := h.intake.CreateWebhook(ctx, domain.NewWebhook{Name: testWebhook, Sender: domain.SenderGitHub, Secret: "webhook-" + testWebhook})
	if err != nil {
		t.Fatal(err)
	}

	if w, err = h.intake.PatchWebhook(ctx, w.ID, domain.WebhookPatch{Open: []domain.WebhookOpening{{FactoryID: factoryID}}}); err != nil {
		t.Fatal(err)
	}

	return h, w
}

// checkRun is a check_run delivery's body: the check name, its conclusion and the head sha.
func checkRun(name, conclusion string) string {
	return fmt.Sprintf(`{"action":"completed","check_run":{"name":%q,"conclusion":%q,"head_sha":"abc123",`+
		`"check_suite":{"head_branch":"main"}},"repository":{"full_name":"acme/app"},"sender":{"login":"jo"}}`, name, conclusion)
}

// deliver receives a check_run delivery id with body on w as the receiver does, with the
// evaluations the engine names, and returns its row.
func (h *harness) deliver(t *testing.T, e interface {
	Evaluations(ctx context.Context, w domain.WebhookEndpoint, event string, body []byte) []domain.NewDeliveryEvaluation
}, w domain.WebhookEndpoint, id, body string,
) domain.WebhookDeliveryID {
	t.Helper()

	evals := e.Evaluations(inOrg(t), w, evCheckRun, []byte(body))

	got, err := h.intake.Receive(inOrg(t), domain.NewReceipt{
		WebhookID: w.ID, ReceivedAt: h.clock.Now(), Receipt: domain.ReceiptAccepted, DeliveryID: id, Event: evCheckRun,
		Body: []byte(body), Named: len(evals) > 0, Evaluations: evals,
	})
	if err != nil {
		t.Fatal(err)
	}

	return got.Row
}

// evaluation is the delivery row's one evaluation.
func (h *harness) evaluation(t *testing.T, row domain.WebhookDeliveryID) domain.DeliveryEvaluation {
	t.Helper()

	es, err := h.intake.DeliveryEvaluations(inOrg(t), row)
	if err != nil || len(es) != 1 {
		t.Fatalf("evaluations = %+v, %v; want one", es, err)
	}

	return es[0]
}

// A delivery a trigger names is evaluated on the next pass: its matching trigger files an item,
// untrusted, with its title from the normalized fields and the payload, and the evaluation is done
// with it. One whose filter doesn't match is done and skipped with the reason.
func TestIntake_DeliveryItemIsUntrusted(t *testing.T) {
	t.Parallel()

	trusted := onFailedRun()
	trusted.Name, trusted.Create.Trusted, trusted.Filter.Authors = "trusted-check", true, []string{"jo"}

	h, w := deliveryHarness(t, onFailedRun(), trusted)
	red := h.deliver(t, h.e, w, "d-1", checkRun("unit", "failure"))
	green := h.deliver(t, h.e, w, "d-2", checkRun("unit", "success"))

	if n, err := h.e.EvaluateDeliveries(t.Context()); err != nil || n != 2 {
		t.Fatalf("EvaluateDeliveries = %d, %v; want both evaluated", n, err)
	}

	e := h.evaluation(t, red)
	if e.State != domain.EvaluationDone || e.Outcome != domain.EvaluationCreated || e.ItemID == "" {
		t.Fatalf("evaluation = %+v; want done with an item", e)
	}

	it := h.item(t, e.ItemID)
	if !it.Untrusted || it.Origin != domain.OriginTrigger || it.Title != `"unit" failed on "abc123"` || it.Source != domain.ItemFromGitHub {
		t.Errorf("item = %+v; want untrusted from GitHub, titled from the event", it)
	}

	checkTrustedAndSkipped(t, h, w, green)
}

// checkTrustedAndSkipped checks the trusted trigger's item is trusted and the passing check skipped.
func checkTrustedAndSkipped(t *testing.T, h *harness, w domain.WebhookEndpoint, green domain.WebhookDeliveryID) {
	t.Helper()

	f, ok, err := h.intake.Firing(inOrg(t), factoryID, "trusted-check", string(w.ID)+"/d-1")
	if err != nil || !ok || f.ItemID == "" || h.item(t, f.ItemID).Untrusted {
		t.Errorf("trusted trigger's firing = %+v, %v, %v; want a trusted item", f, ok, err)
	}

	if e := h.evaluation(t, green); e.State != domain.EvaluationDone || e.Outcome != domain.EvaluationSkipped || e.Reason != domain.ReasonNoTriggerMatches {
		t.Errorf("a passing check's evaluation = %+v; want skipped, no filter matches", e)
	}
}

// A failing evaluation backs off, longer each time, and is failed with its error after five
// attempts: here its factory has no line on the instance.
func TestEngine_EvaluateDeliveriesBacksOffThenFails(t *testing.T) {
	t.Parallel()

	h, w := deliveryHarness(t, onFailedRun())

	const unloaded = domain.FactoryID("00000000-0000-4000-8000-0000000000aa")

	h.store.AddFactory(testOrg, unloaded)

	body := checkRun("unit", "failure")

	got, err := h.intake.Receive(inOrg(t), domain.NewReceipt{
		WebhookID: w.ID, ReceivedAt: h.clock.Now(), Receipt: domain.ReceiptAccepted, DeliveryID: "d-1", Event: evCheckRun,
		Body: []byte(body), Named: true, Evaluations: []domain.NewDeliveryEvaluation{{FactoryID: unloaded, Triggers: []string{"red-check"}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var waits []time.Duration

	for attempt := 1; attempt <= domain.MaxEvaluationAttempts; attempt++ {
		if _, err := h.e.EvaluateDeliveries(t.Context()); err != nil {
			t.Fatal(err)
		}

		e := h.evaluation(t, got.Row)
		if e.Attempts != attempt {
			t.Fatalf("attempt %d: evaluation = %+v; want it tried again", attempt, e)
		}

		if attempt < domain.MaxEvaluationAttempts {
			if e.State != domain.EvaluationPending || !strings.Contains(e.Reason, "not loaded") {
				t.Fatalf("attempt %d: evaluation = %+v; want pending with the error", attempt, e)
			}

			if n, _ := h.e.EvaluateDeliveries(t.Context()); n != 0 || h.evaluation(t, got.Row).Attempts != attempt {
				t.Fatalf("attempt %d: tried again before its backoff", attempt)
			}

			waits = append(waits, e.NextAt.Sub(h.clock.Now()))
			h.clock.Advance(e.NextAt.Sub(h.clock.Now()))

			continue
		}

		if e.State != domain.EvaluationFailed || e.Outcome != domain.EvaluationFailedOutcome || !strings.Contains(e.Reason, "not loaded") {
			t.Errorf("after %d attempts: evaluation = %+v; want failed with the error", attempt, e)
		}
	}

	checkGrowing(t, waits)
}

// checkGrowing checks each wait is longer than the one before.
func checkGrowing(t *testing.T, waits []time.Duration) {
	t.Helper()

	for i := 1; i < len(waits); i++ {
		if waits[i] <= waits[i-1] {
			t.Errorf("waits %v; want each longer than the last", waits)
		}
	}
}

// Two instances share the evaluations: each delivery is evaluated once and files one item, and a
// row whose instance died with its lease is claimed again by the other.
func TestEngine_EvaluateDeliveriesSharedAcrossInstances(t *testing.T) {
	t.Parallel()

	h, w := deliveryHarness(t, onFailedRun())
	other := h.engine(t, h.store)

	for i := range 8 {
		h.deliver(t, h.e, w, fmt.Sprintf("d-%d", i), checkRun(fmt.Sprintf("check %d", i), "failure"))
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		total int
	)

	for _, e := range []interface {
		EvaluateDeliveries(ctx context.Context) (int, error)
	}{h.e, other, h.e, other} {
		wg.Go(func() {
			n, err := e.EvaluateDeliveries(t.Context())
			if err != nil {
				t.Error(err)
			}

			mu.Lock()
			total += n
			mu.Unlock()
		})
	}

	wg.Wait()

	if total != 8 || h.items(t) != 8 {
		t.Fatalf("%d evaluations done and %d items; want 8 of each", total, h.items(t))
	}

	checkLapsedLease(t, h, w, other)
}

// checkLapsedLease checks a row whose instance died with its lease is evaluated by another.
func checkLapsedLease(t *testing.T, h *harness, w domain.WebhookEndpoint, other *engine.Engine) {
	t.Helper()

	// An instance claims a row and dies: once its lease runs out, the other evaluates it.
	row := h.deliver(t, h.e, w, "d-dead", checkRun("late", "failure"))

	if got, err := h.intake.ClaimEvaluations(inOrg(t), "goald-dead", h.clock.Now(), domain.EvaluationLease, 10); err != nil || len(got) != 1 {
		t.Fatalf("the dead instance's claim = %+v, %v; want the row", got, err)
	}

	if n, err := other.EvaluateDeliveries(t.Context()); err != nil || n != 0 {
		t.Fatalf("EvaluateDeliveries under a live lease = %d, %v; want nothing", n, err)
	}

	h.clock.Advance(domain.EvaluationLease + time.Second)

	if n, err := other.EvaluateDeliveries(t.Context()); err != nil || n != 1 {
		t.Fatalf("EvaluateDeliveries after the lease = %d, %v; want the row", n, err)
	}

	if e := h.evaluation(t, row); e.State != domain.EvaluationDone || e.Attempts != 2 || e.ItemID == "" {
		t.Errorf("evaluation = %+v; want done on its second attempt", e)
	}
}

// limits.perHour skips the events past it in the hour before each, and creates again once the
// hour has passed.
func TestIntake_PerHour(t *testing.T) {
	t.Parallel()

	tr := failedCheck()
	tr.Limits.PerHour = 2
	h := intakeHarness(t, tr)

	wantCreated(t, h.receive(t, failure("e1", "a")))
	wantCreated(t, h.receive(t, failure("e2", "b")))

	f := h.receive(t, failure("e3", "c"))
	wantSkipped(t, f, domain.StepPerHour)

	if !strings.Contains(f.Reason, "2 items created in the last hour, the limit is 2") {
		t.Errorf("reason %q; want the count and the limit", f.Reason)
	}

	h.clock.Advance(61 * time.Minute)
	wantCreated(t, h.receive(t, failure("e4", "d")))
}

// perDay holds when two instances fire the same trigger at once: the store checks it again under
// the trigger's lock, so of many events only perDay create items.
func TestIntake_LimitsHoldAcrossInstances(t *testing.T) {
	t.Parallel()

	tr := failedCheck()
	tr.Limits.PerDay = 3
	h := intakeHarness(t, tr)
	other := h.engine(t, h.store)

	var wg sync.WaitGroup

	for i := range 12 {
		e := h.e
		if i%2 == 1 {
			e = other
		}

		wg.Go(func() {
			if _, err := e.Intake(inOrg(t), failure(fmt.Sprintf("e%d", i), fmt.Sprintf("check %d", i))); err != nil {
				t.Error(err)
			}
		})
	}

	wg.Wait()

	if n := h.items(t); n != 3 {
		t.Errorf("%d items from 12 events on two instances; want perDay's 3", n)
	}
}

// An untrusted item's approved merge waits for a person, whatever the policy says.
func TestEngine_UntrustedApprovalWaitsForAPerson(t *testing.T) {
	t.Parallel()

	h := newHarness(t, gateMergeLine(), byAgent(map[string]string{
		agWorker:   `{"pull_request":"` + prURL + `"}`,
		agReviewer: approve,
	}))
	h.codehost.Add(openPR(prURL))

	n, err := domain.File(h.line, domain.FileRequest{
		Origin: domain.OriginTrigger, By: domain.EngineActor, Type: typeFeature, Title: "From a webhook", Untrusted: true, At: h.clock.Now(),
		Station: h.line.Stations[0].ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	n.FactoryID = factoryID

	it, err := h.store.CreateItem(inOrg(t), n)
	if err != nil {
		t.Fatal(err)
	}

	h.step(t) // built
	h.step(t) // reviewed, not merged

	h.expectAt(t, it, stReview, domain.StateWaitingHuman)

	if merges := h.codehost.Merges(); len(merges) != 0 {
		t.Fatalf("merges %+v, want none before a person merges", merges)
	}

	waits := h.eventsOf(it.ID, domain.EventWaitingHuman)
	if reason, _ := waits[len(waits)-1].Data["reason"].(string); !strings.Contains(reason, "outside text") {
		t.Errorf("reason %q; want it to say the item came from outside text", reason)
	}
}

// An untrusted item's follow-ups go to a triage item of their own when its work lands, filed
// paused with the reason, and never to the factory's collecting triage item.
func TestEngine_UntrustedFollowUpsGoToTheirOwnTriage(t *testing.T) {
	t.Parallel()

	b := &byCall{runs: map[string]int{}, out: map[string][]string{agWorker: {landedFollowUps, `{}`}}}
	h := newHarness(t, withFollowUps(lineOf(agentStation(stBuild, agWorker, 1, 1))), b.agent)

	n, err := domain.File(h.line, domain.FileRequest{
		Origin: domain.OriginTrigger, By: domain.EngineActor, Type: typeFeature, Title: "From a webhook", Untrusted: true, At: h.clock.Now(),
		Station: h.line.Stations[0].ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	n.FactoryID = factoryID

	it, err := h.store.CreateItem(inOrg(t), n)
	if err != nil {
		t.Fatal(err)
	}

	h.untilDone(t, it.ID, 4)
	h.step(t)

	got := h.followUps(t, it.ID)
	if len(got) != 2 || got[0].FiledItem == "" || got[0].FiledItem != got[1].FiledItem {
		t.Fatalf("follow-ups = %+v; want both on one triage item", got)
	}

	tri := h.item(t, got[0].FiledItem)
	if !tri.Triage || !tri.Untrusted || tri.State != domain.StateBlocked {
		t.Errorf("triage item = %+v; want an untrusted triage item, paused", tri)
	}

	paused := h.eventsOf(tri.ID, domain.EventPaused)
	if len(paused) != 1 || paused[0].Data["reason"] != domain.ReasonUntrustedFollowUps {
		t.Errorf("paused events = %+v; want one with %q", paused, domain.ReasonUntrustedFollowUps)
	}
}

var _ ports.SenderEvents = eventTables{}
