package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// definitionChannel is where the factories and definition_versions triggers notify (00010).
const definitionChannel = "goal_definitions"

var _ ports.DefinitionNotices = (*DefinitionNotices)(nil)

// DefinitionNotices implements ports.DefinitionNotices with LISTEN goal_definitions on a
// connection of its own; it hears every organization's changes, from every goald. Draft and
// current notices are read back through the row-level security of the organization they name
// (review 2.2 M4) and carry the version's own fields; a factory or org notice carries ids only.
type DefinitionNotices struct {
	l listener[ports.DefinitionNotice]
}

// NewDefinitionNotices listens with the connection config of s's pool and reads notices back
// through s. log may be nil.
func NewDefinitionNotices(s *Store, log *slog.Logger) *DefinitionNotices {
	return &DefinitionNotices{l: listener[ports.DefinitionNotice]{
		cfg: s.pool.Config().ConnConfig, log: discardIfNil(log), channels: []string{definitionChannel}, name: "definition notices",
		resync: ports.DefinitionNotice{Resync: true}, decode: definitionNoticeFrom, readBack: s.readBackDefinitionNotice,
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

// readBackDefinitionNotice checks n against the rows of its organization. A version row changes in
// place (its revision, number and author move on), so a notice cannot be rebuilt from it once a
// later change committed; instead it must be borne out: a draft notice names a version of the
// factory, at a revision the version reached and with its number if it has one; a current notice
// names a published version of the factory with that version's number (or no version, when the
// factory now has none). A factory or org notice carries ids only (listeners re-read on it).
// Anything else, and anything the organization cannot see, reports false. By is the claim's:
// listeners only re-read on it.
func (s *Store) readBackDefinitionNotice(ctx context.Context, n ports.DefinitionNotice) (ports.DefinitionNotice, bool, error) {
	// Only the fields the kind carries (ports.NoticeDraft, NoticeCurrent).
	out := ports.DefinitionNotice{OrgID: n.OrgID, FactoryID: n.FactoryID, Kind: n.Kind, VersionID: n.VersionID, Number: n.Number}
	if n.Kind == ports.NoticeDraft {
		out.Revision, out.By = n.Revision, n.By
	}

	switch n.Kind {
	case ports.NoticeFactory, ports.NoticeOrg:
		return ports.DefinitionNotice{OrgID: n.OrgID, FactoryID: n.FactoryID, Kind: n.Kind}, true, nil
	case ports.NoticeDraft, ports.NoticeCurrent:
		return readBack(ctx, s, n.OrgID, func(q *db.Queries) (ports.DefinitionNotice, error) { return out, checkDefinitionNotice(ctx, q, n) })
	default:
		return ports.DefinitionNotice{}, false, nil
	}
}

// checkDefinitionNotice returns errNothingToReadBack (or the read's not-found) unless the rows bear
// out the draft or current notice n.
func checkDefinitionNotice(ctx context.Context, q *db.Queries, n ports.DefinitionNotice) error {
	factory, err := uuid.Parse(string(n.FactoryID))
	if err != nil {
		return errNothingToReadBack
	}

	if n.Kind == ports.NoticeCurrent && n.VersionID == "" {
		current, err := q.NoticeCurrent(ctx, factory)
		if err != nil {
			return err
		}

		if current != nil {
			return errNothingToReadBack
		}

		return nil
	}

	version, err := uuid.Parse(string(n.VersionID))
	if err != nil {
		return errNothingToReadBack
	}

	r, err := q.NoticeVersion(ctx, version)
	if err != nil {
		return err
	}

	if r.FactoryID != factory || !bearsOut(r, n) {
		return errNothingToReadBack
	}

	return nil
}

// bearsOut reports whether version r of the notice's factory bears out the draft or current
// notice n: a draft at a revision it reached and with its number if it has one, a current version
// published with that number.
func bearsOut(r db.NoticeVersionRow, n ports.DefinitionNotice) bool {
	numbered := r.Number != nil && int(*r.Number) == n.Number

	if n.Kind == ports.NoticeDraft {
		return n.Revision >= 1 && n.Revision <= r.Revision && (n.Number == 0 || numbered)
	}

	return r.State == string(domain.VersionPublished) && numbered
}
