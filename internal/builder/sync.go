package builder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var refPattern = regexp.MustCompile(`^(?:skills\.sh:)?([^/@#]+)/([^/@#]+)@([^/@#]+)(?:#([^/@#]+))?$`)

// ParseRef reads owner/repo@skill[#commit] (optionally prefixed skills.sh:), the form of
// skills[].origin and of the sync's sources file. The commit, when given, is a full 40-digit
// lowercase hex SHA.
func ParseRef(s string) (ports.SkillRef, error) {
	m := refPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return ports.SkillRef{}, fmt.Errorf("%q is not owner/repo@skill[#commit]", s)
	}

	ref := ports.SkillRef{Owner: m[1], Repo: m[2], Skill: m[3], Commit: m[4]}

	if !(SkillKey{Owner: ref.Owner, Repo: ref.Repo, Skill: ref.Skill}).Valid() {
		return ports.SkillRef{}, fmt.Errorf("%q: owner, repository and skill are letters, digits, dots, dashes and underscores", s)
	}

	if ref.Commit != "" && !commitPattern.MatchString(ref.Commit) {
		return ports.SkillRef{}, fmt.Errorf("%q: the commit is a full 40-digit lowercase hex SHA", s)
	}

	return ref, nil
}

// SyncStatus is what a sync did with one skill.
type SyncStatus string

// The outcomes of one skill's sync.
const (
	// SyncAdded: the skill's folder at that commit was written, or the index entry changed.
	SyncAdded SyncStatus = "added"
	// SyncUnchanged: the catalog already had the skill at that commit.
	SyncUnchanged SyncStatus = "unchanged"
	// SyncFailed: nothing was written for it; Err says why.
	SyncFailed SyncStatus = "failed"
)

// SyncResult is one line of a sync's report. Key is owner/repo/skill, or search:<term> for a
// search that failed.
type SyncResult struct {
	Key    string
	Commit string
	Status SyncStatus
	Err    error
}

// SyncReport is what a sync did: one result per skill (and failed search), in order, and how
// many skills the index lists afterwards.
type SyncReport struct {
	Results []SyncResult
	Skills  int
}

// Failed counts the failed results.
func (r SyncReport) Failed() int {
	n := 0

	for _, res := range r.Results {
		if res.Status == SyncFailed {
			n++
		}
	}

	return n
}

// SyncSkills fetches each ref, plus the top results of each search term, through src and
// stores them in the catalog: a commit's folder is written only where absent (files already
// there must equal the fetched ones, else the skill fails), then index.json is replaced with
// one Put when it changed. A skill that fails keeps its previous entry; nothing is ever
// deleted. Everything fetched is checked again here: the limits, the path rules, SKILL.md at
// the root and the commit asked for. The error is for the index only (unreadable, malformed or
// not written); each skill's failure is in the report. It is the operator's command (`goal
// catalog skills sync`), using platform bucket credentials.
func (s *Service) SyncSkills(ctx context.Context, src ports.SkillSource, refs []ports.SkillRef, search []string,
	top int,
) (SyncReport, error) {
	if len(refs) == 0 && len(search) == 0 {
		return SyncReport{}, errNoSources
	}

	skills, before, err := s.readIndexForWrite(ctx)
	if err != nil {
		return SyncReport{}, fmt.Errorf("%w: %w", domain.ErrCatalogUnavailable, err)
	}

	todo, installs, report := collect(ctx, src, refs, search, top)
	seen := map[string]bool{}

	for _, ref := range todo {
		key := keyOf(ref).String()
		if seen[key] {
			continue
		}

		seen[key] = true

		sk, status, err := s.syncOne(ctx, src, ref)
		if err != nil {
			report.Results = append(report.Results, SyncResult{Key: key, Commit: ref.Commit, Status: SyncFailed, Err: err})
			continue
		}

		n, listed := installs[key]
		status = merge(skills, sk, status, n, listed)
		report.Results = append(report.Results, SyncResult{Key: key, Commit: sk.Commit, Status: status})
	}

	report.Skills = len(skills)

	return report, s.writeIndex(ctx, skills, before)
}

// collect lists what a sync fetches: refs, then the top listings of each search term (with
// their installs), and the searches that failed.
func collect(ctx context.Context, src ports.SkillSource, refs []ports.SkillRef, search []string, top int) (
	[]ports.SkillRef, map[string]int64, SyncReport,
) {
	var report SyncReport

	todo, installs := slices.Clone(refs), map[string]int64{}

	for _, term := range search {
		listings, err := src.Search(ctx, term, top)
		if err != nil {
			report.Results = append(report.Results, SyncResult{Key: "search:" + term, Status: SyncFailed, Err: err})
			continue
		}

		for _, l := range listings {
			todo = append(todo, l.Ref)
			installs[keyOf(l.Ref).String()] = l.Installs
		}
	}

	return todo, installs, report
}

// merge puts a synced skill into the index: its installs from the directory when listed, else
// the previous entry's; the previous sync time when nothing changed. A changed entry is added.
func merge(skills map[string]CatalogSkill, sk CatalogSkill, status SyncStatus, installs int64, listed bool) SyncStatus {
	prev, had := skills[sk.String()]

	sk.Installs = prev.Installs
	if listed {
		sk.Installs = installs
	}

	if had && prev.Commit == sk.Commit && prev.Hash == sk.Hash {
		sk.SyncedAt = prev.SyncedAt
	}

	if !had || prev != sk {
		status = SyncAdded
	}

	skills[sk.String()] = sk

	return status
}

func keyOf(r ports.SkillRef) SkillKey { return SkillKey{Owner: r.Owner, Repo: r.Repo, Skill: r.Skill} }

// syncOne fetches one skill, checks it and stores its folder where absent.
func (s *Service) syncOne(ctx context.Context, src ports.SkillSource, ref ports.SkillRef) (CatalogSkill, SyncStatus, error) {
	key := keyOf(ref)
	if !key.Valid() || (ref.Commit != "" && !commitPattern.MatchString(ref.Commit)) {
		return CatalogSkill{}, "", fmt.Errorf("%s is not a valid skill reference", ref)
	}

	tree, err := src.Fetch(ctx, ref)
	if err != nil {
		return CatalogSkill{}, "", err
	}

	switch got := tree.Ref; {
	case keyOf(got) != key:
		return CatalogSkill{}, "", fmt.Errorf("the source answered %s for %s", got, ref)
	case !commitPattern.MatchString(got.Commit), ref.Commit != "" && got.Commit != ref.Commit:
		return CatalogSkill{}, "", fmt.Errorf("the source answered commit %q for %s", got.Commit, ref)
	}

	total, err := checkTree(tree.Files)
	if err != nil {
		return CatalogSkill{}, "", err
	}

	written, err := s.storeFolder(ctx, folder(key, tree.Ref.Commit), tree.Files)
	if err != nil {
		return CatalogSkill{}, "", err
	}

	status := SyncUnchanged
	if written {
		status = SyncAdded
	}

	return CatalogSkill{
		SkillKey: key, Commit: tree.Ref.Commit, Description: describe(tree.Files[domain.SkillFile]),
		Files: len(tree.Files), Bytes: total, Hash: treeHash(tree.Files), SyncedAt: s.clock.Now().UTC(),
	}, status, nil
}

// storeFolder writes the files missing under prefix. Files already there must equal the given
// ones, and nothing else may be there: a commit's folder is written once and never changed.
func (s *Service) storeFolder(ctx context.Context, prefix string, files map[string][]byte) (bool, error) {
	infos, err := s.objects.List(ctx, prefix)
	if err != nil {
		return false, fmt.Errorf("%w: %w", domain.ErrCatalogUnavailable, err)
	}

	stored := map[string]ports.ObjectInfo{}

	for _, info := range infos {
		p := strings.TrimPrefix(info.Key, prefix)
		if _, ok := files[p]; !ok {
			return false, fmt.Errorf("the catalog's folder %s holds %s, which the skill does not have", prefix, p)
		}

		stored[p] = info
	}

	written := false

	for _, p := range slices.Sorted(maps.Keys(files)) {
		info, ok := stored[p]
		if !ok {
			// Written only where absent, so a concurrent sync never overwrites either.
			var created bool

			if info, created, err = s.objects.PutIfAbsent(ctx, prefix+p, files[p]); err != nil {
				return written, fmt.Errorf("%w: %w", domain.ErrCatalogUnavailable, err)
			}

			if created {
				written = true
				continue
			}
		}

		obj, err := s.objects.Get(ctx, info.Key)
		if err != nil {
			return written, fmt.Errorf("%w: %w", domain.ErrCatalogUnavailable, err)
		}

		if sha256.Sum256(obj.Body) != sha256.Sum256(files[p]) {
			return written, fmt.Errorf("the catalog's %s differs from the skill at the same commit; it is never overwritten", info.Key)
		}
	}

	return written, nil
}

// writeIndex replaces index.json with skills when that changes it.
func (s *Service) writeIndex(ctx context.Context, skills map[string]CatalogSkill, before []byte) error {
	body, err := encodeIndex(skills)
	if err != nil {
		return err
	}

	if bytes.Equal(body, before) || (before == nil && len(skills) == 0) {
		return nil
	}

	if _, err := s.objects.Put(ctx, indexKey, body); err != nil {
		return fmt.Errorf("%w: write %s: %w", domain.ErrCatalogUnavailable, indexKey, err)
	}

	s.mu.Lock()
	s.cache = indexCache{}
	s.mu.Unlock()

	return nil
}

// ListSkills returns the catalog's skills matching q, most installed first, read straight from
// the bucket (the operator's `goal catalog skills list`): a malformed index is an error here.
func (s *Service) ListSkills(ctx context.Context, q string) ([]CatalogSkill, error) {
	skills, _, err := s.readIndexForWrite(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrCatalogUnavailable, err)
	}

	q = strings.ToLower(strings.TrimSpace(q))

	var out []CatalogSkill

	for _, sk := range skills {
		if matches(sk, q) {
			out = append(out, sk)
		}
	}

	sortSkills(out)

	return out, nil
}

// Limits of the description taken from SKILL.md.
const (
	maxFrontMatter  = 64 << 10
	maxDescriptionR = 500
)

// describe takes the description from SKILL.md's YAML front matter, as one line of printable
// text of at most maxDescriptionR characters; "" when there is none.
func describe(skillMD []byte) string {
	text := strings.ReplaceAll(string(skillMD), "\r\n", "\n")

	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return ""
	}

	end := strings.Index(rest, "\n---")
	if end < 0 || end > maxFrontMatter {
		return ""
	}

	var fm struct {
		Description string `yaml:"description"`
	}

	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return ""
	}

	clean := strings.Map(func(r rune) rune {
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			return ' '
		}

		return r
	}, fm.Description)

	clean = strings.Join(strings.Fields(clean), " ")
	if utf8.RuneCountInString(clean) > maxDescriptionR {
		clean = string([]rune(clean)[:maxDescriptionR-1]) + "…"
	}

	return clean
}

// errNoSources marks a sync with nothing to fetch.
var errNoSources = errors.New("nothing to sync: give skills or search terms")
