package domain_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// The Changed since your plan section lists each entry by its merged item or, without one, by
// its commit, with the overlapping paths; it says when the comparison was truncated, bounds each
// entry's paths and is empty with no entries.
func TestChangedSinceSection(t *testing.T) {
	t.Parallel()

	if got := domain.ChangedSinceSection("c0ffee", nil, true); got != "" {
		t.Errorf("section with no entries = %q, want empty", got)
	}

	many := make([]string, domain.MaxChangedPaths+2)
	for i := range many {
		many[i] = fmt.Sprintf("docs/%02d.md", i)
	}

	entries := []domain.ChangedSinceEntry{
		{Ref: "acme#3", Title: "Move terraform", SHA: "1111111111111111", Subject: "Move terraform (#40)", Paths: []string{"deploy/terraform/main.tf", "docs/a.md"}},
		{SHA: "2222222222222222", Subject: "Fix the lint", Paths: []string{"internal/a.go"}},
		{Ref: "acme#4", Title: "Docs", SHA: "3333333333333333", Paths: many},
	}

	got := domain.ChangedSinceSection("abcdef0123456789", entries, false)

	for _, want := range []string{
		"## Changed since your plan\n\n",
		"since abcdef012345",
		`- acme#3 "Move terraform": deploy/terraform/main.tf, docs/a.md`,
		`- commit 222222222222 "Fix the lint": internal/a.go`,
		"docs/19.md and 2 more",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("section lacks %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, "docs/20.md") || strings.Contains(got, "truncated") {
		t.Errorf("section lists past MaxChangedPaths or says truncated:\n%s", got)
	}

	if !strings.HasPrefix(got, "## Changed since your plan") {
		t.Errorf("section does not open with its heading:\n%s", got)
	}

	if !strings.Contains(domain.ChangedSinceSection("abc", entries[:1], true), "truncated") {
		t.Error("a truncated comparison's section does not say so")
	}
}
