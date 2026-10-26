package definitions

import (
	"context"
	"fmt"
	"path"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/core/domain"
)

// Rules returns the factory rules of definition version id that apply to run (ADR 0014), in
// declaration order, as the gateway serves them in the run bundle (GET /run): each with the hash
// of its rule file in the version's manifest, its paths and its content, the file the worker
// writes to the agent's rules. A run therefore gets the rules of the version it started on.
func (c *Cache) Rules(ctx context.Context, id domain.DefinitionVersionID, run domain.RuleTarget) ([]domain.RunRule, error) {
	v, err := c.version(ctx, id)
	if err != nil {
		return nil, err
	}

	declared := make([]domain.Rule, 0, len(v.factory.Spec.Rules))
	hashes := make(map[string]domain.BlobHash, len(v.factory.Spec.Rules))

	for _, r := range v.factory.Spec.Rules {
		h, ok := v.manifest[path.Clean(r.Source)]
		if !ok {
			return nil, fmt.Errorf("definition version %s: rule %s: %s is not in the manifest", id, r.Name, r.Source)
		}

		hashes[r.Name] = h
		declared = append(declared, domain.Rule{
			Name: r.Name, Description: r.Description, AppliesTo: r.AppliesTo, Paths: r.Paths, Body: r.Body,
		})
	}

	effective := domain.EffectiveRules(declared, run)
	out := make([]domain.RunRule, 0, len(effective))

	for _, r := range effective {
		content, err := ruleContent(r)
		if err != nil {
			return nil, fmt.Errorf("definition version %s: rule %s: %w", id, r.Name, err)
		}

		out = append(out, domain.RunRule{Name: r.Name, SHA256: hashes[r.Name], Paths: r.Paths, Content: content})
	}

	return out, nil
}

// ruleFront is the frontmatter of a rule as the run gets it: what Claude Code reads of a rule,
// without appliesTo, which goald has already applied. Its field order is the file's key order.
type ruleFront struct {
	Description string   `yaml:"description"`
	Paths       []string `yaml:"paths,omitempty"`
}

// ruleContent is r's file for the run: its frontmatter, a blank line and its body.
func ruleContent(r domain.Rule) (string, error) {
	front, err := yaml.Marshal(ruleFront{Description: r.Description, Paths: r.Paths})
	if err != nil {
		return "", fmt.Errorf("frontmatter: %w", err)
	}

	var b strings.Builder

	b.WriteString("---\n")
	b.Write(front)
	b.WriteString("---\n\n")
	b.WriteString(r.Body)

	return b.String(), nil
}
