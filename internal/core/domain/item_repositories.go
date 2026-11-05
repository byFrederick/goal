package domain

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// MaxItemRepositories is how many repositories one item may have (ADR 0041 decision 4).
const MaxItemRepositories = 10

// RepositoryJoin is how a repository joined an item. Values match work_item_repositories.joined.
type RepositoryJoin string

// Ways a repository joins an item.
const (
	// JoinedFiling is a repository named when the item was filed.
	JoinedFiling RepositoryJoin = "filing"
	// JoinedDefault is the default entry's repository, for an item filed naming none.
	JoinedDefault RepositoryJoin = "default"
	// JoinedPush is a repository an agent pushed to.
	JoinedPush RepositoryJoin = "push"
	// JoinedOutput is a repository a station's output named.
	JoinedOutput RepositoryJoin = "output"
)

// ItemRepository is one repository of an item: Repo is its normalized host/owner/name in lower
// case (RepositoryKey), which entries are matched against, so renaming or reordering
// runtime.repositories never moves an item. Rows are never removed.
type ItemRepository struct {
	Repo     string
	Joined   RepositoryJoin
	JoinedAt time.Time
	// Ordinal keeps the order repositories joined in, 0 for the first.
	Ordinal int
	// BaseCommit and IntegrationBranch are the repository's, "" until set.
	BaseCommit        string
	IntegrationBranch string
}

// ItemRepositories are an item's repositories in the order they joined: those named at filing in
// the order named, then the rest by when they joined.
type ItemRepositories []ItemRepository

// First is the item's first repository (ADR 0041 decision 4): the first named at filing, else
// the default entry's, else the first that joined; false when the item has none.
func (rs ItemRepositories) First() (ItemRepository, bool) {
	for _, j := range []RepositoryJoin{JoinedFiling, JoinedDefault} {
		if i := slices.IndexFunc(rs, func(r ItemRepository) bool { return r.Joined == j }); i >= 0 {
			return rs[i], true
		}
	}

	if len(rs) == 0 {
		return ItemRepository{}, false
	}

	return rs[0], true
}

// ResolveItemRepositories are an item's repositories at run start: its rows, or, for an item
// with none (filed before repositories, or by an old instance during a deploy), the line's
// default entry's repository joined default, for good; none without a default entry.
func ResolveItemRepositories(rows []ItemRepository, l Line) ItemRepositories {
	if len(rows) > 0 {
		return ItemRepositories(rows)
	}

	return ItemRepositories(defaultRepository(l))
}

// defaultRepository is the default entry's repository joined default, or none.
func defaultRepository(l Line) []ItemRepository {
	e, ok := l.Runtime.DefaultEntry()
	if !ok {
		return nil
	}

	key, ok := RepositoryKey(e.URL)
	if !ok {
		return nil
	}

	return []ItemRepository{{Repo: key, Joined: JoinedDefault}}
}

// fileRepositories are the repositories a filed item starts with: each of named, matched
// against l's entries and joined filing, once and in the order named, or the default entry's
// when none is named. A repository no entry covers, one that is not host/owner/name and more
// than MaxItemRepositories are ErrInvalid.
func fileRepositories(l Line, named []string) ([]ItemRepository, error) {
	if len(named) == 0 {
		return defaultRepository(l), nil
	}

	var out []ItemRepository

	for _, raw := range named {
		key, ok := RepositoryKey(raw)
		if !ok {
			return nil, fmt.Errorf("repository %q is not https://host/owner/name or host/owner/name: %w", raw, ErrInvalid)
		}

		if !slices.ContainsFunc(l.Runtime.Repositories, func(e RepositoryEntry) bool { return e.Covers(key) }) {
			return nil, fmt.Errorf("repository %s is not one of the factory's runtime.repositories: %w", key, ErrInvalid)
		}

		if !slices.ContainsFunc(out, func(r ItemRepository) bool { return r.Repo == key }) {
			out = append(out, ItemRepository{Repo: key, Joined: JoinedFiling, Ordinal: len(out)})
		}
	}

	if len(out) > MaxItemRepositories {
		return nil, fmt.Errorf("%d repositories, more than an item's %d: %w", len(out), MaxItemRepositories, ErrInvalid)
	}

	return out, nil
}

// RepositoryKey is one repository's normalized host/owner/name in lower case, without .git:
// raw is an https URL without credentials, query or fragment, or host/owner/name; a nested
// owner keeps every group. False for anything else, an owner pattern included.
func RepositoryKey(raw string) (string, bool) {
	segs, ok := repositorySegments(raw)
	if !ok || len(segs) < 3 || segs[len(segs)-1] == "*" {
		return "", false
	}

	segs[len(segs)-1] = strings.TrimSuffix(segs[len(segs)-1], ".git")
	if segs[len(segs)-1] == "" {
		return "", false
	}

	return strings.Join(segs, "/"), true
}

// repositorySegments are raw's host and path segments in lower case, false when raw is not a
// plain https URL (or one without its scheme) or has an empty or wildcard segment before its last.
func repositorySegments(raw string) ([]string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, false
	}

	segs := append([]string{strings.ToLower(u.Host)}, strings.Split(strings.Trim(strings.ToLower(u.Path), "/"), "/")...)
	for _, s := range segs[:len(segs)-1] {
		if s == "" || strings.Contains(s, "*") {
			return nil, false
		}
	}

	return segs, true
}

// Covers reports whether e covers the repository key names (RepositoryKey): an entry of one
// repository covers itself, an owner pattern every repository below its owner, nested owners
// included. Case and .git are ignored.
func (e RepositoryEntry) Covers(key string) bool {
	if !e.OwnerPattern() {
		k, ok := RepositoryKey(e.URL)
		return ok && k == key
	}

	segs, ok := repositorySegments(e.URL)
	if !ok || len(segs) < 3 {
		return false
	}

	return strings.HasPrefix(key, strings.Join(segs[:len(segs)-1], "/")+"/")
}

// OwnerPattern reports whether e covers an owner's every repository rather than one.
func (e RepositoryEntry) OwnerPattern() bool { return strings.HasSuffix(e.URL, "/*") }
