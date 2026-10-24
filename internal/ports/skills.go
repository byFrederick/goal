package ports

import (
	"context"
	"errors"
)

// ErrTooLarge means content is beyond its limits (a skill folder past domain.MaxSkillFiles or
// domain.MaxSkillBytes).
var ErrTooLarge = errors.New("too large")

// SkillRef names one skill on skills.sh: owner/repo@skill, at a commit when Commit is set (40
// lowercase hex digits).
type SkillRef struct{ Owner, Repo, Skill, Commit string }

func (r SkillRef) String() string {
	s := r.Owner + "/" + r.Repo + "@" + r.Skill
	if r.Commit != "" {
		s += "#" + r.Commit
	}

	return s
}

// SkillListing is one entry of the skills.sh directory.
type SkillListing struct {
	Ref         SkillRef
	Description string
	Installs    int64
}

// SkillTree is a skill folder at one commit; SKILL.md is at its root.
type SkillTree struct {
	// Ref has its Commit resolved.
	Ref SkillRef
	// Files maps relative, slash-separated paths to their content: regular files only.
	Files map[string][]byte
}

// SkillSource is where the skills catalog comes from: the skills.sh directory and the GitHub
// repositories its skills live in. Only `goal catalog skills sync` uses it; goald never fetches
// third-party content. What it returns is untrusted: callers validate it again.
type SkillSource interface {
	// Search lists directory entries matching query, most installed first, at most limit.
	Search(ctx context.Context, query string, limit int) ([]SkillListing, error)
	// Fetch resolves ref (the default branch head when Commit is empty) and returns its files.
	// Errors wrap ErrNotFound for an unknown skill and ErrTooLarge beyond the limits.
	Fetch(ctx context.Context, ref SkillRef) (SkillTree, error)
}
