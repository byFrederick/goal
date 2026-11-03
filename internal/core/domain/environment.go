package domain

import (
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// DefaultEnvironment is the environment every factory on secrets has, which an agent that names
// none gets (ADR 0037 decision 5).
const DefaultEnvironment = "default"

// Environment is one environment file (environments/<name>.yaml) as the engine resolves it at a
// run's start (ADR 0037 decision 10). Values of Env, Files and the MCP servers' Headers and Env
// may hold {{secret}} references.
type Environment struct {
	Name string
	// Size is the box's size; empty is medium. Image is the box's image; empty is Goal's.
	Size  SandboxSize
	Image string
	// Tools are mise tool specs and Setup the commands prepare runs after installing them.
	Tools []string
	Setup []string
	// Env are the agent's variables and Files the files the worker writes, by path.
	Env   map[string]string
	Files map[string]string
	// MCP are the MCP servers the agent gets, by name.
	MCP map[string]MCPServer
}

// MCPServer is an MCP server an environment gives: remote (URL and Headers) or stdio (Command and
// Env). Tools maps a tool name or glob to its mode, allow, ask or off.
type MCPServer struct {
	URL     string
	Headers map[string]string
	Command []string
	Env     map[string]string
	Tools   map[string]string
}

// The prefixes of the platform-reserved variables a run's files and its MCP servers' secrets go in
// (ReservedRunEnv reserves GOAL_*, so an environment's env cannot set them).
const (
	envFilePrefix   = "GOAL_FILE_"
	envSecretPrefix = "GOAL_SECRET_"
)

// FileEnv maps each of files' paths to the variable its content goes in: GOAL_FILE_1, GOAL_FILE_2…
// numbered by sorted path. The engine fills the variables and the worker writes each file from
// its variable, so both number them here.
func FileEnv(files map[string]string) map[string]string {
	out := make(map[string]string, len(files))
	for i, p := range slices.Sorted(maps.Keys(files)) {
		out[p] = envFilePrefix + strconv.Itoa(i+1)
	}

	return out
}

// SecretEnv is the variable secret name's value goes in for an MCP server's header or stdio env:
// GOAL_SECRET_ and the name upper-cased, each character other than a letter or digit turned into _.
func SecretEnv(name string) string {
	return envSecretPrefix + strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, name)
}

// RenderSecrets is value with each {{secret}} reference replaced by ${GOAL_SECRET_<NAME>}
// (SecretEnv), so a config holding it names the variable and never the value.
func RenderSecrets(value string) string {
	return secretReference.ReplaceAllStringFunc(value, func(ref string) string {
		return "${" + SecretEnv(secretReference.FindStringSubmatch(ref)[1]) + "}"
	})
}

// SecretNames are every secret the server's headers and env refer to, in sorted key order, each
// once.
func (s MCPServer) SecretNames() []string {
	var out []string

	for _, m := range []map[string]string{s.Headers, s.Env} {
		for _, k := range slices.Sorted(maps.Keys(m)) {
			for _, n := range SecretNames(m[k]) {
				if !slices.Contains(out, n) {
					out = append(out, n)
				}
			}
		}
	}

	return out
}

// Rendered is the server with each {{secret}} of its headers and env rendered as RenderSecrets
// does.
func (s MCPServer) Rendered() MCPServer {
	render := func(m map[string]string) map[string]string {
		if m == nil {
			return nil
		}

		out := make(map[string]string, len(m))
		for k, v := range m {
			out[k] = RenderSecrets(v)
		}

		return out
	}

	s.Headers, s.Env = render(s.Headers), render(s.Env)
	s.Command, s.Tools = slices.Clone(s.Command), maps.Clone(s.Tools)

	return s
}

// secretReference is one {{secret}} in a value, spaces inside the braces allowed; its group is the
// secret's name. internal/config checks the names when a version is published.
var secretReference = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// SecretNames are the secrets value refers to, in order, each once.
func SecretNames(value string) []string {
	var out []string

	for _, m := range secretReference.FindAllStringSubmatch(value, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}

	return out
}

// FillSecrets is value with each {{secret}} reference replaced by the secret's value in values,
// whether the reference is the whole value or part of it.
func FillSecrets(value string, values map[string]string) string {
	return secretReference.ReplaceAllStringFunc(value, func(ref string) string {
		return values[secretReference.FindStringSubmatch(ref)[1]]
	})
}

// The variables a run's model key goes in (ADR 0037 decision 7).
const (
	EnvClaudeOAuthToken = "CLAUDE_CODE_OAUTH_TOKEN" //nolint:gosec // a variable's name, not a credential
	EnvAnthropicAPIKey  = "ANTHROPIC_API_KEY"       //nolint:gosec // a variable's name, not a credential
)

// ModelKeyEnv is the variable a model key's value goes in: CLAUDE_CODE_OAUTH_TOKEN for a Claude
// Code OAuth token (sk-ant-oat…), ANTHROPIC_API_KEY for any other key.
func ModelKeyEnv(value string) string {
	if strings.HasPrefix(value, "sk-ant-oat") {
		return EnvClaudeOAuthToken
	}

	return EnvAnthropicAPIKey
}

// OnSecrets reports whether the line's runs get their keys from secrets (ADR 0037): its version
// has environment files or a runtime.modelKey. A version published before runs on connections and
// credentials until Goal converts it.
func (l Line) OnSecrets() bool { return l.Runtime.ModelKey != "" || len(l.Environments) > 0 }

// Environment is the environment an agent that names name gets: name's file, or DefaultEnvironment's
// for "". False when the version has no file of that name.
func (l Line) Environment(name string) (Environment, bool) {
	if name == "" {
		name = DefaultEnvironment
	}

	i := slices.IndexFunc(l.Environments, func(e Environment) bool { return e.Name == name })
	if i < 0 {
		return Environment{}, false
	}

	return l.Environments[i], true
}
