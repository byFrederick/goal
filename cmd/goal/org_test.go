package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// cmdOrg is the goal org command.
const cmdOrg = "org"

// fromConfig is the org a test writes to the config file.
const fromConfig = "from-config"

// flagName is goal org's --name.
const flagName = "--name"

// orgAcme is the stub installs' first organization.
const orgAcme = "acme"

// testEnv is a CLI environment: goald at api, a config directory of the test's own (never the
// user's), and extra variables.
func testEnv(t *testing.T, api string, extra map[string]string) func(string) string {
	t.Helper()

	env := map[string]string{"GOAL_API_URL": api, "XDG_CONFIG_HOME": t.TempDir()}
	maps.Copy(env, extra)

	return func(k string) string { return env[k] }
}

// orgGoald serves the organizations of a stub install and records what it was sent.
type orgGoald struct {
	srv *httptest.Server

	mu       sync.Mutex
	orgs     []gen.OrgSummary
	requests []string
}

func newOrgGoald(t *testing.T, slugs ...string) *orgGoald {
	t.Helper()

	g := &orgGoald{}
	for i, s := range slugs {
		g.orgs = append(g.orgs, gen.OrgSummary{Id: "019a1c00-0000-7000-8000-00000000000" + string(rune('1'+i)), Slug: s, Name: strings.ToUpper(s[:1]) + s[1:], Mark: "Ac"})
	}

	reply := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/orgs", func(w http.ResponseWriter, _ *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()

		reply(w, http.StatusOK, gen.OrgList{Orgs: g.orgs})
	})
	mux.HandleFunc("GET /v1/orgs/{org}", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()

		for _, o := range g.orgs {
			if o.Slug == r.PathValue("org") || o.Id == r.PathValue("org") {
				reply(w, http.StatusOK, gen.Org{Id: o.Id, Slug: o.Slug, Name: o.Name, Mark: o.Mark})
				return
			}
		}

		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(gen.Problem{Title: "Organization not found", Status: http.StatusNotFound})
	})
	mux.HandleFunc("POST /v1/orgs", func(w http.ResponseWriter, r *http.Request) {
		var n gen.NewOrg

		_ = json.NewDecoder(r.Body).Decode(&n)
		reply(w, http.StatusCreated, gen.Org{Id: "019a1c00-0000-7000-8000-0000000000ff", Slug: n.Slug, Name: n.Name, Mark: deref(n.Mark)})
	})

	mux.HandleFunc("PATCH /v1/orgs/{org}/settings", func(w http.ResponseWriter, r *http.Request) {
		var p gen.OrgSettingsPatch

		_ = json.NewDecoder(r.Body).Decode(&p)

		s := gen.OrgSettings{Name: "Acme", Slug: r.PathValue("org"), Mark: "Ac", Slots: gen.OrgSlots{Factories: []gen.FactorySlots{}}}
		if p.Name != nil {
			s.Name = *p.Name
		}

		if p.Slots != nil {
			s.Slots.Max = p.Slots.Max
		}

		reply(w, http.StatusOK, s)
	})
	mux.HandleFunc("DELETE /v1/orgs/{org}", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusAccepted, gen.OrgDeleted{PurgeAfter: time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)})
	})

	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))

		g.mu.Lock()
		g.requests = append(g.requests, r.Method+" "+r.URL.Path+" "+string(body))
		g.mu.Unlock()

		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(g.srv.Close)

	return g
}

func TestResolveOrg_FlagThenEnvThenConfigThenTheOnlyOrg(t *testing.T) {
	t.Parallel()

	one := newOrgGoald(t, orgAcme)
	two := newOrgGoald(t, orgAcme, "example-labs")

	tests := []struct {
		name string
		api  *orgGoald
		// env is GOAL_ORG and the like; config, when set, is the config file's org.
		env     map[string]string
		config  string
		flag    string
		want    string
		wantErr string
	}{
		{name: "--org wins", api: two, env: map[string]string{"GOAL_ORG": "from-env"}, config: fromConfig, flag: "from-flag", want: "from-flag"},
		{name: "then GOAL_ORG", api: two, env: map[string]string{"GOAL_ORG": "from-env"}, config: fromConfig, want: "from-env"},
		{name: "then the config file", api: two, config: fromConfig, want: fromConfig},
		{name: "then the only org", api: one, want: orgAcme},
		{name: "else an error listing them", api: two, wantErr: "there are acme, example-labs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := testEnv(t, tt.api.srv.URL, tt.env)
			if tt.config != "" {
				if _, err := saveConfig(env, cliConfig{Org: tt.config}); err != nil {
					t.Fatal(err)
				}
			}

			c, err := connect(t.Context(), env, tt.flag)

			switch {
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("connect = %q, %v; want an error mentioning %q", c.org, err, tt.wantErr)
			case tt.wantErr == "" && (err != nil || c.org != tt.want):
				t.Errorf("connect = %q, %v; want %q", c.org, err, tt.want)
			}
		})
	}
}

func TestOrgUse_WritesTheConfigFileForItsOwnerOnly(t *testing.T) {
	t.Parallel()

	g := newOrgGoald(t, orgAcme, "example-labs")
	env := testEnv(t, g.srv.URL, nil)

	if _, err := saveConfig(env, cliConfig{APIURL: "http://127.0.0.1:9999"}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run(t.Context(), []string{cmdOrg, "use", "example-labs"}, &printer{w: &out}, env, nil); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(env("XDG_CONFIG_HOME"), "goal", "config.json")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("config file mode %o, want 600", mode)
	}

	cfg, err := loadConfig(env)
	if err != nil || cfg.Org != "example-labs" || cfg.APIURL != "http://127.0.0.1:9999" {
		t.Errorf("config = %+v, %v; want the org saved and api_url kept", cfg, err)
	}

	if err := run(t.Context(), []string{cmdOrg, "use", "nobody"}, &printer{w: &out}, env, nil); err == nil ||
		!strings.Contains(err.Error(), "Organization not found") {
		t.Errorf("goal org use nobody = %v, want goald's 404", err)
	}

	out.Reset()

	if err := run(t.Context(), []string{cmdOrg, "list"}, &printer{w: &out}, env, nil); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "* Example-labs") || strings.Contains(out.String(), "* Acme") {
		t.Errorf("goal org list =\n%s\nwant example-labs marked current", out.String())
	}
}

func TestOrgCreate_SendsTheSlugNameAndMark(t *testing.T) {
	t.Parallel()

	g := newOrgGoald(t, orgAcme)
	env := testEnv(t, g.srv.URL, nil)

	var out bytes.Buffer
	if err := run(t.Context(), []string{cmdOrg, "create", "example-labs", "--name", "Example Labs", "--mark", "EL"}, &printer{w: &out}, env, nil); err != nil {
		t.Fatal(err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	want := `POST /v1/orgs {"mark":"EL","name":"Example Labs","slug":"example-labs"}`
	if len(g.requests) != 1 || strings.TrimSpace(g.requests[0]) != want {
		t.Errorf("requests %q, want %s", g.requests, want)
	}

	for _, args := range [][]string{{cmdOrg, "create", "x-y"}, {cmdOrg, "create", "--name", "N"}, {cmdOrg}, {cmdOrg, "rename"}} {
		if err := run(t.Context(), args, &printer{w: &out}, env, nil); !errors.Is(err, errUsage) {
			t.Errorf("goal %v = %v, want a usage error", args, err)
		}
	}
}

func TestUsage_DecisionsTakeNoBy(t *testing.T) {
	t.Parallel()

	if strings.Contains(usage, "--by") {
		t.Error("the usage still offers --by; decisions are the local user's")
	}
}

// sent returns the requests the stub got whose method and path start with prefix.
func (g *orgGoald) sent(prefix string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	var out []string

	for _, r := range g.requests {
		if strings.HasPrefix(r, prefix) {
			out = append(out, strings.TrimSpace(r))
		}
	}

	return out
}

func TestOrgUpdate_PatchesOnlyTheNamedSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args []string
		want string
	}{
		{[]string{flagName, "Acme Co", "--mark", "AC"}, `PATCH /v1/orgs/acme/settings {"mark":"AC","name":"Acme Co"}`},
		{[]string{"--slug", "acme-co"}, `PATCH /v1/orgs/acme/settings {"slug":"acme-co"}`},
		{[]string{"--max-slots", "6"}, `PATCH /v1/orgs/acme/settings {"slots":{"max":6}}`},
		{[]string{"--max-slots", "0"}, `PATCH /v1/orgs/acme/settings {"slots":{"max":null}}`},
	}

	for _, tt := range tests {
		g := newOrgGoald(t, orgAcme)
		env := testEnv(t, g.srv.URL, nil)

		var out bytes.Buffer
		if err := run(t.Context(), append([]string{cmdOrg, "update", orgAcme}, tt.args...), &printer{w: &out}, env, nil); err != nil {
			t.Fatalf("goal org update %v: %v", tt.args, err)
		}

		if got := g.sent("PATCH"); len(got) != 1 || got[0] != tt.want {
			t.Errorf("goal org update %v sent %q, want %s", tt.args, got, tt.want)
		}
	}

	g := newOrgGoald(t, orgAcme)

	for _, args := range [][]string{{cmdOrg, "update", orgAcme}, {cmdOrg, "update", flagName, "N"}} {
		if err := run(t.Context(), args, &printer{w: io.Discard}, testEnv(t, g.srv.URL, nil), nil); !errors.Is(err, errUsage) {
			t.Errorf("goal %v = %v, want a usage error", args, err)
		}
	}
}

func TestOrgDelete_AsksForTheNameUnlessYes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		args  []string
		typed string
		sends bool
	}{
		{name: "the name typed", args: []string{orgAcme}, typed: "Acme\n", sends: true},
		{name: "another name typed", args: []string{orgAcme}, typed: "acme\n"},
		{name: "nothing typed", args: []string{orgAcme}},
		{name: "--yes", args: []string{orgAcme, "--yes"}, sends: true},
	}

	for _, tt := range tests {
		g := newOrgGoald(t, orgAcme)

		var out bytes.Buffer

		err := run(t.Context(), append([]string{cmdOrg, "delete"}, tt.args...), &printer{w: &out, in: strings.NewReader(tt.typed)},
			testEnv(t, g.srv.URL, nil), nil)

		got := g.sent("DELETE")

		switch {
		case tt.sends && (err != nil || len(got) != 1 || got[0] != `DELETE /v1/orgs/acme {"confirm":"Acme"}`):
			t.Errorf("%s: %v, sent %q; want the delete confirmed with the name", tt.name, err, got)
		case !tt.sends && (err == nil || len(got) != 0):
			t.Errorf("%s: %v, sent %q; want an error and nothing sent", tt.name, err, got)
		}
	}
}
