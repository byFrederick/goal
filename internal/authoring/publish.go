package authoring

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// maxMessage is the longest publish note, in characters.
const maxMessage = 200

// PublishRequest publishes a factory's open draft.
type PublishRequest struct {
	// Revision is the draft revision the editor validated.
	Revision int64
	// ExpectedCurrent is the version the editor saw running; empty for a first publish.
	ExpectedCurrent domain.DefinitionVersionID
	Message         string
	// StationMoves maps each station the draft removes that has open items to a station of the
	// new version.
	StationMoves map[string]string
	// Start, on a first publish only, starts the factory; false creates it paused (New-Review's
	// "Start the line right away" unticked). Nil means true.
	Start *bool
}

// Publish makes the open draft the factory's next version (B §4.2): it refuses a draft that
// moved since Revision (ErrDraftConflict), a current version other than ExpectedCurrent
// (ErrBaseMoved), a draft with error problems (*InvalidError), a name another factory took
// (ErrNameTaken) and open items at removed stations that no move maps
// (*ports.ItemsAtRemovedStationError). Items at removed stations move in the same transaction.
func (s *Service) Publish(ctx context.Context, f domain.FactoryID, r PublishRequest) (domain.DefinitionVersion, error) {
	by, err := actor(ctx)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	rec, d, err := s.publishable(ctx, f, r)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	c, err := s.validateDraft(ctx, rec, d, nil, r.StationMoves)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	if !c.OK {
		return domain.DefinitionVersion{}, &InvalidError{Problems: c.Problems}
	}

	removed, moves, err := s.lineChange(ctx, rec, c.factory, r.StationMoves)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	if s.resolver != nil {
		if _, err := s.resolver.Resolve(ctx, c.factory); err != nil {
			return domain.DefinitionVersion{}, fmt.Errorf("resolve connections: %w", err)
		}
	}

	v, err := s.store.Publish(ctx, ports.Publish{
		Factory: f, DraftRevision: r.Revision, ExpectedCurrent: r.ExpectedCurrent,
		Name: c.factory.Metadata.Name, Title: c.factory.Metadata.Title, Message: r.Message,
		RemovedStations: removed, StationMoves: moves, Start: r.Start == nil || *r.Start, By: by, At: s.clock.Now(),
	})
	if errors.Is(err, ports.ErrConflict) {
		return domain.DefinitionVersion{}, fmt.Errorf("publish %s: %w", f, ErrDraftConflict)
	}

	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("publish %s: %w", f, err)
	}

	return v, nil
}

// publishable checks a publish request's guards before validating: the message, the draft's
// revision and the current version.
func (s *Service) publishable(ctx context.Context, f domain.FactoryID, r PublishRequest) (ports.FactoryRecord, domain.DefinitionVersion, error) {
	if utf8.RuneCountInString(r.Message) > maxMessage {
		return ports.FactoryRecord{}, domain.DefinitionVersion{},
			fmt.Errorf("a publish message has at most %d characters: %w", maxMessage, ErrInvalidRequest)
	}

	rec, err := s.record(ctx, f)
	if err != nil {
		return ports.FactoryRecord{}, domain.DefinitionVersion{}, err
	}

	d, err := s.draft(ctx, f)
	if err != nil {
		return ports.FactoryRecord{}, domain.DefinitionVersion{}, err
	}

	if d.Revision != r.Revision {
		return ports.FactoryRecord{}, domain.DefinitionVersion{},
			fmt.Errorf("the draft of %s is at revision %d, not %d: %w", f, d.Revision, r.Revision, ErrDraftConflict)
	}

	if current := currentID(rec); current != r.ExpectedCurrent {
		return ports.FactoryRecord{}, domain.DefinitionVersion{},
			fmt.Errorf("the current version of %s is %q, not %q: %w", f, current, r.ExpectedCurrent, ErrBaseMoved)
	}

	return rec, d, nil
}

// lineChange lists the stations of the running version that next drops and the moves of
// their items, each arriving in the state its new station takes.
func (s *Service) lineChange(
	ctx context.Context, rec ports.FactoryRecord, next *config.Factory, requested map[string]string,
) ([]string, map[string]ports.StationMove, error) {
	if rec.Current == nil {
		return nil, nil, nil
	}

	cur, err := s.byID(ctx, rec.ID, rec.Current.ID, VersionSelector{Kind: SelectCurrent})
	if err != nil {
		return nil, nil, err
	}

	m, _, err := s.versionModel(s.tree(ctx, cur.Manifest))
	if err != nil && !isParseError(err) {
		return nil, nil, err
	}

	stations := next.Line().Stations
	kept := make([]string, 0, len(stations))

	for _, st := range stations {
		kept = append(kept, st.ID)
	}

	var removed []string

	for _, id := range stationIDs(m) {
		if !slices.Contains(kept, id) {
			removed = append(removed, id)
		}
	}

	moves := map[string]ports.StationMove{}

	for from, to := range requested {
		if !slices.Contains(removed, from) {
			continue
		}

		i := slices.Index(kept, to)
		if i < 0 {
			return nil, nil, fmt.Errorf("station move %s → %s: the new version has no station %s: %w", from, to, to, ErrInvalidRequest)
		}

		moves[from] = ports.StationMove{To: to, State: stations[i].Arrival()}
	}

	return removed, moves, nil
}

func currentID(rec ports.FactoryRecord) domain.DefinitionVersionID {
	if rec.Current == nil {
		return ""
	}

	return rec.Current.ID
}

// RestoreRequest points a factory back at one of its published versions.
type RestoreRequest struct {
	Version VersionSelector
	// ExpectedCurrent is the version the caller saw running.
	ExpectedCurrent domain.DefinitionVersionID
	// StationMoves map stations the restored version lacks, as for Publish.
	StationMoves map[string]string
}

// Restore moves the factory's current pointer to an older published version (decision 3:
// numbers are not reused and an open draft stays open). It refuses a current version other
// than ExpectedCurrent (ErrBaseMoved), a name another factory took since (ErrNameTaken) and
// unmapped open items at stations the restored version lacks.
func (s *Service) Restore(ctx context.Context, f domain.FactoryID, r RestoreRequest) (ports.FactoryRecord, error) {
	by, err := actor(ctx)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	rec, err := s.record(ctx, f)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	if current := currentID(rec); current != r.ExpectedCurrent {
		return ports.FactoryRecord{}, fmt.Errorf("the current version of %s is %q, not %q: %w", f, current, r.ExpectedCurrent, ErrBaseMoved)
	}

	v, err := s.selectVersion(ctx, f, r.Version)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	if v == nil || v.State != domain.VersionPublished {
		return ports.FactoryRecord{}, fmt.Errorf("%s of %s is not a published version: %w", r.Version, f, ErrVersionNotFound)
	}

	t := s.tree(ctx, v.Manifest)

	restored, _ := config.Check(t)
	if t.err != nil {
		return ports.FactoryRecord{}, t.err
	}

	if restored == nil {
		return ports.FactoryRecord{}, fmt.Errorf("v%d of %s no longer loads: %w", v.Number, f, ErrDefinitionInvalid)
	}

	removed, moves, err := s.lineChange(ctx, rec, restored, r.StationMoves)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	out, err := s.store.Restore(ctx, ports.Restore{
		Factory: f, Version: v.ID, ExpectedCurrent: r.ExpectedCurrent,
		Name: restored.Metadata.Name, Title: restored.Metadata.Title,
		RemovedStations: removed, StationMoves: moves, By: by, At: s.clock.Now(),
	})
	if errors.Is(err, ports.ErrNotFound) {
		return ports.FactoryRecord{}, fmt.Errorf("restore %s of %s: %w", r.Version, f, ErrVersionNotFound)
	}

	if err != nil {
		return ports.FactoryRecord{}, fmt.Errorf("restore %s of %s: %w", r.Version, f, err)
	}

	return out, nil
}

// Pause stops the factory taking work; runs in progress finish.
func (s *Service) Pause(ctx context.Context, f domain.FactoryID) (ports.FactoryRecord, error) {
	return s.setPaused(ctx, f, true)
}

// Resume starts a paused factory again.
func (s *Service) Resume(ctx context.Context, f domain.FactoryID) (ports.FactoryRecord, error) {
	return s.setPaused(ctx, f, false)
}

func (s *Service) setPaused(ctx context.Context, f domain.FactoryID, paused bool) (ports.FactoryRecord, error) {
	by, err := actor(ctx)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	rec, err := s.store.SetPaused(ctx, f, paused, by)
	if errors.Is(err, ports.ErrNotFound) {
		return ports.FactoryRecord{}, fmt.Errorf("factory %s: %w", f, ErrFactoryNotFound)
	}

	if err != nil {
		return ports.FactoryRecord{}, fmt.Errorf("pause factory %s: %w", f, err)
	}

	return rec, nil
}

// DeleteFactory deletes a factory that was never published, with its drafts (the wizard's
// Cancel): ErrFactoryPublished once it was.
func (s *Service) DeleteFactory(ctx context.Context, f domain.FactoryID) error {
	by, err := actor(ctx)
	if err != nil {
		return err
	}

	err = s.store.DeleteUnpublished(ctx, f, by)

	switch {
	case errors.Is(err, ports.ErrConflict):
		return fmt.Errorf("delete factory %s: %w", f, ErrFactoryPublished)
	case errors.Is(err, ports.ErrNotFound):
		return fmt.Errorf("factory %s: %w", f, ErrFactoryNotFound)
	case err != nil:
		return fmt.Errorf("delete factory %s: %w", f, err)
	}

	return nil
}
