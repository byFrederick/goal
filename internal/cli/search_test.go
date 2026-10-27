package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

// searchFactory is the factory the search tests' hits are in.
const searchFactory = "web"

// searchGoald answers GET /v1/orgs/acme/search with status and body, and records the query it
// was sent.
func searchGoald(t *testing.T, status int, contentType string, body []byte) (*httptest.Server, func() url.Values) {
	t.Helper()

	var (
		mu    sync.Mutex
		query url.Values
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orgs/acme/search", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		query = r.URL.Query()
		mu.Unlock()

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, func() url.Values {
		mu.Lock()
		defer mu.Unlock()

		return query
	}
}

func searchJSON(t *testing.T, v gen.OrgSearch) []byte {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func goalSearch(t *testing.T, api string, args ...string) (string, error) {
	t.Helper()

	var buf bytes.Buffer

	err := run(t.Context(), append([]string{"search"}, args...), &printer{w: &buf}, testEnv(t, api, map[string]string{envOrg: orgAcme}), nil)

	return buf.String(), err
}

// The groups print in the order Items, Agents, Factories, Connections, an empty one left out
// (Factories here); q and --limit reach goald.
func TestSearch_TextGroupsInOrder(t *testing.T) {
	t.Parallel()

	srv, sent := searchGoald(t, http.StatusOK, "application/json", searchJSON(t, gen.OrgSearch{
		Items: []gen.SearchItem{
			{FactoryId: "f1", FactoryName: searchFactory, Id: "i1", Number: 2, State: gen.ItemStateInProgress, Station: "build", Title: "Fix the login page"},
			{FactoryId: "f1", FactoryName: searchFactory, Id: "i2", Number: 14, State: gen.ItemStateDone, Station: "done", Title: "Log in with a key"},
		},
		Agents:      []gen.AgentHit{{FactoryId: "f1", FactoryName: searchFactory, Name: "login-coder", Stations: []string{"build", "rework"}}},
		Factories:   []gen.FactoryHit{},
		Connections: []gen.ConnectionHit{{Kind: "github", Name: "login-bot"}},
	}))

	out, err := goalSearch(t, srv.URL, "login", "--limit", "3")
	if err != nil {
		t.Fatalf("goal search login = %v", err)
	}

	if q := sent(); q.Get("q") != "login" || q.Get("limit") != "3" {
		t.Errorf("goald was sent q=%q limit=%q; want login and 3", q.Get("q"), q.Get("limit"))
	}

	checkGolden(t, "search-results", out)

	// A factory hit, alone, prints its name and title.
	srv, _ = searchGoald(t, http.StatusOK, "application/json", searchJSON(t, gen.OrgSearch{
		Factories: []gen.FactoryHit{{Id: "f1", Name: searchFactory, Title: "Operations"}},
	}))

	if out, err := goalSearch(t, srv.URL, searchFactory); err != nil || out != "Factories\n  web  Operations\n" {
		t.Errorf("goal search web = %q, %v; want the one factory", out, err)
	}
}

func TestSearch_NoResults(t *testing.T) {
	t.Parallel()

	srv, _ := searchGoald(t, http.StatusOK, "application/json", searchJSON(t, gen.OrgSearch{
		Items: []gen.SearchItem{}, Agents: []gen.AgentHit{}, Factories: []gen.FactoryHit{}, Connections: []gen.ConnectionHit{},
	}))

	if out, err := goalSearch(t, srv.URL, "nothing"); err != nil || out != "No results\n" {
		t.Errorf("goal search nothing = %q, %v; want No results", out, err)
	}
}

// --json writes goald's body as it came, spacing and key order included.
func TestSearch_JSONAsSent(t *testing.T) {
	t.Parallel()

	body := []byte(`{"items": [], "agents":[{"name":"coder","factory_id":"f1","factory_name":"ops","stations":["build"]}],
 "factories":[],"connections":[]}`)
	srv, _ := searchGoald(t, http.StatusOK, "application/json", body)

	if out, err := goalSearch(t, srv.URL, "coder", "--json"); err != nil || out != string(body) {
		t.Errorf("goal search coder --json = %q, %v; want %q", out, err, body)
	}
}

// A 403 is an access refusal with the problem's text, which Run exits 77 for.
func TestSearch_Forbidden(t *testing.T) {
	t.Parallel()

	detail := "you do not hold org:read"

	b, err := json.Marshal(gen.Problem{Title: "Forbidden", Status: http.StatusForbidden, Code: new("forbidden"), Detail: &detail})
	if err != nil {
		t.Fatal(err)
	}

	srv, _ := searchGoald(t, http.StatusForbidden, "application/problem+json", b)

	_, err = goalSearch(t, srv.URL, "login")

	var refused *accessError
	if !errors.As(err, &refused) || err.Error() != "Forbidden: you do not hold org:read" {
		t.Errorf("goal search on a 403 = %v; want the problem as an access refusal", err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run(t.Context(), []string{"search", "login"}, bytes.NewReader(nil), &stdout, &stderr,
		testEnv(t, srv.URL, map[string]string{envOrg: orgAcme}), fixedBuildInfo); code != exitNoPerm {
		t.Errorf("Run exit = %d; want %d", code, exitNoPerm)
	}
}

func TestSearch_NeedsOneQuery(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{nil, {"a", "b"}} {
		if _, err := goalSearch(t, "http://127.0.0.1:1", args...); !errors.Is(err, errUsage) {
			t.Errorf("goal search %q = %v; want the usage", args, err)
		}
	}
}
