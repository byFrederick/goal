package domain

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestCodeHost_CheckAcceptsANamedPublicHost(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		host string
		kind CodeHostKind
		want CodeHost
	}{
		{" GHE.Example.COM ", CodeHostGitHub, CodeHost{Host: "ghe.example.com", Kind: CodeHostGitHub}},
		{"acme.ghe.com", CodeHostGitHub, CodeHost{Host: "acme.ghe.com", Kind: CodeHostGitHub}},
		{"gitlab.example.com", CodeHostGitLab, CodeHost{Host: "gitlab.example.com", Kind: CodeHostGitLab}},
	} {
		got, err := CheckCodeHost(c.host, c.kind)
		if err != nil || got != c.want {
			t.Errorf("CheckCodeHost(%q, %q) = %+v, %v; want %+v", c.host, c.kind, got, err, c.want)
		}
	}
}

func TestCodeHost_CheckRefusesWhatGoaldCannotOrMustNotDial(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name, host string
		kind       CodeHostKind
		want       error
	}{
		{"a scheme", "https://ghe.example.com", CodeHostGitHub, ErrCodeHostInvalid},
		{"a path", "ghe.example.com/acme", CodeHostGitHub, ErrCodeHostInvalid},
		{"a port", "ghe.example.com:8443", CodeHostGitHub, ErrCodeHostInvalid},
		{"an IPv4 literal", "203.0.113.7", CodeHostGitHub, ErrCodeHostInvalid},
		{"an IPv6 literal", "[2001:db8::1]", CodeHostGitHub, ErrCodeHostInvalid},
		{"localhost", "localhost", CodeHostGitHub, ErrCodeHostInvalid},
		{"a single label", "gitlab", CodeHostGitLab, ErrCodeHostInvalid},
		{"a .local name", "git.acme.local", CodeHostGitLab, ErrCodeHostInvalid},
		{"an .internal name", "git.acme.internal", CodeHostGitLab, ErrCodeHostInvalid},
		{"a .localhost name", "git.localhost", CodeHostGitLab, ErrCodeHostInvalid},
		{"nothing", "  ", CodeHostGitHub, ErrCodeHostInvalid},
		{"an unknown kind", "git.example.com", "bitbucket", ErrCodeHostInvalid},
		{"no kind", "git.example.com", "", ErrCodeHostInvalid},
		{"github.com", "GitHub.com", CodeHostGitHub, ErrCodeHostBuiltIn},
		{"gitlab.com", "gitlab.com", CodeHostGitLab, ErrCodeHostBuiltIn},
	} {
		if _, err := CheckCodeHost(c.host, c.kind); !errors.Is(err, c.want) || !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: CheckCodeHost(%q, %q) = %v; want %v", c.name, c.host, c.kind, err, c.want)
		}
	}
}

func TestCodeHost_CheckListSortsAndRefusesDuplicatesAndTooMany(t *testing.T) {
	t.Parallel()

	got, err := CheckCodeHosts([]CodeHost{{Host: "Zeta.example.com", Kind: CodeHostGitLab}, {Host: "alpha.example.com", Kind: CodeHostGitHub}})
	want := []CodeHost{{Host: "alpha.example.com", Kind: CodeHostGitHub}, {Host: "zeta.example.com", Kind: CodeHostGitLab}}

	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("CheckCodeHosts = %+v, %v; want %+v", got, err, want)
	}

	if got, err := CheckCodeHosts(nil); err != nil || len(got) != 0 {
		t.Errorf("CheckCodeHosts(nil) = %+v, %v; want none", got, err)
	}

	dup := []CodeHost{{Host: "git.example.com", Kind: CodeHostGitHub}, {Host: "GIT.example.com", Kind: CodeHostGitLab}}
	if _, err := CheckCodeHosts(dup); !errors.Is(err, ErrCodeHostInvalid) {
		t.Errorf("a duplicate: %v; want ErrCodeHostInvalid", err)
	}

	many := make([]CodeHost, MaxCodeHosts+1)
	for i := range many {
		many[i] = CodeHost{Host: fmt.Sprintf("git%d.example.com", i), Kind: CodeHostGitHub}
	}

	if _, err := CheckCodeHosts(many[:MaxCodeHosts]); err != nil {
		t.Errorf("%d hosts: %v; want accepted", MaxCodeHosts, err)
	}

	if _, err := CheckCodeHosts(many); !errors.Is(err, ErrCodeHostInvalid) {
		t.Errorf("%d hosts: %v; want ErrCodeHostInvalid", len(many), err)
	}

	if _, err := CheckCodeHosts([]CodeHost{{Host: "github.com", Kind: CodeHostGitHub}}); !errors.Is(err, ErrCodeHostBuiltIn) {
		t.Errorf("a built-in host: %v; want ErrCodeHostBuiltIn", err)
	}
}

func TestCodeHost_BuiltInsAreGitHubAndGitLab(t *testing.T) {
	t.Parallel()

	want := []CodeHost{{Host: "github.com", Kind: CodeHostGitHub, BuiltIn: true}, {Host: "gitlab.com", Kind: CodeHostGitLab, BuiltIn: true}}
	if got := BuiltInCodeHosts(); !reflect.DeepEqual(got, want) {
		t.Errorf("BuiltInCodeHosts() = %+v; want %+v", got, want)
	}

	// A copy: changing it changes no later answer.
	BuiltInCodeHosts()[0].Host = "example.com"
	if got := BuiltInCodeHosts(); got[0].Host != "github.com" {
		t.Errorf("BuiltInCodeHosts()[0] = %+v after a caller changed its copy", got[0])
	}
}

func TestCodeHost_APIBase(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		host CodeHost
		want string
	}{
		{CodeHost{Host: "github.com", Kind: CodeHostGitHub}, "https://api.github.com"},
		{CodeHost{Host: "acme.ghe.com", Kind: CodeHostGitHub}, "https://api.acme.ghe.com"},
		{CodeHost{Host: "ghe.example.com", Kind: CodeHostGitHub}, "https://ghe.example.com/api/v3"},
		{CodeHost{Host: "gitlab.com", Kind: CodeHostGitLab}, "https://gitlab.com/api/v4"},
		{CodeHost{Host: "gitlab.example.com", Kind: CodeHostGitLab}, "https://gitlab.example.com/api/v4"},
	} {
		if got := CodeHostAPIBase(c.host); got != c.want {
			t.Errorf("CodeHostAPIBase(%+v) = %q; want %q", c.host, got, c.want)
		}
	}
}
