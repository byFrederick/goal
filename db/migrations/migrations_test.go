package migrations

import (
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

// TestFS_VersionsAreUnique fails when two migration files share a version prefix: goose refuses
// duplicates, so a reused number would break every database migration on main.
func TestFS_VersionsAreUnique(t *testing.T) {
	names, err := fs.Glob(FS, "*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no migrations embedded")
	}
	seen := make(map[int64]string, len(names))
	for _, name := range names {
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			t.Errorf("%s: no version prefix before an underscore", name)
			continue
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			t.Errorf("%s: version prefix %q is not a number: %v", name, prefix, err)
			continue
		}
		if other, dup := seen[version]; dup {
			t.Errorf("version %d is used by both %s and %s: give the newer one the next free number", version, other, name)
			continue
		}
		seen[version] = name
	}
}
