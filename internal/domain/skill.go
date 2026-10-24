package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// Limits of one skill folder from skills.sh, enforced when the catalog syncs it and again when
// it is read (2.2-D §5.1).
const (
	MaxSkillFiles = 200
	MaxSkillBytes = 1 << 20
	// MaxSkillPathLen keeps a vendored path (skills/<skill>/<path>) within a definition's
	// 255-character path limit.
	MaxSkillPathLen = 200
)

// SkillFile is the file every skill folder has at its root.
const SkillFile = "SKILL.md"

// The skills catalog's refusals. The API answers ErrSkillNotFound with 404 skill-not-found and
// ErrCatalogUnavailable (the bucket failed, or holds a folder that does not match its index)
// with 503 catalog-unavailable.
var (
	ErrSkillNotFound      = errors.New("skill not found")
	ErrCatalogUnavailable = errors.New("skills catalog unavailable")
)

var (
	skillName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
	skillPath = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
)

// ValidSkillName reports whether s may be the owner, repository or skill of a skills.sh
// reference: letters, digits, dots, dashes and underscores, but never . or .., since names
// become path segments of bucket keys and API URLs.
func ValidSkillName(s string) bool {
	return skillName.MatchString(s) && s != "." && s != ".."
}

// ValidSkillPath reports whether p may be a file's path inside a skill folder: relative,
// slash-separated segments of letters, digits, dots, dashes and underscores, none of them
// hidden (starting with a dot, so never . or ..), at most MaxSkillPathLen characters. These are
// the rules a definition's own paths follow, so a vendored skill always fits in a draft.
func ValidSkillPath(p string) bool {
	if len(p) > MaxSkillPathLen || !skillPath.MatchString(p) {
		return false
	}

	for seg := range strings.SplitSeq(p, "/") {
		if strings.HasPrefix(seg, ".") {
			return false
		}
	}

	return true
}

// HiddenSkillPath reports whether a segment of p starts with a dot (.gitignore, .github/...).
// The catalog leaves such files out of a skill: a definition may not hold them.
func HiddenSkillPath(p string) bool {
	for seg := range strings.SplitSeq(p, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}

	return false
}

// SkillKey names a skill of the catalog: <owner>/<repo>/<skill>.
type SkillKey struct{ Owner, Repo, Skill string }

func (k SkillKey) String() string { return k.Owner + "/" + k.Repo + "/" + k.Skill }

// Valid reports whether every part of k is a valid name.
func (k SkillKey) Valid() bool {
	return ValidSkillName(k.Owner) && ValidSkillName(k.Repo) && ValidSkillName(k.Skill)
}

// CatalogSkill is one skill of the catalog, pinned at a commit: the folder of that commit is
// stored once and never overwritten, and Hash (sha256 over its files) is checked on every read.
type CatalogSkill struct {
	SkillKey

	Commit      string
	Description string
	Installs    int64
	Files       int
	Bytes       int64
	Hash        string
	SyncedAt    time.Time
}

// Origin is the provenance a definition records for the skill once vendored (skills[].origin):
// skills.sh:<owner>/<repo>@<skill>#<commit>.
func (s CatalogSkill) Origin() string {
	return "skills.sh:" + s.Owner + "/" + s.Repo + "@" + s.Skill + "#" + s.Commit
}

// SkillPage is one page of a catalog search; NextAfter is the key to continue after, empty on
// the last page.
type SkillPage struct {
	Skills    []CatalogSkill
	NextAfter string
}

// Model is a model the editor offers: its id (runtime.model, agents[].model) and its label.
type Model struct{ ID, Label string }

// EditorCapabilities says which later phases are wired, so the editor shows their controls.
type EditorCapabilities struct {
	// Credentials: phase 4.1's credential selects and factory credentials.
	Credentials bool
	// Connectors: phase 4.2's connector catalog and connector panel.
	Connectors bool
	// ToolApprovals: "Ask me" on built-in tools is enforced.
	ToolApprovals bool
}

// BuilderMeta is what the factory editor needs to know about the install: the models (the
// first is the default), the capabilities and the skill limits.
type BuilderMeta struct {
	Models        []Model
	Capabilities  EditorCapabilities
	MaxSkillBytes int
	MaxSkillFiles int
}
