package domain

import (
	"errors"
	"maps"
	"net/http"
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
	hostGitHub          = "github.com"
	fieldHost           = "host"
	envGHHost           = "GH_HOST"
	kindAWS             = "aws"
	mintAWSSTS          = "aws-sts"
	apiExampleURL       = "https://api.example.com"
	fieldAPIURL         = "api_url"
	hostGHE             = "ghe.example.com"
	envAWSRegion        = "AWS_REGION"
)

// githubKind is spec 4 §4.2's example kind.
func githubKind() ConnectionKind {
	return ConnectionKind{Name: "github", Title: "GitHub", Spec: KindSpec{
		Fields: []KindField{
			{Name: fieldToken, Title: "Token", Secret: true, Required: true},
			{Name: fieldHost, Title: "Host", Default: hostGitHub},
		},
		Env: map[string]string{envGHToken: tmplToken, envGHHost: "{{host}}"},
		Git: &KindGit{Host: "{{host}}", Username: "x-access-token", PasswordEnv: envGHToken},
		MCP: &KindMCP{URL: "https://api.githubcopilot.com/mcp/", Headers: map[string]string{headerAuthorization: "Bearer ${GH_TOKEN}"}},
		CLI: &KindCLI{Tool: "gh"}, CodeHost: "github",
		Test: &KindTest{Method: "GET", URL: "https://api.{{host}}/user", Headers: map[string]string{headerAuthorization: "Bearer {{token}}"}, Expect: 200},
		Docs: "Use gh and git; GH_TOKEN is set.",
	}}
}

// githubAPIKind is the platform's github kind as built: its test signs in to the API at api_url,
// github.com's by default.
func githubAPIKind() ConnectionKind {
	k := githubKind()
	k.Spec.Fields = append(k.Spec.Fields, KindField{Name: fieldAPIURL, Default: "https://api.github.com"})
	k.Spec.Test.URL = "{{api_url}}/user"

	return k
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

	// minting is the example kind minting, with change placing a minted value somewhere.
	minting := func(change func(*ConnectionKind)) ConnectionKind {
		return with(func(k *ConnectionKind) {
			k.Spec.Mint = mintAWSSTS
			k.Spec.Test.Basic = &KindBasicAuth{Username: "x"}
			change(k)
		})
	}

	const minted, mintedOnlyInEnv = "{{minted.token}}", "may appear only in env values"

	tests := []struct {
		name string
		kind ConnectionKind
		want string // "" when valid
	}{
		{name: "the spec's example", kind: githubKind()},
		{name: "env names from a field", kind: apiKeyKind()},
		{name: "optional mcp url and command", kind: mcpKind()},
		{name: "minted values with mint", kind: ConnectionKind{Name: kindAWS, Spec: KindSpec{
			Fields: []KindField{{Name: fieldKey, Secret: true}}, Mint: mintAWSSTS,
			Env: map[string]string{envAWSSession: "{{minted.session_token}}"},
		}}},
		{name: "bad name", kind: with(func(k *ConnectionKind) { k.Name = "GitHub" }), want: "name"},
		{name: "unknown field", kind: with(func(k *ConnectionKind) { k.Spec.Env["GH_X"] = "{{nope}}" }), want: "{{nope}} names no field"},
		{name: "outside the grammar", kind: with(func(k *ConnectionKind) { k.Spec.Env["GH_X"] = "{{token | base64}}" }), want: "neither"},
		{name: "unclosed template", kind: with(func(k *ConnectionKind) { k.Spec.Env["GH_X"] = "{{token" }), want: "unclosed"},
		{name: "minted in an env name", kind: minting(func(k *ConnectionKind) { k.Spec.Env[minted] = "x" }), want: mintedOnlyInEnv},
		{name: "minted in an mcp url", kind: minting(func(k *ConnectionKind) { k.Spec.MCP.URL = "https://x.example.com/?k=" + minted }), want: mintedOnlyInEnv},
		{name: "minted in an mcp header", kind: minting(func(k *ConnectionKind) { k.Spec.MCP.Headers["X-Token"] = minted }), want: mintedOnlyInEnv},
		{name: "minted in an mcp command", kind: minting(func(k *ConnectionKind) { k.Spec.MCP.Command = []string{"tool", minted} }), want: mintedOnlyInEnv},
		{name: "minted in a git login", kind: minting(func(k *ConnectionKind) { k.Spec.Git.Username = minted }), want: mintedOnlyInEnv},
		{name: "minted in a test header", kind: minting(func(k *ConnectionKind) { k.Spec.Test.Headers[headerAuthorization] = minted }), want: mintedOnlyInEnv},
		{name: "minted in a test password", kind: minting(func(k *ConnectionKind) { k.Spec.Test.Basic.Password = minted }), want: mintedOnlyInEnv},
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
		// rule is the field rule the error names, when it breaks one: ErrFieldRequired or ErrFieldUnknown.
		rule error
	}{
		{name: "secret set, default host", kind: githubKind(), secrets: []string{fieldToken}},
		{name: "own host", kind: githubKind(), fields: map[string]string{fieldHost: "git.example.com"}, secrets: []string{fieldToken}},
		{name: "required secret missing", kind: githubKind(), want: `"token" is required`, rule: ErrFieldRequired},
		{name: "secret as a field", kind: githubKind(), fields: map[string]string{fieldToken: "x"}, secrets: []string{fieldToken}, want: "is secret", rule: ErrFieldUnknown},
		{name: "unknown field", kind: githubKind(), fields: map[string]string{"org": "example"}, secrets: []string{fieldToken}, want: `no field "org"`, rule: ErrFieldUnknown},
		{name: "unknown secret", kind: githubKind(), secrets: []string{fieldToken, fieldHost}, want: `no secret field "host"`, rule: ErrFieldUnknown},
		{name: "env names from a prefix", kind: apiKeyKind(), fields: map[string]string{fieldPrefix: "ACME", fieldBaseURL: apiExampleURL}, secrets: []string{fieldKey}},
		{name: "prefix makes a bad name", kind: apiKeyKind(), fields: map[string]string{fieldPrefix: "acme api", fieldBaseURL: "u"}, secrets: []string{fieldKey}, want: "not a variable name"},
		{name: "prefix makes a reserved name", kind: apiKeyKind(), fields: map[string]string{fieldPrefix: "GOAL", fieldBaseURL: "u"}, secrets: []string{fieldKey}, want: "reserved"},
		{name: "nothing required", kind: mcpKind()},
		{
			name: "an enterprise host tested on github.com", kind: githubAPIKind(), fields: map[string]string{fieldHost: hostGHE}, secrets: []string{fieldToken},
			want: "test.url (from api_url) is on api.github.com",
		},
		{
			name: "an enterprise host tested on its API", kind: githubAPIKind(), secrets: []string{fieldToken},
			fields: map[string]string{fieldHost: hostGHE, fieldAPIURL: "https://" + hostGHE + "/api/v3"},
		},
		{name: "github.com tested on its API", kind: githubAPIKind(), secrets: []string{fieldToken}},
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
			case tt.rule != nil && !errors.Is(err, tt.rule):
				t.Errorf("CheckValues = %v; want it to be %v", err, tt.rule)
			}
		})
	}
}

func TestConnectionKind_WithDefaults(t *testing.T) {
	t.Parallel()

	values := map[string]string{fieldToken: "t0ken", "unknown": "kept"}

	got := githubKind().WithDefaults(values)
	want := map[string]string{fieldToken: "t0ken", fieldHost: hostGitHub, "unknown": "kept"}

	if !maps.Equal(got, want) {
		t.Errorf("WithDefaults = %v, want %v", got, want)
	}

	if _, ok := values[fieldHost]; ok {
		t.Error("WithDefaults changed its argument")
	}

	if got := githubKind().WithDefaults(map[string]string{fieldHost: "ghe.example.com"}); got[fieldHost] != "ghe.example.com" {
		t.Errorf("host = %q, want the value set over the default", got[fieldHost])
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
				Env: map[string]string{envGHToken: token, envGHHost: hostGitHub},
				Git: &KindGit{Host: hostGitHub, Username: "x-access-token", PasswordEnv: envGHToken},
				MCP: &KindMCP{URL: "https://api.githubcopilot.com/mcp/", Headers: map[string]string{headerAuthorization: "Bearer ${GH_TOKEN}"}},
				CLI: "gh", Docs: "Use gh and git; GH_TOKEN is set.",
			},
		},
		{
			name: "env names from a field", kind: apiKeyKind(),
			values: map[string]string{fieldPrefix: "ACME", fieldBaseURL: apiExampleURL, fieldKey: "k"},
			want: RunConnection{Env: map[string]string{
				"ACME_URL": apiExampleURL, "ACME_KEY": "k", "ACME_HEADER": headerAuthorization,
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
			kind: ConnectionKind{Name: kindAWS, Spec: KindSpec{
				Fields: []KindField{{Name: "region", Default: "us-east-1"}}, Mint: mintAWSSTS,
				Env: map[string]string{envAWSSession: "{{minted.session_token}}", envAWSRegion: "{{region}}"},
			}},
			want: RunConnection{Env: map[string]string{envAWSSession: "st", envAWSRegion: "us-east-1"}},
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

func TestResolvedConnection_GitLogin(t *testing.T) {
	t.Parallel()

	const token = "ghp_secret-value"

	resolved := func(k ConnectionKind, values map[string]string) ResolvedConnection {
		return ResolvedConnection{Connection: Connection{Name: "code"}, Kind: k, Values: values}
	}

	host, password, err := resolved(githubKind(), map[string]string{fieldToken: token, fieldHost: "GHE.Example.com"}).GitLogin()
	if err != nil || host != "ghe.example.com" || password != token {
		t.Errorf("GitLogin = %q, %q, %v; want ghe.example.com and the token", host, password, err)
	}

	for name, rc := range map[string]ResolvedConnection{
		"no git login": resolved(apiKeyKind(), map[string]string{
			fieldPrefix: "DOCS", fieldBaseURL: apiExampleURL, fieldKey: token,
		}),
		"no password value": resolved(githubKind(), map[string]string{}),
	} {
		if _, _, err := rc.GitLogin(); !errors.Is(err, ErrInvalid) || strings.Contains(err.Error(), token) {
			t.Errorf("%s: GitLogin = %v; want ErrInvalid without a value", name, err)
		}
	}
}

// Render's refusals name fields and variables, never a value: they reach logs.
func TestConnectionKind_RenderErrorsHoldNoValue(t *testing.T) {
	t.Parallel()

	const secret = "s3cr3t-value-that-must-not-leak"

	aws := ConnectionKind{Name: kindAWS, Spec: KindSpec{
		Fields: []KindField{{Name: fieldKey, Secret: true}}, Mint: mintAWSSTS,
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

// Outline says what a connection gives a run from what anyone may see of it, its non-secret fields
// and which secret fields are set: the names of its variables, its git login, MCP server and CLI
// tool, none holding a secret value.
func TestConnectionKind_Outline(t *testing.T) {
	t.Parallel()

	aws := ConnectionKind{Name: kindAWS, Spec: KindSpec{
		Fields: []KindField{{Name: fieldKey, Secret: true, Required: true}, {Name: "region", Default: "us-east-1"}},
		Mint:   mintAWSSTS,
		Env:    map[string]string{"AWS_ACCESS_KEY_ID": "{{minted.access_key_id}}", envAWSRegion: "{{region}}"},
	}}
	copilot := &KindMCP{URL: "https://api.githubcopilot.com/mcp/", Headers: map[string]string{headerAuthorization: "Bearer ${GH_TOKEN}"}}
	login := func(host string) *KindGit {
		return &KindGit{Host: host, Username: "x-access-token", PasswordEnv: envGHToken}
	}

	tests := []struct {
		name string
		kind ConnectionKind
		conn Connection
		want ConnectionOutline
	}{
		{
			name: "the default host", kind: githubKind(), conn: Connection{SecretFields: []string{fieldToken}},
			want: ConnectionOutline{Env: []string{envGHHost, envGHToken}, Git: login(hostGitHub), MCP: copilot, CLI: "gh"},
		},
		{
			name: "a host of its own", kind: githubKind(),
			conn: Connection{Fields: map[string]string{fieldHost: "git.example.com"}, SecretFields: []string{fieldToken}},
			want: ConnectionOutline{Env: []string{envGHHost, envGHToken}, Git: login("git.example.com"), MCP: copilot, CLI: "gh"},
		},
		{
			name: "no token, no git login and no header", kind: githubKind(),
			want: ConnectionOutline{
				Env: []string{envGHHost}, MCP: &KindMCP{URL: copilot.URL, Headers: map[string]string{}}, CLI: "gh",
			},
		},
		{
			name: "env names from a field", kind: apiKeyKind(),
			conn: Connection{Fields: map[string]string{fieldPrefix: "ACME", fieldBaseURL: apiExampleURL}, SecretFields: []string{fieldKey}},
			want: ConnectionOutline{Env: []string{"ACME_HEADER", "ACME_KEY", "ACME_URL"}},
		},
		{
			name: "a stdio server", kind: mcpKind(), conn: Connection{Fields: map[string]string{"command": "npx -y @acme/mcp@1.0.0"}},
			want: ConnectionOutline{MCP: &KindMCP{Command: []string{"sh", "-c", "npx -y @acme/mcp@1.0.0"}}},
		},
		{
			name: "minted values", kind: aws, conn: Connection{SecretFields: []string{fieldKey}},
			want: ConnectionOutline{Env: []string{"AWS_ACCESS_KEY_ID", envAWSRegion}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.kind.Outline(tt.conn); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Outline =\n%#v\nwant\n%#v", got, tt.want)
			}
		})
	}
}

func TestConnectionKind_RenderTest(t *testing.T) {
	t.Parallel()

	const token = "ghp_secret-value"

	jira := ConnectionKind{Name: "jira", Spec: KindSpec{
		Fields: []KindField{{Name: "site", Required: true}, {Name: "email", Required: true}, {Name: fieldToken, Secret: true, Required: true}},
		Test:   &KindTest{Method: http.MethodGet, URL: "https://{{site}}/rest/api/3/myself", Basic: &KindBasicAuth{Username: "{{email}}", Password: tmplToken}, Expect: 200},
	}}

	tests := []struct {
		name   string
		kind   ConnectionKind
		values map[string]string
		want   *KindTest
		err    string // "" when it renders
	}{
		{
			name: "defaults and secrets in headers", kind: githubKind(), values: map[string]string{fieldToken: token},
			want: &KindTest{Method: http.MethodGet, URL: "https://api.github.com/user", Headers: map[string]string{headerAuthorization: "Bearer " + token}, Expect: 200},
		},
		{
			name: "basic authentication", kind: jira, values: map[string]string{"site": "acme.atlassian.net", "email": "ops@example.com", fieldToken: token},
			want: &KindTest{
				Method: http.MethodGet, URL: "https://acme.atlassian.net/rest/api/3/myself", Headers: map[string]string{},
				Basic: &KindBasicAuth{Username: "ops@example.com", Password: token}, Expect: 200,
			},
		},
		{name: "no test", kind: mcpKind()},
		{name: "a missing value", kind: githubKind(), values: map[string]string{}, err: "has no value"},
		{
			name: "a test off the host of the login", kind: githubAPIKind(), values: map[string]string{fieldToken: token, fieldHost: hostGHE},
			err: "not on " + hostGHE + " or api." + hostGHE,
		},
		{name: "a url that is not http", kind: jira, values: map[string]string{"site": "/x", "email": "e", fieldToken: token}, err: "not an http or https URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.kind.RenderTest(tt.values)

			switch {
			case tt.err != "":
				if err == nil || !strings.Contains(err.Error(), tt.err) || strings.Contains(err.Error(), token) {
					t.Errorf("RenderTest = %+v, %v; want an error mentioning %q without the secret", got, err, tt.err)
				}
			case err != nil:
				t.Fatal(err)
			case !reflect.DeepEqual(got, tt.want):
				t.Errorf("RenderTest = %+v; want %+v", got, tt.want)
			}
		})
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
