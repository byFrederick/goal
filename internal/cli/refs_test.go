package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

// factoryGoal is the factory most refGoald items belong to.
const factoryGoal = "goal"

// refGoald knows factories goal and sre; goal has items #1 to #3, sre #3 and #4. It records
// every request path as sent, so tests see how references travel.
type refGoald struct {
	srv   *httptest.Server
	mu    sync.Mutex
	paths []string
}

func newRefGoald(t *testing.T) *refGoald {
	t.Helper()

	g := &refGoald{}
	items := map[string]bool{"goal#1": true, "goal#2": true, "goal#3": true, "sre#3": true, "sre#4": true}
	item := func(ref string) gen.Item {
		return gen.Item{Id: "id-" + ref, Ref: ref, State: "done", Station: "done"}
	}
	reply := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orgs", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, gen.OrgList{Orgs: []gen.OrgSummary{{Id: "019a1c00-0000-7000-8000-0000000000aa", Slug: orgAcme, Name: "Acme", Mark: "Ac"}}})
	})
	mux.HandleFunc("GET /v1/orgs/acme/factories", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, gen.FactoryList{Factories: []gen.Factory{{Name: factoryGoal}, {Name: "sre"}}})
	})
	mux.HandleFunc("GET /v1/orgs/acme/items/{item}", func(w http.ResponseWriter, r *http.Request) {
		if !items[r.PathValue("item")] {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(gen.Problem{Title: "Not Found", Status: http.StatusNotFound})

			return
		}

		reply(w, http.StatusOK, item(r.PathValue("item")))
	})
	mux.HandleFunc("POST /v1/orgs/acme/items/{item}/{verb}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, item(r.PathValue("item")))
	})

	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.paths = append(g.paths, r.Method+" "+r.URL.EscapedPath())
		g.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(g.srv.Close)

	return g
}

func TestItemRef_BareNumbersResolveToTheOneFactoryThatHasThem(t *testing.T) {
	t.Parallel()

	g := newRefGoald(t)

	c, err := newAPI(testEnv(t, g.srv.URL, nil))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		arg, factory, want, wantErr string
	}{
		{arg: "2", want: "goal#2"},
		{arg: "4", want: "sre#4"},
		{arg: "3", wantErr: "goal#3, sre#3"},
		{arg: "3", factory: "sre", want: "sre#3"},
		{arg: "9", wantErr: "no factory has an item #9"},
		{arg: "goal#3", want: "goal#3"},
		{arg: "0198f3c2-7a1b-7c3d-8e4f-5a6b7c8d9e0f", want: "0198f3c2-7a1b-7c3d-8e4f-5a6b7c8d9e0f"},
	}

	for _, tt := range tests {
		got, err := itemRef(t.Context(), orgClient{ClientWithResponses: c, org: orgAcme}, tt.arg, tt.factory)

		switch {
		case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
			t.Errorf("itemRef(%s) = %q, %v; want an error mentioning %q", tt.arg, got, err, tt.wantErr)
		case tt.wantErr == "" && (err != nil || got != tt.want):
			t.Errorf("itemRef(%s, --factory %q) = %q, %v; want %q", tt.arg, tt.factory, got, err, tt.want)
		}
	}
}

func TestDecide_RetryAndMergeByReference(t *testing.T) {
	t.Parallel()

	g := newRefGoald(t)
	env := testEnv(t, g.srv.URL, nil) // no --org, GOAL_ORG or config: the install's only org

	for _, verb := range []string{"retry", "merge"} {
		var buf bytes.Buffer
		if err := decide(t.Context(), &printer{w: &buf}, env, verb, []string{"2"}); err != nil {
			t.Fatalf("goal %s 2: %v", verb, err)
		}

		if want := "✓ " + done[verb] + " goal#2; now done at done\n"; buf.String() != want {
			t.Errorf("goal %s 2 printed %q, want %q", verb, buf.String(), want)
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// The # of a reference travels percent-encoded, so it is not taken for a URL fragment.
	for _, want := range []string{"POST /v1/orgs/acme/items/goal%232/retry", "POST /v1/orgs/acme/items/goal%232/merge"} {
		if !strings.Contains(strings.Join(g.paths, "\n"), want) {
			t.Errorf("requests %v; want %s", g.paths, want)
		}
	}
}
