package main

import (
	"github.com/byFrederick/goal/internal/adapters/codehost/github"
	"github.com/byFrederick/goal/internal/adapters/codehost/gitlab"
	"github.com/byFrederick/goal/internal/core/ports"
)

// codeHosts are the code host adapters by the kind of code host they speak (domain.CodeHostKind).
// Merges and repository tests reach a repository's host through the connection transport: public
// addresses only (egress.PublicDialer), unless allowPrivate lets a laptop's stand-in hosts be
// reached.
func codeHosts(allowPrivate bool) map[string]ports.CodeHost {
	return map[string]ports.CodeHost{
		"github": github.New(github.Config{Transport: connectionTransport(allowPrivate)}),
		"gitlab": gitlab.New(gitlab.Config{Transport: connectionTransport(allowPrivate)}),
	}
}
