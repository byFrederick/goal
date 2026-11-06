package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// What the handlers that read the factories' current versions and send tests from goald share:
// which factories use a name, the limits of a test request and the answer for an unknown
// connection a knowledge source names.

// Limits of a test request goald sends (an environment test's, a repository test's): goald waits
// connectionTestTimeout for its answer and reads at most connectionTestBody of it.
const (
	connectionTestTimeout = 10 * time.Second
	connectionTestBody    = 64 << 10
)

// errConnectionNotFound is a connection a request names that the organization does not have,
// answered with its code by connectionProblem.
var errConnectionNotFound = errors.New("connection not found")

// connectionProblem answers an unknown connection, and a store that cannot open a sealed value
// for want of a master key when no route's own answer names it.
func connectionProblem(err error) (gen.Problem, bool) {
	if errors.Is(err, errConnectionNotFound) {
		p := detailed(http.StatusNotFound, err)
		p.Code = optional("connection-not-found")

		return p, true
	}

	if errors.Is(err, ports.ErrNoMasterKey) {
		detail := "connections with secrets are unavailable: goald has no master key (set GOAL_MASTER_KEYS; .env.example shows how)"

		return gen.Problem{
			Title: http.StatusText(http.StatusServiceUnavailable), Status: http.StatusServiceUnavailable, Detail: &detail,
			Code: optional("connections-unavailable"),
		}, true
	}

	return gen.Problem{}, false
}

// noRedirects makes the test client answer a redirect as it came: a test follows none.
func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// usersBy are the factories goald runs for the organization that are not archived, by the
// names each one's current version uses (names). A factory the store cannot be asked about
// fails the whole answer: an archived one is never assumed, nor a live one dropped.
func (s *server) usersBy(ctx context.Context, names func(*config.Factory) []string) (map[string][]factoryUse, error) {
	recs, err := s.records(ctx)
	if err != nil {
		return nil, err
	}

	out := map[string][]factoryUse{}

	for _, f := range s.factories.Factories(ctx) {
		if recs[domain.FactoryID(f.Metadata.ID)].Archived() {
			continue
		}

		for _, name := range names(f) {
			out[name] = append(out[name], factoryUse{id: domain.FactoryID(f.Metadata.ID), name: f.Metadata.Name})
		}
	}

	return out, nil
}

// factoryUse is a factory that names a secret, a connection or a knowledge source.
type factoryUse struct {
	id   domain.FactoryID
	name string
	// scope is who reads a knowledge source in the factory; zero for every run.
	scope domain.RuleScope
}
