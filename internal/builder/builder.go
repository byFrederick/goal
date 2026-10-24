// Package builder is the backend of the factory editor that is not B's authoring: the editor's
// metadata (the install's models, which later phases are wired, the skill limits) and the
// skills catalog, skills.sh skills kept in the bucket so drafts can vendor them.
//
// The catalog lives under catalog/skills/ (2.2-D §7): index.json lists every skill with the
// commit it is pinned at, and catalog/skills/<owner>/<repo>/<skill>/<commit>/ holds that
// commit's folder, written once and never overwritten or deleted, since published drafts may
// have vendored it. Each index entry carries a sha256 over its folder's files, checked on every
// read, so the bucket hands out only what the sync stored. Only the operator's
// `goal catalog skills sync` writes the catalog, fetching through ports.SkillSource; goald only
// reads it, and nothing in it belongs to an organization.
//
// The package imports domain, ports and config only (depguard rule builder). The API reaches
// it through api.Builder; cmd/goald wires *Service, and cmd/goal uses SyncSkills and ListSkills.
package builder

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// The catalog's types live in domain, so the API can name them; these are their names here.
type (
	// Model is a model the editor offers.
	Model = domain.Model
	// Capabilities says which later phases are wired.
	Capabilities = domain.EditorCapabilities
	// Meta is the editor's metadata.
	Meta = domain.BuilderMeta
	// SkillKey names a catalog skill: owner/repo/skill.
	SkillKey = domain.SkillKey
	// CatalogSkill is one skill of the catalog at its pinned commit.
	CatalogSkill = domain.CatalogSkill
	// SkillPage is one page of a catalog search.
	SkillPage = domain.SkillPage
)

// Config is the builder's view of the install.
type Config struct {
	// Models come from GOAL_MODELS; the first is the default.
	Models []Model
	// Capabilities are set in cmd/goald; phase 4 flips its flags in its wiring PRs.
	Capabilities Capabilities
	// Logger may be nil.
	Logger *slog.Logger
}

// Paging of catalog searches.
const (
	DefaultSkillLimit = 20
	MaxSkillLimit     = 50
)

// Service serves the editor's metadata and the skills catalog. It is safe for concurrent use;
// the only thing it keeps is the parsed index of one bucket object version, a cache of
// immutable content.
type Service struct {
	objects ports.ObjectStore
	clock   ports.Clock
	cfg     Config
	log     *slog.Logger

	mu    sync.Mutex
	cache indexCache
}

// indexCache is the parsed index.json of one object version and when that version was last
// confirmed current.
type indexCache struct {
	loaded  bool
	version string
	skills  []CatalogSkill // installs desc, then key
	checked time.Time
}

// New returns the builder over the bucket.
func New(objects ports.ObjectStore, clock ports.Clock, cfg Config) *Service {
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Service{objects: objects, clock: clock, cfg: cfg, log: log}
}

// Meta returns the editor's metadata.
func (s *Service) Meta(context.Context) Meta {
	return Meta{
		Models:        slices.Clone(s.cfg.Models),
		Capabilities:  s.cfg.Capabilities,
		MaxSkillBytes: domain.MaxSkillBytes,
		MaxSkillFiles: domain.MaxSkillFiles,
	}
}
