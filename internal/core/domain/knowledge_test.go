package domain

import (
	"errors"
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
