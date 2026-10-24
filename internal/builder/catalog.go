package builder

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// The catalog in the bucket (2.2-D §7).
const (
	catalogRoot = "catalog/skills/"
	indexKey    = catalogRoot + "index.json"
	// indexTTL is how long a parsed index is used before the bucket is asked whether it changed.
	indexTTL = 5 * time.Minute
	// maxIndexBytes bounds the index read; a larger one is treated as malformed.
	maxIndexBytes = 16 << 20
)

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	hashPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

	// errBadTree marks a skill folder that breaks the path rules.
	errBadTree = errors.New("invalid skill folder")
	// errBadIndex marks an index.json that does not parse or holds an invalid entry.
	errBadIndex = errors.New("malformed skills index")
)

// indexEntry is one skill in index.json.
type indexEntry struct {
	Key         string    `json:"key"`
	Owner       string    `json:"owner"`
	Repo        string    `json:"repo"`
	Skill       string    `json:"skill"`
	Commit      string    `json:"commit"`
	Description string    `json:"description"`
	Installs    int64     `json:"installs"`
	Files       int       `json:"files"`
	Bytes       int64     `json:"bytes"`
	Hash        string    `json:"hash"`
	SyncedAt    time.Time `json:"synced_at"`
}

func entryOf(s CatalogSkill) indexEntry {
	return indexEntry{
		Key: s.String(), Owner: s.Owner, Repo: s.Repo, Skill: s.Skill, Commit: s.Commit, Description: s.Description,
		Installs: s.Installs, Files: s.Files, Bytes: s.Bytes, Hash: s.Hash, SyncedAt: s.SyncedAt.UTC(),
	}
}

func (e indexEntry) skill() (CatalogSkill, error) {
	s := CatalogSkill{
		SkillKey: SkillKey{Owner: e.Owner, Repo: e.Repo, Skill: e.Skill}, Commit: e.Commit, Description: e.Description,
		Installs: e.Installs, Files: e.Files, Bytes: e.Bytes, Hash: e.Hash, SyncedAt: e.SyncedAt,
	}

	switch {
	case !s.Valid() || e.Key != s.String():
		return s, fmt.Errorf("%w: entry %q has an invalid key", errBadIndex, e.Key)
	case !commitPattern.MatchString(e.Commit) || !hashPattern.MatchString(e.Hash):
		return s, fmt.Errorf("%w: entry %s has an invalid commit or hash", errBadIndex, e.Key)
	case e.Files < 1 || e.Files > domain.MaxSkillFiles || e.Bytes < 0 || e.Bytes > domain.MaxSkillBytes || e.Installs < 0:
		return s, fmt.Errorf("%w: entry %s is beyond the skill limits", errBadIndex, e.Key)
	}

	return s, nil
}

// parseIndex reads index.json, refusing it whole when any entry is invalid or repeated.
func parseIndex(body []byte) ([]CatalogSkill, error) {
	var entries []indexEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("%w: %w", errBadIndex, err)
	}

	out := make([]CatalogSkill, 0, len(entries))
	seen := make(map[string]bool, len(entries))

	for _, e := range entries {
		s, err := e.skill()
		if err != nil {
			return nil, err
		}

		if seen[e.Key] {
			return nil, fmt.Errorf("%w: entry %s is repeated", errBadIndex, e.Key)
		}

		seen[e.Key] = true

		out = append(out, s)
	}

	sortSkills(out)

	return out, nil
}

func encodeIndex(skills map[string]CatalogSkill) ([]byte, error) {
	entries := make([]indexEntry, 0, len(skills))
	for _, k := range slices.Sorted(maps.Keys(skills)) {
		entries = append(entries, entryOf(skills[k]))
	}

	return json.MarshalIndent(entries, "", "  ")
}

// sortSkills orders skills most installed first, then by key.
func sortSkills(skills []CatalogSkill) {
	slices.SortFunc(skills, func(a, b CatalogSkill) int {
		return cmp.Or(cmp.Compare(b.Installs, a.Installs), strings.Compare(a.String(), b.String()))
	})
}

// matches reports whether q (lowercase) is in the skill's key, name or description.
func matches(s CatalogSkill, q string) bool {
	return q == "" || strings.Contains(strings.ToLower(s.String()), q) || strings.Contains(strings.ToLower(s.Skill), q) ||
		strings.Contains(strings.ToLower(s.Description), q)
}

// folder is where a skill's commit is stored.
func folder(k SkillKey, commit string) string {
	return catalogRoot + k.Owner + "/" + k.Repo + "/" + k.Skill + "/" + commit + "/"
}

// treeHash is the sha256 over a folder's files: one "<path>\x00<sha256 of the content>\n" line
// per file, in path order.
func treeHash(files map[string][]byte) string {
	h := sha256.New()

	for _, p := range slices.Sorted(maps.Keys(files)) {
		sum := sha256.Sum256(files[p])
		_, _ = fmt.Fprintf(h, "%s\x00%x\n", p, sum)
	}

	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// checkTree enforces the skill limits (ports.ErrTooLarge) and the path rules on a folder, which
// must have SKILL.md at its root.
func checkTree(files map[string][]byte) (int64, error) {
	if len(files) > domain.MaxSkillFiles {
		return 0, fmt.Errorf("%d files, at most %d: %w", len(files), domain.MaxSkillFiles, ports.ErrTooLarge)
	}

	var total int64

	for p, body := range files {
		if !domain.ValidSkillPath(p) {
			return 0, fmt.Errorf("%w: path %q", errBadTree, p)
		}

		total += int64(len(body))
	}

	if total > domain.MaxSkillBytes {
		return 0, fmt.Errorf("%d bytes, at most %d: %w", total, domain.MaxSkillBytes, ports.ErrTooLarge)
	}

	if _, ok := files[domain.SkillFile]; !ok {
		return 0, fmt.Errorf("%w: no %s at its root", errBadTree, domain.SkillFile)
	}

	return total, nil
}

// SearchSkills returns one page of the catalog's skills matching q (case-insensitive, in the
// key, name or description), most installed first, then by key, continuing after the key after.
// A limit outside [1, MaxSkillLimit] means DefaultSkillLimit. An after that is no longer in the
// results ends the paging. An unreadable bucket is domain.ErrCatalogUnavailable; a malformed
// index is logged and reads as an empty catalog.
func (s *Service) SearchSkills(ctx context.Context, q string, limit int, after string) (SkillPage, error) {
	if limit < 1 || limit > MaxSkillLimit {
		limit = DefaultSkillLimit
	}

	skills, err := s.index(ctx)
	if err != nil {
		return SkillPage{}, err
	}

	q = strings.ToLower(strings.TrimSpace(q))

	var hits []CatalogSkill

	for _, sk := range skills {
		if matches(sk, q) {
			hits = append(hits, sk)
		}
	}

	if after != "" {
		i := slices.IndexFunc(hits, func(sk CatalogSkill) bool { return sk.String() == after })
		if i < 0 {
			return SkillPage{Skills: []CatalogSkill{}}, nil
		}

		hits = hits[i+1:]
	}

	page := SkillPage{Skills: hits[:min(limit, len(hits))]}
	if len(hits) > limit {
		page.NextAfter = hits[limit-1].String()
	}

	return page, nil
}

// SkillFiles returns a catalog skill and its folder's files, after checking them against the
// index: their limits, paths and hash. An unknown skill is domain.ErrSkillNotFound; a bucket
// that fails, or a folder that does not match its index entry, domain.ErrCatalogUnavailable.
func (s *Service) SkillFiles(ctx context.Context, key SkillKey) (CatalogSkill, map[string][]byte, error) {
	if !key.Valid() {
		return CatalogSkill{}, nil, fmt.Errorf("skill %s: %w", key, domain.ErrSkillNotFound)
	}

	skills, err := s.index(ctx)
	if err != nil {
		return CatalogSkill{}, nil, err
	}

	i := slices.IndexFunc(skills, func(sk CatalogSkill) bool { return sk.SkillKey == key })
	if i < 0 {
		return CatalogSkill{}, nil, fmt.Errorf("skill %s: %w", key, domain.ErrSkillNotFound)
	}

	files, err := s.readFolder(ctx, skills[i])
	if err != nil {
		s.log.ErrorContext(ctx, "builder.skill_files_failed", slog.String("key", key.String()), slog.Any("error", err))

		return CatalogSkill{}, nil, fmt.Errorf("skill %s: %w: %w", key, domain.ErrCatalogUnavailable, err)
	}

	return skills[i], files, nil
}

// readFolder reads a skill's stored folder and checks it against its index entry.
func (s *Service) readFolder(ctx context.Context, sk CatalogSkill) (map[string][]byte, error) {
	prefix := folder(sk.SkillKey, sk.Commit)

	infos, err := s.objects.List(ctx, prefix)
	if err != nil {
		return nil, err
	}

	if len(infos) != sk.Files {
		return nil, fmt.Errorf("the folder has %d files, the index %d", len(infos), sk.Files)
	}

	var total int64
	for _, info := range infos {
		total += info.Size
	}

	if total > domain.MaxSkillBytes {
		return nil, fmt.Errorf("the folder has %d bytes: %w", total, ports.ErrTooLarge)
	}

	files := make(map[string][]byte, len(infos))

	for _, info := range infos {
		obj, err := s.objects.Get(ctx, info.Key, info.Version)
		if err != nil {
			return nil, err
		}

		files[strings.TrimPrefix(info.Key, prefix)] = obj.Body
	}

	if _, err := checkTree(files); err != nil {
		return nil, err
	}

	if got := treeHash(files); got != sk.Hash {
		return nil, fmt.Errorf("the folder's hash %s is not the index's %s", got, sk.Hash)
	}

	return files, nil
}

// index returns the catalog's skills from the cached index, confirming at most every indexTTL
// that the bucket's index.json is still the version parsed. When the bucket cannot be read, a
// parsed index keeps serving (its content is immutable) and the error is logged.
func (s *Service) index(ctx context.Context) ([]CatalogSkill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock.Now()
	if s.cache.loaded && now.Sub(s.cache.checked) < indexTTL {
		return s.cache.skills, nil
	}

	info, found, err := s.indexInfo(ctx)
	if err == nil && found && (!s.cache.loaded || info.Version != s.cache.version) {
		var skills []CatalogSkill

		if skills, err = s.loadIndex(ctx, info); err == nil {
			s.cache = indexCache{loaded: true, version: info.Version, skills: skills}
		}
	}

	if err == nil && !found {
		s.cache = indexCache{loaded: true}
	}

	if err != nil {
		if !s.cache.loaded {
			return nil, fmt.Errorf("%w: read %s: %w", domain.ErrCatalogUnavailable, indexKey, err)
		}

		s.log.WarnContext(ctx, "builder.skills_index_unreadable", slog.Any("error", err))

		return s.cache.skills, nil
	}

	s.cache.checked = now

	return s.cache.skills, nil
}

// indexInfo finds the current index.json.
func (s *Service) indexInfo(ctx context.Context) (ports.ObjectInfo, bool, error) {
	infos, err := s.objects.List(ctx, indexKey)
	if err != nil {
		return ports.ObjectInfo{}, false, err
	}

	i := slices.IndexFunc(infos, func(o ports.ObjectInfo) bool { return o.Key == indexKey })
	if i < 0 {
		return ports.ObjectInfo{}, false, nil
	}

	return infos[i], true, nil
}

// loadIndex reads and parses one version of index.json. A malformed index is logged and reads
// as an empty catalog; only a failing bucket is an error.
func (s *Service) loadIndex(ctx context.Context, info ports.ObjectInfo) ([]CatalogSkill, error) {
	if info.Size > maxIndexBytes {
		s.log.ErrorContext(ctx, "builder.skills_index_invalid", slog.String("version", info.Version),
			slog.String("error", fmt.Sprintf("%d bytes, at most %d", info.Size, maxIndexBytes)))

		return nil, nil
	}

	obj, err := s.objects.Get(ctx, indexKey, info.Version)
	if err != nil {
		return nil, err
	}

	skills, err := parseIndex(obj.Body)
	if err != nil {
		s.log.ErrorContext(ctx, "builder.skills_index_invalid", slog.String("version", info.Version), slog.Any("error", err))

		return nil, nil
	}

	return skills, nil
}

// readIndexForWrite reads the current index for a sync, which rewrites it: missing is empty,
// and a malformed one is an error, so a sync never replaces skills it could not read.
func (s *Service) readIndexForWrite(ctx context.Context) (map[string]CatalogSkill, []byte, error) {
	info, found, err := s.indexInfo(ctx)
	if err != nil || !found {
		return map[string]CatalogSkill{}, nil, err
	}

	obj, err := s.objects.Get(ctx, indexKey, info.Version)
	if err != nil {
		return nil, nil, err
	}

	skills, err := parseIndex(obj.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w; fix or remove it before syncing", indexKey, err)
	}

	out := make(map[string]CatalogSkill, len(skills))
	for _, sk := range skills {
		out[sk.String()] = sk
	}

	return out, bytes.Clone(obj.Body), nil
}
