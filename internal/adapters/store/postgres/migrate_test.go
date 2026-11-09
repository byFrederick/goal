package postgres

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/byFrederick/goal/db/migrations"
)

// Migrate skips goose only when every embedded migration is applied: a database that lacks one,
// new or behind, migrates; one that also has a newer release's migrations has nothing to do.
func TestUpToDate_ComparesAppliedWithEmbeddedVersions(t *testing.T) {
	t.Parallel()

	embedded := []int64{1, 2, 3}

	tests := []struct {
		name    string
		applied []int64
		want    bool
	}{
		{name: "a new database", applied: nil, want: false},
		{name: "only goose's table", applied: []int64{0}, want: false},
		{name: "behind", applied: []int64{0, 1, 2}, want: false},
		{name: "a gap below the latest", applied: []int64{0, 1, 3}, want: false},
		{name: "at the latest", applied: []int64{0, 1, 2, 3}, want: true},
		{name: "in any order", applied: []int64{3, 1, 2}, want: true},
		{name: "past the latest", applied: []int64{0, 1, 2, 3, 4}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := upToDate(tt.applied, embedded); got != tt.want {
				t.Errorf("upToDate(%v, %v) = %v, want %v", tt.applied, embedded, got, tt.want)
			}
		})
	}
}

func TestEmbeddedVersions_AreTheMigrationsNumbers(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"00002_b.sql":   {Data: []byte("-- +goose Up\n")},
		"00001_a.sql":   {Data: []byte("-- +goose Up\n")},
		"migrations.go": {Data: []byte("package migrations\n")},
	}

	if got, err := embeddedVersions(fsys); err != nil || !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("embeddedVersions = %v (%v), want [1 2]", got, err)
	}

	got, err := embeddedVersions(migrations.FS)
	if err != nil || len(got) == 0 {
		t.Fatalf("embeddedVersions(migrations.FS) = %v (%v), want every migration", got, err)
	}

	if got, err := embeddedVersions(fstest.MapFS{"x_bad.sql": {}}); err == nil {
		t.Errorf("embeddedVersions of a file without a number = %v, want an error", got)
	}
}
