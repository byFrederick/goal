package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.SkillSource = (*SkillSource)(nil)

// SkillSource is a skills.sh directory and its repositories in memory. Each skill has commits
// (the last added is the head); Search matches the skill name as a substring.
type SkillSource struct {
	mu       sync.Mutex
	skills   map[string]*sourceSkill // owner/repo/skill → skill
	searchFn func(query string) error
	fetches  []ports.SkillRef
}

type sourceSkill struct {
	ref      ports.SkillRef
	installs int64
	commits  map[string]map[string][]byte
	head     string
	err      error
}

// NewSkillSource returns an empty directory.
func NewSkillSource() *SkillSource {
	return &SkillSource{skills: map[string]*sourceSkill{}}
}

// Add publishes files as commit of owner/repo@skill, which becomes its head.
func (s *SkillSource) Add(ref ports.SkillRef, installs int64, files map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := sourceKey(ref)

	sk, ok := s.skills[k]
	if !ok {
		sk = &sourceSkill{ref: ports.SkillRef{Owner: ref.Owner, Repo: ref.Repo, Skill: ref.Skill}, commits: map[string]map[string][]byte{}}
		s.skills[k] = sk
	}

	tree := make(map[string][]byte, len(files))
	for p, body := range files {
		tree[p] = []byte(body)
	}

	sk.commits[ref.Commit], sk.head, sk.installs = tree, ref.Commit, installs
}

// FailFetch makes every Fetch of owner/repo@skill fail with err.
func (s *SkillSource) FailFetch(ref ports.SkillRef, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sk, ok := s.skills[sourceKey(ref)]; ok {
		sk.err = err
	}
}

// FailSearch makes Search answer fn's error (nil: succeed).
func (s *SkillSource) FailSearch(fn func(query string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.searchFn = fn
}

// Fetches returns the refs Fetch was asked for, in order.
func (s *SkillSource) Fetches() []ports.SkillRef {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.fetches)
}

// Search implements ports.SkillSource.
func (s *SkillSource) Search(_ context.Context, query string, limit int) ([]ports.SkillListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.searchFn != nil {
		if err := s.searchFn(query); err != nil {
			return nil, err
		}
	}

	var out []ports.SkillListing

	for _, sk := range s.skills {
		if strings.Contains(sk.ref.Skill, query) {
			out = append(out, ports.SkillListing{Ref: sk.ref, Installs: sk.installs})
		}
	}

	slices.SortFunc(out, func(a, b ports.SkillListing) int {
		return cmp.Or(cmp.Compare(b.Installs, a.Installs), strings.Compare(sourceKey(a.Ref), sourceKey(b.Ref)))
	})

	return out[:min(limit, len(out))], nil
}

// Fetch implements ports.SkillSource.
func (s *SkillSource) Fetch(_ context.Context, ref ports.SkillRef) (ports.SkillTree, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.fetches = append(s.fetches, ref)

	sk, ok := s.skills[sourceKey(ref)]
	if !ok {
		return ports.SkillTree{}, fmt.Errorf("skill %s: %w", ref, ports.ErrNotFound)
	}

	if sk.err != nil {
		return ports.SkillTree{}, sk.err
	}

	commit := cmp.Or(ref.Commit, sk.head)

	files, ok := sk.commits[commit]
	if !ok {
		return ports.SkillTree{}, fmt.Errorf("skill %s: commit %s: %w", ref, commit, ports.ErrNotFound)
	}

	resolved := sk.ref
	resolved.Commit = commit

	out := make(map[string][]byte, len(files))
	for p, body := range maps.All(files) {
		out[p] = slices.Clone(body)
	}

	return ports.SkillTree{Ref: resolved, Files: out}, nil
}

func sourceKey(r ports.SkillRef) string { return r.Owner + "/" + r.Repo + "/" + r.Skill }
