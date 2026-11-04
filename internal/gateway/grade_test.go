package gateway

import (
	"cmp"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// gradeTry is the try the grade tests grade a result for, and gradeModel the grades' model.
const (
	gradeTry   domain.PastTryID = "try-1"
	gradeModel string           = "claude-haiku-5-5"
)

// fakeGrades stands in for what chooses a grade's material (internal/jonah): the material set for
// a try and a result, and ports.ErrNotFound for any other.
type fakeGrades struct {
	mu       sync.Mutex
	material map[domain.PastTryID]map[domain.GradeOf]domain.GradeMaterial
}

func (f *fakeGrades) set(try domain.PastTryID, of domain.GradeOf, m domain.GradeMaterial) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.material == nil {
		f.material = map[domain.PastTryID]map[domain.GradeOf]domain.GradeMaterial{}
	}

	if f.material[try] == nil {
		f.material[try] = map[domain.GradeOf]domain.GradeMaterial{}
	}

	f.material[try][of] = m
}

func (f *fakeGrades) GradeMaterial(_ context.Context, try domain.PastTryID, of domain.GradeOf) (domain.GradeMaterial, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	m, ok := f.material[try][of]
	if !ok {
		return domain.GradeMaterial{}, ports.ErrNotFound
	}

	return m, nil
}

// gradeClaims are a grade token's claims: gradeTry's result of, at the build station, with the
// model and no item or connection.
func (h *harness) gradeClaims(of domain.GradeOf) domain.TaskClaims {
	c := h.claims()
	c.Kind, c.TryOf, c.GradeOf, c.ItemID, c.Connections, c.Agent, c.RunID = domain.TaskKindGrade, gradeTry, of, "", nil, "grader", "run-grade"
	c.Model = gradeModel

	return c
}

// A grade's bundle (ADR 0039 decision 10) is spec.grade, the reason the item went back and the
// result the token names, with the token's model and Jonah's budget left as max_usd: no item,
// repository, definition, file, connection, knowledge, rule or tool. Its notes are none.
func TestBundle_Grade(t *testing.T) {
	t.Parallel()

	h := newJonahHarness(t, harnessDeps{})
	it := h.submit(t, factoryID, "Add a health endpoint", "")
	now := h.clock.Now()
	h.finishedRun(t, it, now.Add(-2*time.Hour), 200) // Jonah's budget is $10, a run's cap $2
	h.finishedJonahRun(t, now.Add(-time.Hour), 8.5)

	material := domain.GradeMaterial{Reason: "The health endpoint returned 500.", Output: `{"pr":1}`, Diff: "diff --git a/main.go b/main.go\n"}
	h.grades.set(gradeTry, domain.GradeOriginal, material)

	c := h.gradeClaims(domain.GradeOriginal)
	url := h.runServer(t)

	status, _, body := request(t, http.MethodGet, url, h.token(t, c))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var raw struct {
		Spec struct {
			Grade json.RawMessage `json:"grade"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}

	want := `{"reason":"The health endpoint returned 500.","result":{"kind":"original","output":"{\"pr\":1}","diff":"diff --git a/main.go b/main.go\n"}}`
	if string(raw.Spec.Grade) != want {
		t.Errorf("spec.grade = %s, want %s", raw.Spec.Grade, want)
	}

	var b runBundle
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}

	expectGradeBox(t, b, c)
	expectNoKeys(t, body)

	if math.Abs(b.Spec.MaxUSD-1.5) > 1e-9 {
		t.Errorf("max_usd = %v, want 1.5: the $10 budget less the $8.50 spent, under the $2 cap", b.Spec.MaxUSD)
	}

	status, _, body = request(t, http.MethodGet, url+"?notes_after=0", h.token(t, c))
	if status != http.StatusOK || !strings.Contains(string(body), `"notes":[]`) {
		t.Errorf("notes: status = %d (%s), want 200 and none: a grade has no item", status, body)
	}
}

// expectGradeBox fails the test unless b is a grade's bundle of c: c's run, station, agent and
// model, no item, and nothing to work with.
func expectGradeBox(t *testing.T, b runBundle, c domain.TaskClaims) {
	t.Helper()

	s := b.Spec
	if s.Grade == nil || s.ItemID != "" || s.RunID != c.RunID || s.Agent != c.Agent || s.Station != c.Station || s.Model != c.Model || s.FactoryID != factoryID {
		t.Errorf("spec = %+v; want a grade of run %s by %s on %s with no item", s, c.RunID, c.Agent, c.Model)
	}

	if b.Repository != nil || b.Definition != nil || len(b.Files) != 0 || len(b.Git) != 0 || len(b.Tools) != 0 {
		t.Errorf("bundle = %+v; want no repository, definition, file, git login or tool", b)
	}

	expectNothingToWorkWith(t, s)
}

// expectNothingToWorkWith fails the test unless s has no brief, connection, knowledge, rule,
// skill, tool, granted call, environment or other mode.
func expectNothingToWorkWith(t *testing.T, s bundleSpec) {
	t.Helper()

	if len(s.Connections) != 0 || s.Knowledge != nil || len(s.Rules) != 0 || len(s.Skills) != 0 || len(s.ToolsAllow) != 0 ||
		len(s.GrantedCalls) != 0 || s.Environment != nil || s.Jonah != nil || s.DraftRequest != nil || s.Brief != "" {
		t.Errorf("spec = %+v; want no connection, knowledge, rule, skill, tool, call, environment or other mode", s)
	}
}

// A grade's bundle is 503 without Deps.Grades or Deps.Jonah, and 403 for a version that is not the
// token's factory's or a result the grades do not have.
func TestBundle_GradeRefused(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		deps   harnessDeps
		change func(*domain.TaskClaims)
		status int
	}{
		"without Deps.Grades":     {deps: harnessDeps{noGrades: true}, status: http.StatusServiceUnavailable},
		"without Deps.Jonah":      {deps: harnessDeps{noJonah: true}, status: http.StatusServiceUnavailable},
		"another factory's":       {change: func(c *domain.TaskClaims) { c.VersionID = otherVersion }, status: http.StatusForbidden},
		"a result it has not got": {change: func(c *domain.TaskClaims) { c.GradeOf = domain.GradeFinal }, status: http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newJonahHarness(t, tt.deps)
			if h.grades != nil {
				h.grades.set(gradeTry, domain.GradeOriginal, domain.GradeMaterial{Reason: "r"})
			}

			c := h.gradeClaims(domain.GradeOriginal)
			if tt.change != nil {
				tt.change(&c)
			}

			if status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c)); status != tt.status {
				t.Errorf("status = %d, want %d (%s)", status, tt.status, body)
			}
		})
	}
}

// A grade gets no MCP tool: its session lists none and refuses every call.
func TestBundle_GradeTokenGetsNoTools(t *testing.T) {
	t.Parallel()

	for _, protocol := range []string{legacyProtocol, ""} {
		t.Run("protocol "+cmp.Or(protocol, "latest"), func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, Config{})

			cs, err := h.connectAs(t, protocol, h.gradeClaims(domain.GradeTry))
			if err != nil {
				t.Fatal(err)
			}

			res, err := cs.ListTools(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}

			if got := toolNames(res); len(got) != 0 {
				t.Errorf("tools = %v, want none", got)
			}

			if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: questionTool, Arguments: map[string]any{"question": "Why?"}}); err == nil ||
				!strings.Contains(err.Error(), "is not available") {
				t.Errorf("CallTool(%s) = %v, want it refused as an unknown tool", questionTool, err)
			}
		})
	}
}
