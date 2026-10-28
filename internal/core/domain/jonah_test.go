package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// A proposal edits only factory.yaml, agents/, skills/, rules/ and contracts/ (ADR 0034 decision
// 4), never evals/ or a path that climbs out of its directory.
func TestProposalPathAllowed(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]bool{
		"factory.yaml":                true,
		"agents/frontend-engineer.md": true,
		"skills/deliver/SKILL.md":     true,
		"rules/evidence.md":           true,
		"contracts/change.json":       true,
		"evals/cases/a.yaml":          false,
		"evals/judges/x.md":           false,
		"agents/../evals/x.md":        false,
		"rules/./a.md":                false,
		"factory.yaml.bak":            false,
		"agents/":                     false,
		"README.md":                   false,
		"/agents/a.md":                false,
		"rules/a\nb.md":               false,
	} {
		if got := domain.ProposalPathAllowed(path); got != want {
			t.Errorf("ProposalPathAllowed(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestProposal_Check(t *testing.T) {
	t.Parallel()

	ok := domain.NewProposal{Title: "Ask for a screenshot", Files: []domain.ProposalFile{{Path: "rules/evidence.md", Content: "x"}}}
	if err := ok.Check(); err != nil {
		t.Fatalf("a valid proposal: %v", err)
	}

	for name, change := range map[string]func(p *domain.NewProposal){
		"no title":       func(p *domain.NewProposal) { p.Title = " " },
		"no files":       func(p *domain.NewProposal) { p.Files = nil },
		"evals path":     func(p *domain.NewProposal) { p.Files[0].Path = "evals/cases/a.yaml" },
		"path twice":     func(p *domain.NewProposal) { p.Files = append(p.Files, p.Files[0]) },
		"bad base hash":  func(p *domain.NewProposal) { p.Files[0].BaseSHA256 = "abc" },
		"too many bytes": func(p *domain.NewProposal) { p.Files[0].Content = strings.Repeat("x", domain.MaxProposalFilesBytes+1) },
		"nul byte":       func(p *domain.NewProposal) { p.Files[0].Content = "a\x00b" },
		"unknown metric": func(p *domain.NewProposal) { p.Estimate.Metric = "happiness" },
	} {
		p := ok
		p.Files = append([]domain.ProposalFile(nil), ok.Files...)
		change(&p)

		if err := p.Check(); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: Check = %v, want ErrInvalid", name, err)
		}
	}
}

// A review result names only its own proposals, and a decision is final: never back to
// proposed, and a rejection says why.
func TestJonahReviewResult_AndDecisionCheck(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	p := domain.NewProposal{Title: "a", Files: []domain.ProposalFile{{Path: "rules/a.md"}}}

	res := domain.JonahReviewResult{
		State: domain.JonahDone, At: at, Proposals: []domain.NewProposal{p},
		Observations: []domain.NewObservation{{Summary: "seen", Count: 1, Proposal: 2}},
	}
	if err := res.Check(); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("observation naming proposal 2 of 1: %v, want ErrInvalid", err)
	}

	res.Observations[0].Proposal = 1
	if err := res.Check(); err != nil {
		t.Errorf("valid result: %v", err)
	}

	if err := (domain.JonahReviewResult{State: domain.JonahRunning, At: at}).Check(); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("result running: %v, want ErrInvalid", err)
	}

	for name, d := range map[string]domain.ProposalDecision{
		"back to proposed":   {State: domain.ProposalProposed, By: domain.LocalUser, At: at},
		"reject, no reason":  {State: domain.ProposalRejected, RejectReason: "  ", By: domain.LocalUser, At: at},
		"no actor":           {State: domain.ProposalApplied, At: at},
		"not a state at all": {State: "maybe", By: domain.LocalUser, At: at},
	} {
		if err := d.Check(); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: Check = %v, want ErrInvalid", name, err)
		}
	}
}

// A station's sent-back rate is its rejections over the items that left it, and has no value
// when no item left it in the window.
func TestStationFlow_SentBackRate(t *testing.T) {
	t.Parallel()

	if rate, ok := (domain.StationFlow{Passed: 4, SentBack: 1}).SentBackRate(); !ok || rate != 0.25 {
		t.Errorf("1 of 4: SentBackRate = %v, %v; want 0.25, true", rate, ok)
	}

	if rate, ok := (domain.StationFlow{SentBack: 2}).SentBackRate(); ok || rate != 0 {
		t.Errorf("no item passed: SentBackRate = %v, %v; want 0, false", rate, ok)
	}
}
