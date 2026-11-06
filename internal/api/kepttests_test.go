package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/definitions"
	"github.com/byFrederick/goal/internal/factorymodel"
)

// lineTests are the line factory's kept tests; line has one agent station, build.
const lineTests = "/v1/orgs/acme/factories/line/jonah/tests"

// notPassed is the refusal of an item that did not pass the station the first time.
const notPassed = "not-passed-first-time"

// builtItem seeds an item of line with these events at build, and a finished first run of a
// claim there (its branch head recorded) when run is set.
func (h *harness) builtItem(t *testing.T, run bool, kinds ...domain.EventKind) domain.Item {
	t.Helper()

	ctx := inOrg(t)
	it := h.seed(t, stBuild, domain.StateQueued, t0)

	for _, k := range kinds {
		e := domain.Event{FactoryID: lineID, ItemID: it.ID, At: h.clock.Now(), Kind: k, Station: stBuild}
		if k == domain.EventMoved {
			e.Data = map[string]any{"from": stBuild, "to": stMerge}
		}

		if err := h.store.AppendEvents(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	if !run {
		return it
	}

	id, err := h.store.StartRun(ctx, ports.NewRun{
		FactoryID: lineID, ItemID: it.ID, Station: stBuild, Agent: agBackend, Model: runModel, StartedAt: t0, Kind: domain.RunKindStation,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := h.store.RecordRunInput(ctx, id, domain.RunInput{Repositories: []domain.RunRepository{{BranchHead: strings.Repeat("c", 40), BaseCommit: strings.Repeat("b", 40)}}}); err != nil {
		t.Fatal(err)
	}

	if err := h.store.FinishRun(ctx, id, domain.RunResult{Status: domain.RunSucceeded}, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	return it
}

// keepBody is a keep request of item number n at station.
func keepBody(n int64, station string) string {
	return `{"item":` + strconv.FormatInt(n, 10) + `,"station":"` + station + `"}`
}

func (h *harness) keptTests(t *testing.T, path string) []gen.KeptTest {
	t.Helper()

	var list gen.KeptTestList
	if code := h.call(t, http.MethodGet, path, "", &list); code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, code)
	}

	return list.Tests
}

func TestJonah_KeptTests(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	passed := h.builtItem(t, true, domain.EventClaimed, domain.EventMoved)

	kept := h.keep(t, passed)

	h.clock.Advance(time.Minute)

	second := h.builtItem(t, true, domain.EventClaimed, domain.EventMoved)
	if code := h.call(t, http.MethodPost, lineTests, keepBody(second.Number, stBuild), nil); code != http.StatusCreated {
		t.Fatalf("keep a second item = %d, want 201", code)
	}

	if got := h.keptTests(t, lineTests); len(got) != 2 || got[0].Item.Number != second.Number || got[1].Id != kept.Id {
		t.Errorf("kept tests = %+v, want the second item's first, then %s", got, kept.Id)
	}

	code, p := problemOfCall(t, h, http.MethodPost, lineTests, keepBody(passed.Number, stBuild))
	wantProblem(t, "keep the item at build again", code, p, http.StatusConflict, "kept-test-exists")

	h.wantKeepRefusals(t)

	h.wantRemoveRefusals(t, kept.Id)

	if code := h.call(t, http.MethodDelete, lineTests+"/"+kept.Id, "", nil); code != http.StatusNoContent {
		t.Fatalf("remove = %d, want 204", code)
	}

	if got := h.keptTests(t, lineTests); len(got) != 1 || got[0].Item.Number != second.Number {
		t.Errorf("kept tests after removing = %+v, want the second item's only", got)
	}
}

// keep keeps it at build and checks the answer.
func (h *harness) keep(t *testing.T, it domain.Item) gen.KeptTest {
	t.Helper()

	var kept gen.KeptTest
	if code := h.call(t, http.MethodPost, lineTests, keepBody(it.Number, stBuild), &kept); code != http.StatusCreated {
		t.Fatalf("keep = %d, want 201", code)
	}

	if kept.Id == "" || kept.Item.Id != string(it.ID) || kept.Item.Number != it.Number || kept.Item.Title != it.Title ||
		kept.Station != stBuild || kept.KeptBy == "" || !kept.KeptAt.Equal(h.clock.Now()) || kept.LastResult != nil {
		t.Errorf("kept = %+v, want item %d at %s, kept now, never run", kept, it.Number, stBuild)
	}

	return kept
}

// wantRemoveRefusals checks that line's test id cannot be removed through acme, nor an unknown one.
func (h *harness) wantRemoveRefusals(t *testing.T, id string) {
	t.Helper()

	code, p := problemOfCall(t, h, http.MethodDelete, acmeJonah+"/tests/"+id, "")
	wantProblem(t, "remove line's test through acme", code, p, http.StatusNotFound, "kept-test-not-found")

	code, p = problemOfCall(t, h, http.MethodDelete, lineTests+"/019a0000-0000-7000-8000-00000000dead", "")
	wantProblem(t, "remove an unknown test", code, p, http.StatusNotFound, "kept-test-not-found")
}

// wantKeepRefusals checks the keeps that are refused, none of them stored.
func (h *harness) wantKeepRefusals(t *testing.T) {
	t.Helper()

	passed := h.builtItem(t, true, domain.EventClaimed, domain.EventMoved)
	rejected := h.builtItem(t, true, domain.EventClaimed, domain.EventGateRejected, domain.EventMoved)
	twice := h.builtItem(t, true, domain.EventClaimed, domain.EventMoved, domain.EventClaimed, domain.EventMoved)
	notMoved := h.builtItem(t, true, domain.EventClaimed)
	noRun := h.builtItem(t, false, domain.EventClaimed, domain.EventMoved)

	for _, c := range []struct {
		what, body string
		status     int
		code       string
	}{
		{"an unknown item", keepBody(9999, stBuild), http.StatusNotFound, "item-not-found"},
		{"a human station", keepBody(passed.Number, stMerge), http.StatusBadRequest, ""},
		{"a station not in the line", keepBody(passed.Number, "nowhere"), http.StatusBadRequest, ""},
		{"an item a gate rejected", keepBody(rejected.Number, stBuild), http.StatusConflict, notPassed},
		{"an item claimed there twice", keepBody(twice.Number, stBuild), http.StatusConflict, notPassed},
		{"an item not moved on", keepBody(notMoved.Number, stBuild), http.StatusConflict, notPassed},
		{"an item with no finished first run", keepBody(noRun.Number, stBuild), http.StatusConflict, notPassed},
	} {
		code, p := problemOfCall(t, h, http.MethodPost, lineTests, c.body)
		wantProblem(t, "keep "+c.what, code, p, c.status, c.code)
	}

	if got := h.keptTests(t, lineTests); len(got) != 2 {
		t.Errorf("after refusals %d kept tests, want the 2 kept", len(got))
	}
}

// tryLines is jonah.TryDefinitions over the definitions cache, as goald's: a change's line is its
// version with the edits applied, and the stations it reaches factorymodel.StationsReached's.
type tryLines struct{ c *definitions.Cache }

func (d tryLines) CurrentLines(ctx context.Context) ([]domain.Line, error) {
	return d.c.CurrentLines(ctx)
}

func (d tryLines) ProposedLine(ctx context.Context, v domain.DefinitionVersionID, edits []domain.ProposalEdit) (domain.Line, []string, error) {
	before, err := d.c.VersionFiles(ctx, v)
	if err != nil {
		return domain.Line{}, nil, err
	}

	p, err := d.c.Proposed(ctx, v, edits)
	if err != nil {
		return domain.Line{}, nil, err
	}

	after, err := p.VersionFiles(ctx, v)
	if err != nil {
		return domain.Line{}, nil, err
	}

	snap := func(files map[string][]byte) factorymodel.Snapshot {
		m := make(domain.Manifest, len(files))
		for path, b := range files {
			m[path] = domain.HashBlob(b)
		}

		return factorymodel.Snapshot{Manifest: m, Files: config.Files(files)}
	}

	return onSecrets(p.Line()), factorymodel.StationsReached(snap(before), snap(after)), nil
}

func (d tryLines) ValidateProposed(ctx context.Context, v domain.DefinitionVersionID, edits []domain.ProposalEdit, contract string,
	output json.RawMessage,
) error {
	p, err := d.c.Proposed(ctx, v, edits)
	if err != nil {
		return err
	}

	return p.Validate(ctx, v, contract, output)
}

func (d tryLines) ContentKey(ctx context.Context, v domain.DefinitionVersionID, edits []domain.ProposalEdit) (string, error) {
	p, err := d.c.Proposed(ctx, v, edits)
	if err != nil {
		return "", err
	}

	files, err := p.VersionFiles(ctx, v)
	if err != nil {
		return "", err
	}

	return domain.ContentKey(files), nil
}

// lineDraftKeptTests is line's open draft's kept tests.
const lineDraftKeptTests = "/v1/orgs/acme/factories/line/draft/kept-tests"

// backendDraft is the backend prompt line's draft gives: a change that reaches build.
const backendDraft = "# backend, test first\n"

// draftBackend opens line's draft and writes backendDraft as the backend prompt; it returns
// domain.ContentKey of the draft's files.
func (h *harness) draftBackend(t *testing.T) string {
	t.Helper()

	if r := h.request(t, http.MethodPost, "/v1/orgs/acme/factories/line/draft", `{}`, nil); r.status != http.StatusCreated {
		t.Fatalf("open line's draft: %d %s", r.status, r.body)
	}

	path := "/v1/orgs/acme/factories/line/draft/files/agents%2Fbackend.md"

	file := h.request(t, http.MethodGet, path, "", nil)
	if file.status != http.StatusOK {
		t.Fatalf("read the backend prompt: %d %s", file.status, file.body)
	}

	decode[gen.SaveResult](t, h.request(t, http.MethodPut, path, backendDraft, http.Header{ifMatchHeader: {file.header.Get("ETag")}}), http.StatusOK)

	files := map[string][]byte{}
	for p, c := range lineFiles {
		files[p] = []byte(c)
	}

	files["agents/backend.md"] = []byte(backendDraft)

	return domain.ContentKey(files)
}

func (h *harness) draftKeptTests(t *testing.T, method string) gen.DraftKeptTests {
	t.Helper()

	var out gen.DraftKeptTests
	if code := h.call(t, method, lineDraftKeptTests, "", &out); code != http.StatusOK {
		t.Fatalf("%s %s = %d, want 200", method, lineDraftKeptTests, code)
	}

	return out
}

// The publish dialog's kept tests (ADR 0039 decision 12): GET starts nothing; POST queues a
// publish's try of each kept test the draft reaches, and again queues nothing; a test a proposal
// with the draft's content tried reuses that try. A test's last result is its newest finished try.
func TestJonah_DraftKeptTestsStartAndReuse(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx := inOrg(t)

	code, p := problemOfCall(t, h, http.MethodPost, lineDraftKeptTests, "")
	wantProblem(t, "kept tests without a draft", code, p, http.StatusNotFound, "no-draft")

	h.doneLineItem(t, 2, 998) // the line spent $1,000: Jonah's budget is $50

	fresh := h.keep(t, h.builtItem(t, true, domain.EventClaimed, domain.EventMoved))
	h.clock.Advance(time.Minute)
	tried := h.keep(t, h.builtItem(t, true, domain.EventClaimed, domain.EventMoved))
	key := h.draftBackend(t)

	// A proposal with exactly the draft's content tried the second test, and it passed.
	h.triedKeptTest(t, tried, key)

	if got := h.draftKeptTests(t, http.MethodGet); got.State != gen.DraftKeptTestsStateDone || len(got.Tests) != 2 || resultOf(got, fresh.Id) != "" {
		t.Fatalf("GET = %+v, want both tests, the fresh one not started", got)
	}

	got := h.draftKeptTests(t, http.MethodPost)
	if got.State != gen.DraftKeptTestsStateTrying || resultOf(got, fresh.Id) != gen.KeptTestResultRunning || resultOf(got, tried.Id) != gen.KeptTestResultPassed {
		t.Fatalf("POST = %+v, want the fresh test running and the tried one passed", got)
	}

	h.draftKeptTests(t, http.MethodPost)

	tries, err := h.store.KeptTestTries(ctx, lineID, key)
	if err != nil || len(tries) != 2 {
		t.Fatalf("tries of the draft's content = %+v, %v; want the proposal's and one publish's", tries, err)
	}

	if pub := tries[1]; pub.ProposalID != "" || pub.KeptTestID != domain.KeptTestID(fresh.Id) || len(pub.Edits) != 1 || pub.Edits[0].Content != backendDraft {
		t.Errorf("publish try = %+v, want the fresh test's, with no proposal and the draft's prompt", pub)
	}

	h.wantLastResults(t, tried.Id, fresh.Id)
}

// wantLastResults fails t unless line's kept test passed has last result passed and running has
// none yet.
func (h *harness) wantLastResults(t *testing.T, passed, running string) {
	t.Helper()

	for _, k := range h.keptTests(t, lineTests) {
		switch {
		case k.Id == passed && (k.LastResult == nil || k.LastResult.Result != gen.KeptTestLastResultResultPassed):
			t.Errorf("last result of the tried test = %+v, want passed", k.LastResult)
		case k.Id == running && k.LastResult != nil:
			t.Errorf("last result of a test whose try runs = %+v, want null", k.LastResult)
		}
	}
}

// triedKeptTest queues and finishes, passed, a proposal's try of k on content key.
func (h *harness) triedKeptTest(t *testing.T, k gen.KeptTest, key string) {
	t.Helper()

	ctx := inOrg(t)
	now := h.clock.Now()

	if _, err := h.store.QueueJonahReview(ctx, domain.NewJonahReview{
		FactoryID: lineID, VersionID: h.lineRecord(t).Current.ID, Trigger: domain.JonahManual, WindowStart: now.Add(-time.Hour), WindowEnd: now,
		By: domain.LocalUser, At: now,
	}); err != nil {
		t.Fatal(err)
	}

	rv, err := h.store.ClaimJonahReview(ctx, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	_, ps, err := h.store.FinishJonahReview(ctx, rv, domain.JonahReviewResult{State: domain.JonahDone, At: now, Proposals: []domain.NewProposal{{
		Title: "Test first", Edits: []domain.ProposalEdit{{Path: "agents/backend.md", Replacements: []domain.Replacement{{Old: "# backend\n", New: backendDraft}}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	run, err := h.store.PastTryTarget(ctx, domain.ItemID(k.Item.Id), stBuild)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.store.QueuePastTries(ctx, lineID, ps[0].ID, []domain.NewPastTry{{
		ProposalID: ps[0].ID, KeptTestID: domain.KeptTestID(k.Id), ContentKey: key, ItemID: domain.ItemID(k.Item.Id), Station: stBuild,
		OriginalRunID: run.ID, Model: runModel,
	}}, h.clock.Now()); err != nil {
		t.Fatal(err)
	}

	c, err := h.store.ClaimPastTry(ctx, lineID, h.clock.Now(), time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.store.FinishPastTry(ctx, c, domain.PastTryResult{Verdict: domain.PastTryFixed, Reason: "it passed", At: h.clock.Now()}); err != nil {
		t.Fatal(err)
	}
}

// resultOf is test id's result in d, "" for null.
func resultOf(d gen.DraftKeptTests, id string) gen.KeptTestResult {
	for _, e := range d.Tests {
		if e.Test.Id == id && e.Result != nil {
			return *e.Result
		}
	}

	return ""
}
