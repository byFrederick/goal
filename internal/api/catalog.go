package api

import (
	"context"
	"maps"
	"slices"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// GetCatalog answers the tool templates goald ships (ADR 0037 decision 15), by name.
func (s *server) GetCatalog(context.Context, gen.GetCatalogRequestObject) (gen.GetCatalogResponseObject, error) {
	out := gen.GetCatalog200JSONResponse{Templates: make([]gen.ToolTemplate, 0, len(s.catalog))}

	for _, t := range s.catalog {
		out.Templates = append(out.Templates, toolTemplateOut(t))
	}

	return out, nil
}

// toolTemplateOut is a tool template as the contract has it.
func toolTemplateOut(t domain.ToolTemplate) gen.ToolTemplate {
	out := gen.ToolTemplate{
		Name: t.Name, Title: t.Title, Description: t.Description, Secrets: make([]gen.TemplateSecret, 0, len(t.Secrets)),
		Adds: gen.TemplateAdds{
			Tools: append([]string{}, t.Tools...), Env: cloneMap(t.Env), Files: cloneMap(t.Files), Mcp: templateMCPOut(t.MCP),
		},
		Test: templateTestOut(t.Test),
	}

	for _, sec := range t.Secrets {
		out.Secrets = append(out.Secrets, gen.TemplateSecret{Slot: sec.Slot, Title: sec.Title, Description: sec.Description, Suggest: sec.Suggest})
	}

	if len(t.Parameters) > 0 {
		params := make([]gen.TemplateParameter, 0, len(t.Parameters))
		for _, p := range t.Parameters {
			params = append(params, templateParameterOut(p))
		}

		out.Parameters = &params
	}

	return out
}

// templateParameterOut is a template's parameter as the contract has it: its otherwise with only
// the fields it replaces.
func templateParameterOut(p domain.TemplateParameter) gen.TemplateParameter {
	out := gen.TemplateParameter{Name: p.Name, Title: p.Title, Description: p.Description, Default: p.Default}

	if o := p.Otherwise; o != nil {
		out.Otherwise = &gen.TemplateOtherwise{Test: templateTestOut(o.Test)}

		if o.Tools != nil {
			out.Otherwise.Tools = new(slices.Clone(o.Tools))
		}

		if o.Env != nil {
			out.Otherwise.Env = new(cloneMap(o.Env))
		}

		if o.Files != nil {
			out.Otherwise.Files = new(cloneMap(o.Files))
		}

		if o.MCP != nil {
			out.Otherwise.Mcp = new(templateMCPOut(o.MCP))
		}
	}

	return out
}

// templateMCPOut are a template's MCP servers as the contract has them.
func templateMCPOut(servers map[string]domain.TemplateMCP) map[string]gen.TemplateMCP {
	out := make(map[string]gen.TemplateMCP, len(servers))

	for name, m := range servers {
		o := gen.TemplateMCP{Tools: make(map[string]gen.TemplateMCPTools, len(m.Tools))}
		for tool, mode := range m.Tools {
			o.Tools[tool] = gen.TemplateMCPTools(mode)
		}

		if m.URL != "" {
			o.Url, o.Headers = &m.URL, new(cloneMap(m.Headers))
		} else {
			o.Command, o.Env = new(slices.Clone(m.Command)), new(cloneMap(m.Env))
		}

		out[name] = o
	}

	return out
}

// templateTestOut is a template's test as the contract has it, or nil.
func templateTestOut(tt *domain.TemplateTest) *gen.TemplateTest {
	if tt == nil {
		return nil
	}

	out := &gen.TemplateTest{Method: tt.Method, Url: tt.URL, Headers: cloneMap(tt.Headers), Expect: tt.Expect}
	if tt.Basic != nil {
		out.Basic = &struct {
			Password string `json:"password"`
			Username string `json:"username"`
		}{Password: tt.Basic.Password, Username: tt.Basic.Username}
	}

	return out
}

// cloneMap is m, or an empty map for nil, so the answer has {} rather than null.
func cloneMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	maps.Copy(out, m)

	return out
}
