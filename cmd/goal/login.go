package main

// goal login, goal logout and goal whoami (spec 3 §9). goal login signs the CLI in with the
// device authorization grant (RFC 8628) of the identity service, through goald's /api/auth: the
// person opens <origin>/device in a browser where they are signed in, types the code the terminal
// shows and approves; the CLI polls for the session meanwhile. goal login --token stores a personal
// token (or an API key) read from stdin instead. This file is the CLI's one caller of /api/auth
// (ADR 0007's client contract, Better Auth's own request and response shapes).

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/byFrederick/goal/internal/api/gen"
)

const (
	// cliClientID is the one client the identity service's device grant serves (apps/auth/src/auth.ts).
	cliClientID = "goal-cli"
	deviceGrant = "urn:ietf:params:oauth:grant-type:device_code"
	// defaultPollEvery is RFC 8628's polling interval when the service names none; slowDownStep
	// is what each slow_down adds to it.
	defaultPollEvery = 5 * time.Second
	slowDownStep     = 5 * time.Second
	defaultCodeLife  = 10 * time.Minute
	// browserWait is how long goal login waits for the browser opener to say whether it opened the
	// page; a $BROWSER that stays in the foreground is left running.
	browserWait = 2 * time.Second
	// maxTokenBytes bounds what goal login --token reads from stdin.
	maxTokenBytes = 4 << 10
	// maxAuthAnswer bounds what the CLI reads of an identity service answer.
	maxAuthAnswer = 64 << 10
	flagToken     = "token"
)

var (
	// userCodeChars is what a user code the CLI prints may hold: Better Auth's alphabet, and dashes.
	userCodeChars = regexp.MustCompile(`^[A-Z0-9-]{4,32}$`)
	// errorCodeForm is an OAuth error code (authorization_pending) as the CLI repeats one.
	errorCodeForm = regexp.MustCompile(`^[a-z_]{1,64}$`)
)

// errTokenOnCommandLine refuses a token given as an argument: argv shows in ps and in the shell's
// history.
var errTokenOnCommandLine = errors.New("goal login --token reads the token from stdin only (goal login --token < token-file, " +
	"or paste it when asked): a token on the command line shows in ps and in your shell history")

// accountCommand is a command on the CLI's own sign-in, given the user's keychain.
type accountCommand func(ctx context.Context, out *printer, getenv func(string) string, args []string, kc keychain) error

// loginArgs are goal login's flags.
type loginArgs struct {
	store string
	token bool
}

// parseLogin reads goal login [--store keychain|file] [--token]. A token among the arguments
// (--token=…, or a value after --token) is refused: tokens come from stdin.
func parseLogin(args []string) (loginArgs, error) {
	if slices.ContainsFunc(args, func(a string) bool {
		return strings.HasPrefix(a, "--"+flagToken+"=") || strings.HasPrefix(a, "-"+flagToken+"=")
	}) {
		return loginArgs{}, errTokenOnCommandLine
	}

	var a loginArgs

	fs := newFlags("login")
	fs.StringVar(&a.store, "store", "", "where to keep the credential: keychain or file")
	fs.BoolVar(&a.token, flagToken, false, "store a personal token read from stdin")

	if err := fs.Parse(args); err != nil {
		return loginArgs{}, errUsage
	}

	switch {
	case fs.NArg() == 0:
		return a, nil
	case a.token || slices.Contains(args, "--"+flagToken) || slices.Contains(args, "-"+flagToken):
		return loginArgs{}, errTokenOnCommandLine
	default:
		return loginArgs{}, errUsage
	}
}

// login runs goal login [--store keychain|file] and goal login --token [--store keychain|file].
func login(ctx context.Context, out *printer, getenv func(string) string, args []string, kc keychain) error {
	a, err := parseLogin(args)
	if err != nil {
		return err
	}

	tg, err := apiTarget(getenv)
	if err != nil {
		return err
	}

	if err := sendsCredentialsTo(tg.origin); err != nil {
		return err
	}

	storeName := cmp.Or(a.store, tg.cfg.CredentialStore, storeKeychain)

	store, err := openStore(getenv, storeName, kc)
	if err != nil {
		return err
	}

	off, err := signInOff(ctx, tg)
	if err != nil {
		return err
	}

	if off {
		out.printf("Sign-in is off on %s (GOAL_AUTH=none): commands act as the local user, with nothing to sign in to.\n", tg.origin)

		return nil
	}

	svc := identityService{base: tg.base, client: httpClient(tg.origin, nil)}

	cred, err := obtainCredential(ctx, out, getenv, a.token, svc, tg.origin)
	if err != nil {
		return err
	}

	who, err := checkCredential(ctx, tg, cred)
	if err != nil {
		return err
	}

	if err := replaceCredential(ctx, getenv, tg, storeName, svc, cred, kc); err != nil {
		return err
	}

	out.printf("Signed in to %s as %s. The %s is in %s.\n", tg.origin, who, credentialNoun(cred), store.where())

	if getenv(envAPIKey) != "" {
		out.printf("%s is set, and commands use it rather than this sign-in while it is.\n", envAPIKey)
	}

	return nil
}

// obtainCredential runs the device flow, or reads a token from stdin with --token.
func obtainCredential(ctx context.Context, out *printer, getenv func(string) string, token bool, svc identityService, origin string) (credential, error) {
	if token {
		return tokenLogin(out)
	}

	return deviceLogin(ctx, out, getenv, svc, origin, sleepFor)
}

// signInOff reports whether goald runs without sign-in (GOAL_AUTH=none), from its public auth
// configuration.
func signInOff(ctx context.Context, tg target) (bool, error) {
	c, err := gen.NewClientWithResponses(tg.base, gen.WithHTTPClient(httpClient(tg.origin, nil)))
	if err != nil {
		return false, err
	}

	res, err := c.GetAuthConfigWithResponse(ctx)
	if err != nil {
		return false, apiDown(err)
	}

	if res.JSON200 == nil {
		return false, problem(res.HTTPResponse, nil)
	}

	return res.JSON200.Mode == gen.None, nil
}

// replaceCredential stores cred for the target's origin in the store named storeName and
// remembers the store in config.json, after forgetting the credential it replaces.
func replaceCredential(ctx context.Context, getenv func(string) string, tg target, storeName string, svc identityService,
	cred credential, kc keychain,
) error {
	store, err := openStore(getenv, storeName, kc)
	if err != nil {
		return err
	}

	forgetPrevious(ctx, getenv, tg, storeName, svc, cred, kc)

	if err := store.put(tg.origin, cred); err != nil {
		return err
	}

	if tg.cfg.CredentialStore == storeName {
		return nil
	}

	cfg := tg.cfg
	cfg.CredentialStore = storeName
	_, err = saveConfig(getenv, cfg)

	return err
}

// forgetPrevious ends the session a new goal login replaces, so a second sign-in leaves nothing
// valid behind, and removes it from the store config.json named when the new one goes elsewhere.
// Failures are ignored: the old session expires on its own.
func forgetPrevious(ctx context.Context, getenv func(string) string, tg target, storeName string, svc identityService, cred credential, kc keychain) {
	previous, old, found, err := storedCredential(getenv, tg.cfg, tg.origin, kc)
	if err != nil || !found {
		return
	}

	if old.Kind == kindSession && old.Token != cred.Token {
		_ = svc.endSession(ctx, "/sign-out", old.Token)
	}

	if tg.cfg.CredentialStore != storeName {
		_, _ = previous.remove(tg.origin)
	}
}

// deviceCode is the identity service's answer to POST /api/auth/device/code. Its
// verification_uri_complete (the page with the code in its URL) is never read: the person types
// the code, so a link someone sends cannot approve anything.
type deviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// deviceLogin asks for a code, tells the person where to enter it, opens their browser there and
// waits for the session.
func deviceLogin(ctx context.Context, out *printer, getenv func(string) string, svc identityService, origin string,
	wait func(context.Context, time.Duration) error,
) (credential, error) {
	code, err := svc.deviceCode(ctx)
	if err != nil {
		return credential{}, err
	}

	page := verificationPage(origin, code.VerificationURI)
	out.printf("Open %s and enter %s\n", page, displayCode(code.UserCode))

	if err := openBrowser(ctx, getenv, page); err != nil {
		out.printf("(No browser opened: open the address yourself, on any device.)\n")
	}

	out.printf("Waiting for the approval in the browser…\n")

	token, err := pollDeviceToken(ctx, svc, code, wait)
	if err != nil {
		return credential{}, err
	}

	return credential{Kind: kindSession, Token: token, KeptAliveAt: time.Now().UTC()}, nil
}

// verificationPage is where the person enters the code: the service's verification_uri when it is
// the /device page of an http(s) origin, else the API origin's /device. Never with the code.
func verificationPage(origin, uri string) string {
	u, err := url.Parse(uri)
	if err != nil || (u.Scheme != schemeHTTP && u.Scheme != schemeHTTPS) || u.Host == "" || u.User != nil || u.Path != "/device" ||
		u.RawQuery != "" || u.Fragment != "" {
		return origin + "/device"
	}

	return u.String()
}

// displayCode is the user code as the terminal shows it: ABCD-EFGH.
func displayCode(code string) string {
	if len(code) == 8 && !strings.Contains(code, "-") {
		return code[:4] + "-" + code[4:]
	}

	return code
}

// errCodeExpired ends a sign-in nobody approved in time.
var errCodeExpired = errors.New("the code expired before it was approved: run goal login again")

// pollDeviceToken asks for the session at the service's interval until the person approves or
// denies the code or it expires (RFC 8628 §3.5): authorization_pending waits on, slow_down waits
// 5 s longer from then on.
func pollDeviceToken(ctx context.Context, svc identityService, code deviceCode, wait func(context.Context, time.Duration) error) (string, error) {
	every := time.Duration(code.Interval) * time.Second
	if every <= 0 {
		every = defaultPollEvery
	}

	deadline := time.Now().Add(cmp.Or(time.Duration(code.ExpiresIn)*time.Second, defaultCodeLife))

	for {
		if err := wait(ctx, every); err != nil {
			return "", err
		}

		status, body, err := svc.post(ctx, "/device/token", map[string]string{
			"grant_type": deviceGrant, "device_code": code.DeviceCode, "client_id": cliClientID,
		}, "")
		if err != nil {
			return "", err
		}

		token, slower, err := pollAnswer(status, body)
		if token != "" || err != nil {
			return token, err
		}

		if slower {
			every += slowDownStep
		}

		if time.Now().After(deadline) {
			return "", errCodeExpired
		}
	}
}

// pollAnswer reads one answer to a token poll: the session, or whether to wait longer, or why the
// sign-in ended.
func pollAnswer(status int, body []byte) (token string, slower bool, err error) {
	switch status {
	case http.StatusOK:
		var t struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.Unmarshal(body, &t); err != nil || !sessionTokenForm.MatchString(t.AccessToken) {
			return "", false, errors.New("the identity service answered the sign-in with something that is not a session")
		}

		return t.AccessToken, false, nil
	case http.StatusTooManyRequests:
		return "", true, nil
	}

	var e struct {
		Error string `json:"error"`
	}

	_ = json.Unmarshal(body, &e)

	switch e.Error {
	case "authorization_pending":
		return "", false, nil
	case "slow_down":
		return "", true, nil
	case "expired_token":
		return "", false, errCodeExpired
	case "access_denied":
		return "", false, &accessError{msg: "the sign-in was denied in the browser; nothing was stored"}
	default:
		return "", false, fmt.Errorf("the identity service refused the sign-in (%s)", authAnswer(status, e.Error))
	}
}

// sleepFor waits d, or until ctx ends.
func sleepFor(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// browserCommand is the program and arguments that open page: $BROWSER when set (a program and
// its arguments separated by spaces, as in BROWSER="firefox --new-window"), else the system's URL
// opener.
func browserCommand(getenv func(string) string, page string) []string {
	switch browser := strings.TrimSpace(getenv("BROWSER")); {
	case browser != "":
		return append(strings.Fields(browser), page)
	case runtime.GOOS == "darwin":
		return []string{"open", page}
	case runtime.GOOS == "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", page}
	default:
		return []string{"xdg-open", page}
	}
}

// openBrowser opens page in the person's browser, waiting up to browserWait for the opener to
// say whether it did.
func openBrowser(ctx context.Context, getenv func(string) string, page string) error {
	argv := browserCommand(getenv, page)

	// security: page is an http(s) URL without the code (verificationPage), passed as one argument
	// with no shell; the program is the person's $BROWSER or their system's URL opener.
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // see above
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)

	go func() { done <- cmd.Wait() }()

	timer := time.NewTimer(browserWait)
	defer timer.Stop()

	select {
	case err := <-done:
		return err
	case <-timer.C:
		return nil
	}
}

// tokenLogin reads a personal token (or an API key) from stdin: typed without echo on a
// terminal, else read whole (goal login --token < token-file).
func tokenLogin(out *printer) (credential, error) {
	raw, err := readToken(out.in)
	if err != nil {
		return credential{}, err
	}

	token := strings.TrimSpace(string(raw))
	if token == "" {
		return credential{}, errTokenOnCommandLine
	}

	if !goalKeyForm.MatchString(token) {
		return credential{}, errors.New("that is neither a personal token (goal_pat_…) nor an API key (goal_sk_…); nothing was stored")
	}

	return credential{Kind: kindToken, Token: token}, nil
}

// readToken reads a token from in: without echo when in is a terminal, asking on stderr.
func readToken(in io.Reader) ([]byte, error) {
	if in == nil {
		return nil, nil
	}

	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(os.Stderr, "Paste the token (it is not shown): ")

		b, err := term.ReadPassword(int(f.Fd()))

		fmt.Fprintln(os.Stderr)

		return b, err
	}

	b, err := io.ReadAll(io.LimitReader(in, maxTokenBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read the token from stdin: %w", err)
	}

	return b, nil
}

// checkCredential asks goald who cred is (GET /v1/me), refusing one it does not accept, and names
// them for people.
func checkCredential(ctx context.Context, tg target, cred credential) (string, error) {
	c, err := gen.NewClientWithResponses(tg.base, gen.WithHTTPClient(httpClient(tg.origin, &resolved{cred: cred})))
	if err != nil {
		return "", err
	}

	res, err := c.GetMeWithResponse(ctx)
	if err != nil {
		return "", apiDown(err)
	}

	if res.JSON200 != nil {
		return describeMe(res.JSON200), nil
	}

	if res.StatusCode() == http.StatusUnauthorized {
		return "", &accessError{
			msg:       fmt.Sprintf("%s does not accept that %s: it may have expired or been revoked; nothing was stored", tg.origin, credentialNoun(cred)),
			signedOut: true, origin: tg.origin,
		}
	}

	return "", problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
}

// credentialNoun names a credential for people.
func credentialNoun(c credential) string {
	switch {
	case c.Kind == kindSession:
		return "session"
	case c.isPersonalToken():
		return "personal token"
	default:
		return "API key"
	}
}

// describeMe names who a request acts for: a person by name and email (by email alone while they
// have no name), an API key by name.
func describeMe(me *gen.Me) string {
	switch {
	case me.Mode == gen.None:
		return "the local user"
	case me.User != nil && strings.TrimSpace(me.User.Name) == "":
		return me.User.Email
	case me.User != nil:
		return fmt.Sprintf("%s (%s)", me.User.Name, me.User.Email)
	case me.Key != nil:
		return "the API key " + me.Key.Name
	default:
		return string(me.Kind)
	}
}

// logout runs goal logout [--all]: it ends the stored session in Goal (--all: every session of
// the person) and forgets it on this machine, even when Goal could not be reached. A stored
// personal token or API key is only forgotten: it is revoked in the browser.
func logout(ctx context.Context, out *printer, getenv func(string) string, args []string, kc keychain) error {
	fs := newFlags("logout")
	all := fs.Bool("all", false, "end every session of yours: every browser and the CLI")

	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errUsage
	}

	tg, err := apiTarget(getenv)
	if err != nil {
		return err
	}

	if getenv(envAPIKey) != "" {
		defer out.printf("%s is still set: commands keep using it.\n", envAPIKey)
	}

	store, cred, found, err := storedCredential(getenv, tg.cfg, tg.origin, kc)
	if err != nil {
		return err
	}

	if !found {
		out.printf("Not signed in to %s.\n", tg.origin)

		return nil
	}

	var ended error

	if cred.Kind == kindSession {
		path := "/sign-out"
		if *all {
			path = "/revoke-sessions"
		}

		ended = identityService{base: tg.base, client: httpClient(tg.origin, nil)}.endSession(ctx, path, cred.Token)
	}

	if _, err := store.remove(tg.origin); err != nil {
		return err
	}

	return saySignedOut(out, tg.origin, cred, *all, ended)
}

// saySignedOut tells how goal logout went; ended is why Goal did not end the session, if it did not.
func saySignedOut(out *printer, origin string, cred credential, all bool, ended error) error {
	switch {
	case cred.Kind != kindSession:
		where := "API keys"
		if cred.isPersonalToken() {
			where = "API keys → Personal tokens"
		}

		out.printf("Removed the %s for %s from this machine. It keeps working until it expires: revoke it in Goal under %s if it may have leaked.\n",
			credentialNoun(cred), origin, where)
	case ended != nil:
		return fmt.Errorf("removed the session from this machine, but %s could not end it (%w); it stays valid until it expires, "+
			"in 7 days at most: sign out everywhere from Goal's account menu to end it now", origin, ended)
	case all:
		out.printf("Signed out of %s everywhere: every browser and CLI session of yours ended. Personal tokens and API keys keep working until revoked.\n",
			origin)
	default:
		out.printf("Signed out of %s.\n", origin)
	}

	return nil
}

// whoami runs goal whoami: who the CLI acts as on goald, how, and in which organizations.
func whoami(ctx context.Context, out *printer, getenv func(string) string, args []string, kc keychain) error {
	if len(args) != 0 {
		return errUsage
	}

	tg, err := apiTarget(getenv)
	if err != nil {
		return err
	}

	cred, err := resolveCredential(getenv, tg.cfg, tg.origin, kc)
	if err != nil {
		return err
	}

	c, err := gen.NewClientWithResponses(tg.base, gen.WithHTTPClient(httpClient(tg.origin, cred)))
	if err != nil {
		return err
	}

	res, err := c.GetMeWithResponse(ctx)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if res.JSON200.Mode == gen.None {
		out.printf("The local user on %s: sign-in is off there (GOAL_AUTH=none).\n", tg.origin)

		return nil
	}

	orgs, err := listOrgs(ctx, c)
	if err != nil {
		return err
	}

	sayWho(out, tg.origin, res.JSON200, cred)
	sayToken(out, res.JSON200.Token, orgs)
	sayOrgs(out, orgs)

	return nil
}

// sayWho prints who the CLI acts as and with which credential.
func sayWho(out *printer, origin string, me *gen.Me, cred *resolved) {
	who := describeMe(me)
	if me.User == nil { // the CLI's own words, not a person's name or email
		who = strings.ToUpper(who[:1]) + who[1:]
	}

	out.printf("%s on %s\n", who, origin)

	switch {
	case cred == nil:
	case cred.store == nil:
		out.printf("With the %s in %s\n", credentialNoun(cred.cred), envAPIKey)
	default:
		out.printf("With the %s goal login stored in %s\n", credentialNoun(cred.cred), cred.store.where())
	}

	if me.PlatformAdmin {
		out.printf("Platform admin\n")
	}

	if me.Session != nil {
		out.printf("Session expires %s\n", me.Session.ExpiresAt.Local().Format("Jan 2 15:04"))
	}
}

// sayToken prints the personal token the CLI acts through, if any: its name, organization, scopes
// and expiry ("Personal token laptop in Acme · expires Nov 23").
func sayToken(out *printer, t *gen.MeToken, orgs []gen.OrgSummary) {
	if t == nil {
		return
	}

	org := t.OrgId
	for _, o := range orgs {
		if o.Id == t.OrgId {
			org = o.Name
		}
	}

	out.printf("Personal token %s in %s · expires %s\n", t.Name, org, t.ExpiresAt.Local().Format("Jan 2"))
	out.printf("Can: %s\n", strings.Join(t.Scopes, ", "))
}

// sayOrgs prints the organizations the CLI may act in, with the role held in each.
func sayOrgs(out *printer, orgs []gen.OrgSummary) {
	if len(orgs) == 0 {
		out.printf("In no organization yet: ask an owner for an invitation.\n")

		return
	}

	out.printf("Organizations:\n")

	for _, o := range orgs {
		role := ""
		if o.Role != nil {
			role = string(*o.Role)
		}

		out.printf("  %-20s %-24s %s\n", o.Slug, o.Name, role)
	}
}

// identityService reaches the identity service's endpoints the CLI uses, at goald's /api/auth.
type identityService struct {
	base   string
	client *http.Client
}

// post sends body as JSON to /api/auth<path>, with token as its bearer credential when set, and
// reads the answer.
func (s identityService) post(ctx context.Context, path string, body any, token string) (int, []byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+"/api/auth"+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := s.client.Do(req)
	if err != nil {
		return 0, nil, apiDown(err)
	}
	defer func() { _ = res.Body.Close() }()

	answer, err := io.ReadAll(io.LimitReader(res.Body, maxAuthAnswer))

	return res.StatusCode, answer, err
}

// deviceCode asks for a device code for the CLI.
func (s identityService) deviceCode(ctx context.Context) (deviceCode, error) {
	status, body, err := s.post(ctx, "/device/code", map[string]string{"client_id": cliClientID}, "")
	if err != nil {
		return deviceCode{}, err
	}

	switch {
	case status == http.StatusTooManyRequests:
		return deviceCode{}, errors.New("too many sign-ins from this network: try again in a minute")
	case status == http.StatusNotFound:
		return deviceCode{}, errors.New("goald serves no sign-in (is it running with GOAL_AUTH=session?)")
	case status != http.StatusOK:
		return deviceCode{}, fmt.Errorf("the identity service did not start the sign-in (%s)", authAnswer(status, ""))
	}

	var code deviceCode
	if err := json.Unmarshal(body, &code); err != nil || code.DeviceCode == "" || !userCodeChars.MatchString(code.UserCode) {
		return deviceCode{}, errors.New("the identity service answered the sign-in with something that is not a device code")
	}

	return code, nil
}

// endSession ends the session token is (POST /sign-out), or every session of its person
// (/revoke-sessions). A session that is gone already (401) is ended.
func (s identityService) endSession(ctx context.Context, path, token string) error {
	status, _, err := s.post(ctx, path, map[string]string{}, token)
	if err != nil {
		return err
	}

	if status != http.StatusOK && status != http.StatusUnauthorized {
		return fmt.Errorf("it answered %s", authAnswer(status, ""))
	}

	return nil
}

// authAnswer names an identity service answer in an error: its status, and its error code when it
// is one (never other text of the answer, which goes to a terminal).
func authAnswer(status int, code string) string {
	s := fmt.Sprintf("%d %s", status, http.StatusText(status))
	if errorCodeForm.MatchString(code) {
		s += ", " + code
	}

	return s
}
