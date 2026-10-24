package domain

import (
	"errors"
	"net/url"
	"slices"
	"strings"
)

// SecretPolicy is the install's rule for the env:NAME references of factory definitions until
// phase 4's credentials (docs/specs/README.md, "Interim rule until phase 4"): which of goald's
// environment variables a definition may not reference at all, and which it may send to one
// host only. goald builds it once from its settings and hands the same value to authoring, which
// refuses a definition that breaks it, and to the MCP gateway, which checks it again before it
// resolves a reference for an upstream request or a sidecar, so a version published before a
// rule existed cannot send the secret elsewhere either. The zero value restricts nothing.
type SecretPolicy struct {
	// Reserved are the variables a definition may not reference: goald's own settings and the
	// platform's secrets (the model's access, keys that never reach a task).
	Reserved []string
	// RepositoryOnly are the variables holding the install's repository token: a definition may
	// use them for runtime.repository and, in a connection, only in the headers of an https
	// connection to one of ConnectionHosts (host names, matched without case, any port). A
	// sidecar never gets them: its network reaches any host.
	RepositoryOnly  []string
	ConnectionHosts []string
}

// SecretVerdict is what SecretPolicy.ForConnection decides about one reference.
type SecretVerdict int

// Secret verdicts.
const (
	// SecretAllowed: the connection may send the reference's value.
	SecretAllowed SecretVerdict = iota
	// SecretReserved: the reference names a Reserved variable.
	SecretReserved
	// SecretOffHost: the reference names a RepositoryOnly variable and its target is not an
	// https url on one of ConnectionHosts.
	SecretOffHost
)

// Reserves reports whether a definition may not reference the variable name.
func (p SecretPolicy) Reserves(name string) bool { return slices.Contains(p.Reserved, name) }

// ForConnection decides whether a connection may send ref's value to target: the url of the
// connection whose header holds ref, or "" for a sidecar's env value or command word. A
// reference other than env:NAME is allowed; the policy is about goald's environment.
func (p SecretPolicy) ForConnection(ref, target string) SecretVerdict {
	name, ok := strings.CutPrefix(ref, "env:")

	switch {
	case !ok:
		return SecretAllowed
	case p.Reserves(name):
		return SecretReserved
	case slices.Contains(p.RepositoryOnly, name) && !p.connectionHost(target):
		return SecretOffHost
	default:
		return SecretAllowed
	}
}

// ForRepository decides whether ref, a factory's runtime.repository.token, may be resolved for
// its repository (a clone, a pull request's merge): any reference but one naming a Reserved
// variable, which goes nowhere, not even to the install's trusted repository host. The
// workspaces and the code host check it before they resolve the token, so a version published
// before the variable was reserved cannot send it either.
func (p SecretPolicy) ForRepository(ref string) SecretVerdict {
	if name, ok := strings.CutPrefix(ref, "env:"); ok && p.Reserves(name) {
		return SecretReserved
	}

	return SecretAllowed
}

// ErrSecretReserved means a reference names a variable the install's SecretPolicy reserves.
var ErrSecretReserved = errors.New("the reference names a variable reserved for goald (env-reserved)")

// connectionHost reports whether target is an https url on one of ConnectionHosts.
func (p SecretPolicy) connectionHost(target string) bool {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" {
		return false
	}

	return slices.ContainsFunc(p.ConnectionHosts, func(h string) bool { return strings.EqualFold(h, u.Hostname()) })
}
