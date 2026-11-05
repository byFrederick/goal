package authoring_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
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

// A draft that still has an evals/ folder publishes at once: the folder is only a warning, and
// no eval run gates the publish (ADR 0039 decision 21).
func TestPublish_EvalsFolderOnlyWarns(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	files := acmeFiles(acmeYAML)
	files["evals/cases/fix.yaml"] = []byte("agent: engineer\ntries: 3\n")
	files["evals/judges/followed.md"] = []byte("---\npassMark: 0.6\n---\nDid it follow the plan?\n")
	f := h.imported(t, files)

	val, err := h.svc.ValidateDraft(h.ctx, f, authoring.ValidateRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if !val.OK || !slices.Contains(codes(val.Problems), "evals-ignored") {
		t.Fatalf("validation = ok %v, problems %v; want ok with evals-ignored", val.OK, codes(val.Problems))
	}

	if v, err := h.svc.Publish(h.ctx, f, h.publishRequest(t, f)); err != nil || v.Number != 1 {
		t.Errorf("publish with an evals/ folder = %+v, %v; want v1", v, err)
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

// reviewRepo is the repository acmeYAML's runtime.repository names, as an item's row keys it.
const reviewRepo = "git.example.com/acme/review"

// repositoriesYAML is acmeYAML with runtime.repositories in place of the shorthand: one entry
// that writes, named name, at url.
func repositoriesYAML(name, url string) string {
	return strings.Replace(acmeYAML,
		`repository: { url: "https://git.example.com/acme/review.git", token: "{{code-token}}", forge: github }`,
		fmt.Sprintf(`repositories: [{ name: %s, url: %q, token: "{{code-token}}", access: write }]`, name, url), 1)
}

// itemUsing files an item of f in state at the build station whose repositories are repos.
func (h *harness) itemUsing(t *testing.T, f domain.FactoryID, state domain.ItemState, repos ...string) domain.Item {
	t.Helper()

	n := domain.NewItem{FactoryID: f, Type: "feature", Title: "An item", Station: "build", State: state, CreatedAt: h.clock.Now()}
	for _, r := range repos {
		n.Repositories = append(n.Repositories, domain.ItemRepository{Repo: r, Joined: domain.JoinedFiling})
	}

	it, err := h.items.CreateItem(h.ctx, n)
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// currentID is the id of f's running version; f has an open draft.
func (h *harness) currentID(t *testing.T, f domain.FactoryID) domain.DefinitionVersionID {
	t.Helper()

	d, err := h.svc.Draft(h.ctx, f)
	if err != nil || d.Current == nil {
		t.Fatalf("draft = %+v, %v; want a current version", d, err)
	}

	return d.Current.ID
}

func TestPublish_RemovedRepositoryWithOpenItemsBlocks(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)
	a := h.itemUsing(t, f, domain.StateQueued, reviewRepo)
	b := h.itemUsing(t, f, domain.StateInProgress, reviewRepo)
	done := h.itemUsing(t, f, domain.StateDone, reviewRepo)
	h.edit(t, f, repositoriesYAML("app", "https://git.example.com/acme/app"))
	running := h.currentID(t, f)

	_, err := h.svc.Publish(h.ctx, f, h.publishRequest(t, f))

	invalid, ok := errors.AsType[*authoring.InvalidError](err)
	if !ok {
		t.Fatalf("publish = %v, want an InvalidError", err)
	}

	p, ok := problem(invalid.Problems, authoring.CodeItemsUseRemovedRepository)
	if !ok || p.Check != authoring.CheckItems || p.Severity != domain.SeverityError {
		t.Fatalf("problems = %+v; want an items-use-removed-repository error", invalid.Problems)
	}

	for _, want := range []string{reviewRepo, fmt.Sprintf("#%d", a.Number), fmt.Sprintf("#%d", b.Number)} {
		if !strings.Contains(p.Message, want) {
			t.Errorf("message %q does not name %s", p.Message, want)
		}
	}

	if strings.Contains(p.Message, fmt.Sprintf("#%d", done.Number)) {
		t.Errorf("message %q names the done item #%d", p.Message, done.Number)
	}

	if got := h.currentID(t, f); got != running {
		t.Errorf("current = %s; want %s, the running version", got, running)
	}
}

func TestPublish_RemovedRepositoryWithOnlyDoneItemsPublishes(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)
	h.itemUsing(t, f, domain.StateDone, reviewRepo)
	h.itemUsing(t, f, domain.StateCancelled, reviewRepo)
	h.itemUsing(t, f, domain.StateQueued)
	h.edit(t, f, repositoriesYAML("app", "https://git.example.com/acme/app"))

	if v := h.publish(t, f); v.Number != 2 {
		t.Errorf("published version %d; want 2", v.Number)
	}
}

func TestPublish_RenamedRepositoryEntryPublishes(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)
	h.itemUsing(t, f, domain.StateInProgress, reviewRepo)
	h.edit(t, f, repositoriesYAML("main", "https://GIT.example.com/acme/Review"))

	if v := h.publish(t, f); v.Number != 2 {
		t.Errorf("published version %d; want 2", v.Number)
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

	if detail.Number != 3 || !detail.Current || len(detail.Files) != 3 {
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

// ruleYAML is acmeYAML declaring one factory rule, rules/secrets.md.
var ruleYAML = strings.Replace(acmeYAML, "  agents:\n", "  rules:\n    - { name: secrets, source: ./rules/secrets.md }\n  agents:\n", 1)

const ruleFile = "rules/secrets.md"

// ruleFiles is the acme definition with ruleYAML and rules/secrets.md written with front, its
// frontmatter.
func ruleFiles(front string) map[string][]byte {
	files := acmeFiles(ruleYAML)
	files[ruleFile] = []byte("---\n" + front + "---\n\nKeep secrets out of outputs.\n")

	return files
}

// A rule's file is a blob of the draft like any other: it validates and publishes in the manifest.
func TestPublish_RulesFiles(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	files := ruleFiles("description: Secrets stay out of logs and outputs.\n")
	f := h.imported(t, files)

	val, err := h.svc.ValidateDraft(h.ctx, f, authoring.ValidateRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if !val.OK {
		t.Fatalf("validation problems %v", codes(val.Problems))
	}

	v := h.publish(t, f)

	detail, err := h.svc.Version(h.ctx, f, authoring.VersionSelector{Kind: authoring.SelectNumber, Number: v.Number})
	if err != nil {
		t.Fatal(err)
	}

	i := slices.IndexFunc(detail.Files, func(fi authoring.FileInfo) bool { return fi.Path == ruleFile })
	if i < 0 || detail.Files[i].SHA256 != domain.HashBlob(files[ruleFile]) {
		t.Errorf("v%d files = %+v, want %s with its hash", v.Number, detail.Files, ruleFile)
	}
}

// withPolicies checks definitions against the organization policy p.
func withPolicies(p ports.OrgPolicies) option {
	return func(d *authoring.Deps) { d.Policies = p }
}

// cappedPolicies are the organization's default policy with mcp as its MCP tool mode cap.
type cappedPolicies struct {
	*porttest.OrgPolicies
	mcp map[string]domain.ConnectionToolMode
}

func (c cappedPolicies) Get(ctx context.Context) (domain.OrgPolicy, error) {
	p, err := c.OrgPolicies.Get(ctx)
	p.MCPToolModes = c.mcp

	return p, err
}

// ADR 0037 decisions 8 and 12: a tool mode the organization's MCP tool mode cap makes stricter is
// a policy warning at that tool, naming the file's mode and the effective one, and the version
// still publishes, since the cap applies in every run anyway.
func TestPublish_WarnsWhereThePolicyCapsAToolMode(t *testing.T) {
	t.Parallel()

	policies := cappedPolicies{
		OrgPolicies: porttest.NewOrgPolicies([]string{"claude-sonnet-5-5"}, nil, time.Now),
		mcp:         map[string]domain.ConnectionToolMode{"*": domain.ToolModeAsk},
	}
	h := newHarness(t, withPolicies(policies))

	files := acmeFiles(acmeYAML)
	files[envFile] = []byte(acmeEnvironment + "  mcp:\n    linear:\n      url: https://mcp.linear.example.com/mcp\n" +
		"      tools: {create_comment: allow, delete_issue: off}\n")
	f := h.imported(t, files)

	val, err := h.svc.ValidateDraft(h.ctx, f, authoring.ValidateRequest{})
	if err != nil {
		t.Fatal(err)
	}

	var capped []domain.DefinitionProblem

	for _, p := range val.Problems {
		if p.Code == config.CodeMCPToolModeCapped {
			capped = append(capped, p)
		}
	}

	if !val.OK || len(capped) != 1 {
		t.Fatalf("validation = ok %v, problems %+v; want ok with one mcp-tool-mode-capped", val.OK, val.Problems)
	}

	if p := capped[0]; p.Check != config.CheckPolicy || p.Severity != domain.SeverityWarning || p.File != envFile ||
		p.Pointer != "/environments/default/mcp/linear/tools/create_comment" || !strings.Contains(p.Message, "allow") || !strings.Contains(p.Message, "ask") {
		t.Errorf("problem %+v; want a policy warning at create_comment naming allow and ask", p)
	}

	if _, err := h.svc.Publish(h.ctx, f, h.publishRequest(t, f)); err != nil {
		t.Errorf("publish with a capped tool mode = %v", err)
	}
}

// A version that names a model the organization policy does not allow is refused, with a
// problem at that key that names the agent and the model; once its models are allowed, it publishes.
func TestPublish_RefusesAModelThePolicyDoesNotAllow(t *testing.T) {
	t.Parallel()

	// The platform offers only sonnet and haiku, so the organization's default policy allows only them.
	h := newHarness(t, withPolicies(porttest.NewOrgPolicies([]string{"claude-sonnet-5-5", "claude-haiku-5-5"}, nil, time.Now)))
	opus := strings.Replace(acmeYAML, "      prompt: ./agents/engineer.md\n", "      prompt: ./agents/engineer.md\n      model: claude-opus-5-5\n", 1)
	f := h.imported(t, acmeFiles(opus))

	val, err := h.svc.ValidateDraft(h.ctx, f, authoring.ValidateRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if val.OK || !slices.ContainsFunc(val.Checks, func(c authoring.CheckResult) bool { return c.ID == "policy" && c.Status == authoring.CheckFailed }) {
		t.Errorf("validation = ok %v, checks %+v; want the policy check failed", val.OK, val.Checks)
	}

	_, err = h.svc.Publish(h.ctx, f, h.publishRequest(t, f))

	invalid, ok := errors.AsType[*authoring.InvalidError](err)
	if !ok {
		t.Fatalf("publish with a model the policy does not allow = %v", err)
	}

	i := slices.IndexFunc(invalid.Problems, func(p domain.DefinitionProblem) bool { return p.Code == "model-not-allowed" })
	if i < 0 {
		t.Fatalf("problems %v, want model-not-allowed", codes(invalid.Problems))
	}

	if p := invalid.Problems[i]; p.Check != "policy" || p.Line == 0 ||
		!strings.Contains(p.Message, "engineer") || !strings.Contains(p.Message, "claude-opus-5-5") {
		t.Errorf("problem %+v; want the policy check at the agent's model, naming engineer and claude-opus-5-5", p)
	}

	sonnet := strings.Replace(opus, "claude-opus-5-5", "claude-sonnet-5-5", 1)
	if _, err := h.svc.PutFile(h.ctx, f, authoring.FileWrite{Path: yamlFile, Content: []byte(sonnet)}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.svc.Publish(h.ctx, f, h.publishRequest(t, f)); err != nil {
		t.Errorf("publish with an allowed model = %v", err)
	}
}

// webhooksStub is the organization's one GitHub webhook, open to the factories in open.
type webhooksStub struct {
	mu   sync.Mutex
	open []domain.FactoryID
}

func (w *webhooksStub) Webhooks(context.Context) ([]domain.WebhookEndpoint, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	hook := domain.WebhookEndpoint{ID: "w1", Name: "gh-acme", Sender: domain.SenderGitHub}
	for _, f := range w.open {
		hook.Openings = append(hook.Openings, domain.WebhookOpening{FactoryID: f})
	}

	return []domain.WebhookEndpoint{hook}, nil
}

// Publish refuses a trigger naming a webhook that isn't open to the factory, and a filter its
// sender's event never supplies; once the webhook is open and the filter fits, it publishes.
func TestPublish_RefusesWebhookNotOpen(t *testing.T) {
	t.Parallel()

	hooks := &webhooksStub{}
	h := newHarness(t, func(d *authoring.Deps) { d.Webhooks = hooks })

	withTrigger := func(filter string) string {
		return strings.Replace(acmeYAML, "  board:\n", "  board:\n    workTypes: [feature]\n    triggers:\n"+
			"      - name: red\n        on: gh-acme.check_run\n"+filter+"        create: { type: feature, station: build }\n", 1)
	}

	f := h.imported(t, acmeFiles(withTrigger("")))

	_, err := h.svc.Publish(h.ctx, f, h.publishRequest(t, f))
	if invalid, ok := errors.AsType[*authoring.InvalidError](err); !ok || !slices.Contains(codes(invalid.Problems), authoring.CodeWebhookNotOpen) {
		t.Fatalf("publish naming a closed webhook = %v; want %s", err, authoring.CodeWebhookNotOpen)
	}

	hooks.mu.Lock()
	hooks.open = []domain.FactoryID{f}
	hooks.mu.Unlock()

	if _, err := h.svc.PutFile(h.ctx, f, authoring.FileWrite{Path: yamlFile, Content: []byte(withTrigger("        filter: { paths: [\"**/*.go\"] }\n"))}); err != nil {
		t.Fatal(err)
	}

	_, err = h.svc.Publish(h.ctx, f, h.publishRequest(t, f))
	if invalid, ok := errors.AsType[*authoring.InvalidError](err); !ok || !slices.Contains(codes(invalid.Problems), config.CodeFilterUnsupplied) {
		t.Fatalf("publish with a filter check_run can't supply = %v; want %s", err, config.CodeFilterUnsupplied)
	}

	if _, err := h.svc.PutFile(h.ctx, f, authoring.FileWrite{Path: yamlFile, Content: []byte(withTrigger("        filter: { conclusion: failure }\n"))}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.svc.Publish(h.ctx, f, h.publishRequest(t, f)); err != nil {
		t.Errorf("publish naming an open webhook = %v; want it published", err)
	}
}

// acmeCounts are the counts of acmeYAML's tree as the version list shows them.
var acmeCounts = authoring.Counts{Stations: 4, Agents: 1, Skills: 0, Connections: 0}

// blobCountingStore counts the blobs read through it and fails them while fail is set.
type blobCountingStore struct {
	ports.DefinitionStore

	reads atomic.Int64
	fail  atomic.Bool
}

var errBucketDown = errors.New("bucket down")

func (s *blobCountingStore) Blob(ctx context.Context, h domain.BlobHash) ([]byte, error) {
	s.reads.Add(1)

	if s.fail.Load() {
		return nil, errBucketDown
	}

	return s.DefinitionStore.Blob(ctx, h)
}

func withBlobCounting(s **blobCountingStore) option {
	return withStore(func(inner ports.DefinitionStore) ports.DefinitionStore {
		*s = &blobCountingStore{DefinitionStore: inner}

		return *s
	})
}

func TestVersions_ListReadsNoTrees(t *testing.T) {
	t.Parallel()

	var blobs *blobCountingStore

	h := newHarness(t, withBlobCounting(&blobs))
	f := h.published(t)

	for i := 2; i <= authoring.MaxVersionPage; i++ {
		h.edit(t, f, strings.Replace(acmeYAML, "wip: 1", fmt.Sprintf("wip: %d", i), 1))
		h.publish(t, f)
	}

	blobs.reads.Store(0)

	start := time.Now()

	page, err := h.svc.Versions(h.ctx, f, 0, authoring.MaxVersionPage)
	if err != nil {
		t.Fatal(err)
	}

	if took := time.Since(start); took > time.Second {
		t.Errorf("listing %d versions took %v, want under a second", authoring.MaxVersionPage, took)
	}

	if n := blobs.reads.Load(); n != 0 {
		t.Errorf("listing versions read %d blobs, want none", n)
	}

	if len(page.Versions) != authoring.MaxVersionPage {
		t.Fatalf("listed %d versions, want %d", len(page.Versions), authoring.MaxVersionPage)
	}

	for _, v := range page.Versions {
		if v.Counts != acmeCounts {
			t.Errorf("v%d counts = %+v, want %+v", v.Number, v.Counts, acmeCounts)
		}
	}
}

func TestVersions_BackfillsMissingCounts(t *testing.T) {
	t.Parallel()

	var blobs *blobCountingStore

	h := newHarness(t, withBlobCounting(&blobs))
	f := h.imported(t, acmeFiles(acmeYAML))

	// porttest.Publish publishes through the store without counts, as versions published
	// before counts were kept are.
	for _, files := range []map[string][]byte{
		acmeFiles(acmeYAML),
		acmeFiles(strings.Replace(acmeYAML, "wip: 1", "wip: 2", 1)),
		{yamlFile: []byte("spec: [does not parse\n")},
	} {
		if _, err := porttest.Publish(h.ctx, h.store, f, "acme-review", files); err != nil {
			t.Fatal(err)
		}
	}

	blobs.fail.Store(true)

	if _, err := h.svc.Versions(h.ctx, f, 0, 10); !errors.Is(err, errBucketDown) {
		t.Errorf("list while the bucket is down = %v, want %v", err, errBucketDown)
	}

	for i, c := range h.storedCounts(t, f) {
		if c != nil {
			t.Errorf("version %d got counts %+v from a failed tree read, want none", 3-i, *c)
		}
	}

	blobs.fail.Store(false)

	want := []authoring.Counts{{}, acmeCounts, acmeCounts}

	for _, round := range []string{"first list", "second list"} {
		blobs.reads.Store(0)

		if got := h.listedCounts(t, f); !slices.Equal(got, want) {
			t.Errorf("%s counts = %+v, want %+v", round, got, want)
		}

		if n := blobs.reads.Load(); (n == 0) != (round == "second list") {
			t.Errorf("%s read %d blobs", round, n)
		}
	}

	wantStored := []domain.VersionCounts{{}, {Stations: 4, Agents: 1}, {Stations: 4, Agents: 1}}

	for i, c := range h.storedCounts(t, f) {
		if c == nil || *c != wantStored[i] {
			t.Errorf("version %d stored counts %v, want %+v", 3-i, c, wantStored[i])
		}
	}
}

// listedCounts lists a factory's versions and returns their counts, newest first.
func (h *harness) listedCounts(t *testing.T, f domain.FactoryID) []authoring.Counts {
	t.Helper()

	page, err := h.svc.Versions(h.ctx, f, 0, authoring.MaxVersionPage)
	if err != nil {
		t.Fatal(err)
	}

	out := make([]authoring.Counts, 0, len(page.Versions))
	for _, v := range page.Versions {
		out = append(out, v.Counts)
	}

	return out
}

// storedCounts returns the counts the store keeps for a factory's versions, newest first.
func (h *harness) storedCounts(t *testing.T, f domain.FactoryID) []*domain.VersionCounts {
	t.Helper()

	vs, err := h.store.Versions(h.ctx, f, 0, authoring.MaxVersionPage)
	if err != nil {
		t.Fatal(err)
	}

	out := make([]*domain.VersionCounts, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Counts)
	}

	return out
}

// A definition with several entries, one that only reads, and an agent that only reads publishes
// (ADR 0041 decisions 2 and 3): the reader's tokens and its GitHub tools are warnings only.
func TestPublish_SeveralEntriesAndAReaderPublishWithWarnings(t *testing.T) {
	t.Parallel()

	tests := porttest.NewEnvironmentTests()
	h := newHarness(t, withSecretsCheck(func(c *authoring.SecretsCheck) { c.Tests = tests }))
	h.secret(t, "code-read", domain.SecretScope{All: true})

	yaml := strings.Replace(withCode("read"),
		`repository: { url: "https://git.example.com/acme/review.git", token: "{{code-token}}", forge: github }`,
		`repositories: [`+
			`{ name: services, url: "https://git.example.com/acme/*", token: "{{code-token}}", access: write, default: false }, `+
			`{ name: docs, url: "https://git.example.com/acme/docs", token: "{{code-read}}", access: read }]`, 1)

	files := acmeFiles(yaml)
	files[envFile] = []byte(gitHubEnv("{}"))

	f := h.imported(t, files)

	org, _ := domain.OrgFrom(h.ctx)
	tests.AddFactory(org, f)

	val, err := h.svc.ValidateDraft(h.ctx, f, authoring.ValidateRequest{})
	if err != nil {
		t.Fatal(err)
	}

	got := codes(val.Problems)
	for _, want := range []string{"reader-write-token", authoring.CodeGitHubToolsUnknown} {
		if !slices.Contains(got, want) {
			t.Errorf("problems %v; want %s", got, want)
		}
	}

	if p, ok := problem(val.Problems, "reader-write-token"); ok && p.Message != "engineer only reads, but the only token for services can write." {
		t.Errorf("reader-write-token says %q", p.Message)
	}

	for _, p := range val.Problems {
		if p.Severity == domain.SeverityError {
			t.Errorf("error %s: %s; want warnings only", p.Code, p.Message)
		}
	}

	if v := h.publish(t, f); v.Number != 1 {
		t.Errorf("published version %d; want 1", v.Number)
	}
}
