// Command goal is the Goal CLI. For now it validates factory definitions;
// `goal up` (daemon + UI) lands in phase 1.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/byFrederick/goal/internal/config"
)

const usage = `usage: goal validate [factory-dir ...]

With no arguments, validates every folder under ./factories.`

func main() {
	if len(os.Args) < 2 || os.Args[1] != "validate" {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}

	dirs := os.Args[2:]
	if len(dirs) == 0 {
		dirs, _ = filepath.Glob("factories/*")
	}

	failed := false
	seen := map[string]string{} // factory id → folder

	for _, dir := range dirs {
		f, err := config.Load(dir)
		if err != nil {
			failed = true

			fmt.Printf("✗ %s\n%v\n\n", dir, err)

			continue
		}

		if other, dup := seen[f.Metadata.ID]; dup {
			failed = true

			fmt.Printf("✗ %s\nid %s is already used by %s; copy a factory, then give it a new id (uuidgen)\n\n", dir, f.Metadata.ID, other)

			continue
		}

		seen[f.Metadata.ID] = dir
		fmt.Printf("✓ %s (%s) — %d agents, %d stations, kb s3://…/%s\n",
			f.Metadata.Name, f.Metadata.ID[:8], len(f.Spec.Agents), len(f.Spec.Board.Stations), f.Prefix())
	}

	if failed {
		os.Exit(1)
	}
}
