// Package connkind reads connection kinds (spec 4 §4.2): it parses a kind file, validates it
// against contracts/definitions/connection-kind.schema.json and the kind's own rules, and embeds the platform's
// kinds (kinds/*.yaml), which goald upserts at its start. Organizations' kinds, sent through the
// API, go through the same Parse.
package connkind

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/byFrederick/goal/contracts/definitions"
	"github.com/byFrederick/goal/internal/domain"
)

// MaxBytes bounds a kind file (00051 bounds the stored kind the same).
const MaxBytes = 64 << 10

// defaultExpect is a test's expected status when the kind names none.
const defaultExpect = 200

//go:embed kinds/*.yaml
var platformFiles embed.FS

// english prints schema messages, as the validator does by default.
var english = message.NewPrinter(language.English)

// document is a kind file.
type document struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"metadata"`
	Spec domain.KindSpec `json:"spec"`
}

// compiledSchema compiles the embedded schema once; Parse is safe for concurrent use.
var compiledSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	s, err := jsonschema.UnmarshalJSON(bytes.NewReader(definitions.ConnectionKind))
	if err != nil {
		return nil, err
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("connection-kind.schema.json", s); err != nil {
		return nil, err
	}

	return c.Compile("connection-kind.schema.json")
})

// Parse reads one kind file, YAML or JSON. A file that breaks the schema or the kind's rules
// (domain.ConnectionKind.Check) is an error wrapping domain.ErrInvalid that names every broken
// rule.
func Parse(data []byte) (domain.ConnectionKind, error) {
	if len(data) > MaxBytes {
		return domain.ConnectionKind{}, fmt.Errorf("a connection kind has at most %d bytes: %w", MaxBytes, domain.ErrInvalid)
	}

	var tree any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return domain.ConnectionKind{}, fmt.Errorf("connection kind: %w: %w", err, domain.ErrInvalid)
	}

	raw, err := json.Marshal(tree)
	if err != nil {
		return domain.ConnectionKind{}, fmt.Errorf("connection kind: %w: %w", err, domain.ErrInvalid)
	}

	if err := validate(raw); err != nil {
		return domain.ConnectionKind{}, err
	}

	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return domain.ConnectionKind{}, fmt.Errorf("connection kind: %w: %w", err, domain.ErrInvalid)
	}

	if doc.Spec.Test != nil && doc.Spec.Test.Expect == 0 {
		doc.Spec.Test.Expect = defaultExpect
	}

	k := domain.ConnectionKind{Name: doc.Metadata.Name, Title: doc.Metadata.Title, Description: doc.Metadata.Description, Spec: doc.Spec}

	return k, k.Check()
}

// validate checks raw against the schema; the error lists each failed rule with its path.
func validate(raw []byte) error {
	compiled, err := compiledSchema()
	if err != nil {
		return fmt.Errorf("connection kind schema: %w", err)
	}

	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("connection kind: %w: %w", err, domain.ErrInvalid)
	}

	err = compiled.Validate(v)

	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return err
	}

	var msgs []string

	for _, l := range leaves(ve) {
		if !slices.Contains(msgs, l) {
			msgs = append(msgs, l)
		}
	}

	return fmt.Errorf("connection kind: %s: %w", strings.Join(msgs, "; "), domain.ErrInvalid)
}

// leaves are the failed rules below e, each with its path.
func leaves(e *jsonschema.ValidationError) []string {
	if len(e.Causes) == 0 {
		where := "/" + strings.Join(e.InstanceLocation, "/")

		return []string{where + ": " + e.ErrorKind.LocalizedString(english)}
	}

	var out []string
	for _, c := range e.Causes {
		out = append(out, leaves(c)...)
	}

	return out
}

// Platform returns the kinds goald ships, by name. Every one is marked Platform.
func Platform() ([]domain.ConnectionKind, error) {
	names, err := fs.Glob(platformFiles, "kinds/*.yaml")
	if err != nil {
		return nil, err
	}

	out := make([]domain.ConnectionKind, 0, len(names))

	for _, name := range names {
		data, err := platformFiles.ReadFile(name)
		if err != nil {
			return nil, err
		}

		k, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("platform kind %s: %w", name, err)
		}

		if want := strings.TrimSuffix(path.Base(name), ".yaml"); k.Name != want {
			return nil, fmt.Errorf("platform kind %s is named %q", name, k.Name)
		}

		k.Platform = true
		out = append(out, k)
	}

	return out, nil
}
