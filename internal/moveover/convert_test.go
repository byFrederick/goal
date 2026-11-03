package moveover_test

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/moveover"
)

// Test values; none is a real key.
const (
	githubValue  = "github-token-for-tests-1111"
	cfValue      = "cloudflare-token-for-tests-2222"
	ctx7Value    = "context7-key-for-tests-3333"
	oauthValue   = "sk-ant-oat01-token-for-tests-4444"
	ticketsID    = "tickets-id-for-tests-5555"
	ticketsValue = "tickets-secret-for-tests-6666"
	factoryUUID  = "0b6f1c9e-5d0a-4c39-9a51-6f1f3f0e2a77"
)

// Names the fixtures share.
const (
	connGitHub     = "github"
	connCloudflare = "cloudflare"
	connContext7   = "context7"
	fieldToken     = "token"
	accountID      = "acme-account"
	sizeLarge      = "large"
	context7URL    = "https://mcp.context7.com/mcp"
	envGHToken     = "GH_TOKEN"
	envContext7    = "CONTEXT7_API_KEY"
	refGitHub      = "{{github}}"
	refContext7    = "{{context7}}"
)

// Organization kinds of the fixtures: an API token CLI, a remote MCP server with a key, a kind
// with two secret fields and a stdio server with no fields.
const (
	cloudflareKind = `apiVersion: goal/v1
kind: ConnectionKind
metadata: {name: cloudflare, title: Cloudflare}
spec:
  fields:
    - {name: account_id, title: Account ID, required: true}
    - {name: api_token, title: API token, secret: true, required: true}
  env:
    CLOUDFLARE_ACCOUNT_ID: "{{account_id}}"
    CLOUDFLARE_API_TOKEN: "{{api_token}}"
  cli: {tool: "npm:wrangler@4"}
`
	context7Kind = `apiVersion: goal/v1
kind: ConnectionKind
metadata: {name: context7, title: Context7}
spec:
  fields:
    - {name: key, title: API key, secret: true, required: true}
  env:
    CONTEXT7_API_KEY: "{{key}}"
  mcp:
    url: https://mcp.context7.com/mcp
    headers: {CONTEXT7_API_KEY: "${CONTEXT7_API_KEY}"}
`
	ticketsKind = `apiVersion: goal/v1
kind: ConnectionKind
metadata: {name: tickets, title: Tickets}
spec:
  fields:
    - {name: key_id, title: Key ID, secret: true, required: true}
    - {name: key_secret, title: Key secret, secret: true, required: true}
  env:
    TICKETS_KEY_ID: "{{key_id}}"
    TICKETS_KEY_SECRET: "{{key_secret}}"
`
	stdioKind = `apiVersion: goal/v1
kind: ConnectionKind
metadata: {name: local-browser, title: Local browser}
spec:
  fields: []
  mcp:
    command: [npx, "@playwright/mcp@0.0.41"]
`
)

// productionFactory is a factory on connections as the goal organization's run today: github as
// the repository's and the factory's connection, cloudflare and context7, the claude
// subscription as the model connection, a size and tools, and one agent with fewer connections.
const productionFactory = `apiVersion: goal/v1
kind: Factory
metadata:
  id: ` + factoryUUID + `
  name: acme-software
spec:
  runtime:
    model: claude-sonnet-5-5
    modelConnection: claude
    size: large
    tools: ["go@1.26.10"]
    repository: { url: "https://github.com/acme/app.git", connection: github }
  connections: [github, cloudflare, context7]
  agents:
    - name: engineer
      prompt: ./agents/engineer.md
      connections: [github, cloudflare, context7]
    - name: reviewer
      prompt: ./agents/reviewer.md
      connections: [github]
  board:
    stations:
      - { id: inbox, kind: queue }
      - { id: build, kind: agent, agent: engineer }
      - { id: review, kind: agent, agent: reviewer }
      - { id: done, kind: done }
`

func files(factory string) map[string][]byte {
	return map[string][]byte{
		config.FileName:      []byte(factory),
		"agents/engineer.md": []byte("# engineer\n\nBuild the change.\n"),
		"agents/reviewer.md": []byte("# reviewer\n\nReview the change.\n"),
	}
}

func kind(t *testing.T, name, raw string) domain.ConnectionKind {
	t.Helper()

	if raw == "" {
		kinds, err := connkind.Platform()
		if err != nil {
			t.Fatal(err)
		}

		i := slices.IndexFunc(kinds, func(k domain.ConnectionKind) bool { return k.Name == name })
		if i < 0 {
			t.Fatalf("no platform kind %s", name)
		}

		return kinds[i]
	}

	k, err := connkind.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("kind %s: %v", name, err)
	}

	return k
}

// conn is an opened connection of kind k with its plain fields and sealed values.
func conn(name string, k domain.ConnectionKind, fields, values map[string]string) domain.ResolvedConnection {
	all := maps.Clone(values)
	if all == nil {
		all = map[string]string{}
	}

	maps.Copy(all, fields)

	return domain.ResolvedConnection{
		Connection: domain.Connection{ID: domain.ConnectionID("id-" + name), Name: name, Kind: k.Name, Fields: fields, ToolModes: map[string]domain.ConnectionToolMode{}},
		Kind:       k, Values: all,
	}
}

// productionConnections are the goal organization's four connections, context7's resolve tool
// set to ask.
func productionConnections(t *testing.T) []domain.ResolvedConnection {
	t.Helper()

	ctx7 := conn("context7", kind(t, "context7", context7Kind), nil, map[string]string{"key": ctx7Value})
	ctx7.Connection.ToolModes["resolve-library-id"] = domain.ToolModeAsk
	ctx7.Connection.ToolModes["query-docs"] = domain.ToolModeAllow

	return []domain.ResolvedConnection{
		conn("github", kind(t, "github", ""), nil, map[string]string{"token": githubValue}),
		conn("cloudflare", kind(t, "cloudflare", cloudflareKind), map[string]string{"account_id": "acme-account"}, map[string]string{"api_token": cfValue}),
		ctx7,
		conn("claude", kind(t, "claude-subscription", ""), nil, map[string]string{"token": oauthValue}),
	}
}

// environment parses a converted environment file.
func environment(t *testing.T, tree map[string][]byte, name string) config.EnvironmentSpec {
	t.Helper()

	raw, ok := tree["environments/"+name+".yaml"]
	if !ok {
		t.Fatalf("no environments/%s.yaml in %v", name, slices.Sorted(maps.Keys(tree)))
	}

	e, err := config.ParseEnvironment(name, raw)
	if err != nil {
		t.Fatalf("environments/%s.yaml: %v\n%s", name, err, raw)
	}

	return e.Spec
}

// factoryOf loads the converted tree as publish checks it, failing on any error problem.
func factoryOf(t *testing.T, tree map[string][]byte) *config.Factory {
	t.Helper()

	fs := fstest.MapFS{}
	for p, b := range tree {
		fs[p] = &fstest.MapFile{Data: b}
	}

	f, problems := config.Check(fs)
	for _, p := range problems {
		if p.Severity == domain.SeverityError {
			t.Errorf("converted tree: %s: %s (%s)", p.File, p.Message, p.Code)
		}
	}

	if f == nil {
		t.Fatalf("the converted tree does not load:\n%s", tree[config.FileName])
	}

	return f
}

// testProductionFactory converts the production factory: factory.yaml, the default
// environment, the reviewer's and the names.
func testProductionFactory(t *testing.T) {
	t.Helper()

	got := moveover.Convert(files(productionFactory), productionConnections(t))
	if len(got.Problems) > 0 {
		t.Fatalf("problems %v", got.Problems)
	}

	f := factoryOf(t, got.Files)
	checkRuntime(t, f)
	checkDefault(t, environment(t, got.Files, config.DefaultEnvironment))
	checkAgents(t, f, environment(t, got.Files, "reviewer"))
	checkNames(t, got.Names)

	for p, b := range got.Files {
		if slices.ContainsFunc([]string{githubValue, cfValue, ctx7Value, oauthValue}, func(v string) bool { return strings.Contains(string(b), v) }) {
			t.Errorf("%s holds a secret value", p)
		}
	}

	if string(got.Files["agents/engineer.md"]) != "# engineer\n\nBuild the change.\n" {
		t.Error("the prompt files changed")
	}
}

func checkRuntime(t *testing.T, f *config.Factory) {
	t.Helper()

	rt := f.Spec.Runtime
	if rt.ModelKey != "{{claude}}" || rt.ModelConnection != "" {
		t.Errorf("modelKey %q, modelConnection %q; want {{claude}} and none", rt.ModelKey, rt.ModelConnection)
	}

	if r := rt.Repository; r == nil || r.Token != refGitHub || r.Connection != "" || r.URL != "https://github.com/acme/app.git" {
		t.Errorf("repository %+v; want the URL with token {{github}}", r)
	}

	if rt.Size != "" || len(rt.Tools) > 0 || len(f.Spec.Connections) > 0 {
		t.Errorf("runtime size %q, tools %v, connections %v left in factory.yaml", rt.Size, rt.Tools, f.Spec.Connections)
	}
}

func checkDefault(t *testing.T, def config.EnvironmentSpec) {
	t.Helper()

	wantEnv := map[string]string{ //nolint:gosec // references to test secrets, not values
		envGHToken: refGitHub, "GH_HOST": "github.com",
		"CLOUDFLARE_ACCOUNT_ID": accountID, "CLOUDFLARE_API_TOKEN": "{{cloudflare}}",
		envContext7: refContext7,
	}
	if !maps.Equal(def.Env, wantEnv) {
		t.Errorf("default env %v; want %v", def.Env, wantEnv)
	}

	if def.Size != sizeLarge || !slices.Equal(def.Tools, []string{"go@1.26.10", "npm:wrangler@4"}) {
		t.Errorf("default size %q, tools %v; want large and the runtime's tools then wrangler", def.Size, def.Tools)
	}

	gh, c7 := def.MCP[connGitHub], def.MCP[connContext7]
	if gh.URL != "https://api.githubcopilot.com/mcp/" || gh.Headers["Authorization"] != "Bearer "+refGitHub {
		t.Errorf("github MCP %+v; want its URL and a Bearer {{github}} header", gh)
	}

	if c7.URL != context7URL || c7.Headers[envContext7] != refContext7 {
		t.Errorf("context7 MCP %+v; want its URL and the key header", c7)
	}

	if !maps.Equal(c7.Tools, map[string]string{"resolve-library-id": "ask"}) {
		t.Errorf("context7 tools %v; want the organization's ask mode only", c7.Tools)
	}
}

// checkAgents: the engineer has the factory's set; the reviewer's differs, so it gets its own file.
func checkAgents(t *testing.T, f *config.Factory, rev config.EnvironmentSpec) {
	t.Helper()

	for _, a := range f.Spec.Agents {
		want := map[string]string{"engineer": "", "reviewer": "reviewer"}[a.Name]
		if a.Environment != want || len(a.Connections) > 0 {
			t.Errorf("agent %s: environment %q, connections %v; want %q and none", a.Name, a.Environment, a.Connections, want)
		}
	}

	if !maps.Equal(rev.Env, map[string]string{envGHToken: refGitHub, "GH_HOST": "github.com"}) || len(rev.MCP) != 1 || rev.Size != sizeLarge {
		t.Errorf("reviewer environment %+v; want github's only, with the runtime's size", rev)
	}
}

func checkNames(t *testing.T, names map[string]map[string]string) {
	t.Helper()

	want := map[string]map[string]string{
		connGitHub: {fieldToken: connGitHub}, connCloudflare: {"api_token": connCloudflare}, connContext7: {"key": connContext7}, "claude": {fieldToken: "claude"},
	}
	if len(names) != len(want) {
		t.Errorf("names %v; want %v", names, want)
	}

	for c, m := range want {
		if !maps.Equal(names[c], m) {
			t.Errorf("names of %s %v; want %v", c, names[c], m)
		}
	}
}

// testSeveralSecretFields names a kind's several secret fields <connection>-<field> and uses a credential a field refers to.
func testSeveralSecretFields(t *testing.T) {
	t.Helper()

	tk := kind(t, "tickets", ticketsKind)
	tickets := conn("tickets", tk, nil, map[string]string{"key_id": ticketsID})
	tickets.Connection.SecretRefs = map[string]string{"key_secret": "tickets-shared"}

	got := moveover.Convert(files(strings.ReplaceAll(productionFactory, "[github, cloudflare, context7]", "[tickets]")),
		append(productionConnections(t), tickets))
	if len(got.Problems) > 0 {
		t.Fatalf("problems %v", got.Problems)
	}

	def := environment(t, got.Files, "default")
	if def.Env["TICKETS_KEY_ID"] != "{{tickets-key-id}}" || def.Env["TICKETS_KEY_SECRET"] != "{{tickets-shared}}" {
		t.Errorf("default env %v; want <connection>-<field> and the credential's name", def.Env)
	}

	secrets := moveover.Secrets([]domain.ResolvedConnection{tickets})
	if len(secrets) != 1 || secrets[0].Name != "tickets-key-id" || secrets[0].Value != ticketsID || !secrets[0].Scope.All {
		t.Errorf("secrets %v; want tickets-key-id only, for every factory: the reference is a secret already", secrets)
	}
}

// testTriggerModel turns a trigger's modelAgrees.connection into modelAgrees.key.
func testTriggerModel(t *testing.T) {
	t.Helper()

	withTrigger := strings.Replace(productionFactory, "  board:\n", `  board:
    triggers:
      - name: alerts
        on: webhook.alerts.fired
        only: {modelAgrees: {question: "Is it urgent?", connection: claude}}
        create: {type: incident, station: inbox, title: "{{title}}"}
`, 1)

	got := moveover.Convert(files(withTrigger), productionConnections(t))
	if len(got.Problems) > 0 {
		t.Fatalf("problems %v", got.Problems)
	}

	var doc struct {
		Spec struct {
			Board struct {
				Triggers []struct {
					Only struct {
						ModelAgrees map[string]string `yaml:"modelAgrees"`
					} `yaml:"only"`
				} `yaml:"triggers"`
			} `yaml:"board"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(got.Files[config.FileName], &doc); err != nil {
		t.Fatal(err)
	}

	if m := doc.Spec.Board.Triggers[0].Only.ModelAgrees; m["key"] != "{{claude}}" || m["connection"] != "" {
		t.Errorf("modelAgrees %v; want key {{claude}} and no connection", m)
	}
}

func TestConvert(t *testing.T) {
	t.Parallel()

	t.Run("production factory", func(t *testing.T) {
		t.Parallel()
		testProductionFactory(t)
	})

	t.Run("several secret fields and a credential reference", func(t *testing.T) {
		t.Parallel()
		testSeveralSecretFields(t)
	})

	t.Run("a trigger's model connection", func(t *testing.T) {
		t.Parallel()
		testTriggerModel(t)
	})

	refusals := []struct {
		name, factory string
		conn          func(t *testing.T) domain.ResolvedConnection
		want          string
	}{
		{
			name: "aws", factory: "[github, aws-prod]",
			conn: func(t *testing.T) domain.ResolvedConnection {
				t.Helper()
				return conn("aws-prod", kind(t, "aws", ""), nil, map[string]string{"access_key_id": "a", "secret_access_key": "b"})
			},
			want: "connection aws-prod uses the aws kind, which Goal no longer converts: remove it from the factory",
		},
		{
			name: "bedrock", factory: "[github, models]",
			conn: func(t *testing.T) domain.ResolvedConnection {
				t.Helper()
				return conn("models", kind(t, "bedrock", ""), nil, map[string]string{"access_key_id": "a", "secret_access_key": "b"})
			},
			want: "connection models uses the bedrock kind",
		},
		{
			name: "stdio custom kind without fields", factory: "[github, browser]",
			conn: func(t *testing.T) domain.ResolvedConnection {
				t.Helper()
				return conn("browser", kind(t, "local-browser", stdioKind), nil, nil)
			},
			want: "connection browser uses the custom kind local-browser, a stdio command with no fields",
		},
	}

	for _, tt := range refusals {
		t.Run("refuses "+tt.name, func(t *testing.T) {
			t.Parallel()

			conns := append(productionConnections(t), tt.conn(t))
			got := moveover.Convert(files(strings.Replace(productionFactory, "[github, cloudflare, context7]", tt.factory, 1)), conns)

			if got.Files != nil {
				t.Error("a refused factory got files")
			}

			if !slices.ContainsFunc(got.Problems, func(p string) bool { return strings.HasPrefix(p, tt.want) }) {
				t.Errorf("problems %q; want one starting %q", got.Problems, tt.want)
			}

			if s := moveover.Secrets(conns[len(conns)-1:]); len(s) != 0 {
				t.Errorf("secrets %v of a refused connection; want none", s)
			}
		})
	}

	t.Run("a version on secrets already", func(t *testing.T) {
		t.Parallel()

		tree := files(strings.Replace(productionFactory, "modelConnection: claude", `modelKey: "{{claude}}"`, 1))
		if got := moveover.Convert(tree, productionConnections(t)); !got.OnSecrets || got.Files != nil || len(got.Problems) > 0 {
			t.Errorf("Convert = %+v; want OnSecrets and nothing to do", got)
		}
	})
}
