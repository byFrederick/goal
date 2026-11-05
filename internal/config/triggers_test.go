package config

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

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
        filter: { branch: main, repo: acme/app, authors: [acme-bot], skipBots: true }
        only: { failuresInARow: 2, dedupe: check.name }
        create: { type: fix, station: work, expedite: true, title: "Fix {{check.name}}" }
        limits: { perDay: 3, pauseOnBudget: true }
      - name: deploy-alert
        on: webhook.deploys.failed
        create: { type: fix, station: inbox }
      - name: pushes
        on: github.push
        filter: { repo: [acme/app, acme/web], paths: ["**/*.go"], match: { head_commit.author.name: [jo, al], forced: "false" } }
        create: { type: fix, station: inbox, title: "Push {{payload.after}}" }
        limits: { perHour: 4 }
`

// withTriggers is baseFactory with triggerBlock in its board, after the given replacements of
// the block, and station work routing lane backend.
func withTriggers(pairs ...string) string {
	return edited("  board:\n", "  board:\n"+replaced(triggerBlock, pairs...),
		workAgentLine, "        route: { backend: worker }\n")
}

// redMain names the second trigger of triggerBlock in a message.
const redMain = "trigger red-main"

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
		code: codePlaceholderInvalid, message: []string{redMain, "create.title", "{{check..name}}"},
	},
	{
		name: "brief file not found", doc: withTriggers(`          brief: "Look at {{check.name}} on {{repo}}"`, `          brief: ./triggers/sweep.md #here`),
		code: codeBriefNotFound, message: []string{sweep, "./triggers/sweep.md", "not found"},
	},
	{
		name: "brief that only mentions a file", doc: withTriggers(`          brief: "Look at {{check.name}} on {{repo}}"`, `          brief: "Read ./triggers/sweep.md first"`),
	},
	{
		name: "model check without a question", doc: withTriggers("dedupe: check.name }", "dedupe: check.name, modelAgrees: { model: claude-haiku-5-5 } } #here"),
		code: codeSchemaRequired, message: []string{"question"},
	},
	{
		name: "event not allowed", doc: withTriggers("        on: github.check_run.failed\n", "        on: Slack.Message #here\n"),
		code: codeSchemaInvalid, message: []string{"spec.board.triggers[1].on"},
	},
	{
		name: "a sender without an event", doc: withTriggers("        on: github.check_run.failed\n", "        on: github #here\n"),
		code: codeOnNeedsEvent, message: []string{redMain, "github.<event>"},
	},
	{
		name: "a filter the sender's event never supplies",
		doc:  withTriggers("repo: acme/app, authors:", `repo: acme/app, paths: ["**/*.go"], authors:`, "skipBots: true }\n", "skipBots: true } #here\n"),
		code: CodeFilterUnsupplied, message: []string{redMain, "github.check_run carries no paths", "filter.paths"},
	},
	{
		name: "a label on a push", doc: withTriggers("filter: { repo: [acme/app", "filter: { label: bug, repo: [acme/app", `forced: "false" } }`+"\n", `forced: "false" } } #here`+"\n"),
		code: CodeFilterUnsupplied, message: []string{"trigger pushes", "github.push carries no label"},
	},
	{
		name: "trusted on a GitHub sender with authors", doc: withTriggers("expedite: true,", "expedite: true, trusted: true,"),
	},
	{
		name: "trusted on a GitHub sender without authors",
		doc:  withTriggers(`create: { type: fix, station: inbox, title: "Push`, `create: { type: fix, station: inbox, trusted: true, title: "Push`, `{{payload.after}}" }`+"\n", `{{payload.after}}" } #here`+"\n"),
		code: CodeTrustedRefused, message: []string{"trigger pushes", "filter.authors"},
	},
	{
		name: "trusted on the gitlab sender",
		doc: withTriggers("        on: github.push\n", "        on: gitlab.push\n", `create: { type: fix, station: inbox, title: "Push`,
			`create: { type: fix, station: inbox, trusted: true, title: "Push`, `{{payload.after}}" }`+"\n", `{{payload.after}}" } #here`+"\n"),
		code: CodeTrustedRefused, message: []string{"trigger pushes", "gitlab sender"},
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

	f, err := LoadDir("testdata/.support")
	if err != nil {
		t.Fatal(err)
	}

	want := []Trigger{
		{
			Name: "morning-sweep", On: "schedule", Cron: "0 8 * * MON-FRI", TimeZone: "Europe/Berlin",
			Create: TriggerCreate{Type: "question", Station: "inbox", Title: "Inbox sweep", Brief: "./triggers/morning-sweep.md"},
		},
		{
			Name: "shop-incident", On: "webhook.shop-alerts.failed",
			Filter: &TriggerFilter{Repo: StringList{"acme/shop"}, SkipBots: true},
			Only: &TriggerOnly{
				FailuresInARow: 2, Dedupe: "service", ModelAgrees: &TriggerModelAgrees{Question: "Is this a real outage rather than a blip?"},
			},
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

	// A brief that names a file of the definition is that file's text (ADR 0040 decision 17).
	if got := f.Line().Triggers[0].Create.Brief; got != supportSweepBrief {
		t.Errorf("morning-sweep's brief = %q; want the text of ./triggers/morning-sweep.md", got)
	}
}

// supportSweepBrief is testdata/.support/triggers/morning-sweep.md.
const supportSweepBrief = "Sweep the inbox: answer what you can and route the rest.\n"

// A live version whose trigger has a refusal of the deliveries check (written before ADR 0040
// decision 14) still loads, so its factory keeps running; publish refuses it.
func TestLoad_KeepsDeliveryRefusals(t *testing.T) {
	t.Parallel()

	doc := withTriggers("repo: acme/app, authors:", `repo: acme/app, paths: ["**/*.go"], authors:`)

	_, problems := Check(checkTree(doc, nil))
	if len(problems) != 1 || problems[0].Check != CheckDeliveries || !problems[0].Blocking() {
		t.Fatalf("problems = %+v; want one blocking deliveries problem", problems)
	}

	f, err := Load(checkTree(doc, nil))
	if err != nil {
		t.Fatalf("Load = %v; want it to load", err)
	}

	if got := f.Line().Triggers[1].On; got != "github.check_run" {
		t.Errorf("red-main on = %q; want github.check_run", got)
	}
}

// checkAgent is the agent the check rows' checks run.
const checkAgent = "worker"

// withCheck is withTriggers with nightly-sweep firing on cron and running a check by agent with
// the extra check lines.
func withCheck(cron, agent, extra string) string {
	return withTriggers(`        cron: "0 6 * * MON-FRI"`, `        cron: "`+cron+`"`,
		"        timeZone: Europe/Berlin\n", "        timeZone: Europe/Berlin\n        check: { agent: "+agent+", brief: Look for new incidents."+extra+" }\n")
}

// problemsOf are doc's problems with code.
func problemsOf(t *testing.T, doc string, extra map[string]string, code string) []domain.DefinitionProblem {
	t.Helper()

	_, problems := Check(checkTree(doc, extra))

	var out []domain.DefinitionProblem

	for _, p := range problems {
		if p.Code == code {
			out = append(out, p)
		}
	}

	return out
}

// A check's cron may fire at most every 5 minutes; a schedule without a check may fire more
// often. A check names an agent of the definition, a brief file that exists and a timeout of at
// most an hour, and loads with its defaults.
func TestCheck_TriggerCheckCronFloor(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, doc string
		extra     map[string]string
		code      string
	}{
		{"every 5 minutes", withCheck("*/5 * * * *", checkAgent, ""), nil, ""},
		{"every 2 minutes", withCheck("*/2 * * * *", checkAgent, ""), nil, codeCheckCronTooOften},
		{"two minutes apart in an hour", withCheck("0,2 9 * * *", checkAgent, ""), nil, codeCheckCronTooOften},
		{"every 2 minutes without a check", withTriggers(`        cron: "0 6 * * MON-FRI"`, `        cron: "*/2 * * * *"`), nil, ""},
		{"unknown agent", withCheck("*/5 * * * *", "ghost", ""), nil, codeUnknownAgent},
		{"brief file missing", replaced(withCheck("*/5 * * * *", checkAgent, ""), "brief: Look for new incidents.", "brief: ./triggers/incidents.md"), nil, codeBriefNotFound},
		{
			"brief file", replaced(withCheck("*/5 * * * *", checkAgent, ""), "brief: Look for new incidents.", "brief: ./triggers/incidents.md"),
			map[string]string{"triggers/incidents.md": "Look for new incidents.\n"},
			"",
		},
		{"timeout over an hour", withCheck("*/5 * * * *", checkAgent, ", timeout: 2h"), nil, codeCheckTimeoutInvalid},
		{"perCheck on a check", replaced(withCheck("*/5 * * * *", checkAgent, ""), "        create:\n          type: feature\n", "        limits: { perCheck: 2 }\n        create:\n          type: feature\n"), nil, ""},
		{"perCheck without a check", withTriggers("        limits: { perDay: 3, pauseOnBudget: true }", "        limits: { perDay: 3, perCheck: 2 }"), nil, codePerCheckWithoutCheck},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			errs := errorsOf(checkTree(tt.doc, tt.extra))
			if tt.code == "" && len(errs) > 0 || tt.code != "" && (len(errs) != 1 || errs[0].Code != tt.code) {
				t.Errorf("errors = %+v; want %q", errs, tt.code)
			}
		})
	}
}

// errorsOf are the error problems of the definition files.
func errorsOf(files fstest.MapFS) []domain.DefinitionProblem {
	_, problems := Check(files)

	var errs []domain.DefinitionProblem

	for _, p := range problems {
		if p.Severity == domain.SeverityError {
			errs = append(errs, p)
		}
	}

	return errs
}

// A check loads with its brief file's text and the defaults.
func TestCheck_TriggerCheckLoads(t *testing.T) {
	t.Parallel()

	f, err := Load(checkTree(replaced(withCheck("*/5 * * * *", checkAgent, ""), "brief: Look for new incidents.", "brief: ./triggers/incidents.md"),
		map[string]string{"triggers/incidents.md": "Look for new incidents.\n"}))
	if err != nil {
		t.Fatal(err)
	}

	tr, _ := f.Line().Trigger("nightly-sweep")
	if c := tr.Check; c == nil || c.Agent != checkAgent || c.Brief != "Look for new incidents.\n" || c.MaxUSD != domain.DefaultCheckMaxUSD || c.Timeout != domain.DefaultCheckTimeout {
		t.Errorf("check = %+v; want the worker with the brief file's text and the defaults", tr.Check)
	}

	if tr.Limits.PerCheck != domain.DefaultCheckPerCheck {
		t.Errorf("limits.perCheck = %d; want the default %d", tr.Limits.PerCheck, domain.DefaultCheckPerCheck)
	}

	if other, _ := f.Line().Trigger("red-main"); other.Limits.PerCheck != 0 {
		t.Errorf("a trigger without a check has perCheck %d; want none", other.Limits.PerCheck)
	}
}

// A check on an event trigger is refused by the schema.
func TestCheck_TriggerCheckOnlyOnASchedule(t *testing.T) {
	t.Parallel()

	doc := withTriggers("        create: { type: fix, station: inbox }\n", "        create: { type: fix, station: inbox }\n        check: { agent: worker, brief: b }\n")
	if _, problems := Check(checkTree(doc, nil)); len(problems) == 0 {
		t.Error("a check on deploy-alert, an event trigger, has no problem; want the schema's")
	}
}

// Publish warns when a check's agent may use tools that aren't read-only: built-in edit, run or
// subagents, or an MCP server of its environment with a tool it allows.
func TestCheck_TriggerCheckWarnsWriteTools(t *testing.T) {
	t.Parallel()

	readOnlyEnv := baseEnvironment + "  mcp:\n    pager: { url: \"https://pager.example.com/mcp\", tools: { \"*\": off, \"get_*\": ask } }\n"
	openEnv := baseEnvironment + "  mcp:\n    pager: { url: \"https://pager.example.com/mcp\", tools: { \"get_*\": allow } }\n"
	gitHubEnv := baseEnvironment + "  mcp:\n    github: { url: \"https://api.githubcopilot.com/mcp/\" }\n"

	for _, tt := range []struct {
		name, doc string
		env       string
		want      string
	}{
		{"read-only", withCheck("*/5 * * * *", checkAgent, ""), readOnlyEnv, ""},
		{"edit allowed", replaced(withCheck("*/5 * * * *", checkAgent, ""), agentPrompt, agentPrompt+"      builtins: { read: allowed, edit: allowed }\n"), "", "builtins.edit"},
		{"run allowed", replaced(withCheck("*/5 * * * *", checkAgent, ""), agentPrompt, agentPrompt+"      builtins: { run: allowed }\n"), "", "builtins.run"},
		{"an MCP tool allowed", withCheck("*/5 * * * *", checkAgent, ""), openEnv, "mcp pager"},
		{"a GitHub server, judged by the secrets check", withCheck("*/5 * * * *", checkAgent, ""), gitHubEnv, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var extra map[string]string
			if tt.env != "" {
				extra = map[string]string{defaultEnvFile: tt.env}
			}

			got := problemsOf(t, tt.doc, extra, codeCheckToolsNotReadOnly)

			switch {
			case tt.want == "" && len(got) > 0:
				t.Errorf("warnings = %+v; want none", got)
			case tt.want != "" && (len(got) != 1 || got[0].Severity != domain.SeverityWarning || !strings.Contains(got[0].Message, tt.want)):
				t.Errorf("warnings = %+v; want one naming %q", got, tt.want)
			}
		})
	}
}
