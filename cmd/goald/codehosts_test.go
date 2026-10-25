package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// Until merges use the organization's connections, the repository token is a github connection
// on the install's GitHub host, which the github code host can use.
func TestRepositoryConnections_TheRepositoryTokenOnTheInstallsHost(t *testing.T) {
	t.Parallel()

	const token = "ghp_repository-token" //nolint:gosec // a made-up token

	open := repositoryConnections(porttest.Secrets{"GOAL_GITHUB_TOKEN": token}, domain.SecretPolicy{}, "ghe.example.com")

	line := domain.Line{Runtime: domain.Runtime{Repository: domain.Repository{
		URL: "https://ghe.example.com/acme/app.git", AuthRef: "env:GOAL_GITHUB_TOKEN",
	}}}

	conns, err := open(t.Context(), line)
	if err != nil || len(conns) != 1 {
		t.Fatalf("connections = %+v, %v; want one", conns, err)
	}

	if host, password, err := conns[0].GitLogin(); err != nil || host != "ghe.example.com" || password != token ||
		codeHosts(settings{})[conns[0].Kind.Spec.CodeHost] == nil {
		t.Errorf("connection's code host %q, git login %q (%v); want a code host goald has, for ghe.example.com with the token",
			conns[0].Kind.Spec.CodeHost, host, err)
	}

	// A factory without a repository token has no connection to merge with.
	if conns, err := open(t.Context(), domain.Line{}); err != nil || len(conns) != 0 {
		t.Errorf("connections without a token = %+v, %v; want none", conns, err)
	}
}

// A version published before a variable was reserved may still name it as the repository's token:
// the install's policy refuses it before it is resolved, so the code host never receives goald's
// own secrets.
func TestRepositoryConnections_RefusesAReservedToken(t *testing.T) {
	t.Parallel()

	const secret = "gateway-key-never-sent"

	open := repositoryConnections(porttest.Secrets{"GOAL_GATEWAY_KEY": secret},
		domain.SecretPolicy{Reserved: []string{"GOAL_GATEWAY_KEY"}}, "github.com")

	line := domain.Line{Runtime: domain.Runtime{Repository: domain.Repository{URL: "https://github.com/acme/app.git", AuthRef: "env:GOAL_GATEWAY_KEY"}}}

	conns, err := open(t.Context(), line)
	if !errors.Is(err, domain.ErrSecretReserved) || len(conns) != 0 || strings.Contains(err.Error(), secret) {
		t.Errorf("connections = %+v, %v; want the reserved token refused", conns, err)
	}
}
