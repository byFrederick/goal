package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
)

const cmdKey = "key"

// The key the tests create, and the verb.
const (
	ciKey      = "ci-deploy"
	verbCreate = "create"
	scopeRead  = "org:read"
)

// flagScope is goal key create's --scope.
const flagScope = "--scope"

// createKey runs goal key create for ci-deploy and returns its stdout and stderr.
func createKey(t *testing.T, env func(string) string) (stdout, stderr string) {
	t.Helper()

	var out, notes bytes.Buffer

	args := []string{cmdKey, verbCreate, ciKey, flagScope, "factory:read,item:create", flagScope, scopeRead, "--role", "approver", "--expires", "30d"}
	if err := run(t.Context(), args, &printer{w: &out, e: &notes}, env, nil); err != nil {
		t.Fatal(err)
	}

	return out.String(), notes.String()
}

// hashOf is how goald stores a key.
func hashOf(secret string) string {
	sum := sha256.Sum256([]byte(secret))

	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// goal key create prints the secret alone on stdout and the rest on stderr, and the key carries
// every --scope (repeated or comma-separated), the role and the factories (all by default).
func TestKey_Create(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	stdout, stderr := createKey(t, testEnv(t, g.srv.URL, nil))

	secret := strings.TrimSuffix(stdout, "\n")
	if !regexp.MustCompile(`^goal_sk_[A-Za-z0-9_-]{43}$`).MatchString(secret) || !strings.Contains(stderr, "GOAL_API_KEY") {
		t.Fatalf("stdout %q, stderr %q; want the secret alone on stdout and how to store it on stderr", stdout, stderr)
	}

	k, ok := g.keys.Key(hashOf(secret))
	got := struct {
		name      string
		scopes    []domain.Permission
		ceiling   domain.FactoryRole
		factories []domain.FactoryID
	}{k.Name, k.Scopes, k.Ceiling, k.Factories}

	if !ok || got.name != ciKey || !slices.Equal(got.scopes, []domain.Permission{"factory:read", "item:create", scopeRead}) ||
		got.ceiling != domain.FactoryApprover || got.factories != nil {
		t.Errorf("stored key = %+v, %v; want ci-deploy with its three scopes, approver at most, every factory", got, ok)
	}
}

// goal key list shows a key by its hint; goal key revoke takes its name (case ignored), after
// which it cannot be revoked by name again.
func TestKey_ListRevoke(t *testing.T) {
	t.Parallel()

	g := newDefGoald(t)
	env := testEnv(t, g.srv.URL, nil)
	stdout, _ := createKey(t, env)
	secret := strings.TrimSuffix(stdout, "\n")

	var out bytes.Buffer
	if err := run(t.Context(), []string{cmdKey, subList}, &printer{w: &out}, env, nil); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{ciKey, secret[:12] + "…", "at most approver", "all factories", "last used never"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("list = %q; want %q in it", out.String(), want)
		}
	}

	if strings.Contains(out.String(), secret[12:]) {
		t.Errorf("list = %q; it shows the secret", out.String())
	}

	out.Reset()

	if err := run(t.Context(), []string{cmdKey, "revoke", "CI-DEPLOY"}, &printer{w: &out}, env, nil); err != nil || !strings.Contains(out.String(), "revoked ci-deploy") {
		t.Fatalf("revoke = %v, %q", err, out.String())
	}

	if k, _ := g.keys.Key(hashOf(secret)); k.RevokedAt.IsZero() {
		t.Error("the key is not revoked")
	}

	if err := run(t.Context(), []string{cmdKey, "revoke", ciKey}, &printer{w: &out}, env, nil); err == nil || !strings.Contains(err.Error(), "no live API key") {
		t.Errorf("revoking it again = %v; want no live key of that name", err)
	}
}

func TestKey_Usage(t *testing.T) {
	t.Parallel()

	env := testEnv(t, "http://127.0.0.1:1", nil)

	for _, args := range [][]string{
		{cmdKey},
		{cmdKey, "nope"},
		{cmdKey, verbCreate, "ci"},
		{cmdKey, verbCreate, flagScope, scopeRead},
		{cmdKey, subList, "extra"},
		{cmdKey, "revoke"},
	} {
		if err := run(t.Context(), args, &printer{w: &bytes.Buffer{}}, env, nil); !errors.Is(err, errUsage) {
			t.Errorf("%v = %v; want usage", args, err)
		}
	}

	for _, v := range []string{"0d", "91d", "a week"} {
		err := run(t.Context(), []string{cmdKey, verbCreate, "ci", flagScope, scopeRead, "--expires", v}, &printer{w: &bytes.Buffer{}}, env, nil)
		if !errors.Is(err, errExpires) {
			t.Errorf("--expires %s = %v; want it refused before any request", v, err)
		}
	}
}

// keyGoald records the Authorization header of every request it serves: one organization, acme.
type keyGoald struct {
	srv *httptest.Server

	mu   sync.Mutex
	seen []string
}

func newKeyGoald(t *testing.T) *keyGoald {
	t.Helper()

	g := &keyGoald{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.seen = append(g.seen, r.Header.Get("Authorization"))
		g.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gen.OrgList{Orgs: []gen.OrgSummary{{Id: "019a1c00-0000-7000-8000-000000000001", Slug: orgAcme, Name: strings.ToUpper(orgAcme[:1]) + orgAcme[1:]}}})
	}))
	t.Cleanup(g.srv.Close)

	return g
}

func (g *keyGoald) headers() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.seen)
}

// Spec 3 §9: a key or personal token stored with `goal login --token` acts in its own
// organization, like GOAL_API_KEY, not the config file's; a session from `goal login` still acts
// in the one config.json names.
func TestStoredToken_ActsInItsOwnOrganization(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		cred credential
		want string
	}{
		{"a personal token", credential{Kind: kindToken, Token: testPAT}, orgAcme},
		{"an API key", credential{Kind: kindToken, Token: testKey}, orgAcme},
		{"a session", credential{Kind: kindSession, Token: testSession}, fromConfig},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			g := newKeyGoald(t)
			env := testEnv(t, g.srv.URL, nil)

			if _, err := saveConfig(env, cliConfig{Org: fromConfig, CredentialStore: storeFile}); err != nil {
				t.Fatal(err)
			}

			store, err := openStore(env, storeFile, nil)
			if err != nil {
				t.Fatal(err)
			}

			origin, err := originOf(g.srv.URL)
			if err != nil {
				t.Fatal(err)
			}

			if err := store.put(origin, c.cred); err != nil {
				t.Fatal(err)
			}

			got, err := connect(t.Context(), env, "")
			if err != nil || got.org != c.want {
				t.Errorf("org with %s = %q, %v; want %q", c.name, got.org, err, c.want)
			}
		})
	}
}

// Spec 3 §9: with GOAL_API_KEY a command acts in the key's own organization (the only one it
// lists), not the config file's, and every request carries the key.
func TestAPIKeyEnv_ActsInTheKeysOrganization(t *testing.T) {
	t.Parallel()

	g := newKeyGoald(t)
	env := testEnv(t, g.srv.URL, map[string]string{envAPIKey: testKey})

	if _, err := saveConfig(env, cliConfig{Org: fromConfig}); err != nil {
		t.Fatal(err)
	}

	c, err := connect(t.Context(), env, "")
	if err != nil || c.org != orgAcme {
		t.Errorf("org with a key = %q, %v; want the key's own (the only one listed), not the config file's", c.org, err)
	}

	if seen := g.headers(); len(seen) != 1 || seen[0] != "Bearer "+testKey {
		t.Errorf("Authorization = %q; want the key as a bearer", seen)
	}
}
