package engine_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// draftRequest is a draft request of the test factory about agReviewer.
func draftRequest() domain.DraftRequest {
	return domain.DraftRequest{
		ID: "d0000000-0000-4000-8000-000000000001", Org: testOrg, Factory: factoryID, Text: "Give review a 4th reviewer",
		Agent: agReviewer, Status: domain.DraftRequestRunning,
	}
}

// definitionOutput is a definition-mode output that changed factory.yaml and deleted an agent.
const definitionOutput = `{"summary":"Gave review a 4th reviewer","changed":[{"path":"factory.yaml","sha256":"` +
	`ab12","content":"a2luZDogRmFjdG9yeQo="}],"deleted":["agents/old.md"],"rejected":[]}`

// definitionLine is the test line with a repository and an agent connection, neither of which a
// draft request's box may get.
func definitionLine() domain.Line {
	l := lineOf(agentStation(stWork, agReviewer, 1, 1))
	l.Runtime.Repository = domain.Repository{URL: "https://" + gitHost + "/acme/api.git", Connection: connGit}
	l.Connections = []string{connTracker}

	for i := range l.Agents {
		l.Agents[i].Connections = []string{connTracker}
	}

	return l
}

func TestRunDefinition_ModelConnectionOnly(t *testing.T) {
	t.Parallel()

	h := newHarness(t, definitionLine(), func(context.Context, porttest.ExecCall, io.Writer) domain.RunResult {
		return domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(definitionOutput), CostUSD: 0.42, Turns: 7}
	})

	req := draftRequest()

	got, err := h.e.RunDefinition(inOrg(t), ports.DefinitionTask{Request: req, Revision: 5})
	if err != nil {
		t.Fatal(err)
	}

	want := ports.DefinitionResult{
		Status: domain.RunSucceeded, Summary: "Gave review a 4th reviewer",
		Changed: []ports.DefinitionFile{{Path: "factory.yaml", SHA256: "ab12", Content: []byte("kind: Factory\n")}},
		Deleted: []string{"agents/old.md"}, Rejected: []string{}, CostUSD: 0.42, Turns: 7,
	}
	if !equalResults(got, want) {
		t.Errorf("result = %+v, want %+v", got, want)
	}

	prepares, runs := h.worker.Prepares(), h.worker.Runs()
	if len(prepares) != 1 || len(runs) != 1 {
		t.Fatalf("%d prepares and %d runs, want one of each", len(prepares), len(runs))
	}

	box := prepares[0].Sandbox
	if box.ItemID != "" || box.Station != "" || box.RunID != domain.RunID(req.ID) || box.FactoryID != factoryID || box.Image != agentImage {
		t.Errorf("sandbox = %+v, want the request's run with no item and no station", box)
	}

	// prepare gets the task token and no git password; run gets the model connection's env alone.
	if keys := slices.Sorted(maps.Keys(prepares[0].Env)); !slices.Equal(keys, []string{"GOAL_GATEWAY_TOKEN", "GOAL_GATEWAY_URL"}) {
		t.Errorf("prepare env = %v, want only the gateway's", keys)
	}

	if keys := slices.Sorted(maps.Keys(runs[0].Env)); !slices.Equal(keys, []string{"GOAL_GATEWAY_URL", envModelKey}) {
		t.Errorf("run env = %v, want only the model connection's", keys)
	}

	if runs[0].Env[envModelKey] != modelKey {
		t.Errorf("run env %s is not the model connection's key", envModelKey)
	}

	claims, err := domain.VerifyTaskToken([]byte("k3y"), prepares[0].Env["GOAL_GATEWAY_TOKEN"], h.clock.Now())
	if err != nil {
		t.Fatal(err)
	}

	if claims.Kind != domain.TaskKindDraftRequest || claims.DraftRequest != req.ID || claims.DraftRevision != 5 ||
		claims.VersionID != v1 || claims.ItemID != "" || claims.Station != "" || len(claims.Connections) != 0 {
		t.Errorf("token claims = %+v, want draft request %s at revision 5 of version %s", claims, req.ID, v1)
	}

	if h.sandboxes.Has(domain.RunID(req.ID)) {
		t.Error("the request's sandbox was not deleted")
	}
}

func TestRunDefinition_RefusesExhaustedBudget(t *testing.T) {
	t.Parallel()

	l := definitionLine()
	l.Runtime.WeeklyUSD, l.Runtime.OnExhausted = 10, domain.BudgetDrain

	h := newHarness(t, l, porttest.Succeed(definitionOutput))
	h.spent(t, t0, 12)

	_, err := h.e.RunDefinition(inOrg(t), ports.DefinitionTask{Request: draftRequest(), Revision: 1})
	if !errors.Is(err, ports.ErrBudgetExhausted) {
		t.Fatalf("err = %v, want ErrBudgetExhausted", err)
	}

	if n := len(h.worker.Prepares()); n != 0 {
		t.Errorf("%d prepares, want no box for an exhausted budget", n)
	}
}

func TestRunDefinition_WorkerErrorDeletesTheBox(t *testing.T) {
	t.Parallel()

	req := draftRequest()

	t.Run("prepare fails", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, definitionLine(), porttest.Succeed(definitionOutput))
		h.worker.FailPrepare(1, "GET /run: 503 gateway unavailable "+modelKey)

		_, err := h.e.RunDefinition(inOrg(t), ports.DefinitionTask{Request: req, Revision: 1})
		if err == nil || strings.Contains(err.Error(), modelKey) || errors.Is(err, ports.ErrConflict) {
			t.Errorf("err = %v, want a scrubbed prepare error that is no conflict", err)
		}

		if len(h.worker.Runs()) != 0 || h.sandboxes.Has(domain.RunID(req.ID)) {
			t.Error("the agent ran or the sandbox was left after prepare failed")
		}
	})

	t.Run("the draft moved", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, definitionLine(), porttest.Succeed(definitionOutput))
		h.worker.FailPrepare(1, "GET /run: 409 the draft moved past revision 1")

		_, err := h.e.RunDefinition(inOrg(t), ports.DefinitionTask{Request: req, Revision: 1})
		if !errors.Is(err, ports.ErrConflict) {
			t.Errorf("err = %v, want ErrConflict", err)
		}

		if h.sandboxes.Has(domain.RunID(req.ID)) {
			t.Error("the sandbox was left after the draft moved")
		}
	})

	t.Run("the worker finishes failed", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, definitionLine(), func(context.Context, porttest.ExecCall, io.Writer) domain.RunResult {
			return domain.RunResult{Status: domain.RunFailed, Error: "rejected evals/review.yaml with key " + modelKey, CostUSD: 0.1, Turns: 2}
		})

		got, err := h.e.RunDefinition(inOrg(t), ports.DefinitionTask{Request: req, Revision: 1})
		if err != nil {
			t.Fatal(err)
		}

		if got.Status != domain.RunFailed || !strings.Contains(got.Error, "evals/review.yaml") || strings.Contains(got.Error, modelKey) ||
			got.CostUSD != 0.1 || got.Turns != 2 || len(got.Changed) != 0 {
			t.Errorf("result = %+v, want a failed, scrubbed result with its cost", got)
		}

		if h.sandboxes.Has(domain.RunID(req.ID)) {
			t.Error("the sandbox was left after the worker failed")
		}
	})

	t.Run("the worker crashes", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, definitionLine(), func(context.Context, porttest.ExecCall, io.Writer) domain.RunResult {
			return domain.RunResult{}
		})

		if _, err := h.e.RunDefinition(inOrg(t), ports.DefinitionTask{Request: req, Revision: 1}); err == nil {
			t.Error("a worker without a result is no error")
		}

		if h.sandboxes.Has(domain.RunID(req.ID)) {
			t.Error("the sandbox was left after the worker crashed")
		}
	})

	t.Run("a changed file holds a secret", func(t *testing.T) {
		t.Parallel()

		out := `{"summary":"done","changed":[{"path":"rules/key.md","sha256":"cd34","content":"` +
			b64(modelKey) + `"}],"deleted":[],"rejected":[]}`
		h := newHarness(t, definitionLine(), porttest.Succeed(out))

		got, err := h.e.RunDefinition(inOrg(t), ports.DefinitionTask{Request: req, Revision: 1})
		if err != nil {
			t.Fatal(err)
		}

		if got.Status != domain.RunFailed || len(got.Changed) != 0 || strings.Contains(got.Error, modelKey) {
			t.Errorf("result = %+v, want a failed result without the file", got)
		}
	})
}

func TestRunDefinition_OtherOrganizationsFactoryIsNotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t, definitionLine(), porttest.Succeed(definitionOutput))
	other := h.newOrg(t, "other")

	_, err := h.e.RunDefinition(domain.WithOrg(t.Context(), other), ports.DefinitionTask{Request: draftRequest(), Revision: 1})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}

	if n := len(h.worker.Prepares()); n != 0 {
		t.Errorf("%d prepares, want none", n)
	}
}

func equalResults(a, b ports.DefinitionResult) bool {
	return a.Status == b.Status && a.Summary == b.Summary && a.CostUSD == b.CostUSD && a.Turns == b.Turns && a.Error == b.Error &&
		slices.Equal(a.Deleted, b.Deleted) && slices.Equal(a.Rejected, b.Rejected) &&
		slices.EqualFunc(a.Changed, b.Changed, func(x, y ports.DefinitionFile) bool {
			return x.Path == y.Path && x.SHA256 == y.SHA256 && string(x.Content) == string(y.Content)
		})
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
