package definitions

import (
	"errors"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// A proposed version serves the version's files with the proposal's written over them, under the
// version's own id, and leaves the version itself as it was.
func TestProposed_ServesTheVersionWithTheProposalsFiles(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	files := acme(acmeName, noteContract)
	v := h.publish(t, org, files)
	c := h.cache(t, nil)
	ctx := ctxOf(t, org)

	p, err := c.Proposed(ctx, v.ID, []domain.ProposalEdit{{Path: "agents/writer.md", Replacements: []domain.Replacement{{Old: files["agents/writer.md"], New: "# writer, proposed\n"}}}})
	if err != nil {
		t.Fatalf("Proposed: %v", err)
	}

	got, err := p.AgentFiles(ctx, v.ID, writer, note)
	if err != nil || string(got["prompt.md"]) != "# writer, proposed\n" {
		t.Errorf("proposed prompt = %q, %v; want the proposal's", got["prompt.md"], err)
	}

	if l := p.Line(); l.VersionID != v.ID || l.OrgID != org {
		t.Errorf("proposed line = %s of %s; want version %s of %s", l.VersionID, l.OrgID, v.ID, org)
	}

	if orig, err := c.AgentFiles(ctx, v.ID, writer, note); err != nil || string(orig["prompt.md"]) != files["agents/writer.md"] {
		t.Errorf("version prompt = %q, %v; want it unchanged", orig["prompt.md"], err)
	}

	if _, err := p.Version(ctx, "other"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Version of another id = %v; want ErrNotFound", err)
	}

	if _, err := c.Proposed(ctx, v.ID, []domain.ProposalEdit{{Path: "factory.yaml", Content: "spec: [", BaseSHA256: string(domain.HashBlob([]byte(files["factory.yaml"])))}}); err == nil {
		t.Error("Proposed with a factory.yaml that does not load = nil error; want one")
	}

	stale := []domain.ProposalEdit{{Path: "agents/writer.md", Replacements: []domain.Replacement{{Old: "not in the file", New: "x"}}}}
	if _, err := c.Proposed(ctx, v.ID, stale); !errors.Is(err, domain.ErrEditDoesNotApply) {
		t.Errorf("Proposed with an edit that does not apply = %v; want ErrEditDoesNotApply", err)
	}
}
