package domain

import (
	"regexp"
	"slices"
	"strings"
)

// DefaultEnvironment is the environment every factory on secrets has, which an agent that names
// none gets (ADR 0037 decision 5).
const DefaultEnvironment = "default"

// Environment is the part of one environment file (environments/<name>.yaml) the engine gives a
// run at its start (ADR 0037 decision 10): its env, whose values may hold {{secret}} references.
type Environment struct {
	Name string
	Env  map[string]string
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

// RepositoryTokenEnv is the variable goal-agent prepare gets runtime.repository.token's value in
// (ADR 0037 decision 7). It is reserved (ReservedRunEnv), so no environment file sets it, and the
// agent's own env never has it.
const RepositoryTokenEnv = "GOAL_REPOSITORY_TOKEN"

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
