package porttest_test

import (
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
func TestEvals_BakeOffs(t *testing.T) {
	t.Parallel()

	ctx := domain.WithOrg(t.Context(), "00000000-0000-7000-8000-0000000000a1")
	e := porttest.NewEvals(time.Now)
	hash := domain.BlobHash(strings.Repeat("c", 64))
	n := domain.NewBakeOff{
		FactoryID: "00000000-0000-7000-8000-0000000000f1", VersionID: "00000000-0000-7000-8000-0000000000b1", ContentSHA256: hash,
		Station: "build", Agent: "frontend-engineer", TriesPerCase: 2,
		Cases: []domain.EvalCase{{Name: "spec-review", SHA256: hash}, {Name: "merge-step", SHA256: hash}},
		Arms:  []domain.NewBakeOffArm{{Model: "claude-test-now-1", Current: true}, {Model: "claude-test-other-1"}},
	}

	if _, err := e.CreateRun(ctx, domain.NewEvalRun{
		FactoryID: n.FactoryID, ContentSHA256: hash, Trigger: domain.EvalManual, Cases: []domain.EvalCase{{Name: "merge-step", SHA256: hash, Tries: 1}},
	}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	// Finish the eval run's one try, so the bake-off's tries are the next ones claimed.
	first, err := e.ClaimResult(ctx, time.Now(), time.Minute)
	if err != nil || first.Arm != 0 {
		t.Fatalf("ClaimResult of the eval run = %+v, %v; want arm 0", first, err)
	}

	if err := e.RecordResult(ctx, first, domain.EvalOutcome{State: domain.EvalPass}); err != nil {
		t.Fatal(err)
	}

	b, err := e.CreateBakeOff(ctx, n, domain.LocalUser)
	if err != nil {
		t.Fatal(err)
	}

	if b.Kind != domain.EvalKindBakeOff || len(b.Arms) != 2 || b.Arms[1].Arm != 2 || !b.Arms[0].Current || len(b.Results) != 8 {
		t.Errorf("CreateBakeOff = %+v, want 2 arms and 8 tries", b)
	}

	var claims []string

	for range 3 {
		r, err := e.ClaimResult(ctx, time.Now(), time.Minute)
		if err != nil {
			t.Fatal(err)
		}

		claims = append(claims, fmt.Sprintf("arm %d %s/%d", r.Arm, r.Case, r.Try))

		if err := e.RecordResult(ctx, r, domain.EvalOutcome{State: domain.EvalPass, Duration: time.Second}); err != nil {
			t.Fatal(err)
		}
	}

	if got, want := strings.Join(claims, ", "), "arm 1 merge-step/1, arm 2 merge-step/1, arm 1 merge-step/2"; got != want {
		t.Errorf("claims = %s, want %s", got, want)
	}

	if err := e.RecordArmSummary(ctx, b.ID, 2, domain.ArmSummary{Tries: 4, Passed: 3}); err != nil {
		t.Fatal(err)
	}

	got, err := e.GetBakeOff(ctx, b.ID)
	if err != nil || got.Arms[1].Summary == nil || got.Arms[1].Summary.Passed != 3 || got.Results[0].Duration != time.Second {
		t.Errorf("GetBakeOff = %+v, %v; want arm 2's summary and the tries' durations", got, err)
	}

	if err := e.RecordArmSummary(ctx, b.ID, 3, domain.ArmSummary{}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("RecordArmSummary of arm 3 of 2 = %v, want ErrNotFound", err)
	}

	if list, err := e.ListBakeOffs(ctx, n.FactoryID, 10); err != nil || len(list) != 1 || list[0].ID != b.ID || list[0].Results != nil {
		t.Errorf("ListBakeOffs = %+v, %v; want the bake-off without results", list, err)
	}

	if latest, err := e.LatestRunForContent(ctx, n.FactoryID, hash); err != nil || latest.Kind != domain.EvalKindEval {
		t.Errorf("LatestRunForContent = %+v, %v; want the eval run, never the bake-off", latest, err)
	}

	if h, err := e.CaseHistory(ctx, n.FactoryID); err != nil || len(h) != 1 || len(h[0].Tries) != 1 || h[0].Tries[0].Arm != 0 {
		t.Errorf("CaseHistory = %+v, %v; want the eval run's one try", h, err)
	}
}
