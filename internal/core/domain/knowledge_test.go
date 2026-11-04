package domain

import (
	"errors"
	"testing"
	"time"
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

	if got, err := DocumentType("report.pdf", []byte("%PDF")); err != nil || got != MediaPDF {
		t.Errorf("DocumentType of a PDF = %q, %v; want it for the conversion service", got, err)
	}

	for name, body := range map[string]string{"image": "\x89PNG\x00\x01", "slides.key": "PK", "archive.zip": "PK"} {
		if _, err := DocumentType(name, []byte(body)); !errors.Is(err, ErrUnsupportedDocument) {
			t.Errorf("DocumentType(%s) = %v, want it unsupported", name, err)
		}
	}
}

func TestNewKnowledgeSource_SyncEveryDefaultOnDemandOrAtLeastTheMinimum(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		every *time.Duration
		ok    bool
	}{
		{nil, true},
		{new(SyncOnDemand), true},
		{new(MinSyncEvery), true},
		{new(24 * time.Hour), true},
		{new(time.Second), false},
		{new(MinSyncEvery - time.Second), false},
		{new(-time.Hour), false},
	} {
		n := NewKnowledgeSource{Name: "handbook", Title: "Handbook", Kind: SourceUpload, SyncEvery: tc.every}
		if err := n.Check(); (err == nil) != tc.ok || (err != nil && !errors.Is(err, ErrInvalid)) {
			t.Errorf("NewKnowledgeSource{SyncEvery: %v}.Check() = %v, want ok %t", tc.every, err, tc.ok)
		}

		if tc.every == nil {
			continue
		}

		if err := (KnowledgeSourcePatch{SyncEvery: tc.every}).Check(); (err == nil) != tc.ok {
			t.Errorf("KnowledgeSourcePatch{SyncEvery: %s}.Check() = %v, want ok %t", *tc.every, err, tc.ok)
		}
	}
}
