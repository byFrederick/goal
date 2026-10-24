//go:build e2e

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	osexec "os/exec"
	"strings"
	"testing"
)

// personalTokens is spec 3 §15 (3-12b) on the real stack, after membersFlow handed e2e-labs to the
// invitee: the former owner, now its admin, creates a personal token in the browser (the
// client's cookie session; their secret once, uncached); the real CLI uses it through
// GOAL_API_KEY to read (goal whoami, goal factory list) and to make a write its scopes allow
// (goal factory push, a new factory's draft) but not one they do not (goal andon pull, exit
// 77); the identity service never accepts it; and when the new owner removes them from the
// organization, the token stops at once.
func (s *e2eStack) personalTokens(t *testing.T, newOwner *e2eStack) {
	t.Helper()

	secret := s.createToken(t)
	goal := goalWith(t, secret)
	s.useToken(t, secret, goal)

	var org struct {
		ID string `json:"id"`
	}
	newOwner.decode(t, newOwner.do(t, http.MethodGet, "/v1/orgs/e2e-labs", ""), &org)

	remove := fmt.Sprintf(`{"organizationId":%q,"memberIdOrEmail":%q}`, org.ID, s.owner)
	if res := newOwner.do(t, http.MethodPost, "/api/auth/organization/remove-member", remove); res.status != http.StatusOK {
		t.Fatalf("remove the former owner = %d %s", res.status, res.body)
	}

	var exit *osexec.ExitError
	if out, err := goal("whoami").CombinedOutput(); !errors.As(err, &exit) || exit.ExitCode() != 77 {
		t.Errorf("goal whoami with a removed member's token = %v\n%s; want exit 77", err, out)
	}

	if got := withBearer(t, secret, "/v1/me"); got != http.StatusUnauthorized {
		t.Errorf("a removed member's token = %d; want 401", got)
	}
}

// createToken creates the caller's personal token in e2e-labs, in the browser, and returns its
// secret.
func (s *e2eStack) createToken(t *testing.T) string {
	t.Helper()

	res := s.do(t, http.MethodPost, "/v1/orgs/e2e-labs/personal-tokens",
		`{"name":"cli","scopes":["org:read","factory:read","factory:create","definition:edit"],"expires_in_days":7}`)
	if res.status != http.StatusCreated || res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("create token = %d %q %s; want 201, no-store", res.status, res.header.Get("Cache-Control"), res.body)
	}

	var created struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(res.body, &created); err != nil || !strings.HasPrefix(created.Secret, "goal_pat_") {
		t.Fatalf("created = %s, %v; want a goal_pat_ secret", res.body, err)
	}

	return created.Secret
}

// goalWith runs the stack's CLI with the personal token secret in GOAL_API_KEY.
func goalWith(t *testing.T, secret string) func(args ...string) *osexec.Cmd {
	t.Helper()

	return func(args ...string) *osexec.Cmd {
		cmd := osexec.CommandContext(t.Context(), "../../bin/goal", args...) //nolint:gosec // the stack's own CLI
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "XDG_CONFIG_HOME=" + t.TempDir(), "GOAL_API_URL=" + e2ePublicURL,
			"GOAL_API_KEY=" + secret,
		}

		return cmd
	}
}

// useToken reads and writes with the token from the CLI, within its scopes and not beyond, and
// checks what it never reaches.
func (s *e2eStack) useToken(t *testing.T, secret string, goal func(args ...string) *osexec.Cmd) {
	t.Helper()

	if out, err := goal("whoami").CombinedOutput(); err != nil || !strings.Contains(string(out), "Personal token cli in E2E Labs") ||
		!strings.Contains(string(out), s.owner) || strings.Contains(string(out), "Platform admin") {
		t.Errorf("goal whoami with the token = %v\n%s; want the person through cli in E2E Labs, no platform admin", err, out)
	}

	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../../internal/config/testdata/line")); err != nil {
		t.Fatal(err)
	}

	if out, err := goal("factory", "push", dir).CombinedOutput(); err != nil {
		t.Errorf("goal factory push with the token = %v\n%s; want the new factory's draft", err, out)
	}

	if out, err := goal("factory", "list").CombinedOutput(); err != nil || !strings.Contains(string(out), "acme-line") {
		t.Errorf("goal factory list with the token = %v\n%s; want acme-line", err, out)
	}

	var exit *osexec.ExitError
	if out, err := goal("andon", "pull", "acme-line", "--reason", "stop").CombinedOutput(); !errors.As(err, &exit) || exit.ExitCode() != 77 {
		t.Errorf("goal andon pull without the scope = %v\n%s; want exit 77", err, out)
	}

	for path, want := range map[string]int{
		"/v1/orgs/default": http.StatusNotFound, "/v1/orgs/e2e-labs/api-keys": http.StatusForbidden,
		"/v1/orgs/e2e-labs/personal-tokens": http.StatusForbidden, "/api/auth/get-session": http.StatusUnauthorized,
	} {
		if got := withBearer(t, secret, path); got != want {
			t.Errorf("GET %s with the token = %d; want %d", path, got, want)
		}
	}
}

// withBearer sends GET path to goald with secret as the bearer credential and no cookie.
func withBearer(t *testing.T, secret, path string) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e2ePublicURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Authorization", "Bearer "+secret)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	return res.StatusCode
}
