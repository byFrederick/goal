package authoring_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
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

// legacyYAML is acmeYAML as published before connections: its repository signs in with an env:
// reference, a form connections replaced.
var legacyYAML = strings.Replace(acmeYAML, "connection: code }", "token: env:ACME_GIT_TOKEN }", 1)

// Since the cutover a form connections replaced is an error: such a draft does not publish, and
// the problem carries the fix that takes it out.
func TestValidate_RemovedFormsAreErrors(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(legacyYAML))
	if err != nil {
		t.Fatal(err)
	}

	p, ok := problem(v.Problems, "env-reference-removed")
	if !ok || v.OK || p.Severity != domain.SeverityError || p.Pointer != "/basics/repository/token" ||
		len(p.Fixes) != 1 || p.Fixes[0].Label != "Remove the token" {
		t.Fatalf("validation = ok %v, problem %+v (found %v); want env-reference-removed, an error with its fix", v.OK, p, ok)
	}

	if strings.Contains(p.Message, "ACME_GIT_TOKEN") {
		t.Errorf("message %q names the reference; it names the fix", p.Message)
	}
}

func TestValidate_ListsEveryCheckThatRan(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	v, err := h.svc.ValidateFiles(h.ctx, acmeFiles(acmeYAML))
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"yaml", "schema", "line", "references", "tools", "contracts", "removed", "availability", "name", "connections",
	}
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

// A rule naming an agent the factory does not have is an error of the Agents section, at the rule.
func TestValidate_RuleUnknownAgent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.imported(t, ruleFiles("description: Secrets stay out of logs.\nappliesTo: { agents: [engineer, ghost] }\n"))

	v, err := h.svc.ValidateDraft(h.ctx, f, authoring.ValidateRequest{})
	if err != nil {
		t.Fatal(err)
	}

	p, ok := problem(v.Problems, "unknown-agent")
	if !ok || v.OK || p.Severity != domain.SeverityError || p.Section != domain.SectionAgents || !strings.HasPrefix(p.Pointer, "/agents/rules/0") {
		t.Fatalf("validation ok %v, unknown-agent problem %+v (problems %v)", v.OK, p, codes(v.Problems))
	}

	_, err = h.svc.Publish(h.ctx, f, h.publishRequest(t, f))
	if authoring.ProblemCode(err) != authoring.CodeDefinitionInvalid {
		t.Errorf("publish = %v, want %s", err, authoring.CodeDefinitionInvalid)
	}
}
