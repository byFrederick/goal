package gateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Grades reads what a grade run grades (ADR 0039 decision 10): the reason try's item went back
// and the result of, a try's output and diff or the item's original or final one, for the
// organization in ctx. Choosing them is grading's, so internal/jonah implements it; a try or a
// result it does not have wraps ports.ErrNotFound.
type Grades interface {
	GradeMaterial(ctx context.Context, try domain.PastTryID, of domain.GradeOf) (domain.GradeMaterial, error)
}

// errNoGrades answers a grade token's bundle when the gateway has no Deps.Grades or Deps.Jonah: 503.
var errNoGrades = errors.New("the gateway has no grades")

// bundleGrade is a grade's spec.grade, as the worker's Grade (apps/worker/src/spec.ts) reads it:
// the reason the item went back and the result to grade.
type bundleGrade struct {
	Reason string            `json:"reason"`
	Result bundleGradeResult `json:"result"`
}

// bundleGradeResult is the result a grade grades: which one, its output and its diff.
type bundleGradeResult struct {
	Kind   domain.GradeOf `json:"kind"`
	Output string         `json:"output"`
	Diff   string         `json:"diff"`
}

// gradeBundle is a grade's bundle: spec.grade from Deps.Grades for the token's try and result,
// the token's model and, as max_usd, what Jonah's share has left (jonahBudgetLeft), since a
// grade's cost is Jonah's. It has no item, repository, definition, file, connection, knowledge,
// rule or tool. A version that is not the token's factory's wraps errForbidden.
func (g *Gateway) gradeBundle(ctx context.Context, c domain.TaskClaims) (runBundle, error) {
	if g.grades == nil || g.jonah == nil {
		return runBundle{}, errNoGrades
	}

	f, err := g.defs.Version(ctx, c.VersionID)
	if err != nil {
		return runBundle{}, fmt.Errorf("load factory %s version %s: %w", c.FactoryID, c.VersionID, err)
	}

	if f.Metadata.ID != string(c.FactoryID) {
		return runBundle{}, fmt.Errorf("%w: version %s is not a version of factory %s", errForbidden, c.VersionID, c.FactoryID)
	}

	m, err := g.grades.GradeMaterial(ctx, c.TryOf, c.GradeOf)
	if err != nil {
		return runBundle{}, fmt.Errorf("grade of try %s's %s result: %w", c.TryOf, c.GradeOf, err)
	}

	left, err := g.jonahBudgetLeft(ctx, c.FactoryID, f.Line().Jonah)
	if err != nil {
		return runBundle{}, err
	}

	return runBundle{
		Spec: bundleSpec{
			FactoryID: c.FactoryID, RunID: c.RunID, Station: c.Station, Agent: c.Agent, Model: c.Model, MaxUSD: left,
			Skills: []string{}, ToolsAllow: []string{}, ToolsDeny: []string{},
			Rules: []bundleRule{}, GrantedCalls: []grantedCall{},
			Grade: &bundleGrade{Reason: m.Reason, Result: bundleGradeResult{Kind: c.GradeOf, Output: m.Output, Diff: m.Diff}},
		},
		Files: map[string][]byte{},
		Git:   []gitCredential{},
		Tools: []string{},
	}, nil
}
