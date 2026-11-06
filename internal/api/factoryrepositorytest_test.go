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
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The factory repository test's routes, and the value a replaced repo-token holds.
const (
	factoryRepoTestPath  = acmeFactory + "/repositories/test"
	factoryRepoTestsPath = acmeFactory + "/repositories/tests"
	repoTokenReplaced    = "ghp_repo-token-replaced-51c0" //nolint:gosec // a made-up token for the fake code host
	readOnlyWarning      = "Goal won't push, but agents could. Use a read-only token."
	testedRepoKey        = "github.com/acme/app"
	acmeOwnerKey         = "github.com/acme/*"
	appShown             = "GitHub shows acme/app to the token"
	accessRead           = "read"
	codeRepoURLInvalid   = "repository-url-invalid"
)

// The repository a repository test tests and the secret it opens.
const (
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

// repositoryTestsOf keeps repository tests of factories, org's.
func repositoryTestsOf(org domain.OrgID, factories ...domain.FactoryID) *porttest.RepositoryTests {
	tests := porttest.NewRepositoryTests()
	for _, f := range factories {
		tests.AddFactory(org, f)
	}

	return tests
}

// testFactoryRepository posts a test of repo with secret for an entry of access to path.
func (h *harness) testFactoryRepository(t *testing.T, path, repo, secret, access string) response {
	t.Helper()

	return h.request(t, http.MethodPost, path, `{"url":"`+repo+`","secret":"`+secret+`","access":"`+access+`"}`, nil)
}

// keptRepositoryTests are acme's kept repository tests.
func (h *harness) keptRepositoryTests(t *testing.T) []gen.FactoryRepositoryTest {
	t.Helper()

	return decode[gen.FactoryRepositoryTestList](t, h.request(t, http.MethodGet, factoryRepoTestsPath, "", nil), http.StatusOK).Tests
}

// readNoPush is what a read-only token gets at acme/app.
var readNoPush = ports.RepositoryAccess{
	Read: ports.AccessCheck{OK: true, Reason: appShown},
	Push: ports.AccessCheck{Reason: "GitHub grants the token no push to acme/app (permissions.push is false)"},
}

// readAndPush is what a token that can push gets at acme/app.
var readAndPush = ports.RepositoryAccess{
	Read: ports.AccessCheck{OK: true, Reason: appShown},
	Push: ports.AccessCheck{OK: true, Reason: "GitHub grants the token push to acme/app"},
}

// A read entry whose token reads and can't push answers "Can read", with no warning and no note;
// the token goes to the code host and nowhere in the answer.
func TestFactoryRepositoryTest_Read(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.codehost.SetAccess(testedRepo, repoTokenValue, readNoPush)

	r := h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, accessRead)
	got := decode[gen.FactoryRepositoryTest](t, r, http.StatusOK)

	if !got.Tested || !got.Ok || got.Summary != "Can read" || got.Warning != nil || got.Note != nil ||
		got.Url != testedRepoKey || got.Push.Ok || !strings.Contains(got.Push.Reason, "no push") || got.Readable != nil {
		t.Errorf("test = %s; want Can read, no warning, the host's reasons", body(t, got))
	}

	if tokens := h.codehost.AccessTokens(); len(tokens) != 1 || tokens[0] != repoTokenValue {
		t.Errorf("the code host got %d tokens; want the secret's value once", len(tokens))
	}

	if strings.Contains(string(r.body), repoTokenValue) {
		t.Error("the answer holds the token")
	}
}

// A read entry whose token can also push passes, and warns that agents could push.
func TestFactoryRepositoryTest_ReadWarnsOnPush(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.codehost.SetAccess(testedRepo, repoTokenValue, readAndPush)

	got := decode[gen.FactoryRepositoryTest](t, h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, accessRead), http.StatusOK)
	if !got.Ok || got.Summary != "Can read" || valueOf(got.Warning) != readOnlyWarning {
		t.Errorf("test = %s; want Can read with the read-only warning", body(t, got))
	}
}

// An owner pattern counts the repositories the token can read, "at least" past the adapter's
// limit, and warns a read entry when it can push to any.
func TestFactoryRepositoryTest_OwnerPattern(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.codehost.SetOwnerAccess("github.com", "acme", repoTokenValue, ports.OwnerAccess{Readable: 48, Reason: "GitHub lists 48 repositories of acme"})
	h.codehost.SetOwnerAccess("github.com", "labs", repoTokenValue, ports.OwnerAccess{
		Readable: 1000, Pushable: 2, Truncated: true, Reason: "GitHub lists 1000 repositories of labs",
	})

	got := decode[gen.FactoryRepositoryTest](t, h.testFactoryRepository(t, factoryRepoTestPath, "https://github.com/acme/*", repoSecret, accessRead), http.StatusOK)
	if !got.Tested || !got.Ok || got.Summary != "Can read 48 repositories in acme" || got.Warning != nil || got.Url != acmeOwnerKey {
		t.Errorf("acme/* = %s; want Can read 48 repositories in acme, without a warning", body(t, got))
	}

	if counts := [3]any{valueOf(got.Readable), valueOf(got.Pushable), valueOf(got.Truncated)}; counts != [3]any{48, 0, false} {
		t.Errorf("acme/* counts = %v; want 48 readable, 0 pushable, not truncated", counts)
	}

	got = decode[gen.FactoryRepositoryTest](t, h.testFactoryRepository(t, factoryRepoTestPath, "https://github.com/labs/*", repoSecret, accessRead), http.StatusOK)
	if !got.Ok || got.Summary != "Can read at least 1000 repositories in labs" || valueOf(got.Warning) != readOnlyWarning {
		t.Errorf("labs/* = %s; want Can read at least 1000 repositories in labs, with the warning", body(t, got))
	}

	got = decode[gen.FactoryRepositoryTest](t, h.testFactoryRepository(t, factoryRepoTestPath, "https://github.com/nobody/*", repoSecret, accessRead), http.StatusOK)
	if !got.Tested || got.Ok || !strings.HasPrefix(got.Summary, "Can't read any repositories in nobody") {
		t.Errorf("nobody/* = %s; want a failed test that says so", body(t, got))
	}
}

// A write entry whose token reads and pushes answers "Can read and push" with the note that the
// host reports no pull-request permission; one that can't push fails with the host's reason.
func TestFactoryRepositoryTest_ReadAndPush(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.codehost.SetAccess(testedRepo, repoTokenValue, readAndPush)

	got := decode[gen.FactoryRepositoryTest](t, h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, "write"), http.StatusOK)
	if !got.Ok || got.Summary != "Can read and push" || got.Warning != nil ||
		valueOf(got.Note) != "The host reports no pull-request permission, so Goal can't check it." {
		t.Errorf("test = %s; want Can read and push with the pull-request note", body(t, got))
	}

	h.codehost.SetAccess(testedRepo, repoTokenValue, readNoPush)

	got = decode[gen.FactoryRepositoryTest](t, h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, "write"), http.StatusOK)
	if got.Ok || !strings.HasPrefix(got.Summary, "Can read but can't push: ") || !strings.Contains(got.Summary, "no push") {
		t.Errorf("test = %s; want a failed write test with the host's reason", body(t, got))
	}
}

// A host that gives no answer within the limit answers, and keeps, tested false with
// "couldn't test".
func TestFactoryRepositoryTest_HostTimeout(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.codehost.StallAccess()

	r := h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, accessRead)
	got := decode[gen.FactoryRepositoryTest](t, r, http.StatusOK)

	if got.Tested || got.Ok || !strings.HasPrefix(got.Summary, "couldn't test") || !strings.HasPrefix(got.Read.Reason, "couldn't test") {
		t.Errorf("test = %s; want tested false, couldn't test", body(t, got))
	}

	if strings.Contains(string(r.body), repoTokenValue) {
		t.Error("the answer holds the token")
	}

	if kept := h.keptRepositoryTests(t); len(kept) != 1 || kept[0].Tested {
		t.Errorf("kept = %s; want the untested result", body(t, kept))
	}
}

// The organization route's refusals hold for the factory's, before the secret is opened or a code
// host called; an access other than read or write, and an unknown factory, are refused too.
func TestFactoryRepositoryTest_Refusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)

	for _, tc := range []struct {
		name, path, repo, secret, access string
		status                           int
		code                             string
	}{
		{"a missing secret", factoryRepoTestPath, testedRepo, "no-such-secret", accessRead, http.StatusNotFound, codeSecretNotFound},
		{"an unknown host", factoryRepoTestPath, "https://git.example.com/acme/app", repoSecret, accessRead, http.StatusBadRequest, "repository-host-unknown"},
		{"an unknown host's owner", factoryRepoTestPath, "https://git.example.com/acme/*", repoSecret, accessRead, http.StatusBadRequest, "repository-host-unknown"},
		{"no repository", factoryRepoTestPath, "https://github.com/acme", repoSecret, accessRead, http.StatusBadRequest, codeRepoURLInvalid},
		{"a nested GitHub owner", factoryRepoTestPath, "https://github.com/acme/team/*", repoSecret, accessRead, http.StatusBadRequest, codeRepoURLInvalid},
		{"not a URL", factoryRepoTestPath, "github.com/acme/app", repoSecret, accessRead, http.StatusBadRequest, codeRepoURLInvalid},
		{"an unknown factory", "/v1/orgs/acme/factories/nope/repositories/test", testedRepo, repoSecret, accessRead, http.StatusNotFound, codeFactoryNotFound},
	} {
		refused(t, tc.name, h.testFactoryRepository(t, tc.path, tc.repo, tc.secret, tc.access), tc.status, tc.code)
	}

	if r := h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, "admin"); r.status != http.StatusBadRequest {
		t.Errorf("access admin = %d; want 400", r.status)
	}

	if n := len(h.codehost.AccessTokens()); n != 0 {
		t.Errorf("the code host was called %d times; want none", n)
	}

	if kept := h.keptRepositoryTests(t); len(kept) != 0 {
		t.Errorf("kept = %s; want none", body(t, kept))
	}
}

// A secret open to acme only is opened for acme, and refused for another factory with
// secret-not-allowed: the secret is opened for the factory under test.
func TestFactoryRepositoryTest_SecretForOtherFactory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createSecret(t) // acme-token: open to factory acme only
	h.codehost.SetAccess(testedRepo, secretValue, readNoPush)

	got := decode[gen.FactoryRepositoryTest](t, h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, "acme-token", accessRead), http.StatusOK)
	if !got.Ok {
		t.Errorf("acme's test = %s; want it to pass", body(t, got))
	}

	refused(t, "line's test", h.testFactoryRepository(t, "/v1/orgs/acme/factories/line/repositories/test", testedRepo, "acme-token", accessRead),
		http.StatusBadRequest, "secret-not-allowed")
}

// Every test is kept per factory and url, the newest replacing the one before, and the GET lists
// them by url without calling the code host.
func TestFactoryRepositoryTest_KeptAndListed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.codehost.SetAccess(testedRepo, repoTokenValue, readNoPush)
	h.codehost.SetOwnerAccess("github.com", "acme", repoTokenValue, ports.OwnerAccess{Readable: 3, Reason: "GitHub lists 3 repositories of acme"})

	h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, "write")
	h.testFactoryRepository(t, factoryRepoTestPath, testedRepo+".git", repoSecret, accessRead)
	h.testFactoryRepository(t, factoryRepoTestPath, "https://github.com/acme/*", repoSecret, accessRead)

	calls := len(h.codehost.AccessTokens())

	kept := h.keptRepositoryTests(t)
	if len(kept) != 2 || kept[0].Url != acmeOwnerKey || kept[1].Url != testedRepoKey ||
		kept[1].Access != gen.FactoryRepositoryTestAccessRead || kept[1].Summary != "Can read" || kept[0].Summary != "Can read 3 repositories in acme" {
		t.Errorf("kept = %s; want acme/* then acme/app, as last tested", body(t, kept))
	}

	if n := len(h.codehost.AccessTokens()); n != calls {
		t.Errorf("listing called the code host %d times; want none", n-calls)
	}

	line := decode[gen.FactoryRepositoryTestList](t, h.request(t, http.MethodGet, "/v1/orgs/acme/factories/line/repositories/tests", "", nil), http.StatusOK)
	if len(line.Tests) != 0 {
		t.Errorf("line's kept tests = %s; want none", body(t, line))
	}
}

// Replacing a secret's value tests every kept test that names it again, with the new value, and
// keeps the new result; a test of another secret is left alone.
func TestFactoryRepositoryTest_RetestOnSecretReplace(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.createSecret(t)
	h.codehost.SetAccess(testedRepo, repoTokenValue, readNoPush)
	h.codehost.SetAccess("https://github.com/acme/web", secretValue, readNoPush)

	h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, "write")
	h.testFactoryRepository(t, factoryRepoTestPath, "https://github.com/acme/web", "acme-token", accessRead)

	if kept := h.keptRepositoryTests(t); len(kept) != 2 || kept[0].Ok {
		t.Fatalf("kept before = %s; want acme/app failing to push", body(t, kept))
	}

	h.codehost.SetAccess(testedRepo, repoTokenReplaced, readAndPush)
	calls := len(h.codehost.AccessTokens())

	r := h.request(t, http.MethodPut, secretsPath+"/"+repoSecret+"/value", `{"value":"`+repoTokenReplaced+`"}`, nil)
	decode[gen.Secret](t, r, http.StatusOK)

	if tokens := h.codehost.AccessTokens()[calls:]; len(tokens) != 1 || tokens[0] != repoTokenReplaced {
		t.Errorf("the replace sent the code host %d tokens; want the new value once", len(tokens))
	}

	kept := h.keptRepositoryTests(t)
	if len(kept) != 2 || !kept[0].Ok || kept[0].Summary != "Can read and push" || kept[1].Url != "github.com/acme/web" || !kept[1].Ok {
		t.Errorf("kept after = %s; want acme/app retested with the new value, acme/web as it was", body(t, kept))
	}
}

// A replace whose retest finds no host answering still replaces the value.
func TestFactoryRepositoryTest_RetestNeverFailsReplace(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.codehost.SetAccess(testedRepo, repoTokenValue, readNoPush)
	h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, accessRead)
	h.codehost.StallAccess()

	decode[gen.Secret](t, h.request(t, http.MethodPut, secretsPath+"/"+repoSecret+"/value", `{"value":"`+repoTokenReplaced+`"}`, nil), http.StatusOK)

	if kept := h.keptRepositoryTests(t); len(kept) != 1 || kept[0].Tested {
		t.Errorf("kept = %s; want the untested retest", body(t, kept))
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
func TestFactoryRepositoryTest_UnlistedHostSendsNothing(t *testing.T) {
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
		r := h.testFactoryRepository(t, factoryRepoTestPath, repo, repoSecret, accessRead)
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
// the token, and answers its read and push checks for a read entry.
func TestFactoryRepositoryTest_ListedEnterpriseHost(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.listHosts(t, domain.CodeHost{Host: enterpriseHost, Kind: domain.CodeHostGitHub})

	repo := "https://GIT.example.com/acme/app"
	h.codehost.SetAccess(repo, repoTokenValue, ports.RepositoryAccess{
		Read: ports.AccessCheck{OK: true, Reason: "GitHub shows acme/app to the token"},
		Push: ports.AccessCheck{Reason: "GitHub grants the token no push to acme/app (permissions.push is false)"},
	})

	got := decode[gen.FactoryRepositoryTest](t, h.testFactoryRepository(t, factoryRepoTestPath, repo, repoSecret, accessRead), http.StatusOK)
	if !got.Tested || !got.Ok || !got.Read.Ok || got.Push.Ok || !strings.Contains(got.Push.Reason, "no push") {
		t.Errorf("test = %s; want read ok, push failed with the host's reason", body(t, got))
	}

	if tokens := h.codehost.AccessTokens(); len(tokens) != 1 || tokens[0] != repoTokenValue {
		t.Errorf("the code host got %d tokens; want the secret's value once", len(tokens))
	}
}

// ciReadable and ciRefused are the fake host's CI checks of acme/app.
var (
	ciReadable = ports.AccessCheck{OK: true, Reason: "GitHub shows the checks and statuses of acme/app"}
	ciRefused  = ports.AccessCheck{Reason: "the token has no read access to Checks and Commit statuses in acme/app"}
)

// A one-repository test whose token reads CI answers, and keeps, ci passing with the host's
// reason.
func TestFactoryRepositoryTest_CIPasses(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)

	a := readNoPush
	a.CI = ciReadable
	h.codehost.SetAccess(testedRepo, repoTokenValue, a)

	got := decode[gen.FactoryRepositoryTest](t, h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, accessRead), http.StatusOK)
	if got.Ci == nil || !got.Ci.Ok || got.Ci.Reason != ciReadable.Reason {
		t.Errorf("test = %s; want ci passing with the host's reason", body(t, got))
	}

	if kept := h.keptRepositoryTests(t); len(kept) != 1 || kept[0].Ci == nil || !kept[0].Ci.Ok {
		t.Errorf("kept = %s; want the passing ci kept", body(t, kept))
	}
}

// A token that can't read CI fails ci with the host's reason, and ok, summary, warning, read and
// push stay as they are without it: only a factory that waits for CI needs it. A host that gives
// no answer fails ci with the same couldn't test reason as read and push.
func TestFactoryRepositoryTest_CIFailsKeepsOK(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)

	for _, access := range []ports.RepositoryAccess{readNoPush, readAndPush} {
		for _, entry := range []string{accessRead, "write"} {
			h.codehost.SetAccess(testedRepo, repoTokenValue, access)
			want := decode[gen.FactoryRepositoryTest](t, h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, entry), http.StatusOK)

			refused := access
			refused.CI = ciRefused
			h.codehost.SetAccess(testedRepo, repoTokenValue, refused)
			got := decode[gen.FactoryRepositoryTest](t, h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, entry), http.StatusOK)

			if got.Ci == nil || got.Ci.Ok || got.Ci.Reason != ciRefused.Reason {
				t.Errorf("%s test = %s; want ci failing with the host's reason", entry, body(t, got))
			}

			if got.Ok != want.Ok || got.Summary != want.Summary || valueOf(got.Warning) != valueOf(want.Warning) || got.Read != want.Read || got.Push != want.Push {
				t.Errorf("%s test with CI refused = %s; want ok, summary, warning, read and push as without it: %s", entry, body(t, got), body(t, want))
			}
		}
	}

	h.codehost.StallAccess()

	got := decode[gen.FactoryRepositoryTest](t, h.testFactoryRepository(t, factoryRepoTestPath, testedRepo, repoSecret, accessRead), http.StatusOK)
	if got.Ci == nil || got.Ci.Ok || got.Ci.Reason != got.Read.Reason || !strings.HasPrefix(got.Ci.Reason, "couldn't test") {
		t.Errorf("untested = %s; want ci failing with read's couldn't test reason", body(t, got))
	}
}

// A test kept before goald checked CI, and an owner pattern's, answer no ci key at all: absent,
// not failed.
func TestFactoryRepositoryTest_CIAbsent(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createRepoSecret(t)
	h.codehost.SetOwnerAccess("github.com", "acme", repoTokenValue, ports.OwnerAccess{Readable: 2, Reason: "GitHub lists 2 repositories of acme"})

	old := domain.RepositoryTest{
		Factory: acmeID, URL: testedRepoKey, Access: accessRead, Secret: repoSecret, At: h.clock.Now(), Tested: true, OK: true,
		Summary: "Can read", ReadOK: true, ReadReason: appShown, PushReason: "no push",
	}
	if err := h.repoTests.Record(inOrg(t), old); err != nil {
		t.Fatal(err)
	}

	r := h.testFactoryRepository(t, factoryRepoTestPath, "https://github.com/acme/*", repoSecret, accessRead)
	if got := decode[gen.FactoryRepositoryTest](t, r, http.StatusOK); got.Ci != nil || strings.Contains(string(r.body), `"ci"`) {
		t.Errorf("owner pattern = %s; want no ci", r.body)
	}

	list := h.request(t, http.MethodGet, factoryRepoTestsPath, "", nil)
	if kept := decode[gen.FactoryRepositoryTestList](t, list, http.StatusOK).Tests; len(kept) != 2 || kept[0].Ci != nil || kept[1].Ci != nil {
		t.Errorf("kept = %s; want both tests without ci", body(t, kept))
	}

	if strings.Contains(string(list.body), `"ci"`) {
		t.Errorf("list = %s; want no ci key", list.body)
	}
}
