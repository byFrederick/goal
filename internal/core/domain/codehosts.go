package domain

import (
	"cmp"
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// Errors of the organization's code hosts (ADR 0041, decision 1). Each wraps ErrInvalid; the API
// answers both with 400, ErrCodeHostInvalid as code-host-invalid and ErrCodeHostBuiltIn as
// code-host-built-in.
var (
	// ErrCodeHostInvalid means a host that is no public DNS name, an unknown kind, a host listed
	// twice or more than MaxCodeHosts hosts.
	ErrCodeHostInvalid = fmt.Errorf("not a code host: %w", ErrInvalid)
	// ErrCodeHostBuiltIn means github.com or gitlab.com, which are always listed and never added.
	ErrCodeHostBuiltIn = fmt.Errorf("the host is built in: %w", ErrInvalid)
)

// AuditOrgCodeHostsSet is the audit action of a change to the organization's added code hosts,
// targeting the organization (AuditTargetOrg). Its data holds the hosts after the change and
// before it (from), each {host, kind}.
const AuditOrgCodeHostsSet = "org.code_hosts_set"

// MaxCodeHosts is how many hosts an organization may add, besides the built-in ones.
const MaxCodeHosts = 20

// CodeHostKind is the API a code host speaks.
type CodeHostKind string

// The kinds of code host.
const (
	CodeHostGitHub CodeHostKind = "github"
	CodeHostGitLab CodeHostKind = "gitlab"
)

// CodeHost is a host the organization's repository tokens and knowledge secrets may be sent to:
// a built-in one (github.com, gitlab.com) or a GitHub Enterprise or self-managed GitLab host an
// owner added. Host is a DNS name in lower case, with no scheme, port or path.
type CodeHost struct {
	Host    string
	Kind    CodeHostKind
	BuiltIn bool
}

// BuiltInCodeHosts are the hosts every organization lists, github.com first; a new slice each call.
func BuiltInCodeHosts() []CodeHost {
	return []CodeHost{{Host: "github.com", Kind: CodeHostGitHub, BuiltIn: true}, {Host: "gitlab.com", Kind: CodeHostGitLab, BuiltIn: true}}
}

// CheckCodeHost returns host as an added code host of kind is stored, trimmed and lower case.
// goald dials public addresses only (ADR 0009), so a host must be a DNS name of two labels or
// more that is not an IP literal, localhost or a name under .local, .internal or .localhost;
// anything else, a scheme, port or path included, wraps ErrCodeHostInvalid, and a built-in host
// wraps ErrCodeHostBuiltIn. The name is not resolved: goald's dialer refuses a private address.
func CheckCodeHost(host string, kind CodeHostKind) (CodeHost, error) {
	h := strings.ToLower(strings.TrimSpace(host))

	if slices.ContainsFunc(BuiltInCodeHosts(), func(b CodeHost) bool { return b.Host == h }) {
		return CodeHost{}, fmt.Errorf("host %q: %w", h, ErrCodeHostBuiltIn)
	}

	if _, err := netip.ParseAddr(strings.Trim(h, "[]")); err == nil {
		return CodeHost{}, fmt.Errorf("host %q is an IP address, not a name: %w", h, ErrCodeHostInvalid)
	}

	if len(h) > 253 || !domainName.MatchString(h) {
		return CodeHost{}, fmt.Errorf("host %q is not a DNS name such as git.example.com: %w", h, ErrCodeHostInvalid)
	}

	for _, private := range []string{".local", ".internal", ".localhost"} {
		if strings.HasSuffix(h, private) {
			return CodeHost{}, fmt.Errorf("host %q is not reachable from the internet: %w", h, ErrCodeHostInvalid)
		}
	}

	if kind != CodeHostGitHub && kind != CodeHostGitLab {
		return CodeHost{}, fmt.Errorf("host %q: kind %q is not github or gitlab: %w", h, kind, ErrCodeHostInvalid)
	}

	return CodeHost{Host: h, Kind: kind}, nil
}

// CheckCodeHosts checks each of hosts with CheckCodeHost and returns them sorted by host; a host
// listed twice, case ignored, or more than MaxCodeHosts hosts wrap ErrCodeHostInvalid.
func CheckCodeHosts(hosts []CodeHost) ([]CodeHost, error) {
	if len(hosts) > MaxCodeHosts {
		return nil, fmt.Errorf("%d hosts, more than %d: %w", len(hosts), MaxCodeHosts, ErrCodeHostInvalid)
	}

	out := make([]CodeHost, 0, len(hosts))
	seen := map[string]bool{}

	for _, h := range hosts {
		c, err := CheckCodeHost(h.Host, h.Kind)
		if err != nil {
			return nil, err
		}

		if seen[c.Host] {
			return nil, fmt.Errorf("host %q is listed twice: %w", c.Host, ErrCodeHostInvalid)
		}

		seen[c.Host] = true
		out = append(out, c)
	}

	slices.SortFunc(out, func(a, b CodeHost) int { return cmp.Compare(a.Host, b.Host) })

	return out, nil
}

// CodeHostsAuditData is the data of an org.code_hosts_set entry: the hosts after the change and
// before it (from), each {host, kind}.
func CodeHostsAuditData(hosts, from []CodeHost) map[string]any {
	list := func(hs []CodeHost) []any {
		out := make([]any, 0, len(hs))
		for _, h := range hs {
			out = append(out, map[string]any{"host": h.Host, "kind": string(h.Kind)})
		}

		return out
	}

	return map[string]any{"hosts": list(hosts), "from": list(from)}
}

// CodeHostAPIBase is the REST API root of h, as the codehost adapters derive it: for GitHub,
// https://api.github.com for github.com, https://api.<name>.ghe.com for GitHub Enterprise Cloud
// and https://<host>/api/v3 for GitHub Enterprise Server; for GitLab, https://<host>/api/v4.
func CodeHostAPIBase(h CodeHost) string {
	switch {
	case h.Kind == CodeHostGitLab:
		return "https://" + h.Host + "/api/v4"
	case h.Host == "github.com" || strings.HasSuffix(h.Host, ".ghe.com"):
		return "https://api." + h.Host
	default:
		return "https://" + h.Host + "/api/v3"
	}
}
