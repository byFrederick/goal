package config

import (
	"strconv"
	"strings"
)

// SecretPurpose says what a secret reference signs in to.
type SecretPurpose string

// Secret purposes.
const (
	PurposeRepository SecretPurpose = "repository" // clones and merges runtime.repository
	PurposeConnection SecretPurpose = "connection" // an inline connection's headers, env or command
)

// refPrefixes start a secret reference: env:NAME, and the references of other secrets providers.
// Anything else is a literal value.
var refPrefixes = []string{"env:", "op://", "arn:"}

// SecretUse is one place a definition of a removed form needs a secret: the repository's token
// or an inline connection's value, which the engine and the MCP gateway resolve until the
// cutover to connections.
type SecretUse struct {
	// Path names the place for people: spec.connections[1].headers.Authorization.
	Path string
	// Doc is the place as an RFC 6901 pointer into factory.yaml, for Factory.Locate.
	Doc        string
	Purpose    SecretPurpose
	Connection string // the connection's name, for PurposeConnection
	// URL is where the secret is sent: the connection's url for a header value, the repository's
	// url for its token.
	URL string
	// Ref is the reference as written: "env:GOAL_GITHUB_TOKEN", "op://…", "arn:…". It is empty for a literal value, which is never copied here.
	Ref string
}

// Literal reports whether the place holds a value instead of a reference.
func (u SecretUse) Literal() bool { return u.Ref == "" }

// SecretUses lists every secret reference in the definition, and every literal where a
// secret belongs, in file order: the repository token; each connection's header and env
// values (each reference word of a value such as "Bearer env:TOKEN", or the value as a
// literal when it has none) and its command words that are references.
func (f *Factory) SecretUses() []SecretUse {
	var uses []SecretUse

	if r := f.Spec.Runtime.Repository; r != nil && r.Token != "" {
		for _, u := range use(at("spec", "runtime", "repository", "token"), PurposeRepository, "", r.Token) {
			u.URL = r.URL
			uses = append(uses, u)
		}
	}

	for i, c := range f.Spec.Connections {
		base := at("spec", "connections", i)

		for _, h := range sortedKeys(c.Headers) {
			for _, u := range use(append(base, "headers", h), PurposeConnection, c.Name, c.Headers[h]) {
				u.URL = c.URL
				uses = append(uses, u)
			}
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
