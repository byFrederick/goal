package domain

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const stationTriage = "triage"

// The section lists each entry with its ref, title, state, station and paths, ranked by how many
// of its globs' literal prefixes the request names, then by most recently updated; entries
// without paths come last.
func TestInFlightSection(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	request := "Move terraform/ and the docs/design/ docs"

	entries := []InFlightEntry{
		{Ref: "acme#1", Title: "No paths, newest", State: StateQueued, Station: stationTriage, UpdatedAt: at.Add(5 * time.Hour)},
		{Ref: "acme#2", Title: "One overlap, older", State: StateInProgress, Station: stationBuild, Paths: []string{"terraform/**", "cmd/**"}, UpdatedAt: at},
		{Ref: "acme#3", Title: "Two overlaps", State: StateAssembly, Station: "plan", Paths: []string{"terraform/*.tf", "docs/design/**"}, UpdatedAt: at},
		{Ref: "acme#4", Title: "One overlap, newer", State: StateBlocked, Station: stationBuild, Paths: []string{"docs/design/**"}, UpdatedAt: at.Add(time.Hour)},
		{Ref: "acme#5", Title: "No overlap, wildcard first", State: StateReady, Station: stationBuild, Paths: []string{"**/*.go", "{a,b}/x"}, UpdatedAt: at.Add(2 * time.Hour)},
	}

	got := InFlightSection(request, entries)

	want := strings.Join([]string{
		"## In flight",
		"",
		"The factory's other open items and assemblies, with the paths each expects to change, ranked by how many of their paths this item names (5 of 5):",
		"",
		`- acme#3 "Two overlaps": assembly at plan, paths terraform/*.tf, docs/design/**`,
		`- acme#4 "One overlap, newer": blocked at build, paths docs/design/**`,
		`- acme#2 "One overlap, older": in_progress at build, paths terraform/**, cmd/**`,
		`- acme#5 "No overlap, wildcard first": ready at build, paths **/*.go, {a,b}/x`,
		`- acme#1 "No paths, newest": queued at triage, no paths known`,
	}, "\n")

	if got != want {
		t.Errorf("section =\n%s\nwant\n%s", got, want)
	}
}

// The section holds at most MaxInFlight entries, the best ranked, and says how many there were.
func TestInFlightSection_BoundedToMaxInFlight(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	entries := make([]InFlightEntry, 0, MaxInFlight+5)

	for i := range MaxInFlight + 5 {
		entries = append(entries, InFlightEntry{
			Ref: fmt.Sprintf("acme#%d", i+1), Title: "t", State: StateQueued, Station: stationTriage,
			Paths: []string{fmt.Sprintf("dir%d/**", i)}, UpdatedAt: at.Add(time.Duration(i) * time.Minute),
		})
	}

	got := InFlightSection("touches dir0/ only", entries)

	if n := strings.Count(got, "\n- "); n != MaxInFlight {
		t.Errorf("section lists %d entries, want %d:\n%s", n, MaxInFlight, got)
	}

	if !strings.Contains(got, fmt.Sprintf("(%d of %d)", MaxInFlight, MaxInFlight+5)) {
		t.Errorf("section does not say it lists %d of %d:\n%s", MaxInFlight, MaxInFlight+5, got)
	}

	if first := strings.SplitN(got, "\n- ", 2)[1]; !strings.HasPrefix(first, "acme#1 ") {
		t.Errorf("first entry = %q, want acme#1, the only one that overlaps", first)
	}

	// The oldest entries without overlap are the ones left out.
	if strings.Contains(got, "acme#2 ") || !strings.Contains(got, fmt.Sprintf("acme#%d ", MaxInFlight+5)) {
		t.Errorf("section kept the wrong entries:\n%s", got)
	}
}

// With nothing else open the section says so.
func TestInFlightSection_NothingOpen(t *testing.T) {
	t.Parallel()

	want := "## In flight\n\nNo other item or assembly of the factory is open."
	if got := InFlightSection("anything", nil); got != want {
		t.Errorf("section = %q, want %q", got, want)
	}
}
