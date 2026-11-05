package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
)

// fakeHookSecret is an obviously fake signing secret: 64 hex characters.
var fakeHookSecret = strings.Repeat("0123456789abcdef", 4)

const (
	intakeFactory  = "docs"
	intakeEndpoint = "gh"
	intakeHookID   = "019a1c00-0000-7000-8000-0000000000e1"
	intakeDocsID   = "019a1c00-0000-7000-8000-00000000d0c5"
	hookInUse      = "in-use"
	hookOwnSecret  = "pd-key"
	triggerNightly = "nightly"
	cmdTriggers    = "triggers"
	cmdWebhooks    = "webhooks"
	subTry         = "try"
	flagEvent      = "--event"
	firingCreate   = "f-create"
	firingSkip     = "f-skip"
	firingGone     = "f-gone"
	firingHook     = "f-hook"
	argExtra       = "extra"
	answerCreated  = "created"
	flagKind       = "--kind"
	eventSchedule  = "schedule"
	cmdIntake      = "intake"
	stationTriage  = "triage"
)

// intakeGoald is a stub goald for the intake commands: it answers the factory docs's intake, its
// trigger nightly, its webhook endpoints and their deliveries with indented JSON, and records the
// bodies and queries it was sent.
type intakeGoald struct {
	srv *httptest.Server

	// bodies are the answers by route, as sent.
	bodies map[string][]byte

	mu    sync.Mutex
	sent  []byte
	query url.Values
}

func newIntakeGoald(t *testing.T) *intakeGoald {
	t.Helper()

	day := func(d int) openapi_types.Date {
		return openapi_types.Date{Time: time.Date(2026, 10, 20+d, 0, 0, 0, 0, time.UTC)}
	}
	days := make([]gen.WebhookDeliveryDay, 0, 7)

	for i := range 7 {
		days = append(days, gen.WebhookDeliveryDay{Day: day(i), Delivered: i, Refused: i % 2})
	}

	at := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	lane, reason, item := "fast", "dedupe: an open item has the title", int64(12)

	nightly := gen.IntakeTrigger{
		Name: triggerNightly, Enabled: true, On: eventSchedule, Source: eventSchedule,
		When: "0 8 * * MON-FRI, Europe/Berlin", Days: []int{0, 1, 0, 2, 1, 0, 1},
	}
	nightly.Create.Type, nightly.Create.Station = "task", stationTriage

	ci := gen.IntakeTrigger{
		Name: "ci-failures", On: "github.check_run.failed", Source: importedGitHub,
		When: "github.check_run.failed", Days: []int{0, 0, 0, 0, 0, 0, 0},
	}
	ci.Create.Type, ci.Create.Station, ci.Create.Expedite, ci.Create.Lane = "bug", "ship", true, &lane
	ci.Problem = &struct {
		Kind    gen.IntakeTriggerProblemKind `json:"kind"`
		Message string                       `json:"message"`
	}{Kind: "missing_connection", Message: "the factory has no webhook endpoint of kind github"}

	detail := gen.IntakeTriggerDetail{Trigger: nightly, Events: []gen.IntakeEvent{
		{Id: firingCreate, At: at, Event: eventSchedule, Outcome: gen.IntakeEventOutcomeMerged, Title: "Nightly docs sweep", ItemNumber: &item},
		{Id: firingSkip, At: at.Add(-24 * time.Hour), Event: eventSchedule, Outcome: gen.IntakeEventOutcomeSkipped, Title: "Nightly docs sweep", Reason: &reason},
	}}
	detail.Week.Created, detail.Week.Merged, detail.Week.SpendUsd = 3, 1, 4.2

	next := "cursor-2"
	wouldCreate, skipped := tryAnswers()

	g := &intakeGoald{bodies: map[string][]byte{
		cmdIntake: indented(t, intakeAnswer(nightly, ci, at)),
		"trigger": indented(t, detail),
		subDeliveries: indented(t, gen.WebhookDeliveryPage{NextAfter: &next, Deliveries: []gen.WebhookDelivery{
			{
				Id: "d2", ReceivedAt: at, Outcome: gen.WebhookDeliveryOutcomeCreated, Item: &gen.WebhookDeliveryItem{Id: "i12", Number: 12},
				Receipt: new(gen.WebhookDeliveryReceiptAccepted), Evaluations: []gen.WebhookDeliveryEvaluation{
					{Factory: "f1", FactoryName: "docs", State: gen.WebhookDeliveryEvaluationStateDone, Outcome: new(gen.WebhookDeliveryEvaluationOutcomeCreated)},
					{Factory: "f2", FactoryName: "line", State: gen.WebhookDeliveryEvaluationStatePending},
				},
			},
			{
				Id: "d1", ReceivedAt: at.Add(-time.Minute), Outcome: gen.WebhookDeliveryOutcomeRefused, Reason: "signature did not match",
				Receipt: new(gen.WebhookDeliveryReceiptRefused), Evaluations: []gen.WebhookDeliveryEvaluation{},
			},
			{Id: "d0", ReceivedAt: at.Add(-2 * time.Minute), Outcome: gen.WebhookDeliveryOutcomeAccepted, Evaluations: []gen.WebhookDeliveryEvaluation{}},
		}}),
		firingCreate: indented(t, wouldCreate),
		firingSkip:   indented(t, skipped),
	}}
	maps.Copy(g.bodies, webhookBodies(t, days, at))
	maps.Copy(g.bodies, checkBodies(t, at))

	reply := g.reply
	base := "/v1/orgs/acme/factories/" + intakeFactory
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+base+"/intake", reply(cmdIntake, http.StatusOK))
	mux.HandleFunc("GET "+base+"/intake/triggers/"+triggerNightly, reply("trigger", http.StatusOK))
	mux.HandleFunc("GET "+base+"/intake/triggers/{other}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"trigger-not-found","title":"Not found","status":404,"detail":"trigger not found"}`))
	})
	mux.HandleFunc("POST "+base+"/intake/triggers/"+triggerNightly+"/try", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)

		var req gen.TriggerTryRequest

		_ = json.Unmarshal(b, &req)

		g.mu.Lock()
		g.sent, g.query = b, r.URL.Query()
		g.mu.Unlock()

		switch req.Firing {
		case firingGone:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"firing-not-found","title":"Not found","status":404,"detail":"no such event among the trigger's 20 newest"}`))
		case firingHook:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"code":"event-not-stored","title":"Cannot try this event","status":422,"detail":"only schedule firings can be tried"}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(g.bodies[req.Firing])
		}
	})
	g.webhookRoutes(mux, base)
	mux.HandleFunc("GET "+base+"/intake/triggers/"+triggerNightly+"/checks", reply(bodyChecks, http.StatusOK))
	mux.HandleFunc("POST "+base+"/intake/triggers/"+triggerNightly+"/checks", reply(bodyTried, http.StatusAccepted))

	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)

	return g
}

// reply answers with the body named and status, recording what it was sent.
func (g *intakeGoald) reply(name string, status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)

		g.mu.Lock()
		g.sent, g.query = b, r.URL.Query()
		g.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(g.bodies[name])
	}
}

// intakeAnswer is docs's intake: the triggers given and one with a check, items coming up of each
// kind, origins, a triage item with a waiting source, and webhooks in each state.
func intakeAnswer(nightly, ci gen.IntakeTrigger, at time.Time) gen.Intake {
	checked := gen.IntakeTrigger{Name: "new-incidents", Enabled: true, On: eventSchedule, Source: eventSchedule, When: "*/5 * * * *, UTC", Days: make([]int, 7)}
	checked.Create.Type, checked.Create.Station = "task", stationTriage
	checked.Check = &struct {
		Agent  string  `json:"agent"`
		MaxUsd float64 `json:"max_usd"`
	}{Agent: "watcher", MaxUsd: 0.5}

	window, total, via, from := gen.IntakeWindowN7d, 3, gen.IntakeComingUpVia("cli"), "app#9"
	triage := &gen.IntakeTriage{
		Item:    &gen.IntakeTriageItem{FollowUps: 3, Id: "i20", Ref: "docs#20", Station: stationTriage, Title: "Follow-ups from docs#11"},
		Waiting: []gen.IntakeTriageSource{{FollowUps: 2, Id: "i12", Ref: "docs#12", Title: "Write the guide"}},
	}

	return gen.Intake{
		Triggers: []gen.IntakeTrigger{nightly, ci, checked}, Suggestions: []gen.TriggerSuggestion{}, Window: &window,
		Origins: &gen.IntakeOrigins{Person: 12, Plan: 3, Trigger: 5, FollowUp: 2},
		ComingUp: &[]gen.IntakeComingUp{
			{Id: "i14", Ref: "docs#14", Title: "Check the cache again", Origin: "follow-up", Start: &gen.ItemStart{At: at.Add(24 * time.Hour)}, Needs: []string{}, FromItem: &from, CreatedAt: at},
			{Id: "i15", Ref: "docs#15", Title: "Ship the guide", Origin: "person", Via: &via, Start: &gen.ItemStart{At: at.Add(48 * time.Hour)}, Needs: []string{"docs#12"}, CreatedAt: at},
		},
		ComingUpTotal: &total, Triage: triage,
		Webhooks: &[]gen.IntakeWebhook{
			{Id: "w1", Name: "github-main", Sender: "github", State: gen.Receiving},
			{Id: "w2", Name: "pager", Sender: "pagerduty", State: gen.NoTrigger},
			{Id: "w3", Name: "old-ci", Sender: "standard", State: gen.Closed},
		},
	}
}

// The bodies of a trigger's checks and of a try check.
const (
	bodyChecks = "checks"
	bodyTried  = "tried"
	tryID      = "f-try"
)

// checkBodies are goald's answers about nightly's checks: a try, done, saying what it would do;
// a failed check; and a done one that filed, reopened, noted and skipped.
func checkBodies(t *testing.T, at time.Time) map[string][]byte {
	t.Helper()

	ref, old, reason := "#12", "#7", "the check run ended failed: timed out after 5m0s"
	checked := at.Add(time.Minute)

	try := gen.TriggerCheck{
		Id: tryID, At: at.Add(time.Hour), CheckedAt: &checked, State: gen.TriggerCheckStateDone, Try: true, CostUsd: 0.04,
		Filed:   []gen.TriggerCheckFiled{{Key: "INC-9", Title: "Checkout is slow"}},
		Noted:   []gen.TriggerCheckNoted{},
		Skipped: []gen.TriggerCheckSkip{{Key: "INC-3", Reason: "a test page"}},
	}
	failed := gen.TriggerCheck{
		Id: "f-failed", At: at.Add(5 * time.Minute), State: gen.TriggerCheckStateFailed, CostUsd: 0.01, Reason: &reason,
		Filed: []gen.TriggerCheckFiled{}, Noted: []gen.TriggerCheckNoted{}, Skipped: []gen.TriggerCheckSkip{},
	}
	done := gen.TriggerCheck{
		Id: "f-done", At: at, CheckedAt: &checked, State: gen.TriggerCheckStateDone, CostUsd: 0.03,
		Filed: []gen.TriggerCheckFiled{{Key: "INC-1", Title: "API returns 5xx", Ref: &ref}, {Key: "INC-2", Title: "Disk full\non db-1", Ref: &ref, Reopens: &old}},
		Noted: []gen.TriggerCheckNoted{{Key: "INC-4", Ref: "#9", Text: "Still failing"}},
		Skipped: []gen.TriggerCheckSkip{
			{Key: "INC-5", Reason: "closed on Oct 25: fixed upstream"}, {Key: "INC-6", Reason: "over the limit"},
		},
	}
	quiet := gen.TriggerCheck{
		Id: "f-quiet", At: at.Add(-5 * time.Minute), CheckedAt: &checked, State: gen.TriggerCheckStateDone,
		Filed: []gen.TriggerCheckFiled{}, Noted: []gen.TriggerCheckNoted{}, Skipped: []gen.TriggerCheckSkip{},
	}

	queued := try
	queued.State, queued.CheckedAt, queued.Filed, queued.Skipped = gen.TriggerCheckStateQueued, nil, []gen.TriggerCheckFiled{}, []gen.TriggerCheckSkip{}

	return map[string][]byte{
		bodyChecks: indented(t, gen.TriggerCheckList{Checks: []gen.TriggerCheck{try, failed, done, quiet}}),
		bodyTried:  indented(t, queued),
	}
}

// docsFactory is the body that answers the factory docs.
const docsFactory = "docs-factory"

// webhookBodies are goald's answers about the organization's webhook gh, open to docs for
// acme/api, and about the factories.
func webhookBodies(t *testing.T, days []gen.WebhookDeliveryDay, at time.Time) map[string][]byte {
	t.Helper()

	repos := []string{"acme/api"}
	gh := gen.Webhook{
		Id: intakeHookID, Name: intakeEndpoint, Sender: gen.WebhookSenderGithub, Enabled: true, Address: "/hooks/" + intakeHookID,
		Secret: "webhook-gh", Openings: []gen.WebhookOpening{{Factory: intakeDocsID, Repositories: &repos}},
		Triggers: []gen.WebhookTrigger{{Factory: intakeDocsID, FactoryName: intakeFactory, Trigger: "ci-failures"}}, Days: days, CreatedAt: at.Add(-48 * time.Hour),
	}
	value := fakeHookSecret
	created := gen.CreatedWebhook{
		Id: gh.Id, Name: gh.Name, Sender: gh.Sender, Enabled: true, Address: gh.Address, Secret: gh.Secret, SecretValue: &value,
		Openings: []gen.WebhookOpening{}, Triggers: []gen.WebhookTrigger{}, Days: []gen.WebhookDeliveryDay{}, CreatedAt: gh.CreatedAt,
	}
	own := created
	own.Name, own.Sender, own.Secret, own.SecretValue = "pd", gen.WebhookSenderPagerduty, hookOwnSecret, nil

	return map[string][]byte{
		subList:       indented(t, gen.WebhookList{Webhooks: []gen.Webhook{gh}}),
		answerCreated: indented(t, created),
		"own":         indented(t, own),
		"webhook":     indented(t, gh),
		docsFactory:   indented(t, gen.Factory{Id: intakeDocsID, Name: intakeFactory}),
		"factories":   indented(t, gen.FactoryList{Factories: []gen.Factory{{Id: intakeDocsID, Name: intakeFactory}}}),
	}
}

// webhookRoutes serve the organization's webhooks, and the factory docs at base and in the list.
func (g *intakeGoald) webhookRoutes(mux *http.ServeMux, base string) {
	mux.HandleFunc("GET /v1/orgs/acme/factories", g.reply("factories", http.StatusOK))
	mux.HandleFunc("GET "+base, g.reply(docsFactory, http.StatusOK))

	hooks := "/v1/orgs/acme/webhooks"
	mux.HandleFunc("GET "+hooks, g.reply(subList, http.StatusOK))
	mux.HandleFunc("GET "+hooks+"/{webhook}", g.reply("webhook", http.StatusOK))
	mux.HandleFunc("POST "+hooks, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(b))

		if bytes.Contains(b, []byte(`"secret"`)) {
			g.reply("own", http.StatusCreated)(w, r)
		} else {
			g.reply(answerCreated, http.StatusCreated)(w, r)
		}
	})
	mux.HandleFunc("PATCH "+hooks+"/"+intakeEndpoint, g.reply("webhook", http.StatusOK))
	mux.HandleFunc("DELETE "+hooks+"/"+intakeEndpoint, g.reply("none", http.StatusNoContent))
	mux.HandleFunc("DELETE "+hooks+"/"+hookInUse, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"webhook-in-use","title":"Conflict","status":409,"detail":"webhook \"in-use\" is used by the published triggers docs ci-failures; publish them without it first"}`))
	})
	mux.HandleFunc("GET "+hooks+"/"+intakeEndpoint+"/deliveries", g.reply(subDeliveries, http.StatusOK))
	mux.HandleFunc("GET "+hooks+"/{other}/deliveries", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"webhook-not-found","title":"Not found","status":404,"detail":"webhook \"nope\" not found"}`))
	})
}

// The step outcomes a try answers.
const (
	stepPassed     gen.TriggerTryStepOutcome = "passed"
	stepNotReached gen.TriggerTryStepOutcome = "not_reached"
)

// tryAnswers are goald's answers to a try of nightly: firing f-create would create an item after
// the model check agreed, and firing f-skip is skipped by only.dedupe.
func tryAnswers() (gen.TriggerTry, gen.TriggerTry) {
	reason := func(s string) *string { return &s }

	create := gen.TriggerTry{Firing: firingCreate, Matched: true, Outcome: gen.TriggerTryOutcome("would_create"), Steps: []gen.TriggerTryStep{
		{Step: "when", Outcome: stepPassed, Reason: reason("the schedule fired at 2026-10-26T08:00:00Z")},
		{Step: "only.dedupe", Outcome: stepPassed, Reason: reason("no open item has the title")},
		{Step: verbCreate, Outcome: stepPassed},
		{Step: "limits.perDay", Outcome: stepPassed, Reason: reason("1 of 3 today")},
		{Step: "limits.pauseOnBudget", Outcome: stepPassed, Reason: reason("$4.20 of $50.00 this week")},
	}}
	create.Item = &struct {
		Brief    string  `json:"brief"`
		Expedite bool    `json:"expedite"`
		Lane     *string `json:"lane"`
		Station  string  `json:"station"`
		Title    string  `json:"title"`
		Type     string  `json:"type"`
	}{Brief: "Sweep the docs.", Station: stationTriage, Title: "Nightly docs sweep", Type: "task"}

	skip := gen.TriggerTry{Firing: firingSkip, Matched: true, Outcome: gen.TriggerTryOutcome("skipped"), Reason: reason("only.dedupe: an open item has the title"), Steps: []gen.TriggerTryStep{
		{Step: "when", Outcome: stepPassed, Reason: reason("the schedule fired at 2026-10-25T08:00:00Z")},
		{Step: "only.dedupe", Outcome: "skipped", Reason: reason("an open item has the title")},
		{Step: verbCreate, Outcome: stepNotReached},
		{Step: "limits.perDay", Outcome: stepNotReached},
	}}

	return create, skip
}

// last is the body and the query of the last request goald answered.
func (g *intakeGoald) last() ([]byte, url.Values) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.sent, g.query
}

// indented is v as indented JSON, so an answer printed as sent differs from one re-encoded.
func indented(t *testing.T, v any) []byte {
	t.Helper()

	b, err := json.MarshalIndent(v, "", "   ")
	if err != nil {
		t.Fatal(err)
	}

	return b
}

// goalIntake runs a goal command line against api and returns its stdout and stderr.
func goalIntake(t *testing.T, api string, args ...string) (string, string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	err := run(t.Context(), args, &printer{w: &stdout, e: &stderr}, testEnv(t, api, map[string]string{envOrg: orgAcme}), nil)

	return stdout.String(), stderr.String(), err
}

// goal intake asks for the window and prints what's coming: the items that haven't started with
// when they start or what they wait for and where they came from, the origins, the triage item and
// its waiting source, the triggers with the check marked, and the open webhooks with their state.
func TestIntakeCommands_IntakeShowsComingUp(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	out, _, err := goalIntake(t, g.srv.URL, cmdIntake, intakeFactory, flagWindow, "7d")
	if err != nil {
		t.Fatalf("goal intake = %v", err)
	}

	checkGolden(t, cmdIntake, out)

	g.mu.Lock()
	defer g.mu.Unlock()

	if got := g.query.Get("window"); got != "7d" {
		t.Errorf("window sent = %q; want 7d", got)
	}
}

// The triggers print in definition order: name, on or off, when, what they create, the 7-day
// total and a trigger's problem.
func TestTriggers_List(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	out, _, err := goalIntake(t, g.srv.URL, cmdTriggers, subList, intakeFactory)
	if err != nil {
		t.Fatalf("goal triggers list = %v", err)
	}

	checkGolden(t, "triggers-list", out)
}

// A trigger prints with its week and its recent events; a name the factory lacks is goald's
// trigger-not-found.
func TestTriggers_Show(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	out, _, err := goalIntake(t, g.srv.URL, cmdTriggers, subShow, intakeFactory, triggerNightly)
	if err != nil {
		t.Fatalf("goal triggers show = %v", err)
	}

	checkGolden(t, "triggers-show", out)

	if _, _, err := goalIntake(t, g.srv.URL, cmdTriggers, subShow, intakeFactory, "nope"); err == nil || !strings.Contains(err.Error(), "trigger not found") {
		t.Errorf("goal triggers show of an unknown trigger = %v; want goald's trigger-not-found", err)
	}
}

// A try posts the firing and prints what would happen: matched, every step with its reason, the
// item, the model check's cost, and that nothing was created.
func TestTriggers_TryWouldCreate(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	out, _, err := goalIntake(t, g.srv.URL, cmdTriggers, subTry, intakeFactory, triggerNightly, flagEvent, firingCreate)
	if err != nil {
		t.Fatalf("goal triggers try = %v", err)
	}

	var sent gen.TriggerTryRequest

	if b, _ := g.last(); json.Unmarshal(b, &sent) != nil || sent.Firing != firingCreate {
		t.Errorf("goal triggers try sent %s; want {firing: %s}", b, firingCreate)
	}

	checkGolden(t, "triggers-try-create", out)
}

// A skipped event prints the recorded reason, the steps not reached, that no item would be
// created and that the model check did not run, and still that nothing was created.
func TestTriggers_TrySkipped(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	out, _, err := goalIntake(t, g.srv.URL, cmdTriggers, subTry, intakeFactory, triggerNightly, flagEvent, firingSkip)
	if err != nil {
		t.Fatalf("goal triggers try = %v", err)
	}

	checkGolden(t, "triggers-try-skipped", out)
}

// goald's problems (an event not among the newest, one whose fields were not stored) are errors
// carrying goald's message, and nothing is printed.
func TestTriggers_TryProblem(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	for firing, want := range map[string]string{
		firingGone: "no such event among the trigger's 20 newest",
		firingHook: "only schedule firings can be tried",
	} {
		out, _, err := goalIntake(t, g.srv.URL, cmdTriggers, subTry, intakeFactory, triggerNightly, flagEvent, firing)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("goal triggers try --event %s = %v; want goald's %q", firing, err, want)
		}

		if out != "" {
			t.Errorf("goal triggers try --event %s printed %q; want nothing", firing, out)
		}
	}
}

// A check trigger's checks print newest first: when, state and cost, then what each filed (with
// the item, or would file for a try), noted and skipped with why, a failure's reason, and Nothing
// new for a check that found nothing.
func TestTriggers_Checks(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	out, _, err := goalIntake(t, g.srv.URL, cmdTriggers, "checks", intakeFactory, triggerNightly, "--limit", "5")
	if err != nil {
		t.Fatalf("goal triggers checks = %v", err)
	}

	if _, q := g.last(); q.Get("limit") != "5" {
		t.Errorf("goal triggers checks sent %v; want limit 5", q)
	}

	checkGolden(t, "triggers-checks", out)
}

// A try without --event posts a try check, waits for it among the trigger's checks, and prints
// what it would do and that nothing was filed, noted or saved.
func TestTriggers_TryCheck(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	out, _, err := goalIntake(t, g.srv.URL, cmdTriggers, subTry, intakeFactory, triggerNightly)
	if err != nil {
		t.Fatalf("goal triggers try = %v", err)
	}

	checkGolden(t, "triggers-try-check", out)
}

// The organization's webhooks print with their sender, state, address, secret's name, the
// factories they are open to (by name, with their repositories), the triggers that use them and
// their 7-day totals.
func TestWebhooks_List(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	out, _, err := goalIntake(t, g.srv.URL, cmdWebhooks, subList)
	if err != nil {
		t.Fatalf("goal webhooks list = %v", err)
	}

	checkGolden(t, "webhooks-list", out)
}

// The deliveries of the webhook named print newest first, then the next page's command;
// --limit and --after reach goald, and an unknown webhook is goald's problem.
func TestWebhooks_Deliveries(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	out, _, err := goalIntake(t, g.srv.URL, cmdWebhooks, subDeliveries, intakeEndpoint, "--limit", "3", "--after", "cursor-1")
	if err != nil {
		t.Fatalf("goal webhooks deliveries = %v", err)
	}

	checkGolden(t, "webhooks-deliveries", out)

	if _, q := g.last(); q.Get("limit") != "3" || q.Get("after") != "cursor-1" {
		t.Errorf("goald was sent %v; want limit=3 and after=cursor-1", q)
	}

	if _, _, err := goalIntake(t, g.srv.URL, cmdWebhooks, subDeliveries, "nope"); err == nil || !strings.Contains(err.Error(), `webhook "nope" not found`) {
		t.Errorf("goal webhooks deliveries of an unknown webhook = %v; want goald's problem", err)
	}
}

// create posts the name, the sender (standard by default, --kind generic|github its alias) and
// the secret named; a generated value prints alone on stdout, the address and the warning on
// stderr.
func TestWebhooks_CreatePrintsSecretOnce(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	for _, tc := range []struct {
		args   []string
		sender gen.WebhookSender
	}{
		{[]string{intakeEndpoint, "--sender", importedGitHub}, gen.WebhookSenderGithub},
		{[]string{intakeEndpoint, flagKind, importedGitHub}, gen.WebhookSenderGithub},
		{[]string{intakeEndpoint, flagKind, "generic"}, gen.WebhookSenderStandard},
		{[]string{intakeEndpoint}, gen.WebhookSenderStandard},
	} {
		stdout, stderr, err := goalIntake(t, g.srv.URL, append([]string{cmdWebhooks, verbCreate}, tc.args...)...)
		if err != nil {
			t.Fatalf("goal webhooks create %q = %v", tc.args, err)
		}

		var sent gen.NewWebhook

		b, _ := g.last()
		if err := json.Unmarshal(b, &sent); err != nil || sent.Name != intakeEndpoint || sent.Sender != tc.sender || sent.Secret != nil || sent.Token != nil {
			t.Errorf("goal webhooks create %q sent %s; want %s, sender %s and no secret", tc.args, b, intakeEndpoint, tc.sender)
		}

		if stdout != fakeHookSecret+"\n" {
			t.Errorf("goal webhooks create %q stdout = %q; want the value alone", tc.args, stdout)
		}

		if !strings.Contains(stderr, "/hooks/"+intakeHookID) || !strings.Contains(stderr, "store it now") || !strings.Contains(stderr, "goal webhooks open gh") ||
			strings.Contains(stderr, fakeHookSecret) {
			t.Errorf("goal webhooks create %q stderr = %q; want the address, how to open it and the warning, without the value", tc.args, stderr)
		}
	}
}

// create --secret signs the webhook with the secret named: goald makes no value, so none prints.
func TestWebhooks_CreateWithItsSecret(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	stdout, stderr, err := goalIntake(t, g.srv.URL, cmdWebhooks, verbCreate, "pd", "--sender", "pagerduty", "--secret", hookOwnSecret)
	if err != nil {
		t.Fatalf("goal webhooks create --secret = %v", err)
	}

	var sent gen.NewWebhook

	b, _ := g.last()
	if err := json.Unmarshal(b, &sent); err != nil || sent.Sender != gen.WebhookSenderPagerduty || sent.Secret == nil || *sent.Secret != hookOwnSecret {
		t.Errorf("goal webhooks create --secret sent %s; want pagerduty signed by %s", b, hookOwnSecret)
	}

	if stdout != "" || strings.Contains(stderr, "store it now") || !strings.Contains(stderr, "signed by the secret "+hookOwnSecret) {
		t.Errorf("goal webhooks create --secret = %q, %q; want nothing on stdout and the secret's name on stderr", stdout, stderr)
	}
}

// open and close patch the webhook with the factory's id, open with the repositories listed or
// none for every one.
func TestWebhooks_OpenAndClose(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	for _, tc := range []struct {
		line []string
		sent string
		say  string
	}{
		{
			[]string{subOpen, intakeEndpoint, intakeFactory, "--repos", "acme/api, acme/web"},
			`{"open":[{"factory":"` + intakeDocsID + `","repositories":["acme/api","acme/web"]}]}`, "✓ opened webhook gh to docs for acme/api, acme/web",
		},
		{[]string{subOpen, intakeEndpoint, intakeFactory}, `{"open":[{"factory":"` + intakeDocsID + `"}]}`, "every repository"},
		{[]string{subClose, intakeEndpoint, intakeFactory}, `{"close":["` + intakeDocsID + `"]}`, "✓ closed webhook gh to docs"},
	} {
		stdout, stderr, err := goalIntake(t, g.srv.URL, append([]string{cmdWebhooks}, tc.line...)...)
		if err != nil {
			t.Fatalf("goal webhooks %q = %v", tc.line, err)
		}

		if b, _ := g.last(); string(b) != tc.sent {
			t.Errorf("goal webhooks %q sent %s; want %s", tc.line, b, tc.sent)
		}

		if stdout != "" || !strings.Contains(stderr, tc.say) {
			t.Errorf("goal webhooks %q = %q, %q; want %q on stderr", tc.line, stdout, stderr, tc.say)
		}
	}
}

// delete removes the webhook, and goald's refusal while triggers use it names them.
func TestWebhooks_Delete(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	if _, stderr, err := goalIntake(t, g.srv.URL, cmdWebhooks, "delete", intakeEndpoint); err != nil || !strings.Contains(stderr, "✓ deleted webhook gh") {
		t.Errorf("goal webhooks delete = %q, %v; want it deleted", stderr, err)
	}

	if _, _, err := goalIntake(t, g.srv.URL, cmdWebhooks, "delete", hookInUse); err == nil || !strings.Contains(err.Error(), "docs ci-failures") {
		t.Errorf("goal webhooks delete of a webhook in use = %v; want the triggers named", err)
	}
}

// No command but create prints a secret's value, even when goald's answer carried one.
func TestWebhooks_SecretOnlyOnCreate(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	// A careless goald that leaks the value into every answer: the text output still never shows it.
	for k, b := range g.bodies {
		if k != answerCreated {
			g.bodies[k] = bytes.Replace(b, []byte(`"name": "`), []byte(`"secret_value": "`+fakeHookSecret+`",
   "name": "`), 1)
		}
	}

	for _, line := range [][]string{
		{cmdTriggers, subList, intakeFactory},
		{cmdTriggers, subShow, intakeFactory, triggerNightly},
		{cmdWebhooks, subList},
		{cmdWebhooks, subOpen, intakeEndpoint, intakeFactory},
		{cmdWebhooks, subDeliveries, intakeEndpoint},
	} {
		stdout, stderr, err := goalIntake(t, g.srv.URL, line...)
		if err != nil {
			t.Fatalf("goal %q = %v", line, err)
		}

		if strings.Contains(stdout+stderr, fakeHookSecret) {
			t.Errorf("goal %q printed the value:\n%s%s", line, stdout, stderr)
		}
	}
}

// --json prints goald's answer as it came, for each command that has one.
func TestIntakeCommands_JSONAsSent(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	for _, tc := range []struct {
		line []string
		body string
	}{
		{[]string{cmdIntake, intakeFactory}, cmdIntake},
		{[]string{cmdTriggers, subList, intakeFactory}, cmdIntake},
		{[]string{cmdTriggers, subShow, intakeFactory, triggerNightly}, "trigger"},
		{[]string{cmdTriggers, subTry, intakeFactory, triggerNightly, flagEvent, firingCreate}, firingCreate},
		{[]string{cmdWebhooks, subList}, subList},
		{[]string{cmdWebhooks, verbCreate, intakeEndpoint}, answerCreated},
		{[]string{cmdWebhooks, subOpen, intakeEndpoint, intakeFactory}, "webhook"},
		{[]string{cmdWebhooks, subClose, intakeEndpoint, intakeFactory}, "webhook"},
		{[]string{cmdWebhooks, subDeliveries, intakeEndpoint}, subDeliveries},
	} {
		stdout, _, err := goalIntake(t, g.srv.URL, append(tc.line, "--json")...)
		if want := string(g.bodies[tc.body]); err != nil || stdout != want {
			t.Errorf("goal %q --json = %q, %v; want %q", tc.line, stdout, err, want)
		}
	}
}

// Missing or extra arguments, an unknown subcommand (rotate included), a kind other than generic
// or github, an unknown sender and both --sender and --kind are the usage, before goald is asked.
func TestIntakeCommands_Usage(t *testing.T) {
	t.Parallel()

	for _, line := range [][]string{
		{cmdIntake},
		{cmdIntake, intakeFactory, argExtra},
		{cmdIntake, intakeFactory, flagWindow, "1d"},
		{cmdTriggers},
		{cmdTriggers, "edit", intakeFactory},
		{cmdTriggers, subList},
		{cmdTriggers, subList, intakeFactory, argExtra},
		{cmdTriggers, subShow, intakeFactory},
		{cmdTriggers, subShow, intakeFactory, triggerNightly, argExtra},
		{cmdTriggers, subTry, intakeFactory, triggerNightly, "--wait", "0s"},
		{cmdTriggers, "checks", intakeFactory},
		{cmdTriggers, "checks", intakeFactory, triggerNightly, argExtra},
		{cmdTriggers, "checks", intakeFactory, triggerNightly, "--limit", "101"},
		{cmdTriggers, subTry, intakeFactory, flagEvent, firingCreate},
		{cmdTriggers, subTry, intakeFactory, triggerNightly, argExtra, flagEvent, firingCreate},
		{cmdWebhooks},
		{cmdWebhooks, "rotate", intakeEndpoint},
		{cmdWebhooks, subList, argExtra},
		{cmdWebhooks, verbCreate},
		{cmdWebhooks, verbCreate, intakeEndpoint, argExtra},
		{cmdWebhooks, verbCreate, intakeEndpoint, flagKind, "gitlab"},
		{cmdWebhooks, verbCreate, intakeEndpoint, "--sender", "bitbucket"},
		{cmdWebhooks, verbCreate, intakeEndpoint, "--sender", importedGitHub, flagKind, importedGitHub},
		{cmdWebhooks, subOpen, intakeEndpoint},
		{cmdWebhooks, subOpen, intakeEndpoint, intakeFactory, argExtra},
		{cmdWebhooks, subClose, intakeEndpoint},
		{cmdWebhooks, "delete"},
		{cmdWebhooks, "delete", intakeEndpoint, argExtra},
		{cmdWebhooks, subDeliveries},
		{cmdWebhooks, subDeliveries, intakeEndpoint, argExtra},
		{cmdWebhooks, subDeliveries, intakeEndpoint, "--limit", "0"},
	} {
		if _, _, err := goalIntake(t, "http://127.0.0.1:1", line...); !errors.Is(err, errUsage) {
			t.Errorf("goal %q = %v; want the usage", line, err)
		}
	}
}
