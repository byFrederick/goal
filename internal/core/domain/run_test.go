package domain

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseVerdict_RejectionNeedsAReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   Decision
		bad    bool
	}{
		{name: "approve", output: `{"decision":"approve"}`, want: Approve},
		{name: "reject with reason", output: `{"decision":"reject","reason":"no evidence"}`, want: Reject},
		{name: "reject without reason", output: `{"decision":"reject"}`, bad: true},
		{name: "unknown decision", output: `{"decision":"maybe"}`, bad: true},
		{name: "not json", output: `ok`, bad: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v, err := ParseVerdict(json.RawMessage(tt.output))
			if tt.bad {
				if !errors.Is(err, ErrBadVerdict) {
					t.Fatalf("err = %v, want ErrBadVerdict", err)
				}

				return
			}

			if err != nil || v.Decision != tt.want {
				t.Fatalf("ParseVerdict = %+v, %v; want %s", v, err, tt.want)
			}
		})
	}
}

// A reviewer's contract may give a category; the verdict keeps it for the agent statistics.
func TestParseVerdict_KeepsTheCategory(t *testing.T) {
	t.Parallel()

	v, err := ParseVerdict(json.RawMessage(`{"decision":"reject","reason":"no links","category":"Claims without evidence"}`))
	if err != nil || v.Category != catEvidence {
		t.Fatalf("ParseVerdict = %+v, %v; want the category kept", v, err)
	}
}

// PathsFrom reads the output field a definition maps to the item's paths; an unmapped field, a
// missing or empty one and one that is not a list of strings set none.
func TestPathsFrom(t *testing.T) {
	t.Parallel()

	const field = "touches"

	tests := []struct {
		name, field, output string
		want                []string
	}{
		{"mapped", field, `{"touches":["internal/a/**","docs/a.md"]}`, []string{"internal/a/**", "docs/a.md"}},
		{"blank entries dropped", field, `{"touches":["", "internal/a/**"]}`, []string{"internal/a/**"}},
		{"unmapped", "", `{"touches":["internal/a/**"]}`, nil},
		{"absent", field, `{"lane":"backend"}`, nil},
		{"empty", field, `{"touches":[]}`, nil},
		{"only blanks", field, `{"touches":[""]}`, nil},
		{"not a list", field, `{"touches":"internal/a/**"}`, nil},
		{"not strings", field, `{"touches":[1,2]}`, nil},
		{"not an object", field, `[1]`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := PathsFrom(json.RawMessage(tt.output), tt.field)
			if !slices.Equal(got, tt.want) || (got == nil) != (tt.want == nil) {
				t.Errorf("PathsFrom(%s, %q) = %#v, want %#v", tt.output, tt.field, got, tt.want)
			}
		})
	}
}

// A run gets its model from a connection or from a key, never both, and a run on a key opens no
// connection; a Jonah run has no environment.
func TestRunRequest_Check(t *testing.T) {
	t.Parallel()

	station := RunRequest{OrgID: "org", FactoryID: "factory", Agent: "engineer", Item: "item", Station: "work"}

	tests := []struct {
		name   string
		change func(*RunRequest)
		ok     bool
	}{
		{name: "a model connection", change: func(r *RunRequest) { r.ModelConnection = testModelConn }, ok: true},
		{name: "a model key", change: func(r *RunRequest) {
			r.ModelKey, r.Environment = testModelKey, Environment{Name: DefaultEnvironment}
		}, ok: true},
		{name: "no model", change: func(*RunRequest) {}},
		{name: "both", change: func(r *RunRequest) { r.ModelKey, r.ModelConnection = testModelKey, testModelConn }},
		{name: "a key and a connection", change: func(r *RunRequest) { r.ModelKey, r.Connections = testModelKey, []string{"issues"} }},
		{name: "a key and a repository connection", change: func(r *RunRequest) {
			r.ModelKey, r.Repository = testModelKey, Repository{URL: "https://example.com/acme.git", Connection: "code"}
		}},
		{name: "a Jonah run with an environment", change: func(r *RunRequest) {
			r.Kind, r.Item, r.Station = RunKindJonah, "", ""
			r.ModelKey, r.Environment = testModelKey, Environment{Name: DefaultEnvironment}
		}},
		{name: "a Jonah run on a key", change: func(r *RunRequest) {
			r.Kind, r.Item, r.Station, r.ModelKey = RunKindJonah, "", "", testModelKey
		}, ok: true},
	}

	for _, tt := range tests {
		r := station
		tt.change(&r)

		err := r.Check()
		if tt.ok != (err == nil) {
			t.Errorf("%s: Check = %v; want ok %v", tt.name, err, tt.ok)
		}

		if err != nil && !errors.Is(err, ErrBadRunRequest) {
			t.Errorf("%s: Check = %v; want ErrBadRunRequest", tt.name, err)
		}
	}
}

// A try of a past item (ADR 0039 decision 9) reruns a station on no item, under no lease, with no
// connection but the model's, from its recorded commit; only a try names a try or a commit.
func TestRunRequest_CheckTry(t *testing.T) {
	t.Parallel()

	try := RunRequest{
		OrgID: "org-1", FactoryID: "factory", Agent: "engineer", Station: "work", Kind: RunKindTry, Bundle: TaskKindTry,
		ModelKey: testModelKey, TryOf: "try-1", Commit: strings.Repeat("a", 40),
		Repository: Repository{URL: "https://example.com/acme.git"},
	}

	tests := []struct {
		name   string
		change func(*RunRequest)
		ok     bool
	}{
		{name: "a try", change: func(*RunRequest) {}, ok: true},
		{name: "with its station's checks", change: func(r *RunRequest) { r.Checks = []string{"go test ./..."} }, ok: true},
		{name: "a station run with checks", change: func(r *RunRequest) {
			r.Kind, r.Item, r.Bundle, r.TryOf, r.Commit, r.Checks = RunKindStation, "item-1", "", "", "", []string{"go test ./..."}
		}},
		{name: "on a model connection", change: func(r *RunRequest) {
			r.ModelKey, r.ModelConnection, r.Repository.Connection = "", testModelConn, "code"
		}, ok: true},
		{name: "with an item", change: func(r *RunRequest) { r.Item = "item" }},
		{name: "with a lease", change: func(r *RunRequest) { r.Lease = &Lease{} }},
		{name: "with a connection", change: func(r *RunRequest) {
			r.ModelKey, r.ModelConnection, r.Connections = "", testModelConn, []string{"issues"}
		}},
		{name: "without a recorded commit", change: func(r *RunRequest) { r.Commit = "" }},
		{name: "without a repository", change: func(r *RunRequest) { r.Repository = Repository{} }},
		{name: "without its try", change: func(r *RunRequest) { r.TryOf = "" }},
		{name: "without a station", change: func(r *RunRequest) { r.Station = "" }},
		{name: "on a station bundle", change: func(r *RunRequest) { r.Bundle = "" }},
		{name: "a station run naming a try", change: func(r *RunRequest) { r.Kind, r.Item, r.Bundle = RunKindStation, "item", "" }},
	}

	for _, tt := range tests {
		r := try
		tt.change(&r)

		err := r.Check()
		if tt.ok != (err == nil) {
			t.Errorf("%s: Check = %v; want ok %v", tt.name, err, tt.ok)
		}

		if err != nil && !errors.Is(err, ErrBadRunRequest) {
			t.Errorf("%s: Check = %v; want ErrBadRunRequest", tt.name, err)
		}
	}
}

// A grade run (ADR 0039 decision 10) is a run of kind try on a grade bundle: it names its try,
// the result it grades, its station and the model, and nothing a try's agent works with.
func TestRunRequest_CheckGrade(t *testing.T) {
	t.Parallel()

	grade := RunRequest{
		OrgID: "org-1", FactoryID: "factory", Agent: "engineer", Station: "work", Kind: RunKindTry, Bundle: TaskKindGrade,
		ModelKey: testModelKey, TryOf: "try-1", GradeOf: GradeTry,
	}

	tests := []struct {
		name   string
		change func(*RunRequest)
		ok     bool
	}{
		{name: "a grade", change: func(*RunRequest) {}, ok: true},
		{name: "of the final result", change: func(r *RunRequest) { r.GradeOf = GradeFinal }, ok: true},
		{name: "on a model connection", change: func(r *RunRequest) { r.ModelKey, r.ModelConnection = "", testModelConn }, ok: true},
		{name: "with an item", change: func(r *RunRequest) { r.Item = "item" }},
		{name: "with a lease", change: func(r *RunRequest) { r.Lease = &Lease{} }},
		{name: "with a connection", change: func(r *RunRequest) {
			r.ModelKey, r.ModelConnection, r.Connections = "", testModelConn, []string{"issues"}
		}},
		{name: "with a repository", change: func(r *RunRequest) { r.Repository = Repository{URL: "https://example.com/acme.git"} }},
		{name: "with a commit", change: func(r *RunRequest) { r.Commit = strings.Repeat("a", 40) }},
		{name: "with checks", change: func(r *RunRequest) { r.Checks = []string{"go test ./..."} }},
		{name: "with an environment", change: func(r *RunRequest) { r.Environment = Environment{Name: "default"} }},
		{name: "without its try", change: func(r *RunRequest) { r.TryOf = "" }},
		{name: "without a result", change: func(r *RunRequest) { r.GradeOf = "" }},
		{name: "with an unknown result", change: func(r *RunRequest) { r.GradeOf = "other" }},
		{name: "without a station", change: func(r *RunRequest) { r.Station = "" }},
		{name: "without a model", change: func(r *RunRequest) { r.Model = "" }},
		{name: "a try naming a result", change: func(r *RunRequest) {
			r.Bundle, r.Commit, r.Repository = TaskKindTry, strings.Repeat("a", 40), Repository{URL: "https://example.com/acme.git"}
		}},
		{name: "a station run naming a result", change: func(r *RunRequest) { r.Kind, r.Item, r.Bundle, r.TryOf = RunKindStation, "item", "", "" }},
		{name: "a jonah run naming a result", change: func(r *RunRequest) { r.Kind, r.Station, r.Bundle, r.TryOf = RunKindJonah, "", "", "" }},
	}

	for _, tt := range tests {
		r := grade
		r.Model = "claude-haiku-5-5"
		tt.change(&r)

		err := r.Check()
		if tt.ok != (err == nil) {
			t.Errorf("%s: Check = %v; want ok %v", tt.name, err, tt.ok)
		}

		if err != nil && !errors.Is(err, ErrBadRunRequest) {
			t.Errorf("%s: Check = %v; want ErrBadRunRequest", tt.name, err)
		}
	}
}
