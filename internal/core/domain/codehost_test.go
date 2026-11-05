package domain

import (
	"errors"
	"strings"
	"testing"
)

// acmeOwner and webName name the parse tests' repositories.
const (
	acmeOwner = "acme"
	webName   = "web"
	appName   = "app"
	appKey    = "github.com/acme/app"
)

// A repository URL, or a pull request's or merge request's in it, names one repository of a listed
// host: .git and a trailing slash are dropped, the key is in lower case, and GitLab's groups nest.
func TestCodeHost_RepositoryParse(t *testing.T) {
	t.Parallel()

	ghe := CodeHost{Host: hostGHE, Kind: CodeHostGitHub}
	lab := CodeHost{Host: hostGitAcme, Kind: CodeHostGitLab}
	hosts := ListedCodeHosts([]CodeHost{ghe, lab})
	gitHub, gitLab := BuiltInCodeHosts()[0], BuiltInCodeHosts()[1]

	for _, c := range []struct {
		raw  string
		want CodeRepository
		key  string
	}{
		{raw: "https://github.com/acme/app", want: CodeRepository{Host: gitHub, Owner: acmeOwner, Name: appName}, key: appKey},
		{raw: "https://GitHub.com/Acme/App.git", want: CodeRepository{Host: gitHub, Owner: "Acme", Name: "App"}, key: appKey},
		{raw: "https://github.com/acme/app/", want: CodeRepository{Host: gitHub, Owner: acmeOwner, Name: appName}, key: appKey},
		{raw: "https://github.com/acme/app/pull/7", want: CodeRepository{Host: gitHub, Owner: acmeOwner, Name: appName}, key: appKey},
		{raw: "https://GHE.example.com/acme/app.git", want: CodeRepository{Host: ghe, Owner: acmeOwner, Name: appName}, key: hostGHE + "/acme/app"},
		{raw: "https://gitlab.com/acme/web", want: CodeRepository{Host: gitLab, Owner: acmeOwner, Name: webName}, key: "gitlab.com/acme/web"},
		{
			raw:  "https://gitlab.com/acme/platform/web/-/merge_requests/3",
			want: CodeRepository{Host: gitLab, Owner: "acme/platform", Name: webName}, key: "gitlab.com/acme/platform/web",
		},
		{
			raw:  "https://" + hostGitAcme + "/Acme/Platform/Web.git",
			want: CodeRepository{Host: lab, Owner: "Acme/Platform", Name: "Web"}, key: hostGitAcme + "/acme/platform/web",
		},
	} {
		got, err := ParseCodeRepository(hosts, c.raw)
		if err != nil || got != c.want || got.Key() != c.key {
			t.Errorf("ParseCodeRepository(%s) = %+v (key %q), %v; want %+v, key %q", c.raw, got, got.Key(), err, c.want, c.key)
		}

		if again, err := ParseCodeRepository(hosts, got.URL()); err != nil || again != got {
			t.Errorf("ParseCodeRepository(URL() %s) = %+v, %v; want %+v", got.URL(), again, err, got)
		}

		if key, ok := RepositoryKey(c.raw); !strings.Contains(c.raw, "/-/") && !strings.Contains(c.raw, "/pull/") && (!ok || key != got.Key()) {
			t.Errorf("RepositoryKey(%s) = %q, %v; want the repository's Key %q", c.raw, key, ok, got.Key())
		}
	}
}

// A URL on a host the organization doesn't list, a port included, fails with
// ErrCodeHostUnlisted naming the host; a URL that names no repository wraps ErrInvalid, and never
// shows credentials it carries.
func TestCodeHost_RepositoryParseRefuses(t *testing.T) {
	t.Parallel()

	hosts := ListedCodeHosts([]CodeHost{{Host: hostGHE, Kind: CodeHostGitHub}})

	for _, raw := range []string{
		"https://code.example.com/acme/app",
		"https://" + hostGHE + ":8443/acme/app.git",
		"https://" + hostGitAcme + "/acme/app/-/merge_requests/1",
	} {
		_, err := ParseCodeRepository(hosts, raw)
		host, _, _ := strings.Cut(strings.TrimPrefix(raw, "https://"), "/")

		if !errors.Is(err, ErrCodeHostUnlisted) || !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), host) {
			t.Errorf("ParseCodeRepository(%s) = %v; want ErrCodeHostUnlisted naming %s", raw, err, host)
		}
	}

	for _, raw := range []string{
		"", "::", "http://github.com/acme/app", "https://github.com/acme", "https://github.com/acme/app?x=1",
		"https://github.com/acme/app#x", "https://github.com/acme/app/issues/7", "https://github.com/acme/app/pull/0",
		"https://github.com/acme/app/tree/main", "https://github.com/acme/team/app", "https://gitlab.com/acme",
		"https://gitlab.com/acme/web/-/issues/3", "https://gitlab.com/-/merge_requests/3", "https://github.com/acme/a%20b", "https://github.com/../app",
		"https://x-token:ghp_secret-value@github.com/acme/app",
	} {
		_, err := ParseCodeRepository(BuiltInCodeHosts(), raw)
		if !errors.Is(err, ErrInvalid) || errors.Is(err, ErrCodeHostUnlisted) || strings.Contains(fmtErr(err), "ghp_secret-value") {
			t.Errorf("ParseCodeRepository(%q) = %v; want ErrInvalid without credentials", raw, err)
		}
	}
}

// ListedCodeHosts are the built-in hosts, then the added ones.
func TestListedCodeHosts_BuiltInsFirst(t *testing.T) {
	t.Parallel()

	ghe := CodeHost{Host: hostGHE, Kind: CodeHostGitHub}

	got := ListedCodeHosts([]CodeHost{ghe})
	if len(got) != 3 || got[0].Host != hostGitHub || got[1].Host != hostGitLab || got[2] != ghe {
		t.Errorf("ListedCodeHosts = %+v; want github.com, gitlab.com, then %s", got, hostGHE)
	}
}

func fmtErr(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}
