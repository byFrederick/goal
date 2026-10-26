package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// Test credentials: a session token from the device grant, a personal token and an API key.
const (
	testSession  = "CliSessionCliSessionCliSession01"
	testSession2 = "CliSessionCliSessionCliSession02"
)

// testOrigin is an install's origin over https.
const testOrigin = "https://goal.example.com"

var (
	testPAT = "goal_pat_" + strings.Repeat("P", 43)
	testKey = "goal_sk_" + strings.Repeat("K", 43)
)

// fakeKeychain is an OS keychain in memory, by service and account.
type fakeKeychain struct {
	mu      sync.Mutex
	entries map[string]string
	err     error
}

func newFakeKeychain() *fakeKeychain { return &fakeKeychain{entries: map[string]string{}} }

func (k *fakeKeychain) Get(service, account string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.err != nil {
		return "", k.err
	}

	v, ok := k.entries[service+"/"+account]
	if !ok {
		return "", keyring.ErrNotFound
	}

	return v, nil
}

func (k *fakeKeychain) Set(service, account, secret string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.err != nil {
		return k.err
	}

	k.entries[service+"/"+account] = secret

	return nil
}

func (k *fakeKeychain) Delete(service, account string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.err != nil {
		return k.err
	}

	if _, ok := k.entries[service+"/"+account]; !ok {
		return keyring.ErrNotFound
	}

	delete(k.entries, service+"/"+account)

	return nil
}

// entry is the credential the keychain holds for origin, decoded.
func (k *fakeKeychain) entry(t *testing.T, origin string) (credential, bool) {
	t.Helper()

	raw, err := k.Get(keychainService, origin)
	if errors.Is(err, keyring.ErrNotFound) {
		return credential{}, false
	}

	var c credential
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("keychain entry for %s: %v", origin, err)
	}

	return c, true
}

// The keychain keeps one entry per origin under service goal; forgetting one leaves the others.
func TestKeychainStore_OneEntryPerOrigin(t *testing.T) {
	t.Parallel()

	kc := newFakeKeychain()
	store := keychainStore{kc: kc}

	if _, ok, err := store.get(testOrigin); ok || err != nil {
		t.Fatalf("get from an empty keychain = %v, %v; want nothing", ok, err)
	}

	if err := store.put(testOrigin, credential{Kind: kindSession, Token: testSession}); err != nil {
		t.Fatal(err)
	}

	if err := store.put("http://127.0.0.1:8080", credential{Kind: kindToken, Token: testPAT}); err != nil {
		t.Fatal(err)
	}

	if c, ok := kc.entry(t, testOrigin); !ok || c.Token != testSession || c.Kind != kindSession {
		t.Errorf("keychain entry goal/https://goal.example.com = %+v, %v; want the session", c, ok)
	}

	if removed, err := store.remove(testOrigin); !removed || err != nil {
		t.Errorf("remove = %v, %v", removed, err)
	}

	if c, ok, _ := store.get("http://127.0.0.1:8080"); !ok || c.Token != testPAT {
		t.Errorf("the other origin's entry = %+v, %v; want it kept", c, ok)
	}
}

// A machine without a keychain is told to use the file store.
func TestKeychainStore_WithoutAKeychain(t *testing.T) {
	t.Parallel()

	kc := newFakeKeychain()
	kc.err = errors.New("no keychain on this machine")

	err := keychainStore{kc: kc}.put(testOrigin, credential{Kind: kindSession, Token: testSession})
	if err == nil || !strings.Contains(err.Error(), "--store file") {
		t.Errorf("put without a keychain = %v; want it to point at --store file", err)
	}
}

// The file store is readable by its owner only, and refuses a file others may read.
func TestFileStore_OwnerOnly(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("file modes are Unix's")
	}

	dir := filepath.Join(t.TempDir(), "goal")
	store := fileStore{path: filepath.Join(dir, "credentials.json")}

	if err := store.put(testOrigin, credential{Kind: kindSession, Token: testSession}); err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]os.FileMode{dir: 0o700, store.path: 0o600} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != want {
			t.Errorf("%s mode = %v, %v; want %o", path, info.Mode().Perm(), err, want)
		}
	}

	if c, ok, err := store.get(testOrigin); !ok || err != nil || c.Token != testSession {
		t.Errorf("get = %+v, %v, %v; want the session", c, ok, err)
	}

	if err := os.Chmod(store.path, 0o644); err != nil { //nolint:gosec // others may read it: what the store must refuse
		t.Fatal(err)
	}

	if _, _, err := store.get(testOrigin); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("get from a file others may read = %v; want a refusal", err)
	}
}

// Credentials are keyed by origin: scheme and host in lower case, without the scheme's default
// port or any path.
func TestOriginOf(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"HTTPS://Goal.Example.com:443/v1":    testOrigin,
		"https://goal.example.com:8443":      "https://goal.example.com:8443",
		"http://127.0.0.1:8080/":             "http://127.0.0.1:8080",
		"http://localhost:80":                "http://localhost",
		"http://[::1]:8080":                  "http://[::1]:8080",
		"ftp://goal.example.com":             "",
		"https://user:pass@goal.example.com": "",
		"goal.example.com":                   "",
	}

	for in, want := range tests {
		got, err := originOf(in)
		if got != want || (want == "") != (err != nil) {
			t.Errorf("originOf(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

// A credential travels only over https, or plain http to this machine (spec 3 §9).
func TestSendsCredentialsTo(t *testing.T) {
	t.Parallel()

	for origin, ok := range map[string]bool{
		testOrigin: true, "http://127.0.0.1:8080": true, "http://localhost:8080": true, "http://[::1]:8080": true,
		"http://goal.example.com": false, "http://192.0.2.10:8080": false,
	} {
		if err := sendsCredentialsTo(origin); (err == nil) != ok {
			t.Errorf("sendsCredentialsTo(%s) = %v; want allowed %v", origin, err, ok)
		}
	}
}

// The transport adds the credential to requests for its origin only, so a redirect or another
// URL never carries it elsewhere; every request names the CLI.
func TestCredentialTransport_OnlyItsOrigin(t *testing.T) {
	t.Parallel()

	var seen []string

	var mu sync.Mutex

	record := func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		seen = append(seen, r.Host+" "+r.Header.Get("Authorization")+" "+r.Header.Get("User-Agent"))

		w.WriteHeader(http.StatusNoContent)
	}

	other := httptest.NewServer(http.HandlerFunc(record))
	t.Cleanup(other.Close)

	own := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			http.Redirect(w, r, other.URL+"/landed", http.StatusFound)

			return
		}

		record(w, r)
	}))
	t.Cleanup(own.Close)

	origin, err := originOf(own.URL)
	if err != nil {
		t.Fatal(err)
	}

	client := httpClient(origin, &resolved{cred: credential{Kind: kindToken, Token: testPAT}})

	for _, u := range []string{own.URL + "/v1/me", own.URL + "/elsewhere", other.URL + "/v1/me"} {
		res, err := client.Get(u) //nolint:noctx // a test request
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()
	}

	ownHost, otherHost := strings.TrimPrefix(own.URL, "http://"), strings.TrimPrefix(other.URL, "http://")
	agent := userAgent()
	want := []string{
		ownHost + " Bearer " + testPAT + " " + agent,
		otherHost + "  " + agent,
		otherHost + "  " + agent,
	}

	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests seen:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}

	if !strings.HasPrefix(agent, "goal/") || !strings.HasSuffix(agent, "("+runtime.GOOS+")") {
		t.Errorf("user agent %q; want goal/<version> (%s)", agent, runtime.GOOS)
	}
}

// GOAL_API_KEY wins over a stored sign-in and is never written; it must be an API key or a
// personal token, refused before any request otherwise, and like every credential it travels
// only over https or to this machine.
func TestResolveCredential_APIKeyWins(t *testing.T) {
	t.Parallel()

	kc := newFakeKeychain()
	origin := testOrigin

	if err := (keychainStore{kc: kc}).put(origin, credential{Kind: kindSession, Token: testSession}); err != nil {
		t.Fatal(err)
	}

	cfg := cliConfig{CredentialStore: storeKeychain}

	for _, key := range []string{testKey, testPAT} {
		r, err := resolveCredential(testEnv(t, origin, map[string]string{envAPIKey: key}), cfg, origin, kc)
		if err != nil || r == nil || r.cred.Token != key || r.store != nil {
			t.Errorf("with GOAL_API_KEY=%s… = %+v, %v; want the key", key[:9], r, err)
		}
	}

	r, err := resolveCredential(testEnv(t, origin, nil), cfg, origin, kc)
	if err != nil || r == nil || r.cred.Token != testSession || r.store == nil {
		t.Errorf("without GOAL_API_KEY = %+v, %v; want the stored session", r, err)
	}

	if len(kc.entries) != 1 {
		t.Errorf("the keychain holds %d entries; GOAL_API_KEY must never be written", len(kc.entries))
	}
}

// GOAL_API_KEY other than an API key or a personal token is refused without repeating it, and,
// like every credential, it travels only over https or to this machine; without a sign-in there
// is no credential, and the keychain is not read.
func TestResolveCredential_Refusals(t *testing.T) {
	t.Parallel()

	kc := newFakeKeychain()
	kc.err = errors.New("the keychain must not be read")
	origin := testOrigin
	cfg := cliConfig{CredentialStore: storeKeychain}

	for _, bad := range []string{"secret", "goal_sk_short", testSession, "goal_xx_" + strings.Repeat("K", 43)} {
		if _, err := resolveCredential(testEnv(t, origin, map[string]string{envAPIKey: bad}), cfg, origin, kc); err == nil ||
			strings.Contains(err.Error(), bad) {
			t.Errorf("GOAL_API_KEY=%q = %v; want a refusal that does not repeat it", bad, err)
		}
	}

	if _, err := resolveCredential(testEnv(t, "", map[string]string{envAPIKey: testKey}), cliConfig{}, "http://goal.example.com", kc); err == nil {
		t.Error("GOAL_API_KEY over plain http to another machine was not refused")
	}

	if r, err := resolveCredential(testEnv(t, origin, nil), cliConfig{}, origin, kc); r != nil || err != nil {
		t.Errorf("without a sign-in = %+v, %v; want none, and no keychain read", r, err)
	}
}

// A stored session is shown to the identity service at most once a day, so Better Auth extends
// it; the time is recorded beside the token.
func TestCredentialTransport_KeepsTheSessionAlive(t *testing.T) {
	t.Parallel()

	var keptAlive []string

	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/get-session" {
			mu.Lock()

			keptAlive = append(keptAlive, r.Header.Get("Authorization"))

			mu.Unlock()

			_, _ = w.Write([]byte(`{"session":{},"user":{}}`))

			return
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	origin, _ := originOf(srv.URL)
	store := keychainStore{kc: newFakeKeychain()}

	use := func(at time.Time) {
		c := credential{Kind: kindSession, Token: testSession, KeptAliveAt: at}
		if err := store.put(origin, c); err != nil {
			t.Fatal(err)
		}

		client := httpClient(origin, &resolved{cred: c, store: store})
		for range 2 {
			res, err := client.Get(srv.URL + "/v1/me") //nolint:noctx // a test request
			if err != nil {
				t.Fatal(err)
			}

			_ = res.Body.Close()
		}
	}

	use(time.Now().Add(-25 * time.Hour))

	if c, _, _ := store.get(origin); time.Since(c.KeptAliveAt) > time.Minute {
		t.Errorf("kept_alive_at = %v after keeping the session alive; want now", c.KeptAliveAt)
	}

	use(time.Now().Add(-time.Hour))

	if len(keptAlive) != 1 || keptAlive[0] != "Bearer "+testSession {
		t.Errorf("get-session calls = %v; want one, for the day-old session, with its token", keptAlive)
	}
}
