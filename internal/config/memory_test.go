package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// memoryEntry is one memory.yaml entry with text of n characters.
func memoryEntry(n int) string {
	return fmt.Sprintf("- text: %s\n  from: [sre#225]\n  added: 2026-11-01\n", strings.Repeat("a", n))
}

// padded is body with a comment that makes it exactly size bytes.
func padded(t *testing.T, body string, size int) string {
	t.Helper()

	pad := size - len(body) - len("#\n")
	if pad < 0 {
		t.Fatalf("body is %d bytes, more than %d", len(body), size)
	}

	return body + "#" + strings.Repeat("x", pad) + "\n"
}

// memoryProblems are the problems of baseFactory with memory.yaml as written.
func memoryProblems(body string) (*Factory, []domain.DefinitionProblem) {
	return Check(checkTree(baseFactory, map[string]string{MemoryFile: body}))
}

// memory.yaml is a list of short facts with the items they came from and the date they were
// added; a definition without one has no factory memory.
func TestCheck_FactoryMemory(t *testing.T) {
	t.Parallel()

	t.Run("absent", memoryAbsent)

	t.Run("loads", memoryLoads)

	t.Run("an entry over 300 characters", memoryEntryTooLong)

	t.Run("more than 50 entries", memoryTooManyEntries)

	t.Run("more than 8 KB", memoryTooManyBytes)

	t.Run("what the schema refuses", memorySchemaRefuses)
}

func memoryAbsent(t *testing.T) {
	t.Parallel()

	f, problems := Check(checkTree(baseFactory, nil))
	if f == nil || len(problems) > 0 || f.Memory() != nil {
		t.Fatalf("problems = %+v, memory = %+v; want none", problems, f.Memory())
	}
}

func memoryLoads(t *testing.T) {
	t.Parallel()

	f, problems := memoryProblems("- text: staging-db disk alerts below 85% are noise; the nightly vacuum clears them.\n" +
		"  from: [sre#225, sre#231]\n  added: 2026-11-01\n- text: Deploys freeze on Fridays.\n  added: \"2026-11-02\"\n")
	if f == nil || len(problems) > 0 {
		t.Fatalf("problems = %+v, want none", problems)
	}

	want := []MemoryEntry{
		{Text: "staging-db disk alerts below 85% are noise; the nightly vacuum clears them.", From: []string{"sre#225", "sre#231"}, Added: "2026-11-01"},
		{Text: "Deploys freeze on Fridays.", Added: "2026-11-02"},
	}
	if got := f.Memory(); !reflect.DeepEqual(got, want) {
		t.Errorf("memory = %+v\nwant %+v", got, want)
	}

	if _, err := Load(checkTree(baseFactory, map[string]string{MemoryFile: memoryEntry(10)})); err != nil {
		t.Errorf("Load: %v", err)
	}
}

func memoryEntryTooLong(t *testing.T) {
	t.Parallel()

	if _, problems := memoryProblems(memoryEntry(10) + memoryEntry(domain.MaxMemoryText)); len(problems) > 0 {
		t.Errorf("300 characters: problems = %+v, want none", problems)
	}

	_, problems := memoryProblems(memoryEntry(10) + memoryEntry(domain.MaxMemoryText+1))

	want := domain.DefinitionProblem{Code: codeMemoryTooLong, Severity: domain.SeverityError, Section: domain.SectionFiles, File: MemoryFile, Line: 4}
	if i := indexOf(problems, codeMemoryTooLong); i < 0 || len(problems) != 1 {
		t.Fatalf("problems = %+v, want one memory-too-long", problems)
	} else if p := problems[i]; p.Severity != want.Severity || p.Section != want.Section || p.File != want.File || p.Line != want.Line || p.Check == "" {
		t.Errorf("problem = %+v, want an error in %s at line %d", p, want.File, want.Line)
	}
}

func memoryTooManyEntries(t *testing.T) {
	t.Parallel()

	fifty := strings.Repeat(memoryEntry(10), domain.MaxMemoryEntries)
	if _, problems := memoryProblems(fifty); len(problems) > 0 {
		t.Errorf("50 entries: problems = %+v, want none", problems)
	}

	_, problems := memoryProblems(fifty + memoryEntry(10))
	if i := indexOf(problems, codeMemoryTooBig); i < 0 || len(problems) != 1 || problems[i].File != MemoryFile || problems[i].Severity != domain.SeverityError {
		t.Errorf("51 entries: problems = %+v, want one memory-too-big error in %s", problems, MemoryFile)
	}
}

func memoryTooManyBytes(t *testing.T) {
	t.Parallel()

	body := strings.Repeat(memoryEntry(200), 30)
	if _, problems := memoryProblems(padded(t, body, domain.MaxMemoryBytes)); len(problems) > 0 {
		t.Errorf("8 KB: problems = %+v, want none", problems)
	}

	_, problems := memoryProblems(padded(t, body, domain.MaxMemoryBytes+1))
	if i := indexOf(problems, codeMemoryTooBig); i < 0 || len(problems) != 1 || problems[i].File != MemoryFile || problems[i].Severity != domain.SeverityError {
		t.Errorf("8 KB and a byte: problems = %+v, want one memory-too-big error in %s", problems, MemoryFile)
	}
}

func memorySchemaRefuses(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		"text: not a list\n",
		"- from: [sre#225]\n  added: 2026-11-01\n",
		"- text: a fact\n",
		"- text: a fact\n  added: yesterday\n",
		"- text: a fact\n  from: [not-an-item]\n  added: 2026-11-01\n",
		"- text: a fact\n  added: 2026-11-01\n  colour: red\n",
	} {
		f, problems := memoryProblems(body)
		if len(problems) == 0 || problems[0].File != MemoryFile || problems[0].Severity != domain.SeverityError || f.Memory() != nil {
			t.Errorf("%q: problems = %+v, want an error in %s and no memory", body, problems, MemoryFile)
		}
	}
}
