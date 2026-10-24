package factorystore

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// LegacyRoot is where every factory lived before organizations: factories/<id>/. Only
// MigrateLegacy reads it.
const LegacyRoot = "factories/"

// Migration counts what MigrateLegacy did. Copied objects were missing or different in the
// organization; Present ones were already there; Gone ones were deleted by someone else between
// the listing and the copy; Deleted legacy keys now carry a delete marker.
type Migration struct {
	Copied, Present, Gone, Deleted int
}

// MigrateLegacy moves every factory of the layout before organizations into org: each current
// object under factories/ is copied to the same path under OrgPrefix(org) when it is missing
// there or differs, factory.yaml last within each factory so no reader sees a definition point at
// files not copied yet. Once every legacy object it read is present and equal in org, it deletes
// the legacy keys; in the versioned bucket that leaves delete markers, so the old versions stay
// readable. Until then nothing is deleted.
//
// The steps are idempotent and convergent: several goald instances (or the CLI) may run it at
// once without a lock, and a run that fails part-way is finished by the next one.
func MigrateLegacy(ctx context.Context, store ports.ObjectStore, org domain.OrgID) (Migration, error) {
	var m Migration

	legacy, err := store.List(ctx, LegacyRoot)
	if err != nil {
		return m, fmt.Errorf("list %s: %w", LegacyRoot, err)
	}

	slices.SortFunc(legacy, func(a, b ports.ObjectInfo) int {
		fa, ra := legacyPath(a.Key)
		fb, rb := legacyPath(b.Key)

		return cmp.Or(cmp.Compare(fa, fb), cmp.Compare(isDefinition(ra), isDefinition(rb)), cmp.Compare(ra, rb))
	})

	var copied []string

	for _, o := range legacy {
		state, err := migrate(ctx, store, o.Key, OrgPrefix(org)+o.Key)

		switch {
		case err != nil:
			return m, err
		case state == objectGone:
			m.Gone++
			continue
		case state == objectCopied:
			m.Copied++
		default:
			m.Present++
		}

		copied = append(copied, o.Key)
	}

	for _, key := range copied {
		if err := verify(ctx, store, key, OrgPrefix(org)+key); err != nil {
			return m, fmt.Errorf("legacy objects are kept: %w", err)
		}
	}

	for _, key := range copied {
		if err := store.Delete(ctx, key); err != nil {
			return m, fmt.Errorf("delete legacy %s: %w", key, err)
		}

		m.Deleted++
	}

	return m, nil
}

// legacyPath splits a legacy key into its factory id and the path inside that factory.
func legacyPath(key string) (id, rel string) {
	id, rel, _ = strings.Cut(strings.TrimPrefix(key, LegacyRoot), "/")
	return id, rel
}

// isDefinition orders a factory's factory.yaml after its other files.
func isDefinition(rel string) int {
	if rel == config.FileName {
		return 1
	}

	return 0
}

type objectState int

const (
	objectPresent objectState = iota
	objectCopied
	objectGone
)

// migrate copies the current version of src to dst unless dst already holds the same bytes.
func migrate(ctx context.Context, store ports.ObjectStore, src, dst string) (objectState, error) {
	from, err := store.Get(ctx, src, "")
	if errors.Is(err, ports.ErrNotFound) {
		return objectGone, nil
	}

	if err != nil {
		return 0, fmt.Errorf("read legacy %s: %w", src, err)
	}

	to, err := store.Get(ctx, dst, "")

	switch {
	case err == nil && bytes.Equal(to.Body, from.Body):
		return objectPresent, nil
	case err != nil && !errors.Is(err, ports.ErrNotFound):
		return 0, fmt.Errorf("read %s: %w", dst, err)
	}

	if _, err := store.Put(ctx, dst, from.Body); err != nil {
		return 0, fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}

	return objectCopied, nil
}

// verify checks that dst holds the current content of src; a src deleted meanwhile passes.
func verify(ctx context.Context, store ports.ObjectStore, src, dst string) error {
	from, err := store.Get(ctx, src, "")
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read legacy %s: %w", src, err)
	}

	to, err := store.Get(ctx, dst, "")
	if err != nil {
		return fmt.Errorf("read %s: %w", dst, err)
	}

	if !bytes.Equal(to.Body, from.Body) {
		return fmt.Errorf("%s differs from %s", dst, src)
	}

	return nil
}
