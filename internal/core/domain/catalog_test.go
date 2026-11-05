package domain

import "testing"

// localMCP is the program of the tests' stdio server.
const localMCP = "local-mcp"

// stdioArg is the argument that starts it over stdio.
const stdioArg = "stdio"

// CatalogCommand takes exactly a command a template ships, in its own lines or a parameter's
// otherwise lines, and nothing else: no other argument, no fewer, no other program.
func TestCatalogCommand_OnlyTheCatalogsCommands(t *testing.T) {
	t.Parallel()

	catalog := []ToolTemplate{
		{Name: "remote", MCP: map[string]TemplateMCP{"srv": {URL: "https://mcp.example.com/"}}},
		{
			Name: "local",
			MCP:  map[string]TemplateMCP{"srv": {Command: []string{localMCP, stdioArg}}},
			Parameters: []TemplateParameter{{Name: "host", Default: "example.com", Otherwise: &TemplateLines{
				MCP: map[string]TemplateMCP{"srv": {Command: []string{localMCP, stdioArg, "--read-only"}}},
			}}},
		},
	}

	for _, tc := range []struct {
		command []string
		want    bool
	}{
		{[]string{localMCP, stdioArg}, true},
		{[]string{localMCP, stdioArg, "--read-only"}, true},
		{[]string{localMCP}, false},
		{[]string{localMCP, stdioArg, "--toolsets", "all"}, false},
		{[]string{"/usr/local/bin/" + localMCP, stdioArg}, false},
		{[]string{"sh", "-c", localMCP + " " + stdioArg}, false},
		{nil, false},
	} {
		if got := CatalogCommand(catalog, tc.command); got != tc.want {
			t.Errorf("CatalogCommand(%q) = %v; want %v", tc.command, got, tc.want)
		}
	}
}
