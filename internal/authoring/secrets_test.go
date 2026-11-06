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
type sources []domain.KnowledgeSource

func (s sources) Sources(context.Context) ([]domain.KnowledgeSource, error) { return s, nil }

// uploads are upload sources of the names.
func uploads(names ...string) sources {
	out := make(sources, 0, len(names))
	for _, n := range names {
		out = append(out, domain.KnowledgeSource{Name: n, Kind: domain.SourceUpload})
	}

	return out
}

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
			code: authoring.CodeKnowledgeSourceUnknown, file: yamlFile, pointer: firstSourcePointer, inPlaces: []string{"handbook"},
			fixes: []domain.ProblemFix{{Label: "Remove handbook", Section: domain.SectionBasics, Patch: []domain.PatchOp{{Op: "remove", Path: "/knowledge_sources/0"}}}},
		},
		{
			name: "an environment variable naming a webhook's secret", yaml: acmeYAML, env: withEnv(`HOOK_SECRET: "{{webhook-ci-hooks}}"`),
			code: authoring.CodeSecretSignsWebhook, file: envFile, pointer: "/environments/default/env/HOOK_SECRET",
			inPlaces: []string{hookSecret, "ci-hooks", "forge"},
		},
		{
			name: "a model key naming a webhook's secret", yaml: strings.Replace(acmeYAML, "{{claude-key}}", "{{webhook-ci-hooks}}", 1),
			code: authoring.CodeSecretSignsWebhook, file: yamlFile, pointer: "/basics/model_key", inPlaces: []string{"runtime.modelKey", hookSecret},
		},
		{
			name: "a repository token naming a webhook's secret", yaml: strings.Replace(acmeYAML, "{{code-token}}", "{{webhook-ci-hooks}}", 1),
			code: authoring.CodeSecretSignsWebhook, file: yamlFile, pointer: "/basics/repository/token",
			inPlaces: []string{"runtime.repository.token", hookSecret},
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

			h := newHarness(t, withSecretsCheck(func(c *authoring.SecretsCheck) {
				c.Webhooks = webhookSecrets{{Secret: hookSecret, Webhook: "ci-hooks"}}
			}))
			h.secret(t, "other-only", domain.SecretScope{Factories: []domain.FactoryID{"0b6f1c9e-5d0a-4c39-9a51-000000000000"}})
			h.secret(t, hookSecret, domain.SecretScope{All: true})

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

	h := newHarness(t, withKnowledge(uploads("handbook")))

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

// hookSecret is the secret the organization's webhook ci-hooks is signed by.
const hookSecret = "webhook-ci-hooks" //nolint:gosec // a secret's name, not a value

// webhookSecrets are the secrets the organization's webhooks are signed by.
type webhookSecrets []domain.WebhookSecretUse

func (w webhookSecrets) WebhookSecretUses(context.Context) ([]domain.WebhookSecretUse, error) {
	return w, nil
}

// failingSources cannot list the organization's knowledge sources.
type failingSources struct{}

func (failingSources) Sources(context.Context) ([]domain.KnowledgeSource, error) {
	return nil, errors.New("knowledge unavailable")
}

// repositoryKey is a repository source's config key of its URL, and codeToken a secret open to
// every factory.
const (
	repositoryKey = "repository"
	codeToken     = "code-token"
)

// withSources is acmeYAML whose knowledge.sources are the entries, YAML lines below sources:.
func withSources(entries ...string) string {
	return strings.Replace(acmeYAML, "  agents:\n", "  knowledge:\n    sources:\n      "+strings.Join(entries, "\n      ")+"\n  agents:\n", 1)
}

// Each source a factory lists must exist and must not hand the factory a secret's reach outside
// the secret's scope; a source that copies the factory's own repository is only a warning, since
// its runs already have those files.
func TestSecretsCheck_KnowledgeSources(t *testing.T) {
	t.Parallel()

	org := sources{
		{Name: "handbook", Kind: domain.SourceUpload},
		{Name: "scoped", Kind: domain.SourceRepository, Secret: "other-only", Config: map[string]string{repositoryKey: "https://git.example.com/acme/docs"}},
		{Name: "open", Kind: domain.SourceRepository, Secret: codeToken, Config: map[string]string{repositoryKey: "https://git.example.com/acme/docs"}},
		{Name: "self", Kind: domain.SourceRepository, Secret: codeToken, Config: map[string]string{repositoryKey: "https://GIT.example.com/Acme/Review/"}},
		{
			Name: "self-synced", Kind: domain.SourceSynced, Connection: "code",
			Config: map[string]string{repositoryKey: "https://git.example.com/acme/review.git", "include": "docs/**"},
		},
		{Name: "wiki", Kind: domain.SourceConfluence, Secret: codeToken, Config: map[string]string{"spaces": "ENG"}},
	}

	tests := []secretsRow{
		{name: "sources that exist, by name or object", yaml: withSources("- handbook", "- { name: open, appliesTo: { agents: [engineer] }, readWhen: Before a review. }", "- wiki")},
		{
			name: "an object entry the organization lacks", yaml: withSources("- handbook", "- { name: ghost, readWhen: Never. }"),
			code: authoring.CodeKnowledgeSourceUnknown, file: yamlFile, pointer: "/basics/knowledge_sources/1", inPlaces: []string{"ghost"},
			fixes: []domain.ProblemFix{{Label: "Remove ghost", Section: domain.SectionBasics, Patch: []domain.PatchOp{{Op: "remove", Path: "/knowledge_sources/1"}}}},
		},
		{
			name: "a source whose secret is not open to the factory", yaml: withSources("- handbook", "- scoped"),
			code: authoring.CodeKnowledgeSecretNotInScope, file: yamlFile, pointer: "/basics/knowledge_sources/1", inPlaces: []string{"scoped", "other-only"},
		},
		{
			name: "a repository source of the factory's own repository", yaml: withSources("- self"),
			code: authoring.CodeKnowledgeCopiesCheckout, severity: domain.SeverityWarning, file: yamlFile, pointer: firstSourcePointer,
			inPlaces: []string{"self", "https://git.example.com/acme/review.git"},
		},
		{
			name: "a repository source of the factory's one entry of runtime.repositories", yaml: withRepositories(withSources("- self"),
				`{ name: review, url: "https://git.example.com/acme/review", token: "{{code-token}}", access: write }`),
			code: authoring.CodeKnowledgeCopiesCheckout, severity: domain.SeverityWarning, file: yamlFile, pointer: firstSourcePointer,
			inPlaces: []string{"self", "https://git.example.com/acme/review"},
		},
		{name: "a repository source of one of the factory's two repositories", yaml: withRepositories(withSources("- self"),
			`{ name: review, url: "https://git.example.com/acme/review", token: "{{code-token}}", access: write }`,
			`{ name: docs, url: "https://git.example.com/acme/docs", token: "{{code-token}}", access: read }`)},
		{
			name: "a synced repository source of the factory's own repository", yaml: withSources("- { name: self-synced, appliesTo: { agents: [engineer] } }"),
			code: authoring.CodeKnowledgeCopiesCheckout, severity: domain.SeverityWarning, file: yamlFile, pointer: firstSourcePointer,
			inPlaces: []string{"self-synced"},
		},
		{name: "a repository source while the factory has no repository", yaml: strings.Replace(withSources("- self"),
			"    repository: { url: \"https://git.example.com/acme/review.git\", token: \"{{code-token}}\", forge: github }\n", "", 1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, withKnowledge(org))
			h.secret(t, "other-only", domain.SecretScope{Factories: []domain.FactoryID{"0b6f1c9e-5d0a-4c39-9a51-000000000000"}})

			v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(tt.yaml))
			if err != nil {
				t.Fatal(err)
			}

			wantSecretsProblem(t, tt, v)
		})
	}
}

// firstSourcePointer is where the first entry of knowledge.sources shows in the editor.
const firstSourcePointer = "/basics/knowledge_sources/0"

// withRepositories is yaml with runtime.repositories' entries in place of runtime.repository.
func withRepositories(yaml string, entries ...string) string {
	return strings.Replace(yaml, "    repository: { url: \"https://git.example.com/acme/review.git\", token: \"{{code-token}}\", forge: github }\n",
		"    repositories: ["+strings.Join(entries, ", ")+"]\n", 1)
}

// withRepository is acmeYAML whose repository is the runtime YAML lines repo.
func withRepository(repo string) string {
	return strings.Replace(acmeYAML,
		"    repository: { url: \"https://git.example.com/acme/review.git\", token: \"{{code-token}}\", forge: github }\n", repo, 1)
}

// A repository token goes only to a code host the organization lists: the built-in github.com
// and gitlab.com, or one an owner added. A URL on any other host is refused at its key.
func TestSecretsCheck_RepositoryHosts(t *testing.T) {
	t.Parallel()

	tests := []secretsRow{
		{name: "the shorthand on an added host", yaml: acmeYAML},
		{name: "the shorthand on github.com", yaml: withRepository("    repository: { url: \"https://github.com/acme/review\", token: \"{{code-token}}\" }\n")},
		{name: "an entry on gitlab.com", yaml: withRepository(
			"    repositories: [{ name: review, url: \"https://gitlab.com/acme/review\", token: \"{{code-token}}\", access: write }]\n")},
		{name: "an owner pattern on an added host", yaml: withRepository(
			"    repositories: [{ name: acme, url: \"https://GIT.example.com/acme/*\", token: \"{{code-token}}\", access: write }]\n")},
		{
			name: "the shorthand on a host nobody listed",
			yaml: withRepository("    repository: { url: \"https://git.example.org/acme/review.git\", token: \"{{code-token}}\", forge: github }\n"),
			code: authoring.CodeRepositoryHostUnlisted, file: yamlFile, pointer: "/basics/repository/url",
			inPlaces: []string{"git.example.org", "Code hosts"},
		},
		{
			name: "an entry on a host nobody listed",
			yaml: withRepository(
				"    repositories: [{ name: review, url: \"https://git.example.org/acme/review\", token: \"{{code-token}}\", access: write }]\n"),
			code: authoring.CodeRepositoryHostUnlisted, file: yamlFile, inPlaces: []string{"review", "git.example.org"},
		},
		{
			name: "an entry on a listed host's other port",
			yaml: withRepository(
				"    repositories: [{ name: review, url: \"https://git.example.com:8443/acme/review\", token: \"{{code-token}}\", access: write }]\n"),
			code: authoring.CodeRepositoryHostUnlisted, file: yamlFile, inPlaces: []string{"git.example.com:8443"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)

			v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(tt.yaml))
			if err != nil {
				t.Fatal(err)
			}

			wantSecretsProblem(t, tt, v)
		})
	}

	t.Run("without the organization's hosts, only the built-in ones", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, withSecretsCheck(func(c *authoring.SecretsCheck) { c.Hosts = nil }))

		v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(acmeYAML))
		if err != nil {
			t.Fatal(err)
		}

		wantSecretsProblem(t, secretsRow{code: authoring.CodeRepositoryHostUnlisted, file: yamlFile, pointer: "/basics/repository/url"}, v)
	})
}
