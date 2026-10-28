package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// triggerBlock declares two valid triggers in baseFactory's board: a schedule and a GitHub
// event. Rows replace one of its lines and mark it with "#here".
const triggerBlock = `    workTypes: [fix, feature]
    triggers:
      - name: nightly-sweep
        on: schedule
        cron: "0 6 * * MON-FRI"
        timeZone: Europe/Berlin
        create:
          type: feature
          station: work
          lane: backend
          title: "Sweep {{ repo }}"
          brief: "Look at {{check.name}} on {{repo}}"
      - name: red-main
        enabled: false
        on: github.check_run.failed
        filter: { branch: main, repo: acme/app, paths: ["**/*.go"], authors: [acme-bot], skipBots: true }
        only: { failuresInARow: 2, dedupe: check.name }
        create: { type: fix, station: work, expedite: true, title: "Fix {{check.name}}" }
        limits: { perDay: 3, pauseOnBudget: true }
      - name: deploy-alert
        on: webhook.deploys.failed
        create: { type: fix, station: inbox }
`

// withTriggers is baseFactory with triggerBlock in its board, after the given replacements of
// the block, and station work routing lane backend.
func withTriggers(pairs ...string) string {
	return edited("  board:\n", "  board:\n"+replaced(triggerBlock, pairs...),
		workAgentLine, "        route: { backend: worker }\n")
}

// sweep names the first trigger of triggerBlock in a message.
const sweep = "trigger nightly-sweep"

// triggerRows are the rows of TestCheck_Triggers: a definition and the one problem it must
// produce, none when code is "".
var triggerRows = []struct {
	name    string
	doc     string
	code    string
	message []string // what the message must name
}{
	{name: "valid triggers", doc: withTriggers()},
	{name: "schedule in UTC by default", doc: withTriggers("        timeZone: Europe/Berlin\n", "")},
	{name: "cron with lists, ranges and steps", doc: withTriggers(`"0 6 * * MON-FRI"`, `"*/15 0-6,22 1-31/2 jan,JUL-dec 0-6"`)},
	{
		name: "duplicate trigger name", doc: withTriggers("      - name: red-main\n", "      - name: nightly-sweep #here\n"),
		code: codeDuplicateName, message: []string{sweep, "used by another trigger"},
	},
	{
		name: "create.type not a work type", doc: withTriggers("          type: feature\n", "          type: chore #here\n"),
		code: codeUnknownWorkType, message: []string{sweep, `"chore"`, "board.workTypes"},
	},
	{
		name: "create.station not a station", doc: withTriggers("          station: work\n", "          station: ghost #here\n"),
		code: codeUnknownStation, message: []string{sweep, `"ghost"`},
	},
	{
		name: "create.lane not routed by the station", doc: withTriggers("          lane: backend\n", "          lane: expedite #here\n"),
		code: codeUnknownLane, message: []string{sweep, `"expedite"`, "station work"},
	},
	{
		name: "lane on a station without a route",
		doc:  withTriggers("create: { type: fix, station: inbox }", "create: { type: fix, station: inbox, lane: backend } #here"),
		code: codeUnknownLane, message: []string{"trigger deploy-alert", `"backend"`, "station inbox"},
	},
	{
		name: "schedule without cron", doc: withTriggers(`        cron: "0 6 * * MON-FRI"`+"\n", "", "      - name: nightly-sweep\n", "      - name: nightly-sweep #here\n"),
		code: codeSchemaRequired, message: []string{"spec.board.triggers[0]", "cron"},
	},
	{
		name: "cron on an event trigger", doc: withTriggers("        on: github.check_run.failed\n", "        on: github.check_run.failed\n        cron: \"0 6 * * *\" #here\n"),
		code: codeSchemaInvalid, message: []string{"spec.board.triggers[1].cron"},
	},
	{
		name: "cron of four fields", doc: withTriggers(`        cron: "0 6 * * MON-FRI"`, `        cron: "0 6 * *" #here`),
		code: codeCronInvalid, message: []string{sweep, `"0 6 * *"`, "five fields"},
	},
	{
		name: "cron minute out of range", doc: withTriggers(`        cron: "0 6 * * MON-FRI"`, `        cron: "60 6 * * *" #here`),
		code: codeCronInvalid, message: []string{sweep, `"60 6 * * *"`, "minute"},
	},
	{
		name: "cron descriptor", doc: withTriggers(`        cron: "0 6 * * MON-FRI"`, `        cron: "@daily" #here`),
		code: codeCronInvalid, message: []string{sweep, `"@daily"`},
	},
	{
		name: "cron backwards range", doc: withTriggers(`        cron: "0 6 * * MON-FRI"`, `        cron: "0 6 * * FRI-MON" #here`),
		code: codeCronInvalid, message: []string{sweep, "day of week"},
	},
	{
		name: "cron step of zero", doc: withTriggers(`        cron: "0 6 * * MON-FRI"`, `        cron: "*/0 6 * * *" #here`),
		code: codeCronInvalid, message: []string{sweep, "minute"},
	},
	{
		name: "unknown time zone", doc: withTriggers("        timeZone: Europe/Berlin\n", "        timeZone: Mars/Olympus #here\n"),
		code: codeTimeZoneInvalid, message: []string{sweep, `"Mars/Olympus"`},
	},
	{
		name: "unclosed placeholder", doc: withTriggers(`          title: "Sweep {{ repo }}"`, `          title: "Sweep {{ repo" #here`),
		code: codePlaceholderInvalid, message: []string{sweep, "create.title", "{{ repo"},
	},
	{
		name: "empty placeholder", doc: withTriggers(`          brief: "Look at {{check.name}} on {{repo}}"`, `          brief: "Look at {{ }}" #here`),
		code: codePlaceholderInvalid, message: []string{sweep, "create.brief", "{{ }}"},
	},
	{
		name: "placeholder not a field name", doc: withTriggers(`          brief: "Look at {{check.name}} on {{repo}}"`, `          brief: "Look at {{check name}}" #here`),
		code: codePlaceholderInvalid, message: []string{sweep, "create.brief", "{{check name}}"},
	},
	{
		name: "placeholder with an empty segment", doc: withTriggers(`"Fix {{check.name}}" }`, `"Fix {{check..name}}" } #here`),
		code: codePlaceholderInvalid, message: []string{"trigger red-main", "create.title", "{{check..name}}"},
	},
	{
		name: "event not allowed", doc: withTriggers("        on: github.check_run.failed\n", "        on: slack.message #here\n"),
		code: codeSchemaInvalid, message: []string{"spec.board.triggers[1].on"},
	},
	{
		name: "unknown key in create", doc: withTriggers("          lane: backend\n", "          lane: backend\n          colour: red #here\n"),
		code: codeSchemaUnknownKey, message: []string{"colour"},
	},
}

func TestCheck_Triggers(t *testing.T) {
	t.Parallel()

	for _, tt := range triggerRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checkOneProblem(t, tt.doc, tt.code, tt.message)
		})
	}
}

// checkOneProblem checks that doc has no problem when code is "", and otherwise exactly one: an
// error with that code in factory.yaml, on the line marked "#here", whose message names each of
// message.
func checkOneProblem(t *testing.T, doc, code string, message []string) {
	t.Helper()

	_, problems := Check(checkTree(doc, nil))

	if code == "" {
		if len(problems) > 0 {
			t.Fatalf("problems = %+v, want none", problems)
		}

		return
	}

	if len(problems) != 1 {
		t.Fatalf("want one %s problem, got %+v", code, problems)
	}

	p := problems[0]
	if p.Code != code || p.Severity != domain.SeverityError || p.File != FileName || p.Check == "" {
		t.Errorf("problem = %+v, want an error %s in %s with a check", p, code, FileName)
	}

	if want := markedLine(doc); p.Line != want {
		t.Errorf("problem line = %d, want %d: %+v", p.Line, want, p)
	}

	for _, m := range message {
		if !strings.Contains(p.Message, m) {
			t.Errorf("message %q does not name %s", p.Message, m)
		}
	}
}

func TestLoad_ParsesTriggers(t *testing.T) {
	t.Parallel()

	f, err := LoadDir("testdata/support")
	if err != nil {
		t.Fatal(err)
	}

	want := []Trigger{
		{
			Name: "morning-sweep", On: "schedule", Cron: "0 8 * * MON-FRI", TimeZone: "Europe/Berlin",
			Create: TriggerCreate{Type: "question", Station: "inbox", Title: "Inbox sweep"},
		},
		{
			Name: "shop-incident", On: "webhook.shop-alerts.failed",
			Filter: &TriggerFilter{Repo: "acme/shop", SkipBots: true},
			Only:   &TriggerOnly{FailuresInARow: 2, Dedupe: "service"},
			Create: TriggerCreate{
				Type: "incident", Station: "triage", Expedite: true,
				Title: "{{service}} is failing", Brief: "Alert: {{alert.summary}}",
			},
			Limits: &TriggerLimits{PerDay: 3, PauseOnBudget: true},
		},
	}

	if got := f.Spec.Board.Triggers; !reflect.DeepEqual(got, want) {
		t.Errorf("triggers =\n%+v\nwant\n%+v", got, want)
	}
}
