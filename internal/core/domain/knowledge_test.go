package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestDocumentType_ByExtensionElseByContent(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"notes.md": MediaMarkdown, "README.MARKDOWN": MediaMarkdown, "page.htm": MediaHTML, "data.json": MediaJSON,
		"deploy.yml": MediaYAML, "plain.txt": MediaText,
	} {
		if got, err := DocumentType(name, []byte("x")); err != nil || got != want {
			t.Errorf("DocumentType(%s) = %q, %v; want %s", name, got, err, want)
		}
	}

	if got, err := DocumentType("LICENSE", []byte("Permission is granted")); err != nil || got != MediaText {
		t.Errorf("DocumentType of text without an extension = %q, %v", got, err)
	}

	if got, err := DocumentType("index", []byte("<!DOCTYPE html><p>x")); err != nil || got != MediaHTML {
		t.Errorf("DocumentType of HTML without an extension = %q, %v", got, err)
	}

	for name, body := range map[string]string{"report.pdf": "%PDF", "image": "\x89PNG\x00\x01", "sheet.xlsx": "PK"} {
		if _, err := DocumentType(name, []byte(body)); !errors.Is(err, ErrUnsupportedDocument) {
			t.Errorf("DocumentType(%s) = %v, want it unsupported", name, err)
		}
	}
}

func TestKnowledgeVerdict_Check(t *testing.T) {
	t.Parallel()

	agent := Actor{Kind: ActorAgent, ID: "investigator"}
	ok := KnowledgeVerdict{Document: "d", Verdict: KnowledgeHelped, Note: strings.Repeat("é", MaxVerdictNote), By: agent}

	if err := ok.Check(); err != nil {
		t.Errorf("Check of a verdict with a note of %d characters = %v", MaxVerdictNote, err)
	}

	for what, v := range map[string]KnowledgeVerdict{
		"no document":         {Verdict: KnowledgeHelped, By: agent},
		"an unknown verdict":  {Document: "d", Verdict: "meh", By: agent},
		"a note too long":     {Document: "d", Verdict: KnowledgeMisled, Note: strings.Repeat("x", MaxVerdictNote+1), By: agent},
		"a service actor":     {Document: "d", Verdict: KnowledgeHelped, By: Actor{Kind: ActorService, ID: "k"}},
		"an actor with no id": {Document: "d", Verdict: KnowledgeHelped, By: Actor{Kind: ActorUser}},
		"an item with no run": {Document: "d", Verdict: KnowledgeHelped, Item: "i", By: agent},
	} {
		if err := v.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("Check of a verdict with %s = %v, want ErrInvalid", what, err)
		}
	}
}

func TestKnowledgeUse_Check(t *testing.T) {
	t.Parallel()

	ok := KnowledgeUse{Factory: "f", Item: "i", Run: "r", Source: "s", Document: "d", Kind: KnowledgeSearched, Chunks: []int64{1}}
	if err := ok.Check(); err != nil {
		t.Errorf("Check of a search use = %v", err)
	}

	for what, u := range map[string]KnowledgeUse{
		"an unknown kind": {Factory: "f", Item: "i", Run: "r", Source: "s", Document: "d", Kind: "browse"},
		"no run":          {Factory: "f", Item: "i", Source: "s", Document: "d", Kind: KnowledgeRead},
		"no document":     {Factory: "f", Item: "i", Run: "r", Source: "s", Kind: KnowledgeRead},
		"too many chunks": {
			Factory: "f", Item: "i", Run: "r", Source: "s", Document: "d", Kind: KnowledgeSearched,
			Chunks: make([]int64, MaxUseChunks+1),
		},
	} {
		if err := u.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("Check of a use with %s = %v, want ErrInvalid", what, err)
		}
	}
}

func TestCheckUsageDays(t *testing.T) {
	t.Parallel()

	for _, d := range []int{1, DefaultUsageDays, MaxUsageDays} {
		if err := CheckUsageDays(d); err != nil {
			t.Errorf("CheckUsageDays(%d) = %v", d, err)
		}
	}

	for _, d := range []int{-1, 0, MaxUsageDays + 1} {
		if err := CheckUsageDays(d); !errors.Is(err, ErrInvalid) {
			t.Errorf("CheckUsageDays(%d) = %v, want ErrInvalid", d, err)
		}
	}
}
