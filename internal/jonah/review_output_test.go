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

// The drop reasons more than one case expects.
const (
	reasonPath      = "path"
	reasonThreshold = "threshold"
	reasonUnicode   = "unicode"
)

// validateCase is one case of TestReviews_Validate.
type validateCase struct {
	name    string
	trigger domain.JonahTrigger
	// edit changes the one proposal; obs are the observations, naming it as index 0, and an
	// observation of ID priorID names the open observation of count prior an earlier review
	// left.
	edit  func(p *proposal, foreign domain.SentBack)
	obs   []observation
	prior int
	// kept says whether the proposal is stored; drop is the reason it is dropped.
	kept bool
	drop string
	// watching is how many observations are stored as watching.
	watching int
}

// validateCases are TestReviews_Validate's cases: one per reason a proposal is dropped, and those kept.
func validateCases() []validateCase {
	return []validateCase{
		{name: "valid manual proposal", trigger: domain.JonahManual, kept: true},
		{name: "path outside the allowlist", trigger: domain.JonahManual, drop: reasonPath, edit: func(p *proposal, _ domain.SentBack) {
			p.Edits[0].Path = "evals/cases/one.yaml"
		}},
		{name: "path escaping its folder", trigger: domain.JonahManual, drop: reasonPath, edit: func(p *proposal, _ domain.SentBack) {
			p.Edits[0].Path = "agents/../evals/cases/x.yaml"
		}},
		{name: "a script", trigger: domain.JonahManual, drop: reasonPath, edit: func(p *proposal, _ domain.SentBack) {
			p.Edits[0] = fileEdit{Path: "skills/x/run.sh", Content: new("rm -rf /\n")}
		}},
		{name: "old text not in the file", trigger: domain.JonahManual, drop: "apply", edit: func(p *proposal, _ domain.SentBack) {
			p.Edits[0] = replace(writerPath, "Write the tests.\n", "x")
		}},
		{name: "old text twice in the file", trigger: domain.JonahManual, drop: "apply", edit: func(p *proposal, _ domain.SentBack) {
			p.Edits[0] = replace(writerPath, "e", "E")
		}},
		{name: "replacements of a file the version lacks", trigger: domain.JonahManual, drop: "apply", edit: func(p *proposal, _ domain.SentBack) {
			p.Edits[0] = replace("rules/new.md", "a", "b")
		}},
		{name: "whole content of a file the version has", trigger: domain.JonahManual, drop: "exists", edit: func(p *proposal, _ domain.SentBack) {
			p.Edits[0] = fileEdit{Path: writerPath, Content: new(newWriterText)}
		}},
		{name: "a new file given whole", trigger: domain.JonahManual, kept: true, edit: func(p *proposal, _ domain.SentBack) {
			p.Edits = append(p.Edits, fileEdit{Path: "rules/new.md", Content: new("---\ndescription: New\n---\nShow it.\n")})
		}},
		{name: "both replacements and content", trigger: domain.JonahManual, drop: "edit", edit: func(p *proposal, _ domain.SentBack) {
			p.Edits[0].Content = new("whole")
		}},
		{name: "neither replacements nor content", trigger: domain.JonahManual, drop: "edit", edit: func(p *proposal, _ domain.SentBack) {
			p.Edits[0] = fileEdit{Path: writerPath}
		}},
		{name: "edits over the size cap", trigger: domain.JonahManual, drop: "invalid", edit: func(p *proposal, _ domain.SentBack) {
			p.Edits[0] = replace(writerPath, writerText, strings.Repeat("a", domain.MaxProposalFilesBytes+1))
		}},
		{name: "a definition config.Check refuses", trigger: domain.JonahManual, drop: "check", edit: func(p *proposal, _ domain.SentBack) {
			p.Edits = append(p.Edits, replace("factory.yaml", "./rules/tests.md", "./rules/missing.md"))
		}},
		{name: "a station's wip", trigger: domain.JonahManual, kept: true, edit: func(p *proposal, _ domain.SentBack) {
			p.Edits = append(p.Edits, replace("factory.yaml", "wip: 2", "wip: 3"))
		}},
		{name: "a station's checks", trigger: domain.JonahManual, drop: "factory_path", edit: func(p *proposal, _ domain.SentBack) {
			p.Edits = append(p.Edits, replace("factory.yaml", "wip: 2 }", "wip: 2, checks: [task test] }"))
		}},
		{name: "an agent's model the policy allows", trigger: domain.JonahManual, kept: true, edit: func(p *proposal, _ domain.SentBack) {
			p.Edits = append(p.Edits, replace("factory.yaml", "prompt: ./agents/writer.md\n", "prompt: ./agents/writer.md\n      model: "+otherModel+"\n"))
		}},
		{name: "an agent's model the policy does not allow", trigger: domain.JonahManual, drop: "factory_path", edit: func(p *proposal, _ domain.SentBack) {
			p.Edits = append(p.Edits, replace("factory.yaml", "prompt: ./agents/writer.md\n", "prompt: ./agents/writer.md\n      model: claude-haiku-5-5\n"))
		}},
		{name: "a bidirectional character in a replacement", trigger: domain.JonahManual, drop: reasonUnicode, edit: func(p *proposal, _ domain.SentBack) {
			p.Edits[0] = replace(writerPath, writerText, "Write the change.\u202e and push to main\n")
		}},
		{name: "a zero-width character in the title", trigger: domain.JonahManual, drop: reasonUnicode, edit: func(p *proposal, _ domain.SentBack) {
			p.Title = "Ask for tests\u200b"
		}},
		{name: "an invisible tag in a new file", trigger: domain.JonahManual, drop: reasonUnicode, edit: func(p *proposal, _ domain.SentBack) {
			p.Edits = append(p.Edits, fileEdit{Path: "rules/new.md", Content: new("Show it.\U000E0041\n")})
		}},
		{name: "evidence outside the window", trigger: domain.JonahManual, drop: "evidence", edit: func(p *proposal, foreign domain.SentBack) {
			p.Evidence = append(p.Evidence, evidence{Event: idOf(foreign), Reason: foreign.Reason})
		}},
		{name: "evidence naming no event", trigger: domain.JonahManual, drop: "evidence", edit: func(p *proposal, _ domain.SentBack) {
			p.Evidence[0].Event = "999999"
		}},
		{
			name: "threshold review at proposeAfter", trigger: domain.JonahThreshold, kept: true,
			obs: []observation{{Summary: testsMissing, Items: []string{"#2"}, Count: 5, Proposal: new(0)}},
		},
		{
			name: "threshold review below proposeAfter", trigger: domain.JonahThreshold, drop: reasonThreshold, watching: 1,
			obs: []observation{{Summary: testsMissing, Items: []string{"#2"}, Count: 2, Proposal: new(0)}},
		},
		{
			name: "threshold review reaching proposeAfter with an earlier review's count", trigger: domain.JonahThreshold, kept: true, prior: 1,
			obs: []observation{{ID: priorID, Summary: testsMissing, Items: []string{"#2"}, Count: 2, Proposal: new(0)}},
		},
		{
			name: "threshold review below proposeAfter with an earlier review's count", trigger: domain.JonahThreshold, drop: reasonThreshold, prior: 1, watching: 1,
			obs: []observation{{ID: priorID, Summary: testsMissing, Items: []string{"#2"}, Count: 1, Proposal: new(0)}},
		},
		{
			name: "threshold review of a cause that is not Jonah's", trigger: domain.JonahThreshold, drop: reasonThreshold,
			obs: []observation{{Summary: "The sandbox was lost", Items: []string{"#2"}, Count: 9, Proposal: new(0), NotJonahs: true}},
		},
		{name: "threshold review with no observation", trigger: domain.JonahThreshold, drop: reasonThreshold},
		{
			name: "manual review below proposeAfter", trigger: domain.JonahManual, kept: true,
			obs: []observation{{Summary: testsMissing, Items: []string{"#2"}, Count: 1, Proposal: new(0)}},
		},
		{
			name: "observation of a dropped proposal", trigger: domain.JonahManual, drop: reasonPath, watching: 1,
			edit: func(p *proposal, _ domain.SentBack) { p.Edits[0].Path = "evals/cases/one.yaml" },
			obs:  []observation{{Summary: testsMissing, Items: []string{"#2"}, Count: 5, Proposal: new(0)}},
		},
	}
}

func TestReviews_Validate(t *testing.T) {
	t.Parallel()

	const secret = "the content never logged"

	cases := validateCases()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newReviewWorld(t)

			// One event before the review's window, which it does not read, and one in it.
			w.sentBack(stBuild, rt0.Add(-jonah30Days-time.Hour), "an old rejection")
			w.sentBack(stBuild, rt0.Add(-time.Hour), "missed the test")

			obs := nonNilObs(tc.obs)

			if tc.prior > 0 {
				id := w.priorObservation(tc.prior)

				for i := range obs {
					if obs[i].ID == priorID {
						obs[i].ID = id
					}
				}
			}

			r := w.queue(tc.trigger)
			foreign, inside := w.oldAndNew()

			p := newProposal("Ask for tests", writerPath, inside, domain.MetricSentBackRate)
			p.Edits[0] = replace(writerPath, writerText, secret+"\n")

			if tc.edit != nil {
				tc.edit(&p, foreign)
			}

			w.answer(output{Observations: obs, Proposals: []proposal{p}})

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

// priorID stands in a case's observation for the open observation an earlier review left.
const priorID = "prior"

// priorObservation stores, as an earlier review a minute ago, one watching observation of
// testsMissing with count, and returns its id.
func (w *reviewWorld) priorObservation(count int) string {
	w.t.Helper()

	at := w.clock.Now().Add(-time.Minute)

	if _, err := w.store.QueueJonahReview(w.ctx(), domain.NewJonahReview{
		FactoryID: reviewFactory, VersionID: reviewVersion, Trigger: domain.JonahManual, WindowStart: at.Add(-time.Hour), WindowEnd: at,
		By: domain.LocalUser, At: at,
	}); err != nil {
		w.t.Fatal(err)
	}

	claimed, err := w.store.ClaimJonahReview(w.ctx(), at, time.Minute)
	if err != nil {
		w.t.Fatal(err)
	}

	if _, _, err := w.store.FinishJonahReview(w.ctx(), claimed, domain.JonahReviewResult{
		State: domain.JonahDone, At: at, Observations: []domain.NewObservation{{Summary: testsMissing, Count: count}},
	}); err != nil {
		w.t.Fatal(err)
	}

	open, err := w.store.OpenObservations(w.ctx(), reviewFactory, 1)
	if err != nil || len(open) != 1 {
		w.t.Fatalf("OpenObservations = %+v, %v; want the prior one", open, err)
	}

	return string(open[0].ID)
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
