package knowledge

import (
	"errors"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
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

	for _, mt := range []string{domain.MediaText, domain.MediaMarkdown, "text/x-markdown", domain.MediaJSON, domain.MediaYAML, "application/x-yaml", "text/yaml"} {
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
		"not UTF-8":       {domain.MediaText, "caf\xe9", domain.ErrInvalid},
		"binary in text":  {domain.MediaMarkdown, "a\x00b", domain.ErrInvalid},
		"an Office sheet": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "PK", domain.ErrUnsupportedDocument},
	} {
		if _, err := Extract(tc.mt, []byte(tc.body)); !errors.Is(err, tc.want) {
			t.Errorf("Extract %s = %v, want %v", name, err, tc.want)
		}
	}
}
