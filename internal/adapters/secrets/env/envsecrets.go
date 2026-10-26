// Package env implements ports.Secrets on the process environment, which goald fills
// from the shell and a git-ignored .env file.
//
// A reference is "env:NAME" or a literal value. References meant for other providers (op://
// for 1Password, arn: for AWS Secrets Manager) are rejected rather than passed through as
// literals, so a definition written for another provider fails loudly instead of sending its
// reference string as a credential. Errors name the reference, never the value.
package env

import (
	"context"
	"fmt"
	"strings"

	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Secrets = (*Env)(nil)

// envPrefix marks a reference to an environment variable.
const envPrefix = "env:"

// foreignPrefixes start references that other Secrets adapters resolve.
var foreignPrefixes = []string{"op://", "arn:"}

// Env resolves references from an environment. It is safe for concurrent use when lookup is.
type Env struct {
	lookup func(string) (string, bool)
}

// New returns a resolver over lookup; goald passes os.LookupEnv.
func New(lookup func(string) (string, bool)) *Env {
	return &Env{lookup: lookup}
}

// Resolve returns the value of an env:NAME reference, or ref itself when it is a literal. An
// unset or empty variable is ErrSecretNotFound: .env.example lists every name with an empty
// value, and an empty credential is never what a definition means.
func (e *Env) Resolve(_ context.Context, ref string) (string, error) {
	name, isEnv := strings.CutPrefix(ref, envPrefix)
	if !isEnv {
		for _, p := range foreignPrefixes {
			if strings.HasPrefix(ref, p) {
				return "", fmt.Errorf("unsupported secret reference scheme %q: the env provider resolves only env:NAME", p)
			}
		}

		return ref, nil
	}

	if name == "" {
		return "", fmt.Errorf("secret reference %q names no variable", ref)
	}

	v, ok := e.lookup(name)
	if !ok || v == "" {
		return "", fmt.Errorf("resolve env:%s: %w", name, ports.ErrSecretNotFound)
	}

	return v, nil
}
