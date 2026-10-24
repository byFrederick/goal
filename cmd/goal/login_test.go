package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
)

// cmdWhoami is the goal whoami command.
const cmdWhoami = "whoami"

// authGoald is a stub goald under GOAL_AUTH=session (or none) with its identity service: the
// device grant answers polls from a script, /v1/me knows the CLI's credentials, and every request
// is recorded.
type authGoald struct {
	srv *httptest.Server

	mu   sync.Mutex
	mode gen.AuthMode
	// polls are the answers to POST /api/auth/device/token, one per poll; the last repeats.
	// "ok" hands out testSession.
	polls []string
	// me is who each bearer credential is.
	me map[string]gen.Me
	// signOut is the status of /api/auth/sign-out and /revoke-sessions.
	signOut  int
	requests []authRequest
}

type authRequest struct {
	method, path, auth string
	body               map[string]string
}

func newAuthGoald(t *testing.T) *authGoald {
	t.Helper()

	g := &authGoald{
		mode: gen.Session, polls: []string{"ok"}, signOut: http.StatusOK,
		me: map[string]gen.Me{
			testSession: {
				Kind: gen.MeKindUser, Mode: gen.Session, User: &gen.User{Id: "u1", Name: "F. Cruz", Email: "f.cruz@example.com"},
				PlatformAdmin: true, Session: &gen.MeSession{ExpiresAt: time.Date(2026, 10, 31, 14, 2, 0, 0, time.Local)},
			},
			testPAT: {
				Kind: gen.MeKindUser, Mode: gen.Session, User: &gen.User{Id: "u1", Name: "F. Cruz", Email: "f.cruz@example.com"},
				Token: &gen.MeToken{
					Id: "t1", Name: "laptop", OrgId: "o1", Scopes: []string{"org:read", "item:create"},
					ExpiresAt: time.Date(2026, 11, 23, 12, 0, 0, 0, time.Local),
				},
			},
			testKey: {Kind: gen.MeKindService, Mode: gen.Session, Key: &gen.MeKey{Id: "k1", Name: "ci", OrgId: "o1"}},
		},
	}

	reply := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	bearer := func(r *http.Request) string { return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/auth/config", func(w http.ResponseWriter, _ *http.Request) {
		g.mu.Lock()
		mode := g.mode
		g.mu.Unlock()
		reply(w, http.StatusOK, gen.AuthConfig{Mode: mode, Methods: []gen.SignInMethod{}})
	})
	mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		me, ok := g.me[bearer(r)]
		if !ok {
			reply(w, http.StatusUnauthorized, gen.Problem{Title: "Not signed in", Status: http.StatusUnauthorized, Code: new("unauthenticated")})

			return
		}

		reply(w, http.StatusOK, me)
	})
	mux.HandleFunc("GET /v1/orgs", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := g.me[bearer(r)]; !ok {
			reply(w, http.StatusUnauthorized, gen.Problem{Title: "Not signed in", Status: http.StatusUnauthorized, Code: new("unauthenticated")})

			return
		}

		owner := gen.OrgRole("owner")
		reply(w, http.StatusOK, gen.OrgList{Orgs: []gen.OrgSummary{{Id: "o1", Slug: orgAcme, Name: "Acme Corp", Mark: "Ac", Role: &owner}}})
	})
	mux.HandleFunc("POST /api/auth/device/code", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, map[string]any{
			"device_code": "device-code-1", "user_code": "ABCDEFGH", "verification_uri": g.srv.URL + "/device",
			"verification_uri_complete": g.srv.URL + "/device?user_code=ABCDEFGH", "expires_in": 600, "interval": 1,
		})
	})
	mux.HandleFunc("POST /api/auth/device/token", func(w http.ResponseWriter, _ *http.Request) {
		g.mu.Lock()

		answer := g.polls[0]
		if len(g.polls) > 1 {
			g.polls = g.polls[1:]
		}

		g.mu.Unlock()

		if answer == "ok" {
			reply(w, http.StatusOK, map[string]any{"access_token": testSession, "token_type": "Bearer", "expires_in": 604800})

			return
		}

		reply(w, http.StatusBadRequest, map[string]string{"error": answer, "error_description": "…"})
	})

	for _, path := range []string{"POST /api/auth/sign-out", "POST /api/auth/revoke-sessions"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			g.mu.Lock()
			status := g.signOut
			g.mu.Unlock()
			reply(w, status, map[string]bool{"success": status == http.StatusOK})
		})
	}

	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))

		var body map[string]string

		_ = json.Unmarshal(raw, &body)

		g.mu.Lock()
		g.requests = append(g.requests, authRequest{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"), body: body})
		g.mu.Unlock()

		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(g.srv.Close)

	return g
}

// set changes the stub's script under its lock.
func (g *authGoald) set(change func(*authGoald)) {
	g.mu.Lock()
	defer g.mu.Unlock()

	change(g)
}

// sent lists the requests to path.
func (g *authGoald) sent(path string) []authRequest {
	g.mu.Lock()
	defer g.mu.Unlock()

	var out []authRequest

	for _, r := range g.requests {
		if r.path == path {
			out = append(out, r)
		}
	}

	return out
}

func (g *authGoald) origin(t *testing.T) string {
	t.Helper()

	o, err := originOf(g.srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	return o
}

// browserScript is a $BROWSER that writes the address it was asked to open to the returned file.
func browserScript(t *testing.T) (string, string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the stand-in browser is a shell script")
	}

	dir := t.TempDir()
	script, opened := filepath.Join(dir, "browser"), filepath.Join(dir, "opened")

	if err := os.WriteFile(script, []byte("printf '%s' \"$1\" > \""+opened+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The shell reads the script: executing a file just written can fail with "text file busy"
	// while parallel tests fork.
	return "/bin/sh " + script, opened
}

// loginToFile signs in to g with goal login --store file and a stand-in browser, and returns the
// CLI's environment, what goal login printed and the file the browser wrote the page it opened to.
func loginToFile(t *testing.T, g *authGoald) (func(string) string, string, string) {
	t.Helper()

	script, opened := browserScript(t)
	getenv := testEnv(t, g.srv.URL, map[string]string{"BROWSER": script})

	var buf bytes.Buffer
	if err := login(t.Context(), &printer{w: &buf}, getenv, []string{"--store", "file"}, newFakeKeychain()); err != nil {
		t.Fatalf("login: %v\n%s", err, buf.String())
	}

	return getenv, buf.String(), opened
}

// Spec 3 §9: goal login runs the device flow: it asks for a code as the Goal CLI, says where to
// enter it, opens the browser there (never at the address with the code in it) and polls for the
// session as RFC 8628 says.
func TestLogin_DeviceFlow(t *testing.T) {
	t.Parallel()

	g := newAuthGoald(t)
	_, printed, opened := loginToFile(t, g)
	origin := g.origin(t)

	for _, want := range []string{"Open " + origin + "/device and enter ABCD-EFGH\n", "Signed in to " + origin + " as F. Cruz (f.cruz@example.com)"} {
		if !strings.Contains(printed, want) {
			t.Errorf("output %q lacks %q", printed, want)
		}
	}

	if page, err := os.ReadFile(opened); err != nil || string(page) != origin+"/device" { //nolint:gosec // the test's own file
		t.Errorf("the browser opened %q, %v; want %s/device exactly, without the code", page, err, origin)
	}

	if code := g.sent("/api/auth/device/code"); len(code) != 1 || code[0].body["client_id"] != cliClientID || code[0].auth != "" {
		t.Errorf("device code requests = %+v; want one, as goal-cli, with no credential", code)
	}

	poll := g.sent("/api/auth/device/token")
	if len(poll) != 1 || poll[0].body["grant_type"] != deviceGrant || poll[0].body["device_code"] != "device-code-1" ||
		poll[0].body["client_id"] != cliClientID {
		t.Errorf("token polls = %+v; want one RFC 8628 poll", poll)
	}
}

// With --store file the session goes to credentials.json, mode 0600, which config.json remembers;
// commands then send it, and only to its origin.
func TestLogin_FileStoreByOrigin(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("file modes are Unix's")
	}

	g := newAuthGoald(t)
	getenv, _, _ := loginToFile(t, g)
	origin := g.origin(t)

	path, _ := credentialsPath(getenv)
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials.json = %v, %v; want mode 0600", info, err)
	}

	if cfg, _ := loadConfig(getenv); cfg.CredentialStore != storeFile {
		t.Errorf("config credential_store = %q; want file remembered", cfg.CredentialStore)
	}

	var buf bytes.Buffer
	if err := run(t.Context(), []string{cmdWhoami}, &printer{w: &buf}, getenv, nil); err != nil {
		t.Fatalf("whoami: %v", err)
	}

	for _, want := range []string{"F. Cruz (f.cruz@example.com) on " + origin, "Platform admin", "Session expires Oct 31 14:02", "acme", "owner"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("whoami %q lacks %q", buf.String(), want)
		}
	}

	if me := g.sent("/v1/me"); me[len(me)-1].auth != "Bearer "+testSession {
		t.Errorf("whoami sent %q; want the stored session", me[len(me)-1].auth)
	}

	// Another API URL is another origin: nothing goes there.
	other := newAuthGoald(t)
	if err := run(t.Context(), []string{cmdWhoami}, &printer{w: io.Discard}, withEnv(getenv, "GOAL_API_URL", other.srv.URL), nil); !isSignedOut(err) {
		t.Errorf("whoami against another origin = %v; want not signed in", err)
	}

	if me := other.sent("/v1/me"); len(me) != 1 || me[0].auth != "" {
		t.Errorf("the other origin received %+v; want no credential", me)
	}
}

// Without --store, goal login keeps the session in the OS keychain (service goal, account the
// origin), and config.json remembers that; logging in again ends the session it replaces.
func TestLogin_KeychainByDefault(t *testing.T) {
	t.Parallel()

	g := newAuthGoald(t)
	script, _ := browserScript(t)
	getenv := testEnv(t, g.srv.URL, map[string]string{"BROWSER": script})
	kc := newFakeKeychain()
	origin := g.origin(t)

	if err := (keychainStore{kc: kc}).put(origin, credential{Kind: kindSession, Token: testSession2}); err != nil {
		t.Fatal(err)
	}

	if _, err := saveConfig(getenv, cliConfig{CredentialStore: storeKeychain}); err != nil {
		t.Fatal(err)
	}

	if err := login(t.Context(), &printer{w: io.Discard}, getenv, nil, kc); err != nil {
		t.Fatal(err)
	}

	if c, ok := kc.entry(t, origin); !ok || c.Token != testSession || c.Kind != kindSession {
		t.Errorf("keychain entry = %+v, %v; want the new session", c, ok)
	}

	if out := g.sent("/api/auth/sign-out"); len(out) != 1 || out[0].auth != "Bearer "+testSession2 {
		t.Errorf("sign-outs = %+v; want the replaced session ended", out)
	}

	if path, _ := credentialsPath(getenv); fileExists(path) {
		t.Error("goal login wrote credentials.json without --store file")
	}

	var buf bytes.Buffer
	if err := whoami(t.Context(), &printer{w: &buf}, getenv, nil, kc); err != nil || !strings.Contains(buf.String(), "the OS keychain") {
		t.Errorf("whoami = %v %q; want the keychain's session", err, buf.String())
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// withEnv is getenv with key set to value.
func withEnv(getenv func(string) string, key, value string) func(string) string {
	return func(k string) string {
		if k == key {
			return value
		}

		return getenv(k)
	}
}

func isSignedOut(err error) bool {
	var refused *accessError

	return errors.As(err, &refused) && refused.signedOut
}

// RFC 8628 §3.5: the CLI polls at the service's interval, waits on while the approval is
// pending, 5 s longer after each slow_down, and stops on a denial or an expired code.
func TestPollDeviceToken_States(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		polls []string
		waits []time.Duration
		err   string
	}{
		{
			"approved after waiting",
			[]string{"authorization_pending", "slow_down", "authorization_pending", "ok"},
			[]time.Duration{time.Second, time.Second, 6 * time.Second, 6 * time.Second},
			"",
		},
		{"denied", []string{"authorization_pending", "access_denied"}, []time.Duration{time.Second, time.Second}, "denied"},
		{"expired", []string{"expired_token"}, []time.Duration{time.Second}, "expired"},
		{"refused otherwise", []string{"invalid_grant"}, []time.Duration{time.Second}, "invalid_grant"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newAuthGoald(t)
			g.set(func(g *authGoald) { g.polls = tt.polls })

			var waits []time.Duration

			wait := func(_ context.Context, d time.Duration) error {
				waits = append(waits, d)

				return nil
			}

			svc := identityService{base: g.srv.URL, client: httpClient(g.origin(t), nil)}

			token, err := pollDeviceToken(t.Context(), svc, deviceCode{DeviceCode: "d", UserCode: "ABCDEFGH", ExpiresIn: 600, Interval: 1}, wait)
			if tt.err == "" && (err != nil || token != testSession) {
				t.Errorf("poll = %q, %v; want the session", token, err)
			}

			if tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)) {
				t.Errorf("poll = %q, %v; want an error about %s", token, err, tt.err)
			}

			if !slices.Equal(waits, tt.waits) {
				t.Errorf("waits = %v; want %v", waits, tt.waits)
			}
		})
	}
}

// The browser opens the page the service names only when it is an http(s) /device page with no
// code or anything else in its URL; otherwise the API origin's /device.
func TestVerificationPage_NeverTheCode(t *testing.T) {
	t.Parallel()

	origin := testOrigin
	for uri, want := range map[string]string{
		"https://goal.example.com/device":                    "https://goal.example.com/device",
		"https://public.example.com/device":                  "https://public.example.com/device",
		"https://goal.example.com/device?user_code=ABCDEFGH": origin + "/device",
		"https://goal.example.com/device#ABCDEFGH":           origin + "/device",
		"javascript:alert(1)":                                origin + "/device",
		"https://goal.example.com/elsewhere":                 origin + "/device",
		"":                                                   origin + "/device",
	} {
		if got := verificationPage(origin, uri); got != want {
			t.Errorf("verificationPage(%q) = %q; want %q", uri, got, want)
		}
	}
}

// Under GOAL_AUTH=none there is nothing to sign in to: goal login says so and stores nothing.
func TestLogin_SignInOff(t *testing.T) {
	t.Parallel()

	g := newAuthGoald(t)
	g.set(func(g *authGoald) { g.mode = gen.None })
	getenv := testEnv(t, g.srv.URL, nil)

	var buf bytes.Buffer
	if err := login(t.Context(), &printer{w: &buf}, getenv, []string{"--store", "file"}, newFakeKeychain()); err != nil ||
		!strings.Contains(buf.String(), "Sign-in is off") {
		t.Errorf("login under none = %v %q; want it to say sign-in is off", err, buf.String())
	}

	if len(g.sent("/api/auth/device/code")) != 0 {
		t.Error("goal login asked for a device code under none")
	}
}

// goal login --token reads a personal token or an API key from stdin only (spec 3 §9): on the
// command line it is refused before anything is sent.
func TestLogin_TokenNeverOnTheCommandLine(t *testing.T) {
	t.Parallel()

	g := newAuthGoald(t)
	getenv := testEnv(t, g.srv.URL, nil)

	for _, args := range [][]string{{"--token=" + testPAT}, {"--" + flagToken, testPAT}, {"--store", "file", "--" + flagToken, testPAT}, {"-token=" + testPAT}} {
		err := login(t.Context(), &printer{w: io.Discard, in: strings.NewReader("")}, getenv, args, newFakeKeychain())
		if !errors.Is(err, errTokenOnCommandLine) || strings.Contains(err.Error(), testPAT) {
			t.Errorf("login %v = %v; want the token refused on the command line", args[0], err)
		}
	}

	if n := len(g.sent("/v1/auth/config")) + len(g.sent("/v1/me")); n != 0 {
		t.Fatalf("a token on the command line sent %d requests; want none", n)
	}
}

// From stdin, a personal token or an API key is checked with /v1/me, then stored by origin like a
// session, and never printed.
func TestLogin_TokenFromStdin(t *testing.T) {
	t.Parallel()

	g := newAuthGoald(t)
	getenv := testEnv(t, g.srv.URL, nil)

	for _, token := range []string{testPAT, testKey} {
		kc := newFakeKeychain()

		var buf bytes.Buffer
		if err := login(t.Context(), &printer{w: &buf, in: strings.NewReader(token + "\n")}, getenv, []string{"--" + flagToken}, kc); err != nil {
			t.Fatalf("login --token: %v", err)
		}

		if strings.Contains(buf.String(), token) {
			t.Errorf("login --token printed the token: %q", buf.String())
		}

		if c, ok := kc.entry(t, g.origin(t)); !ok || c.Token != token || c.Kind != kindToken {
			t.Errorf("stored = %+v, %v; want the token by origin", c, ok)
		}

		if me := g.sent("/v1/me"); me[len(me)-1].auth != "Bearer "+token {
			t.Errorf("checked with %q; want the token", me[len(me)-1].auth)
		}
	}
}

// Spec 3 §9: with a personal token, whoami names the person and the token: its organization,
// scopes and expiry; never a session or a platform admin.
func TestWhoami_PersonalToken(t *testing.T) {
	t.Parallel()

	g := newAuthGoald(t)
	getenv := withEnv(testEnv(t, g.srv.URL, nil), envAPIKey, testPAT)

	var buf bytes.Buffer
	if err := whoami(t.Context(), &printer{w: &buf}, getenv, nil, newFakeKeychain()); err != nil {
		t.Fatalf("whoami: %v", err)
	}

	for _, want := range []string{
		"F. Cruz (f.cruz@example.com) on " + g.origin(t), "With the personal token in " + envAPIKey,
		"Personal token laptop in Acme Corp · expires Nov 23", "Can: org:read, item:create",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("whoami %q lacks %q", buf.String(), want)
		}
	}

	if strings.Contains(buf.String(), "Platform admin") || strings.Contains(buf.String(), "Session expires") || strings.Contains(buf.String(), testPAT) {
		t.Errorf("whoami %q; want no platform admin, no session and never the token", buf.String())
	}
}

// A person who signed up by code has no name yet (the bootstrap owner of an upgrade, say): the CLI
// names them by their email alone, as written, and capitalizes only its own words.
func TestSayWho_NamesAPersonWithoutANameByEmail(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		me   gen.Me
		want string
	}{
		{gen.Me{Mode: gen.Session, Kind: gen.MeKindUser, User: &gen.User{Email: "owner@example.com"}}, "owner@example.com on " + testOrigin},
		{gen.Me{Mode: gen.Session, Kind: gen.MeKindUser, User: &gen.User{Name: "F. Cruz", Email: "f.cruz@example.com"}}, "F. Cruz (f.cruz@example.com) on " + testOrigin},
		{gen.Me{Mode: gen.Session, Kind: gen.MeKindService, Key: &gen.MeKey{Name: "ci"}}, "The API key ci on " + testOrigin},
	} {
		var buf bytes.Buffer
		sayWho(&printer{w: &buf}, testOrigin, &c.me, nil)

		if first, _, _ := strings.Cut(buf.String(), "\n"); first != c.want {
			t.Errorf("sayWho = %q; want %q", first, c.want)
		}
	}
}

// Nothing is stored for stdin that is not a token, nothing at all, or a token goald refuses.
func TestLogin_TokenRefused(t *testing.T) {
	t.Parallel()

	g := newAuthGoald(t)
	getenv := testEnv(t, g.srv.URL, nil)
	kc := newFakeKeychain()

	for stdin, want := range map[string]string{
		"not a token":                         "neither a personal token",
		"":                                    "stdin",
		"goal_pat_" + strings.Repeat("X", 43): "does not accept",
	} {
		err := login(t.Context(), &printer{w: io.Discard, in: strings.NewReader(stdin)}, getenv, []string{"--" + flagToken}, kc)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("login --token < %q = %v; want %q", stdin, err, want)
		}
	}

	if len(kc.entries) != 0 {
		t.Errorf("a refused token was stored: %v", kc.entries)
	}
}

// goal logout ends the session in Goal (--all: every session) and forgets it here, even when Goal
// fails, which it says; a personal token is only forgotten, with where to revoke it.
func TestLogout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cred      credential
		args      []string
		signOut   int
		path      string
		wantErr   bool
		wantPrint string
	}{
		{"session", credential{Kind: kindSession, Token: testSession}, nil, http.StatusOK, "/api/auth/sign-out", false, "Signed out of"},
		{"everywhere", credential{Kind: kindSession, Token: testSession}, []string{"--all"}, http.StatusOK, "/api/auth/revoke-sessions", false, "everywhere"},
		{"goal fails", credential{Kind: kindSession, Token: testSession}, nil, http.StatusServiceUnavailable, "/api/auth/sign-out", true, ""},
		{"personal token", credential{Kind: kindToken, Token: testPAT}, nil, http.StatusOK, "", false, "API keys → Personal tokens"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newAuthGoald(t)
			g.set(func(g *authGoald) { g.signOut = tt.signOut })
			getenv := testEnv(t, g.srv.URL, nil)
			kc := newFakeKeychain()

			if err := (keychainStore{kc: kc}).put(g.origin(t), tt.cred); err != nil {
				t.Fatal(err)
			}

			if _, err := saveConfig(getenv, cliConfig{CredentialStore: storeKeychain}); err != nil {
				t.Fatal(err)
			}

			var buf bytes.Buffer

			err := logout(t.Context(), &printer{w: &buf}, getenv, tt.args, kc)
			if (err != nil) != tt.wantErr || !strings.Contains(buf.String(), tt.wantPrint) {
				t.Errorf("logout = %v %q; want error %v, output with %q", err, buf.String(), tt.wantErr, tt.wantPrint)
			}

			if tt.wantErr && (err == nil || !strings.Contains(err.Error(), "removed the session from this machine")) {
				t.Errorf("logout's error %v does not say the session is gone from here", err)
			}

			if _, ok := kc.entry(t, g.origin(t)); ok {
				t.Error("the credential is still stored")
			}

			checkSignOuts(t, g, tt.path)
		})
	}
}

// checkSignOuts checks goal logout called path with the session, or, for "", no sign-out at all.
func checkSignOuts(t *testing.T, g *authGoald, path string) {
	t.Helper()

	calls := append(g.sent("/api/auth/sign-out"), g.sent("/api/auth/revoke-sessions")...)

	switch {
	case path == "" && len(calls) != 0:
		t.Errorf("a personal token's logout called %+v", calls)
	case path != "" && (len(calls) != 1 || calls[0].path != path || calls[0].auth != "Bearer "+testSession):
		t.Errorf("logout called %+v; want %s with the session", calls, path)
	}
}

// A 401 says how to sign in (exit 77); a 403 reauth-required says to sign in again; with
// GOAL_API_KEY set the refusal names the key, since goal login would not help.
func TestRefusals_SayHowToSignIn(t *testing.T) {
	t.Parallel()

	g := newAuthGoald(t)
	origin := g.origin(t)

	err := run(t.Context(), []string{cmdWhoami}, &printer{w: io.Discard}, testEnv(t, g.srv.URL, nil), nil)
	if !isSignedOut(err) || err.Error() != "Not signed in to "+origin+": run goal login" {
		t.Errorf("whoami signed out = %v; want the sign-in hint", err)
	}

	err = run(t.Context(), []string{cmdWhoami}, &printer{w: io.Discard}, testEnv(t, g.srv.URL, map[string]string{envAPIKey: "goal_sk_" + strings.Repeat("Z", 43)}), nil)
	if !isSignedOut(err) || !strings.Contains(err.Error(), "refused the key in GOAL_API_KEY") {
		t.Errorf("whoami with a refused GOAL_API_KEY = %v; want it named", err)
	}

	stale := &http.Response{StatusCode: http.StatusForbidden, Request: httptest.NewRequestWithContext(t.Context(), http.MethodDelete, g.srv.URL+"/v1/orgs/acme", nil)}

	var refused *accessError
	if err := problem(stale, &gen.Problem{Title: "Sign in again", Status: 403, Code: new("reauth-required")}); !errors.As(err, &refused) ||
		err.Error() != "This needs a recent sign-in: run goal login again" {
		t.Errorf("reauth-required = %v; want the re-sign-in hint", err)
	}

	if err := problem(stale, &gen.Problem{Title: "Forbidden", Status: 403, Code: new("forbidden")}); !errors.As(err, &refused) {
		t.Errorf("forbidden = %v; want an access refusal (exit 77)", err)
	}
}
