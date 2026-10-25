package authoring_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/domain"
)

// noBuildYAML is acmeYAML with the build station replaced by a code station.
var noBuildYAML = strings.Replace(acmeYAML, "id: build,", "id: code,", 1)

// buildToCode moves the items of the removed build station to code.
func buildToCode() map[string]string { return map[string]string{"build": "code"} }

func (h *harness) itemAt(t *testing.T, f domain.FactoryID, station string, state domain.ItemState) domain.Item {
	t.Helper()

	it, err := h.items.CreateItem(h.ctx, domain.NewItem{
		FactoryID: f, Type: "feature", Title: "An item", Station: station, State: state, CreatedAt: h.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

func TestPublish_FirstVersion(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.imported(t, acmeFiles(acmeYAML))

	start := false
	r := h.publishRequest(t, f)
	r.Start, r.Message = &start, "First version"

	v, err := h.svc.Publish(h.ctx, f, r)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	rec, err := h.svc.Factory(h.ctx, string(f))
	if err != nil {
		t.Fatal(err)
	}

	if v.Number != 1 || v.Message != "First version" || rec.Current == nil || rec.Current.ID != v.ID || !rec.Paused() {
		t.Errorf("published %+v, factory %+v; want v1 current and paused", v, rec)
	}
}

func TestPublish_RefusesAStaleRevisionAndAMovedCurrent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)
	h.edit(t, f, strings.Replace(acmeYAML, "wip: 1", "wip: 2", 1))

	r := h.publishRequest(t, f)

	stale := r
	stale.Revision--

	if _, err := h.svc.Publish(h.ctx, f, stale); !errors.Is(err, authoring.ErrDraftConflict) {
		t.Errorf("publish of a stale revision = %v", err)
	}

	moved := r
	moved.ExpectedCurrent = ""

	if _, err := h.svc.Publish(h.ctx, f, moved); authoring.ProblemCode(err) != authoring.CodeBaseMoved {
		t.Errorf("publish against a moved current = %v", err)
	}

	v, err := h.svc.Publish(h.ctx, f, r)
	if err != nil || v.Number != 2 {
		t.Fatalf("publish = %+v, %v", v, err)
	}
}

func TestPublish_RefusesErrorsButNotWarnings(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.imported(t, acmeFiles(strings.Replace(acmeYAML, "agent: engineer", "agent: nobody", 1)))

	_, err := h.svc.Publish(h.ctx, f, h.publishRequest(t, f))

	invalid, ok := errors.AsType[*authoring.InvalidError](err)
	if !ok || !slices.Contains(codes(invalid.Problems), "unknown-agent") || authoring.ProblemCode(err) != authoring.CodeDefinitionInvalid {
		t.Fatalf("publish with an unknown agent = %v", err)
	}

	// A built-in group set to ask is a warning only: it is served as off.
	warned := strings.Replace(acmeYAML, "      prompt: ./agents/engineer.md\n", "      prompt: ./agents/engineer.md\n      builtins: { read: ask }\n", 1)

	if _, err := h.svc.PutFile(h.ctx, f, authoring.FileWrite{Path: yamlFile, Content: []byte(warned)}); err != nil {
		t.Fatal(err)
	}

	val, err := h.svc.ValidateDraft(h.ctx, f, authoring.ValidateRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if !val.OK || !slices.Contains(codes(val.Problems), "builtin-ask-unavailable") {
		t.Fatalf("validation = ok %v, problems %v", val.OK, codes(val.Problems))
	}

	if _, err := h.svc.Publish(h.ctx, f, h.publishRequest(t, f)); err != nil {
		t.Errorf("publish with warnings only = %v", err)
	}
}

func TestPublish_RemovedStationsNeedMoves(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)
	queued := h.itemAt(t, f, "build", domain.StateQueued)
	h.edit(t, f, noBuildYAML)

	val, err := h.svc.ValidateDraft(h.ctx, f, authoring.ValidateRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if val.OK {
		t.Fatal("a draft removing a station with waiting items validates")
	}

	assertMoveFixes(t, val.Problems)

	moved, err := h.svc.ValidateDraft(h.ctx, f, authoring.ValidateRequest{StationMoves: buildToCode()})
	if err != nil || !moved.OK {
		t.Errorf("validation with the move = ok %v, %v (problems %v)", moved.OK, err, codes(moved.Problems))
	}

	if _, err := h.svc.Publish(h.ctx, f, h.publishRequest(t, f)); !errors.Is(err, authoring.ErrDefinitionInvalid) {
		t.Errorf("publish without a move = %v", err)
	}

	r := h.publishRequest(t, f)
	r.StationMoves = map[string]string{"build": "nowhere"}

	if _, err := h.svc.Publish(h.ctx, f, r); !errors.Is(err, authoring.ErrDefinitionInvalid) {
		t.Errorf("publish moving to a station the line lacks = %v", err)
	}

	r.StationMoves = buildToCode()

	if _, err := h.svc.Publish(h.ctx, f, r); err != nil {
		t.Fatalf("publish with the move: %v", err)
	}

	it, err := h.items.Item(h.ctx, queued.ID)
	if err != nil {
		t.Fatal(err)
	}

	if it.Station != "code" || it.State != domain.StateQueued {
		t.Errorf("moved item at %s, %s; want code, queued", it.Station, it.State)
	}
}

// assertMoveFixes requires the items problem to offer a move to each station of the new line,
// as a patch on the publish request.
func assertMoveFixes(t *testing.T, problems []domain.DefinitionProblem) {
	t.Helper()

	p, ok := problem(problems, authoring.CodeItemsAtRemovedStation)
	if !ok || len(p.Fixes) != 4 {
		t.Fatalf("items problem = %+v (problems %v), want 4 fixes", p, codes(problems))
	}

	fix := p.Fixes[1]
	got := []string{fix.Label, fix.Section, fix.Patch[0].Op, fix.Patch[0].Path, string(fix.Patch[0].Value)}
	want := []string{"Move them to code", authoring.FixSectionPublish, domain.PatchAdd, "/station_moves/build", `"code"`}

	if !slices.Equal(got, want) {
		t.Errorf("fix = %v, want %v", got, want)
	}
}

func TestPublish_ItemsInProgressAtARemovedStationBlock(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)
	h.itemAt(t, f, "build", domain.StateInProgress)
	h.edit(t, f, noBuildYAML)

	r := h.publishRequest(t, f)
	r.StationMoves = buildToCode()

	_, err := h.svc.Publish(h.ctx, f, r)

	invalid, ok := errors.AsType[*authoring.InvalidError](err)
	if !ok {
		t.Fatalf("publish = %v, want an InvalidError", err)
	}

	p, _ := problem(invalid.Problems, authoring.CodeItemsAtRemovedStation)
	if len(p.Fixes) != 0 || !strings.Contains(p.Message, "in progress") {
		t.Errorf("problem = %+v", p)
	}
}

func TestRestore_MovesThePointerBack(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)
	h.edit(t, f, strings.Replace(acmeYAML, "wip: 1", "wip: 2", 1))
	v2 := h.publish(t, f)
	v1 := authoring.VersionSelector{Kind: authoring.SelectNumber, Number: 1}

	if _, err := h.svc.Restore(h.ctx, f, authoring.RestoreRequest{Version: v1}); !errors.Is(err, authoring.ErrBaseMoved) {
		t.Errorf("restore against a stale current = %v", err)
	}

	rec, err := h.svc.Restore(h.ctx, f, authoring.RestoreRequest{Version: v1, ExpectedCurrent: v2.ID})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	if rec.Current == nil || rec.Current.Number != 1 {
		t.Fatalf("current after restore = %+v", rec.Current)
	}

	if rec.LastVersionNumber != 2 {
		t.Errorf("last number = %d, want 2: numbers are not reused", rec.LastVersionNumber)
	}

	page, err := h.svc.Versions(h.ctx, f, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	running := []bool{}
	for _, v := range page.Versions {
		running = append(running, v.Current)
	}

	if !slices.Equal(running, []bool{false, true}) {
		t.Errorf("history running flags (v2, v1) = %v", running)
	}

	_, err = h.svc.Restore(h.ctx, f, authoring.RestoreRequest{
		Version: authoring.VersionSelector{Kind: authoring.SelectNumber, Number: 9}, ExpectedCurrent: rec.Current.ID,
	})
	if !errors.Is(err, authoring.ErrVersionNotFound) {
		t.Errorf("restore of a missing version = %v", err)
	}
}

func TestOpenDraft_AfterARestoreStartsFromTheRestoredVersion(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)
	h.edit(t, f, strings.Replace(acmeYAML, "wip: 1", "wip: 2", 1))
	v2 := h.publish(t, f)

	if _, err := h.svc.Restore(h.ctx, f, authoring.RestoreRequest{
		Version: authoring.VersionSelector{Kind: authoring.SelectNumber, Number: 1}, ExpectedCurrent: v2.ID,
	}); err != nil {
		t.Fatal(err)
	}

	d, _, err := h.svc.OpenDraft(h.ctx, f, authoring.OpenDraftRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if got := [2]int{d.NextNumber, d.Base.Number}; got != [2]int{3, 1} {
		t.Errorf("draft of v%d based on v%d, want v3 based on v1", got[0], got[1])
	}
}

func TestVersions_Pages(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.publishedThree(t)

	page, err := h.svc.Versions(h.ctx, f, 0, 2)
	if err != nil {
		t.Fatal(err)
	}

	if got := pageNumbers(page); !slices.Equal(got, []int{3, 2, 2}) {
		t.Errorf("first page numbers and next = %v, want [3 2 2]", got)
	}

	if c := page.Versions[0].Counts; c.Stations != 4 || c.Agents != 1 {
		t.Errorf("counts = %+v", c)
	}

	page, err = h.svc.Versions(h.ctx, f, page.NextBefore, 2)
	if err != nil {
		t.Fatal(err)
	}

	if got := pageNumbers(page); !slices.Equal(got, []int{1, 0}) {
		t.Errorf("last page numbers and next = %v, want [1 0]", got)
	}
}

// publishedThree publishes the acme factory as v1, v2 (wip 2) and v3 (wip 3).
func (h *harness) publishedThree(t *testing.T) domain.FactoryID {
	t.Helper()

	f := h.published(t)

	for _, wip := range []string{"wip: 2", "wip: 3"} {
		h.edit(t, f, strings.Replace(acmeYAML, "wip: 1", wip, 1))
		h.publish(t, f)
	}

	return f
}

// pageNumbers lists a page's version numbers, then its NextBefore.
func pageNumbers(p authoring.VersionPage) []int {
	out := []int{}
	for _, v := range p.Versions {
		out = append(out, v.Number)
	}

	return append(out, p.NextBefore)
}

func TestVersion_FilesAndCompare(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.publishedThree(t)

	detail, err := h.svc.Version(h.ctx, f, authoring.VersionSelector{Kind: authoring.SelectCurrent})
	if err != nil {
		t.Fatal(err)
	}

	if detail.Number != 3 || !detail.Current || len(detail.Files) != 2 {
		t.Errorf("current = v%d, running %v, %d files", detail.Number, detail.Current, len(detail.Files))
	}

	file, err := h.svc.VersionFile(h.ctx, f, authoring.VersionSelector{Kind: authoring.SelectNumber, Number: 2}, yamlFile)
	if err != nil {
		t.Fatal(err)
	}

	if !file.Immutable || !strings.Contains(string(file.Content), "wip: 2") {
		t.Errorf("v2 factory.yaml: immutable %v\n%s", file.Immutable, file.Content)
	}

	cmp, err := h.svc.Compare(h.ctx, f,
		authoring.VersionSelector{Kind: authoring.SelectNumber, Number: 1}, authoring.VersionSelector{Kind: authoring.SelectCurrent})
	if err != nil {
		t.Fatal(err)
	}

	if got := []int{cmp.Counts.Changes, cmp.From.Number, cmp.To.Number}; !slices.Equal(got, []int{1, 1, 3}) {
		t.Errorf("compare v1..current changes, from, to = %v", got)
	}
}

func TestPauseResumeAndDelete(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)

	if rec, err := h.svc.Pause(h.ctx, f); err != nil || !rec.Paused() {
		t.Errorf("pause = %+v, %v", rec, err)
	}

	if rec, err := h.svc.Resume(h.ctx, f); err != nil || rec.Paused() {
		t.Errorf("resume = %+v, %v", rec, err)
	}

	if err := h.svc.DeleteFactory(h.ctx, f); !errors.Is(err, authoring.ErrFactoryPublished) {
		t.Errorf("delete of a published factory = %v", err)
	}

	draft := h.imported(t, acmeFiles(credentialYAML))
	if err := h.svc.DeleteFactory(h.ctx, draft); err != nil {
		t.Errorf("delete of a never-published factory = %v", err)
	}

	if _, err := h.svc.Draft(h.ctx, draft); !errors.Is(err, authoring.ErrFactoryNotFound) {
		t.Errorf("draft of a deleted factory = %v", err)
	}
}
