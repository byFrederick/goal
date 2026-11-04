package domain_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// repeat is n copies of v.
func repeat(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}

	return out
}

// rate is n samples of which sent are 1 and the rest 0.
func rate(sent, n int) []float64 { return append(repeat(1, sent), repeat(0, n-sent)...) }

// spread is n samples around mid, evenly from mid-width to mid+width.
func spread(mid, width float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = mid - width + 2*width*float64(i)/float64(n-1)
	}

	return out
}

func TestWatchInterval(t *testing.T) {
	t.Parallel()

	near := func(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

	tests := []struct {
		name          string
		metric        domain.ProposalMetric
		before, after []float64
		ok            bool
		low, high     float64
		tol           float64
	}{
		// Agresti–Caffo: p̃ = 21/102 and 41/102, z = 1.645.
		{"a rate's difference of proportions", domain.MetricSentBackRate, rate(20, 100), rate(40, 100), true, 0.0926, 0.2996, 0.002},
		{"a rate that did not move", domain.MetricSentBackRate, rate(30, 200), rate(30, 200), true, -0.0590, 0.0590, 0.002},
		// Welch on the mean: means 1 and 2, both of variance 0.17 (n = 50).
		{"cost per item's Welch interval", domain.MetricCostPerItem, spread(1, 0.7, 50), spread(2, 0.7, 50), true, 0.862, 1.138, 0.005},
		{"lead time's bootstrap holds the difference", domain.MetricLeadTimeP85, spread(100, 10, 60), spread(200, 10, 60), true, 99, 101.5, 1.5},
		{"one item a side is no interval", domain.MetricCostPerItem, []float64{1}, []float64{2}, false, 0, 0, 0},
		{"no items after is no interval", domain.MetricSentBackRate, rate(1, 10), nil, false, 0, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := domain.WatchIntervalOf(tc.metric, tc.before, tc.after)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (%+v)", ok, tc.ok, got)
			}

			if ok && (!near(got.Low, tc.low, tc.tol) || !near(got.High, tc.high, tc.tol)) {
				t.Errorf("interval = [%.4f, %.4f], want [%.4f, %.4f] ± %v", got.Low, got.High, tc.low, tc.high, tc.tol)
			}
		})
	}

	// The bootstrap's seed is fixed: the same samples give the same interval.
	a, _ := domain.WatchIntervalOf(domain.MetricLeadTimeP85, spread(100, 40, 30), spread(110, 40, 30))
	b, _ := domain.WatchIntervalOf(domain.MetricLeadTimeP85, spread(100, 40, 30), spread(110, 40, 30))

	if a != b {
		t.Errorf("the bootstrap differs between calls: %+v and %+v", a, b)
	}
}

func TestWatchVerdict(t *testing.T) {
	t.Parallel()

	day := 24 * time.Hour
	side := func(n int, v float64) domain.WatchSide { return domain.WatchSide{N: n, Value: v} }
	iv := func(lo, hi float64) *domain.WatchInterval { return &domain.WatchInterval{Low: lo, High: hi} }

	tests := []struct {
		name          string
		metric        domain.ProposalMetric
		before, after domain.WatchSide
		interval      *domain.WatchInterval
		age           time.Duration
		want          domain.WatchVerdict
	}{
		{"a rate that rose by more than 5 points is worse", domain.MetricSentBackRate, side(100, 0.2), side(100, 0.4), iv(0.09, 0.29), day, domain.WatchWorse},
		{"a rate that fell by more than 5 points helped", domain.MetricSentBackRate, side(100, 0.4), side(100, 0.2), iv(-0.29, -0.09), day, domain.WatchHelped},
		{"a rate inside ±5 points is no change", domain.MetricSentBackRate, side(200, 0.15), side(200, 0.15), iv(-0.04, 0.04), day, domain.WatchNoChange},
		{"a rate's interval across the minimum is too early", domain.MetricSentBackRate, side(20, 0.2), side(20, 0.3), iv(-0.1, 0.3), day, domain.WatchTooEarly},
		{"an interval that just touches the minimum is too early", domain.MetricSentBackRate, side(20, 0.2), side(20, 0.3), iv(0.05, 0.3), day, domain.WatchTooEarly},
		{"cost up by more than 10% of before is worse", domain.MetricCostPerItem, side(50, 1), side(50, 2), iv(0.8, 1.2), day, domain.WatchWorse},
		{"cost inside ±10% of before is no change", domain.MetricCostPerItem, side(50, 2), side(50, 2.05), iv(-0.15, 0.18), day, domain.WatchNoChange},
		{"lead time down by more than 10% helped", domain.MetricLeadTimeP85, side(60, 100), side(60, 80), iv(-30, -12), day, domain.WatchHelped},
		{"no interval yet is too early", domain.MetricCostPerItem, side(1, 2), side(0, 0), nil, day, domain.WatchTooEarly},
		{"too early after 14 days is no clear change", domain.MetricSentBackRate, side(20, 0.2), side(20, 0.3), iv(-0.1, 0.3), 14 * day, domain.WatchNoClearChange},
		{"too early after 150 items is no clear change", domain.MetricSentBackRate, side(150, 0.2), side(150, 0.22), iv(-0.06, 0.1), day, domain.WatchNoClearChange},
		{"no interval after 14 days is no clear change", domain.MetricCostPerItem, side(0, 0), side(0, 0), nil, 15 * day, domain.WatchNoClearChange},
		{"worse stays worse after 14 days", domain.MetricSentBackRate, side(100, 0.2), side(100, 0.4), iv(0.09, 0.29), 20 * day, domain.WatchWorse},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := domain.WatchVerdictOf(tc.metric, tc.before, tc.after, tc.interval, tc.age); got != tc.want {
				t.Errorf("verdict = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestWatchSideOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		metric  domain.ProposalMetric
		samples []float64
		want    domain.WatchSide
	}{
		{domain.MetricSentBackRate, rate(1, 4), domain.WatchSide{N: 4, Value: 0.25}},
		{domain.MetricCostPerItem, []float64{1, 2, 3}, domain.WatchSide{N: 3, Value: 2}},
		{domain.MetricLeadTimeP85, []float64{10, 20, 30, 40, 50}, domain.WatchSide{N: 5, Value: 44}},
		{domain.MetricCostPerItem, nil, domain.WatchSide{}},
	}

	for _, tc := range tests {
		if got := domain.WatchSideOf(tc.metric, tc.samples); math.Abs(got.Value-tc.want.Value) > 1e-9 || got.N != tc.want.N {
			t.Errorf("WatchSideOf(%s, %v) = %+v, want %+v", tc.metric, tc.samples, got, tc.want)
		}
	}
}

func TestUndoReplacements(t *testing.T) {
	t.Parallel()

	base := "# Build\n\nWrite the code.\nRun the tests.\nOpen a pull request.\n\n## Notes\n\nBe brief.\n"
	published := "# Build\n\nWrite the code.\nRun the tests twice.\nOpen a pull request.\n\n## Notes\n\nBe brief.\nCite the spec.\n"

	reps := domain.ReplacementsOf(base, published)
	if len(reps) != 2 {
		t.Fatalf("replacements = %+v, want one for each changed place", reps)
	}

	if got, err := domain.ApplyReplacements(base, reps); err != nil || got != published {
		t.Fatalf("applying them to the base = %q, %v; want the published file", got, err)
	}

	undo := domain.ReverseReplacements(reps)

	t.Run("reverses the change", func(t *testing.T) {
		t.Parallel()

		if got, err := domain.ApplyReplacements(published, undo); err != nil || got != base {
			t.Errorf("undo = %q, %v; want the base", got, err)
		}
	})

	t.Run("works after an unrelated publish", func(t *testing.T) {
		t.Parallel()

		later := "# Build the change\n\nWrite the code.\nRun the tests twice.\nOpen a pull request.\n\n## Notes\n\nBe brief.\nCite the spec.\nAsk when unsure.\n"
		want := "# Build the change\n\nWrite the code.\nRun the tests.\nOpen a pull request.\n\n## Notes\n\nBe brief.\nAsk when unsure.\n"

		if got, err := domain.ApplyReplacements(later, undo); err != nil || got != want {
			t.Errorf("undo = %q, %v; want %q", got, err, want)
		}
	})

	t.Run("refuses when the changed text is gone", func(t *testing.T) {
		t.Parallel()

		rewritten := "# Build\n\nWrite the code.\nRun every test.\n"
		if _, err := domain.ApplyReplacements(rewritten, undo); !errors.Is(err, domain.ErrReplacementMissing) {
			t.Errorf("err = %v, want ErrReplacementMissing", err)
		}
	})

	t.Run("refuses when the changed text occurs twice", func(t *testing.T) {
		t.Parallel()

		twice := published + published
		if _, err := domain.ApplyReplacements(twice, undo); !errors.Is(err, domain.ErrReplacementMissing) {
			t.Errorf("err = %v, want ErrReplacementMissing", err)
		}
	})

	t.Run("context makes each old text unique", func(t *testing.T) {
		t.Parallel()

		before := "a\nx\nb\nx\nc\n"
		after := "a\nx\nb\ny\nc\n"

		rs := domain.ReplacementsOf(before, after)
		if got, err := domain.ApplyReplacements(after, domain.ReverseReplacements(rs)); err != nil || got != before {
			t.Errorf("undo = %q, %v; want %q (replacements %+v)", got, err, before, rs)
		}
	})

	t.Run("no change is no replacement", func(t *testing.T) {
		t.Parallel()

		if rs := domain.ReplacementsOf(base, base); len(rs) != 0 {
			t.Errorf("replacements = %+v, want none", rs)
		}
	})
}
