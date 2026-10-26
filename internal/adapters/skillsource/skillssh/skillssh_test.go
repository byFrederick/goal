package skillssh_test

import (
	"cmp"
	"crypto/sha1" //nolint:gosec // the fake GitHub computes git object ids, which are SHA-1
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/adapters/skillsource/skillssh"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const (
	head   = "1111111111111111111111111111111111111111"
	pinned = "2222222222222222222222222222222222222222"
	token  = "ghp_test-token-never-shown" //nolint:gosec // a made-up token the tests look for in errors

	keySHA   = "sha"
	lintMD   = "lint/SKILL.md"
	tfSource = "acme/tf"
)

// entry is a file of the fake repository: a regular file unless mode says otherwise.
type entry struct {
	body string
	mode string
	// size, when set, is what the tree claims instead of the body's length.
	size int64
}

// fakeGitHub serves acme/skills: one commit per sha, each a tree of files, through the REST
// endpoints the adapter reads. It records every request.
type fakeGitHub struct {
	t       *testing.T
	srv     *httptest.Server
	commits map[string]map[string]entry // commit → path → file
	answer  map[string]string           // commit asked → commit answered (default: the same)

	mu       sync.Mutex
	trees    map[string][]map[string]any // tree sha → its recursive entries (paths relative to it)
	blobs    map[string][]byte
	requests []string
	auth     []string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()

	g := &fakeGitHub{
		t: t, commits: map[string]map[string]entry{}, answer: map[string]string{},
		trees: map[string][]map[string]any{}, blobs: map[string][]byte{},
	}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)

	return g
}

func blobID(body []byte) string {
	sum := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(body))), body...)) //nolint:gosec // git object ids

	return hex.EncodeToString(sum[:])
}

// build makes the trees of a commit and returns its root tree's sha.
func (g *fakeGitHub) build(files map[string]entry) string {
	dirs := map[string]bool{"": true}

	for p := range files {
		for d := path.Dir(p); d != "."; d = path.Dir(d) {
			dirs[d] = true
		}
	}

	var fingerprint strings.Builder
	for _, p := range slices.Sorted(maps.Keys(files)) {
		fingerprint.WriteString(p + "\x00" + files[p].body + "\x00")
	}

	treeSHA := func(dir string) string {
		sum := sha1.Sum([]byte("tree:" + dir + "\x00" + fingerprint.String())) //nolint:gosec // a fake id

		return hex.EncodeToString(sum[:])
	}

	for dir := range dirs {
		prefix := dir + "/"
		if dir == "" {
			prefix = ""
		}

		var entries []map[string]any

		for d := range dirs {
			if rel, ok := strings.CutPrefix(d, prefix); ok && d != dir && d != "" {
				entries = append(entries, map[string]any{"path": rel, "mode": "040000", "type": "tree", keySHA: treeSHA(d)})
			}
		}

		for p, e := range files {
			if rel, ok := strings.CutPrefix(p, prefix); ok {
				entries = append(entries, g.blobEntry(rel, e))
			}
		}

		g.trees[treeSHA(dir)] = entries
	}

	return treeSHA("")
}

// blobEntry stores a file's blob and returns its tree entry.
func (g *fakeGitHub) blobEntry(rel string, e entry) map[string]any {
	body := []byte(e.body)
	id := blobID(body)
	g.blobs[id] = body

	mode, typ := cmp.Or(e.mode, "100644"), "blob"
	if mode == "160000" {
		typ = "commit"
	}

	return map[string]any{"path": rel, "mode": mode, "type": typ, keySHA: id, "size": cmp.Or(e.size, int64(len(body)))}
}

func (g *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.requests = append(g.requests, r.URL.Path)
	g.auth = append(g.auth, r.Header.Get("Authorization"))
	g.mu.Unlock()

	rest, ok := strings.CutPrefix(r.URL.Path, "/repos/acme/skills/")
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch kind, id, _ := strings.Cut(rest, "/"); {
	case kind == "commits":
		g.commit(w, id)
	case kind == "git" && strings.HasPrefix(id, "trees/"):
		g.tree(w, strings.TrimPrefix(id, "trees/"), r.URL.Query().Get("recursive") == "1")
	case kind == "git" && strings.HasPrefix(id, "blobs/"):
		body, ok := g.blobs[strings.TrimPrefix(id, "blobs/")]
		if !ok {
			http.NotFound(w, r)
			return
		}

		enc := base64.StdEncoding.EncodeToString(body)

		var lines []string
		for len(enc) > 60 {
			lines, enc = append(lines, enc[:60]), enc[60:]
		}

		writeJSON(w, map[string]any{"content": strings.Join(append(lines, enc), "\n"), "encoding": "base64"})
	default:
		http.NotFound(w, r)
	}
}

func (g *fakeGitHub) commit(w http.ResponseWriter, id string) {
	if id == "HEAD" {
		id = head
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	files, ok := g.commits[id]
	if !ok {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	writeJSON(w, map[string]any{keySHA: cmp.Or(g.answer[id], id), "commit": map[string]any{"tree": map[string]any{keySHA: g.build(files)}}})
}

// set makes commit hold files; answer, when set, is the commit GitHub answers for it.
func (g *fakeGitHub) set(commit string, files map[string]entry, answer string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.commits[commit] = files
	g.answer[commit] = answer
}

func (g *fakeGitHub) tree(w http.ResponseWriter, sha string, recursive bool) {
	g.mu.Lock()
	entries, ok := g.trees[sha]
	g.mu.Unlock()

	if !ok {
		http.NotFound(w, nil)
		return
	}

	if !recursive {
		entries = slices.DeleteFunc(slices.Clone(entries), func(e map[string]any) bool {
			p, _ := e["path"].(string)
			return strings.Contains(p, "/")
		})
	}

	writeJSON(w, map[string]any{keySHA: sha, "truncated": false, "tree": entries})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (g *fakeGitHub) blobRequests() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	n := 0

	for _, r := range g.requests {
		if strings.Contains(r, "/git/blobs/") {
			n++
		}
	}

	return n
}

func (g *fakeGitHub) source(t *testing.T, cfg skillssh.Config) *skillssh.Source {
	t.Helper()

	cfg.GitHubAPIURL = g.srv.URL
	if cfg.DirectoryURL == "" {
		cfg.DirectoryURL = "http://127.0.0.1:1"
	}

	s, err := skillssh.New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func lint(c string) ports.SkillRef {
	return ports.SkillRef{Owner: "acme", Repo: "skills", Skill: "lint", Commit: c}
}

func names(files map[string][]byte) string {
	out := make([]string, 0, len(files))
	for p := range files {
		out = append(out, p)
	}

	slices.Sort(out)

	return strings.Join(out, ",")
}

func TestFetch_TheHeadsFolderWithItsFiles(t *testing.T) {
	t.Parallel()

	g := newFakeGitHub(t)
	g.commits[head] = map[string]entry{
		"README.md":                    {body: "repo readme"},
		"skills/lint/SKILL.md":         {body: "---\nname: lint\n---\n"},
		"skills/lint/scripts/run.sh":   {body: strings.Repeat("echo hi\n", 100), mode: "100755"},
		"skills/lint/.github/notes.md": {body: "hidden"},
		"skills/lint-extra/SKILL.md":   {body: "another skill"},
		"skills/other/SKILL.md":        {body: "other"},
	}

	tree, err := g.source(t, skillssh.Config{GitHubToken: token}).Fetch(t.Context(), lint(""))
	if err != nil {
		t.Fatal(err)
	}

	if tree.Ref != lint(head) || names(tree.Files) != "SKILL.md,scripts/run.sh" ||
		string(tree.Files["scripts/run.sh"]) != strings.Repeat("echo hi\n", 100) {
		t.Errorf("tree = %+v %s", tree.Ref, names(tree.Files))
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	for i, a := range g.auth {
		if a != "Bearer "+token {
			t.Errorf("request %s carried %q, want the token", g.requests[i], a)
		}
	}
}

// A pinned commit is the commit read, and GitHub answering another is refused.
func TestFetch_PinsTheCommit(t *testing.T) {
	t.Parallel()

	g := newFakeGitHub(t)
	g.set(pinned, map[string]entry{lintMD: {body: "pinned"}}, "")
	g.set(head, map[string]entry{lintMD: {body: "head"}}, "")

	tree, err := g.source(t, skillssh.Config{}).Fetch(t.Context(), lint(pinned))
	if err != nil || tree.Ref.Commit != pinned || string(tree.Files["SKILL.md"]) != "pinned" {
		t.Fatalf("fetch = %+v, %v; want the pinned commit's folder", tree, err)
	}

	g.set(pinned, map[string]entry{lintMD: {body: "pinned"}}, head)

	if _, err := g.source(t, skillssh.Config{}).Fetch(t.Context(), lint(pinned)); err == nil {
		t.Error("an answer for another commit was accepted")
	}

	if _, err := g.source(t, skillssh.Config{}).Fetch(t.Context(), lint("3333333333333333333333333333333333333333")); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("unknown commit = %v, want not found", err)
	}
}

// Outside the conventional folders the one folder named after the skill is found; two are
// ambiguous.
func TestFetch_FindsTheSkillAnywhereOnce(t *testing.T) {
	t.Parallel()

	g := newFakeGitHub(t)
	g.set(head, map[string]entry{"plugins/dev/skills/lint/SKILL.md": {body: "deep"}}, "")

	tree, err := g.source(t, skillssh.Config{}).Fetch(t.Context(), lint(""))
	if err != nil || string(tree.Files["SKILL.md"]) != "deep" {
		t.Fatalf("fetch = %+v, %v; want the nested folder", tree, err)
	}

	g.set(head, map[string]entry{"a/lint/SKILL.md": {body: "1"}, "b/lint/SKILL.md": {body: "2"}}, "")

	if _, err := g.source(t, skillssh.Config{}).Fetch(t.Context(), lint("")); err == nil || !strings.Contains(err.Error(), "2 folders") {
		t.Errorf("two candidates = %v, want ambiguous", err)
	}

	g.set(head, map[string]entry{"lint/README.md": {body: "no skill file"}}, "")

	if _, err := g.source(t, skillssh.Config{}).Fetch(t.Context(), lint("")); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("no SKILL.md = %v, want not found", err)
	}
}

// The limits and the file rules are checked from the tree, before anything is downloaded.
func TestFetch_RefusesBeforeDownloading(t *testing.T) {
	t.Parallel()

	many := map[string]entry{lintMD: {body: "x"}}
	for i := range domain.MaxSkillFiles {
		many[fmt.Sprintf("lint/refs/%03d.md", i)] = entry{body: strconv.Itoa(i)}
	}

	tests := []struct {
		name     string
		files    map[string]entry
		tooLarge bool
	}{
		{name: "too many files", files: many, tooLarge: true},
		{name: "too many bytes", files: map[string]entry{lintMD: {body: "x", size: domain.MaxSkillBytes + 1}}, tooLarge: true},
		{name: "a symlink", files: map[string]entry{lintMD: {body: "x"}, "lint/link": {body: "/etc/passwd", mode: "120000"}}},
		{name: "a submodule", files: map[string]entry{lintMD: {body: "x"}, "lint/vendor": {body: "x", mode: "160000"}}},
		{name: "a path out of the folder", files: map[string]entry{lintMD: {body: "x"}, "lint/a/../../b": {body: "x"}}},
		{name: "a path with a space", files: map[string]entry{lintMD: {body: "x"}, "lint/my notes.md": {body: "x"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newFakeGitHub(t)
			g.set(head, tt.files, "")

			_, err := g.source(t, skillssh.Config{}).Fetch(t.Context(), lint(""))
			if err == nil || errors.Is(err, ports.ErrTooLarge) != tt.tooLarge || g.blobRequests() != 0 {
				t.Errorf("fetch = %v after %d downloads; want a refusal (too large: %v) before any", err, g.blobRequests(), tt.tooLarge)
			}
		})
	}
}

// A blob that is not what its tree entry says is refused.
func TestFetch_ChecksEveryBlob(t *testing.T) {
	t.Parallel()

	g := newFakeGitHub(t)
	g.set(head, map[string]entry{lintMD: {body: "x", size: 2}}, "")

	if _, err := g.source(t, skillssh.Config{}).Fetch(t.Context(), lint("")); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("fetch = %v, want the blob refused", err)
	}
}

// A redirect is followed only on the host asked, so the token never leaves it, and the token is
// scrubbed from the error even when the redirect's URL carries it.
func TestFetch_NoRedirectOffHostAndNoTokenInErrors(t *testing.T) {
	t.Parallel()

	var elsewhere sync.Map

	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		elsewhere.Store(r.URL.Path, r.Header.Get("Authorization"))
	}))
	t.Cleanup(other.Close)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/steal?t="+token, http.StatusFound)
	}))
	t.Cleanup(api.Close)

	s, err := skillssh.New(skillssh.Config{GitHubAPIURL: api.URL, GitHubToken: token})
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.Fetch(t.Context(), lint(""))
	if err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("fetch = %v, want an error without the token", err)
	}

	elsewhere.Range(func(k, _ any) bool {
		t.Errorf("the other host was asked for %v", k)
		return true
	})
}

func TestSearch_MapsTheDirectory(t *testing.T) {
	t.Parallel()

	var gotAuth, gotQuery string

	dir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/search" {
			http.NotFound(w, r)
			return
		}

		gotAuth, gotQuery = r.Header.Get("Authorization"), r.URL.RawQuery

		gh := func(slug string, installs int) map[string]any {
			return map[string]any{"slug": slug, "source": tfSource, "sourceType": "github", "installs": installs}
		}

		writeJSON(w, map[string]any{"data": []map[string]any{
			gh("style", 20),
			{"slug": "docs", "source": "docs.example.com", "sourceType": "well-known", "installs": 99},
			gh("..", 98), gh("modules", 50), gh("style", 20), gh("test", 5),
		}})
	}))
	t.Cleanup(dir.Close)

	s, err := skillssh.New(skillssh.Config{DirectoryURL: dir.URL, DirectoryToken: "dir-token", GitHubToken: token})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.Search(t.Context(), "terraform", 2)
	if err != nil {
		t.Fatal(err)
	}

	want := []ports.SkillListing{
		{Ref: ports.SkillRef{Owner: "acme", Repo: "tf", Skill: "modules"}, Installs: 50},
		{Ref: ports.SkillRef{Owner: "acme", Repo: "tf", Skill: "style"}, Installs: 20},
	}
	if !slices.Equal(got, want) {
		t.Errorf("search = %+v, want %+v", got, want)
	}

	if gotAuth != "Bearer dir-token" || gotQuery != "limit=2&q=terraform" {
		t.Errorf("request carried %q, query %q; want the directory's token only", gotAuth, gotQuery)
	}
}

func TestSearch_NeedsAToken(t *testing.T) {
	t.Parallel()

	dir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(dir.Close)

	s, err := skillssh.New(skillssh.Config{DirectoryURL: dir.URL})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Search(t.Context(), "terraform", 5); err == nil || !strings.Contains(err.Error(), "GOAL_SKILLS_DIRECTORY_TOKEN") {
		t.Errorf("search = %v, want a hint at the token", err)
	}
}

func TestNew_RefusesPlainHTTPOffThisMachine(t *testing.T) {
	t.Parallel()

	for _, cfg := range []skillssh.Config{
		{GitHubAPIURL: "http://api.example.com"},
		{DirectoryURL: "http://skills.example.com"},
		{GitHubAPIURL: "ftp://127.0.0.1"},
		{GitHubAPIURL: "https://user:pw@api.example.com"}, //nolint:gosec // credentials in a URL are what New must refuse
	} {
		if _, err := skillssh.New(cfg); err == nil {
			t.Errorf("New(%+v) succeeded", cfg)
		}
	}
}
