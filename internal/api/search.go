package api

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The shell's search box (tag shell): one read over the organization's items, agents, factories
// and secrets, each group best first and only what the caller sees.

// Limits of a search.
const (
	maxSearchQuery     = 200
	defaultSearchLimit = 5
	maxSearchLimit     = 20
)

// permSecretRead is the permission the search's secrets group needs, reading the organization's
// secrets' metadata and never a value; without it the group is empty.
const permSecretRead domain.Permission = "secret:read"

func (s *server) SearchOrg(ctx context.Context, req gen.SearchOrgRequestObject) (gen.SearchOrgResponseObject, error) {
	q := strings.TrimSpace(req.Params.Q)
	if q == "" || utf8.RuneCountInString(q) > maxSearchQuery {
		return nil, fmt.Errorf("%w: q must have 1 to %d characters", errInvalid, maxSearchQuery)
	}

	limit := defaultSearchLimit
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

	if limit < 1 || limit > maxSearchLimit {
		return nil, fmt.Errorf("%w: limit must be in [1, %d]", errInvalid, maxSearchLimit)
	}

	fs := s.visibleFactories(ctx)

	items, err := s.searchItems(ctx, fs, q, limit)
	if err != nil {
		return nil, err
	}

	secrets, err := s.searchSecrets(ctx, q, limit)
	if err != nil {
		return nil, err
	}

	return gen.SearchOrg200JSONResponse{
		Items: items, Agents: searchAgents(fs, q, limit), Factories: searchFactories(fs, q, limit), Secrets: secrets,
	}, nil
}

// searchItems finds the items of fs whose title contains q or whose number q is ("147" or "#147").
func (s *server) searchItems(ctx context.Context, fs []*config.Factory, q string, limit int) ([]gen.SearchItem, error) {
	byID := make(map[domain.FactoryID]*config.Factory, len(fs))
	ids := make([]domain.FactoryID, 0, len(fs))

	for _, f := range fs {
		id := domain.FactoryID(f.Metadata.ID)
		byID[id], ids = f, append(ids, id)
	}

	number, err := strconv.ParseInt(strings.TrimPrefix(q, "#"), 10, 64)
	if err != nil || number < 1 {
		number = 0
	}

	found, err := s.items.SearchItems(ctx, ports.ItemSearch{Factories: ids, Title: q, Number: number, Limit: limit})
	if err != nil {
		return nil, err
	}

	out := make([]gen.SearchItem, 0, len(found))
	for _, it := range found {
		out = append(out, gen.SearchItem{
			Id: string(it.ID), Number: it.Number, FactoryId: string(it.FactoryID), FactoryName: byID[it.FactoryID].Metadata.Name,
			Title: it.Title, State: gen.ItemState(it.State), Station: it.Station,
		})
	}

	return out, nil
}

// searchAgents finds the agents of fs' current definitions whose name contains q, with the
// stations each works at in line order.
func searchAgents(fs []*config.Factory, q string, limit int) []gen.AgentHit {
	var out []gen.AgentHit

	for _, f := range fs {
		l := f.Line()

		for _, a := range l.Agents {
			if matchRank(a.Name, q) < 0 {
				continue
			}

			stations := []string{}

			for _, st := range l.Stations {
				if slices.Contains(st.LaneAgents(), a.Name) {
					stations = append(stations, st.ID)
				}
			}

			out = append(out, gen.AgentHit{Name: a.Name, Stations: stations, FactoryId: f.Metadata.ID, FactoryName: f.Metadata.Name})
		}
	}

	slices.SortFunc(out, func(a, b gen.AgentHit) int {
		return cmp.Or(cmp.Compare(matchRank(a.Name, q), matchRank(b.Name, q)), cmp.Compare(a.FactoryName, b.FactoryName), cmp.Compare(a.Name, b.Name))
	})

	return firstOf(out, limit)
}

// searchFactories finds those of fs whose name or title contains q.
func searchFactories(fs []*config.Factory, q string, limit int) []gen.FactoryHit {
	var out []gen.FactoryHit

	for _, f := range fs {
		if hit := factoryHit(f); matchRank(hit.Name, q) >= 0 || matchRank(hit.Title, q) >= 0 {
			out = append(out, hit)
		}
	}

	// The better of its name's and its title's matches.
	rank := func(h gen.FactoryHit) int {
		r, t := matchRank(h.Name, q), matchRank(h.Title, q)
		if r < 0 || (t >= 0 && t < r) {
			return t
		}

		return r
	}

	slices.SortFunc(out, func(a, b gen.FactoryHit) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.Name, b.Name))
	})

	return firstOf(out, limit)
}

// searchSecrets finds the organization's secrets whose name contains q, with their scope and
// expiry and never a value, for a caller who holds secret:read; anyone else finds none.
func (s *server) searchSecrets(ctx context.Context, q string, limit int) ([]gen.SecretHit, error) {
	out := []gen.SecretHit{}

	// Without secret:read the group is empty, not a refusal: the search still answers.
	if err := s.ask(ctx, permSecretRead, ""); err != nil {
		return out, nil //nolint:nilerr // a denial empties the group
	}

	secrets, err := s.secrets.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, sec := range secrets {
		if matchRank(sec.Name, q) >= 0 {
			out = append(out, gen.SecretHit{Name: sec.Name, Scope: secretScopeOut(sec.Scope), ExpiresAt: sec.ExpiresAt})
		}
	}

	slices.SortFunc(out, func(a, b gen.SecretHit) int {
		return cmp.Or(cmp.Compare(matchRank(a.Name, q), matchRank(b.Name, q)), cmp.Compare(a.Name, b.Name))
	})

	return firstOf(out, limit), nil
}

// matchRank is how well name matches q, case-insensitively: 0 equal, 1 a prefix, 2 contained,
// -1 not at all.
func matchRank(name, q string) int {
	n, l := strings.ToLower(name), strings.ToLower(q)

	switch {
	case n == l:
		return 0
	case strings.HasPrefix(n, l):
		return 1
	case strings.Contains(n, l):
		return 2
	default:
		return -1
	}
}

// firstOf is at most the first n of hits, never nil.
func firstOf[T any](hits []T, n int) []T {
	if hits == nil {
		return []T{}
	}

	return hits[:min(n, len(hits))]
}
