package gateway

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The stations of a try's past item: the one it reruns and the one before it.
const (
	tryStation = "build"
	tryEarlier = "spec"
)

// The commits a try's original run recorded.
var (
	tryHead = strings.Repeat("c", 40)
	tryBase = strings.Repeat("b", 40)
)

// pastTry records a past item of h's factory whose build run recorded a brief and its heads, after
// a spec run with an output, and a proposal of the engineer's prompt queued for a try of it.
func (h *harness) pastTry(t *testing.T) (domain.Item, domain.PastTry) {
	t.Helper()

	return h.pastTryOn(t, []domain.RunRepository{{Repo: "github.com/acme/api", BranchHead: tryHead, BaseCommit: tryBase}})
}

// pastTryOn is pastTry with the original run's recorded repositories.
func (h *harness) pastTryOn(t *testing.T, repos []domain.RunRepository) (domain.Item, domain.PastTry) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), h.org)
	it := h.submit(t, factoryID, "Add a health endpoint", "")
	now := h.clock.Now()

	run := func(station string, at time.Time, in domain.RunInput, output string) domain.RunID {
		id, err := h.store.StartRun(ctx, ports.NewRun{FactoryID: factoryID, ItemID: it.ID, Station: station, Agent: agentEngineer, StartedAt: at})
		if err != nil {
			t.Fatal(err)
		}

		if err := h.store.RecordRunInput(ctx, id, in); err != nil {
			t.Fatal(err)
		}

		if err := h.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded, Output: json.RawMessage(output)}, at.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}

		return id
	}

	run(tryEarlier, now.Add(-3*time.Hour), domain.RunInput{}, `{"spec":"add /health"}`)
	orig := run(tryStation, now.Add(-2*time.Hour), domain.RunInput{Brief: "the recorded brief", Repositories: repos}, `{"pr":1}`)

	_, err := h.store.QueueJonahReview(ctx, domain.NewJonahReview{
		FactoryID: factoryID, VersionID: version, Trigger: domain.JonahManual, WindowStart: now.Add(-time.Hour), WindowEnd: now,
		By: domain.LocalUser, At: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	rv, err := h.store.ClaimJonahReview(ctx, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	_, ps, err := h.store.FinishJonahReview(ctx, rv, domain.JonahReviewResult{State: domain.JonahDone, At: now, Proposals: []domain.NewProposal{{
		Title: "Test first", Edits: []domain.ProposalEdit{{Path: engineerPrompt, Replacements: []domain.Replacement{{Old: "# engineer\n", New: "# engineer, test first\n"}}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	tries, err := h.store.QueuePastTries(ctx, factoryID, ps[0].ID, []domain.NewPastTry{{
		ProposalID: ps[0].ID, ItemID: it.ID, Station: tryStation, OriginalRunID: orig, Model: "claude-sonnet-5-5",
	}}, now)
	if err != nil {
		t.Fatal(err)
	}

	return it, tries[0]
}

// tryClaims are a try token's claims: the try, its station and agent, no item and no connection.
func (h *harness) tryClaims(try domain.PastTry) domain.TaskClaims {
	c := h.claims()
	c.Kind, c.TryOf, c.ItemID, c.Connections = domain.TaskKindTry, try.ID, "", nil

	return c
}

// A try's bundle (ADR 0039 decision 9) is the station's on the version with the proposal's edits,
// from the original run's recorded brief, the item's earlier outputs and its commit and base
// commit, with the login that clones the repository. Its environment keeps its setup, tools and
// env names, marked setup only, and drops its files and MCP servers; it has no connection, and
// serving it records nothing on the past item.
func TestBundle_TryGetsRecordedBriefAndNoEnvironment(t *testing.T) {
	t.Parallel()

	yaml := strings.Replace(secretsBundleYAML("https://github.com/acme/api.git", ""),
		"{ name: engineer, prompt: ./agents/engineer.md }", "{ name: engineer, prompt: ./agents/engineer.md, environment: ci-box }", 1)
	files := maps.Clone(secretsBundleFiles)
	files["environments/ci-box.yaml"] = `apiVersion: goal/v1
kind: Environment
spec:
  tools: [node@22]
  setup: [pnpm install --frozen-lockfile]
  env: {NPM_TOKEN: "{{npm-token}}"}
  files:
    ~/.npmrc: "{{npmrc}}"
  mcp:
    linear:
      url: https://mcp.linear.example.com/mcp
`

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: yaml, files: files})
	it, try := h.pastTry(t)
	before := len(h.store.Events(it.ID))

	status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, h.tryClaims(try)))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var b runBundle
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}

	if b.Spec.Brief != "the recorded brief" || string(b.Spec.Inputs[tryEarlier]) != `{"spec":"add /health"}` || len(b.Spec.Inputs) != 1 {
		t.Errorf("brief %q, inputs %s; want the recorded brief and the spec run's output alone", b.Spec.Brief, b.Spec.Inputs)
	}

	if got := string(b.Files["agent/prompt.md"]); got != "# engineer, test first\n" {
		t.Errorf("prompt %q; want the proposal's", got)
	}

	expectTryBox(t, b)

	if n := len(h.store.Events(it.ID)); n != before {
		t.Errorf("%d events of the past item, want %d: a try records nothing on it", n, before)
	}
}

// A try clones the run's first recorded repository with a branch head at its commits (ADR 0041
// decision 17); a row recorded without a repository (before 00131) is the version's. A run with
// no branch head, or whose repository the version has not, is refused.
func TestBundle_TryRecordedRepositories(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		repos []domain.RunRepository
		ok    bool
	}{
		"backfilled row":     {[]domain.RunRepository{{BranchHead: tryHead, BaseCommit: tryBase}}, true},
		"first with a head":  {[]domain.RunRepository{{Repo: "github.com/acme/api", PushedHead: tryBase}, {BranchHead: tryHead, BaseCommit: tryBase}}, true},
		"no branch head":     {[]domain.RunRepository{{Repo: "github.com/acme/api", BaseCommit: tryBase}}, false},
		"another repository": {[]domain.RunRepository{{Repo: "github.com/acme/web", BranchHead: tryHead, BaseCommit: tryBase}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newHarnessWith(t, Config{}, harnessDeps{yaml: secretsBundleYAML("https://github.com/acme/api.git", ""), files: secretsBundleFiles})
			_, try := h.pastTryOn(t, c.repos)

			status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, h.tryClaims(try)))
			if !c.ok {
				if status != http.StatusForbidden {
					t.Errorf("status = %d, want 403 (%s)", status, body)
				}

				return
			}

			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", status, body)
			}

			var b runBundle
			if err := json.Unmarshal(body, &b); err != nil {
				t.Fatal(err)
			}

			if b.Repository == nil || b.Repository.URL != "https://github.com/acme/api.git" {
				t.Errorf("repository %+v; want the version's", b.Repository)
			}

			expectTryRepository(t, b)
		})
	}
}

// A try token of another station or agent than its try's is refused, and its notes are none.
func TestBundle_TryOfAnotherStationForbidden(t *testing.T) {
	t.Parallel()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: secretsBundleYAML("https://github.com/acme/api.git", ""), files: secretsBundleFiles})
	_, try := h.pastTry(t)

	c := h.tryClaims(try)
	c.Station = tryEarlier

	if status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c)); status != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (%s)", status, body)
	}

	status, _, body := request(t, http.MethodGet, h.runServer(t)+"?notes_after=0", h.token(t, h.tryClaims(try)))
	if status != http.StatusOK || !strings.Contains(string(body), `"notes":[]`) {
		t.Errorf("notes = %d %s; want 200 with none", status, body)
	}
}

// expectTryBox fails the test unless b's environment keeps its setup and env names, marked setup
// only, without files or MCP servers, b has no connection, granted call or knowledge, and its
// repository is at the recorded commits with the repository token's login to clone it.
func expectTryBox(t *testing.T, b runBundle) {
	t.Helper()

	e := b.Spec.Environment
	if e == nil || !e.SetupOnly || len(e.Files) != 0 || len(e.MCPServers) != 0 || !slices.Equal(e.Env, []string{"NPM_TOKEN"}) ||
		!slices.Equal(e.Setup, []string{"pnpm install --frozen-lockfile"}) {
		t.Errorf("environment %+v; want its setup and env names, setup only, without files or MCP servers", e)
	}

	if len(b.Spec.Connections) != 0 || len(b.Spec.GrantedCalls) != 0 || b.Spec.Knowledge != nil {
		t.Errorf("connections %+v, granted calls %+v, knowledge %+v; want none", b.Spec.Connections, b.Spec.GrantedCalls, b.Spec.Knowledge)
	}

	expectTryRepository(t, b)
}

// expectTryRepository fails the test unless b's repository is at the recorded commits with the
// repository token's login to clone it.
func expectTryRepository(t *testing.T, b runBundle) {
	t.Helper()

	r := b.Repository
	if r == nil || r.Commit != tryHead || r.BaseCommit != tryBase || len(b.Git) != 1 || !b.Git[0].Store {
		t.Errorf("repository %+v with logins %+v; want the recorded commits and the repository token's login", r, b.Git)
	}
}

// A publish's try of a kept test (ADR 0039 decision 12) has no proposal: its bundle is the
// station's on its own version with its own edits, the draft's files; a token of another version
// is refused.
func TestBundle_TryOfAPublishGetsTheDraftsFiles(t *testing.T) {
	t.Parallel()

	h := newHarnessWith(t, Config{}, harnessDeps{yaml: secretsBundleYAML("https://github.com/acme/api.git", ""), files: secretsBundleFiles})
	_, proposed := h.pastTry(t)
	ctx := domain.WithOrg(t.Context(), h.org)

	k, err := h.store.KeepTest(ctx, domain.NewKeptTest{FactoryID: factoryID, ItemID: proposed.ItemID, Station: tryStation, KeptBy: domain.LocalUser, KeptAt: h.clock.Now()})
	if err != nil {
		t.Fatal(err)
	}

	edits := []domain.ProposalEdit{{Path: engineerPrompt, Content: "# engineer, from the draft\n", BaseSHA256: string(domain.HashBlob([]byte("# engineer\n")))}}

	tries, err := h.store.QueuePublishTries(ctx, factoryID, version, edits, []domain.NewPastTry{{
		KeptTestID: k.ID, ContentKey: "key", ItemID: proposed.ItemID, Station: tryStation, OriginalRunID: proposed.OriginalRunID, Model: proposed.Model,
	}}, h.clock.Now())
	if err != nil {
		t.Fatal(err)
	}

	status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, h.tryClaims(tries[0])))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var b runBundle
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}

	if got := string(b.Files["agent/prompt.md"]); got != "# engineer, from the draft\n" {
		t.Errorf("prompt %q; want the draft's", got)
	}

	other := h.tryClaims(tries[0])
	other.VersionID = "another-version"

	if status, _, _ := request(t, http.MethodGet, h.runServer(t), h.token(t, other)); status != http.StatusForbidden {
		t.Errorf("status of a token of another version = %d, want 403", status)
	}
}
