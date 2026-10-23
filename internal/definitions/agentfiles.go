package definitions

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/byFrederick/goal/internal/ports"
)

// Agent files are read-only for the container's user, who may not own them on Linux; they
// hold no secrets.
const (
	fileMode = 0o644
	dirMode  = 0o755
)

// WriteAgentFiles writes one task's prompt, skills and output schema into dir, from the
// factory's current definition. It implements docker.AgentFiles.
//
// Only skills that ship inside the factory folder (source "./…") are written. A skills.sh
// skill is not staged in phase 1: the agent loads it by name from the checkout's own
// .claude/skills when the repository vendors it, and does without it otherwise.
func (c *Cache) WriteAgentFiles(ctx context.Context, t ports.TaskSpec, dir string) error {
	l, err := c.get(t.FactoryID)
	if err != nil {
		return err
	}

	files := l.snapshot.FS()

	if err := copyFile(files, path.Clean(t.PromptPath), filepath.Join(dir, "prompt.md")); err != nil {
		return fmt.Errorf("agent %s prompt: %w", t.Agent, err)
	}

	for _, sk := range t.Skills {
		src, local := strings.CutPrefix(sk.Source, "./")
		if !local {
			c.log.DebugContext(ctx, "skill not staged; the checkout may vendor it", slog.String("skill", sk.Name), slog.String("source", sk.Source))
			continue
		}

		if !fs.ValidPath(sk.Name) || strings.Contains(sk.Name, "/") {
			return fmt.Errorf("skill name %q is not a folder name", sk.Name)
		}

		if err := copyTree(files, path.Clean(src), filepath.Join(dir, "skills", sk.Name)); err != nil {
			return fmt.Errorf("skill %s: %w", sk.Name, err)
		}
	}

	if t.Output == "" {
		return nil
	}

	file, err := contractFile(l, t.Output)
	if err != nil {
		return err
	}

	if err := copyFile(files, file, filepath.Join(dir, "output.schema.json")); err != nil {
		return fmt.Errorf("contract %s: %w", t.Output, err)
	}

	return nil
}

func copyTree(files fs.FS, root, dst string) error {
	return fs.WalkDir(files, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		target := filepath.Join(dst, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(p, root), "/")))
		if d.IsDir() {
			return os.MkdirAll(target, dirMode)
		}

		return copyFile(files, p, target)
	})
}

func copyFile(files fs.FS, src, dst string) error {
	body, err := fs.ReadFile(files, src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), dirMode); err != nil {
		return err
	}

	return os.WriteFile(dst, body, fileMode)
}
