package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

const (
	judgeFile = "evals/judges/answer-clear.md"
	caseFile  = "evals/cases/clear-answer.yaml"
	patchFile = "evals/fixtures/clear-answer.patch"
)

// validJudge scores baseFactory's one agent. Rows mark the line a problem must point at with
// "#here", which is a YAML comment in the front matter.
const validJudge = `---
title: Answer is clear
criterion: The answer says what changed in plain words
agents: [worker]
outcomes:
  - {score: 1, label: Clear, meaning: A newcomer understands it}
  - {score: 0, label: Unclear, meaning: It needs a second reading}
passMark: 0.7
sample: 0.25
model: claude-sonnet-5-5
enabled: true
---
Read the run's output and pick one outcome.
`

// validCase runs baseFactory's one agent and is checked by validJudge.
const validCase = `apiVersion: goal/v1
kind: EvalCase
metadata:
  description: A short request gets a clear answer
spec:
  agent: worker
  input:
    title: Add a version flag
    body: The CLI prints its version.
  tries: 3
  check:
    judge: answer-clear
`

// replaced applies old → new replacements to s, each once.
func replaced(s string, pairs ...string) string {
	for i := 0; i+1 < len(pairs); i += 2 {
		s = strings.Replace(s, pairs[i], pairs[i+1], 1)
	}

	return s
}

// evalRow is one row of TestCheck_Judges and TestCheck_EvalCases: the files besides baseFactory
// and the problem they must produce in file, "" for none.
type evalRow struct {
	name   string
	files  map[string]string
	code   string
	file   string
	noLine bool // a problem about the file as a whole, or the parser's own line
}

func runEvalRows(t *testing.T, rows []evalRow) {
	t.Helper()

	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, problems := Check(checkTree(baseFactory, tt.files))

			if tt.code == "" {
				if len(problems) > 0 {
					t.Fatalf("problems = %+v, want none", problems)
				}

				return
			}

			wantOneProblem(t, tt, problems)
		})
	}
}

// wantOneProblem fails unless problems hold exactly one of the row's code in its file, located
// at the row's marked line.
func wantOneProblem(t *testing.T, tt evalRow, problems []domain.DefinitionProblem) {
	t.Helper()

	var found []domain.DefinitionProblem

	for _, p := range problems {
		if p.Code == tt.code && p.File == tt.file {
			found = append(found, p)
		}
	}

	if len(found) != 1 {
		t.Fatalf("want one %s problem in %s, got %d in %+v", tt.code, tt.file, len(found), problems)
	}

	p := found[0]
	if p.Severity != domain.SeverityError || p.Section != domain.SectionFiles || p.Check == "" || p.Message == "" {
		t.Errorf("problem = %+v, want an error in section files with a check and a message", p)
	}

	switch want := markedLine(tt.files[tt.file]); {
	case tt.noLine:
	case want == 0:
		t.Fatalf("row %s marks no line in %s", tt.name, tt.file)
	case p.Line != want:
		t.Errorf("problem line = %d, want %d: %+v", p.Line, want, p)
	}
}

func TestCheck_Judges(t *testing.T) {
	t.Parallel()

	judge := func(body string) map[string]string { return map[string]string{judgeFile: body} }

	runEvalRows(t, []evalRow{
		{name: "valid judge", files: judge(validJudge)},
		{name: "empty agents means all agents", files: judge(replaced(validJudge, "agents: [worker]", "agents: []"))},
		{name: "agents omitted", files: judge(replaced(validJudge, "agents: [worker]\n", ""))},
		{name: "a judge without model or enabled", files: judge(replaced(validJudge, "model: claude-sonnet-5-5\n", "", "enabled: true\n", ""))},
		{
			name:  "judge names an unknown agent",
			files: judge(replaced(validJudge, "agents: [worker]", "agents: [worker, ghost] #here")),
			code:  codeUnknownAgent, file: judgeFile,
		},
		{
			name: "empty outcomes",
			files: judge(replaced(validJudge,
				"outcomes:\n  - {score: 1, label: Clear, meaning: A newcomer understands it}\n  - {score: 0, label: Unclear, meaning: It needs a second reading}",
				"outcomes: [] #here")),
			code: codeSchemaInvalid, file: judgeFile,
		},
		{
			name:  "duplicate outcome score",
			files: judge(replaced(validJudge, "{score: 0, label: Unclear,", "{score: 1, label: Unclear, #here\n    ")),
			code:  codeJudgeOutcomeDuplicate, file: judgeFile,
		},
		{
			name:  "duplicate outcome label",
			files: judge(replaced(validJudge, "{score: 0, label: Unclear,", "{score: 0, label: Clear, #here\n    ")),
			code:  codeJudgeOutcomeDuplicate, file: judgeFile,
		},
		{
			name:  "outcome score above 1",
			files: judge(replaced(validJudge, "{score: 1, label: Clear,", "{score: 2, label: Clear, #here\n    ")),
			code:  codeSchemaInvalid, file: judgeFile,
		},
		{
			name:  "passMark above 1",
			files: judge(replaced(validJudge, "passMark: 0.7", "passMark: 1.5 #here")),
			code:  codeSchemaInvalid, file: judgeFile,
		},
		{
			name:  "passMark below 0",
			files: judge(replaced(validJudge, "passMark: 0.7", "passMark: -0.1 #here")),
			code:  codeSchemaInvalid, file: judgeFile,
		},
		{
			name:  "sample above 1",
			files: judge(replaced(validJudge, "sample: 0.25", "sample: 25 #here")),
			code:  codeSchemaInvalid, file: judgeFile,
		},
		{
			name:  "sample below 0",
			files: judge(replaced(validJudge, "sample: 0.25", "sample: -1 #here")),
			code:  codeSchemaInvalid, file: judgeFile,
		},
		{name: "sample 0 pauses the judge", files: judge(replaced(validJudge, "sample: 0.25", "sample: 0"))},
		{
			name:  "no front matter",
			files: judge("Read the run's output and pick one outcome. #here\n"),
			code:  codeJudgeFrontMissing, file: judgeFile,
		},
		{
			name:  "empty front matter",
			files: judge("---\n---\nRead the run's output. #here\n"),
			code:  codeJudgeFrontMissing, file: judgeFile, noLine: true,
		},
		{
			name:  "unknown key",
			files: judge(replaced(validJudge, "sample: 0.25\n", "sample: 0.25\nweight: 2 #here\n")),
			code:  codeSchemaUnknownKey, file: judgeFile,
		},
		{
			name:  "required key missing",
			files: judge(replaced(validJudge, "criterion: The answer says what changed in plain words\n", "")),
			code:  codeSchemaRequired, file: judgeFile, noLine: true,
		},
		{
			name:  "front matter does not parse",
			files: judge(replaced(validJudge, "agents: [worker]", "agents: [worker")),
			code:  codeYAMLInvalid, file: judgeFile, noLine: true,
		},
		{
			name:  "file name is not a slug",
			files: map[string]string{"evals/judges/Answer_Clear.md": validJudge},
			code:  codeEvalNameInvalid, file: "evals/judges/Answer_Clear.md", noLine: true,
		},
	})
}

func TestCheck_EvalCases(t *testing.T) {
	t.Parallel()

	withCase := func(body string, more ...string) map[string]string {
		files := map[string]string{judgeFile: validJudge, caseFile: body}
		for i := 0; i+1 < len(more); i += 2 {
			files[more[i]] = more[i+1]
		}

		return files
	}

	byPatch := replaced(validCase, "    judge: answer-clear", "    referencePatch: ./"+patchFile)

	runEvalRows(t, []evalRow{
		{name: "valid case checked by a judge", files: withCase(validCase)},
		{name: "valid case checked by the contract", files: withCase(replaced(validCase, "judge: answer-clear", "contract: true"))},
		{name: "valid case checked by a reference patch", files: withCase(byPatch, patchFile, "diff --git a/x b/x\n")},
		{
			name: "case with every input", files: withCase(replaced(validCase, "    body: The CLI prints its version.\n",
				"    body: The CLI prints its version.\n    type: feature\n    lane: backend\n    outputs: {spec: {title: x}}\n    ref: 3f2a9c1\n")),
		},
		{
			name:  "case names an unknown agent",
			files: withCase(replaced(validCase, "agent: worker", "agent: ghost #here")),
			code:  codeUnknownAgent, file: caseFile,
		},
		{
			name:  "unknown judge",
			files: withCase(replaced(validCase, "judge: answer-clear", "judge: answer-short #here")),
			code:  codeUnknownJudge, file: caseFile,
		},
		{
			name:  "missing reference patch",
			files: withCase(replaced(byPatch, "referencePatch: ./"+patchFile, "referencePatch: ./"+patchFile+" #here")),
			code:  codePatchNotFound, file: caseFile,
		},
		{
			name: "reference patch outside the tree",
			files: withCase(replaced(validCase, "    judge: answer-clear", "    referencePatch: ./../outside.patch #here"),
				"outside.patch", "diff\n"),
			code: codePatchNotFound, file: caseFile,
		},
		{
			name:  "two checks",
			files: withCase(replaced(validCase, "  check:\n", "  check: #here\n    contract: true\n")),
			code:  codeSchemaInvalid, file: caseFile,
		},
		{
			name:  "no tries",
			files: withCase(replaced(validCase, "tries: 3", "tries: 0 #here")),
			code:  codeSchemaInvalid, file: caseFile,
		},
		{
			name:  "the Evals kind is gone",
			files: withCase(replaced(validCase, "kind: EvalCase", "kind: Evals #here")),
			code:  codeSchemaEnum, file: caseFile,
		},
		{
			name:  "unknown key",
			files: withCase(replaced(validCase, "  tries: 3\n", "  tries: 3\n  split: tune #here\n")),
			code:  codeSchemaUnknownKey, file: caseFile,
		},
		{
			name:  "required key missing",
			files: withCase(replaced(validCase, "  agent: worker\n", "")),
			code:  codeSchemaRequired, file: caseFile, noLine: true,
		},
		{
			name:  "file name is not a slug",
			files: map[string]string{judgeFile: validJudge, "evals/cases/Clear_Answer.yaml": validCase},
			code:  codeEvalNameInvalid, file: "evals/cases/Clear_Answer.yaml", noLine: true,
		},
		{
			// otherFileProblems reports it; the case check adds nothing.
			name:  "case does not parse",
			files: withCase(replaced(validCase, "agent: worker", "agent: [worker")),
			code:  codeYAMLInvalid, file: caseFile, noLine: true,
		},
	})
}

// caseAgent is the agent of baseFactory that validCase runs.
const caseAgent = "worker"

func TestEvalCases_ParsesEveryCase(t *testing.T) {
	t.Parallel()

	contract := replaced(validCase, "judge: answer-clear", "contract: true", "tries: 3", "tries: 1",
		"    body: The CLI prints its version.\n", "    body: The CLI prints its version.\n    type: feature\n    lane: backend\n    ref: abc123\n    outputs:\n      spec: {title: Version}\n")
	patch := replaced(validCase, "judge: answer-clear", "referencePatch: ./"+patchFile)

	files := checkTree(baseFactory, map[string]string{
		judgeFile:                   validJudge,
		caseFile:                    validCase,
		"evals/cases/contract.yaml": contract,
		"evals/cases/by-patch.yml":  patch,
		"evals/cases/.hidden.yaml":  validCase,
		patchFile:                   "--- a/x\n+++ b/x\n",
	})

	got, err := EvalCases(files)
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.EvalCaseSpec{
		{
			Name: "by-patch", SHA256: domain.HashBlob([]byte(patch)), Agent: caseAgent, Description: "A short request gets a clear answer",
			Input: domain.EvalInput{Title: "Add a version flag", Body: "The CLI prints its version."}, Tries: 3, ReferencePatch: "./" + patchFile,
		},
		{
			Name: "clear-answer", SHA256: domain.HashBlob([]byte(validCase)), Agent: caseAgent, Description: "A short request gets a clear answer",
			Input: domain.EvalInput{Title: "Add a version flag", Body: "The CLI prints its version."}, Tries: 3, Judge: "answer-clear",
		},
		{
			Name: "contract", SHA256: domain.HashBlob([]byte(contract)), Agent: caseAgent, Description: "A short request gets a clear answer",
			Input: domain.EvalInput{
				Title: "Add a version flag", Body: "The CLI prints its version.", Type: "feature", Lane: laneBackend, Ref: "abc123",
				Outputs: map[string]any{"spec": map[string]any{"title": "Version"}},
			},
			Tries: 1, Contract: true,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("EvalCases =\n%+v\nwant\n%+v", got, want)
	}

	for _, c := range got {
		if err := c.Check(); err != nil {
			t.Errorf("case %s: %v", c.Name, err)
		}
	}

	none, err := EvalCases(checkTree(baseFactory, nil))
	if err != nil || len(none) != 0 {
		t.Errorf("EvalCases of a definition without cases = %v, %v; want none", none, err)
	}

	if _, err := EvalCases(checkTree(baseFactory, map[string]string{caseFile: "spec: [unclosed"})); err == nil {
		t.Error("EvalCases of a case that does not parse: no error")
	}
}
