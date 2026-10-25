package confluence

import (
	"runtime/debug"
	"strings"
	"testing"
)

// TestToMarkdown covers how a page's storage format becomes the Markdown the knowledge base
// indexes: the title on top and the page's headings under it, paragraphs, lists, tables, code,
// links, the macros with a body kept and the others dropped.
func TestToMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		storage string
		want    string
	}{
		{
			name:    "headings under the title, paragraphs, inline code, entities and whitespace",
			storage: "<h1>Deploy</h1>\n<p>Run <code>task deploy</code> from a clean <strong>main</strong>.</p><h2>Roll&nbsp;back</h2><p>Revert   the\n merge &amp; tell &lt;ops&gt;&rsquo;s channel.</p><h6>Deepest</h6>",
			want:    "# Runbook\n\n## Deploy\n\nRun `task deploy` from a clean main.\n\n### Roll back\n\nRevert the merge & tell <ops>’s channel.\n\n###### Deepest\n",
		},
		{
			name:    "nested and numbered lists, line breaks",
			storage: "<ul><li>One</li><li><p>Two</p><ul><li>Two a<br />continued</li></ul></li></ul><ol><li>First</li><li><p>Second</p><ol><li>Inner</li></ol></li></ol>",
			want:    "# Runbook\n\n- One\n- Two\n  - Two a\n    continued\n\n1. First\n2. Second\n   1. Inner\n",
		},
		{
			name: "a table with its header row, pipes escaped and short rows padded",
			storage: `<table data-layout="default"><colgroup><col /><col /></colgroup><tbody>` +
				`<tr><th><p>Service</p></th><th><p>Owner</p></th></tr>` +
				`<tr><td><p>api</p></td><td><p>Team <em>A</em> | B</p><p>on call</p></td></tr>` +
				`<tr><td><p>web</p></td></tr></tbody></table>`,
			want: "# Runbook\n\n| Service | Owner |\n| --- | --- |\n| api | Team A \\| B on call |\n| web |  |\n",
		},
		{
			name: "the code macro with its language, noformat and pre; a fence longer than the code's",
			storage: `<ac:structured-macro ac:name="code" ac:schema-version="1" ac:macro-id="m1"><ac:parameter ac:name="language">go</ac:parameter>` +
				`<ac:parameter ac:name="title">main.go</ac:parameter><ac:plain-text-body><![CDATA[func main() {
	fmt.Println("<hi> & bye")
}]]></ac:plain-text-body></ac:structured-macro>` +
				`<ac:structured-macro ac:name="noformat"><ac:plain-text-body><![CDATA[a  b` + "\n```" + `]]></ac:plain-text-body></ac:structured-macro>` +
				"<pre>line 1\n  line 2</pre>",
			want: "# Runbook\n\n```go\nfunc main() {\n\tfmt.Println(\"<hi> & bye\")\n}\n```\n\n````\na  b\n```\n````\n\n```\nline 1\n  line 2\n```\n",
		},
		{
			name: "links: web links kept, page links as their text, script links dropped",
			storage: `<p>See <a href="https://example.com/runbook">the runbook</a>, ` +
				`<ac:link><ri:page ri:content-title="Paging" /><ac:plain-text-link-body><![CDATA[how to page]]></ac:plain-text-link-body></ac:link>, ` +
				`<ac:link><ri:page ri:content-title="Escalation" ri:space-key="SUP" /></ac:link>, ` +
				`<ac:link><ri:attachment ri:filename="plan.pdf" /><ac:link-body>the <em>plan</em></ac:link-body></ac:link>, ` +
				`<ac:link><ri:url ri:value="https://status.example.com" /></ac:link> and ` +
				`<a href="javascript:alert(1)">this</a> or <a href="/wiki/spaces/ENG">home</a>.</p>`,
			want: "# Runbook\n\nSee [the runbook](https://example.com/runbook), how to page, Escalation, the plan, " +
				"[https://status.example.com](https://status.example.com) and this or home.\n",
		},
		{
			name: "macros with a body keep it and their title; the others are dropped, inline ones without breaking the paragraph",
			storage: `<ac:structured-macro ac:name="toc"><ac:parameter ac:name="maxLevel">3</ac:parameter></ac:structured-macro>` +
				`<ac:structured-macro ac:name="info"><ac:parameter ac:name="title">Heads up</ac:parameter><ac:rich-text-body><p>Freeze on Fridays.</p></ac:rich-text-body></ac:structured-macro>` +
				`<ac:structured-macro ac:name="expand"><ac:rich-text-body><p>Details.</p></ac:rich-text-body></ac:structured-macro>` +
				`<p>Status <ac:structured-macro ac:name="status"><ac:parameter ac:name="title">DONE</ac:parameter></ac:structured-macro> since ` +
				`<time datetime="2026-11-01" /> <ac:emoticon ac:name="smile" /><ac:image><ri:attachment ri:filename="x.png" /></ac:image>` +
				`<ac:placeholder>Type here</ac:placeholder><ac:inline-comment-marker ac:ref="c1">today</ac:inline-comment-marker>.</p>` +
				`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">OPS-1</ac:parameter></ac:structured-macro>` +
				`<ac:structured-macro ac:name="html"><ac:plain-text-body><![CDATA[<script>x()</script>]]></ac:plain-text-body></ac:structured-macro>`,
			want: "# Runbook\n\nHeads up\n\nFreeze on Fridays.\n\nDetails.\n\nStatus since 2026-11-01 today.\n",
		},
		{
			name: "task lists, layouts, quotes and the cloud editor's fallback",
			storage: `<ac:task-list><ac:task><ac:task-id>1</ac:task-id><ac:task-status>complete</ac:task-status><ac:task-body>Tag the release</ac:task-body></ac:task>` +
				`<ac:task><ac:task-id>2</ac:task-id><ac:task-status>incomplete</ac:task-status><ac:task-body>Announce it</ac:task-body></ac:task></ac:task-list>` +
				`<ac:layout><ac:layout-section ac:type="two_equal"><ac:layout-cell><p>Left</p></ac:layout-cell><ac:layout-cell><p>Right</p><hr /></ac:layout-cell></ac:layout-section></ac:layout>` +
				`<blockquote><p>Quote one</p><p>Quote two</p></blockquote>` +
				`<ac:adf-extension><ac:adf-node type="decision-list"><ac:adf-node type="decision-item"><ac:adf-content>Use Go</ac:adf-content></ac:adf-node></ac:adf-node>` +
				`<ac:adf-fallback><ul><li>Use Go</li></ul></ac:adf-fallback></ac:adf-extension>`,
			want: "# Runbook\n\n- [x] Tag the release\n- [ ] Announce it\n\nLeft\n\nRight\n\n> Quote one\n>\n> Quote two\n\n- Use Go\n",
		},
		{
			name:    "an empty page is its title",
			storage: "",
			want:    "# Runbook\n",
		},
		{
			name:    "missing end tags are tolerated",
			storage: "<p>open <strong>bold</p><p>next<br>line</p>",
			want:    "# Runbook\n\nopen bold\n\nnext\nline\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := toMarkdown("Runbook", tt.storage)
			if err != nil {
				t.Fatal(err)
			}

			if got != tt.want {
				t.Fatalf("toMarkdown =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// TestToMarkdown_NestingIsBounded: a page nested 100,000 elements deep keeps its text without
// recursing that deep, so a crafted page cannot exhaust goald's stack (here 8 MiB, which the
// unbounded recursion overflows, killing the test binary). Not parallel: the stack limit is the
// process's.
func TestToMarkdown_NestingIsBounded(t *testing.T) { //nolint:paralleltest // SetMaxStack is process-wide
	const depth = 100_000

	defer debug.SetMaxStack(debug.SetMaxStack(8 << 20))

	storage := strings.Repeat("<div>", depth) + "<p>deep</p>" + strings.Repeat("</div>", depth)

	got, err := toMarkdown("Deep", storage)
	if err != nil {
		t.Fatal(err)
	}

	if got != "# Deep\n\ndeep\n" {
		t.Fatalf("toMarkdown = %q, want the text kept", got)
	}
}
