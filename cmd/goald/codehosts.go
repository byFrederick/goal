package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/adapters/github"
	"github.com/byFrederick/goal/internal/adapters/gitlab"
	"github.com/byFrederick/goal/internal/connkind"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/engine"
	"github.com/byFrederick/goal/internal/ports"
)

// githubKind is the platform kind the repository token is a connection of until merges use the
// organization's connections.
const githubKind = "github"

// codeHosts are the code host adapters by the name connection kinds give them (codeHost).
func codeHosts(s settings) map[string]ports.CodeHost {
	return map[string]ports.CodeHost{
		githubKind: github.New(github.Config{APIURL: s.GitHubAPIURL}),
		"gitlab":   gitlab.New(gitlab.Config{}),
	}
}

// repositoryConnections are the connections a factory's merges use until they are the
// organization's own (spec 4 PR 4-14, which replaces this source and nothing else): the factory
// repository's token (runtime.repository.token, GOAL_GITHUB_TOKEN) as a connection of the
// platform's github kind on the install's GitHub host (GOAL_GITHUB_WEB_URL). A token reference
// the install reserves (policy) is refused before it is resolved, so the code host, though
// trusted with the repository token, never receives goald's own secrets.
func repositoryConnections(secrets ports.Secrets, policy domain.SecretPolicy, host string) engine.MergeConnections {
	platform := sync.OnceValues(connkind.Platform)

	return func(ctx context.Context, l domain.Line) ([]domain.ResolvedConnection, error) {
		ref := l.Runtime.Repository.AuthRef
		if ref == "" {
			return nil, nil
		}

		kinds, err := platform()
		if err != nil {
			return nil, err
		}

		i := slices.IndexFunc(kinds, func(k domain.ConnectionKind) bool { return k.Name == githubKind })
		if i < 0 {
			return nil, errors.New("the platform has no github connection kind")
		}

		kind := kinds[i]

		if policy.ForRepository(ref) == domain.SecretReserved {
			return nil, fmt.Errorf("repository token %s: %w", ref, domain.ErrSecretReserved)
		}

		token, err := secrets.Resolve(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("repository token: %w", err)
		}

		return []domain.ResolvedConnection{{
			Connection: domain.Connection{Name: "repository", Kind: kind.Name}, Kind: kind,
			Values: map[string]string{"token": token, "host": host},
		}}, nil
	}
}
