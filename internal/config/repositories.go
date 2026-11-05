package config

import (
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Repository access and agents' code (ADR 0041 decisions 2 and 3).
const (
	AccessRead  = "read"
	AccessWrite = "write"
	CodeRead    = "read"
	CodeWrite   = "write"
)

// checkRepositories: runtime.repositories, agents' code and what a line that merges needs (ADR
// 0041). Publish refuses it; Load skips it, so a version published before keeps running.
const checkRepositories = "repositories"

// Problem codes of the repositories check.
const (
	// codeRepositoryForms: both runtime.repository and runtime.repositories.
	codeRepositoryForms = "repository-forms-both"
	// codeRepositoryURL: an entry's url is neither one repository nor an owner pattern ending in /*.
	codeRepositoryURL = "repository-url-invalid"
	// codeRepositoryToken: an entry that writes has no token.
	codeRepositoryToken = "repository-token-missing"
	// codeRepositoryDefaults: more than one entry has default.
	codeRepositoryDefaults = "repository-default-many"
	// codeRepositoryDefaultPattern: default on an owner pattern, which names no one repository.
	codeRepositoryDefaultPattern = "repository-default-pattern"
	// codeMergeNeedsWrite: a merge station or integration branches without an entry that writes
	// and an agent whose code is write.
	codeMergeNeedsWrite = "merge-needs-write"
	// codeReaderWriteToken, a warning: an agent that only reads would get a write entry's token,
	// because no read entry matches.
	codeReaderWriteToken = "reader-write-token"
	// codeRepositoryNotSupported: what run start and merges can't do yet (notSupportedYet).
	codeRepositoryNotSupported = "repository-not-supported"
)

// shorthandName names the shorthand's entry when its URL's last segment is not a slug.
const shorthandName = "repo"

// RepositoryEntry is an entry of runtime.repositories: a name (a slug, unique in the factory),
// the URL of one repository or of an owner's every repository (an owner pattern ending in /*),
// the {{secret}} token that opens it (required for write), its access, read or write, and
// whether an item that names no repository uses it.
type RepositoryEntry struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Token   string `json:"token,omitempty"`
	Access  string `json:"access"`
	Default bool   `json:"default,omitempty"`
}

// OwnerPattern reports whether e covers an owner's every repository rather than one.
func (e RepositoryEntry) OwnerPattern() bool { return strings.HasSuffix(e.URL, "/*") }

// RepositoryEntries are the factory's repositories as entries: runtime.repository as one write
// entry, the default, named after its URL's last segment (shorthandName when that is no slug),
// or runtime.repositories, whose only entry is the default when it names one repository. None
// is a factory with no code. A definition with both forms is refused; this reads the shorthand.
func (r Runtime) RepositoryEntries() []RepositoryEntry {
	if s := r.Repository; s != nil {
		return []RepositoryEntry{{Name: ShorthandName(s.URL), URL: s.URL, Token: s.Token, Access: AccessWrite, Default: true}}
	}

	out := slices.Clone(r.Repositories)
	if len(out) == 1 && !out[0].OwnerPattern() {
		out[0].Default = true
	}

	return out
}

// DefaultRepository is the entry an item that names no repository uses; false when none is.
func (r Runtime) DefaultRepository() (RepositoryEntry, bool) {
	entries := r.RepositoryEntries()
	if i := slices.IndexFunc(entries, func(e RepositoryEntry) bool { return e.Default }); i >= 0 {
		return entries[i], true
	}

	return RepositoryEntry{}, false
}

// Writes reports whether the factory has an entry that writes.
func (r Runtime) Writes() bool {
	return slices.ContainsFunc(r.RepositoryEntries(), func(e RepositoryEntry) bool { return e.Access == AccessWrite })
}

// ShorthandName is the name of runtime.repository's entry: its URL's last segment, in lower case
// and without .git, when that is a slug, else shorthandName.
func ShorthandName(raw string) string {
	p := raw
	if u, err := url.Parse(raw); err == nil {
		p = u.Path
	}

	name := strings.TrimSuffix(strings.ToLower(path.Base(strings.TrimRight(p, "/"))), ".git")
	if slugName.MatchString(name) {
		return name
	}

	return shorthandName
}

// AgentCode is a's code: what it says, else write when the factory has an entry that writes and
// read otherwise, so definitions written before code keep writing.
func (f *Factory) AgentCode(a Agent) string {
	if a.Code != "" {
		return a.Code
	}

	if f.Spec.Runtime.Writes() {
		return CodeWrite
	}

	return CodeRead
}

// repoURL is a repository entry's URL as matching reads it: its host and owner path in lower
// case, and the repository's name unless it is an owner pattern.
type repoURL struct {
	owner   string // host/owner, with every group of a nested owner
	name    string // "" for an owner pattern
	pattern bool
}

// parseRepoURL reads an https URL of one repository (https://host/owner/name, .git optional) or
// of an owner pattern (https://host/owner/*); false for anything else.
func parseRepoURL(raw string) (repoURL, bool) {
	u, ok := plainHTTPS(raw)
	if !ok {
		return repoURL{}, false
	}

	segs := strings.Split(strings.Trim(strings.ToLower(u.Path), "/"), "/")
	pattern := segs[len(segs)-1] == "*"

	if pattern {
		segs = segs[:len(segs)-1]
	} else {
		segs[len(segs)-1] = strings.TrimSuffix(segs[len(segs)-1], ".git")
	}

	if len(segs) == 0 || !pattern && len(segs) < 2 || slices.ContainsFunc(segs, func(s string) bool { return s == "" || strings.Contains(s, "*") }) {
		return repoURL{}, false
	}

	host := strings.ToLower(u.Host)
	if pattern {
		return repoURL{owner: host + "/" + strings.Join(segs, "/"), pattern: true}, true
	}

	return repoURL{owner: host + "/" + strings.Join(segs[:len(segs)-1], "/"), name: segs[len(segs)-1]}, true
}

// plainHTTPS parses raw as an https URL with a host and no credentials, query or fragment.
func plainHTTPS(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, false
	}

	return u, true
}

// repositories checks runtime.repositories, agents' code and what a line that merges needs.
func (c *checker) repositories() {
	rt := c.f.Spec.Runtime
	base := at("spec", keyRuntime, "repositories")

	if rt.Repository != nil && len(rt.Repositories) > 0 {
		c.errorf(checkRepositories, codeRepositoryForms, base,
			"runtime: set runtime.repository or runtime.repositories, not both; runtime.repository is the shorthand for one entry that writes")
	}

	defaults := 0

	for i, e := range rt.Repositories {
		p := at("spec", keyRuntime, "repositories", i)
		field := func(k string) []string { return append(slices.Clone(p), k) }

		if slices.ContainsFunc(rt.Repositories[:i], func(o RepositoryEntry) bool { return o.Name == e.Name }) {
			c.errorf(checkReferences, codeDuplicateName, field(keyName), "repository %s: the name is used by another repository", e.Name)
		}

		if _, ok := parseRepoURL(e.URL); !ok {
			c.errorf(checkRepositories, codeRepositoryURL, field("url"),
				"repository %s: %q is neither one repository, such as https://github.com/acme/app, nor an owner's every repository, "+
					"such as https://github.com/acme/*", e.Name, e.URL)
		}

		if e.Access == AccessWrite && strings.TrimSpace(e.Token) == "" {
			c.errorf(checkRepositories, codeRepositoryToken, field("token"),
				"repository %s: access write needs a token, the {{secret}} that pushes and merges", e.Name)
		}

		if !e.Default {
			continue
		}

		defaults++

		switch {
		case defaults > 1:
			c.errorf(checkRepositories, codeRepositoryDefaults, field("default"),
				"repository %s: another repository is the default already; at most one entry has default", e.Name)
		case e.OwnerPattern():
			c.errorf(checkRepositories, codeRepositoryDefaultPattern, field("default"),
				"repository %s: the default must name one repository, and %s is an owner's every repository", e.Name, e.URL)
		}
	}

	c.mergesNeedWrite()
	c.readerTokens()
	c.notSupportedYet()
}

// mergesNeedWrite reports each merge station, and integration branches, on a line without an
// entry that writes and an agent whose code is write: nothing could push or merge.
func (c *checker) mergesNeedWrite() {
	writer := slices.ContainsFunc(c.f.Spec.Agents, func(a Agent) bool { return c.f.AgentCode(a) == CodeWrite })
	if c.f.Spec.Runtime.Writes() && writer {
		return
	}

	const why = "needs a repository entry that writes and an agent whose code is write"

	for i, s := range c.f.Spec.Board.Stations {
		if s.Action == ActionMerge {
			c.errorf(checkRepositories, codeMergeNeedsWrite, at("spec", keyBoard, keyStations, i, "action"), "station %s: action merge %s", s.ID, why)
		}
	}

	if fl := c.f.Spec.Board.Flow; fl != nil && fl.Assemblies != nil && fl.Assemblies.Integration != nil &&
		fl.Assemblies.Integration.Mode == string(domain.IntegrationModeBranch) {
		c.errorf(checkRepositories, codeMergeNeedsWrite, at("spec", keyBoard, "flow", "assemblies", "integration", "mode"),
			"flow.assemblies.integration.mode branch %s", why)
	}
}

// readerTokens warns, for each agent that only reads, about each write entry no read entry
// matches: the agent would get that entry's token, which can push.
func (c *checker) readerTokens() {
	entries := c.f.Spec.Runtime.RepositoryEntries()
	picks := lineEntries(entries)

	for i, a := range c.f.Spec.Agents {
		if c.f.AgentCode(a) != CodeRead {
			continue
		}

		for _, e := range entries {
			if _, ok := parseRepoURL(e.URL); !ok || e.Access != AccessWrite {
				continue
			}

			if got, ok := domain.TokenEntry(picks, e.URL, true); ok && got.Access == AccessWrite {
				c.warnf(checkRepositories, codeReaderWriteToken, at("spec", keyAgents, i, "code"),
					"%s only reads, but the only token for %s can write.", a.Name, e.Name)
			}
		}
	}
}

// notSupportedYet refuses a second repository entry, access read and code read: run start and
// merges work on one repository that writes until part 4-merges (software#720) lands, and that
// part removes this function and its call (ADR 0041 decision 19).
func (c *checker) notSupportedYet() {
	const why = "is not supported yet: runs work on one repository that writes until Goal can start and merge them in several (ADR 0041)"

	for i, e := range c.f.Spec.Runtime.Repositories {
		switch {
		case i > 0:
			c.errorf(checkRepositories, codeRepositoryNotSupported, at("spec", keyRuntime, "repositories", i),
				"repository %s: a second repository %s", e.Name, why)
		case e.Access == AccessRead:
			c.errorf(checkRepositories, codeRepositoryNotSupported, at("spec", keyRuntime, "repositories", i, "access"),
				"repository %s: access read %s", e.Name, why)
		}
	}

	for i, a := range c.f.Spec.Agents {
		if a.Code == CodeRead {
			c.errorf(checkRepositories, codeRepositoryNotSupported, at("spec", keyAgents, i, "code"),
				"agent %s: code read %s", a.Name, why)
		}
	}
}
