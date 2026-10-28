package jonah_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// dropped is one `jonah proposal dropped` record: the proposal's index and the reason.
type dropped struct {
	index  int
	reason string
}

func TestReviews_Validate(t *testing.T) {
	const secret = "the content never logged"

	cases := []struct {
		name    string
		trigger domain.JonahTrigger
		// edit changes the one proposal; obs are the observations, naming it as index 0.
		edit func(p *proposal, foreign domain.SentBack)
		obs  []observation
		// kept says whether the proposal is stored; drop is the reason it is dropped.
		kept bool
		drop string
		// watching is how many observations are stored as watching.
		watching int
	}{
		{name: "valid manual proposal", trigger: domain.JonahManual, kept: true},
		{name: "path outside the allowlist", trigger: domain.JonahManual, drop: "path", edit: func(p *proposal, _ domain.SentBack) {
			p.Files[0].Path = "evals/cases/one.yaml"
		}},
		{name: "path escaping its folder", trigger: domain.JonahManual, drop: "path", edit: func(p *proposal, _ domain.SentBack) {
			p.Files[0].Path = "agents/../evals/judge.yaml"
		}},
		{name: "base_sha256 of another content", trigger: domain.JonahManual, drop: "base_sha256", edit: func(p *proposal, _ domain.SentBack) {
			p.Files[0].BaseSHA256 = string(domain.HashBlob([]byte("an older prompt")))
		}},
		{name: "empty base_sha256 for a file the version has", trigger: domain.JonahManual, drop: "base_sha256", edit: func(p *proposal, _ domain.SentBack) {
			p.Files[0].BaseSHA256 = ""
		}},
		{name: "base_sha256 for a file the version lacks", trigger: domain.JonahManual, drop: "base_sha256", edit: func(p *proposal, _ domain.SentBack) {
			p.Files[0].Path = "rules/new.md"
		}},
		{name: "new file with an empty base_sha256", trigger: domain.JonahManual, kept: true, edit: func(p *proposal, _ domain.SentBack) {
			p.Files[0].Path, p.Files[0].BaseSHA256 = "rules/new.md", ""
		}},
		{name: "content over the size cap", trigger: domain.JonahManual, drop: "size", edit: func(p *proposal, _ domain.SentBack) {
			p.Files[0].Content = strings.Repeat("a", domain.MaxProposalFilesBytes+1)
		}},
		{name: "evidence outside the window", trigger: domain.JonahManual, drop: "evidence", edit: func(p *proposal, foreign domain.SentBack) {
			p.Evidence = append(p.Evidence, evidence{Event: idOf(foreign), Reason: foreign.Reason})
		}},
		{name: "evidence naming no event", trigger: domain.JonahManual, drop: "evidence", edit: func(p *proposal, _ domain.SentBack) {
			p.Evidence[0].Event = "999999"
		}},
		{
			name: "threshold review at proposeAfter", trigger: domain.JonahThreshold, kept: true,
			obs: []observation{{Summary: "Tests are missing", Items: []string{"#1"}, Count: 5, Proposal: ptr(0)}},
		},
		{
			name: "threshold review below proposeAfter", trigger: domain.JonahThreshold, drop: "threshold", watching: 1,
			obs: []observation{{Summary: "Tests are missing", Items: []string{"#1"}, Count: 4, Proposal: ptr(0)}},
		},
		{name: "threshold review with no observation", trigger: domain.JonahThreshold, drop: "threshold"},
		{
			name: "manual review below proposeAfter", trigger: domain.JonahManual, kept: true,
			obs: []observation{{Summary: "Tests are missing", Items: []string{"#1"}, Count: 1, Proposal: ptr(0)}},
		},
		{
			name: "observation of a dropped proposal", trigger: domain.JonahManual, drop: "path", watching: 1,
			edit: func(p *proposal, _ domain.SentBack) { p.Files[0].Path = "evals/cases/one.yaml" },
			obs:  []observation{{Summary: "Tests are missing", Items: []string{"#1"}, Count: 5, Proposal: ptr(0)}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newReviewWorld(t)

			// One event before the review's window, which it does not read, and one in it.
			w.sentBack(stBuild, rt0.Add(-jonah30Days-time.Hour), "an old rejection")
			w.sentBack(stBuild, rt0.Add(-time.Hour), "missed the test")

			r := w.queue(tc.trigger)

			all, err := w.store.SentBackBetween(w.ctx(), reviewFactory, rt0.Add(-2*jonah30Days), rt0, 10)
			if err != nil || len(all) != 2 {
				t.Fatalf("SentBackBetween = %+v, %v; want 2", all, err)
			}

			foreign, inside := all[0], all[1]

			p := newProposal("Ask for tests", writerPath, inside, domain.MetricSentBackRate)
			p.Files[0].Content = secret
			if tc.edit != nil {
				tc.edit(&p, foreign)
			}

			raw, err := json.Marshal(output{Observations: nonNilObs(tc.obs), Proposals: []proposal{p}})
			if err != nil {
				t.Fatal(err)
			}

			w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) { return succeeded(string(raw)), nil }

			if n := w.work(); n != 1 {
				t.Fatalf("Work finished %d, want 1", n)
			}

			if got := w.reviews()[0]; got.ID != r.ID || got.State != domain.JonahDone {
				t.Fatalf("review = %+v, want done", got)
			}

			ps, err := w.store.Proposals(w.ctx(), ports.ProposalQuery{Factory: reviewFactory, Limit: 10})
			if err != nil {
				t.Fatalf("Proposals: %v", err)
			}

			if kept := len(ps) == 1; kept != tc.kept {
				t.Errorf("proposals stored = %d, want kept %v", len(ps), tc.kept)
			}

			var drops []dropped
			for _, rec := range w.logged("jonah proposal dropped") {
				if rec["review_id"] != string(r.ID) {
					t.Errorf("dropped record %v names review %v, want %s", rec, rec["review_id"], r.ID)
				}

				drops = append(drops, dropped{index: int(rec["proposal"].(float64)), reason: rec["reason"].(string)})
			}

			var want []dropped
			if tc.drop != "" {
				want = []dropped{{0, tc.drop}}
			}

			if !slices.Equal(drops, want) {
				t.Errorf("dropped = %+v, want %+v", drops, want)
			}

			if strings.Contains(w.log.String(), secret) {
				t.Errorf("the log holds a proposal's content: %s", w.log)
			}

			obs, err := w.store.Observations(w.ctx(), reviewFactory, 10)
			if err != nil || len(obs) != len(tc.obs) {
				t.Fatalf("Observations = %+v, %v; want %d", obs, err, len(tc.obs))
			}

			watching := 0
			for _, o := range obs {
				if o.State == domain.ObservationWatching {
					watching++
				}
			}

			if watching != tc.watching {
				t.Errorf("watching observations = %d, want %d", watching, tc.watching)
			}
		})
	}
}

// jonah30Days is the first window's length, which the review in TestReviews_Validate reads.
const jonah30Days = 30 * 24 * time.Hour

func nonNilObs(obs []observation) []observation {
	if obs == nil {
		return []observation{}
	}

	return obs
}
