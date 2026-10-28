package porttest_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The fake keeps the bake-off rules of the PostgreSQL store (TestEvals_*BakeOff* in
// internal/adapters/store/postgres), so the eval loop's tests of bake-offs hold for it.

// bakeOffFixture is a fake with a finished eval run of one try, then a bake-off of two arms on
// two cases with two tries each.
type bakeOffFixture struct {
	org     domain.OrgID
	e       *porttest.Evals
	factory domain.FactoryID
	hash    domain.BlobHash
	b       domain.BakeOff
}

func newBakeOffFixture(t *testing.T) bakeOffFixture {
	t.Helper()

	f := bakeOffFixture{
		org: "00000000-0000-7000-8000-0000000000a1", e: porttest.NewEvals(time.Now),
		factory: "00000000-0000-7000-8000-0000000000f1", hash: domain.BlobHash(strings.Repeat("c", 64)),
	}

	if _, err := f.e.CreateRun(f.in(t), domain.NewEvalRun{
		FactoryID: f.factory, ContentSHA256: f.hash, Trigger: domain.EvalManual, Cases: []domain.EvalCase{{Name: "merge-step", SHA256: f.hash, Tries: 1}},
	}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	first := f.claim(t)
	if first.Arm != 0 {
		t.Fatalf("the eval run's try = %+v, want arm 0", first)
	}

	b, err := f.e.CreateBakeOff(f.in(t), domain.NewBakeOff{
		FactoryID: f.factory, VersionID: "00000000-0000-7000-8000-0000000000b1", ContentSHA256: f.hash,
		Station: "build", Agent: "frontend-engineer", TriesPerCase: 2,
		Cases: []domain.EvalCase{{Name: "spec-review", SHA256: f.hash}, {Name: "merge-step", SHA256: f.hash}},
		Arms:  []domain.NewBakeOffArm{{Model: "claude-test-now-1", Current: true}, {Model: "claude-test-other-1"}},
	}, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	f.b = b

	return f
}

// in is t's context in the fixture's organization.
func (f bakeOffFixture) in(t *testing.T) context.Context {
	t.Helper()

	return domain.WithOrg(t.Context(), f.org)
}

// claim claims the next try and records it passed in a second.
func (f bakeOffFixture) claim(t *testing.T) domain.EvalResult {
	t.Helper()

	r, err := f.e.ClaimResult(f.in(t), time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.e.RecordResult(f.in(t), r, domain.EvalOutcome{State: domain.EvalPass, Duration: time.Second}); err != nil {
		t.Fatal(err)
	}

	return r
}

func TestEvals_BakeOffClaimsArmsInTurn(t *testing.T) {
	t.Parallel()

	f := newBakeOffFixture(t)
	if f.b.Kind != domain.EvalKindBakeOff || len(f.b.Arms) != 2 || f.b.Arms[1].Arm != 2 || !f.b.Arms[0].Current || len(f.b.Results) != 8 {
		t.Errorf("CreateBakeOff = %+v, want 2 arms and 8 tries", f.b)
	}

	claims := make([]string, 0, 3)

	for range 3 {
		r := f.claim(t)
		claims = append(claims, fmt.Sprintf("arm %d %s/%d", r.Arm, r.Case, r.Try))
	}

	if got, want := strings.Join(claims, ", "), "arm 1 merge-step/1, arm 2 merge-step/1, arm 1 merge-step/2"; got != want {
		t.Errorf("claims = %s, want %s", got, want)
	}

	if got, err := f.e.GetBakeOff(f.in(t), f.b.ID); err != nil || got.Results[0].Duration != time.Second {
		t.Errorf("GetBakeOff = %+v, %v; want the tries' durations", got, err)
	}
}

func TestEvals_BakeOffArmSummary(t *testing.T) {
	t.Parallel()

	f := newBakeOffFixture(t)
	if err := f.e.RecordArmSummary(f.in(t), f.b.ID, 2, domain.ArmSummary{Tries: 4, Passed: 3}); err != nil {
		t.Fatal(err)
	}

	if got, err := f.e.GetBakeOff(f.in(t), f.b.ID); err != nil || got.Arms[0].Summary != nil || got.Arms[1].Summary == nil || got.Arms[1].Summary.Passed != 3 {
		t.Errorf("GetBakeOff = %+v, %v; want arm 2's summary only", got, err)
	}

	if err := f.e.RecordArmSummary(f.in(t), f.b.ID, 3, domain.ArmSummary{}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("RecordArmSummary of arm 3 of 2 = %v, want ErrNotFound", err)
	}

	if list, err := f.e.ListBakeOffs(f.in(t), f.factory, 10); err != nil || len(list) != 1 || list[0].ID != f.b.ID || list[0].Results != nil {
		t.Errorf("ListBakeOffs = %+v, %v; want the bake-off without results", list, err)
	}
}

func TestEvals_BakeOffIsNotAnEvalRun(t *testing.T) {
	t.Parallel()

	f := newBakeOffFixture(t)
	f.claim(t)

	if latest, err := f.e.LatestRunForContent(f.in(t), f.factory, f.hash); err != nil || latest.Kind != domain.EvalKindEval {
		t.Errorf("LatestRunForContent = %+v, %v; want the eval run, never the bake-off", latest, err)
	}

	if h, err := f.e.CaseHistory(f.in(t), f.factory); err != nil || len(h) != 1 || len(h[0].Tries) != 1 || h[0].Tries[0].Arm != 0 {
		t.Errorf("CaseHistory = %+v, %v; want the eval run's one try", h, err)
	}
}
