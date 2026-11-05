package domain

import (
	"strings"
)

// Repository access and agents' code (ADR 0041 decisions 2 and 3); the values the definition
// uses.
const (
	AccessRead  = "read"
	AccessWrite = "write"
	CodeRead    = "read"
	CodeWrite   = "write"
)

// WorkspacePath is where a run's first repository is cloned, and its working directory; each
// other repository is cloned under ReposPath at its owner/name (ADR 0041 decision 5).
const (
	WorkspacePath = "/workspace"
	ReposPath     = "/repos"
)

// repositoryEntryTokenPrefix starts the variable each picked entry's token goes in for prepare.
const repositoryEntryTokenPrefix = RepositoryTokenEnv + "_"

// RunCode is what a station run gets of the item's repositories and the factory's entries at
// start (ADR 0041 decisions 3 and 5): what prepare clones, the logins git uses and the URL
// prefixes the run may not write.
type RunCode struct {
	// Clones are the item's repositories in the order they joined, the first at WorkspacePath.
	Clones []RunClone
	// Logins are one per entry of the factory that has a token, in definition order.
	Logins []RunLogin
	// ReadOnly are the URL prefixes the run may not write, in definition order: every read
	// entry's, and every entry's for an agent that only reads.
	ReadOnly []string
}

// RunClone is one of the item's repositories: its key (RepositoryKey), the https URL prepare
// clones, where, the {{secret}} reference of the token the agent's access picks for it
// (TokenEntry; "" when no entry with a token covers it), and whether the agent may push to it.
type RunClone struct {
	Repo, URL, Path, Token string
	Write                  bool
}

// RunLogin is the git login of one entry: its URL prefix, the entry's name, the entry whose token
// the agent's access picks (TokenEntry) and that token's {{secret}} reference, and whether the
// run may write under the prefix.
type RunLogin struct {
	Prefix, Entry, TokenEntry, Token string
	Write                            bool
}

// RunCode is what a run of agent gets of repos, the item's repositories as
// ResolveItemRepositories gives them, under l's entries. An agent whose Code is empty writes, as
// every agent did before code; a check's caller reads alone (software#746).
func (l Line) RunCode(agent AgentSpec, repos ItemRepositories) RunCode {
	entries := l.Runtime.Repositories
	reads := agent.Code == CodeRead

	var out RunCode

	for i, r := range repos {
		c := RunClone{Repo: r.Repo, URL: schemeHTTPS + "://" + r.Repo, Path: WorkspacePath}
		if i > 0 {
			c.Path = ReposPath + "/" + ownerName(r.Repo)
		}

		if e, ok := TokenEntry(entries, r.Repo, false); ok {
			c.Write = !reads && e.Access == AccessWrite
		}

		if e, ok := TokenEntry(entries, r.Repo, reads); ok {
			c.Token = e.Token
		}

		out.Clones = append(out.Clones, c)
	}

	for _, e := range entries {
		prefix, ok := entryPrefix(e.URL)
		if !ok {
			continue
		}

		write := !reads && e.Access == AccessWrite
		if !write {
			out.ReadOnly = append(out.ReadOnly, prefix)
		}

		pick, ok := TokenEntry(entries, e.URL, reads)
		if !ok || pick.Token == "" {
			continue
		}

		out.Logins = append(out.Logins, RunLogin{Prefix: prefix, Entry: e.Name, TokenEntry: pick.Name, Token: pick.Token, Write: write})
	}

	return out
}

// RepositoryEntryTokenEnv is the variable goal-agent prepare gets the token of the entry named
// name in: RepositoryTokenEnv, _ and the name upper-cased, each character other than a letter or
// digit turned into _. GOAL_* is reserved (ReservedRunEnv), so no environment file sets it.
func RepositoryEntryTokenEnv(name string) string {
	return repositoryEntryTokenPrefix + strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, name)
}

// TokenEntry is the entry whose token a run uses for the repositories target names (an entry's
// URL, an https URL or a RepositoryKey): the most specific entry that covers them, one repository
// before an owner pattern and a deeper owner before the one it is in, and, when reads, the most
// specific read entry when one does (ADR 0041 decision 3). The first in definition order wins a
// tie; false when no entry covers them. The publish warning on readers' tokens uses it too.
func TokenEntry(entries []RepositoryEntry, target string, reads bool) (RepositoryEntry, bool) {
	t, ok := parseRepoTarget(target)
	if !ok {
		return RepositoryEntry{}, false
	}

	var (
		best  RepositoryEntry
		score = -1
		found bool
	)

	for _, e := range entries {
		u, ok := parseRepoTarget(e.URL)
		if !ok || !u.covers(t) {
			continue
		}

		s := u.specificity()
		if reads && e.Access == AccessRead {
			s += 10000 // a read entry before any write entry
		}

		if s > score {
			best, score, found = e, s, true
		}
	}

	return best, found
}

// repoTarget is one repository or an owner pattern as matching reads it: its host and owner
// path in lower case, and the repository's name unless it is a pattern.
type repoTarget struct {
	owner   string // host/owner, with every group of a nested owner
	name    string // "" for an owner pattern
	pattern bool
}

// parseRepoTarget reads raw as one repository (https://host/owner/name, .git optional, or
// host/owner/name) or an owner pattern (https://host/owner/*); false for anything else.
func parseRepoTarget(raw string) (repoTarget, bool) {
	segs, ok := repositorySegments(raw)
	if !ok || len(segs) < 3 {
		return repoTarget{}, false
	}

	last := segs[len(segs)-1]
	owner := strings.Join(segs[:len(segs)-1], "/")

	if last == "*" {
		return repoTarget{owner: owner, pattern: true}, true
	}

	if last = strings.TrimSuffix(last, ".git"); last == "" || strings.Contains(last, "*") {
		return repoTarget{}, false
	}

	return repoTarget{owner: owner, name: last}, true
}

// covers reports whether e covers every repository t names: one repository covers itself, an
// owner pattern every repository and pattern below its owner.
func (e repoTarget) covers(t repoTarget) bool {
	if !e.pattern {
		return !t.pattern && e == t
	}

	return t.owner == e.owner || strings.HasPrefix(t.owner, e.owner+"/")
}

// specificity orders the entries that match: one repository before an owner pattern, and a
// deeper owner before the one it is in.
func (e repoTarget) specificity() int {
	n := strings.Count(e.owner, "/")
	if !e.pattern {
		n += 1000
	}

	return n
}

// entryPrefix is the URL prefix an entry's URL covers: https://host/owner/name for one
// repository, https://host/owner/ for an owner pattern, in lower case and without .git.
func entryPrefix(raw string) (string, bool) {
	t, ok := parseRepoTarget(raw)
	if !ok {
		return "", false
	}

	if t.pattern {
		return schemeHTTPS + "://" + t.owner + "/", true
	}

	return schemeHTTPS + "://" + t.owner + "/" + t.name, true
}

// ownerName is key's path without its host: owner/name, with every group of a nested owner.
func ownerName(key string) string {
	_, p, _ := strings.Cut(key, "/")
	return p
}
