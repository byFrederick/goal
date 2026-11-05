package domain

import "testing"

// CatalogCommand takes exactly a command a template ships, in its own lines or a parameter's
// otherwise lines, and nothing else: no other argument, no fewer, no other program.
func TestCatalogCommand_OnlyTheCatalogsCommands(t *testing.T) {
	t.Parallel()

	catalog := []ToolTemplate{
		{Name: "acme-remote", MCP: map[string]TemplateMCP{"acme": {URL: "https://mcp.example.com/"}}},
		{
			Name: "acme-local",
			MCP:  map[string]TemplateMCP{"acme": {Command: []string{"acme-mcp", "stdio"}}},
			Parameters: []TemplateParameter{{Name: "host", Default: "example.com", Otherwise: &TemplateLines{
				MCP: map[string]TemplateMCP{"acme": {Command: []string{"acme-mcp", "stdio", "--read-only"}}},
			}}},
		},
	}

	for _, tc := range []struct {
		command []string
		want    bool
	}{
		{[]string{"acme-mcp", "stdio"}, true},
		{[]string{"acme-mcp", "stdio", "--read-only"}, true},
		{[]string{"acme-mcp"}, false},
		{[]string{"acme-mcp", "stdio", "--toolsets", "all"}, false},
		{[]string{"/usr/local/bin/acme-mcp", "stdio"}, false},
		{[]string{"sh", "-c", "acme-mcp stdio"}, false},
		{nil, false},
	} {
		if got := CatalogCommand(catalog, tc.command); got != tc.want {
			t.Errorf("CatalogCommand(%q) = %v; want %v", tc.command, got, tc.want)
		}
	}
}
