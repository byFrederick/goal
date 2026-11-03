package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
)

// Repository tests (ADR 0037 decision 14): POST /v1/orgs/{org}/repositories/test opens a secret
// without a factory and asks the code host of the repository's host, through its API and never
// git, whether the token can read the repository and push to it. Onboarding calls it before any
// factory exists, and factory settings with a repository token call it too. The opened value
// reaches the code host only: every reason is scrubbed of it before it is answered.

// Errors of the repository test route, answered with their codes by repositoryTestProblem.
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

func (s *server) TestRepository(ctx context.Context, req gen.TestRepositoryRequestObject) (gen.TestRepositoryResponseObject, error) {
	b := req.Body

	u, err := url.Parse(b.Url)
	if err != nil {
		return nil, fmt.Errorf("%w: it is not a URL, such as https://github.com/acme/app", errRepositoryURLInvalid)
	}

	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		len(strings.Split(strings.Trim(u.Path, "/"), "/")) < 2 {
		// Redacted: a URL may carry a token in its user info.
		return nil, fmt.Errorf("%w: %s is not a repository's https URL without credentials, such as https://github.com/acme/app",
			errRepositoryURLInvalid, u.Redacted())
	}

	ch, ok := s.codehosts[config.KnownForge(u.Hostname())]
	if !ok {
		return nil, fmt.Errorf("%w: goald tests repositories on github.com and gitlab.com, not on %s", errRepositoryHostUnknown, u.Hostname())
	}

	opened, err := s.secrets.Resolve(ctx, []string{b.Secret}, domain.SecretUse{})
	if err != nil {
		return nil, secretError(err)
	}

	token := opened[0].Value
	values := map[string]string{b.Secret: token}

	tctx, cancel := context.WithTimeout(ctx, s.repoTestTimeout)
	defer cancel()

	access, err := ch.RepositoryAccess(tctx, b.Url, token)

	switch {
	case errors.Is(err, domain.ErrInvalid):
		return nil, fmt.Errorf("%w: %s", errRepositoryURLInvalid, scrubValues(err.Error(), values))
	case err != nil:
		reason := couldNotTest + ": " + scrubValues(err.Error(), values)
		if errors.Is(err, context.DeadlineExceeded) {
			reason = fmt.Sprintf("%s: %s gave no answer within %s", couldNotTest, u.Hostname(), s.repoTestTimeout)
		}

		s.log.WarnContext(ctx, "repository test untested", slog.String("host", u.Hostname()), slog.String("reason", reason))

		check := gen.RepositoryCheck{Reason: reason}

		return gen.TestRepository200JSONResponse(gen.RepositoryTestResult{Read: check, Push: check}), nil
	}

	return gen.TestRepository200JSONResponse(gen.RepositoryTestResult{
		Tested: true,
		Read:   gen.RepositoryCheck{Ok: access.Read.OK, Reason: oneLine(access.Read.Reason, values)},
		Push:   gen.RepositoryCheck{Ok: access.Push.OK, Reason: oneLine(access.Push.Reason, values)},
	}), nil
}

// oneLine is a host's reason scrubbed of opened's values, on one line and cut to what a test keeps.
func oneLine(reason string, opened map[string]string) string {
	return scrubValues(strings.Join(strings.Fields(reason), " "), opened)
}

