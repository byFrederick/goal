package authoring_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
)

func checkIDs(v authoring.Validation) []string {
	out := make([]string, 0, len(v.Checks))
	for _, c := range v.Checks {
		out = append(out, c.ID)
	}

	return out
}

func checkResult(v authoring.Validation, id string) (authoring.CheckResult, bool) {
	for _, c := range v.Checks {
		if c.ID == id {
			return c, true
		}
	}

	return authoring.CheckResult{}, false
}

func TestValidate_InterimCredentialsCheck(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, ref, code string
	}{
		{name: "a set variable passes", ref: "env:ACME_GIT_TOKEN"},
		{name: "an unset variable", ref: "env:ACME_MISSING", code: authoring.CodeSecretEnvUnset},
		{name: "an empty variable", ref: "env:ACME_EMPTY", code: authoring.CodeSecretEnvUnset},
		{name: "a goald setting", ref: "env:GOAL_DATABASE_URL", code: authoring.CodeEnvReserved},
		{name: "a credential reference", ref: "credential:acme-github", code: authoring.CodeCredentialsUnavailable},
		{name: "another provider's reference", ref: "\"op://acme/github/token\"", code: authoring.CodeCredentialsUnavailable},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)

			v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(strings.Replace(acmeYAML, "token: env:ACME_GIT_TOKEN", "token: "+c.ref, 1)))
			if err != nil {
				t.Fatal(err)
			}

			res, ran := checkResult(v, authoring.CheckCredentials)
			if !ran {
				t.Fatalf("credentials check did not run: %v", checkIDs(v))
			}

			if c.code == "" {
				want := authoring.CheckResult{
					ID: authoring.CheckCredentials, Section: domain.SectionConnections, Status: authoring.CheckPassed,
					Summary: "1 secret reference resolves",
				}
				if !v.OK || res != want {
					t.Errorf("validation = ok %v, %+v, problems %v", v.OK, res, codes(v.Problems))
				}

				return
			}

			assertTokenProblem(t, v, c.code)
		})
	}
}

// assertTokenProblem requires an error problem of code from the credentials check, located on
// the repository token, naming no value.
func assertTokenProblem(t *testing.T, v authoring.Validation, code string) {
	t.Helper()

	p, ok := problem(v.Problems, code)
	if !ok || v.OK {
		t.Fatalf("validation = ok %v, problems %v; want %s", v.OK, codes(v.Problems), code)
	}

	if res, _ := checkResult(v, authoring.CheckCredentials); res.Status != authoring.CheckFailed {
		t.Errorf("credentials check = %+v", res)
	}

	type place struct {
		severity              domain.ProblemSeverity
		check, section, point string
		line                  int
	}

	got := place{p.Severity, p.Check, p.Section, p.Pointer, p.Line}
	want := place{domain.SeverityError, authoring.CheckCredentials, domain.SectionBasics, "/basics/repository/token", 13}

	if got != want {
		t.Errorf("problem at %+v, want %+v", got, want)
	}

	if strings.Contains(p.Message, "a-token") {
		t.Errorf("message leaks a value: %s", p.Message)
	}
}

func TestEnvReferences_ResolvesEachReferenceOnce(t *testing.T) {
	t.Parallel()

	calls := 0
	e := authoring.EnvReferences{Secrets: countingSecrets{calls: &calls}}
	uses := []config.SecretUse{
		{Path: "a", Ref: "env:ACME_A"}, {Path: "b", Ref: "env:ACME_A"}, {Path: "c"}, {Path: "d", Ref: "env:1BAD"},
	}

	findings, err := e.Check(t.Context(), "", uses)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 1 || len(findings) != 1 || findings[0].Use.Path != "d" || findings[0].Code != authoring.CodeSecretEnvUnset {
		t.Errorf("calls %d, findings %+v", calls, findings)
	}
}

type countingSecrets struct{ calls *int }

func (c countingSecrets) Resolve(context.Context, string) (string, error) {
	*c.calls++

	return "value", nil
}

func TestEnvReferences_FailsOnASecretsError(t *testing.T) {
	t.Parallel()

	e := authoring.EnvReferences{Secrets: failingSecrets{}}
	if _, err := e.Check(t.Context(), "", []config.SecretUse{{Ref: "env:ACME_A"}}); err == nil {
		t.Error("a failing Secrets port was taken as an unset variable")
	}
}

type failingSecrets struct{}

func (failingSecrets) Resolve(context.Context, string) (string, error) {
	return "", errors.New("secrets backend unreachable")
}

// connectorCheck stands in for phase 3's connector checker.
type connectorCheck struct{}

func (connectorCheck) ID() string { return "connections" }

func (connectorCheck) Check(_ context.Context, in authoring.CheckInput) []domain.DefinitionProblem {
	if in.Factory == nil {
		return nil
	}

	return []domain.DefinitionProblem{{
		Code: "connection-blocked", Severity: domain.SeverityWarning, Check: "connections",
		Section: domain.SectionConnections, Message: "the connection has no credential",
	}}
}

func TestValidate_ChecksOfLaterPhasesAreOmittedUntilRegistered(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(acmeYAML))
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"yaml", "schema", "line", "references", "images", "secrets", "availability", "name", "credentials"}
	if !slices.Equal(checkIDs(v), want) || !v.OK {
		t.Errorf("checks = %v (ok %v, problems %v), want %v", checkIDs(v), v.OK, codes(v.Problems), want)
	}

	for _, c := range v.Checks {
		if c.Status != authoring.CheckPassed || c.Summary == "" {
			t.Errorf("check %+v", c)
		}
	}

	if line, _ := checkResult(v, "line"); line.Summary != "4 stations, ends with done, every agent station has an agent" {
		t.Errorf("line summary = %q", line.Summary)
	}

	if v.Counts != (authoring.Counts{Stations: 4, Agents: 1}) {
		t.Errorf("counts = %+v", v.Counts)
	}
}

func TestValidate_RegisteredChecksRun(t *testing.T) {
	t.Parallel()

	h := newHarness(t, withChecks(connectorCheck{}))

	v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(acmeYAML))
	if err != nil {
		t.Fatal(err)
	}

	res, ran := checkResult(v, "connections")
	want := authoring.CheckResult{ID: "connections", Section: domain.SectionConnections, Status: authoring.CheckWarning, Summary: "1 warning"}

	if !ran || res != want || !v.OK {
		t.Errorf("registered check = %+v (ran %v, ok %v), want %+v", res, ran, v.OK, want)
	}
}

func TestValidate_ChecksAfterASchemaFailureDoNotRun(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(strings.Replace(acmeYAML, "kind: Factory", "kind: Factory\nbogus: true", 1)))
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(checkIDs(v), []string{"yaml", "schema"}) || v.OK {
		t.Errorf("checks = %v, ok %v", checkIDs(v), v.OK)
	}

	if res, _ := checkResult(v, "schema"); res.Status != authoring.CheckFailed {
		t.Errorf("schema check = %+v", res)
	}
}

func TestValidate_NameAndIdentity(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.imported(t, acmeFiles(acmeYAML))
	h.imported(t, acmeFiles(credentialYAML))

	renamed := strings.Replace(acmeYAML, "name: acme-review", "name: acme-source", 1)
	renamed = strings.Replace(renamed, "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10", "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a99", 1)

	v, err := h.svc.ValidateDraft(h.ctx, f, authoring.ValidateRequest{Overlay: map[string][]byte{yamlFile: []byte(renamed)}})
	if err != nil {
		t.Fatal(err)
	}

	taken, ok := problem(v.Problems, authoring.CodeNameTakenProblem)
	if !ok || taken.Pointer != "/basics/name" || taken.Line != 6 {
		t.Errorf("name problem = %+v (problems %v)", taken, codes(v.Problems))
	}

	id, ok := problem(v.Problems, authoring.CodeFactoryIDChanged)
	if !ok || len(id.Fixes) != 1 || string(id.Fixes[0].Patch[0].Value) != `"`+string(f)+`"` {
		t.Errorf("id problem = %+v", id)
	}

	// The overlay is not saved.
	if !strings.Contains(h.draftFile(t, f, yamlFile), "name: acme-review") {
		t.Error("validating an overlay changed the draft")
	}
}

func TestValidateFiles_AFactorysOwnFilesDoNotTakeItsName(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.imported(t, acmeFiles(acmeYAML))

	// goal factory validate and goal factory list --validate check a factory's own files (its
	// id) outside a draft: its name is its own.
	own, err := h.svc.ValidateFiles(h.ctx, acmeFiles(acmeYAML))
	if err != nil {
		t.Fatal(err)
	}

	if p, taken := problem(own.Problems, authoring.CodeNameTakenProblem); taken {
		t.Errorf("a factory's own files take its name: %+v", p)
	}

	// Another id with that name is another factory.
	other, err := h.svc.ValidateFiles(h.ctx, acmeFiles(strings.Replace(acmeYAML,
		"0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a10", "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a99", 1)))
	if err != nil {
		t.Fatal(err)
	}

	if _, taken := problem(other.Problems, authoring.CodeNameTakenProblem); !taken {
		t.Errorf("another id with the name is not name-taken: %v", codes(other.Problems))
	}
}
