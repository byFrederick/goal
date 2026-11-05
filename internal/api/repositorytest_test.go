package api_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The repository test's route, the repository it tests and the secret it opens.
const (
	repoTestPath   = "/v1/orgs/acme/repositories/test"
	testedRepo     = "https://github.com/acme/app"
	repoSecret     = "repo-token"
	repoTokenValue = "ghp_repo-token-value-9d2e" //nolint:gosec // a made-up token for the fake code host
)

// createRepoSecret creates repo-token, open to all factories, holding repoTokenValue.
func (h *harness) createRepoSecret(t *testing.T) {
	t.Helper()

	decode[gen.Secret](t, h.request(t, http.MethodPost, secretsPath,
		`{"name":"`+repoSecret+`","value":"`+repoTokenValue+`","scope":{"all_factories":true}}`, nil), http.StatusCreated)
}

// testRepository posts a repository test of repo with secret.
func (h *harness) testRepository(t *testing.T, repo, secret string) response {
	t.Helper()

	return h.request(t, http.MethodPost, repoTestPath, `{"url":"`+repo+`","secret":"`+secret+`"}`, nil)
}

// A token that can read but not push answers read ok and push failed with the host's reason; the
// token goes to the code host and nowhere in the answer.
func TestRepositoryTest_ReadNoPush(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.codehost.SetAccess(testedRepo, repoTokenValue, ports.RepositoryAccess{
		Read: ports.AccessCheck{OK: true, Reason: "GitHub shows acme/app to the token"},
		Push: ports.AccessCheck{Reason: "GitHub grants the token no push to acme/app (permissions.push is false)"},
	})

	r := h.testRepository(t, testedRepo, repoSecret)
	got := decode[gen.RepositoryTestResult](t, r, http.StatusOK)

	if !got.Tested || !got.Read.Ok || got.Push.Ok || !strings.Contains(got.Push.Reason, "no push") {
		t.Errorf("test = %s; want read ok, push failed with the host's reason", body(t, got))
	}

	if tokens := h.codehost.AccessTokens(); len(tokens) != 1 || tokens[0] != repoTokenValue {
		t.Errorf("the code host got %d tokens; want the secret's value once", len(tokens))
	}

	if strings.Contains(string(r.body), repoTokenValue) {
		t.Error("the answer holds the token")
	}
}

// A token that can read and push answers both ok.
func TestRepositoryTest_ReadAndPush(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.codehost.SetAccess(testedRepo+".git", repoTokenValue, ports.RepositoryAccess{
		Read: ports.AccessCheck{OK: true, Reason: "GitHub shows acme/app to the token"},
		Push: ports.AccessCheck{OK: true, Reason: "GitHub grants the token push to acme/app"},
	})

	got := decode[gen.RepositoryTestResult](t, h.testRepository(t, testedRepo+".git", repoSecret), http.StatusOK)
	if !got.Tested || !got.Read.Ok || !got.Push.Ok || got.Read.Reason == "" || got.Push.Reason == "" {
		t.Errorf("test = %s; want read and push ok, each with a reason", body(t, got))
	}
}

// A missing secret is 404, a host the organization doesn't list 400, a URL naming no
// repository 400, and a secret limited to some factories 400: nothing reaches a code host.
func TestRepositoryTest_Refusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.createSecret(t) // acme-token: open to factory acme only

	for _, tc := range []struct {
		name, repo, secret string
		status             int
		code               string
	}{
		{"a missing secret", testedRepo, "no-such-secret", http.StatusNotFound, "secret-not-found"},
		{"an unknown host", "https://git.example.com/acme/app", repoSecret, http.StatusBadRequest, "repository-host-unknown"},
		{"no repository", "https://github.com/acme", repoSecret, http.StatusBadRequest, "repository-url-invalid"},
		{"not a URL", "github.com/acme/app", repoSecret, http.StatusBadRequest, "repository-url-invalid"},
		{"a secret of one factory", testedRepo, "acme-token", http.StatusBadRequest, "secret-not-allowed"},
	} {
		refused(t, tc.name, h.testRepository(t, tc.repo, tc.secret), tc.status, tc.code)
	}

	if n := len(h.codehost.AccessTokens()); n != 0 {
		t.Errorf("the code host was called %d times; want none", n)
	}
}

// A host that gives no answer within the limit answers tested false with "couldn't test", not a
// failure of the token.
func TestRepositoryTest_HostTimeout(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.codehost.StallAccess()

	r := h.testRepository(t, testedRepo, repoSecret)
	got := decode[gen.RepositoryTestResult](t, r, http.StatusOK)

	if got.Tested || got.Read.Ok || got.Push.Ok || !strings.HasPrefix(got.Read.Reason, "couldn't test") ||
		!strings.HasPrefix(got.Push.Reason, "couldn't test") {
		t.Errorf("test = %s; want tested false, couldn't test", body(t, got))
	}

	if strings.Contains(string(r.body), repoTokenValue) {
		t.Error("the answer holds the token")
	}
}

// enterpriseHost is a GitHub Enterprise host acme lists in the tests that add it.
const enterpriseHost = "git.example.com"

// countedSecrets counts the secrets it opens.
type countedSecrets struct {
	ports.OrgSecrets

	resolves atomic.Int32
}

func (c *countedSecrets) Resolve(ctx context.Context, names []string, use domain.SecretUse) ([]domain.ResolvedSecret, error) {
	c.resolves.Add(1)

	return c.OrgSecrets.Resolve(ctx, names, use)
}

// listHosts sets acme's added code hosts to hosts.
func (h *harness) listHosts(t *testing.T, hosts ...domain.CodeHost) {
	t.Helper()

	if _, err := h.tenancy.SetCodeHosts(domain.WithOrg(t.Context(), testOrg), domain.LocalUser, hosts); err != nil {
		t.Fatalf("set code hosts: %v", err)
	}
}

// A repository on a host acme doesn't list, a GitHub Enterprise host another organization lists
// included, is refused with repository-host-unknown naming the host and Code hosts, before the
// secret is opened or any code host is called.
func TestRepositoryTest_UnlistedHostSendsNothing(t *testing.T) {
	t.Parallel()

	secrets := &countedSecrets{}
	h := newHarnessLogged(t, nil, nil, func(_ *harness, d *api.Deps) {
		secrets.OrgSecrets = d.OrgSecrets
		d.OrgSecrets = secrets
	})
	h.createRepoSecret(t)

	if _, err := h.tenancy.SetCodeHosts(domain.WithOrg(t.Context(), otherOrg), domain.LocalUser,
		[]domain.CodeHost{{Host: enterpriseHost, Kind: domain.CodeHostGitHub}}); err != nil {
		t.Fatalf("set the other organization's code hosts: %v", err)
	}

	h.listHosts(t, domain.CodeHost{Host: "ghe.example.com", Kind: domain.CodeHostGitHub})

	for _, repo := range []string{"https://" + enterpriseHost + "/acme/app", "https://ghe.example.com:8443/acme/app"} {
		r := h.testRepository(t, repo, repoSecret)
		p := refused(t, repo, r, http.StatusBadRequest, "repository-host-unknown")

		host := strings.TrimPrefix(strings.TrimSuffix(repo, "/acme/app"), "https://")
		if d := valueOf(p.Detail); !strings.Contains(d, host) || !strings.Contains(d, "Code hosts") {
			t.Errorf("%s: detail %q; want it to name %s and Code hosts", repo, d, host)
		}
	}

	if n := secrets.resolves.Load(); n != 0 {
		t.Errorf("the secret was opened %d times; want none", n)
	}

	if n := len(h.codehost.AccessTokens()); n != 0 {
		t.Errorf("the code host was called %d times; want none", n)
	}
}

// A repository on a GitHub Enterprise host acme lists as github goes to the github adapter with
// the token, and answers its read and push checks.
func TestRepositoryTest_ListedEnterpriseHost(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.listHosts(t, domain.CodeHost{Host: enterpriseHost, Kind: domain.CodeHostGitHub})

	repo := "https://GIT.example.com/acme/app"
	h.codehost.SetAccess(repo, repoTokenValue, ports.RepositoryAccess{
		Read: ports.AccessCheck{OK: true, Reason: "GitHub shows acme/app to the token"},
		Push: ports.AccessCheck{Reason: "GitHub grants the token no push to acme/app (permissions.push is false)"},
	})

	got := decode[gen.RepositoryTestResult](t, h.testRepository(t, repo, repoSecret), http.StatusOK)
	if !got.Tested || !got.Read.Ok || got.Push.Ok || !strings.Contains(got.Push.Reason, "no push") {
		t.Errorf("test = %s; want read ok, push failed with the host's reason", body(t, got))
	}

	if tokens := h.codehost.AccessTokens(); len(tokens) != 1 || tokens[0] != repoTokenValue {
		t.Errorf("the code host got %d tokens; want the secret's value once", len(tokens))
	}
}
