package config

import (
	"strings"
	"testing"
)

// docsType is the work type starterBlock's item has.
const docsType = "docs"

// starterBlock is a valid starter item (ADR 0032) at the root of baseFactory, whose board gets
// the work types fix and docs. Rows replace one of its lines and mark it with "#here".
const starterBlock = `starter:
  item:
    title: Add a contributing guide to the README
    type: docs
    body: |
      Add a short **Contributing** section.
`

// withStarter is baseFactory with work types and starterBlock, after the given replacements of
// the block.
func withStarter(pairs ...string) string {
	return edited("  board:\n", "  board:\n    workTypes: [fix, docs]\n") + replaced(starterBlock, pairs...)
}

func TestLoad_ParsesStarter(t *testing.T) {
	t.Parallel()

	f, err := Load(checkTree(withStarter(), nil))
	if err != nil {
		t.Fatal(err)
	}

	want := StarterItem{
		Title: "Add a contributing guide to the README", Type: docsType, Body: "Add a short **Contributing** section.\n",
	}
	if f.Starter == nil || f.Starter.Item != want {
		t.Errorf("starter = %+v, want item %+v", f.Starter, want)
	}

	if f, err := Load(checkTree(baseFactory, nil)); err != nil || f.Starter != nil {
		t.Errorf("a factory without a starter: starter = %+v, err = %v; want nil, nil", f.Starter, err)
	}
}

// starterRows are the rows of TestCheck_Starter: a definition and the one problem it must
// produce, none when code is "".
var starterRows = []struct {
	name    string
	doc     string
	code    string
	message []string // what the message must name
}{
	{name: "valid starter", doc: withStarter()},
	{name: "no type", doc: withStarter("    type: docs\n", "")},
	{name: "no body", doc: withStarter("    body: |\n      Add a short **Contributing** section.\n", "")},
	{
		name: "type not a work type", doc: withStarter("    type: docs\n", "    type: chore #here\n"),
		code: codeUnknownWorkType, message: []string{"starter.item.type", `"chore"`, "board.workTypes"},
	},
	{
		name: "type without work types", doc: strings.Replace(withStarter("    type: docs\n", "    type: docs #here\n"), "    workTypes: [fix, docs]\n", "", 1),
		code: codeUnknownWorkType, message: []string{`"docs"`},
	},
	{
		name: "no title", doc: withStarter("  item:\n    title: Add a contributing guide to the README\n", "  item: #here\n"),
		code: codeSchemaRequired, message: []string{"title"},
	},
	{
		name: "empty title", doc: withStarter("    title: Add a contributing guide to the README\n", "    title: \"\" #here\n"),
		code: codeSchemaInvalid, message: []string{"starter.item.title"},
	},
	{
		name: "unknown key in item", doc: withStarter("    type: docs\n", "    type: docs\n    station: work #here\n"),
		code: codeSchemaUnknownKey, message: []string{"station"},
	},
}

func TestCheck_Starter(t *testing.T) {
	t.Parallel()

	for _, tt := range starterRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checkOneProblem(t, tt.doc, tt.code, tt.message)
		})
	}
}
