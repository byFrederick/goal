package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

func TestDraftRequest_Check(t *testing.T) {
	t.Parallel()

	asker := domain.Actor{Kind: domain.ActorUser, ID: "u-1", Name: "Ada"}
	valid := domain.NewDraftRequest{Factory: "f-1", Requester: asker, Text: "give Review a 4th reviewer"}

	tests := []struct {
		name string
		edit func(*domain.NewDraftRequest)
		ok   bool
	}{
		{"valid", func(*domain.NewDraftRequest) {}, true},
		{"with an agent", func(n *domain.NewDraftRequest) { n.Agent = "reviewer" }, true},
		{"2000 characters", func(n *domain.NewDraftRequest) { n.Text = strings.Repeat("é", domain.MaxDraftRequestText) }, true},
		{"empty text", func(n *domain.NewDraftRequest) { n.Text = "" }, false},
		{"blank text", func(n *domain.NewDraftRequest) { n.Text = "  \n " }, false},
		{"text too long", func(n *domain.NewDraftRequest) { n.Text = strings.Repeat("a", domain.MaxDraftRequestText+1) }, false},
		{"bad agent name", func(n *domain.NewDraftRequest) { n.Agent = "Reviewer!" }, false},
		{"no factory", func(n *domain.NewDraftRequest) { n.Factory = "" }, false},
		{"no requester", func(n *domain.NewDraftRequest) { n.Requester = domain.Actor{} }, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			n := valid
			tc.edit(&n)

			err := n.Check()
			if tc.ok && err != nil {
				t.Fatalf("Check = %v, want nil", err)
			}

			if !tc.ok && !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("Check = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestDraftRequest_CheckTrimsText(t *testing.T) {
	t.Parallel()

	n := domain.NewDraftRequest{Factory: "f-1", Requester: domain.LocalUser, Text: "  add a reviewer \n"}
	if err := n.Check(); err != nil {
		t.Fatal(err)
	}

	if n.Text != "add a reviewer" {
		t.Fatalf("Text = %q, want it trimmed", n.Text)
	}
}

func TestDraftRequestStatus_Check(t *testing.T) {
	t.Parallel()

	for _, s := range []domain.DraftRequestStatus{
		domain.DraftRequestQueued, domain.DraftRequestRunning, domain.DraftRequestDone, domain.DraftRequestFailed,
	} {
		if err := s.Check(); err != nil {
			t.Errorf("%s.Check = %v, want nil", s, err)
		}
	}

	for _, s := range []domain.DraftRequestStatus{"", "cancelled", "Done"} {
		if err := s.Check(); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%q.Check = %v, want ErrInvalid", s, err)
		}
	}

	if !domain.DraftRequestQueued.Open() || !domain.DraftRequestRunning.Open() || domain.DraftRequestDone.Open() || domain.DraftRequestFailed.Open() {
		t.Error("Open: want queued and running only")
	}
}

func TestDraftRequestResult_Check(t *testing.T) {
	t.Parallel()

	valid := domain.DraftRequestResult{Status: domain.DraftRequestDone, Summary: "added a reviewer", ChangedPaths: []string{"factory.yaml"}, CostUSD: 0.12, Turns: 7}

	tests := []struct {
		name string
		edit func(*domain.DraftRequestResult)
		ok   bool
	}{
		{"done", func(*domain.DraftRequestResult) {}, true},
		{"failed", func(r *domain.DraftRequestResult) { r.Status, r.Error = domain.DraftRequestFailed, "box exited" }, true},
		{"queued is not an end", func(r *domain.DraftRequestResult) { r.Status = domain.DraftRequestQueued }, false},
		{"running is not an end", func(r *domain.DraftRequestResult) { r.Status = domain.DraftRequestRunning }, false},
		{"unknown status", func(r *domain.DraftRequestResult) { r.Status = "cancelled" }, false},
		{"negative cost", func(r *domain.DraftRequestResult) { r.CostUSD = -1 }, false},
		{"negative turns", func(r *domain.DraftRequestResult) { r.Turns = -1 }, false},
		{"too many turns", func(r *domain.DraftRequestResult) { r.Turns = domain.MaxDraftRequestTurns + 1 }, false},
		{"empty path", func(r *domain.DraftRequestResult) { r.ChangedPaths = []string{""} }, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := valid
			tc.edit(&r)

			err := r.Check()
			if tc.ok && err != nil {
				t.Fatalf("Check = %v, want nil", err)
			}

			if !tc.ok && !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("Check = %v, want ErrInvalid", err)
			}
		})
	}
}
