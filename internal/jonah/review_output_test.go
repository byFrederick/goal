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
	t.Parallel()

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
			obs: []observation{{Summary: testsMissing, Items: []string{"#1"}, Count: 5, Proposal: new(0)}},
		},
		{
			name: "threshold review below proposeAfter", trigger: domain.JonahThreshold, drop: "threshold", watching: 1,
			obs: []observation{{Summary: testsMissing, Items: []string{"#1"}, Count: 4, Proposal: new(0)}},
		},
		{name: "threshold review with no observation", trigger: domain.JonahThreshold, drop: "threshold"},
		{
			name: "manual review below proposeAfter", trigger: domain.JonahManual, kept: true,
			obs: []observation{{Summary: testsMissing, Items: []string{"#1"}, Count: 1, Proposal: new(0)}},
		},
		{
			name: "observation of a dropped proposal", trigger: domain.JonahManual, drop: "path", watching: 1,
			edit: func(p *proposal, _ domain.SentBack) { p.Files[0].Path = "evals/cases/one.yaml" },
			obs:  []observation{{Summary: testsMissing, Items: []string{"#1"}, Count: 5, Proposal: new(0)}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newReviewWorld(t)

			// One event before the review's window, which it does not read, and one in it.
			w.sentBack(stBuild, rt0.Add(-jonah30Days-time.Hour), "an old rejection")
			w.sentBack(stBuild, rt0.Add(-time.Hour), "missed the test")

			r := w.queue(tc.trigger)
			foreign, inside := w.oldAndNew()

			p := newProposal("Ask for tests", writerPath, inside, domain.MetricSentBackRate)
			p.Files[0].Content = secret

			if tc.edit != nil {
				tc.edit(&p, foreign)
			}

			w.answer(output{Observations: nonNilObs(tc.obs), Proposals: []proposal{p}})

			if n := w.work(); n != 1 {
				t.Fatalf("Work finished %d, want 1", n)
			}

			if got := w.reviews()[0]; got.ID != r.ID || got.State != domain.JonahDone {
				t.Fatalf("review = %+v, want done", got)
			}

			if n := len(w.proposals()); (n == 1) != tc.kept {
				t.Errorf("proposals stored = %d, want kept %v", n, tc.kept)
			}

			var want []dropped
			if tc.drop != "" {
				want = []dropped{{0, tc.drop}}
			}

			if got := w.drops(r.ID); !slices.Equal(got, want) {
				t.Errorf("dropped = %+v, want %+v", got, want)
			}

			if strings.Contains(w.log.String(), secret) {
				t.Errorf("the log holds a proposal's content: %s", w.log)
			}

			w.expectWatching(len(tc.obs), tc.watching)
		})
	}
}

// oldAndNew are the two sent-back events of TestReviews_Validate: the one before the review's
// window and the one in it.
func (w *reviewWorld) oldAndNew() (domain.SentBack, domain.SentBack) {
	w.t.Helper()

	all, err := w.store.SentBackBetween(w.ctx(), reviewFactory, rt0.Add(-2*jonah30Days), rt0, 10)
	if err != nil || len(all) != 2 {
		w.t.Fatalf("SentBackBetween = %+v, %v; want 2", all, err)
	}

	return all[0], all[1]
}

// answer makes every review run succeed with out.
func (w *reviewWorld) answer(out output) {
	w.t.Helper()

	raw, err := json.Marshal(out)
	if err != nil {
		w.t.Fatal(err)
	}

	w.runner.answer = func(context.Context, domain.RunRequest) (domain.RunResult, error) { return succeeded(string(raw)), nil }
}

func (w *reviewWorld) proposals() []domain.Proposal {
	w.t.Helper()

	ps, err := w.store.Proposals(w.ctx(), ports.ProposalQuery{Factory: reviewFactory, Limit: 10})
	if err != nil {
		w.t.Fatalf("Proposals: %v", err)
	}

	return ps
}

// drops are the `jonah proposal dropped` records, each of which must name review.
func (w *reviewWorld) drops(review domain.JonahReviewID) []dropped {
	w.t.Helper()

	var out []dropped

	for _, rec := range w.logged("jonah proposal dropped") {
		index, okIndex := rec["proposal"].(float64)
		reason, okReason := rec["reason"].(string)

		if rec["review_id"] != string(review) || !okIndex || !okReason {
			w.t.Errorf("dropped record %v, want review_id %s, a proposal index and a reason", rec, review)
		}

		out = append(out, dropped{index: int(index), reason: reason})
	}

	return out
}

// expectWatching checks that n observations are stored, watching of them watching.
func (w *reviewWorld) expectWatching(n, watching int) {
	w.t.Helper()

	obs, err := w.store.Observations(w.ctx(), reviewFactory, 10)
	if err != nil || len(obs) != n {
		w.t.Fatalf("Observations = %+v, %v; want %d", obs, err, n)
	}

	got := 0

	for _, o := range obs {
		if o.State == domain.ObservationWatching {
			got++
		}
	}

	if got != watching {
		w.t.Errorf("watching observations = %d, want %d", got, watching)
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
