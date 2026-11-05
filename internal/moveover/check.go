package moveover

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"testing/fstest"
	"time"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/environmentcheck"
)

// CheckTimeout bounds each check's call to a code host or an MCP server when Deps sets none, so a
// host that does not answer cannot hold goald's start: the check fails and the next start tries
// again.
const CheckTimeout = 10 * time.Second

// check tests converted version tree of factory before it is published (ADR 0037 decision 19):
// its repository token must read the repository and push to it, which stands in for a clone and
// a push since goald runs no git, and every remote MCP server of every environment file must
// answer tools/list with its headers' secrets opened. It returns a problem per failed check, each
// naming what to fix and none holding a value; err is a refusal to open a secret other than a
// missing or disallowed one, such as a missing master key.
func (m *Mover) check(ctx context.Context, factory domain.FactoryID, tree map[string][]byte) ([]string, error) {
	fs := fstest.MapFS{}
	for p, b := range tree {
		fs[p] = &fstest.MapFile{Data: b}
	}

	f, _ := config.Check(fs)
	if f == nil {
		return []string{"the converted version does not parse"}, nil
	}

	open := environmentcheck.Opens(m.d.Secrets, domain.SecretUse{Factory: factory})

	var problems []string

	if r := f.Spec.Runtime.Repository; r != nil && r.Token != "" {
		p, err := m.repository(ctx, *r, open)
		if err != nil {
			return nil, err
		}

		problems = append(problems, p...)
	}

	for _, e := range f.Environments() {
		for _, name := range slices.Sorted(maps.Keys(e.Spec.MCP)) {
			s := e.Spec.MCP[name]
			if s.URL == "" {
				continue
			}

			tctx, cancel := context.WithTimeout(ctx, m.timeout())
			r, err := environmentcheck.Server(tctx, m.d.Tools, open, ports.MCPEndpoint{URL: s.URL, Headers: s.Headers}, nil, nil)

			cancel()

			if err != nil {
				return nil, fmt.Errorf("test MCP server %s of %s: %w", name, e.File(), err)
			}

			if !r.OK {
				problems = append(problems, fmt.Sprintf("%s: MCP server %s did not answer tools/list: %s. Fix the server's url or headers, or the secret they name",
					e.File(), name, clause(r.Error)))
			}
		}
	}

	return problems, nil
}

// repository tests r's token on its code host: a problem for a read or a push it cannot do, or
// for a host goald cannot ask.
func (m *Mover) repository(ctx context.Context, r config.Repository, open environmentcheck.Opener) ([]string, error) {
	u, err := url.Parse(r.URL)
	if err != nil {
		return []string{fmt.Sprintf("repository %s is not a URL: fix runtime.repository.url", r.URL)}, nil
	}

	repo := u.Redacted()

	kind := cmp.Or(r.Forge, config.KnownForge(u.Hostname()))

	host, ok := m.d.CodeHosts[kind]
	if !ok {
		return []string{fmt.Sprintf("repository %s: goald can test a token on github.com, gitlab.com or a host runtime.repository.forge names, not on %s",
			repo, u.Hostname())}, nil
	}

	// The host as the seed lists it from the version (codeHosts), which runs after this check.
	on, err := domain.ParseCodeRepository([]domain.CodeHost{{Host: strings.ToLower(u.Host), Kind: domain.CodeHostKind(kind)}}, r.URL)
	if err != nil {
		return []string{fmt.Sprintf("repository %s is not a repository's URL: fix runtime.repository.url", repo)}, nil
	}

	names := environmentcheck.Referenced(map[string]string{"token": r.Token})
	if len(names) != 1 {
		return []string{fmt.Sprintf("repository %s: runtime.repository.token is not one {{secret}} reference", repo)}, nil
	}

	values, err := open(ctx, names)

	switch {
	case errors.Is(err, ports.ErrNotFound) || errors.Is(err, ports.ErrSecretNotAllowed):
		return []string{fmt.Sprintf("repository %s: the token's secret %s did not open: %s", repo, names[0], environmentcheck.Cut(err.Error()))}, nil
	case err != nil:
		return nil, fmt.Errorf("open the repository token's secret %s: %w", names[0], err)
	}

	tctx, cancel := context.WithTimeout(ctx, m.timeout())
	defer cancel()

	access, err := host.RepositoryAccess(tctx, on, values[names[0]])

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return []string{fmt.Sprintf("repository %s: couldn't test the token: %s gave no answer within %s", repo, u.Hostname(), m.timeout())}, nil
	case err != nil:
		return []string{fmt.Sprintf("repository %s: couldn't test the token: %s", repo, environmentcheck.Scrub(err.Error(), values))}, nil
	}

	var problems []string

	for _, c := range []struct {
		what  string
		check ports.AccessCheck
	}{{"read the repository", access.Read}, {"push to it", access.Push}} {
		if !c.check.OK {
			problems = append(problems, fmt.Sprintf("repository %s: the token in secret %s can't %s: %s. Give the token that access, or replace the secret's value",
				repo, names[0], c.what, clause(environmentcheck.OneLine(c.check.Reason, values))))
		}
	}

	return problems, nil
}

// clause is a host's reason or error inside a problem's sentence: without its own end, as Needs
// you ends each problem.
func clause(s string) string { return strings.TrimRight(s, ". ") }

// timeout bounds one check's call.
func (m *Mover) timeout() time.Duration { return cmp.Or(m.d.CheckTimeout, CheckTimeout) }
