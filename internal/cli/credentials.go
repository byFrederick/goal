package cli

// The CLI's credentials (spec 3 §9, §11.1 row 35). A command sends goald one bearer credential:
// GOAL_API_KEY from the environment when it is set (an API key goal_sk_… or a personal token
// goal_pat_…, never written anywhere), else what goal login stored for the API's origin, in the
// OS keychain (service goal, account the origin) or, with goal login --store file, in
// $XDG_CONFIG_HOME/goal/credentials.json (mode 0600). Credentials are keyed by origin and the
// transport adds one only to requests for that origin, so changing api_url or GOAL_API_URL never
// sends a token to another host; and only over https, or plain http to this machine.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/byFrederick/goal/internal/api/gen"
)

const (
	// envAPIKey holds an API key or a personal token for scripts and CI; it wins over goal login.
	envAPIKey = "GOAL_API_KEY" //nolint:gosec // the variable's name, not a credential
	// keychainService names the CLI's entries in the OS keychain; each one's account is an origin.
	keychainService = "goal"
	// The credential stores goal login can keep a credential in (--store, credential_store).
	storeKeychain = "keychain"
	storeFile     = "file"
	// keepAliveEvery is how often at most the CLI shows its session to the identity service, which
	// extends a session only when its own endpoints see it (spec 3 §8.3).
	keepAliveEvery = 24 * time.Hour
)

var (
	// goalKeyForm is an API key or a personal token: its prefix and base64url of 32 random bytes.
	goalKeyForm = regexp.MustCompile(`^goal_(sk|pat)_[A-Za-z0-9_-]{43}$`)
	// sessionTokenForm is a Better Auth session token, as the device grant hands one out.
	sessionTokenForm = regexp.MustCompile(`^[A-Za-z0-9]{32}$`)
)

// credentialKind is how a stored credential was obtained, which decides how goal logout ends it.
type credentialKind string

const (
	// kindSession is a session from goal login's device flow: goal logout ends it in Goal.
	kindSession credentialKind = "session"
	// kindToken is a personal token or an API key from goal login --token: goal logout only
	// forgets it, since it is revoked in the browser.
	kindToken credentialKind = "token"
)

// credential is what the CLI sends goald as Authorization: Bearer. Its token is never printed,
// logged or put in an error.
type credential struct {
	Kind  credentialKind `json:"kind"`
	Token string         `json:"token"`
	// KeptAliveAt is when the CLI last showed a session to the identity service.
	KeptAliveAt time.Time `json:"kept_alive_at,omitzero"`
}

// isPersonalToken reports whether the credential is a personal token (rather than an API key or
// a session).
func (c credential) isPersonalToken() bool { return strings.HasPrefix(c.Token, "goal_pat_") }

// credentialStore keeps one credential per origin.
type credentialStore interface {
	get(origin string) (credential, bool, error)
	put(origin string, c credential) error
	// remove forgets the origin's credential, reporting whether there was one.
	remove(origin string) (bool, error)
	// where names the store for people ("the OS keychain", a file's path).
	where() string
}

// keychain is the OS keychain as go-keyring reaches it (macOS Keychain, the Secret Service on
// Linux, the Windows Credential Manager); tests use a fake.
type keychain interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

// osKeychain is the user's OS keychain.
type osKeychain struct{}

func (osKeychain) Get(service, account string) (string, error) { return keyring.Get(service, account) }

func (osKeychain) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret)
}
func (osKeychain) Delete(service, account string) error { return keyring.Delete(service, account) }

// keychainStore keeps each origin's credential as one keychain entry: service goal, account the
// origin, the credential's JSON as the secret.
type keychainStore struct{ kc keychain }

func (k keychainStore) where() string { return "the OS keychain" }

func (k keychainStore) get(origin string) (credential, bool, error) {
	raw, err := k.kc.Get(keychainService, origin)
	if errors.Is(err, keyring.ErrNotFound) {
		return credential{}, false, nil
	}

	if err != nil {
		return credential{}, false, fmt.Errorf("read the credential for %s from the OS keychain: %w", origin, err)
	}

	var c credential
	if err := json.Unmarshal([]byte(raw), &c); err != nil || c.Token == "" {
		return credential{}, false, fmt.Errorf("the OS keychain's entry for %s (service %s) is not the CLI's; run goal logout, then goal login",
			origin, keychainService)
	}

	return c, true, nil
}

func (k keychainStore) put(origin string, c credential) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}

	if err := k.kc.Set(keychainService, origin, string(raw)); err != nil {
		return fmt.Errorf("store the credential in the OS keychain: %w (on a machine without one, use goal login --store file)", err)
	}

	return nil
}

func (k keychainStore) remove(origin string) (bool, error) {
	err := k.kc.Delete(keychainService, origin)
	if errors.Is(err, keyring.ErrNotFound) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("remove the credential for %s from the OS keychain: %w", origin, err)
	}

	return true, nil
}

// fileStore keeps every origin's credential in one JSON file readable by its owner only, for
// machines without a keychain (goal login --store file).
type fileStore struct{ path string }

func (f fileStore) where() string { return f.path }

// read reads the file; a missing one holds nothing. A file other users may read is refused, as
// ssh refuses a private key they may read.
func (f fileStore) read() (map[string]credential, error) {
	info, err := os.Stat(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]credential{}, nil
	}

	if err != nil {
		return nil, err
	}

	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is readable by other users (mode %o): chmod 600 it, or delete it and run goal login again",
			f.path, info.Mode().Perm())
	}

	raw, err := os.ReadFile(f.path)
	if err != nil {
		return nil, err
	}

	all := map[string]credential{}
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", f.path, err)
	}

	return all, nil
}

func (f fileStore) get(origin string) (credential, bool, error) {
	all, err := f.read()
	c, ok := all[origin]

	return c, ok && c.Token != "", err
}

func (f fileStore) put(origin string, c credential) error {
	all, err := f.read()
	if err != nil {
		return err
	}

	all[origin] = c

	return f.write(all)
}

func (f fileStore) remove(origin string) (bool, error) {
	all, err := f.read()
	if err != nil {
		return false, err
	}

	if _, ok := all[origin]; !ok {
		return false, nil
	}

	delete(all, origin)

	return true, f.write(all)
}

func (f fileStore) write(all map[string]credential) error {
	raw, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}

	return writePrivateFile(f.path, append(raw, '\n'))
}

// credentialsPath is the file store's path, beside the config file.
func credentialsPath(getenv func(string) string) (string, error) {
	path, err := configPath(getenv)
	if err != nil {
		return "", err
	}

	return filepath.Join(filepath.Dir(path), "credentials.json"), nil
}

// openStore opens the store named name (storeKeychain or storeFile).
func openStore(getenv func(string) string, name string, kc keychain) (credentialStore, error) {
	switch name {
	case storeKeychain:
		return keychainStore{kc: kc}, nil
	case storeFile:
		path, err := credentialsPath(getenv)
		if err != nil {
			return nil, err
		}

		return fileStore{path: path}, nil
	default:
		return nil, fmt.Errorf("credential store %q is neither %s nor %s", name, storeKeychain, storeFile)
	}
}

// apiBase is goald's API: GOAL_API_URL, else api_url in the config file, else the local goald.
func apiBase(getenv func(string) string, cfg cliConfig) string {
	return strings.TrimRight(cmp.Or(strings.TrimSpace(getenv("GOAL_API_URL")), cfg.APIURL, defaultAPI), "/")
}

// originOf is the origin of an http(s) URL as credentials are keyed by it: the scheme and host in
// lower case, with the scheme's default port left out.
func originOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != schemeHTTP && u.Scheme != schemeHTTPS) || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("goald's API %q is not an http or https URL", raw)
	}

	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}

	if p := u.Port(); p != "" && (u.Scheme != schemeHTTP || p != "80") && (u.Scheme != schemeHTTPS || p != "443") {
		host = net.JoinHostPort(strings.Trim(host, "[]"), p)
	}

	return u.Scheme + "://" + host, nil
}

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// sendsCredentialsTo refuses an origin a credential may not travel to: plain http off this
// machine.
func sendsCredentialsTo(origin string) error {
	u, err := url.Parse(origin)
	if err != nil {
		return err
	}

	ip := net.ParseIP(u.Hostname())
	if u.Scheme == schemeHTTPS || u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()) {
		return nil
	}

	return fmt.Errorf("%s is plain http to another machine: the CLI sends credentials only over https, or over http to this machine", origin)
}

// resolved is the credential a command sends, and where it came from.
type resolved struct {
	cred credential
	// store is the store it was read from; nil for GOAL_API_KEY.
	store credentialStore
}

// resolveCredential finds the credential for origin: GOAL_API_KEY (refused unless it is an API
// key or a personal token), else the one goal login stored for origin in the store config.json
// names; none when nobody signed in. A credential for an origin it may not travel to is refused.
func resolveCredential(getenv func(string) string, cfg cliConfig, origin string, kc keychain) (*resolved, error) {
	r, err := credentialFor(getenv, cfg, origin, kc)
	if err != nil || r == nil {
		return nil, err
	}

	if err := sendsCredentialsTo(origin); err != nil {
		return nil, err
	}

	return r, nil
}

// credentialFor is GOAL_API_KEY's credential, else the stored one for origin; nil for none.
func credentialFor(getenv func(string) string, cfg cliConfig, origin string, kc keychain) (*resolved, error) {
	if key := strings.TrimSpace(getenv(envAPIKey)); key != "" {
		if !goalKeyForm.MatchString(key) {
			return nil, fmt.Errorf("%s is neither an API key (goal_sk_…) nor a personal token (goal_pat_…)", envAPIKey)
		}

		return &resolved{cred: credential{Kind: kindToken, Token: key}}, nil
	}

	store, c, found, err := storedCredential(getenv, cfg, origin, kc)
	if err != nil || !found {
		return nil, err
	}

	return &resolved{cred: c, store: store}, nil
}

// storedCredential is what goal login stored for origin, in the store config.json names; nothing
// (and no keychain read) when nobody signed in on this machine.
func storedCredential(getenv func(string) string, cfg cliConfig, origin string, kc keychain) (credentialStore, credential, bool, error) {
	if cfg.CredentialStore == "" {
		return nil, credential{}, false, nil
	}

	store, err := openStore(getenv, cfg.CredentialStore, kc)
	if err != nil {
		return nil, credential{}, false, err
	}

	c, found, err := store.get(origin)

	return store, c, found, err
}

// target is goald's API as a command reaches it: the CLI's config, the API's base URL and its
// origin.
type target struct {
	cfg          cliConfig
	base, origin string
}

// apiTarget reads where goald's API is.
func apiTarget(getenv func(string) string) (target, error) {
	cfg, err := loadConfig(getenv)
	if err != nil {
		return target{}, err
	}

	base := apiBase(getenv, cfg)

	origin, err := originOf(base)
	if err != nil {
		return target{}, err
	}

	return target{cfg: cfg, base: base, origin: origin}, nil
}

// userAgent names the CLI to goald, and to the person approving a goal login: goal/1.4 (darwin).
func userAgent() string {
	v := "dev"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		v = strings.TrimPrefix(info.Main.Version, "v")
	}

	return fmt.Sprintf("goal/%s (%s)", v, runtime.GOOS)
}

// credentialTransport adds the CLI's user agent to every request and its credential to those for
// the credential's origin only. Before the first request it keeps a stored session alive, at
// most once a day.
type credentialTransport struct {
	base   http.RoundTripper
	origin string
	agent  string
	// cred is nil without a credential.
	cred *resolved

	keepAlive sync.Once
}

func (t *credentialTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.cred != nil && t.cred.store != nil && t.cred.cred.Kind == kindSession {
		t.keepAlive.Do(func() { t.keepSessionAlive(r.Context()) })
	}

	out := r.Clone(r.Context())
	out.Header.Set("User-Agent", t.agent)

	if t.cred != nil {
		if o, err := originOf(r.URL.String()); err == nil && o == t.origin {
			out.Header.Set("Authorization", "Bearer "+t.cred.cred.Token)
		}
	}

	return t.base.RoundTrip(out)
}

// keepSessionAlive shows the stored session to the identity service (GET /api/auth/get-session)
// when it last did more than a day ago, and records when. Failures are ignored: the request that
// follows says whether the session still holds.
func (t *credentialTransport) keepSessionAlive(ctx context.Context) {
	c := t.cred.cred
	if time.Since(c.KeptAliveAt) < keepAliveEvery {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.origin+"/api/auth/get-session", http.NoBody)
	if err != nil {
		return
	}

	req.Header.Set("User-Agent", t.agent)
	req.Header.Set("Authorization", "Bearer "+c.Token)

	res, err := t.base.RoundTrip(req)
	if err != nil {
		return
	}

	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	_ = res.Body.Close()

	if res.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) == "null" {
		return
	}

	c.KeptAliveAt = time.Now().UTC()
	_ = t.cred.store.put(t.origin, c)
}

// httpClient is the CLI's client for goald: its user agent, and cred (nil for none) on requests
// to origin. It has its own copy of http.DefaultTransport, because an httptest server closing
// calls CloseIdleConnections on the default one and breaks a parallel test's request in flight.
func httpClient(origin string, cred *resolved) *http.Client {
	base := http.DefaultTransport
	if t, ok := base.(*http.Transport); ok {
		base = t.Clone()
	}

	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &credentialTransport{base: base, origin: origin, agent: userAgent(), cred: cred},
	}
}

// accessError is goald refusing the credential (401) or the permission (403); main exits 77
// (EX_NOPERM) for it.
type accessError struct {
	msg string
	// signedOut: goald answered 401, so no credential of origin's holds.
	signedOut bool
	origin    string
}

func (e *accessError) Error() string { return e.msg }

// exitNoPerm is the exit status of a refused credential or permission (sysexits' EX_NOPERM).
const exitNoPerm = 77

// refusedAccess is goald's 401 or 403 as the CLI says it; nil for any other answer.
func refusedAccess(res *http.Response, p *gen.Problem) error {
	if res == nil {
		return nil
	}

	origin := ""
	if res.Request != nil {
		origin, _ = originOf(res.Request.URL.String())
	}

	switch res.StatusCode {
	case http.StatusUnauthorized:
		return &accessError{msg: fmt.Sprintf("Not signed in to %s: run goal login", origin), signedOut: true, origin: origin}
	case http.StatusForbidden:
		if p != nil && deref(p.Code) == "reauth-required" {
			return &accessError{msg: "This needs a recent sign-in: run goal login again", origin: origin}
		}

		msg := "goald refused this: you do not hold the permission it needs"
		if p != nil {
			msg = problemText(p)
		}

		return &accessError{msg: msg, origin: origin}
	default:
		return nil
	}
}
