package porttest

import (
	"context"
	"errors"
	"fmt"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Publish publishes files as the next version of factory id in the organization of ctx, as an
// editor publishing a draft would, for tests of what reads published versions: it creates the
// factory (named name) and its first draft when the factory does not exist, or opens a draft on
// the current version and replaces its tree, then publishes the draft. store is any
// ports.DefinitionStore: the fake or the PostgreSQL adapter.
func Publish(ctx context.Context, store ports.DefinitionStore, id domain.FactoryID, name string, files map[string][]byte,
) (domain.DefinitionVersion, error) {
	m, err := store.PutBlobs(ctx, files)
	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("put blobs: %w", err)
	}

	rec, err := store.FactoryRecord(ctx, id)

	var draft domain.DefinitionVersion

	switch {
	case errors.Is(err, ports.ErrNotFound):
		rec, draft, err = store.CreateFactory(ctx, ports.NewFactory{
			ID: id, Name: name, Manifest: m, Source: domain.VersionSource{Kind: domain.SourceBlank}, By: domain.LocalUser,
		})
	case err == nil:
		draft, _, err = store.OpenDraft(ctx, ports.OpenDraft{Factory: id, By: domain.LocalUser})
		if err == nil {
			draft, err = store.SaveDraft(ctx, ports.DraftChange{
				Factory: id, ExpectedRevision: draft.Revision, Set: m, Replace: true, By: domain.LocalUser,
			})
		}
	}

	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("draft of %s: %w", id, err)
	}

	var expected domain.DefinitionVersionID
	if rec.Current != nil {
		expected = rec.Current.ID
	}

	v, err := store.Publish(ctx, ports.Publish{
		Factory: id, DraftRevision: draft.Revision, ExpectedCurrent: expected, Name: name, Start: true, By: domain.LocalUser,
	})
	if err != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("publish %s: %w", id, err)
	}

	return v, nil
}
