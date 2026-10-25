package definitions

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Agent files are read-only for the container's user, who may not own them on Linux; they
// hold no secrets.
const (
	fileMode = 0o644
	dirMode  = 0o755
)

// AgentFiles returns one agent's files from definition version id, by slash-separated path
// under the agent's folder:
//
//	prompt.md                  the agent's prompt (AgentSpec.PromptPath)
//	skills/<name>/SKILL.md     one folder per skill that ships inside the factory, with its other files
//	output.schema.json         the JSON Schema of contract output, when output is not empty
//
// The gateway serves them in the run bundle (GET /run). Only skills that ship inside the
// factory folder (source "./…") are included. A skills.sh skill is not: the agent loads it by
// name from the checkout's own .claude/skills when the repository vendors it, and does without
// it otherwise.
func (c *Cache) AgentFiles(ctx context.Context, id domain.DefinitionVersionID, agent domain.AgentSpec, output string) (map[string][]byte, error) {
	v, err := c.version(ctx, id)
	if err != nil {
		return nil, err
	}

	return c.agentFiles(ctx, v, agent, output)
}

func (c *Cache) agentFiles(ctx context.Context, v *version, agent domain.AgentSpec, output string) (map[string][]byte, error) {
	files := v.files

	prompt, err := fs.ReadFile(files, path.Clean(agent.PromptPath))
	if err != nil {
		return nil, fmt.Errorf("agent %s prompt: %w", agent.Name, err)
	}

	out := map[string][]byte{"prompt.md": prompt}

	for _, sk := range agent.Skills {
		src, local := strings.CutPrefix(sk.Source, "./")
		if !local {
			c.log.DebugContext(ctx, "skill not staged; the checkout may vendor it", slog.String("skill", sk.Name), slog.String("source", sk.Source))
			continue
		}

		if !fs.ValidPath(sk.Name) || strings.Contains(sk.Name, "/") {
			return nil, fmt.Errorf("skill name %q is not a folder name", sk.Name)
		}

		if err := readTree(files, path.Clean(src), path.Join("skills", sk.Name), out); err != nil {
			return nil, fmt.Errorf("skill %s: %w", sk.Name, err)
		}
	}

	if output == "" {
		return out, nil
	}

	file, err := contractFile(v, output)
	if err != nil {
		return nil, err
	}

	if out["output.schema.json"], err = outputSchema(files, file); err != nil {
		return nil, fmt.Errorf("contract %s: %w", output, err)
	}

	return out, nil
}

// WriteAgentFiles writes one task's AgentFiles into dir, from the definition version the task's
// run started on. It implements docker.AgentFiles.
func (c *Cache) WriteAgentFiles(ctx context.Context, t ports.TaskSpec, dir string) error {
	v, err := c.version(ctx, t.DefinitionVersionID)
	if err != nil {
		return err
	}

	if v.factory.Metadata.ID != string(t.FactoryID) {
		return fmt.Errorf("definition version %s is not a version of factory %s: %w", v.id, t.FactoryID, ports.ErrNotFound)
	}

	files, err := c.agentFiles(ctx, v, domain.AgentSpec{Name: t.Agent, PromptPath: t.PromptPath, Skills: t.Skills}, t.Output)
	if err != nil {
		return err
	}

	for p, body := range files {
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), dirMode); err != nil {
			return err
		}

		if err := os.WriteFile(dst, body, fileMode); err != nil {
			return err
		}
	}

	return nil
}

// outputSchema is a contract as the agent gets it, without its "$schema" keyword: Claude Code's
// structured output knows only its default draft and refuses a schema that names another
// (2020-12 contracts would fail every run). goald still validates the output against the full
// contract.
func outputSchema(files fs.FS, src string) ([]byte, error) {
	body, err := fs.ReadFile(files, src)
	if err != nil {
		return nil, err
	}

	var schema map[string]json.RawMessage
	if err := json.Unmarshal(body, &schema); err != nil {
		return nil, fmt.Errorf("%s is not a JSON object: %w", src, err)
	}

	delete(schema, "$schema")

	return json.Marshal(schema)
}

// readTree adds every file under root to out, at its path below root joined to dst.
func readTree(files fs.FS, root, dst string, out map[string][]byte) error {
	return fs.WalkDir(files, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		body, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}

		out[path.Join(dst, strings.TrimPrefix(strings.TrimPrefix(p, root), "/"))] = body

		return nil
	})
}
