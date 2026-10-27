package authoring_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// caseYAML is an eval case of the acme engineer, checked by its contract.
const caseYAML = `apiVersion: goal/v1
kind: EvalCase
metadata:
  description: A small request gets built
spec:
  agent: engineer
  input:
    title: Add a version flag
    body: The CLI prints its version.
  tries: 2
  check:
    contract: true
`

const caseFile = "evals/cases/version-flag.yaml"

// starter queues eval runs in a porttest.Evals, as *jonah.Evals does, and counts them.
type starter struct {
	evals  *porttest.Evals
	starts int
}

func (s *starter) Start(ctx context.Context, n domain.NewEvalRun, cases []domain.EvalCaseSpec, by domain.Actor) (domain.EvalRun, domain.EvalEstimate, error) {
	s.starts++

	for _, c := range cases {
		if n.Case == "" || n.Case == c.Name {
			n.Cases = append(n.Cases, domain.EvalCase{Name: c.Name, SHA256: c.SHA256, Tries: c.Tries})
		}
	}

	r, err := s.evals.CreateRun(ctx, n, by)

	return r, domain.EvalEstimate{}, err
}

// gated is a harness whose publishes go through an eval gate over evals and start.
type gated struct {
	*harness
	evals *porttest.Evals
	start *starter
}

// withCases is a harness with an eval gate and the acme factory imported with caseYAML.
func withCases(t *testing.T) (*gated, domain.FactoryID) {
	t.Helper()

	var g gated

	h := newHarness(t, func(d *authoring.Deps) {
		g.evals = porttest.NewEvals(d.Clock.Now)
		g.start = &starter{evals: g.evals}
		d.EvalGate = &authoring.EvalGate{Runs: g.evals, Starter: g.start}
	})
	g.harness = h

	files := acmeFiles(acmeYAML)
	files[caseFile] = []byte(caseYAML)

	return &g, h.imported(t, files)
}

// finish works every try of run r as state, then sets the run done.
func (g *gated) finish(t *testing.T, r domain.EvalRunID, state domain.EvalResultState) {
	t.Helper()

	for {
		c, err := g.evals.ClaimResult(g.ctx, g.clock.Now(), time.Minute)
		if err != nil {
			break
		}

		if err := g.evals.RecordResult(g.ctx, c, domain.EvalOutcome{State: state, CheckedBy: domain.EvalCheckedByContract}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := g.evals.FinishRun(g.ctx, r, domain.EvalRunDone, g.clock.Now()); err != nil {
		t.Fatal(err)
	}
}

// refused wants err to be the gate's refusal with code and returns it.
func refused(t *testing.T, err error, code string) *authoring.EvalGateError {
	t.Helper()

	e, ok := errors.AsType[*authoring.EvalGateError](err)
	if !ok || authoring.ProblemCode(err) != code {
		t.Fatalf("publish = %v, want the gate's %s", err, code)
	}

	return e
}

func TestPublish_EvalGateNoCasesPublishes(t *testing.T) {
	t.Parallel()

	var g gated

	h := newHarness(t, func(d *authoring.Deps) {
		g.evals = porttest.NewEvals(d.Clock.Now)
		g.start = &starter{evals: g.evals}
		d.EvalGate = &authoring.EvalGate{Runs: g.evals, Starter: g.start}
	})

	h.publish(t, h.imported(t, acmeFiles(acmeYAML)))

	if g.start.starts != 0 {
		t.Errorf("runs started = %d, want none for a definition without cases", g.start.starts)
	}
}

func TestPublish_EvalGateStartsARunWhenNoneExists(t *testing.T) {
	t.Parallel()

	g, f := withCases(t)

	_, err := g.svc.Publish(g.ctx, f, g.publishRequest(t, f))
	e := refused(t, err, authoring.CodeEvalsPending)

	run, gerr := g.evals.GetRun(g.ctx, e.Run)
	if gerr != nil {
		t.Fatal(gerr)
	}

	if !e.Started || run.Trigger != domain.EvalOnPublish || run.Case != "" || len(run.Results) != 2 || run.VersionID != "" {
		t.Errorf("run = %+v (started %v), want a publish run of every case's 2 tries on the draft", run, e.Started)
	}

	// The draft stays open for the next publish.
	if _, err := g.svc.Draft(g.ctx, f); err != nil {
		t.Errorf("draft after the refusal: %v", err)
	}
}

func TestPublish_EvalGateStartsAFullRunAfterAOneCaseRun(t *testing.T) {
	t.Parallel()

	g, f := withCases(t)

	d, err := g.svc.EvalContent(g.ctx, f, authoring.VersionSelector{Kind: authoring.SelectDraft})
	if err != nil {
		t.Fatal(err)
	}

	one, _, err := g.start.Start(g.ctx, domain.NewEvalRun{
		FactoryID: f, ContentSHA256: d.SHA256, Trigger: domain.EvalManual, Case: "version-flag",
	}, d.Cases, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	g.finish(t, one.ID, domain.EvalPass)

	_, err = g.svc.Publish(g.ctx, f, g.publishRequest(t, f))
	if e := refused(t, err, authoring.CodeEvalsPending); !e.Started || e.Run == one.ID {
		t.Errorf("refusal = %+v, want a new run of every case, not the one-case run", e)
	}
}

func TestPublish_EvalGateRunningRefusesWithoutStarting(t *testing.T) {
	t.Parallel()

	g, f := withCases(t)

	_, err := g.svc.Publish(g.ctx, f, g.publishRequest(t, f))
	first := refused(t, err, authoring.CodeEvalsPending)

	_, err = g.svc.Publish(g.ctx, f, g.publishRequest(t, f))
	if e := refused(t, err, authoring.CodeEvalsPending); e.Run != first.Run || e.Started {
		t.Errorf("second refusal = %+v, want run %s again, not started", e, first.Run)
	}

	if g.start.starts != 1 {
		t.Errorf("runs started = %d, want 1", g.start.starts)
	}
}

func TestPublish_EvalGateFailingRefuses(t *testing.T) {
	t.Parallel()

	g, f := withCases(t)

	_, err := g.svc.Publish(g.ctx, f, g.publishRequest(t, f))
	pending := refused(t, err, authoring.CodeEvalsPending)
	g.finish(t, pending.Run, domain.EvalFail)

	_, err = g.svc.Publish(g.ctx, f, g.publishRequest(t, f))
	if e := refused(t, err, authoring.CodeEvalsFailing); e.Run != pending.Run || !slices.Equal(e.Failing, []string{"version-flag"}) {
		t.Errorf("refusal = %+v, want run %s failing version-flag", e, pending.Run)
	}
}

func TestPublish_EvalGatePassingPublishes(t *testing.T) {
	t.Parallel()

	g, f := withCases(t)

	_, err := g.svc.Publish(g.ctx, f, g.publishRequest(t, f))
	g.finish(t, refused(t, err, authoring.CodeEvalsPending).Run, domain.EvalPass)

	if v := g.publish(t, f); v.Number != 1 {
		t.Errorf("published v%d, want v1", v.Number)
	}
}

func TestPublish_EvalGateUnsetPublishesCases(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	files := acmeFiles(acmeYAML)
	files[caseFile] = []byte(caseYAML)

	if v := h.publish(t, h.imported(t, files)); v.Number != 1 {
		t.Errorf("published v%d, want v1", v.Number)
	}
}
