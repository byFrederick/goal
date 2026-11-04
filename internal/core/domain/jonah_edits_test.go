package domain_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// ruleA is the rule file the proposal tests edit.
const ruleA = "rules/a.md"

// An edit applies its replacements to the file as it is now, so an unrelated change since the
// review still applies; it fails only when an old no longer occurs exactly once, or a new file
// already exists (ADR 0039 decision 6).
func TestProposalEdits_Apply(t *testing.T) {
	t.Parallel()

	rules := "# Rules\n\n- Ask for a screenshot.\n- Run the tests.\n"
	edit := domain.ProposalEdit{Path: ruleA, Replacements: []domain.Replacement{{Old: "- Run the tests.\n", New: "- Run the tests twice.\n"}}}

	for name, tc := range map[string]struct {
		edit    domain.ProposalEdit
		content string
		present bool
		want    string
		fails   bool
	}{
		"applies":                     {edit: edit, content: rules, present: true, want: "# Rules\n\n- Ask for a screenshot.\n- Run the tests twice.\n"},
		"applies after another edit":  {edit: edit, content: "Intro\n" + rules, present: true, want: "Intro\n# Rules\n\n- Ask for a screenshot.\n- Run the tests twice.\n"},
		"old gone":                    {edit: edit, content: "# Rules\n", present: true, fails: true},
		"old twice":                   {edit: edit, content: rules + "- Run the tests.\n", present: true, fails: true},
		"file gone":                   {edit: edit, fails: true},
		"new file":                    {edit: domain.ProposalEdit{Path: "rules/b.md", Content: "x"}, want: "x"},
		"new file exists":             {edit: domain.ProposalEdit{Path: "rules/b.md", Content: "x"}, content: "y", present: true, fails: true},
		"whole file on its base":      {edit: domain.ProposalEdit{Path: ruleA, BaseSHA256: string(domain.HashBlob([]byte(rules))), Content: "x"}, content: rules, present: true, want: "x"},
		"whole file off its base":     {edit: domain.ProposalEdit{Path: ruleA, BaseSHA256: string(domain.HashBlob([]byte(rules))), Content: "x"}, content: rules + "z", present: true, fails: true},
		"whole file with a base gone": {edit: domain.ProposalEdit{Path: ruleA, BaseSHA256: string(domain.HashBlob([]byte(rules))), Content: "x"}, fails: true},
	} {
		got, err := tc.edit.Apply([]byte(tc.content), tc.present)

		switch {
		case tc.fails && !errors.Is(err, domain.ErrEditDoesNotApply):
			t.Errorf("%s: Apply = %q, %v, want ErrEditDoesNotApply", name, got, err)
		case !tc.fails && (err != nil || got != tc.want):
			t.Errorf("%s: Apply = %q, %v, want %q", name, got, err, tc.want)
		}
	}
}

// Two proposals overlap only when they edit the same file and their old spans overlap in it, or
// when either gives the file whole: only then does the newer supersede the older.
func TestProposalEdits_Overlap(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{ruleA: []byte("one\ntwo\nthree\nfour\n"), "agents/x.md": []byte("x\n")}
	reps := func(path string, olds ...string) domain.ProposalEdit {
		e := domain.ProposalEdit{Path: path}
		for _, o := range olds {
			e.Replacements = append(e.Replacements, domain.Replacement{Old: o, New: o + "!"})
		}

		return e
	}

	for name, tc := range map[string]struct {
		a, b []domain.ProposalEdit
		want bool
	}{
		"same span":            {a: []domain.ProposalEdit{reps(ruleA, "two\n")}, b: []domain.ProposalEdit{reps(ruleA, "two\n")}, want: true},
		"spans overlap":        {a: []domain.ProposalEdit{reps(ruleA, "one\ntwo\n")}, b: []domain.ProposalEdit{reps(ruleA, "two\nthree\n")}, want: true},
		"spans apart":          {a: []domain.ProposalEdit{reps(ruleA, "one\n")}, b: []domain.ProposalEdit{reps(ruleA, "four\n")}},
		"spans touch":          {a: []domain.ProposalEdit{reps(ruleA, "one\n")}, b: []domain.ProposalEdit{reps(ruleA, "two\n")}},
		"other files":          {a: []domain.ProposalEdit{reps(ruleA, "one\n")}, b: []domain.ProposalEdit{reps("agents/x.md", "x\n")}},
		"one gives it whole":   {a: []domain.ProposalEdit{{Path: ruleA, Content: "new"}}, b: []domain.ProposalEdit{reps(ruleA, "four\n")}, want: true},
		"second of two edits":  {a: []domain.ProposalEdit{reps("agents/x.md", "x\n"), reps(ruleA, "three\n")}, b: []domain.ProposalEdit{reps(ruleA, "three\nfour\n")}, want: true},
		"old not in the file":  {a: []domain.ProposalEdit{reps(ruleA, "gone\n")}, b: []domain.ProposalEdit{reps(ruleA, "two\n")}},
		"both new, same file":  {a: []domain.ProposalEdit{{Path: "rules/n.md", Content: "a"}}, b: []domain.ProposalEdit{{Path: "rules/n.md", Content: "b"}}, want: true},
		"both new, other file": {a: []domain.ProposalEdit{{Path: "rules/n.md", Content: "a"}}, b: []domain.ProposalEdit{{Path: "rules/m.md", Content: "b"}}},
	} {
		if got := domain.EditsOverlap(tc.a, tc.b, files); got != tc.want {
			t.Errorf("%s: EditsOverlap = %v, want %v", name, got, tc.want)
		}

		if got := domain.EditsOverlap(tc.b, tc.a, files); got != tc.want {
			t.Errorf("%s, reversed: EditsOverlap = %v, want %v", name, got, tc.want)
		}
	}
}

// Invisible and bidirectional characters, which have hidden instructions in rules files, are
// found wherever a proposal holds text (ADR 0039 decision 20).
func TestProposalEdits_HiddenCharacters(t *testing.T) {
	t.Parallel()

	for s, want := range map[string]bool{
		"plain text, é and 日本": false,
		"tab\tand\nnewline":    false,
		"zero\u200Bwidth":      true,
		"right\u200Fto left":   true,
		"embed\u202Aing":       true,
		"over\u202Eride":       true,
		"word\u2060joiner":     true,
		"isolate\u2066d\u2069": true,
		"bom\uFEFF":            true,
		"tag\U000E0041s":       true,
	} {
		if _, got := domain.HiddenCharacter(s); got != want {
			t.Errorf("HiddenCharacter(%q) = %v, want %v", s, got, want)
		}
	}

	p := domain.NewProposal{Title: "a", Edits: []domain.ProposalEdit{{Path: ruleA, Replacements: []domain.Replacement{{Old: "a", New: "b\u202E"}}}}}
	if _, ok := p.HiddenCharacter(); !ok {
		t.Error("a hidden character in a replacement's new is not found")
	}

	p.Edits[0].Replacements[0].New = "b"
	p.Summary = "\u200B"

	if _, ok := p.HiddenCharacter(); !ok {
		t.Error("a hidden character in the summary is not found")
	}

	p.Summary = ""
	if r, ok := p.HiddenCharacter(); ok {
		t.Errorf("a clean proposal has hidden character %U", r)
	}
}

// An edit is stored as {path, replacements} or {path, content}, never both, and reads back the
// same; an empty new file keeps its content.
func TestProposalEdits_JSON(t *testing.T) {
	t.Parallel()

	for _, e := range []domain.ProposalEdit{
		{Path: ruleA, Replacements: []domain.Replacement{{Old: "a", New: ""}}},
		{Path: "rules/b.md", Content: ""},
		{Path: "rules/c.md", Content: "x", BaseSHA256: string(domain.HashBlob([]byte("y")))},
	} {
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}

		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}

		_, reps := fields["replacements"]
		_, content := fields["content"]

		if reps == content {
			t.Errorf("%s: %s holds replacements %v and content %v, want one", e.Path, raw, reps, content)
		}

		var back domain.ProposalEdit
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}

		if back.Path != e.Path || back.Content != e.Content || back.BaseSHA256 != e.BaseSHA256 || len(back.Replacements) != len(e.Replacements) {
			t.Errorf("%s: read back %+v, want %+v", e.Path, back, e)
		}
	}
}
