// Package catalog reads the tool templates goald ships (ADR 0037 decision 15), the catalog of
// Add a tool: each template file (internal/catalog/*.yaml) says what one MCP server, command-line tool
// or API adds to an environment file, with {{slot}} secret slots, a suggested secret name per
// slot, non-secret parameters such as a host and an optional HTTP test. GET /v1/catalog lists them; nothing in the organization changes
// when one is applied.
package catalog

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/core/domain"
)

// MaxBytes bounds a template file.
const MaxBytes = 64 << 10

// The header every template file has.
const (
	apiVersion   = "goal/v1"
	templateKind = "Template"
)

// defaultExpect is a test's expected status when the template names none.
const defaultExpect = 200

//go:embed *.yaml
var files embed.FS

// document is a template file.
type document struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name        string `yaml:"name"`
		Title       string `yaml:"title"`
		Description string `yaml:"description"`
	} `yaml:"metadata"`
	Spec struct {
		Secrets []struct {
			Slot        string `yaml:"slot"`
			Title       string `yaml:"title"`
			Description string `yaml:"description"`
			Suggest     string `yaml:"suggest"`
		} `yaml:"secrets"`
		Parameters []struct {
			Name        string `yaml:"name"`
			Title       string `yaml:"title"`
			Description string `yaml:"description"`
			Default     string `yaml:"default"`
			Otherwise   *lines `yaml:"otherwise"`
		} `yaml:"parameters"`
		lines `yaml:",inline"`
	} `yaml:"spec"`
}

// lines are what a template adds: its spec's own, or a parameter's otherwise.
type lines struct {
	Tools []string            `yaml:"tools"`
	Env   map[string]string   `yaml:"env"`
	Files map[string]string   `yaml:"files"`
	MCP   map[string]mcpEntry `yaml:"mcp"`
	Test  *testEntry          `yaml:"test"`
}

type mcpEntry struct {
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
	Command []string          `yaml:"command"`
	Env     map[string]string `yaml:"env"`
	Tools   map[string]string `yaml:"tools"`
}

type testEntry struct {
	Method  string            `yaml:"method"`
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
	Basic   *struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"basic"`
	Expect int `yaml:"expect"`
}

// Parse reads one template file. A file with an unknown key, another apiVersion or kind, or a
// template that breaks domain.ToolTemplate.Check is an error wrapping domain.ErrInvalid.
func Parse(data []byte) (domain.ToolTemplate, error) {
	if len(data) > MaxBytes {
		return domain.ToolTemplate{}, fmt.Errorf("a template has at most %d bytes: %w", MaxBytes, domain.ErrInvalid)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc document
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return domain.ToolTemplate{}, fmt.Errorf("template: %w: %w", err, domain.ErrInvalid)
	}

	if doc.APIVersion != apiVersion || doc.Kind != templateKind {
		return domain.ToolTemplate{}, fmt.Errorf("template %s: apiVersion %s and kind %s are required: %w",
			doc.Metadata.Name, apiVersion, templateKind, domain.ErrInvalid)
	}

	own := doc.Spec.toDomain()
	t := domain.ToolTemplate{
		Name: doc.Metadata.Name, Title: doc.Metadata.Title, Description: strings.TrimSpace(doc.Metadata.Description),
		Tools: own.Tools, Env: own.Env, Files: own.Files, MCP: own.MCP, Test: own.Test,
	}

	for _, s := range doc.Spec.Secrets {
		t.Secrets = append(t.Secrets, domain.TemplateSecret{Slot: s.Slot, Title: s.Title, Description: s.Description, Suggest: s.Suggest})
	}

	for _, p := range doc.Spec.Parameters {
		param := domain.TemplateParameter{Name: p.Name, Title: p.Title, Description: strings.TrimSpace(p.Description), Default: p.Default}
		if p.Otherwise != nil {
			param.Otherwise = new(p.Otherwise.toDomain())
		}

		t.Parameters = append(t.Parameters, param)
	}

	return t, t.Check()
}

// toDomain is l as the domain has it, with a test's expected status defaulted.
func (l lines) toDomain() domain.TemplateLines {
	out := domain.TemplateLines{Tools: l.Tools, Env: l.Env, Files: l.Files}

	if len(l.MCP) > 0 {
		out.MCP = make(map[string]domain.TemplateMCP, len(l.MCP))
		for name, m := range l.MCP {
			out.MCP[name] = domain.TemplateMCP{URL: m.URL, Headers: m.Headers, Command: m.Command, Env: m.Env, Tools: m.Tools}
		}
	}

	if tt := l.Test; tt != nil {
		out.Test = &domain.TemplateTest{Method: tt.Method, URL: tt.URL, Headers: tt.Headers, Expect: tt.Expect}
		if out.Test.Expect == 0 {
			out.Test.Expect = defaultExpect
		}

		if tt.Basic != nil {
			out.Test.Basic = &domain.TemplateBasicAuth{Username: tt.Basic.Username, Password: tt.Basic.Password}
		}
	}

	return out
}

// Templates are the templates goald ships, by name. A file that does not parse, or whose name is
// not its file name, fails them all.
func Templates() ([]domain.ToolTemplate, error) {
	names, err := fs.Glob(files, "*.yaml")
	if err != nil {
		return nil, err
	}

	out := make([]domain.ToolTemplate, 0, len(names))

	for _, name := range names {
		data, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}

		t, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("catalog template %s: %w", name, err)
		}

		if want := strings.TrimSuffix(path.Base(name), ".yaml"); t.Name != want {
			return nil, fmt.Errorf("catalog template %s is named %q", name, t.Name)
		}

		out = append(out, t)
	}

	slices.SortFunc(out, func(a, b domain.ToolTemplate) int { return strings.Compare(a.Name, b.Name) })

	return out, nil
}
