package jonah_test

import (
	"errors"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/jonah"
)

// sendBack is why the graded items went back.
const sendBack = "the change has no tests"

// gradeWorld is a try world with one queued try of a graded item, claimed, and the grade
// material read from the same store.
type gradeWorld struct {
	*tryWorld
	org    domain.OrgID
	item   domain.Item
	try    domain.PastTry
	grades *jonah.Grades
}

func newGradeWorld(t *testing.T, item func(w *tryWorld, org domain.OrgID) domain.Item) *gradeWorld {
	t.Helper()

	w := newTryWorld(t, 1)
	org := w.orgs[0]
	it := item(w, org)
	p := w.propose(org, writerPath, it)
	w.sweep()

	try, err := w.store.ClaimPastTry(w.ctx(org), w.factories[org], rt0, time.Hour, 2)
	if err != nil || try.ProposalID != p.ID {
		t.Fatalf("ClaimPastTry = %+v, %v", try, err)
	}

	return &gradeWorld{tryWorld: w, org: org, item: it, try: try, grades: jonah.NewGrades(jonah.GradeDeps{Tries: w.store, Runs: w.store, Events: w.store})}
}

// ran records the try's run, of output, with diff.
func (g *gradeWorld) ran(output, diff string) {
	g.t.Helper()

	res := g.recorded(g.ctx(g.org), domain.RunRequest{FactoryID: g.try.FactoryID, Station: stBuild, Agent: tryAgent},
		domain.RunResult{Status: domain.RunSucceeded, Output: []byte(output), CostUSD: 0.25})

	if _, err := g.store.RecordPastTryRun(g.ctx(g.org), g.try, res.RunID, diff, 0.25); err != nil {
		g.t.Fatal(err)
	}
}

func (g *gradeWorld) material(of domain.GradeOf) (domain.GradeMaterial, error) {
	return g.grades.GradeMaterial(g.ctx(g.org), g.try.ID, of)
}

// A grade reads the try's run output and its stored diff, the original run's output, or the
// output of the item's newest succeeded run at the try's station; each with the reason of the
// item's first send-back after the original run started.
func TestGrades_MaterialOfEachResult(t *testing.T) {
	t.Parallel()

	g := newGradeWorld(t, func(w *tryWorld, org domain.OrgID) domain.Item {
		return w.gradedItem(org, rt0.Add(-2*time.Hour), sendBack)
	})
	g.ran(`{"tests":"added"}`, tryDiff)

	for _, tt := range []struct {
		of   domain.GradeOf
		want domain.GradeMaterial
	}{
		{of: domain.GradeTry, want: domain.GradeMaterial{Reason: sendBack, Output: `{"tests":"added"}`, Diff: tryDiff}},
		{of: domain.GradeOriginal, want: domain.GradeMaterial{Reason: sendBack, Output: originalOutput}},
		{of: domain.GradeFinal, want: domain.GradeMaterial{Reason: sendBack, Output: `{"tests":true}`}},
	} {
		got, err := g.material(tt.of)
		if err != nil || got != tt.want {
			t.Errorf("GradeMaterial(%s) = %+v, %v; want %+v", tt.of, got, err, tt.want)
		}
	}
}

// A send-back before the original run started is not why the original went back: the item has no
// reason then.
func TestGrades_ReasonAfterTheOriginalRun(t *testing.T) {
	t.Parallel()

	g := newGradeWorld(t, func(w *tryWorld, org domain.OrgID) domain.Item {
		it, _ := w.pastItem(org, rt0.Add(-time.Hour), true)

		err := w.store.AppendEvents(w.ctx(org), domain.Event{
			FactoryID: w.factories[org], ItemID: it.ID, At: rt0.Add(-2 * time.Hour), Kind: domain.EventGateRejected, Station: stBuild,
			Actor: tryAgent, Data: map[string]any{"reason": "an earlier send-back"},
		})
		if err != nil {
			w.t.Fatal(err)
		}

		return it
	})

	got, err := g.material(domain.GradeOriginal)
	if err != nil || got.Reason != "" || got.Output != originalOutput {
		t.Errorf("GradeMaterial(original) = %+v, %v; want the original output and no reason", got, err)
	}
}

// A result that does not exist is ErrNotFound: a try without its run, an item without a final
// accepted result, or a try that is not the organization's.
func TestGrades_MissingResultIsNotFound(t *testing.T) {
	t.Parallel()

	g := newGradeWorld(t, func(w *tryWorld, org domain.OrgID) domain.Item { return w.unacceptedItem(org) })

	if _, err := g.material(domain.GradeTry); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("GradeMaterial(try) without its run = %v; want ErrNotFound", err)
	}

	if _, err := g.material(domain.GradeFinal); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("GradeMaterial(final) of an item without one = %v; want ErrNotFound", err)
	}

	if _, err := g.grades.GradeMaterial(g.ctx(g.org), "00000000-0000-7000-8000-00000000ffff", domain.GradeOriginal); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("GradeMaterial of an unknown try = %v; want ErrNotFound", err)
	}
}
