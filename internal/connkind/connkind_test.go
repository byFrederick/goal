package connkind_test

import (
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The platform kinds of spec 4 §4.2 parse, pass their rules and are marked as the platform's.
func TestPlatform(t *testing.T) {
	t.Parallel()

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, k.Name)

		if !k.Platform || k.Title == "" {
			t.Errorf("%s: platform %v, title %q; want a titled platform kind", k.Name, k.Platform, k.Title)
		}
	}

	want := []string{
		"anthropic", "aws", "bedrock", kindSubscription, "confluence", "datadog", "github", "gitlab", "http-api-key",
		"jira", "mcp-server", "pagerduty", "slack",
	}
	if !slices.Equal(names, want) {
		t.Errorf("platform kinds %v; want %v", names, want)
	}

	models := map[string]bool{}
	for _, k := range kinds {
		models[k.Name] = k.Spec.Model
	}

	for name, model := range models {
		if want := name == kindAnthropic || name == kindSubscription || name == "bedrock"; model != want {
			t.Errorf("%s: model %v; want %v", name, model, want)
		}
	}
}

// Names of the platform kinds the tests look at.
const (
	kindGitHub       = "github"
	kindAnthropic    = "anthropic"
	kindSubscription = "claude-subscription"
	kindMCPServer    = "mcp-server"
	playwright       = "npx -y @playwright/mcp@0.0.41"
)

// renderValues are typical values of the platform kinds that need some; minted are an aws-sts
// minter's.
var (
	renderValues = map[string]map[string]string{
		kindGitHub:       {"token": "v1"},
		"gitlab":         {"token": "v2"},
		kindAnthropic:    {"key": "v3"},
		kindSubscription: {"token": "v4"},
		"http-api-key":   {"prefix": "ACME_API", "base_url": "https://api.example.com", "key": "v5"},
		kindMCPServer:    {"command": playwright},
	}
	minted = map[string]string{"access_key_id": "m1", "secret_access_key": "m2", "session_token": "m3"}
)

// Each platform kind renders the env its runs need from typical values.
func TestPlatform_RenderEnv(t *testing.T) {
	t.Parallel()

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	wantEnv := map[string][]string{
		kindGitHub:       {"GH_HOST", "GH_TOKEN"},
		"gitlab":         {"GITLAB_HOST", "GITLAB_TOKEN"},
		"aws":            {"AWS_ACCESS_KEY_ID", "AWS_DEFAULT_REGION", "AWS_REGION", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"},
		"bedrock":        {"AWS_ACCESS_KEY_ID", "AWS_REGION", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "CLAUDE_CODE_USE_BEDROCK"},
		kindAnthropic:    {"ANTHROPIC_API_KEY"},
		kindSubscription: {"CLAUDE_CODE_OAUTH_TOKEN"},
		"http-api-key":   {"ACME_API_HEADER", "ACME_API_KEY", "ACME_API_URL"},
		kindMCPServer:    nil,
	}

	for _, k := range kinds {
		got, err := k.Render(renderValues[k.Name], minted)
		if err != nil {
			t.Errorf("%s: %v", k.Name, err)

			continue
		}

		want, ok := wantEnv[k.Name]
		if env := slices.Sorted(maps.Keys(got.Env)); ok && !slices.Equal(env, want) {
			t.Errorf("%s: env %v; want %v", k.Name, env, want)
		}
	}
}

// github's token reaches env, the git login and the MCP header; mcp-server with a command is a
// stdio server only.
func TestPlatform_RenderModes(t *testing.T) {
	t.Parallel()

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	byName := func(name string) domain.ConnectionKind {
		return kinds[slices.IndexFunc(kinds, func(k domain.ConnectionKind) bool { return k.Name == name })]
	}

	gh, err := byName(kindGitHub).Render(renderValues[kindGitHub], nil)
	if err != nil {
		t.Fatal(err)
	}

	wantGit := &domain.KindGit{Host: "github.com", Username: "x-access-token", PasswordEnv: "GH_TOKEN"}
	if !reflect.DeepEqual(gh.Git, wantGit) || gh.Env["GH_TOKEN"] != "v1" || gh.MCP == nil || gh.MCP.Headers["Authorization"] != "Bearer ${GH_TOKEN}" {
		t.Errorf("github renders %+v (git %+v, mcp %+v); want its token in env, git's password env and the MCP header's ${GH_TOKEN}", gh, gh.Git, gh.MCP)
	}

	pw, err := byName(kindMCPServer).Render(renderValues[kindMCPServer], nil)
	if want := (&domain.KindMCP{Command: []string{"sh", "-c", playwright}}); err != nil || !reflect.DeepEqual(pw.MCP, want) {
		t.Errorf("mcp-server with a command renders %+v, %v; want a stdio server only", pw.MCP, err)
	}
}

// github's test signs in to github.com's API by default, and to a GitHub Enterprise Server's own
// /api/v3 (or an Enterprise Cloud's api.<host>) when the connection names it.
func TestPlatform_GitHubTestReachesTheConnectionsAPI(t *testing.T) {
	t.Parallel()

	kinds, err := connkind.Platform()
	if err != nil {
		t.Fatal(err)
	}

	gh := kinds[slices.IndexFunc(kinds, func(k domain.ConnectionKind) bool { return k.Name == kindGitHub })]

	tests := []struct{ host, api, want string }{
		{want: "https://api.github.com/user"},
		{host: "ghe.example.com", api: "https://ghe.example.com/api/v3", want: "https://ghe.example.com/api/v3/user"},
		{host: "acme.ghe.com", api: "https://api.acme.ghe.com", want: "https://api.acme.ghe.com/user"},
	}

	for _, tt := range tests {
		values := maps.Clone(renderValues[kindGitHub])
		if tt.host != "" {
			values["host"], values["api_url"] = tt.host, tt.api
		}

		test, err := gh.RenderTest(values)
		if err != nil || test == nil || test.URL != tt.want {
			t.Errorf("test on %q = %+v, %v; want %s", tt.host, test, err, tt.want)
		}
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	const valid = `apiVersion: goal/v1
kind: ConnectionKind
metadata: {name: acme-api, title: Acme API}
spec:
  fields:
    - {name: key, title: Key, secret: true, required: true}
  env: {ACME_KEY: "{{key}}"}
  test: {method: GET, url: https://api.example.com/me, headers: {X-Key: "{{key}}"}}
`

	k, err := connkind.Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}

	if k.Name != "acme-api" || k.Platform || k.Spec.Test.Expect != 200 || k.Spec.Env["ACME_KEY"] != "{{key}}" {
		t.Errorf("Parse = %+v; want acme-api, an org kind, a test expecting 200", k)
	}

	json := `{"apiVersion":"goal/v1","kind":"ConnectionKind","metadata":{"name":"acme-docs","title":"Docs"},` +
		`"spec":{"fields":[],"mcp":{"url":"https://docs.example.com/mcp"}}}`
	if _, err := connkind.Parse([]byte(json)); err != nil {
		t.Errorf("Parse of JSON = %v", err)
	}

	tests := []struct {
		name, file, want string
	}{
		{name: "not yaml", file: "a: [", want: "connection kind"},
		{name: "wrong kind", file: strings.Replace(valid, "kind: ConnectionKind", "kind: Factory", 1), want: "/kind"},
		{name: "unknown key", file: strings.Replace(valid, "  env:", "  secrets: {}\n  env:", 1), want: "secrets"},
		{name: "no title", file: strings.Replace(valid, ", title: Acme API", "", 1), want: "title"},
		{name: "bad name", file: strings.Replace(valid, "acme-api", "Acme", 1), want: "/metadata/name"},
		{name: "number as env value", file: strings.Replace(valid, `ACME_KEY: "{{key}}"`, `ACME_KEY: 1`, 1), want: "/spec/env/ACME_KEY"},
		{name: "unknown hook", file: strings.Replace(valid, "  env:", "  codeHost: bitbucket\n  env:", 1), want: "codeHost"},
		{name: "unknown field", file: strings.Replace(valid, `"{{key}}"}`+"\n  test", `"{{token}}"}`+"\n  test", 1), want: "{{token}} names no field"},
		{name: "secret in the test url", file: strings.Replace(valid, "https://api.example.com/me", `"https://api.example.com/me?k={{key}}"`, 1), want: "secret field"},
		{name: "too large", file: valid + "# " + strings.Repeat("x", connkind.MaxBytes), want: "at most"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := connkind.Parse([]byte(tt.file))
			if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Parse = %v; want ErrInvalid mentioning %q", err, tt.want)
			}
		})
	}
}
