package config

import (
	"errors"
	"io/fs"
	"maps"
	"slices"
	"testing"
	"testing/fstest"
)

func TestFiles_IsAValidFileSystem(t *testing.T) {
	t.Parallel()

	files := Files{
		FileName:                     []byte("kind: Factory\n"),
		"agents/writer.md":           []byte("# writer\n"),
		"skills/acme-notes/SKILL.md": []byte("# Notes\n"),
		"contracts/note.schema.json": []byte("{}"),
	}

	if err := fstest.TestFS(files, slices.Sorted(maps.Keys(files))...); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.Stat(files, "agents/missing.md"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat of a missing file: %v, want ErrNotExist", err)
	}
}
