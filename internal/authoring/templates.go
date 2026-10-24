package authoring

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/factorymodel"
	"github.com/byFrederick/goal/internal/ports"
)

// TemplatesPrefix is where the bucket keeps the template catalog:
// catalog/templates/<name>/<version>/template.yaml beside the definition under factory/.
const TemplatesPrefix = "catalog/templates/"

// BlankTemplate is the template a blank factory starts from when the catalog has it.
const BlankTemplate = "blank"

const (
	templateMeta  = "template.yaml"
	templateFiles = "factory/"
)

// TemplateSummary is one template of the catalog as New-Start lists it.
type TemplateSummary struct {
	Name    string
	Version int
	Title   string
	Summary string
	// Stations and Agents count the template's line and agents.
	Stations, Agents int
	// Connectors are what it connects to.
	Connectors []string
	// Changed is the date the version was made ("2026-10-09"), as template.yaml says.
	Changed string
}

// Template is one version of a template with what its detail shows.
type Template struct {
	TemplateSummary

	Description string
	// Model previews the line, agents and connections.
	Model Model
}

// templateMetaFile is template.yaml.
type templateMetaFile struct {
	Title       string   `yaml:"title"`
	Summary     string   `yaml:"summary"`
	Description string   `yaml:"description"`
	Connectors  []string `yaml:"connectors"`
	Changed     string   `yaml:"changed"`
}

// templateIndex lists the catalog every templateTTL. A template version never changes, so what
// it read of one is kept: a cache of the bucket, not state.
type templateIndex struct {
	objects ports.ObjectStore
	clock   ports.Clock

	mu       sync.Mutex
	listedAt time.Time
	// versions maps a name to its versions, ascending, and keys a version's objects.
	versions map[string][]int
	keys     map[string][]string
	read     map[string]Template
	files    map[string]map[string][]byte
}

func newTemplateIndex(objects ports.ObjectStore, clock ports.Clock) *templateIndex {
	return &templateIndex{objects: objects, clock: clock, read: map[string]Template{}, files: map[string]map[string][]byte{}}
}

func templateKey(name string, version int) string { return name + "@" + strconv.Itoa(version) }

// list refreshes the index when it is older than templateTTL. The caller holds mu.
func (x *templateIndex) list(ctx context.Context) error {
	if x.objects == nil || (x.versions != nil && x.clock.Now().Sub(x.listedAt) < templateTTL) {
		return nil
	}

	objs, err := x.objects.List(ctx, TemplatesPrefix)
	if err != nil {
		return fmt.Errorf("list templates: %w", err)
	}

	versions, keys := map[string][]int{}, map[string][]string{}

	for _, o := range objs {
		name, rest, _ := strings.Cut(strings.TrimPrefix(o.Key, TemplatesPrefix), "/")
		ver, file, _ := strings.Cut(rest, "/")

		v, err := strconv.Atoi(ver)
		if err != nil || v < 1 || name == "" || (file != templateMeta && !strings.HasPrefix(file, templateFiles)) {
			continue
		}

		k := templateKey(name, v)
		if _, ok := keys[k]; !ok {
			versions[name] = append(versions[name], v)
		}

		keys[k] = append(keys[k], o.Key)
	}

	for name := range versions {
		slices.Sort(versions[name])
	}

	x.versions, x.keys, x.listedAt = versions, keys, x.clock.Now()

	return nil
}

// resolve returns the version of name to read: the latest when version is 0.
func (x *templateIndex) resolve(ctx context.Context, name string, version int) (int, error) {
	if err := x.list(ctx); err != nil {
		return 0, err
	}

	vs := x.versions[name]

	switch {
	case len(vs) == 0:
	case version == 0:
		return vs[len(vs)-1], nil
	case slices.Contains(vs, version):
		return version, nil
	}

	return 0, fmt.Errorf("template %s version %d: %w", name, version, ErrTemplateNotFound)
}

// template reads one version's metadata and files.
func (x *templateIndex) template(ctx context.Context, name string, version int) (Template, map[string][]byte, error) {
	x.mu.Lock()
	defer x.mu.Unlock()

	v, err := x.resolve(ctx, name, version)
	if err != nil {
		return Template{}, nil, err
	}

	k := templateKey(name, v)
	if t, ok := x.read[k]; ok {
		return t, x.files[k], nil
	}

	var meta templateMetaFile

	files := map[string][]byte{}

	for _, key := range x.keys[k] {
		obj, err := x.objects.Get(ctx, key, "")
		if err != nil {
			return Template{}, nil, fmt.Errorf("read template %s: %w", k, err)
		}

		file := strings.TrimPrefix(key, TemplatesPrefix+name+"/"+strconv.Itoa(v)+"/")
		if file == templateMeta {
			if err := yaml.Unmarshal(obj.Body, &meta); err != nil {
				return Template{}, nil, fmt.Errorf("read template %s: %s: %w", k, templateMeta, err)
			}

			continue
		}

		files[strings.TrimPrefix(file, templateFiles)] = obj.Body
	}

	t := Template{
		TemplateSummary: TemplateSummary{
			Name: name, Version: v, Title: meta.Title, Summary: meta.Summary, Connectors: meta.Connectors, Changed: meta.Changed,
		},
		Description: meta.Description,
		Model:       Model{Connections: []any{}},
	}

	if m, _, err := factorymodel.FromYAML(files[config.FileName], nil); err == nil {
		t.Model = m
	}

	c := counts(nil, t.Model, Comparison{})
	t.Stations, t.Agents = c.Stations, c.Agents

	if t.Connectors == nil {
		t.Connectors = connectorNames(t.Model)
	}

	x.read[k], x.files[k] = t, files

	return t, files, nil
}

// connectorNames lists what a model's connections connect to.
func connectorNames(m Model) []string {
	out := []string{}

	for _, c := range m.Connections {
		cm, _ := c.(map[string]any)

		name, _ := cm["connector"].(string)
		if name == "" {
			name, _ = cm["name"].(string)
		}

		if name != "" && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}

	return out
}

// Templates lists the latest version of every template of the catalog, by name.
func (s *Service) Templates(ctx context.Context) ([]TemplateSummary, error) {
	x := s.templates

	x.mu.Lock()
	err := x.list(ctx)
	names := slices.Sorted(maps.Keys(x.versions))
	x.mu.Unlock()

	if err != nil {
		return nil, err
	}

	out := make([]TemplateSummary, 0, len(names))

	for _, n := range names {
		t, _, err := x.template(ctx, n, 0)
		if err != nil {
			return nil, err
		}

		out = append(out, t.TemplateSummary)
	}

	slices.SortFunc(out, func(a, b TemplateSummary) int { return cmp.Compare(a.Name, b.Name) })

	return out, nil
}

// Template returns one version of a template (the latest when version is 0):
// ErrTemplateNotFound when the catalog has none.
func (s *Service) Template(ctx context.Context, name string, version int) (Template, error) {
	t, _, err := s.templates.template(ctx, name, version)

	return t, err
}
