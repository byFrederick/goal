package authoring_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The bake-off arms of the tests.
const (
	armModel   = "claude-haiku-5-5"
	otherModel = "claude-sonnet-5-5"
)

func TestService_SetAgentModel(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := h.published(t)

	v1, err := h.svc.Version(h.ctx, f, authoring.VersionSelector{Kind: authoring.SelectCurrent})
	if err != nil {
		t.Fatal(err)
	}

	// No draft is open: it opens one from the current version and writes the model there.
	if err := h.svc.SetAgentModel(h.ctx, f, v1.ID, "engineer", armModel); err != nil {
		t.Fatalf("set the model with no draft open: %v", err)
	}

	if doc := h.draftFile(t, f, yamlFile); !strings.Contains(doc, "model: "+armModel) || !strings.Contains(doc, "# The one engineer.") {
		t.Errorf("draft's factory.yaml lacks the model or lost a comment:\n%s", doc)
	}

	// Never published: the current version is still v1.
	if cur, err := h.svc.Version(h.ctx, f, authoring.VersionSelector{Kind: authoring.SelectCurrent}); err != nil || cur.ID != v1.ID {
		t.Errorf("current version = %v (%v), want v1 %s", cur.ID, err, v1.ID)
	}

	// The open draft's entry differs only by the model it wrote: another arm replaces it.
	if err := h.svc.SetAgentModel(h.ctx, f, v1.ID, "engineer", otherModel); err != nil {
		t.Fatalf("set another model on the open draft: %v", err)
	}

	if doc := h.draftFile(t, f, yamlFile); !strings.Contains(doc, "model: "+otherModel) || strings.Contains(doc, armModel) {
		t.Errorf("draft's factory.yaml does not have only the second model:\n%s", doc)
	}

	// The agent's prompt changed in the draft since v1: a conflict, and the draft is left alone.
	if _, err := h.svc.PutFile(h.ctx, f, authoring.FileWrite{Path: promptFile, Content: []byte("Build it twice.\n")}); err != nil {
		t.Fatal(err)
	}

	err = h.svc.SetAgentModel(h.ctx, f, v1.ID, "engineer", armModel)
	if !errors.Is(err, ports.ErrConflict) || authoring.ProblemCode(err) != authoring.CodeDraftConflict {
		t.Errorf("set the model after the agent changed = %v (code %q), want ErrConflict and draft-conflict", err, authoring.ProblemCode(err))
	}

	if doc := h.draftFile(t, f, yamlFile); !strings.Contains(doc, "model: "+otherModel) {
		t.Errorf("a refused write changed the draft:\n%s", doc)
	}

	// An agent the version does not have is refused as a bad request.
	if err := h.svc.SetAgentModel(h.ctx, f, v1.ID, "reviewer", armModel); !errors.Is(err, authoring.ErrInvalidRequest) {
		t.Errorf("set the model of an agent v1 lacks = %v, want ErrInvalidRequest", err)
	}
}
