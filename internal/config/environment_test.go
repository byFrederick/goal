package config

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// fullEnvironment is ADR 0037's example environment file, with every key.
const fullEnvironment = `apiVersion: goal/v1
kind: Environment
metadata: {name: default}
spec:
  size: medium
  image: ghcr.io/acme/box:2026.11
  tools: [go@1.26, node@22, gh@2]
  setup:
    - pnpm install --frozen-lockfile
  env:
    GH_TOKEN: "{{github-token}}"
    DATABASE_URL: "postgres://app:{{db-pass}}@db/app"
    SENTRY_ORG: acme
  files:
    ~/.kube/config: "{{kube-staging}}"
    /etc/acme/key.pem: "{{ acme-key }}"
  mcp:
    linear:
      url: https://mcp.linear.app/mcp
      headers: {Authorization: "Bearer {{linear-key}}"}
      tools: {create_comment: ask, "delete_*": off}
    playwright:
      command: [npx, "@playwright/mcp@0.0.41"]
      env: {PLAYWRIGHT_TOKEN: "{{playwright-token}}"}
`

// The environment schema takes ADR 0037's file and refuses what decision 5 rules out, each a
// schema problem located in the environment's file.
func TestEnvironmentSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, env, code string
	}{
		{name: "the ADR's example", env: fullEnvironment},
		{name: "only the kind", env: "apiVersion: goal/v1\nkind: Environment\nspec: {}\n"},
		{name: "a size that is not one", env: replaced(fullEnvironment, "size: medium", "size: huge #here"), code: codeSchemaEnum},
		{name: "another kind", env: replaced(fullEnvironment, "kind: Environment", "kind: Factory #here"), code: codeSchemaEnum},
		{name: "an unknown key", env: replaced(fullEnvironment, "  size: medium\n", "  size: medium\n  colour: red #here\n"), code: codeSchemaUnknownKey},
		{name: "a relative file path", env: replaced(fullEnvironment, "~/.kube/config:", ".kube/config:"), code: codeSchemaInvalid},
		{name: "a file that is not a reference", env: replaced(fullEnvironment, `"{{kube-staging}}"`, "plain text #here"), code: codeSchemaInvalid},
		{name: "a tool mode that is not one", env: replaced(fullEnvironment, "create_comment: ask", "create_comment: maybe"), code: codeSchemaEnum},
		{
			name: "a server both remote and stdio",
			env:  replaced(fullEnvironment, "      url: https://mcp.linear.app/mcp\n", "      url: https://mcp.linear.app/mcp\n      command: [linear-mcp]\n"),
			code: codeSchemaInvalid,
		},
		{name: "a server neither remote nor stdio", env: replaced(fullEnvironment, "      url: https://mcp.linear.app/mcp\n", ""), code: codeSchemaInvalid},
		{
			name: "a remote server with env",
			env:  replaced(fullEnvironment, "      url: https://mcp.linear.app/mcp\n", "      url: https://mcp.linear.app/mcp\n      env: {A: b}\n"),
			code: codeSchemaInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, problems := Check(checkTree(baseFactory, map[string]string{defaultEnvFile: tt.env}))

			if tt.code == "" {
				if len(problems) > 0 {
					t.Fatalf("problems = %+v, want none", problems)
				}

				if envs := f.Environments(); len(envs) != 1 || envs[0].Name != DefaultEnvironment {
					t.Errorf("environments = %+v, want default", envs)
				}

				return
			}

			wantSchemaProblem(t, problems, tt.code, markedLine(tt.env))
		})
	}
}

// wantSchemaProblem fails unless problems have a schema error of code in the default
// environment's file, at line when it is not 0.
func wantSchemaProblem(t *testing.T, problems []domain.DefinitionProblem, code string, line int) {
	t.Helper()

	i := indexOf(problems, code)
	if i < 0 {
		t.Fatalf("no %s problem in %+v", code, problems)
	}

	p := problems[i]
	if p.Check != checkSchema || p.File != defaultEnvFile || p.Section != domain.SectionEnvironments || !p.Blocking() || p.Line == 0 {
		t.Errorf("problem = %+v, want a located schema error in %s", p, defaultEnvFile)
	}

	if line > 0 && p.Line != line {
		t.Errorf("problem line = %d, want %d", p.Line, line)
	}
}

// envRow is one row of TestCheck_EnvironmentFiles: a factory.yaml, the files besides it, and the
// one problem they must produce in file, at its #here line.
type envRow struct {
	name, doc string
	files     map[string]string
	code      string
	file      string
	pointer   string
	fixes     []string
}

// The checks of environment files and what names them: every factory has default.yaml, agents,
// judges and cases name environments that exist, files are named and written well, and a
// repository on another host names its forge.
func TestCheck_EnvironmentFiles(t *testing.T) {
	t.Parallel()

	browser := "environments/browser.yaml"
	withAgentEnv := func(env string) string {
		return edited(agentPrompt, agentPrompt+"      environment: "+env+" #here\n")
	}
	withRepo := func(repo string) string {
		return edited(runtimeModel, runtimeModel+"    repository: "+repo+" #here\n")
	}

	rows := []envRow{
		{name: "a default environment", doc: baseFactory},
		{name: "an agent in a named environment", doc: withAgentEnv("browser"), files: map[string]string{browser: baseEnvironment}},
		{name: "a judge and a case in a named environment", doc: baseFactory, files: map[string]string{
			browser: baseEnvironment, judgeFile: replaced(validJudge, "enabled: true\n", "enabled: true\nenvironment: browser\n"),
			caseFile: replaced(validCase, "  agent: worker\n", "  agent: worker\n  environment: browser\n"),
		}},
		{
			name: "no default environment", doc: baseFactory, files: map[string]string{defaultEnvFile: absent, browser: baseEnvironment},
			code: codeEnvironmentDefaultMissing, file: defaultEnvFile,
		},
		{
			name: "an agent names an unknown environment", doc: withAgentEnv("browser"),
			code: codeEnvironmentUnknown, file: FileName, pointer: "/agents/agents/0/environment",
		},
		{
			name: "a judge names an unknown environment", doc: baseFactory,
			files: map[string]string{judgeFile: replaced(validJudge, "enabled: true\n", "enabled: true\nenvironment: browser #here\n")},
			code:  codeEnvironmentUnknown, file: judgeFile,
		},
		{
			name: "a case names an unknown environment", doc: baseFactory, files: map[string]string{
				judgeFile: validJudge, caseFile: replaced(validCase, "  agent: worker\n", "  agent: worker\n  environment: browser #here\n"),
			},
			code: codeEnvironmentUnknown, file: caseFile,
		},
		{
			name: "a file name that is not a slug", doc: baseFactory, files: map[string]string{"environments/Big_Box.yaml": baseEnvironment},
			code: codeEnvironmentNameInvalid, file: "environments/Big_Box.yaml",
		},
		{
			name: "metadata.name is not the file name", doc: baseFactory,
			files: map[string]string{defaultEnvFile: replaced(baseEnvironment, "spec:\n", "metadata: {name: browser} #here\nspec:\n")},
			code:  codeEnvironmentNameMismatch, file: defaultEnvFile, pointer: "/environments/default/metadata/name",
		},
		{
			name: "a tool that is not a mise spec", doc: baseFactory,
			files: map[string]string{defaultEnvFile: baseEnvironment + "  tools: [\"go 1.26\"] #here\n"},
			code:  codeToolSpecInvalid, file: defaultEnvFile, pointer: "/environments/default/tools/0", fixes: []string{"Remove go 1.26"},
		},
		{name: "a repository on github.com", doc: withRepo(`{ url: "https://github.com/acme/app", token: "{{gh-token}}" }`)},
		{name: "a repository on gitlab.com", doc: withRepo(`{ url: "https://gitlab.com/acme/app", token: "{{gl-token}}" }`)},
		{name: "a repository on another host with its forge", doc: withRepo(`{ url: "https://git.example.com/acme/app", forge: gitlab }`)},
		{
			name: "a repository on another host without a forge", doc: withRepo(`{ url: "https://git.example.com/acme/app", token: "{{gl-token}}" }`),
			code: codeForgeMissing, file: FileName, pointer: "/basics/repository/url", fixes: []string{"Set forge to github", "Set forge to gitlab"},
		},
	}

	runEnvRows(t, rows)
}

// runEnvRows runs each row's definition through Check and wants none of its problems or exactly
// one of its code: an error in its file, at the #here line, with its pointer and fixes.
func runEnvRows(t *testing.T, rows []envRow) {
	t.Helper()

	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, problems := Check(checkTree(tt.doc, tt.files))
			if tt.code == "" {
				if len(problems) > 0 {
					t.Fatalf("problems = %+v, want none", problems)
				}

				return
			}

			wantEnvProblem(t, tt, problems)
		})
	}
}

// wantEnvProblem fails unless problems hold exactly one of the row's code, located as the row says.
func wantEnvProblem(t *testing.T, tt envRow, problems []domain.DefinitionProblem) {
	t.Helper()

	var found []domain.DefinitionProblem

	for _, p := range problems {
		if p.Code == tt.code {
			found = append(found, p)
		}
	}

	if len(found) != 1 {
		t.Fatalf("want one %s problem, got %+v", tt.code, problems)
	}

	text := tt.doc
	if tt.file != FileName {
		text = tt.files[tt.file]
	}

	if p := found[0]; p.File != tt.file || !p.Blocking() || p.Check == "" || p.Line != markedLine(text) ||
		tt.pointer != "" && p.Pointer != tt.pointer || !slices.Equal(fixLabels(p.Fixes), tt.fixes) {
		t.Errorf("problem = %+v; want an error in %s at line %d, pointer %q, fixes %q", p, tt.file, markedLine(text), tt.pointer, tt.fixes)
	}
}

func fixLabels(fixes []domain.ProblemFix) []string {
	var out []string
	for _, f := range fixes {
		out = append(out, f.Label)
	}

	return out
}

// A {{secret}} reference is valid only in decision 6's places: an environment file's env, files
// and mcp headers and stdio env, runtime.repository.token, runtime.modelKey and a trigger's
// modelAgrees.key. Anywhere else it is secret-placement, and a reference that names no secret
// is secret-ref-invalid; a trigger's title and brief keep their {{field}} placeholders.
func TestCheck_SecretPlacement(t *testing.T) {
	t.Parallel()

	rows := []envRow{
		{
			name: "every allowed place", doc: replaced(withTriggers("dedupe: check.name }", `dedupe: check.name, modelAgrees: { question: "Real?", key: "{{haiku-key}}" } }`),
				runtimeModel, runtimeModel+`    repository: { url: "https://github.com/acme/app", token: "{{gh-token}}" }`+"\n"),
			files: map[string]string{defaultEnvFile: fullEnvironment},
		},
		{
			name: "an agent's description", doc: edited(agentPrompt, agentPrompt+"      description: Uses {{gh-token}} #here\n"),
			code: codeSecretPlacement, file: FileName, pointer: "/agents/agents/0/description",
		},
		{
			name: "a station's name", doc: edited("      - id: work\n", "      - id: work\n        name: \"{{x}}\" #here\n"),
			code: codeSecretPlacement, file: FileName,
		},
		{
			name: "an environment's setup", doc: baseFactory,
			files: map[string]string{defaultEnvFile: baseEnvironment + "  setup: [\"echo {{gh-token}}\"] #here\n"},
			code:  codeSecretPlacement, file: defaultEnvFile, pointer: "/environments/default/setup/0",
		},
		{
			name: "a remote server's url", doc: baseFactory,
			files: map[string]string{defaultEnvFile: baseEnvironment + "  mcp:\n    linear:\n      url: \"https://mcp.example.com/{{key}}\" #here\n"},
			code:  codeSecretPlacement, file: defaultEnvFile,
		},
		{
			name: "a model key that is not a reference", doc: edited(runtimeModel, "    modelKey: sk-ant-api03-plain #here\n"),
			code: codeReferenceInvalid, file: FileName, pointer: "/basics/model_key",
		},
		{
			name: "a reference that names no secret", doc: baseFactory,
			files: map[string]string{defaultEnvFile: baseEnvironment + "  env:\n    API: \"{{Not A Name}}\" #here\n"},
			code:  codeReferenceInvalid, file: defaultEnvFile,
		},
		{
			name: "an unclosed reference", doc: baseFactory,
			files: map[string]string{defaultEnvFile: baseEnvironment + "  env:\n    API: \"{{api-key\" #here\n"},
			code:  codeReferenceInvalid, file: defaultEnvFile,
		},
	}

	runEnvRows(t, rows)
}

// SecretRefs lists every reference in an allowed place with the key it is at, so authoring's
// checks can find each secret and say where it is used.
func TestFactory_SecretRefs(t *testing.T) {
	t.Parallel()

	doc := edited(runtimeModel, runtimeModel+`    repository: { url: "https://github.com/acme/app", token: "{{gh-token}}" }`+"\n")

	f, problems := Check(checkTree(doc, map[string]string{defaultEnvFile: fullEnvironment}))
	if len(problems) > 0 {
		t.Fatalf("problems = %+v, want none", problems)
	}

	var got []string

	for _, r := range f.SecretRefs() {
		if r.Location.Line == 0 || r.Location.File == "" {
			t.Errorf("reference %+v is not located", r)
		}

		got = append(got, r.Name+" @ "+r.Where)
	}

	want := []string{
		"claude-key @ runtime.modelKey",
		"gh-token @ runtime.repository.token",
		"github-token @ environments/default.yaml env.GH_TOKEN",
		"db-pass @ environments/default.yaml env.DATABASE_URL",
		"kube-staging @ environments/default.yaml files.~/.kube/config",
		"acme-key @ environments/default.yaml files./etc/acme/key.pem",
		"linear-key @ environments/default.yaml mcp.linear.headers.Authorization",
		"playwright-token @ environments/default.yaml mcp.playwright.env.PLAYWRIGHT_TOKEN",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("references =\n%q\nwant\n%q", got, want)
	}

	env := f.Environments()[0]
	fix := env.ReplaceFix("Move to Secrets", at("spec", "env", "SENTRY_ORG"), "{{sentry-org}}")

	if want := (domain.ProblemFix{Label: "Move to Secrets", Section: domain.SectionEnvironments, Patch: []domain.PatchOp{
		{Op: domain.PatchReplace, Path: "/default/env/SENTRY_ORG", Value: json.RawMessage(`"{{sentry-org}}"`)},
	}}); !reflect.DeepEqual(fix, want) || !strings.HasPrefix(env.Locate("spec", "env", "SENTRY_ORG").Pointer, "/environments/default/env/") {
		t.Errorf("fix = %+v, want %+v", fix, want)
	}
}
