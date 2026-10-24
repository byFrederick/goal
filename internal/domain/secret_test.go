package domain

import "testing"

const repoToken = "env:ACME_REPO_TOKEN" //nolint:gosec // a reference, not a credential

// The policy decides, before a value is resolved, whether a connection may send a reference:
// reserved variables go nowhere, and the repository's token only to https on its MCP host, the
// host matched exactly (no look-alikes, no clear text, never a sidecar, which reaches any host).
func TestSecretPolicy_ForConnection(t *testing.T) {
	t.Parallel()

	p := SecretPolicy{
		Reserved:        []string{"ACME_DATABASE_URL"},
		RepositoryOnly:  []string{"ACME_REPO_TOKEN"},
		ConnectionHosts: []string{"code.example.com"},
	}

	tests := []struct {
		name   string
		ref    string
		target string
		want   SecretVerdict
	}{
		{name: "the token to its host", ref: repoToken, target: "https://code.example.com/mcp/", want: SecretAllowed},
		{name: "the token to its host without case", ref: repoToken, target: "https://Code.Example.com/mcp/", want: SecretAllowed},
		{name: "the token to its host on another port", ref: repoToken, target: "https://code.example.com:8443/mcp/", want: SecretAllowed},
		{name: "the token to another host", ref: repoToken, target: "https://attacker.example/mcp", want: SecretOffHost},
		{name: "the token to a look-alike host", ref: repoToken, target: "https://code.example.com.attacker.example/mcp", want: SecretOffHost},
		{name: "the token to a host ending in its host", ref: repoToken, target: "https://evilcode.example.com/mcp", want: SecretOffHost},
		{name: "the token behind userinfo naming its host", ref: repoToken, target: "https://code.example.com@attacker.example/mcp", want: SecretOffHost},
		{name: "the token to its host in clear text", ref: repoToken, target: "http://code.example.com/mcp", want: SecretOffHost},
		{name: "the token to loopback", ref: repoToken, target: "http://127.0.0.1:9999/mcp", want: SecretOffHost},
		{name: "the token to a sidecar", ref: repoToken, target: "", want: SecretOffHost},
		{name: "the token to a url that does not parse", ref: repoToken, target: "https://code.example.com/%zz", want: SecretOffHost},
		{name: "a reserved variable to the token's host", ref: "env:ACME_DATABASE_URL", target: "https://code.example.com/mcp/", want: SecretReserved},
		{name: "a reserved variable to a sidecar", ref: "env:ACME_DATABASE_URL", target: "", want: SecretReserved},
		{name: "another variable anywhere", ref: "env:ACME_TICKETS_TOKEN", target: "https://tickets.example.com/mcp", want: SecretAllowed},
		{name: "another variable to a sidecar", ref: "env:ACME_TICKETS_TOKEN", target: "", want: SecretAllowed},
		{name: "a reference of another provider", ref: "op://vault/acme/token", target: "https://tickets.example.com/mcp", want: SecretAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := p.ForConnection(tt.ref, tt.target); got != tt.want {
				t.Errorf("ForConnection(%q, %q) = %v, want %v", tt.ref, tt.target, got, tt.want)
			}
		})
	}
}

// A repository's token (runtime.repository.token) may be any variable but a reserved one: an old
// version naming goald's own gateway key must not have it resolved for a clone or a merge, even
// though both go only to the install's trusted GitHub host.
func TestSecretPolicy_ForRepository(t *testing.T) {
	t.Parallel()

	p := SecretPolicy{Reserved: []string{"GOAL_GATEWAY_KEY"}, RepositoryOnly: []string{"ACME_REPO_TOKEN"}}

	for ref, want := range map[string]SecretVerdict{
		repoToken:                SecretAllowed,
		"env:ACME_TICKETS_TOKEN": SecretAllowed,
		"env:GOAL_GATEWAY_KEY":   SecretReserved,
		"op://vault/acme/token":  SecretAllowed,
	} {
		if got := p.ForRepository(ref); got != want {
			t.Errorf("ForRepository(%q) = %v, want %v", ref, got, want)
		}
	}
}
