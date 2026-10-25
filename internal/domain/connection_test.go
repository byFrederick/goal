package domain

import (
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"
)

// Names the connection tests repeat.
const (
	fieldToken          = "token"
	fieldBaseURL        = "base_url"
	envGHToken          = "GH_TOKEN"
	envAWSSession       = "AWS_SESSION_TOKEN"
	headerAuthorization = "Authorization"
	fieldPrefix         = "prefix"
	fieldKey            = "key"
	tmplToken           = "{{token}}"
	docsURL             = "https://docs.example.com/mcp"
)

// githubKind is spec 4 §4.2's example kind.
func githubKind() ConnectionKind {
	return ConnectionKind{Name: "github", Title: "GitHub", Spec: KindSpec{
		Fields: []KindField{
			{Name: fieldToken, Title: "Token", Secret: true, Required: true},
			{Name: "host", Title: "Host", Default: "github.com"},
		},
		Env: map[string]string{envGHToken: tmplToken, "GH_HOST": "{{host}}"},
		Git: &KindGit{Host: "{{host}}", Username: "x-access-token", PasswordEnv: envGHToken},
		MCP: &KindMCP{URL: "https://api.githubcopilot.com/mcp/", Headers: map[string]string{headerAuthorization: "Bearer ${GH_TOKEN}"}},
		CLI: &KindCLI{Tool: "gh"}, CodeHost: "github",
		Test: &KindTest{Method: "GET", URL: "https://api.{{host}}/user", Headers: map[string]string{headerAuthorization: "Bearer {{token}}"}, Expect: 200},
		Docs: "Use gh and git; GH_TOKEN is set.",
	}}
}

// apiKeyKind is the generic kind whose env names come from a field.
func apiKeyKind() ConnectionKind {
	return ConnectionKind{Name: "http-api-key", Spec: KindSpec{
		Fields: []KindField{
			{Name: fieldPrefix, Required: true},
			{Name: fieldBaseURL, Required: true},
			{Name: fieldKey, Secret: true, Required: true},
			{Name: "header", Default: headerAuthorization},
		},
		Env: map[string]string{"{{prefix}}_URL": "{{base_url}}", "{{prefix}}_KEY": "{{key}}", "{{prefix}}_HEADER": "{{header}}"},
	}}
}

// mcpKind is the generic MCP server: a remote URL or a stdio command, both optional.
func mcpKind() ConnectionKind {
	return ConnectionKind{Name: "mcp-server", Spec: KindSpec{
		Fields: []KindField{{Name: "url"}, {Name: "command"}},
		MCP:    &KindMCP{URL: "{{url}}", Command: []string{"sh", "-c", "{{command}}"}},
	}}
}

func TestConnectionKind_Check(t *testing.T) {
	t.Parallel()

	with := func(change func(*ConnectionKind)) ConnectionKind {
		k := githubKind()
		change(&k)

		return k
	}

	tests := []struct {
		name string
		kind ConnectionKind
		want string // "" when valid
	}{
		{name: "the spec's example", kind: githubKind()},
		{name: "env names from a field", kind: apiKeyKind()},
		{name: "optional mcp url and command", kind: mcpKind()},
		{name: "minted values with mint", kind: ConnectionKind{Name: "aws", Spec: KindSpec{
			Fields: []KindField{{Name: fieldKey, Secret: true}}, Mint: "aws-sts",
			Env: map[string]string{envAWSSession: "{{minted.session_token}}"},
		}}},
		{name: "bad name", kind: with(func(k *ConnectionKind) { k.Name = "GitHub" }), want: "name"},
		{name: "unknown field", kind: with(func(k *ConnectionKind) { k.Spec.Env["GH_X"] = "{{nope}}" }), want: "{{nope}} names no field"},
		{name: "outside the grammar", kind: with(func(k *ConnectionKind) { k.Spec.Env["GH_X"] = "{{token | base64}}" }), want: "neither"},
		{name: "unclosed template", kind: with(func(k *ConnectionKind) { k.Spec.Env["GH_X"] = "{{token" }), want: "unclosed"},
		{name: "minted without mint", kind: with(func(k *ConnectionKind) { k.Spec.Env["GH_X"] = "{{minted.token}}" }), want: "mints nothing"},
		{name: "secret in a url", kind: with(func(k *ConnectionKind) { k.Spec.MCP.URL = "https://x.example.com/?k={{token}}" }), want: "secret field {{token}}"},
		{name: "secret in an mcp header", kind: with(func(k *ConnectionKind) { k.Spec.MCP.Headers["X-Token"] = tmplToken }), want: "secret field"},
		{name: "mcp header names no env", kind: with(func(k *ConnectionKind) { k.Spec.MCP.Headers["X-Token"] = "${GITHUB_TOKEN}" }), want: "${GITHUB_TOKEN} is not one of"},
		{name: "secret in a command", kind: with(func(k *ConnectionKind) { k.Spec.MCP.Command = []string{"tool", tmplToken} }), want: "secret field"},
		{name: "secret in an env name", kind: with(func(k *ConnectionKind) { k.Spec.Env[tmplToken] = "x" }), want: "secret field"},
		{name: "reserved env", kind: with(func(k *ConnectionKind) { k.Spec.Env["https_proxy"] = "{{host}}" }), want: "reserved"},
		{name: "goal env", kind: with(func(k *ConnectionKind) { k.Spec.Env["GOAL_GATEWAY_KEY"] = tmplToken }), want: "reserved"},
		{name: "not an env name", kind: with(func(k *ConnectionKind) { k.Spec.Env["GH-TOKEN"] = tmplToken }), want: "not a variable name"},
		{name: "secret default", kind: with(func(k *ConnectionKind) { k.Spec.Fields[0].Default = "x" }), want: "no default"},
		{name: "field twice", kind: with(func(k *ConnectionKind) { k.Spec.Fields = append(k.Spec.Fields, k.Spec.Fields[1]) }), want: "twice"},
		{name: "git password not in env", kind: with(func(k *ConnectionKind) { k.Spec.Git.PasswordEnv = "TOKEN" }), want: "passwordEnv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.kind.Check()

			switch {
			case tt.want == "" && err != nil:
				t.Errorf("Check = %v; want nil", err)
			case tt.want != "" && (!errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tt.want)):
				t.Errorf("Check = %v; want ErrInvalid mentioning %q", err, tt.want)
			}
		})
	}
}

func TestConnectionKind_CheckValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    ConnectionKind
		fields  map[string]string
		secrets []string
		want    string
	}{
		{name: "secret set, default host", kind: githubKind(), secrets: []string{fieldToken}},
		{name: "own host", kind: githubKind(), fields: map[string]string{"host": "git.example.com"}, secrets: []string{fieldToken}},
		{name: "required secret missing", kind: githubKind(), want: `"token" is required`},
		{name: "secret as a field", kind: githubKind(), fields: map[string]string{fieldToken: "x"}, secrets: []string{fieldToken}, want: "is secret"},
		{name: "unknown field", kind: githubKind(), fields: map[string]string{"org": "example"}, secrets: []string{fieldToken}, want: `no field "org"`},
		{name: "unknown secret", kind: githubKind(), secrets: []string{fieldToken, "host"}, want: `no secret field "host"`},
		{name: "env names from a prefix", kind: apiKeyKind(), fields: map[string]string{fieldPrefix: "ACME", fieldBaseURL: "https://api.example.com"}, secrets: []string{fieldKey}},
		{name: "prefix makes a bad name", kind: apiKeyKind(), fields: map[string]string{fieldPrefix: "acme api", fieldBaseURL: "u"}, secrets: []string{fieldKey}, want: "not a variable name"},
		{name: "prefix makes a reserved name", kind: apiKeyKind(), fields: map[string]string{fieldPrefix: "GOAL", fieldBaseURL: "u"}, secrets: []string{fieldKey}, want: "reserved"},
		{name: "nothing required", kind: mcpKind()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.kind.CheckValues(tt.fields, tt.secrets)

			switch {
			case tt.want == "" && err != nil:
				t.Errorf("CheckValues = %v; want nil", err)
			case tt.want != "" && (!errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tt.want)):
				t.Errorf("CheckValues = %v; want ErrInvalid mentioning %q", err, tt.want)
			}
		})
	}
}

func TestConnectionKind_Render(t *testing.T) {
	t.Parallel()

	const token = "ghp_secret-value"

	tests := []struct {
		name           string
		kind           ConnectionKind
		values, minted map[string]string
		want           RunConnection
	}{
		{
			name: "defaults fill empty fields", kind: githubKind(), values: map[string]string{fieldToken: token},
			want: RunConnection{
				Env: map[string]string{envGHToken: token, "GH_HOST": "github.com"},
				Git: &KindGit{Host: "github.com", Username: "x-access-token", PasswordEnv: envGHToken},
				MCP: &KindMCP{URL: "https://api.githubcopilot.com/mcp/", Headers: map[string]string{headerAuthorization: "Bearer ${GH_TOKEN}"}},
				CLI: "gh", Docs: "Use gh and git; GH_TOKEN is set.",
			},
		},
		{
			name: "env names from a field", kind: apiKeyKind(),
			values: map[string]string{fieldPrefix: "ACME", fieldBaseURL: "https://api.example.com", fieldKey: "k"},
			want: RunConnection{Env: map[string]string{
				"ACME_URL": "https://api.example.com", "ACME_KEY": "k", "ACME_HEADER": headerAuthorization,
			}},
		},
		{
			name: "a remote server only", kind: mcpKind(), values: map[string]string{"url": docsURL},
			want: RunConnection{Env: map[string]string{}, MCP: &KindMCP{URL: docsURL, Headers: map[string]string{}}},
		},
		{
			name: "a stdio server only", kind: mcpKind(), values: map[string]string{"command": "npx -y @playwright/mcp@0.0.41"},
			want: RunConnection{Env: map[string]string{}, MCP: &KindMCP{Command: []string{"sh", "-c", "npx -y @playwright/mcp@0.0.41"}}},
		},
		{name: "neither", kind: mcpKind(), want: RunConnection{Env: map[string]string{}}},
		{
			name: "a header naming an env variable left out", values: map[string]string{},
			kind: ConnectionKind{Name: "acme-docs", Spec: KindSpec{
				Fields: []KindField{{Name: fieldKey, Secret: true}},
				Env:    map[string]string{"ACME_KEY": "{{key}}"},
				MCP:    &KindMCP{URL: docsURL, Headers: map[string]string{"X-Key": "${ACME_KEY}", "X-Client": "goal"}},
			}},
			want: RunConnection{Env: map[string]string{}, MCP: &KindMCP{URL: docsURL, Headers: map[string]string{"X-Client": "goal"}}},
		},
		{
			name: "minted values", minted: map[string]string{"session_token": "st"},
			kind: ConnectionKind{Name: "aws", Spec: KindSpec{
				Fields: []KindField{{Name: "region", Default: "us-east-1"}}, Mint: "aws-sts",
				Env: map[string]string{envAWSSession: "{{minted.session_token}}", "AWS_REGION": "{{region}}"},
			}},
			want: RunConnection{Env: map[string]string{envAWSSession: "st", "AWS_REGION": "us-east-1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.kind.Render(tt.values, tt.minted)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Render = %+v (git %+v, mcp %+v); want %+v", got, got.Git, got.MCP, tt.want)
			}
		})
	}
}

// Render's refusals name fields and variables, never a value: they reach logs.
func TestConnectionKind_RenderErrorsHoldNoValue(t *testing.T) {
	t.Parallel()

	const secret = "s3cr3t-value-that-must-not-leak"

	aws := ConnectionKind{Name: "aws", Spec: KindSpec{
		Fields: []KindField{{Name: fieldKey, Secret: true}}, Mint: "aws-sts",
		Env: map[string]string{"AWS_KEY": "{{key}}", envAWSSession: "{{minted.session_token}}"},
	}}
	if _, err := aws.Render(map[string]string{fieldKey: secret}, nil); err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("Render without the minted value = %v; want an error without the secret", err)
	}

	bad := apiKeyKind()
	if _, err := bad.Render(map[string]string{fieldPrefix: "MISE", fieldBaseURL: "u", fieldKey: secret}, nil); err == nil ||
		strings.Contains(err.Error(), secret) || !errors.Is(err, ErrInvalid) {
		t.Errorf("Render of a reserved name = %v; want ErrInvalid without the secret", err)
	}

	url := mcpKind()

	url.Spec.MCP.URL = "{{url}}"
	if _, err := url.Render(map[string]string{"url": "file:///" + secret}, nil); err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("Render of a non-http url = %v; want an error without the value", err)
	}
}

func TestReservedRunEnv(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"GOAL_GATEWAY_KEY": true, "goal_x": true, "HOME": true, "Path": true, "http_proxy": true, "NO_PROXY": true,
		"NODE_USE_ENV_PROXY": true, "CLAUDE_CONFIG_DIR": true, "LD_PRELOAD": true, "MISE_DATA_DIR": true,
		envGHToken: false, "ANTHROPIC_API_KEY": false, "GOALS": false, "PATHS": false,
	} {
		if got := ReservedRunEnv(name); got != want {
			t.Errorf("ReservedRunEnv(%q) = %v; want %v", name, got, want)
		}
	}
}

// The associated data binds organization, connection and key id, each to its own position.
func TestConnectionAAD(t *testing.T) {
	t.Parallel()

	base := ConnectionAAD("org-a", "conn-1", "k1")
	if string(base) != "goal/connection/v1|org-a|conn-1|k1" {
		t.Errorf("ConnectionAAD = %q", base)
	}

	others := map[string][]byte{
		"org": ConnectionAAD("org-b", "conn-1", "k1"), "connection": ConnectionAAD("org-a", "conn-2", "k1"),
		fieldKey: ConnectionAAD("org-a", "conn-1", "k2"),
	}
	for what, aad := range maps.All(others) {
		if string(aad) == string(base) {
			t.Errorf("another %s gives the same associated data", what)
		}
	}
}
