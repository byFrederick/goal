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

// schemeHTTPS is the only scheme a repository's URL may have.
const schemeHTTPS = "https"

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

// fileRepositories are the repositories a filed item starts with: each of named, resolved
// against l's entries (resolveRepository) and joined filing, once and in the order named, or the
// default entry's when none is named. A value that resolves to no repository, or to more than
// one, and more than MaxItemRepositories are ErrInvalid.
func fileRepositories(l Line, named []string) ([]ItemRepository, error) {
	if len(named) == 0 {
		return defaultRepository(l), nil
	}

	var out []ItemRepository

	for _, raw := range named {
		key, err := resolveRepository(l.Runtime.Repositories, raw)
		if err != nil {
			return nil, err
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

// resolveRepository is the key (RepositoryKey) of the repository raw names among entries, in
// this order: an entry's name, which must be an entry of one repository; an https URL or
// host/owner/name an entry covers; or owner/name (group/sub/name) without a host, matched under
// each entry's host, which must give exactly one key. Every refusal is ErrInvalid and quotes raw.
func resolveRepository(entries []RepositoryEntry, raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if !strings.Contains(name, "/") {
		i := slices.IndexFunc(entries, func(e RepositoryEntry) bool { return e.Name == name })
		if i < 0 {
			return "", fmt.Errorf("repository %q is not an entry of the factory's runtime.repositories: %w", raw, ErrInvalid)
		}

		key, ok := RepositoryKey(entries[i].URL)
		if !ok {
			return "", fmt.Errorf("repository %q is an entry for an owner's repositories; name owner/name instead: %w", raw, ErrInvalid)
		}

		return key, nil
	}

	if key, ok := RepositoryKey(name); ok && coveredBy(entries, key) {
		return key, nil
	}

	if strings.Contains(name, "://") {
		return "", fmt.Errorf("repository %q is not an https://host/owner/name one of the factory's runtime.repositories covers: %w", raw, ErrInvalid)
	}

	return resolveWithoutHost(entries, raw, name)
}

// resolveWithoutHost is the one key that owner/name (name, without a host) has under the hosts
// of entries; ErrInvalid quoting raw when none covers it, or listing the hosts when several do.
func resolveWithoutHost(entries []RepositoryEntry, raw, name string) (string, error) {
	var keys, hosts []string

	for _, e := range entries {
		segs, ok := repositorySegments(e.URL)
		if !ok {
			continue
		}

		key, ok := RepositoryKey(segs[0] + "/" + name)
		if !ok || !e.Covers(key) || slices.Contains(keys, key) {
			continue
		}

		keys, hosts = append(keys, key), append(hosts, segs[0])
	}

	switch len(keys) {
	case 1:
		return keys[0], nil
	case 0:
		return "", fmt.Errorf("repository %q is not one of the factory's runtime.repositories: %w", raw, ErrInvalid)
	default:
		return "", fmt.Errorf("repository %q is on more than one of the factory's hosts (%s); name host/owner/name: %w", raw, strings.Join(hosts, ", "), ErrInvalid)
	}
}

// RepositoryUse is an open item's use of a repository: one of its rows (ItemRepository).
type RepositoryUse struct {
	Repo   string
	Item   ItemID
	Number int64
}

// RemovedRepository is a repository open items use that a new version's entries no longer
// cover, with the numbers of those items in the order of uses.
type RemovedRepository struct {
	Repo  string
	Items []int64
}

// RemovedRepositories are the repositories of uses that an entry of current covers and no entry
// of next does, in the order of uses (ADR 0041 decision 4). Entries match by URL, never by
// name, so an entry renamed with its URL kept, or a repository an owner pattern of next covers,
// removes nothing.
func RemovedRepositories(current, next []RepositoryEntry, uses []RepositoryUse) []RemovedRepository {
	var out []RemovedRepository

	for _, u := range uses {
		if !coveredBy(current, u.Repo) || coveredBy(next, u.Repo) {
			continue
		}

		i := slices.IndexFunc(out, func(r RemovedRepository) bool { return r.Repo == u.Repo })
		if i < 0 {
			out = append(out, RemovedRepository{Repo: u.Repo})
			i = len(out) - 1
		}

		if !slices.Contains(out[i].Items, u.Number) {
			out[i].Items = append(out[i].Items, u.Number)
		}
	}

	return out
}

// coveredBy reports whether one of entries covers key.
func coveredBy(entries []RepositoryEntry, key string) bool {
	return slices.ContainsFunc(entries, func(e RepositoryEntry) bool { return e.Covers(key) })
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
		raw = schemeHTTPS + "://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil || u.Scheme != schemeHTTPS || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
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

// ResolveRepository is the repository value names among l's runtime.repositories entries, as its
// RepositoryKey (ADR 0041 decision 13), in any case and with or without .git: the name of an
// entry of one repository, host/owner/name or an https URL an entry covers, or owner/name (an
// event's {{repo}}) on the host of an entry that covers it there. A webhook records no host, so
// when entries on several hosts cover owner/name, the one on sender's own host (github.com for
// github, gitlab.com for gitlab) wins, else the first in definition order. False for an empty
// value and one no entry covers.
func (l Line) ResolveRepository(value string, sender WebhookSender) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}

	entries := l.repositoryEntries()

	if i := slices.IndexFunc(entries, func(e RepositoryEntry) bool {
		return !e.OwnerPattern() && e.Name != "" && strings.EqualFold(e.Name, value)
	}); i >= 0 {
		return RepositoryKey(entries[i].URL)
	}

	if key, ok := RepositoryKey(value); ok && slices.ContainsFunc(entries, func(e RepositoryEntry) bool { return e.Covers(key) }) {
		return key, true
	}

	if strings.Contains(value, "://") {
		return "", false
	}

	return ownerNameKey(entries, value, sender)
}

// ownerNameKey is owner/name's key on the host of an entry that covers it there: the entry on
// sender's own host when there is one, else the first in definition order.
func ownerNameKey(entries []RepositoryEntry, value string, sender WebhookSender) (string, bool) {
	var found string

	for _, e := range entries {
		segs, ok := repositorySegments(e.URL)
		if !ok {
			continue
		}

		key, ok := RepositoryKey(segs[0] + "/" + strings.Trim(value, "/"))
		if !ok || !e.Covers(key) {
			continue
		}

		if segs[0] == senderHosts[sender] {
			return key, true
		}

		if found == "" {
			found = key
		}
	}

	return found, found != ""
}

// ResolveRepositories are values resolved by ResolveRepository, once each and in order, empty
// values adding none (PagerDuty's and Datadog's events name no repository); or, for the first
// value no entry covers, NotTheFactorysRepository's reason.
func (l Line) ResolveRepositories(values []string, sender WebhookSender) ([]string, string) {
	var keys []string

	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			continue
		}

		key, ok := l.ResolveRepository(v, sender)
		if !ok {
			return nil, NotTheFactorysRepository(strings.TrimSpace(v), l)
		}

		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}

	return keys, ""
}

// repositoryEntries are l's runtime.repositories entries, or its one repository as an entry for
// a line built with only that.
func (l Line) repositoryEntries() []RepositoryEntry {
	if len(l.Runtime.Repositories) == 0 && l.Runtime.Repository.URL != "" {
		return []RepositoryEntry{{URL: l.Runtime.Repository.URL}}
	}

	return l.Runtime.Repositories
}

// senderHosts are the code hosts the code hosts' own senders deliver from.
var senderHosts = map[WebhookSender]string{SenderGitHub: gitHubDotCom, SenderGitLab: gitLabDotCom}
