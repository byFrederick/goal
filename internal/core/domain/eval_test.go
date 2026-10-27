package domain

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// Names the eval tests use: a case, and a definition file.
const (
	testEvalCase     = "merge-step"
	testEngineerPath = "agents/engineer.md"
	testCasePath     = "evals/cases/merge-step.yaml"
	testFactoryPath  = "factory.yaml"
)

func TestNewEvalRun_Check(t *testing.T) {
	t.Parallel()

	hash := BlobHash(strings.Repeat("a", 64))
	valid := func() NewEvalRun {
		return NewEvalRun{
			FactoryID: "00000000-0000-7000-8000-0000000000f1", ContentSHA256: hash, Trigger: EvalManual, EstimateUSD: 0.5,
			Cases: []EvalCase{{Name: testEvalCase, SHA256: hash, Tries: 3}, {Name: "spec-review", SHA256: hash, Tries: 1}},
		}
	}

	tests := []struct {
		name string
		edit func(n *NewEvalRun)
		ok   bool
	}{
		{"a valid run", func(*NewEvalRun) {}, true},
		{"on publish of a version", func(n *NewEvalRun) { n.Trigger, n.VersionID = EvalOnPublish, "00000000-0000-7000-8000-0000000000a1" }, true},
		{"the one case named", func(n *NewEvalRun) { n.Case, n.Cases = testEvalCase, n.Cases[:1] }, true},
		{"no factory", func(n *NewEvalRun) { n.FactoryID = "" }, false},
		{"bad content hash", func(n *NewEvalRun) { n.ContentSHA256 = "ABC" }, false},
		{"unknown trigger", func(n *NewEvalRun) { n.Trigger = "auto" }, false},
		{"no cases", func(n *NewEvalRun) { n.Cases = nil }, false},
		{"zero tries", func(n *NewEvalRun) { n.Cases[1].Tries = 0 }, false},
		{"duplicate case", func(n *NewEvalRun) { n.Cases[1].Name = testEvalCase }, false},
		{"bad case name", func(n *NewEvalRun) { n.Cases[0].Name = "Merge_Step" }, false},
		{"bad case hash", func(n *NewEvalRun) { n.Cases[0].SHA256 = "" }, false},
		{"negative estimate", func(n *NewEvalRun) { n.EstimateUSD = -1 }, false},
		{"one case named but several run", func(n *NewEvalRun) { n.Case = testEvalCase }, false},
		{"one case named but another runs", func(n *NewEvalRun) { n.Case, n.Cases = "spec-review", n.Cases[:1] }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			n := valid()
			tt.edit(&n)

			err := n.Check()
			if tt.ok && err != nil {
				t.Errorf("Check = %v, want nil", err)
			}

			if !tt.ok && !errors.Is(err, ErrInvalid) {
				t.Errorf("Check = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestEvalOutcome_Check(t *testing.T) {
	t.Parallel()

	for _, o := range []EvalOutcome{
		{State: EvalRunning},
		{State: EvalPending},
		{State: "skipped"},
		{State: EvalPass, CostUSD: -0.1},
		{State: EvalFail, Turns: -1},
	} {
		if err := o.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: Check = %v, want ErrInvalid", o, err)
		}
	}

	for _, s := range []EvalResultState{EvalPass, EvalFail, EvalError} {
		if err := (EvalOutcome{State: s, CostUSD: 0.2, Turns: 4}).Check(); err != nil {
			t.Errorf("%s: Check = %v, want nil", s, err)
		}
	}
}

func TestManifestSHA256(t *testing.T) {
	t.Parallel()

	a, b := BlobHash(strings.Repeat("a", 64)), BlobHash(strings.Repeat("b", 64))
	m := Manifest{testFactoryPath: a, testEngineerPath: b, testCasePath: a}

	got := ManifestSHA256(m)
	if !got.Valid() {
		t.Fatalf("ManifestSHA256 = %q, want 64 lowercase hex", got)
	}

	// The same pairs built in another order hash the same, whatever map order range gives.
	same := Manifest{}
	for _, p := range []string{testCasePath, testFactoryPath, testEngineerPath} {
		same[p] = m[p]
	}

	for range 20 {
		if again := ManifestSHA256(same); again != got {
			t.Fatalf("ManifestSHA256 = %s, then %s for the same manifest", got, again)
		}
	}

	// The pinned value: sha256 of "path\x00hash\n" per file, sorted by path.
	if want := HashBlob([]byte("agents/engineer.md\x00" + string(b) + "\nevals/cases/merge-step.yaml\x00" + string(a) +
		"\nfactory.yaml\x00" + string(a) + "\n")); got != want {
		t.Errorf("ManifestSHA256 = %s, want %s", got, want)
	}

	for name, other := range map[string]Manifest{
		"a file's content changed": {testFactoryPath: b, testEngineerPath: b, testCasePath: a},
		"a file renamed":           {testFactoryPath: a, "agents/backend.md": b, testCasePath: a},
		"a file removed":           {testFactoryPath: a, testEngineerPath: b},
		"two files swapped":        {testFactoryPath: a, testEngineerPath: a, testCasePath: b},
	} {
		if ManifestSHA256(other) == got {
			t.Errorf("%s: hash unchanged", name)
		}
	}

	if ManifestSHA256(nil) != ManifestSHA256(Manifest{}) {
		t.Error("a nil and an empty manifest hash differently")
	}
}

func TestEvalCaseSpec_Check(t *testing.T) {
	t.Parallel()

	ok := EvalCaseSpec{Name: testEvalCase, Agent: "engineer", Tries: 2, Contract: true}

	tests := []struct {
		name      string
		edit      func(*EvalCaseSpec)
		want      string
		checkedBy string
	}{
		{name: "the contract", edit: func(*EvalCaseSpec) {}, checkedBy: "contract"},
		{name: "a judge", edit: func(c *EvalCaseSpec) { c.Contract, c.Judge = false, "spec-followed" }, checkedBy: "judge:spec-followed"},
		{
			name: "a reference patch", edit: func(c *EvalCaseSpec) { c.Contract, c.ReferencePatch = false, "evals/patches/fix.diff" },
			checkedBy: "reference-patch:evals/patches/fix.diff",
		},
		{name: "no check", edit: func(c *EvalCaseSpec) { c.Contract = false }, want: "0 checks"},
		{name: "two checks", edit: func(c *EvalCaseSpec) { c.Judge = "spec-followed" }, want: "2 checks"},
		{name: "no agent", edit: func(c *EvalCaseSpec) { c.Agent = "" }, want: "no agent"},
		{name: "no tries", edit: func(c *EvalCaseSpec) { c.Tries = 0 }, want: "0 tries"},
		{name: "a bad name", edit: func(c *EvalCaseSpec) { c.Name = "Merge Step" }, want: "not a case file's name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := ok
			tt.edit(&c)

			err := c.Check()
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}

				if got := c.CheckedBy(); got != tt.checkedBy {
					t.Errorf("CheckedBy() = %q, want %q", got, tt.checkedBy)
				}

				return
			}

			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want ErrInvalid saying %q", err, tt.want)
			}
		})
	}
}

func TestPatchFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		diff string
		want []string
	}{
		{
			name: "a change",
			diff: "diff --git a/internal/lease.go b/internal/lease.go\nindex 1..2 100644\n--- a/internal/lease.go\n+++ b/internal/lease.go\n@@ -1,2 +1,2 @@\n package lease\n-var x = 1\n+var x = 2\n",
			want: []string{"internal/lease.go"},
		},
		{
			name: "a new file and a deleted one skip /dev/null",
			diff: "--- /dev/null\n+++ b/docs/new.md\n@@ -0,0 +1 @@\n+hello\n--- a/docs/old.md\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n",
			want: []string{"docs/new.md", "docs/old.md"},
		},
		{
			name: "a rename counts under both names",
			diff: "diff --git a/old/name.go b/new/name.go\nsimilarity index 100%\nrename from old/name.go\nrename to new/name.go\n",
			want: []string{"new/name.go", "old/name.go"},
		},
		{
			name: "hunk lines that look like headers are content",
			diff: "--- a/notes.md\n+++ b/notes.md\n@@ -1,2 +1,2 @@\n--- a/secret.md\n+++ b/other.md\n",
			want: []string{"notes.md"},
		},
		{
			name: "plain diff headers with timestamps",
			diff: "--- lease.go\t2026-10-01 12:00:00\n+++ lease.go\t2026-10-02 12:00:00\n@@ -1 +1 @@\n-a\n+b\n",
			want: []string{"lease.go"},
		},
		{name: "nothing", diff: "", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := PatchFiles([]byte(tt.diff)); !slices.Equal(got, tt.want) {
				t.Errorf("PatchFiles = %q, want %q", got, tt.want)
			}
		})
	}
}
