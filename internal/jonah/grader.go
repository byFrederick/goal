package jonah

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ItemRuns reads recorded runs, one or an item's; ports.Store satisfies it.
type ItemRuns interface {
	Run(ctx context.Context, id domain.RunID) (domain.Run, error)
	Runs(ctx context.Context, item domain.ItemID, limit int) ([]domain.Run, error)
}

// GradeDeps are the ports Grades reads through.
type GradeDeps struct {
	Tries  ports.PastTries
	Runs   ItemRuns
	Events ItemEvents
}

// Grades is what a grade run reads (ADR 0039 decision 10), from storage, for the organization in
// ctx; it implements gateway.Grades.
type Grades struct{ d GradeDeps }

// NewGrades returns the grade material read through d.
func NewGrades(d GradeDeps) *Grades { return &Grades{d: d} }

// itemRuns bounds the runs of an item finalRun reads.
const itemRuns = 1000

// GradeMaterial is what a grade of try's result of reads: for GradeTry the try's run output and
// stored diff, for GradeOriginal the original run's output, for GradeFinal the output of the item's
// newest succeeded run at the try's station (finalRun). Each comes with the data.reason of the
// item's first rejected or gate_rejected event after the original run started ("" when none). A
// try or a result that does not exist wraps ports.ErrNotFound.
func (g *Grades) GradeMaterial(ctx context.Context, id domain.PastTryID, of domain.GradeOf) (domain.GradeMaterial, error) {
	try, err := g.d.Tries.PastTry(ctx, id)
	if err != nil {
		return domain.GradeMaterial{}, fmt.Errorf("read try %s: %w", id, err)
	}

	orig, err := g.d.Runs.Run(ctx, try.OriginalRunID)
	if err != nil {
		return domain.GradeMaterial{}, fmt.Errorf("read try %s's original run %s: %w", id, try.OriginalRunID, err)
	}

	var m domain.GradeMaterial

	switch of {
	case domain.GradeTry:
		if try.TryRunID == "" {
			return domain.GradeMaterial{}, fmt.Errorf("try %s has no run: %w", id, ports.ErrNotFound)
		}

		run, err := g.d.Runs.Run(ctx, try.TryRunID)
		if err != nil {
			return domain.GradeMaterial{}, fmt.Errorf("read try %s's run %s: %w", id, try.TryRunID, err)
		}

		m.Output, m.Diff = string(run.Result.Output), try.Diff
	case domain.GradeOriginal:
		m.Output = string(orig.Result.Output)
	case domain.GradeFinal:
		final, err := finalRun(ctx, g.d.Runs, try)
		if err != nil {
			return domain.GradeMaterial{}, err
		}

		m.Output = string(final.Result.Output)
	default:
		return domain.GradeMaterial{}, fmt.Errorf("grade of %q: %w", of, ports.ErrNotFound)
	}

	m.Reason, err = sentBackReason(ctx, g.d.Events, try.ItemID, orig)
	if err != nil {
		return domain.GradeMaterial{}, err
	}

	return m, nil
}

// finalRun is try's item's final accepted result: its newest succeeded run at the try's station.
// ErrNotFound when it has none.
func finalRun(ctx context.Context, runs ItemRuns, try domain.PastTry) (domain.Run, error) {
	rs, err := runs.Runs(ctx, try.ItemID, itemRuns)
	if err != nil {
		return domain.Run{}, fmt.Errorf("read the runs of item %s: %w", try.ItemID, err)
	}

	for _, r := range slices.Backward(rs) {
		if r.Station == try.Station && r.Result.Status == domain.RunSucceeded {
			return r, nil
		}
	}

	return domain.Run{}, fmt.Errorf("item %s has no succeeded run at %s: %w", try.ItemID, try.Station, ports.ErrNotFound)
}

// sentBackReason is the data.reason of item's first rejected or gate_rejected event after orig
// started: why the item went back, which the grader judges each result against.
func sentBackReason(ctx context.Context, events ItemEvents, item domain.ItemID, orig domain.Run) (string, error) {
	for after := int64(0); ; {
		page, err := events.ItemEvents(ctx, item, after, tryEventPage)
		if err != nil {
			return "", fmt.Errorf("read the events of item %s: %w", item, err)
		}

		for _, e := range page {
			if (e.Kind == domain.EventRejected || e.Kind == domain.EventGateRejected) && !e.At.Before(orig.StartedAt) {
				r, _ := e.Data["reason"].(string)

				return r, nil
			}
		}

		if len(page) < tryEventPage {
			return "", nil
		}

		after = page[len(page)-1].ID
	}
}

// Why a grade waits: its try is left to its lease, and a later claim grades it from its recorded
// run.
var (
	errGradeWait  = errors.New("jonah's budget is used up")
	errLeaseShort = errors.New("the try's lease has no time left for a grade run")
)

// minGradeTime is the least time a grade run is started with: a lease with less left waits for
// the next claim.
const minGradeTime = time.Minute

// gradeByModel grades res, try's recorded result, by the grader (ADR 0039 decision 10), with the
// grader checked first on the item's original and final results (graderChecks). When the grader
// misread either one, the try is unknown and gets no grade run; otherwise one grade run of current's
// runtime.model grades it, and the try takes its answer and adds its cost. A grade that gives no
// valid answer is unknown. errGradeWait once Jonah's budget is used up before a grade run.
func (s *Tries) gradeByModel(ctx context.Context, current domain.Line, try domain.PastTry, orig domain.Run, res domain.PastTryResult,
) (domain.PastTryResult, error) {
	checks, err := s.graderChecks(ctx, current, try, orig)
	if err != nil {
		return domain.PastTryResult{}, err
	}

	for _, c := range checks {
		if !c.Hit() {
			want := domain.PastTryFixed
			if c.Result == domain.GraderCheckOriginal {
				want = domain.PastTryNotFixed
			}

			res.Verdict, res.Reason = domain.PastTryUnknown, truncate(fmt.Sprintf(
				"the grader misread the item's %s result: it answered %s where %s was right, so its grade of the try can't be trusted: %s",
				c.Result, c.Answer, want, c.Reason), domain.MaxPastTryReason)

			return res, nil
		}
	}

	a, cost, _, err := s.gradeRun(ctx, current, try, orig, domain.GradeTry)
	if err != nil {
		return domain.PastTryResult{}, err
	}

	res.Verdict, res.Reason, res.CostUSD = a.Verdict, truncate(a.Reason, domain.MaxPastTryReason), res.CostUSD+cost

	return res, nil
}

// graderChecks are the proposal's two checks of the grader on try's item, original then final,
// each run and stored the first time a try of the item is graded. A check another try of the item
// stored first (ErrConflict) is read back, never run twice.
func (s *Tries) graderChecks(ctx context.Context, current domain.Line, try domain.PastTry, orig domain.Run) ([]domain.GraderCheck, error) {
	have, err := s.itemChecks(ctx, try)
	if err != nil {
		return nil, err
	}

	var out []domain.GraderCheck

	for _, r := range []domain.GraderCheckResult{domain.GraderCheckOriginal, domain.GraderCheckFinal} {
		if c, ok := have[r]; ok {
			out = append(out, c)

			continue
		}

		n, err := s.graderCheck(ctx, current, try, orig, r)
		if err != nil {
			return nil, err
		}

		c, err := s.d.Tries.RecordGraderCheck(ctx, n)
		if errors.Is(err, ports.ErrConflict) {
			if have, err = s.itemChecks(ctx, try); err != nil {
				return nil, err
			}

			c, ok := have[r]
			if !ok {
				return nil, fmt.Errorf("the grader's %s check of item %s conflicts but is not stored", r, try.ItemID)
			}

			out = append(out, c)

			continue
		}

		if err != nil {
			return nil, fmt.Errorf("record the grader's %s check of item %s: %w", r, try.ItemID, err)
		}

		out = append(out, c)
	}

	return out, nil
}

// itemChecks are the proposal's stored checks of try's item, by result.
func (s *Tries) itemChecks(ctx context.Context, try domain.PastTry) (map[domain.GraderCheckResult]domain.GraderCheck, error) {
	all, err := s.d.Tries.ProposalGraderChecks(ctx, try.ProposalID)
	if err != nil {
		return nil, fmt.Errorf("read the grader's checks of proposal %d: %w", try.ProposalNumber, err)
	}

	out := map[domain.GraderCheckResult]domain.GraderCheck{}

	for _, c := range all {
		if c.ItemID == try.ItemID {
			out[c.Result] = c
		}
	}

	return out, nil
}

// graderCheck runs the grader on try's item's result r: one grade run, except for an item with no
// final run (finalRun), whose final check is unknown without one.
func (s *Tries) graderCheck(ctx context.Context, current domain.Line, try domain.PastTry, orig domain.Run, r domain.GraderCheckResult,
) (domain.NewGraderCheck, error) {
	n := domain.NewGraderCheck{ProposalID: try.ProposalID, ItemID: try.ItemID, Result: r}

	of := domain.GradeOriginal
	if r == domain.GraderCheckFinal {
		of = domain.GradeFinal

		_, err := finalRun(ctx, s.d.Runs, try)

		switch {
		case errors.Is(err, ports.ErrNotFound):
			n.Answer, n.Reason, n.CheckedAt = domain.PastTryUnknown,
				fmt.Sprintf("the item has no final accepted result: no run of it at %s succeeded", try.Station), s.d.Clock.Now().UTC()

			return n, nil
		case err != nil:
			return domain.NewGraderCheck{}, err
		}
	}

	a, cost, run, err := s.gradeRun(ctx, current, try, orig, of)
	if err != nil {
		return domain.NewGraderCheck{}, err
	}

	n.Answer, n.Reason, n.CostUSD, n.RunID = a.Verdict, truncate(a.Reason, domain.MaxGraderCheckReason), cost, run
	n.CheckedAt = s.d.Clock.Now().UTC()

	return n, nil
}

// gradeRun runs one grade of try's result of on current's runtime.model: a run of kind try on a
// grade bundle with the model's access alone (domain.RunRequest.checkGrade), bounded by what is
// left of the try's lease. It returns the answer, the run's cost and its run. A run that errors,
// fails or gives no valid answer (domain.ParseGradeAnswer) answers unknown with why. errGradeWait
// when Jonah's budget is used up, errLeaseShort when the lease has less than minGradeTime left, and
// a cancelled ctx is an error.
func (s *Tries) gradeRun(ctx context.Context, current domain.Line, try domain.PastTry, orig domain.Run, of domain.GradeOf,
) (domain.GradeAnswer, float64, domain.RunID, error) {
	if used, err := s.budgetUsed(ctx, current); err != nil || used {
		return domain.GradeAnswer{}, 0, "", cmp.Or(err, errGradeWait)
	}

	timeout := min(s.c.Timeout, try.LeaseUntil.Sub(s.d.Clock.Now())-s.c.Margin)
	if timeout < minGradeTime {
		return domain.GradeAnswer{}, 0, "", errLeaseShort
	}

	rt := current.Runtime

	req := domain.RunRequest{
		FactoryID: try.FactoryID, Version: current.VersionID, Kind: domain.RunKindTry, Station: try.Station, Agent: orig.Agent,
		Model: rt.Model, Bundle: domain.TaskKindGrade, TryOf: try.ID, GradeOf: of, ModelConnection: rt.ModelConnection,
		Size: domain.SandboxSmall, Timeout: timeout,
	}
	if current.OnSecrets() { // the model key alone (ADR 0037 decision 7)
		req.ModelConnection, req.ModelKey = "", rt.ModelKey
	}

	req.OrgID, _ = domain.OrgFrom(ctx)

	run, err := s.d.Runner.RunTask(ctx, req)

	unknown := func(format string, a ...any) (domain.GradeAnswer, float64, domain.RunID, error) {
		return domain.GradeAnswer{Verdict: domain.PastTryUnknown, Reason: fmt.Sprintf(format, a...)}, run.CostUSD, run.RunID, nil
	}

	switch {
	case err != nil && ctx.Err() != nil:
		return domain.GradeAnswer{}, 0, "", fmt.Errorf("grade the %s result: %w", of, err)
	case err != nil:
		return unknown("the grade of the %s result could not run: %v", of, err)
	case run.Status != domain.RunSucceeded:
		return unknown("the grade of the %s result %s: %s", of, run.Status, cmp.Or(run.Error, "no error given"))
	}

	a, err := domain.ParseGradeAnswer(run.Output)
	if err != nil {
		return unknown("the grader gave no valid answer on the %s result: %v", of, err)
	}

	return a, run.CostUSD, run.RunID, nil
}
