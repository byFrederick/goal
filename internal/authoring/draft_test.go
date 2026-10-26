package authoring_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// stations is the line section with the given stations (JSON objects).
func lineValue(stations ...string) json.RawMessage {
	return json.RawMessage(`{"stations":[` + strings.Join(stations, ",") + `]}`)
}

var acmeStations = []string{
	`{"id":"inbox","kind":"queue"}`, `{"id":"build","kind":"agent","agent":"engineer","wip":1}`,
	`{"id":"review","kind":"human"}`, `{"id":"done","kind":"done"}`,
}

func TestSaveSection_TwoEditorsOnDifferentSectionsBothSave(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.imported(t, acmeFiles(acmeYAML))

	// Both editors read the model at the same revision.
	m, err := h.svc.DraftModel(h.ctx, f)
	if err != nil {
		t.Fatalf("model: %v", err)
	}

	line := append([]string{}, acmeStations...)
	line[1] = `{"id":"build","kind":"agent","agent":"engineer","wip":2}`

	if _, err := h.svc.SaveSection(h.ctx, f, authoring.SectionSave{
		Section: authoring.Line, IfMatch: m.ETags[authoring.Line], Value: lineValue(line...),
	}); err != nil {
		t.Fatalf("editor A saves the line: %v", err)
	}

	res, err := h.svc.SaveSection(h.ctx, f, authoring.SectionSave{
		Section: authoring.Review, IfMatch: m.ETags[authoring.Review], Value: json.RawMessage(`{"budget":{"weekly_usd":50}}`),
	})
	if err != nil {
		t.Fatalf("editor B saves the budget with the etag read before A's save: %v", err)
	}

	doc := h.draftFile(t, f, yamlFile)
	if !strings.Contains(doc, "wip: 2") || !strings.Contains(doc, "weeklyUsd: 50") || !strings.Contains(doc, "# The one engineer.") {
		t.Errorf("draft lost a save or a comment:\n%s", doc)
	}

	if res.Revision != m.Revision+2 || res.ETag == "" || res.ETag == m.ETags[authoring.Review] {
		t.Errorf("save result = %+v (read at revision %d)", res, m.Revision)
	}
}

func TestSaveSection_SameSectionIsAConflictWithTheCurrentValue(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.imported(t, acmeFiles(acmeYAML))

	m, err := h.svc.DraftModel(h.ctx, f)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.svc.SaveSection(h.ctx, f, authoring.SectionSave{
		Section: authoring.Line, IfMatch: m.ETags[authoring.Line], Value: lineValue(acmeStations[0], acmeStations[3]),
	}); err != nil {
		t.Fatal(err)
	}

	_, err = h.svc.SaveSection(h.ctx, f, authoring.SectionSave{
		Section: authoring.Line, IfMatch: m.ETags[authoring.Line], Value: lineValue(acmeStations...),
	})

	conflict, ok := errors.AsType[*authoring.SectionConflictError](err)
	if !ok {
		t.Fatalf("second save of the line = %v, want a SectionConflictError", err)
	}

	stations, _ := conflict.Value.(map[string]any)["stations"].([]any)
	if len(stations) != 2 || conflict.ETag == m.ETags[authoring.Line] || authoring.ProblemCode(err) != authoring.CodeDraftConflict {
		t.Errorf("conflict = %+v", conflict)
	}

	again, err := h.svc.DraftModel(h.ctx, f)
	if err != nil || again.ETags[authoring.Line] != conflict.ETag {
		t.Errorf("conflict etag %s is not the current one (%v)", conflict.ETag, err)
	}
}

func TestSaveSection_Refusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.imported(t, acmeFiles(acmeYAML))

	_, err := h.svc.SaveSection(h.ctx, f, authoring.SectionSave{Section: authoring.Line, Value: json.RawMessage(`{"stationz":[]}`)})
	if !errors.Is(err, authoring.ErrModelInvalid) {
		t.Errorf("a value of the wrong shape = %v", err)
	}

	if _, err := h.svc.PutFile(h.ctx, f, authoring.FileWrite{Path: yamlFile, Content: []byte("spec: [\n")}); err != nil {
		t.Fatal(err)
	}

	_, err = h.svc.SaveSection(h.ctx, f, authoring.SectionSave{Section: authoring.Line, Value: lineValue(acmeStations...)})
	if authoring.ProblemCode(err) != authoring.CodeYAMLInvalid {
		t.Errorf("a save on a factory.yaml that does not parse = %v", err)
	}

	if _, err := h.svc.DraftModel(h.ctx, f); authoring.ProblemCode(err) != authoring.CodeYAMLInvalid {
		t.Errorf("model of a factory.yaml that does not parse = %v", err)
	}
}

// racingStore saves another file through the store right before the first SaveDraft, so that
// save finds the revision moved.
type racingStore struct {
	ports.DefinitionStore

	once sync.Once
	race func(context.Context)
}

func (r *racingStore) SaveDraft(ctx context.Context, c ports.DraftChange) (domain.DefinitionVersion, error) {
	r.once.Do(func() { r.race(ctx) })

	return r.DefinitionStore.SaveDraft(ctx, c)
}

func TestSaveSection_RevisionRaceReappliesOnTopOfTheOtherSave(t *testing.T) {
	t.Parallel()

	var race *racingStore

	h := newHarness(t, withStore(func(s ports.DefinitionStore) ports.DefinitionStore {
		race = &racingStore{DefinitionStore: s}

		return race
	}))
	f := h.imported(t, acmeFiles(acmeYAML))

	race.race = func(ctx context.Context) {
		d, err := h.store.Draft(ctx, f)
		if err != nil {
			t.Error(err)

			return
		}

		m, err := h.store.PutBlobs(ctx, map[string][]byte{"agents/reviewer.md": []byte("Review it.\n")})
		if err == nil {
			_, err = h.store.SaveDraft(ctx, ports.DraftChange{Factory: f, ExpectedRevision: d.Revision, Set: m, By: domain.LocalUser})
		}

		if err != nil {
			t.Error(err)
		}
	}

	if _, err := h.svc.SaveSection(h.ctx, f, authoring.SectionSave{
		Section: authoring.Line, Value: lineValue(acmeStations[0], acmeStations[1], acmeStations[3]),
	}); err != nil {
		t.Fatalf("save racing another writer: %v", err)
	}

	if doc := h.draftFile(t, f, yamlFile); strings.Contains(doc, "id: review") {
		t.Errorf("the line save was lost:\n%s", doc)
	}

	if got := h.draftFile(t, f, "agents/reviewer.md"); got != "Review it.\n" {
		t.Errorf("the other writer's file was lost: %q", got)
	}
}

func TestSaveSection_AgentWithItsPromptInOneRevision(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.imported(t, acmeFiles(acmeYAML))

	m, err := h.svc.DraftModel(h.ctx, f)
	if err != nil {
		t.Fatal(err)
	}

	agents := json.RawMessage(`{"agents":[{"name":"engineer","prompt":{"path":"./agents/engineer.md"}},` +
		`{"name":"reviewer","prompt":{"path":"./agents/reviewer.md"}}]}`)

	res, err := h.svc.SaveSection(h.ctx, f, authoring.SectionSave{
		Section: authoring.Agents, IfMatch: m.ETags[authoring.Agents], Value: agents,
		Files: map[string][]byte{"agents/reviewer.md": []byte("Review it.\n")},
	})
	if err != nil {
		t.Fatalf("add an agent with its prompt: %v", err)
	}

	if res.Revision != m.Revision+1 || h.draftFile(t, f, "agents/reviewer.md") != "Review it.\n" {
		t.Errorf("agent and prompt not saved in one revision: %+v", res)
	}

	// Removing the reviewer removes its prompt; the engineer's prompt is still used, so it stays.
	_, err = h.svc.SaveSection(h.ctx, f, authoring.SectionSave{
		Section: authoring.Agents, IfMatch: res.ETag,
		Value:       json.RawMessage(`{"agents":[{"name":"engineer","prompt":{"path":"./agents/engineer.md"}}]}`),
		RemoveFiles: []string{"agents/reviewer.md", promptFile},
	})
	if err != nil {
		t.Fatalf("remove the agent: %v", err)
	}

	files, err := h.svc.DraftFiles(h.ctx, f)
	if err != nil {
		t.Fatal(err)
	}

	var paths []string
	for _, fi := range files {
		paths = append(paths, fi.Path)
	}

	if strings.Join(paths, " ") != "agents/engineer.md factory.yaml" {
		t.Errorf("files after removing the reviewer = %v", paths)
	}
}

func TestSaveSection_UnchangedValueSavesNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.imported(t, acmeFiles(acmeYAML))

	m, err := h.svc.DraftModel(h.ctx, f)
	if err != nil {
		t.Fatal(err)
	}

	v, err := json.Marshal(m.Model.Line)
	if err != nil {
		t.Fatal(err)
	}

	res, err := h.svc.SaveSection(h.ctx, f, authoring.SectionSave{Section: authoring.Line, IfMatch: m.ETags[authoring.Line], Value: v})
	if err != nil || res.Revision != m.Revision || res.ETag != m.ETags[authoring.Line] {
		t.Errorf("unchanged save = %+v, %v; want revision %d", res, err, m.Revision)
	}

	if h.draftFile(t, f, yamlFile) != acmeYAML {
		t.Errorf("an unchanged save rewrote factory.yaml")
	}
}

func TestRevertSection_PutsTheBaseValueBack(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)
	h.edit(t, f, strings.Replace(strings.Replace(acmeYAML, "wip: 1", "wip: 3", 1),
		"      - { id: done, kind: done }", "      - { id: qa, kind: human }\n      - { id: done, kind: done }", 1))

	m, err := h.svc.DraftModel(h.ctx, f)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := m.Edited["/line/stations/1/wip"]; !ok {
		t.Fatalf("edited = %v, want /line/stations/1/wip", m.Edited)
	}

	if _, ok := m.Edited["/line/stations/3"]; !ok {
		t.Fatalf("edited = %v, want the new station /line/stations/3", m.Edited)
	}

	if _, err := h.svc.RevertSection(h.ctx, f, authoring.Line, m.ETags[authoring.Line],
		[]string{"/line/stations/1/wip", "/line/stations/3"}); err != nil {
		t.Fatalf("revert: %v", err)
	}

	if doc := h.draftFile(t, f, yamlFile); doc != acmeYAML {
		t.Errorf("reverted draft differs from v1:\n%s", doc)
	}

	if _, err := h.svc.RevertSection(h.ctx, f, authoring.Line, "", []string{"/agents/agents"}); !errors.Is(err, authoring.ErrInvalidRequest) {
		t.Errorf("a pointer of another section = %v", err)
	}
}

func TestPutFile_HashPreconditions(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.imported(t, acmeFiles(acmeYAML))
	old := domain.HashBlob([]byte(enginePrompt))

	res, err := h.svc.PutFile(h.ctx, f, authoring.FileWrite{Path: promptFile, Content: []byte("v2\n"), IfMatch: old})
	if err != nil || res.SHA256 != domain.HashBlob([]byte("v2\n")) {
		t.Fatalf("put with the current hash = %+v, %v", res, err)
	}

	_, err = h.svc.PutFile(h.ctx, f, authoring.FileWrite{Path: promptFile, Content: []byte("v3\n"), IfMatch: old})
	if c, ok := errors.AsType[*authoring.FileConflictError](err); !ok || c.SHA256 != res.SHA256 {
		t.Errorf("put with a stale hash = %v, want a conflict naming the current hash", err)
	}

	_, err = h.svc.PutFile(h.ctx, f, authoring.FileWrite{Path: promptFile, Content: []byte("v3\n"), Create: true})
	if !errors.Is(err, authoring.ErrDraftConflict) {
		t.Errorf("create over an existing file = %v", err)
	}

	for path, want := range map[string]error{"../escape.md": authoring.ErrBadPath, "knowledge/a.md": authoring.ErrBadPath} {
		if _, err := h.svc.PutFile(h.ctx, f, authoring.FileWrite{Path: path, Content: []byte("x")}); !errors.Is(err, want) {
			t.Errorf("put %s = %v, want %v", path, err, want)
		}
	}

	big := make([]byte, domain.MaxBlobSize+1)
	if _, err := h.svc.PutFile(h.ctx, f, authoring.FileWrite{Path: "big.md", Content: big}); !errors.Is(err, authoring.ErrTooLarge) {
		t.Errorf("a file over 1 MiB = %v", err)
	}

	if _, err := h.svc.DeleteFile(h.ctx, f, "agents/missing.md", ""); !errors.Is(err, authoring.ErrFileNotFound) {
		t.Errorf("delete of a missing file = %v", err)
	}

	if _, err := h.svc.DeleteFile(h.ctx, f, promptFile, res.SHA256); err != nil {
		t.Errorf("delete with the current hash = %v", err)
	}
}

func TestReplaceTree_ExpectedRevision(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.imported(t, acmeFiles(acmeYAML))

	stale := int64(0)
	if _, err := h.svc.ReplaceTree(h.ctx, f, acmeFiles(acmeYAML), &stale); !errors.Is(err, authoring.ErrDraftConflict) {
		t.Errorf("replace at a stale revision = %v", err)
	}

	res, err := h.svc.ReplaceTree(h.ctx, f, map[string][]byte{yamlFile: []byte(acmeYAML)}, nil)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}

	files, err := h.svc.DraftFiles(h.ctx, f)
	if err != nil || len(files) != 1 || files[0].Path != yamlFile || files[0].Size != int64(len(acmeYAML)) {
		t.Errorf("files after replace = %+v, %v", files, err)
	}

	// The prompt is gone, which validation reports.
	if res.ProblemCount == 0 {
		t.Errorf("replace result reports no problem: %+v", res)
	}
}

func TestDraft_HeaderCountsChangesAndProblems(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)
	h.edit(t, f, strings.Replace(strings.Replace(acmeYAML, "wip: 1", "wip: 2", 1), "agent: engineer", "agent: nobody", 1))

	d, err := h.svc.Draft(h.ctx, f)
	if err != nil {
		t.Fatal(err)
	}

	if d.NextNumber != 2 || d.Base == nil || d.Base.Number != 1 || d.Current == nil || d.Current.Number != 1 {
		t.Errorf("draft header = next %d, base %+v, current %+v", d.NextNumber, d.Base, d.Current)
	}

	if d.ChangeCount != 1 || d.ProblemCount != 1 {
		t.Errorf("changes %d, problems %d; want 1 and 1", d.ChangeCount, d.ProblemCount)
	}

	for _, s := range d.Sections {
		want := 0
		if s.Section == authoring.Line {
			want = 1
		}

		if s.Changes != want || s.Problems != want {
			t.Errorf("section %s: %d changes, %d problems", s.Section, s.Changes, s.Problems)
		}
	}
}

func TestOpenDraft_AndDiscard(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)

	if _, err := h.svc.Draft(h.ctx, f); !errors.Is(err, authoring.ErrNoDraft) {
		t.Errorf("draft after publish = %v, want ErrNoDraft", err)
	}

	d, created, err := h.svc.OpenDraft(h.ctx, f, authoring.OpenDraftRequest{})
	if err != nil || !created || d.Draft.BaseID == "" {
		t.Fatalf("open = %+v, %v, %v", d.Draft, created, err)
	}

	again, created, err := h.svc.OpenDraft(h.ctx, f, authoring.OpenDraftRequest{Base: authoring.VersionSelector{Kind: authoring.SelectNumber, Number: 1}})
	if err != nil || created || again.Draft.ID != d.Draft.ID {
		t.Errorf("second open = %+v, %v, %v; want the same draft", again.Draft.ID, created, err)
	}

	if err := h.svc.DiscardDraft(h.ctx, f, d.Draft.Revision+1); !errors.Is(err, authoring.ErrDraftConflict) {
		t.Errorf("discard at a stale revision = %v", err)
	}

	if err := h.svc.DiscardDraft(h.ctx, f, d.Draft.Revision); err != nil {
		t.Fatalf("discard: %v", err)
	}

	if err := h.svc.DiscardDraft(h.ctx, f, d.Draft.Revision); !errors.Is(err, authoring.ErrNoDraft) {
		t.Errorf("discard without a draft = %v", err)
	}
}
