package authoring_test

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
)

// sources is the organization's knowledge sources.
type sources []string

func (s sources) SourceNames(context.Context) ([]string, error) { return s, nil }

// withEnv is acmeEnvironment with the variables vars, YAML lines below env:.
func withEnv(vars ...string) string {
	return acmeEnvironment + "  env:\n    " + strings.Join(vars, "\n    ") + "\n"
}

// mergeYAML is acmeYAML whose review station merges.
var mergeYAML = strings.Replace(acmeYAML, "{ id: review, kind: human }", "{ id: review, kind: human, action: merge }", 1)

// secretsRow is one row of TestSecretsCheck: a definition and the one secrets problem it must
// produce, none when code is "".
type secretsRow struct {
	name     string
	yaml     string
	env      string
	code     string
	severity domain.ProblemSeverity
	file     string
	pointer  string
	fixes    []domain.ProblemFix
	inPlaces []string // the message names these
}

// The secrets check reports each problem at its key, for both editors: a {{secret}} the
// organization lacks or that is not open to the factory, a variable the platform keeps, a key in
// plain text, a knowledge source the organization lacks and a merge without a repository token.
// It never repeats a secret's value.
func TestSecretsCheck(t *testing.T) {
	t.Parallel()

	tests := []secretsRow{
		{name: "every secret exists and is open", yaml: acmeYAML, env: withEnv(`GH_TOKEN: "{{code-token}}"`, "SENTRY_ORG: acme")},
		{
			name: "a model key the organization lacks", yaml: strings.Replace(acmeYAML, "{{claude-key}}", "{{ghost-key}}", 1),
			code: authoring.CodeSecretMissing, file: yamlFile, pointer: "/basics/model_key", inPlaces: []string{"runtime.modelKey", "ghost-key"},
		},
		{
			name: "an environment variable's secret the organization lacks", yaml: acmeYAML, env: withEnv(`DATABASE_URL: "postgres://app:{{db-pass}}@db/app"`),
			code: authoring.CodeSecretMissing, file: envFile, pointer: "/environments/default/env/DATABASE_URL",
			inPlaces: []string{"environments/default.yaml env.DATABASE_URL", "db-pass"},
		},
		{
			name: "a secret open to other factories", yaml: acmeYAML, env: withEnv(`SENTRY_AUTH: "{{other-only}}"`),
			code: authoring.CodeSecretNotInScope, file: envFile, pointer: "/environments/default/env/SENTRY_AUTH", inPlaces: []string{"other-only"},
		},
		{
			name: "a variable the platform keeps", yaml: acmeYAML, env: withEnv("GOAL_RUN: x"),
			code: authoring.CodeReservedEnv, file: envFile, pointer: "/environments/default/env/GOAL_RUN", inPlaces: []string{"GOAL_RUN"},
		},
		{
			name: "a key in plain text", yaml: acmeYAML, env: withEnv("GH_TOKEN: ghp_0123456789abcdef"),
			code: authoring.CodePlainKey, file: envFile, pointer: "/environments/default/env/GH_TOKEN", inPlaces: []string{"ghp_", "{{gh-token}}"},
			fixes: []domain.ProblemFix{{Label: authoring.FixMoveToSecrets, Section: domain.SectionEnvironments, Patch: []domain.PatchOp{
				{Op: "replace", Path: "/default/env/GH_TOKEN", Value: json.RawMessage(`"{{gh-token}}"`)},
			}}},
		},
		{
			name: "a plain value under a key's name", yaml: acmeYAML, env: withEnv("SENTRY_AUTH_TOKEN: abc123"),
			code: authoring.CodePlainKey, severity: domain.SeverityWarning, file: envFile, pointer: "/environments/default/env/SENTRY_AUTH_TOKEN",
			fixes: []domain.ProblemFix{{Label: authoring.FixMoveToSecrets, Section: domain.SectionEnvironments, Patch: []domain.PatchOp{
				{Op: "replace", Path: "/default/env/SENTRY_AUTH_TOKEN", Value: json.RawMessage(`"{{sentry-auth-token}}"`)},
			}}},
		},
		{
			name: "a knowledge source the organization lacks", yaml: strings.Replace(acmeYAML, "  agents:\n", "  knowledge: { sources: [handbook] }\n  agents:\n", 1),
			code: authoring.CodeKnowledgeSourceUnknown, file: yamlFile, pointer: "/basics/knowledge_sources/0", inPlaces: []string{"handbook"},
			fixes: []domain.ProblemFix{{Label: "Remove handbook", Section: domain.SectionBasics, Patch: []domain.PatchOp{{Op: "remove", Path: "/knowledge_sources/0"}}}},
		},
		{name: "a merge with a repository token", yaml: mergeYAML},
		{
			name: "a merge without a repository token", yaml: strings.Replace(mergeYAML, `token: "{{code-token}}", `, "", 1),
			code: authoring.CodeMergeNeedsCodeHost, file: yamlFile, pointer: "/line/stations/2/action", inPlaces: []string{"runtime.repository.token"},
			fixes: []domain.ProblemFix{{Label: "Remove the merge step", Section: domain.SectionLine, Patch: []domain.PatchOp{{Op: "remove", Path: "/stations/2/action"}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.secret(t, "other-only", domain.SecretScope{Factories: []domain.FactoryID{"0b6f1c9e-5d0a-4c39-9a51-000000000000"}})

			files := acmeFiles(tt.yaml)
			if tt.env != "" {
				files[envFile] = []byte(tt.env)
			}

			v, err := h.svc.ValidateFiles(h.ctx, files)
			if err != nil {
				t.Fatal(err)
			}

			wantSecretsProblem(t, tt, v)
		})
	}
}

// wantSecretsProblem fails unless v has the row's one secrets problem, or none, and no problem
// repeats a secret's value.
func wantSecretsProblem(t *testing.T, tt secretsRow, v authoring.Validation) {
	t.Helper()

	found := secretsProblems(t, v)

	if tt.code == "" {
		if len(v.Problems) > 0 || !v.OK {
			t.Fatalf("problems = %+v, want none", v.Problems)
		}

		return
	}

	if len(found) != 1 || found[0].Code != tt.code {
		t.Fatalf("secrets problems = %+v, want one %s", found, tt.code)
	}

	p, sev := found[0], cmp.Or(tt.severity, domain.SeverityError)
	if p.Severity != sev || p.File != tt.file || p.Pointer != tt.pointer || p.Line == 0 || v.OK != (sev != domain.SeverityError) {
		t.Errorf("problem = %+v (ok %v); want a %s in %s at %s, located", p, v.OK, sev, tt.file, tt.pointer)
	}

	if missing := slices.DeleteFunc(slices.Clone(tt.inPlaces), func(s string) bool { return strings.Contains(p.Message, s) }); len(missing) > 0 {
		t.Errorf("message %q does not name %q", p.Message, missing)
	}

	if !reflect.DeepEqual(p.Fixes, tt.fixes) {
		t.Errorf("fixes =\n%+v\nwant\n%+v", p.Fixes, tt.fixes)
	}
}

// secretsProblems are v's problems of the secrets check; it fails on any problem that repeats a
// secret's value.
func secretsProblems(t *testing.T, v authoring.Validation) []domain.DefinitionProblem {
	t.Helper()

	var found []domain.DefinitionProblem

	for _, p := range v.Problems {
		if strings.Contains(p.Message, "test-value-") {
			t.Errorf("problem %s repeats a secret's value: %s", p.Code, p.Message)
		}

		if p.Check == authoring.CheckSecrets {
			found = append(found, p)
		}
	}

	return found
}

// A knowledge source the organization has passes, and the check's green line counts what it found.
func TestSecretsCheck_SummarizesWhatExists(t *testing.T) {
	t.Parallel()

	h := newHarness(t, withKnowledge(sources{"handbook"}))

	v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(strings.Replace(acmeYAML, "  agents:\n", "  knowledge: { sources: [handbook] }\n  agents:\n", 1)))
	if err != nil {
		t.Fatal(err)
	}

	c, ok := checkResult(v, authoring.CheckSecrets)
	if !v.OK || !ok || c.Summary != "2 secrets exist, 1 knowledge source exists" || c.Section != domain.SectionEnvironments {
		t.Errorf("validation ok %v, check %+v (problems %v); want the secrets and the source counted", v.OK, c, codes(v.Problems))
	}
}

// A failure to read what the organization has fails validation; it is never a finding.
func TestSecretsCheck_FailsWhenKnowledgeFails(t *testing.T) {
	t.Parallel()

	h := newHarness(t, withKnowledge(failingSources{}))

	if _, err := h.svc.ValidateFiles(h.ctx, acmeFiles(acmeYAML)); err == nil {
		t.Fatal("validate succeeded; want the knowledge sources' failure")
	}
}

// failingSources cannot list the organization's knowledge sources.
type failingSources struct{}

func (failingSources) SourceNames(context.Context) ([]string, error) {
	return nil, errors.New("knowledge unavailable")
}
