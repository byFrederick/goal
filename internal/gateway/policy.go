package gateway

import (
	"path"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/config"
)

// sep joins a connection and a tool into the name an agent sees: "github__create_issue".
const sep = "__"

// policy holds an agent's allow and deny patterns for gateway tools, by connection.
type policy struct {
	allow map[string][]string
	deny  map[string][]string
}

// newPolicy keeps the agent's tools.allow and tools.deny entries that address a factory
// connection ("<connection>__<pattern>"); the rest are worker tool rules.
func newPolicy(conns []config.Connection, a *config.Agent) policy {
	p := policy{allow: map[string][]string{}, deny: map[string][]string{}}
	if a.Tools == nil {
		return p
	}

	add := func(into map[string][]string, entries []string) {
		for _, e := range entries {
			conn, pattern, ok := strings.Cut(e, sep)
			if ok && slices.ContainsFunc(conns, func(c config.Connection) bool { return c.Name == conn }) {
				into[conn] = append(into[conn], pattern)
			}
		}
	}

	add(p.allow, a.Tools.Allow)
	add(p.deny, a.Tools.Deny)

	return p
}

// permits reports whether the agent may see and call tool on conn. Deny wins; a malformed
// deny pattern denies everything on its connection rather than nothing.
func (p policy) permits(conn, tool string) bool {
	if matches(p.deny[conn], tool, true) {
		return false
	}

	allow, restricted := p.allow[conn]

	return !restricted || matches(allow, tool, false)
}

func matches(patterns []string, tool string, onBadPattern bool) bool {
	for _, pat := range patterns {
		ok, err := path.Match(pat, tool)
		if err != nil {
			ok = onBadPattern
		}

		if ok {
			return true
		}
	}

	return false
}
