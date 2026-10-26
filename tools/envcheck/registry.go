package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// The files the check reads, relative to the repository root. Moving one of them changes only this
// table and the next.
const (
	registryFile   = "contracts/env.yaml"
	schemaFile     = "contracts/env.schema.json"
	referenceFile  = "docs/reference/environment.md"
	envExampleFile = ".env.example"
	wranglerFile   = "deploy/cloudflare/wrangler.jsonc"
	composeFile    = "compose.yaml"
	taskfileName   = "Taskfile.yml" // the root Taskfile and each area's it includes
)

// The readers, in the order the reference lists them.
const (
	readerGoald         = "goald"
	readerEgress        = "goal-egress"
	readerCLI           = "goal"
	readerAuth          = "auth"
	readerWorker        = "worker"
	readerControlWorker = "control-worker"
	readerCompose       = "compose"
	readerTaskfile      = "taskfile"
)

// processDirs maps a directory to the process whose code it holds: a read there must list that
// reader. Reads anywhere else (internal/) only need an entry.
var processDirs = []struct{ dir, reader string }{
	{"cmd/goald/", readerGoald},
	{"cmd/goal-egress/", readerEgress},
	{"cmd/goal/", readerCLI},
	{"apps/auth/", readerAuth},
	{"apps/worker/", readerWorker},
	{"deploy/cloudflare/", readerControlWorker},
}

// tsDirs are the TypeScript sources whose reads count.
var tsDirs = []string{"apps/auth/src/", "apps/worker/src/", "deploy/cloudflare/src/"}

// The classes of a variable.
const (
	classPublic = "public"
	classConfig = "config"
	classSecret = "secret"
)

// An entry is one variable of the registry: its name and meaning, never a value.
type entry struct {
	Name     string   `yaml:"name"`
	Readers  []string `yaml:"readers"`
	Class    string   `yaml:"class"`
	Required bool     `yaml:"required"`
	Meaning  string   `yaml:"meaning"`
	Indirect string   `yaml:"indirect,omitempty"`
}

type registry struct {
	Variables []entry `yaml:"variables"`
}

func (r registry) byName() map[string]entry {
	m := make(map[string]entry, len(r.Variables))
	for _, e := range r.Variables {
		m[e.Name] = e
	}

	return m
}

func loadRegistry(r repo) (registry, error) {
	schema, err := fs.ReadFile(r.fsys, schemaFile)
	if err != nil {
		return registry{}, fmt.Errorf("read the schema: %w", err)
	}

	src, err := fs.ReadFile(r.fsys, registryFile)
	if err != nil {
		return registry{}, fmt.Errorf("read the registry: %w", err)
	}

	return parseRegistry(schema, src)
}

// parseRegistry validates the registry's YAML against the schema, then decodes it.
func parseRegistry(schema, src []byte) (registry, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return registry{}, fmt.Errorf("parse %s: %w", schemaFile, err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaFile, doc); err != nil {
		return registry{}, fmt.Errorf("load %s: %w", schemaFile, err)
	}

	sch, err := c.Compile(schemaFile)
	if err != nil {
		return registry{}, fmt.Errorf("compile %s: %w", schemaFile, err)
	}

	var v any
	if err := yaml.Unmarshal(src, &v); err != nil {
		return registry{}, fmt.Errorf("parse %s: %w", registryFile, err)
	}

	if err := sch.Validate(v); err != nil {
		if ve, ok := errors.AsType[*jsonschema.ValidationError](err); ok {
			return registry{}, fmt.Errorf("%s does not match %s: %s", registryFile, schemaFile, strings.TrimSpace(ve.Error()))
		}

		return registry{}, fmt.Errorf("validate %s: %w", registryFile, err)
	}

	var reg registry

	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)

	if err := dec.Decode(&reg); err != nil {
		return registry{}, fmt.Errorf("decode %s: %w", registryFile, err)
	}

	return reg, nil
}

// checkOrder reports a name declared twice or out of order: the registry is sorted by name, so
// each variable has one obvious place.
func checkOrder(reg registry) []problem {
	var problems []problem

	seen := map[string]bool{}

	for i, e := range reg.Variables {
		if seen[e.Name] {
			problems = append(problems, problem{registryFile, e.Name + " is declared twice"})
		}

		seen[e.Name] = true

		if i > 0 && reg.Variables[i-1].Name > e.Name {
			problems = append(problems, problem{registryFile, e.Name + " is out of order: the variables are sorted by name"})
		}
	}

	return problems
}
