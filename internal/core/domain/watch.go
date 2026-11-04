package domain

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
)

// A watch (ADR 0039 decisions 14 and 15) compares one number at one station before and after a
// published change: a Jonah proposal's own number, or for a person's publish the send-back rate
// and the cost per item at each station the change reaches. Each item's outcome at a station is
// credited to the definition version of its run there.

// MetricCostPerItem is the cost of an item's runs at a station, which a person's publish is
// watched on besides the send-back rate.
const MetricCostPerItem ProposalMetric = "cost_per_item"

// WatchVerdict is what a watch says of its change.
type WatchVerdict string

// Watch verdicts.
const (
	WatchTooEarly      WatchVerdict = "too_early"
	WatchHelped        WatchVerdict = "helped"
	WatchWorse         WatchVerdict = "worse"
	WatchNoChange      WatchVerdict = "no_change"
	WatchNoClearChange WatchVerdict = "no_clear_change"
)

// Bounds of a watch.
const (
	// WatchMaxAge is how long a watch runs before a verdict still too early is no clear change.
	WatchMaxAge = 14 * 24 * time.Hour
	// WatchMaxItems is how many items after the change end a watch still too early the same way.
	WatchMaxItems = 150
	// watchZ is the normal quantile of a two-sided 90% interval.
	watchZ = 1.6448536269514722
	// watchRateEffect is the minimum effect on a rate: 5 points.
	watchRateEffect = 0.05
	// watchRelativeEffect is the minimum effect on cost or lead time: 10% of before.
	watchRelativeEffect = 0.10
	// watchBootstrapRounds is how many resamples estimate lead time's interval.
	watchBootstrapRounds = 2000
)

// WatchSide is one side of a watch: the items counted and the metric's value over them (the
// rate, the mean cost in USD, or the 85th percentile of lead time in seconds).
type WatchSide struct {
	N     int
	Value float64
}

// WatchInterval is a 90% interval of after minus before.
type WatchInterval struct {
	Low, High float64
}

// Watch is one change's watch at one station on one metric. ProposalID is set when the change
// is a Jonah proposal's publish, and the watch is then on the proposal's own number. Interval is
// nil while either side has too few items. ClosedAt is set once the verdict is final.
type Watch struct {
	FactoryID  FactoryID
	VersionID  DefinitionVersionID
	ProposalID ProposalID
	Station    string
	Metric     ProposalMetric
	Before     WatchSide
	After      WatchSide
	Interval   *WatchInterval
	Verdict    WatchVerdict
	OpenedAt   time.Time
	ClosedAt   time.Time
	UpdatedAt  time.Time
}

// StationOutcome is one item's outcome at a station on one definition version: the runs it had
// there on that version (from the first, At, until its next run there on another version), the
// cost of those runs, whether it was sent back at the station in that time, and how long it spent
// there, the spans that left in that time.
type StationOutcome struct {
	VersionID DefinitionVersionID
	ItemID    ItemID
	At        time.Time
	SentBack  bool
	CostUSD   float64
	LeadTime  time.Duration
}

// Sample is o's value for metric m: 1 or 0 for the send-back rate, USD for cost, seconds for
// lead time.
func (o StationOutcome) Sample(m ProposalMetric) float64 {
	switch m {
	case MetricSentBackRate:
		if o.SentBack {
			return 1
		}

		return 0
	case MetricCostPerItem:
		return o.CostUSD
	default:
		return o.LeadTime.Seconds()
	}
}

// VersionReach is a published version and the stations it reaches against the version
// published before it: those whose agent file, applied rules, loaded skills, contract or model
// changed. First marks a factory's first version, which reaches every station and has no before.
type VersionReach struct {
	ID          DefinitionVersionID
	Number      int
	PublishedAt time.Time
	Stations    []string
	First       bool
}

// Reaches reports whether v reaches station.
func (v VersionReach) Reaches(station string) bool {
	return v.First || slices.Contains(v.Stations, station)
}

// WatchSideOf is samples' side for metric m.
func WatchSideOf(m ProposalMetric, samples []float64) WatchSide {
	if len(samples) == 0 {
		return WatchSide{}
	}

	if m == MetricLeadTimeP85 {
		return WatchSide{N: len(samples), Value: percentile(samples, 0.85)}
	}

	return WatchSide{N: len(samples), Value: mean(samples)}
}

// WatchIntervalOf is a 90% interval of after minus before for metric m, lower being better: a
// difference of proportions (Agresti–Caffo) for the send-back rate, a Welch interval on the mean
// for cost, and a bootstrap of the 85th percentile, with a fixed seed, for lead time. False while
// either side has fewer than two items.
func WatchIntervalOf(m ProposalMetric, before, after []float64) (WatchInterval, bool) {
	if len(before) < 2 || len(after) < 2 {
		return WatchInterval{}, false
	}

	switch m {
	case MetricSentBackRate:
		return proportionInterval(before, after), true
	case MetricCostPerItem:
		return welchInterval(before, after), true
	default:
		return bootstrapInterval(before, after), true
	}
}

// WatchVerdictOf is the verdict on metric m with these sides and interval (nil when there is
// none yet), a watch age old: Helped or Worse when the interval clears the minimum effect, No
// change when it lies inside it, else Too early, which turns No clear change once the watch is
// WatchMaxAge old or counted WatchMaxItems after.
func WatchVerdictOf(m ProposalMetric, before, after WatchSide, interval *WatchInterval, age time.Duration) WatchVerdict {
	if interval != nil {
		least := watchRateEffect
		if m != MetricSentBackRate {
			least = watchRelativeEffect * math.Abs(before.Value)
		}

		switch {
		case interval.High < -least:
			return WatchHelped
		case interval.Low > least:
			return WatchWorse
		case interval.Low >= -least && interval.High <= least:
			return WatchNoChange
		}
	}

	if age >= WatchMaxAge || after.N >= WatchMaxItems {
		return WatchNoClearChange
	}

	return WatchTooEarly
}

// proportionInterval is the Agresti–Caffo interval of two proportions of 0/1 samples.
func proportionInterval(before, after []float64) WatchInterval {
	adjusted := func(s []float64) (p, v float64) {
		n := float64(len(s)) + 2
		p = (sumOf(s) + 1) / n

		return p, p * (1 - p) / n
	}

	p1, v1 := adjusted(before)
	p2, v2 := adjusted(after)
	d, half := p2-p1, watchZ*math.Sqrt(v1+v2)

	return WatchInterval{Low: d - half, High: d + half}
}

// welchInterval is Welch's interval of the difference of two means, with the
// Welch–Satterthwaite degrees of freedom.
func welchInterval(before, after []float64) WatchInterval {
	n1, n2 := float64(len(before)), float64(len(after))
	a, b := variance(before)/n1, variance(after)/n2
	d := mean(after) - mean(before)

	if a+b == 0 {
		return WatchInterval{Low: d, High: d}
	}

	df := (a + b) * (a + b) / (a*a/(n1-1) + b*b/(n2-1))
	half := tQuantile(df) * math.Sqrt(a+b)

	return WatchInterval{Low: d - half, High: d + half}
}

// tQuantile is Student's t 95th percentile at df degrees of freedom, by the Cornish–Fisher
// expansion around the normal's: within 1% from 2 degrees of freedom.
func tQuantile(df float64) float64 {
	z := watchZ
	z3, z5, z7 := z*z*z, math.Pow(z, 5), math.Pow(z, 7)

	return z + (z3+z)/(4*df) + (5*z5+16*z3+3*z)/(96*df*df) + (3*z7+19*z5+17*z3-15*z)/(384*df*df*df)
}

// bootstrapInterval is the 5th and 95th percentiles of the difference of the 85th percentiles
// over resamples of both sides, from a fixed seed so every goald computes the same interval.
func bootstrapInterval(before, after []float64) WatchInterval {
	r := rand.New(rand.NewPCG(0x6a6f6e6168, 0x7761746368)) //nolint:gosec // a reproducible resample, not a secret

	diffs := make([]float64, watchBootstrapRounds)
	a, b := make([]float64, len(before)), make([]float64, len(after))

	for i := range diffs {
		for j := range a {
			a[j] = before[r.IntN(len(before))]
		}

		for j := range b {
			b[j] = after[r.IntN(len(after))]
		}

		diffs[i] = percentile(b, 0.85) - percentile(a, 0.85)
	}

	return WatchInterval{Low: percentile(diffs, 0.05), High: percentile(diffs, 0.95)}
}

func sumOf(s []float64) float64 {
	t := 0.0
	for _, v := range s {
		t += v
	}

	return t
}

func mean(s []float64) float64 { return sumOf(s) / float64(len(s)) }

// variance is s's sample variance.
func variance(s []float64) float64 {
	m, t := mean(s), 0.0
	for _, v := range s {
		t += (v - m) * (v - m)
	}

	return t / float64(len(s)-1)
}

// percentile is s's p-th quantile, interpolated between ranks as PostgreSQL's percentile_cont.
func percentile(s []float64, p float64) float64 {
	sorted := slices.Clone(s)
	slices.Sort(sorted)

	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, len(sorted)-1)

	return sorted[lo] + (pos-float64(lo))*(sorted[hi]-sorted[lo])
}

// ErrReplacementMissing refuses a replacement whose old text does not occur exactly once.
var ErrReplacementMissing = errors.New("a replacement's old text no longer occurs exactly once")

// Replacement is one exact edit of a file (ADR 0039 decision 6): Old, which occurs exactly once
// in the file, becomes New.
type Replacement struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// ApplyReplacements applies rs to content in order: ErrReplacementMissing, naming the
// replacement, when one's Old is empty or does not occur exactly once at its turn.
func ApplyReplacements(content string, rs []Replacement) (string, error) {
	for i, r := range rs {
		if r.Old == "" || strings.Count(content, r.Old) != 1 {
			return "", fmt.Errorf("replacement %d: %w", i+1, ErrReplacementMissing)
		}

		content = strings.Replace(content, r.Old, r.New, 1)
	}

	return content, nil
}

// ReverseReplacements are the replacements that undo rs: each New becomes its Old again, last
// first.
func ReverseReplacements(rs []Replacement) []Replacement {
	out := make([]Replacement, len(rs))
	for i, r := range rs {
		out[len(rs)-1-i] = Replacement{Old: r.New, New: r.Old}
	}

	return out
}

// maxDiffCells bounds the line diff's table; a larger change is one replacement.
const maxDiffCells = 4 << 20

// ReplacementsOf are the replacements that turn before into after, one for each changed run of
// lines, each with enough unchanged lines around it that its Old occurs once in before and its
// New once in after, so the replacements apply to before and their reverse to after.
func ReplacementsOf(before, after string) []Replacement {
	if before == after {
		return nil
	}

	a, b := strings.SplitAfter(before, "\n"), strings.SplitAfter(after, "\n")
	hunks := lineHunks(a, b)

	for {
		grown := false

		for i := range hunks {
			if hunks[i].unique(a, b) {
				continue
			}

			if hunks[i].grow(len(a), len(b)) {
				grown = true
			}
		}

		hunks = mergeHunks(hunks)

		if !grown {
			break
		}
	}

	out := make([]Replacement, 0, len(hunks))
	for _, h := range hunks {
		out = append(out, Replacement{Old: strings.Join(a[h.a0:h.a1], ""), New: strings.Join(b[h.b0:h.b1], "")})
	}

	return out
}

// lineHunk is a changed run of lines: a[a0:a1] became b[b0:b1].
type lineHunk struct {
	a0, a1, b0, b1 int
}

func (h lineHunk) unique(a, b []string) bool {
	old, nw := strings.Join(a[h.a0:h.a1], ""), strings.Join(b[h.b0:h.b1], "")

	return old != "" && nw != "" && strings.Count(strings.Join(a, ""), old) == 1 && strings.Count(strings.Join(b, ""), nw) == 1
}

// grow takes one more unchanged line on each side, where there is one; false when there is none.
func (h *lineHunk) grow(na, nb int) bool {
	grown := false

	if h.a0 > 0 && h.b0 > 0 {
		h.a0, h.b0, grown = h.a0-1, h.b0-1, true
	}

	if h.a1 < na && h.b1 < nb {
		h.a1, h.b1, grown = h.a1+1, h.b1+1, true
	}

	return grown
}

// mergeHunks joins hunks that overlap or touch.
func mergeHunks(hs []lineHunk) []lineHunk {
	out := []lineHunk{}

	for _, h := range hs {
		if n := len(out); n > 0 && h.a0 <= out[n-1].a1 {
			out[n-1].a1, out[n-1].b1 = max(out[n-1].a1, h.a1), max(out[n-1].b1, h.b1)

			continue
		}

		out = append(out, h)
	}

	return out
}

// lineHunks are the changed runs between a and b, by the longest common subsequence of lines
// after their common start and end.
func lineHunks(a, b []string) []lineHunk {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}

	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}

	x, y := a[pre:len(a)-suf], b[pre:len(b)-suf]
	if (len(x)+1)*(len(y)+1) > maxDiffCells {
		return []lineHunk{{pre, len(a) - suf, pre, len(b) - suf}}
	}

	// lcs[i][j] is the longest common subsequence of x[i:] and y[j:].
	lcs := make([][]int32, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(y)+1)
	}

	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	var (
		out  []lineHunk
		open *lineHunk
	)

	i, j := 0, 0
	for i < len(x) || j < len(y) {
		if i < len(x) && j < len(y) && x[i] == y[j] {
			if open != nil {
				out, open = append(out, *open), nil
			}

			i, j = i+1, j+1

			continue
		}

		if open == nil {
			open = &lineHunk{pre + i, pre + i, pre + j, pre + j}
		}

		if j < len(y) && (i == len(x) || lcs[i][j+1] >= lcs[i+1][j]) {
			j++
			open.b1 = pre + j
		} else {
			i++
			open.a1 = pre + i
		}
	}

	if open != nil {
		out = append(out, *open)
	}

	return out
}
