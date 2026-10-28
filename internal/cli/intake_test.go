package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
	rotated := at.Add(-time.Hour)

	nightly := gen.IntakeTrigger{
		Name: triggerNightly, Enabled: true, On: eventSchedule, Source: eventSchedule,
		When: "0 8 * * MON-FRI, Europe/Berlin", Days: []int{0, 1, 0, 2, 1, 0, 1},
	}
	nightly.Create.Type, nightly.Create.Station = "task", "triage"

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

	gh := gen.WebhookEndpoint{
		Id: intakeHookID, Name: intakeEndpoint, Kind: gen.WebhookKindGithub, Address: "/hooks/" + intakeHookID,
		SecretName: "gh-hook-secret", Triggers: []string{"ci-failures"}, Days: days, CreatedAt: at.Add(-48 * time.Hour), RotatedAt: &rotated,
	}
	created := gen.CreatedWebhookEndpoint{
		Id: gh.Id, Name: gh.Name, Kind: gh.Kind, Address: gh.Address, SecretName: gh.SecretName,
		Triggers: gh.Triggers, Days: gh.Days, CreatedAt: gh.CreatedAt, Secret: fakeHookSecret,
	}
	next := "cursor-2"
	wouldCreate, skipped := tryAnswers()

	g := &intakeGoald{bodies: map[string][]byte{
		"intake":  indented(t, gen.Intake{Triggers: []gen.IntakeTrigger{nightly, ci}}),
		"trigger": indented(t, detail),
		subList:   indented(t, gen.WebhookEndpointList{Endpoints: []gen.WebhookEndpoint{gh}}),
		"created": indented(t, created),
		subDeliveries: indented(t, gen.WebhookDeliveryPage{NextAfter: &next, Deliveries: []gen.WebhookDelivery{
			{Id: "d2", ReceivedAt: at, Outcome: gen.WebhookDeliveryOutcomeCreated, Item: &gen.WebhookDeliveryItem{Id: "i12", Number: 12}},
			{Id: "d1", ReceivedAt: at.Add(-time.Minute), Outcome: gen.WebhookDeliveryOutcomeRefused, Reason: "signature did not match"},
			{Id: "d0", ReceivedAt: at.Add(-2 * time.Minute), Outcome: gen.WebhookDeliveryOutcomeAccepted},
		}}),
		firingCreate: indented(t, wouldCreate),
		firingSkip:   indented(t, skipped),
	}}

	reply := func(name string, status int) http.HandlerFunc {
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

	base := "/v1/orgs/acme/factories/" + intakeFactory
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+base+"/intake", reply("intake", http.StatusOK))
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
	mux.HandleFunc("GET "+base+"/webhooks", reply(subList, http.StatusOK))
	mux.HandleFunc("POST "+base+"/webhooks", reply(answerCreated, http.StatusCreated))
	mux.HandleFunc("POST "+base+"/webhooks/"+intakeHookID+"/rotate", reply(answerCreated, http.StatusOK))
	mux.HandleFunc("GET "+base+"/webhooks/"+intakeHookID+"/deliveries", reply(subDeliveries, http.StatusOK))

	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)

	return g
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
		{Step: "only.modelAgrees", Outcome: stepPassed, Reason: reason("the docs changed\nsince the last sweep")},
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
	}{Brief: "Sweep the docs.", Station: "triage", Title: "Nightly docs sweep", Type: "task"}
	create.ModelCheck = &struct {
		CostUsd      float64 `json:"cost_usd"`
		InputTokens  int     `json:"input_tokens"`
		Model        string  `json:"model"`
		OutputTokens int     `json:"output_tokens"`
		Reason       string  `json:"reason"`
		Worth        bool    `json:"worth"`
	}{CostUsd: 0.0123, InputTokens: 1200, Model: cliHaiku, OutputTokens: 40, Reason: "the docs changed\nsince the last sweep", Worth: true}

	skip := gen.TriggerTry{Firing: firingSkip, Matched: true, Outcome: gen.TriggerTryOutcome("skipped"), Reason: reason("only.dedupe: an open item has the title"), Steps: []gen.TriggerTryStep{
		{Step: "when", Outcome: stepPassed, Reason: reason("the schedule fired at 2026-10-25T08:00:00Z")},
		{Step: "only.dedupe", Outcome: "skipped", Reason: reason("an open item has the title")},
		{Step: "only.modelAgrees", Outcome: stepNotReached},
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

// The endpoints print with their address, kind, secret's name, triggers and 7-day totals.
func TestWebhooks_List(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	out, _, err := goalIntake(t, g.srv.URL, cmdWebhooks, subList, intakeFactory)
	if err != nil {
		t.Fatalf("goal webhooks list = %v", err)
	}

	checkGolden(t, "webhooks-list", out)
}

// The deliveries of the endpoint named print newest first, then the next page's command;
// --limit and --after reach goald.
func TestWebhooks_Deliveries(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	out, _, err := goalIntake(t, g.srv.URL, cmdWebhooks, subDeliveries, intakeFactory, intakeEndpoint, "--limit", "3", "--after", "cursor-1")
	if err != nil {
		t.Fatalf("goal webhooks deliveries = %v", err)
	}

	checkGolden(t, "webhooks-deliveries", out)

	if _, q := g.last(); q.Get("limit") != "3" || q.Get("after") != "cursor-1" {
		t.Errorf("goald was sent %v; want limit=3 and after=cursor-1", q)
	}

	if _, _, err := goalIntake(t, g.srv.URL, cmdWebhooks, subDeliveries, intakeFactory, "nope"); err == nil || !strings.Contains(err.Error(), `no webhook endpoint named "nope"`) {
		t.Errorf("goal webhooks deliveries of an unknown endpoint = %v; want it named", err)
	}
}

// create posts the name and kind, prints the secret alone on stdout and the address and the
// warning on stderr.
func TestWebhooks_CreatePrintsSecretOnce(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	for _, tc := range []struct {
		args []string
		want gen.NewWebhookEndpoint
	}{
		{[]string{intakeEndpoint, flagKind, importedGitHub}, gen.NewWebhookEndpoint{Name: intakeEndpoint, Kind: gen.WebhookKindGithub}},
		{[]string{"plain"}, gen.NewWebhookEndpoint{Name: "plain", Kind: gen.WebhookKindGeneric}},
	} {
		stdout, stderr, err := goalIntake(t, g.srv.URL, append([]string{cmdWebhooks, verbCreate, intakeFactory}, tc.args...)...)
		if err != nil {
			t.Fatalf("goal webhooks create %q = %v", tc.args, err)
		}

		var sent gen.NewWebhookEndpoint

		b, _ := g.last()
		if err := json.Unmarshal(b, &sent); err != nil || sent != tc.want {
			t.Errorf("goal webhooks create %q sent %s; want %+v", tc.args, b, tc.want)
		}

		if stdout != fakeHookSecret+"\n" {
			t.Errorf("goal webhooks create %q stdout = %q; want the secret alone", tc.args, stdout)
		}

		if !strings.Contains(stderr, "/hooks/"+intakeHookID) || !strings.Contains(stderr, "store it now") || strings.Contains(stderr, fakeHookSecret) {
			t.Errorf("goal webhooks create %q stderr = %q; want the address and the warning, without the secret", tc.args, stderr)
		}
	}
}

// rotate replaces the endpoint's secret and prints the new one alone on stdout.
func TestWebhooks_Rotate(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	stdout, stderr, err := goalIntake(t, g.srv.URL, cmdWebhooks, "rotate", intakeFactory, intakeEndpoint)
	if err != nil {
		t.Fatalf("goal webhooks rotate = %v", err)
	}

	if stdout != fakeHookSecret+"\n" {
		t.Errorf("goal webhooks rotate stdout = %q; want the secret alone", stdout)
	}

	if !strings.Contains(stderr, "/hooks/"+intakeHookID) || !strings.Contains(stderr, "store it now") || strings.Contains(stderr, fakeHookSecret) {
		t.Errorf("goal webhooks rotate stderr = %q; want the address and the warning, without the secret", stderr)
	}
}

// No command but create and rotate prints a secret, even when goald's answer carried one.
func TestWebhooks_SecretOnlyOnCreateAndRotate(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	// A careless goald that leaks the secret into every answer: the text output still never shows it.
	for k, b := range g.bodies {
		if k != answerCreated {
			g.bodies[k] = bytes.Replace(b, []byte(`"name": "`), []byte(`"secret": "`+fakeHookSecret+`",
   "name": "`), 1)
		}
	}

	for _, line := range [][]string{
		{cmdTriggers, subList, intakeFactory},
		{cmdTriggers, subShow, intakeFactory, triggerNightly},
		{cmdWebhooks, subList, intakeFactory},
		{cmdWebhooks, subDeliveries, intakeFactory, intakeEndpoint},
	} {
		stdout, stderr, err := goalIntake(t, g.srv.URL, line...)
		if err != nil {
			t.Fatalf("goal %q = %v", line, err)
		}

		if strings.Contains(stdout+stderr, fakeHookSecret) {
			t.Errorf("goal %q printed the secret:\n%s%s", line, stdout, stderr)
		}
	}
}

// --json prints goald's answer as it came, for each of the seven commands.
func TestIntakeCommands_JSONAsSent(t *testing.T) {
	t.Parallel()

	g := newIntakeGoald(t)

	for _, tc := range []struct {
		line []string
		body string
	}{
		{[]string{cmdTriggers, subList, intakeFactory}, "intake"},
		{[]string{cmdTriggers, subShow, intakeFactory, triggerNightly}, "trigger"},
		{[]string{cmdTriggers, subTry, intakeFactory, triggerNightly, flagEvent, firingCreate}, firingCreate},
		{[]string{cmdWebhooks, subList, intakeFactory}, subList},
		{[]string{cmdWebhooks, verbCreate, intakeFactory, intakeEndpoint}, answerCreated},
		{[]string{cmdWebhooks, "rotate", intakeFactory, intakeEndpoint}, answerCreated},
		{[]string{cmdWebhooks, subDeliveries, intakeFactory, intakeEndpoint}, subDeliveries},
	} {
		stdout, _, err := goalIntake(t, g.srv.URL, append(tc.line, "--json")...)
		if want := string(g.bodies[tc.body]); err != nil || stdout != want {
			t.Errorf("goal %q --json = %q, %v; want %q", tc.line, stdout, err, want)
		}
	}
}

// Missing or extra arguments, an unknown subcommand and a kind other than generic or github are
// the usage, before goald is asked.
func TestIntakeCommands_Usage(t *testing.T) {
	t.Parallel()

	for _, line := range [][]string{
		{cmdTriggers},
		{cmdTriggers, "edit", intakeFactory},
		{cmdTriggers, subList},
		{cmdTriggers, subList, intakeFactory, argExtra},
		{cmdTriggers, subShow, intakeFactory},
		{cmdTriggers, subShow, intakeFactory, triggerNightly, argExtra},
		{cmdTriggers, subTry, intakeFactory, triggerNightly},
		{cmdTriggers, subTry, intakeFactory, flagEvent, firingCreate},
		{cmdTriggers, subTry, intakeFactory, triggerNightly, argExtra, flagEvent, firingCreate},
		{cmdWebhooks},
		{cmdWebhooks, "delete", intakeFactory, intakeEndpoint},
		{cmdWebhooks, subList},
		{cmdWebhooks, subList, intakeFactory, argExtra},
		{cmdWebhooks, verbCreate, intakeFactory},
		{cmdWebhooks, verbCreate, intakeFactory, intakeEndpoint, flagKind, "gitlab"},
		{cmdWebhooks, "rotate", intakeFactory},
		{cmdWebhooks, "rotate", intakeFactory, intakeEndpoint, argExtra},
		{cmdWebhooks, subDeliveries, intakeFactory},
		{cmdWebhooks, subDeliveries, intakeFactory, intakeEndpoint, "--limit", "0"},
	} {
		if _, _, err := goalIntake(t, "http://127.0.0.1:1", line...); !errors.Is(err, errUsage) {
			t.Errorf("goal %q = %v; want the usage", line, err)
		}
	}
}
