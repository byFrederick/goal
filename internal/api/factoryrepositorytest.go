package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/environmentcheck"
)

// Factory repository tests (ADR 0041 decision 8): POST .../factories/{factory}/repositories/test
// opens a secret for the factory, as the environment test does, and asks the code host of one
// repository, or of an owner pattern ending in /*, what its token can do there, judged for a read
// or a write entry. Each result is kept per factory and url, so pages read it instead of testing
// on every view, and it is tested again when the secret's value is replaced. The opened value
// reaches the code host only: every reason is scrubbed of it before it is answered or kept.

// Errors of the repository test routes, answered with their codes by repositoryTestProblem.
var (
	errRepositoryHostUnknown = errors.New("repository host unknown")
	errRepositoryURLInvalid  = errors.New("repository URL invalid")
)

// couldNotTest starts both checks' reasons when the host gave no answer.
const couldNotTest = "couldn't test"

// repositoryTestProblem answers the repository test rules with their codes.
func repositoryTestProblem(err error) (gen.Problem, bool) {
	for _, a := range []struct {
		err  error
		code string
	}{
		{errRepositoryHostUnknown, "repository-host-unknown"},
		{errRepositoryURLInvalid, "repository-url-invalid"},
	} {
		if errors.Is(err, a.err) {
			p := detailed(http.StatusBadRequest, err)
			p.Code = optional(a.code)

			return p, true
		}
	}

	return gen.Problem{}, false
}

// repositoryURL is raw parsed as a repository's https URL without credentials, query or
// fragment, with at least an owner and a name in its path; errRepositoryURLInvalid otherwise.
func repositoryURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: it is not a URL, such as https://github.com/acme/app", errRepositoryURLInvalid)
	}

	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		len(strings.Split(strings.Trim(u.Path, "/"), "/")) < 2 {
		// Redacted: a URL may carry a token in its user info.
		return nil, fmt.Errorf("%w: %s is not a repository's https URL without credentials, such as https://github.com/acme/app",
			errRepositoryURLInvalid, u.Redacted())
	}

	return u, nil
}

// repositoryCodeHost is the repository at u on the organization's code hosts (ADR 0041 decision
// 1) and the adapter a token for it goes to: the one of the kind its host is listed as. A host
// the organization doesn't list wraps errRepositoryHostUnknown; a URL that names no repository
// on its host, errRepositoryURLInvalid.
func (s *server) repositoryCodeHost(ctx context.Context, u *url.URL) (domain.CodeRepository, ports.CodeHost, error) {
	added, err := s.orgs.CodeHosts(ctx)
	if err != nil {
		return domain.CodeRepository{}, nil, fmt.Errorf("list code hosts: %w", err)
	}

	host := strings.ToLower(u.Host)

	repo, err := domain.ParseCodeRepository(domain.ListedCodeHosts(added), u.String())

	switch {
	case errors.Is(err, domain.ErrCodeHostUnlisted):
		return domain.CodeRepository{}, nil, fmt.Errorf("%w: %s is not one of the organization's code hosts, so goald sends it no token; "+
			"an owner can add it under Code hosts in the organization's settings", errRepositoryHostUnknown, host)
	case err != nil:
		return domain.CodeRepository{}, nil, fmt.Errorf("%w: %s is not a repository's URL, such as https://%s/acme/app", errRepositoryURLInvalid, u.Redacted(), host)
	}

	ch, ok := s.codehosts[string(repo.Host.Kind)]
	if !ok {
		return domain.CodeRepository{}, nil, fmt.Errorf("%w: %s is a %s host, and this goald has no %s code host", errRepositoryHostUnknown, host, repo.Host.Kind, repo.Host.Kind)
	}

	return repo, ch, nil
}

// The texts a factory repository test answers besides the host's reasons.
const (
	readOnlyWarning = "Goal won't push, but agents could. Use a read-only token."
	noPullRequests  = "The host reports no pull-request permission, so Goal can't check it."
)

// repositoryTarget is what a factory repository test asks a code host about: one repository, or
// every repository of owner on host for an owner pattern. key is the url it is kept under.
type repositoryTarget struct {
	ch    ports.CodeHost
	repo  domain.CodeRepository
	owner string
	host  domain.CodeHost
	key   string
}

// pattern reports whether t is an owner pattern.
func (t repositoryTarget) pattern() bool { return t.owner != "" }

func (s *server) TestFactoryRepository(ctx context.Context, req gen.TestFactoryRepositoryRequestObject) (gen.TestFactoryRepositoryResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	b := req.Body
	if !b.Access.Valid() {
		return nil, fmt.Errorf("%w: access is read or write, not %q", errInvalid, b.Access)
	}

	target, err := s.repositoryTarget(ctx, b.Url)
	if err != nil {
		return nil, err
	}

	t, err := s.testRepositoryFor(ctx, rec.ID, target, b.Secret, string(b.Access))
	if err != nil {
		return nil, err
	}

	return gen.TestFactoryRepository200JSONResponse(repositoryTestOut(t)), nil
}

func (s *server) ListFactoryRepositoryTests(ctx context.Context, req gen.ListFactoryRepositoryTestsRequestObject) (gen.ListFactoryRepositoryTestsResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	kept, err := s.repoTests.List(ctx, rec.ID)
	if err != nil {
		return nil, fmt.Errorf("list repository tests: %w", err)
	}

	out := gen.ListFactoryRepositoryTests200JSONResponse{Tests: make([]gen.FactoryRepositoryTest, 0, len(kept))}
	for _, t := range kept {
		out.Tests = append(out.Tests, repositoryTestOut(t))
	}

	return out, nil
}

// repositoryTarget is the repository, or the owner pattern, raw names on the organization's code
// hosts, with the adapter a token for it goes to; errRepositoryHostUnknown or
// errRepositoryURLInvalid as repositoryCodeHost answers them, before any secret is opened.
func (s *server) repositoryTarget(ctx context.Context, raw string) (repositoryTarget, error) {
	u, err := repositoryURL(raw)
	if err != nil {
		return repositoryTarget{}, err
	}

	prefix, pattern := strings.CutSuffix(strings.TrimSuffix(u.Path, "/"), "/*")
	if pattern {
		// The owner's path is checked as a repository's would be, with a name it can't have.
		u.Path = prefix + "/_"
	}

	repo, ch, err := s.repositoryCodeHost(ctx, u)
	if err != nil {
		return repositoryTarget{}, err
	}

	if !pattern {
		return repositoryTarget{ch: ch, repo: repo, key: repo.Key()}, nil
	}

	return repositoryTarget{ch: ch, owner: repo.Owner, host: repo.Host, key: strings.ToLower(repo.Host.Host + "/" + repo.Owner + "/*")}, nil
}

// testRepositoryFor opens secret for factory, asks target's code host what its token can do
// there, judges that for access and keeps the result. A host that gives no answer is a result,
// tested false; an error is a secret that won't open, a URL the host refuses or a failed write.
func (s *server) testRepositoryFor(ctx context.Context, factory domain.FactoryID, target repositoryTarget, secret, access string) (domain.RepositoryTest, error) {
	opened, err := s.secrets.Resolve(ctx, []string{secret}, domain.SecretUse{Factory: factory})
	if err != nil {
		return domain.RepositoryTest{}, secretError(err)
	}

	token := opened[0].Value
	values := map[string]string{secret: token}

	tctx, cancel := context.WithTimeout(ctx, s.repoTestTimeout)
	defer cancel()

	t := domain.RepositoryTest{Factory: factory, URL: target.key, Access: access, Secret: secret, At: s.clock.Now(), Tested: true}

	if target.pattern() {
		var a ports.OwnerAccess
		if a, err = target.ch.OwnerAccess(tctx, target.host, token, target.owner); err == nil {
			reason := environmentcheck.OneLine(a.Reason, values)
			t.ReadOK, t.ReadReason, t.PushOK, t.PushReason = a.Readable > 0, reason, a.Pushable > 0, reason
			t.Readable, t.Pushable, t.Truncated = a.Readable, a.Pushable, a.Truncated
		}
	} else {
		var a ports.RepositoryAccess
		if a, err = target.ch.RepositoryAccess(tctx, target.repo, token); err == nil {
			t.ReadOK, t.ReadReason = a.Read.OK, environmentcheck.OneLine(a.Read.Reason, values)
			t.PushOK, t.PushReason = a.Push.OK, environmentcheck.OneLine(a.Push.Reason, values)
		}
	}

	switch {
	case errors.Is(err, domain.ErrInvalid):
		return domain.RepositoryTest{}, fmt.Errorf("%w: %s", errRepositoryURLInvalid, environmentcheck.Scrub(err.Error(), values))
	case err != nil:
		reason := couldNotTest + ": " + environmentcheck.Scrub(err.Error(), values)
		if errors.Is(err, context.DeadlineExceeded) {
			reason = fmt.Sprintf("%s: %s gave no answer within %s", couldNotTest, strings.SplitN(target.key, "/", 2)[0], s.repoTestTimeout)
		}

		s.log.WarnContext(ctx, "repository test untested", slog.String("factory", string(factory)), slog.String("url", target.key), slog.String("reason", reason))

		t.Tested, t.Summary, t.ReadReason, t.PushReason = false, reason, reason, reason
	default:
		t.OK, t.Summary = judgeRepositoryTest(t, target.owner)
		if t.Access == domain.RepositoryTestRead && t.PushOK {
			t.Warning = readOnlyWarning
		}
	}

	if err := s.repoTests.Record(ctx, t); err != nil {
		return domain.RepositoryTest{}, fmt.Errorf("keep repository test of %s: %w", target.key, err)
	}

	return t, nil
}

// judgeRepositoryTest is whether tested t gives what its access asks, and its one-line summary.
// owner is the owner of an owner pattern.
func judgeRepositoryTest(t domain.RepositoryTest, owner string) (ok bool, summary string) {
	if owner != "" {
		return judgeOwnerTest(t, owner)
	}

	switch {
	case !t.ReadOK:
		return false, environmentcheck.Cut("Can't read: " + t.ReadReason)
	case t.Access == domain.RepositoryTestRead:
		return true, "Can read"
	case t.PushOK:
		return true, "Can read and push"
	default:
		return false, environmentcheck.Cut("Can read but can't push: " + t.PushReason)
	}
}

// judgeOwnerTest is judgeRepositoryTest for an owner pattern's counts.
func judgeOwnerTest(t domain.RepositoryTest, owner string) (ok bool, summary string) {
	count := func(n int) string {
		if t.Truncated {
			return "at least " + strconv.Itoa(n)
		}

		return strconv.Itoa(n)
	}

	repos := count(t.Readable) + " repositories"
	if t.Readable == 1 && !t.Truncated {
		repos = "1 repository"
	}

	switch {
	case t.Readable == 0:
		return false, environmentcheck.Cut("Can't read any repositories in " + owner + ": " + t.ReadReason)
	case t.Access == domain.RepositoryTestRead:
		return true, "Can read " + repos + " in " + owner
	case t.Pushable == 0:
		return false, "Can read " + repos + " in " + owner + " but push to none"
	case t.Pushable == t.Readable:
		return true, "Can read and push to " + repos + " in " + owner
	default:
		return true, "Can read " + repos + " in " + owner + " and push to " + count(t.Pushable)
	}
}

// repositoryTestOut is kept test t as the API answers it.
func repositoryTestOut(t domain.RepositoryTest) gen.FactoryRepositoryTest {
	out := gen.FactoryRepositoryTest{
		Url: t.URL, Access: gen.FactoryRepositoryTestAccess(t.Access), Secret: t.Secret, At: t.At, Tested: t.Tested, Ok: t.OK, Summary: t.Summary,
		Read: gen.RepositoryCheck{Ok: t.ReadOK, Reason: t.ReadReason}, Push: gen.RepositoryCheck{Ok: t.PushOK, Reason: t.PushReason},
	}

	if t.Warning != "" {
		out.Warning = optional(t.Warning)
	}

	if t.Tested && t.Access == domain.RepositoryTestWrite {
		out.Note = optional(noPullRequests)
	}

	if strings.HasSuffix(t.URL, "/*") {
		readable, pushable, truncated := t.Readable, t.Pushable, t.Truncated
		out.Readable, out.Pushable, out.Truncated = &readable, &pushable, &truncated
	}

	return out
}

// retestRepositories tests again, and keeps, every kept repository test whose token came from
// secret, in each factory that is not archived (ADR 0041 decision 8: tests rerun when the
// secret's value changes). The tests run within the request; one that fails is logged, never
// the value, and the next one goes on.
func (s *server) retestRepositories(ctx context.Context, secret string) {
	kept, err := s.repoTests.BySecret(ctx, secret)
	if err != nil {
		s.log.WarnContext(ctx, "secret repository retest skipped", slog.String("secret", secret), slog.Any("error", err))

		return
	}

	if len(kept) == 0 {
		return
	}

	recs, err := s.records(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "secret repository retest skipped", slog.String("secret", secret), slog.Any("error", err))

		return
	}

	for _, t := range kept {
		if rec, ok := recs[t.Factory]; !ok || rec.Archived() {
			continue
		}

		attrs := []any{slog.String("secret", secret), slog.String("factory", string(t.Factory)), slog.String("url", t.URL)}

		target, err := s.repositoryTarget(ctx, "https://"+t.URL)
		if err == nil {
			_, err = s.testRepositoryFor(ctx, t.Factory, target, secret, t.Access)
		}

		if err != nil {
			s.log.WarnContext(ctx, "secret repository retest failed", append(attrs, slog.Any("error", err))...)
		}
	}
}
