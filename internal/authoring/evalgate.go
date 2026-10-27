package authoring

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The publish eval gate (ADR 0025 decision 4): when a draft has eval cases, it publishes only
// once the newest eval run of every case on the draft's exact content (domain.ManifestSHA256)
// passed every try. Without such a run, publishing starts one and refuses until it passes. A
// draft without cases publishes as before, and so does every draft while Deps.EvalGate is nil.

// EvalRuns are the reads of ports.Evals the gate makes.
type EvalRuns interface {
	LatestRunForContent(ctx context.Context, factory domain.FactoryID, sha256 domain.BlobHash) (domain.EvalRun, error)
	GetRun(ctx context.Context, id domain.EvalRunID) (domain.EvalRun, error)
}

// EvalStarter queues an eval run of cases (every one, or only n.Case), priced; *jonah.Evals
// satisfies it.
type EvalStarter interface {
	Start(ctx context.Context, n domain.NewEvalRun, cases []domain.EvalCaseSpec, by domain.Actor) (domain.EvalRun, domain.EvalEstimate, error)
}

// EvalGate is what the publish eval gate works through; both are required.
type EvalGate struct {
	Runs    EvalRuns
	Starter EvalStarter
}

// The gate's refusals, each wrapped in an *EvalGateError [409].
var (
	// ErrEvalsPending: the draft's cases have no passing run yet: one is queued or running.
	ErrEvalsPending = errors.New("eval cases pending")
	// ErrEvalsFailing: the newest run of every case on the draft's content has a case with a try
	// that did not pass.
	ErrEvalsFailing = errors.New("eval cases failing")
)

// Problem codes of the gate's refusals.
const (
	CodeEvalsPending = "evals-pending"
	CodeEvalsFailing = "evals-failing"
)

// EvalGateError is a publish the gate refused: Run is the eval run it waits on or that failed,
// Started whether this publish queued it, and Failing the cases of a failed run with a try that
// did not pass. The draft stays open, so the same content publishes once a run of it passes.
type EvalGateError struct {
	Run     domain.EvalRunID
	Started bool
	Failing []string
	reason  error
}

func (e *EvalGateError) Error() string {
	if errors.Is(e.reason, ErrEvalsFailing) {
		return fmt.Sprintf("eval run %s of this draft has failing cases: %s. The draft stays open: fix the cases or the definition and publish again",
			e.Run, strings.Join(e.Failing, ", "))
	}

	verb := "is running"
	if e.Started {
		verb = "started"
	}

	return fmt.Sprintf("eval run %s of this draft's cases %s. The draft stays open: publish again once every case passes", e.Run, verb)
}

// Unwrap makes the error match ErrEvalsPending or ErrEvalsFailing.
func (e *EvalGateError) Unwrap() error { return e.reason }

// EvalContent is the eval cases of one version or draft: the content they belong to, its hash
// (domain.ManifestSHA256, the one eval runs and the gate compare by) and its cases by name.
type EvalContent struct {
	Version domain.DefinitionVersion
	SHA256  domain.BlobHash
	Cases   []domain.EvalCaseSpec
}

// EvalContent reads the eval cases of the version sel names (current or the draft, mostly):
// ErrVersionNotFound or ErrNoDraft when there is none.
func (s *Service) EvalContent(ctx context.Context, f domain.FactoryID, sel VersionSelector) (EvalContent, error) {
	v, err := s.selectVersion(ctx, f, sel)
	if err != nil {
		return EvalContent{}, err
	}

	if v == nil {
		return EvalContent{}, fmt.Errorf("%s of %s: %w", sel, f, ErrVersionNotFound)
	}

	return s.evalContent(ctx, *v)
}

func (s *Service) evalContent(ctx context.Context, v domain.DefinitionVersion) (EvalContent, error) {
	t := s.tree(ctx, v.Manifest)

	cases, err := config.EvalCases(t)
	if t.err != nil {
		return EvalContent{}, t.err
	}

	if err != nil {
		return EvalContent{}, fmt.Errorf("eval cases of %s: %w", v.ID, err)
	}

	return EvalContent{Version: v, SHA256: domain.ManifestSHA256(v.Manifest), Cases: cases}, nil
}

// evalGate lets draft d of factory f publish, or refuses with an *EvalGateError. It starts a run
// of every case, triggered by the publish, when the content has none, or when its newest run is
// of one case only.
func (s *Service) evalGate(ctx context.Context, f domain.FactoryID, d domain.DefinitionVersion, by domain.Actor) error {
	if s.evals == nil {
		return nil
	}

	c, err := s.evalContent(ctx, d)
	if err != nil {
		return err
	}

	if len(c.Cases) == 0 {
		return nil
	}

	run, err := s.evals.Runs.LatestRunForContent(ctx, f, c.SHA256)

	switch {
	case errors.Is(err, ports.ErrNotFound) || err == nil && run.Case != "":
		r, _, err := s.evals.Starter.Start(ctx, domain.NewEvalRun{FactoryID: f, ContentSHA256: c.SHA256, Trigger: domain.EvalOnPublish}, c.Cases, by)
		if err != nil {
			return fmt.Errorf("publish %s: start its eval run: %w", f, err)
		}

		return &EvalGateError{Run: r.ID, Started: true, reason: ErrEvalsPending}
	case err != nil:
		return fmt.Errorf("publish %s: read its eval runs: %w", f, err)
	case !run.State.Final():
		return &EvalGateError{Run: run.ID, reason: ErrEvalsPending}
	}

	run, err = s.evals.Runs.GetRun(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("publish %s: read eval run %s: %w", f, run.ID, err)
	}

	if failing := FailingCases(run); len(failing) > 0 {
		return &EvalGateError{Run: run.ID, Failing: failing, reason: ErrEvalsFailing}
	}

	return nil
}

// FailingCases are the cases of run, by name, with a try that is not pass: one that failed,
// erred, or never ran because the run was cancelled or failed.
func FailingCases(run domain.EvalRun) []string {
	var out []string

	for _, r := range run.Results {
		if r.State != domain.EvalPass && !slices.Contains(out, r.Case) {
			out = append(out, r.Case)
		}
	}

	slices.Sort(out)

	return out
}
