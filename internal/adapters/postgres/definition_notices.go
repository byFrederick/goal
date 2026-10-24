package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// definitionChannel is where the factories and definition_versions triggers notify (00010).
const definitionChannel = "goal_definitions"

var _ ports.DefinitionNotices = (*DefinitionNotices)(nil)

// DefinitionNotices implements ports.DefinitionNotices with LISTEN goal_definitions on a
// connection of its own; it hears every organization's changes, from every goald.
type DefinitionNotices struct {
	l listener[ports.DefinitionNotice]
}

// NewDefinitionNotices listens with cfg, usually the store pool's connection config
// (pool.Config().ConnConfig). log may be nil.
func NewDefinitionNotices(cfg *pgx.ConnConfig, log *slog.Logger) *DefinitionNotices {
	return &DefinitionNotices{l: listener[ports.DefinitionNotice]{
		cfg: cfg, log: discardIfNil(log), channels: []string{definitionChannel}, name: "definition notices",
		resync: ports.DefinitionNotice{Resync: true}, decode: definitionNoticeFrom,
	}}
}

// Listen implements ports.DefinitionNotices.
func (n *DefinitionNotices) Listen(ctx context.Context, deliver func(ports.DefinitionNotice)) error {
	return n.l.Listen(ctx, deliver)
}

// definitionPayload is what notify_factory_definition() and notify_definition_version() send, and
// DeleteOrg (kind org, no factory).
type definitionPayload struct {
	OrgID     string  `json:"org_id"`
	FactoryID string  `json:"factory_id"`
	Kind      string  `json:"kind"`
	VersionID *string `json:"version_id"`
	Number    *int    `json:"number"`
	Revision  *int64  `json:"revision"`
	By        *string `json:"by"`
}

func definitionNoticeFrom(_, raw string) (ports.DefinitionNotice, error) {
	var p definitionPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return ports.DefinitionNotice{}, fmt.Errorf("decode %q: %w", raw, err)
	}

	// Only an organization's deletion (DeleteOrg) names no factory.
	if p.OrgID == "" || p.Kind == "" || (p.FactoryID == "" && p.Kind != ports.NoticeOrg) {
		return ports.DefinitionNotice{}, errors.New("notification without an organization, factory or kind")
	}

	n := ports.DefinitionNotice{
		OrgID: domain.OrgID(p.OrgID), FactoryID: domain.FactoryID(p.FactoryID), Kind: p.Kind, By: deref(p.By),
	}

	if p.VersionID != nil {
		n.VersionID = domain.DefinitionVersionID(*p.VersionID)
	}

	if p.Number != nil {
		n.Number = *p.Number
	}

	if p.Revision != nil {
		n.Revision = *p.Revision
	}

	return n, nil
}
