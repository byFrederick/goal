package config

import (
	"slices"
	"strconv"
	"strings"
)

// SecretPurpose says what a secret reference signs in to.
type SecretPurpose string

// Secret purposes.
const (
	PurposeRepository SecretPurpose = "repository" // clones and merges runtime.repository
	PurposeConnection SecretPurpose = "connection" // a connection's headers, env, command or credential
)

// CredentialPrefix starts a reference to an organization credential (phase 3).
const CredentialPrefix = "credential:" //nolint:gosec // a reference prefix, not a credential

// refPrefixes start a secret reference: env:NAME today, a vault credential from phase 3, and
// the references of other secrets providers. Anything else is a literal value.
var refPrefixes = []string{"env:", CredentialPrefix, "op://", "arn:"}

// SecretUse is one place a definition needs a secret.
type SecretUse struct {
	// Path names the place for people: spec.connections[1].headers.Authorization.
	Path string
	// Doc is the place as an RFC 6901 pointer into factory.yaml, for Factory.Locate.
	Doc        string
	Purpose    SecretPurpose
	Connection string // the connection's name, for PurposeConnection
	// Ref is the reference as written: "env:GOAL_GITHUB_TOKEN", "credential:acme-github",
	// "op://…", "arn:…". It is empty for a literal value, which is never copied here.
	Ref string
}

// Literal reports whether the place holds a value instead of a reference.
func (u SecretUse) Literal() bool { return u.Ref == "" }

// SecretUses lists every secret reference in the definition, and every literal where a
// secret belongs, in file order: the repository token; each connection's header and env
// values (each reference word of a value such as "Bearer env:TOKEN", or the value as a
// literal when it has none), its command words that are references, and the connector form's
// credential.
func (f *Factory) SecretUses() []SecretUse {
	var uses []SecretUse

	if r := f.Spec.Runtime.Repository; r != nil && r.Token != "" {
		uses = append(uses, use(at("spec", "runtime", "repository", "token"), PurposeRepository, "", r.Token)...)
	}

	for i, c := range f.Spec.Connections {
		base := at("spec", "connections", i)

		for _, h := range sortedKeys(c.Headers) {
			uses = append(uses, use(append(base, "headers", h), PurposeConnection, c.Name, c.Headers[h])...)
		}

		for _, e := range sortedKeys(c.Env) {
			uses = append(uses, use(append(base, "env", e), PurposeConnection, c.Name, c.Env[e])...)
		}

		for j, w := range c.Command {
			if isRef(w) {
				uses = append(uses, SecretUse{
					Path: displayPath(append(base, "command", strconv.Itoa(j))), Doc: joinPointer(append(base, "command", strconv.Itoa(j))),
					Purpose: PurposeConnection, Connection: c.Name, Ref: w,
				})
			}
		}

		if c.Credential != "" {
			p := slices.Concat(base, []string{"credential"})
			uses = append(uses, SecretUse{
				Path: displayPath(p), Doc: joinPointer(p), Purpose: PurposeConnection, Connection: c.Name,
				Ref: CredentialPrefix + c.Credential,
			})
		}
	}

	return uses
}

// use lists the references in one value, or the value's place as a literal when it has none.
func use(path []string, purpose SecretPurpose, conn, value string) []SecretUse {
	u := SecretUse{Path: displayPath(path), Doc: joinPointer(path), Purpose: purpose, Connection: conn}

	var out []SecretUse

	for w := range strings.SplitSeq(value, " ") {
		if isRef(w) {
			r := u
			r.Ref = w
			out = append(out, r)
		}
	}

	if len(out) == 0 {
		out = append(out, u)
	}

	return out
}

func isRef(word string) bool {
	for _, p := range refPrefixes {
		if strings.HasPrefix(word, p) {
			return true
		}
	}

	return false
}
