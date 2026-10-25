package knowledge

import (
	"errors"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
)

func TestExtract_HTMLBecomesTextWithMarkdownHeadings(t *testing.T) {
	t.Parallel()

	page := `<!doctype html><html><head><title>Runbook</title><style>h1{color:red}</style>
<script>alert("x")</script></head><body>
<h1>Deploys</h1><p>Ship   it
  on <b>Fridays</b>, never.</p>
<h2>Rollback</h2><ul><li>first</li><li>second</li></ul>
<noscript>enable js</noscript><table><tr><td>a</td><td>b</td></tr></table></body></html>`

	got, err := Extract("text/html; charset=utf-8", []byte(page))
	if err != nil {
		t.Fatal(err)
	}

	want := "# Runbook\n\n# Deploys\n\nShip it on Fridays, never.\n\n## Rollback\n\n- first\n- second\n\n| a | b"
	if strings.TrimSpace(got) != want {
		t.Errorf("Extract =\n%q\nwant\n%q", got, want)
	}

	for _, gone := range []string{"alert", "color", "enable js"} {
		if strings.Contains(got, gone) {
			t.Errorf("Extract kept %q from a script, style or noscript", gone)
		}
	}
}

func TestExtract_TextTypesAsTheyAre(t *testing.T) {
	t.Parallel()

	for _, mt := range []string{TypeText, TypeMarkdown, "text/x-markdown", TypeJSON, TypeYAML, "application/x-yaml", "text/yaml"} {
		got, err := Extract(mt, []byte("\xef\xbb\xbfkey: value\r\nother: 1\n"))
		if err != nil || got != "key: value\nother: 1\n" {
			t.Errorf("Extract(%s) = %q, %v; want the text without its byte order mark and CRs", mt, got, err)
		}
	}
}

func TestExtract_Refusals(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mt   string
		body string
		want error
	}{
		"a PDF":           {"application/pdf", "%PDF-1.7", domain.ErrUnsupportedDocument},
		"no type":         {"", "text", domain.ErrUnsupportedDocument},
		"not UTF-8":       {TypeText, "caf\xe9", domain.ErrInvalid},
		"binary in text":  {TypeMarkdown, "a\x00b", domain.ErrInvalid},
		"an Office sheet": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "PK", domain.ErrUnsupportedDocument},
	} {
		if _, err := Extract(tc.mt, []byte(tc.body)); !errors.Is(err, tc.want) {
			t.Errorf("Extract %s = %v, want %v", name, err, tc.want)
		}
	}
}

func TestTypeOf_ByExtensionElseByContent(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"notes.md": TypeMarkdown, "README.MARKDOWN": TypeMarkdown, "page.htm": TypeHTML, "data.json": TypeJSON,
		"deploy.yml": TypeYAML, "plain.txt": TypeText,
	} {
		if got, err := TypeOf(name, []byte("x")); err != nil || got != want {
			t.Errorf("TypeOf(%s) = %q, %v; want %s", name, got, err, want)
		}
	}

	if got, err := TypeOf("LICENSE", []byte("Permission is granted")); err != nil || got != TypeText {
		t.Errorf("TypeOf of text without an extension = %q, %v", got, err)
	}

	if got, err := TypeOf("index", []byte("<!DOCTYPE html><p>x")); err != nil || got != TypeHTML {
		t.Errorf("TypeOf of HTML without an extension = %q, %v", got, err)
	}

	for name, body := range map[string]string{"report.pdf": "%PDF", "image": "\x89PNG\x00\x01", "sheet.xlsx": "PK"} {
		if _, err := TypeOf(name, []byte(body)); !errors.Is(err, domain.ErrUnsupportedDocument) {
			t.Errorf("TypeOf(%s) = %v, want it unsupported", name, err)
		}
	}
}
