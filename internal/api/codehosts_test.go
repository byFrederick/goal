package api_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// codeHostsPath is acme's code hosts; codeHostInvalid answers a host goald refuses.
const (
	codeHostsPath   = acmePath + "/code-hosts"
	codeHostInvalid = "code-host-invalid"
)

// builtInHosts are the hosts every organization lists first.
var builtInHosts = []gen.CodeHost{
	{Host: "github.com", Kind: gen.CodeHostKindGithub, ApiBase: "https://api.github.com", BuiltIn: true},
	{Host: "gitlab.com", Kind: gen.CodeHostKindGitlab, ApiBase: "https://gitlab.com/api/v4", BuiltIn: true},
}

// withCodeHosts is the signed-in harness with acme's people, a fresh owner, admin and the
// owner's fresh session for making keys.
func withCodeHosts(t *testing.T) signedIn {
	t.Helper()

	h := keyed(t)
	withPeople(t, h)

	return h
}

// A fresh owner adds a GitHub Enterprise Server, a GitHub Enterprise Cloud and a GitLab host;
// every member reads them after the built-in ones, sorted and each with its API base. Sending the
// same hosts again audits nothing, and an empty list leaves only the built-in hosts.
func TestCodeHosts_FreshOwnerSetsAndMembersRead(t *testing.T) {
	t.Parallel()

	h := withCodeHosts(t)

	if got := decode[gen.CodeHosts](t, h.as(t, viewerToken, http.MethodGet, codeHostsPath, ""), http.StatusOK); !reflect.DeepEqual(got.Hosts, builtInHosts) {
		t.Fatalf("hosts before = %+v; want only the built-in ones", got.Hosts)
	}

	body := `{"hosts":[{"host":"GitLab.Example.com","kind":"gitlab"},{"host":"acme.ghe.com","kind":"github"},{"host":"ghe.example.com","kind":"github"}]}`

	want := append(append([]gen.CodeHost{}, builtInHosts...),
		gen.CodeHost{Host: "acme.ghe.com", Kind: gen.CodeHostKindGithub, ApiBase: "https://api.acme.ghe.com"},
		gen.CodeHost{Host: "ghe.example.com", Kind: gen.CodeHostKindGithub, ApiBase: "https://ghe.example.com/api/v3"},
		gen.CodeHost{Host: "gitlab.example.com", Kind: gen.CodeHostKindGitlab, ApiBase: "https://gitlab.example.com/api/v4"},
	)

	if got := decode[gen.CodeHosts](t, h.as(t, freshOwnerToken, http.MethodPut, codeHostsPath, body), http.StatusOK); !reflect.DeepEqual(got.Hosts, want) {
		t.Errorf("PUT = %+v; want %+v", got.Hosts, want)
	}

	for _, token := range []string{ownerToken, freshAdminToken, viewerToken} {
		if got := decode[gen.CodeHosts](t, h.as(t, token, http.MethodGet, codeHostsPath, ""), http.StatusOK); !reflect.DeepEqual(got.Hosts, want) {
			t.Errorf("GET as %s = %+v; want %+v", token, got.Hosts, want)
		}
	}

	decode[gen.CodeHosts](t, h.as(t, freshOwnerToken, http.MethodPut, codeHostsPath, body), http.StatusOK)

	if got := decode[gen.CodeHosts](t, h.as(t, freshOwnerToken, http.MethodPut, codeHostsPath, `{"hosts":[]}`), http.StatusOK); !reflect.DeepEqual(got.Hosts, builtInHosts) {
		t.Errorf("after clearing = %+v; want only the built-in ones", got.Hosts)
	}

	page := decode[gen.AuditPage](t, h.as(t, ownerToken, http.MethodGet, acmePath+"/audit?action="+domain.AuditOrgCodeHostsSet, ""), http.StatusOK)
	if len(page.Entries) != 2 {
		t.Errorf("org.code_hosts_set entries = %d; want 2, none for sending the same hosts", len(page.Entries))
	}

	if got := decode[gen.CodeHosts](t, h.as(t, strangerToken, http.MethodGet, "/v1/orgs/"+labsSlug+"/code-hosts", ""), http.StatusOK); !reflect.DeepEqual(got.Hosts, builtInHosts) {
		t.Errorf("another organization's hosts = %+v; want only the built-in ones", got.Hosts)
	}
}

// Only an owner signed in within 10 minutes sets the hosts: a stale owner, an admin, a member and
// an API key are refused, and so is a host goald must not or cannot dial. A refusal changes nothing.
func TestCodeHosts_Refusals(t *testing.T) {
	t.Parallel()

	h := withCodeHosts(t)
	ghe := `{"hosts":[{"host":"ghe.example.com","kind":"github"}]}`

	key, _ := h.create(t, `{"name":"reader","scopes":["org:read"],"role_ceiling":"viewer","expires_in_days":30}`)
	if res := h.request(t, http.MethodGet, codeHostsPath, "", bearer(key.Secret)); res.status != http.StatusOK {
		t.Errorf("GET with an org:read key = %d %s; want 200", res.status, res.body)
	}

	if res := h.request(t, http.MethodPut, codeHostsPath, ghe, bearer(key.Secret)); res.status != http.StatusForbidden {
		t.Errorf("PUT with a key = %d %s; want 403", res.status, res.body)
	}

	for _, c := range []struct {
		name, token, body string
		status            int
		code              string
	}{
		{"a stale owner", ownerToken, ghe, http.StatusForbidden, codeReauth},
		{"an admin", freshAdminToken, ghe, http.StatusForbidden, wantForbidden},
		{"a member with a factory role", viewerToken, ghe, http.StatusForbidden, wantForbidden},
		{"github.com", freshOwnerToken, `{"hosts":[{"host":"github.com","kind":"github"}]}`, http.StatusBadRequest, "code-host-built-in"},
		{"gitlab.com", freshOwnerToken, `{"hosts":[{"host":"GitLab.com","kind":"gitlab"}]}`, http.StatusBadRequest, "code-host-built-in"},
		{"a URL", freshOwnerToken, `{"hosts":[{"host":"https://ghe.example.com","kind":"github"}]}`, http.StatusBadRequest, codeHostInvalid},
		{"a port", freshOwnerToken, `{"hosts":[{"host":"ghe.example.com:8443","kind":"github"}]}`, http.StatusBadRequest, codeHostInvalid},
		{"an IP address", freshOwnerToken, `{"hosts":[{"host":"10.0.0.7","kind":"gitlab"}]}`, http.StatusBadRequest, codeHostInvalid},
		{"localhost", freshOwnerToken, `{"hosts":[{"host":"localhost","kind":"gitlab"}]}`, http.StatusBadRequest, codeHostInvalid},
		{"an internal name", freshOwnerToken, `{"hosts":[{"host":"git.acme.internal","kind":"gitlab"}]}`, http.StatusBadRequest, codeHostInvalid},
		{"an unknown kind", freshOwnerToken, `{"hosts":[{"host":"git.example.com","kind":"bitbucket"}]}`, http.StatusBadRequest, codeHostInvalid},
		{
			"a host twice", freshOwnerToken, `{"hosts":[{"host":"git.example.com","kind":"github"},{"host":"GIT.example.com","kind":"gitlab"}]}`,
			http.StatusBadRequest, codeHostInvalid,
		},
	} {
		if p := problem(t, h.as(t, c.token, http.MethodPut, codeHostsPath, c.body)); p.Status != c.status || deref(p.Code) != c.code {
			t.Errorf("%s: %+v; want %d %q", c.name, p, c.status, c.code)
		}
	}

	if got := decode[gen.CodeHosts](t, h.as(t, ownerToken, http.MethodGet, codeHostsPath, ""), http.StatusOK); !reflect.DeepEqual(got.Hosts, builtInHosts) {
		t.Errorf("hosts after the refusals = %+v; want only the built-in ones", got.Hosts)
	}
}
