package domain

import (
	"errors"
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
