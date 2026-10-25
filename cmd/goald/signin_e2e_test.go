//go:build e2e

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/postgres"
	"github.com/byFrederick/goal/internal/adapters/s3store"
)

// The throwaway stack of the sign-in end-to-end test (task e2e:auth): goald and the auth service
// on ports of their own, a database and a bucket of their own, compose's Mailpit. The install's
// goald (127.0.0.1:8080), database and bucket are never touched.
const (
	e2eHost       = "127.0.0.1" // also the client's address, as goald and the auth service see it
	e2eListen     = e2eHost + ":18089"
	e2eAuthListen = e2eHost + ":18091"
	e2eMailpit    = "http://127.0.0.1:8025"
	e2ePublicURL  = "http://" + e2eListen
)

// e2eStack is one run's goald, auth service, database and bucket.
type e2eStack struct {
	owner    string // the bootstrap owner's email
	database string
	client   *http.Client
}

// forgedClientAddr is the X-Forwarded-For every request of the stack's client carries: goald
// believes no proxy here, as on a laptop, so it must never reach the auth service.
const forgedClientAddr = "203.0.113.50"

// Spec 3 §13 and §15 (3-9): with GOAL_AUTH=session, the bootstrap owner signs in with the code
// Mailpit received, through goald's /api/auth proxy; goald then answers as them (platform admin,
// owner of every organization), creates organizations owned by them, refuses anyone without a
// session, lets them invite someone who joins and to whom they hand that organization (3-11,
// members_e2e_test.go), whose personal token works from the CLI until they are removed from it
// (3-12b, tokens_e2e_test.go), and when they sign out ends their stream and their access at once. With
// GOAL_E2E_WEB=1, task web:e2e-auth (3-10's Playwright flow) runs against the same stack.
func TestE2E_SignInWithAnEmailedCode(t *testing.T) {
	t.Parallel()

	s := startE2EStack(t)

	if res := s.do(t, http.MethodGet, "/v1/orgs", ""); res.status != http.StatusUnauthorized || res.header.Get("WWW-Authenticate") == "" {
		t.Fatalf("anonymous /v1/orgs = %d; want 401 with a challenge", res.status)
	}

	s.signIn(t)
	s.checkClientAddress(t)
	s.checkTheOwner(t)
	s.checkTheirOrganizations(t)
	newOwner := s.membersFlow(t)
	s.checkAPIKeys(t)
	s.personalTokens(t, newOwner)
	s.cliLogsIn(t)
	s.signOutEndsTheStream(t)

	if res := s.do(t, http.MethodGet, "/v1/me", ""); res.status != http.StatusUnauthorized {
		t.Errorf("/v1/me after signing out = %d; want 401", res.status)
	}

	if os.Getenv("GOAL_E2E_WEB") != "" {
		s.webFlow(t)
	}
}

// checkClientAddress checks the auth service records and rate-limits the client by the address
// goald's proxy names (X-Goal-Client-Ip, Better Auth's ipAddressHeaders): with no proxy believed,
// the peer's, never the X-Forwarded-For the client forged (phase 4 review, M3).
func (s *e2eStack) checkClientAddress(t *testing.T) {
	t.Helper()

	conn, err := pgx.Connect(t.Context(), roleURL(t, s.database, postgres.AuthRole))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(t.Context())) }()

	var sessionIPs []string
	if err := conn.QueryRow(t.Context(), `SELECT array_agg(DISTINCT "ipAddress") FROM auth.session`).Scan(&sessionIPs); err != nil {
		t.Fatal(err)
	}

	var counted, forged int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FILTER (WHERE key LIKE $1||'%'), count(*) FILTER (WHERE key LIKE '%'||$2||'%')
		FROM auth."rateLimit"`, e2eHost, forgedClientAddr).Scan(&counted, &forged); err != nil {
		t.Fatal(err)
	}

	if len(sessionIPs) != 1 || sessionIPs[0] != e2eHost || counted == 0 || forged != 0 {
		t.Errorf("session addresses %v, rate limits under 127.0.0.1 %d, under the forged address %d; want the peer's alone", sessionIPs, counted, forged)
	}
}

// checkTheOwner checks goald answers as the bootstrap owner, a platform admin since signing in.
func (s *e2eStack) checkTheOwner(t *testing.T) {
	t.Helper()

	var me struct {
		Mode          string `json:"mode"`
		PlatformAdmin bool   `json:"platform_admin"`
		User          struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	s.decode(t, s.do(t, http.MethodGet, "/v1/me", ""), &me)

	if me.Mode != "session" || me.User.Email != s.owner || !me.PlatformAdmin {
		t.Fatalf("me = %+v; want the bootstrap owner, a platform admin", me)
	}
}

// checkTheirOrganizations checks the owner has every organization (the default one, from before
// sign-in) and owns the one they create.
func (s *e2eStack) checkTheirOrganizations(t *testing.T) {
	t.Helper()

	var orgs struct {
		Orgs []struct {
			Slug string  `json:"slug"`
			Role *string `json:"role"`
		} `json:"orgs"`
	}
	s.decode(t, s.do(t, http.MethodGet, "/v1/orgs", ""), &orgs)

	if len(orgs.Orgs) != 1 || orgs.Orgs[0].Slug != "default" || orgs.Orgs[0].Role == nil || *orgs.Orgs[0].Role != "owner" {
		t.Fatalf("orgs = %+v; want the default organization, owned by the bootstrap owner", orgs)
	}

	if created := s.do(t, http.MethodPost, "/v1/orgs", `{"slug":"e2e-labs","name":"E2E Labs"}`); created.status != http.StatusCreated {
		t.Fatalf("create org = %d %s; want 201", created.status, created.body)
	}

	var org struct {
		Role string   `json:"role"`
		Can  []string `json:"can"`
	}
	s.decode(t, s.do(t, http.MethodGet, "/v1/orgs/e2e-labs", ""), &org)

	if org.Role != "owner" || !strings.Contains(strings.Join(org.Can, ","), "org:delete") {
		t.Errorf("the new org = %+v; want it owned by its creator", org)
	}
}

var userCodeLine = regexp.MustCompile(`Open (\S+) and enter ([A-Z0-9]{4}-[A-Z0-9]{4})`)

// cliLogsIn runs the real goal login (bin/goal, spec 3 §9 and PR 3-13) against the stack: the CLI
// asks for a code and prints it; the owner, whose browser session is the client's cookie jar,
// reads the request through goald and approves it through the proxy; the CLI stores its session
// (in a 0600 file) and goal whoami answers as the owner. goal logout ends that session in Goal, so
// whoami then exits 77, while the browser's session is untouched.
func (s *e2eStack) cliLogsIn(t *testing.T) {
	t.Helper()

	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "XDG_CONFIG_HOME=" + t.TempDir(), "GOAL_API_URL=" + e2ePublicURL, "BROWSER=true"}
	goal := func(args ...string) *osexec.Cmd {
		cmd := osexec.CommandContext(t.Context(), "../../bin/goal", args...) //nolint:gosec // the stack's own CLI
		cmd.Env = env

		return cmd
	}

	code, done := startLogin(t, goal("login", "--store", "file"))
	s.approveDevice(t, code)
	done()

	if out, err := goal("whoami").CombinedOutput(); err != nil || !strings.Contains(string(out), s.owner) {
		t.Errorf("goal whoami = %v\n%s", err, out)
	}

	if out, err := goal("logout").CombinedOutput(); err != nil || !strings.Contains(string(out), "Signed out of "+e2ePublicURL) {
		t.Errorf("goal logout = %v\n%s", err, out)
	}

	var exit *osexec.ExitError
	if out, err := goal("whoami").CombinedOutput(); !errors.As(err, &exit) || exit.ExitCode() != 77 || !strings.Contains(string(out), "run goal login") {
		t.Errorf("goal whoami after logout = %v\n%s; want exit 77", err, out)
	}

	if res := s.do(t, http.MethodGet, "/v1/me", ""); res.status != http.StatusOK {
		t.Errorf("the browser's session after goal logout = %d; want it untouched", res.status)
	}
}

// startLogin starts goal login and reads the code it prints; done waits for it to store the
// session.
func startLogin(t *testing.T, login *osexec.Cmd) (string, func()) {
	t.Helper()

	stdout, err := login.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer

	login.Stderr = &stderr

	if err := login.Start(); err != nil {
		t.Fatalf("goal login: %v", err)
	}

	lines := bufio.NewReader(stdout)

	first, err := lines.ReadString('\n')
	if m := userCodeLine.FindStringSubmatch(first); err == nil && m != nil && m[1] == e2ePublicURL+"/device" {
		return m[2], func() {
			rest, _ := io.ReadAll(lines)
			if err := login.Wait(); err != nil || !strings.Contains(string(rest), "Signed in to "+e2ePublicURL) {
				t.Fatalf("goal login = %v\n%s%s%s", err, first, rest, stderr.String())
			}
		}
	}

	t.Fatalf("goal login printed %q, %v (%s); want where to enter the code", first, err, stderr.String())

	return "", nil
}

// approveDevice does what Device-Approve does with the typed code: read the request through goald
// (the Goal CLI, from this machine), open the code, approve it.
func (s *e2eStack) approveDevice(t *testing.T, code string) {
	t.Helper()

	var request struct {
		ClientName string `json:"client_name"`
		SameIP     bool   `json:"same_ip"`
		UserAgent  string `json:"user_agent"`
	}
	s.decode(t, s.do(t, http.MethodGet, "/v1/auth/device/"+code, ""), &request)

	if request.ClientName != "Goal CLI" || !request.SameIP || !strings.HasPrefix(request.UserAgent, "goal/") {
		t.Errorf("the device request = %+v; want the Goal CLI from this machine", request)
	}

	if res := s.do(t, http.MethodGet, "/api/auth/device?user_code="+code, ""); res.status != http.StatusOK {
		t.Fatalf("open the code = %d %s", res.status, res.body)
	}

	if res := s.do(t, http.MethodPost, "/api/auth/device/approve", fmt.Sprintf(`{"userCode":%q}`, code)); res.status != http.StatusOK {
		t.Fatalf("approve = %d %s", res.status, res.body)
	}
}

// checkAPIKeys checks spec 3 §4.3 against the real stack (3-12): the owner, signed in just now,
// creates a key of e2e-labs; its secret comes once, uncached; the key acts in its organization
// only, never manages keys, and stops at once when revoked.
func (s *e2eStack) checkAPIKeys(t *testing.T) {
	t.Helper()

	res := s.do(t, http.MethodPost, "/v1/orgs/e2e-labs/api-keys", `{"name":"ci","scopes":["org:read","factory:read"],"role_ceiling":"viewer"}`)
	if res.status != http.StatusCreated || res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("create key = %d %q %s; want 201, no-store", res.status, res.header.Get("Cache-Control"), res.body)
	}

	var created struct {
		Key struct {
			ID string `json:"id"`
		} `json:"key"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(res.body, &created); err != nil {
		t.Fatal(err)
	}

	withKey := func(method, path string) int {
		req, err := http.NewRequestWithContext(t.Context(), method, e2ePublicURL+path, nil)
		if err != nil {
			t.Fatal(err)
		}

		req.Header.Set("Authorization", "Bearer "+created.Secret)

		res, err := http.DefaultClient.Do(req) // no cookies: the key alone
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()

		return res.StatusCode
	}

	for path, want := range map[string]int{
		"/v1/me": http.StatusOK, "/v1/orgs/e2e-labs/factories": http.StatusOK, "/v1/orgs/default": http.StatusNotFound,
		"/v1/orgs/e2e-labs/api-keys": http.StatusForbidden,
	} {
		if got := withKey(http.MethodGet, path); got != want {
			t.Errorf("GET %s with the key = %d; want %d", path, got, want)
		}
	}

	if res := s.do(t, http.MethodDelete, "/v1/orgs/e2e-labs/api-keys/"+created.Key.ID, ""); res.status != http.StatusNoContent {
		t.Fatalf("revoke = %d %s; want 204", res.status, res.body)
	}

	if got := withKey(http.MethodGet, "/v1/me"); got != http.StatusUnauthorized {
		t.Errorf("the revoked key = %d; want 401", got)
	}
}

// startE2EStack starts the throwaway stack, removed when the test ends.
func startE2EStack(t *testing.T) *e2eStack {
	t.Helper()

	suffix := randomSuffix(t)
	s := &e2eStack{owner: "owner-" + suffix + "@example.com"}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	s.client = &http.Client{Jar: jar, Timeout: 30 * time.Second}

	database := e2eDatabase(t, "goal_e2e_"+suffix)
	s.database = database
	bucket := e2eBucket(t, "goal-e2e-"+suffix)

	startMailpit(t)

	authKey, gatewayKey := randomSuffix(t)+randomSuffix(t)+randomSuffix(t), randomSuffix(t)+randomSuffix(t)+randomSuffix(t)
	proxySecret := randomSuffix(t) + randomSuffix(t) + randomSuffix(t) + randomSuffix(t)
	home, _ := os.UserHomeDir()

	goaldEnv := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TMPDIR=" + t.TempDir(), "GOAL_AUTH=session", "GOAL_LISTEN=" + e2eListen,
		"GOAL_PUBLIC_URL=" + e2ePublicURL, "GOAL_AUTH_UPSTREAM=http://" + e2eAuthListen, "GOAL_GATEWAY_KEY=" + gatewayKey,
		"GOAL_MIGRATE_DATABASE_URL=" + roleURL(t, database, postgres.OwnerRole), "GOAL_DATABASE_URL=" + roleURL(t, database, postgres.AppRole),
		"GOAL_S3_BUCKET=" + bucket, "GOAL_S3_ENDPOINT=" + os.Getenv("GOAL_S3_ENDPOINT"), "GOAL_S3_ACCESS_KEY=" + os.Getenv("GOAL_S3_ACCESS_KEY"),
		"GOAL_S3_SECRET_KEY=" + os.Getenv("GOAL_S3_SECRET_KEY"), "AWS_REGION=us-east-1",
		"GOAL_LOG_FORMAT=text", "GOAL_AUTH_PROXY_SECRET=" + proxySecret,
	}
	if docker := os.Getenv("DOCKER_HOST"); docker != "" {
		goaldEnv = append(goaldEnv, "DOCKER_HOST="+docker)
	}

	// goald first: it migrates the database, whose schema the auth service checks at its start.
	s.start(t, "goald", e2eListen, "/v1/health", []string{"../../bin/goald"}, goaldEnv)
	s.start(t, "auth service", e2eAuthListen, "/api/auth/ok", []string{"node", "../../auth/dist/server.js"}, []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GOAL_PUBLIC_URL=" + e2ePublicURL, "GOAL_AUTH_LISTEN=" + e2eAuthListen,
		"GOAL_AUTH_DATABASE_URL=" + roleURL(t, database, postgres.AuthRole), "BETTER_AUTH_SECRET=" + authKey,
		"GOAL_BOOTSTRAP_OWNER_EMAIL=" + s.owner, "GOAL_AUTH_PROXY_SECRET=" + proxySecret,
	})

	return s
}

// start runs a process of the stack with env alone, waits until GET health on listen answers 200,
// and stops it (SIGTERM, then a kill) when the test ends. Its output goes to a log printed when
// the test fails.
func (s *e2eStack) start(t *testing.T, name, listen, health string, argv, env []string) {
	t.Helper()

	logPath := filepath.Join(t.TempDir(), strings.ReplaceAll(name, " ", "-")+".log")

	logFile, err := os.Create(logPath) //nolint:gosec // a file of the test's own temporary directory
	if err != nil {
		t.Fatal(err)
	}

	cmd := osexec.Command(argv[0], argv[1:]...) //nolint:gosec,noctx // the stack's own binaries, stopped in Cleanup
	cmd.Env, cmd.Stdout, cmd.Stderr = env, logFile, logFile

	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}

	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)

		done := make(chan struct{})

		go func() { _ = cmd.Wait(); close(done) }()

		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()

			<-done
		}

		_ = logFile.Close()

		if t.Failed() {
			if out, err := os.ReadFile(logPath); err == nil { //nolint:gosec // the test's own log
				t.Logf("%s log:\n%s", name, out)
			}
		}
	})

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get("http://" + listen + health) //nolint:noctx // a bounded poll of the stack's own health check
		if err == nil {
			_ = res.Body.Close()

			if res.StatusCode == http.StatusOK {
				return
			}
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Fatalf("%s did not answer GET %s on %s within a minute", name, health, listen)
}

// signIn asks for a code for the bootstrap owner through goald's proxy, reads it from Mailpit and
// signs in with it; the session cookie lands in the client's jar.
func (s *e2eStack) signIn(t *testing.T) {
	t.Helper()

	since := time.Now().Add(-time.Second)

	sent := s.do(t, http.MethodPost, "/api/auth/email-otp/send-verification-otp", fmt.Sprintf(`{"email":%q,"type":"sign-in"}`, s.owner))
	if sent.status != http.StatusOK {
		t.Fatalf("send a code = %d %s", sent.status, sent.body)
	}

	code := s.codeFromMailpit(t, since)

	signed := s.do(t, http.MethodPost, "/api/auth/sign-in/email-otp", fmt.Sprintf(`{"email":%q,"otp":%q}`, s.owner, code))
	if signed.status != http.StatusOK {
		t.Fatalf("sign in = %d %s", signed.status, signed.body)
	}

	u, _ := url.Parse(e2ePublicURL)
	for _, c := range s.client.Jar.Cookies(u) {
		if c.Name == "goal.session_token" {
			return
		}
	}

	t.Fatal("signing in set no goal.session_token cookie")
}

var sixDigits = regexp.MustCompile(`\b(\d{6})\b`)

// codeFromMailpit is the 6-digit code of the newest message to the owner received after since.
func (s *e2eStack) codeFromMailpit(t *testing.T, since time.Time) string {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var search struct {
			Messages []struct {
				ID      string    `json:"ID"`
				Created time.Time `json:"Created"`
			} `json:"messages"`
		}

		res, err := mailpitGet(t, "/api/v1/search?query="+url.QueryEscape(`to:"`+s.owner+`" subject:"sign-in code"`))
		if err == nil && json.NewDecoder(res.Body).Decode(&search) == nil {
			for _, m := range search.Messages {
				if m.Created.After(since) {
					return s.codeOf(t, m.ID)
				}
			}
		}

		if res != nil {
			_ = res.Body.Close()
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("no sign-in code for %s reached Mailpit within 30 s", s.owner)

	return ""
}

func (s *e2eStack) codeOf(t *testing.T, id string) string {
	t.Helper()

	res, err := mailpitGet(t, "/api/v1/message/"+url.PathEscape(id))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	var message struct {
		Text string `json:"Text"`
	}
	if err := json.NewDecoder(res.Body).Decode(&message); err != nil {
		t.Fatal(err)
	}

	m := sixDigits.FindStringSubmatch(message.Text)
	if m == nil {
		t.Fatalf("message %s has no 6-digit code", id)
	}

	return m[1]
}

// signOutEndsTheStream opens the default organization's stream, signs out, and expects goald to
// end the stream at once (the session's goal_access notice).
func (s *e2eStack) signOutEndsTheStream(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e2ePublicURL+"/v1/orgs/default/stream", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	stream, err := (&http.Client{Jar: s.client.Jar}).Do(req)
	if err != nil || stream.StatusCode != http.StatusOK {
		t.Fatalf("stream = %v, %v", stream, err)
	}
	defer func() { _ = stream.Body.Close() }()

	lines := bufio.NewReader(stream.Body)
	if first, err := lines.ReadString('\n'); err != nil || !strings.HasPrefix(first, "retry:") {
		t.Fatalf("stream opens with %q, %v", first, err)
	}

	if res := s.do(t, http.MethodPost, "/api/auth/sign-out", `{}`); res.status != http.StatusOK {
		t.Fatalf("sign out = %d %s", res.status, res.body)
	}

	for {
		line, err := lines.ReadString('\n')
		if errors.Is(err, io.EOF) {
			return
		}

		if err != nil {
			t.Fatalf("the stream did not end after signing out: %v", err)
		}

		if strings.HasPrefix(line, "event: item") {
			t.Fatalf("the stream sent %q after signing out", line)
		}
	}
}

// webFlow runs 3-10's Playwright sign-in (task web:e2e-auth) against this stack.
func (s *e2eStack) webFlow(t *testing.T) {
	t.Helper()

	cmd := osexec.CommandContext(t.Context(), "pnpm", "exec", "playwright", "test", "--config", "playwright.auth.config.ts") //nolint:gosec // fixed arguments
	cmd.Dir = "../../web"

	cmd.Env = append(os.Environ(), "GOAL_E2E_AUTH_URL="+e2ePublicURL, "GOAL_MAILPIT_URL="+e2eMailpit, "GOAL_BOOTSTRAP_OWNER_EMAIL="+s.owner,
		"GOAL_MAILPIT_AUTH="+mailpitCredentials(t))

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("task web:e2e-auth against the stack: %v\n%s", err, out)
	} else {
		t.Logf("task web:e2e-auth against the stack:\n%s", out)
	}
}

// e2eResponse is one of goald's answers, read whole.
type e2eResponse struct {
	status int
	header http.Header
	body   []byte
}

// do sends a request to goald with the client's cookies, from goald's own origin, naming a
// client address of its own in X-Forwarded-For that goald must not believe.
func (s *e2eStack) do(t *testing.T, method, path, body string) e2eResponse {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, e2ePublicURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", e2ePublicURL)
	req.Header.Set("X-Forwarded-For", forgedClientAddr)

	res, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	return e2eResponse{status: res.StatusCode, header: res.Header, body: bytes.TrimSpace(raw)}
}

func (s *e2eStack) decode(t *testing.T, res e2eResponse, out any) {
	t.Helper()

	if res.status != http.StatusOK {
		t.Fatalf("status %d %s, want 200", res.status, res.body)
	}

	if err := json.Unmarshal(res.body, out); err != nil {
		t.Fatalf("decode %s: %v", res.body, err)
	}
}

func randomSuffix(t *testing.T) string {
	t.Helper()

	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}

	return hex.EncodeToString(b)
}

// startMailpit starts compose.yaml's Mailpit, alone, from the repository root, unless it runs
// already: the test never restarts the install's (task e2e:auth creates its password file).
func startMailpit(t *testing.T) {
	t.Helper()

	cmd := osexec.CommandContext(t.Context(), "docker", "compose", "up", "--detach", "--wait", "--no-recreate", "mailpit") //nolint:gosec // fixed arguments
	cmd.Dir = "../.."

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("docker compose up mailpit: %v\n%s", err, out)
	}
}

// mailpitCredentials is "user:password" from the file goal up (or task e2e:auth) wrote for
// Mailpit's UI and API, $XDG_CONFIG_HOME/goal/mailpit-ui-auth (default ~/.config/goal/...); empty
// when there is none, as for a Mailpit started before it needed one.
func mailpitCredentials(t *testing.T) string {
	t.Helper()

	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}

		dir = filepath.Join(home, ".config")
	}

	raw, err := os.ReadFile(filepath.Join(dir, "goal", "mailpit-ui-auth")) //nolint:gosec // the user's own config dir
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(raw))
}

// mailpitGet GETs path of Mailpit's API with its credentials.
func mailpitGet(t *testing.T, path string) (*http.Response, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e2eMailpit+path, http.NoBody)
	if err != nil {
		return nil, err
	}

	if user, password, ok := strings.Cut(mailpitCredentials(t), ":"); ok {
		req.SetBasicAuth(user, password)
	}

	return http.DefaultClient.Do(req)
}

// e2eDatabase creates database name in compose's PostgreSQL (GOAL_TEST_DATABASE_URL, its
// superuser) with the grants goal db init gives, and drops it when the test ends. The roles exist
// already and keep their passwords: goald and the auth service log in with them.
func e2eDatabase(t *testing.T, name string) string {
	t.Helper()

	base := os.Getenv("GOAL_TEST_DATABASE_URL")
	if base == "" {
		t.Fatal("GOAL_TEST_DATABASE_URL is not set; run task e2e:auth")
	}

	execSQL := func(url, sql string) {
		t.Helper()

		ctx := context.WithoutCancel(t.Context()) // also run by Cleanup

		conn, err := pgx.Connect(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close(ctx) }()

		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}

	execSQL(base, "CREATE DATABASE "+name)
	t.Cleanup(func() { execSQL(base, "DROP DATABASE "+name+" WITH (FORCE)") })

	u.Path = "/" + name
	execSQL(u.String(), "REVOKE TEMPORARY, CONNECT ON DATABASE "+name+" FROM PUBLIC; GRANT CONNECT, CREATE ON DATABASE "+name+" TO goal_owner; "+
		"GRANT CONNECT ON DATABASE "+name+" TO goal_app, goal_auth; GRANT USAGE, CREATE ON SCHEMA public TO goal_owner; "+
		"GRANT USAGE ON SCHEMA public TO goal_app, goal_auth; CREATE EXTENSION IF NOT EXISTS vector")

	return name
}

// roleURL is the throwaway database as role: compose's superuser (GOAL_TEST_DATABASE_URL)
// switching to role with the startup parameter, as the integration tests connect, so no role's
// password is needed or changed. goald's and the auth service's role checks look at the role they
// act as (current_user).
func roleURL(t *testing.T, database, role string) string {
	t.Helper()

	u, err := url.Parse(os.Getenv("GOAL_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}

	// %20, not +: libpq-style parsers (pgx, node-postgres) do not read + as a space.
	u.Path = "/" + database
	u.RawQuery = strings.TrimPrefix(u.RawQuery+"&options=-c%20role%3D"+role, "&")

	return u.String()
}

// e2eBucket creates bucket in compose's RustFS (GOAL_S3_*), and removes it with every version
// when the test ends.
func e2eBucket(t *testing.T, bucket string) string {
	t.Helper()

	cfg := s3store.Config{
		Bucket: bucket, Endpoint: os.Getenv("GOAL_S3_ENDPOINT"), AccessKey: os.Getenv("GOAL_S3_ACCESS_KEY"), SecretKey: os.Getenv("GOAL_S3_SECRET_KEY"),
	}

	store, err := s3store.New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.EnsureBucket(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { removeBucket(t, cfg) })

	return bucket
}

func removeBucket(t *testing.T, cfg s3store.Config) {
	t.Helper()

	ctx := context.WithoutCancel(t.Context())

	ac, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")))
	if err != nil {
		t.Errorf("cleanup: %v", err)

		return
	}

	client := s3.NewFromConfig(ac, func(o *s3.Options) { o.BaseEndpoint, o.UsePathStyle = aws.String(cfg.Endpoint), true })
	pages := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(cfg.Bucket)})

	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			t.Errorf("cleanup: %v", err)

			return
		}

		for _, o := range page.Contents {
			_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(cfg.Bucket), Key: o.Key})
		}
	}

	if _, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(cfg.Bucket)}); err != nil {
		t.Errorf("cleanup: %v", err)
	}
}
