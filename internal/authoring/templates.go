package authoring

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
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

// TemplateCatalog is the bucket's template catalog. goald reads it (Service.Templates, Template
// and the starts of CreateFactory) and lists it every templateTTL; a template version never
// changes, so what it read of one is kept: a cache of the bucket, not state. The operator writes
// it with Push (goal catalog templates push). A version is listed once its template.yaml is
// there, which Push writes last.
type TemplateCatalog struct {
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

// NewTemplateCatalog returns the catalog in objects.
func NewTemplateCatalog(objects ports.ObjectStore, clock ports.Clock) *TemplateCatalog {
	return &TemplateCatalog{objects: objects, clock: clock, read: map[string]Template{}, files: map[string]map[string][]byte{}}
}

func templateKey(name string, version int) string { return name + "@" + strconv.Itoa(version) }

// list refreshes the index when it is older than templateTTL. The caller holds mu.
func (x *TemplateCatalog) list(ctx context.Context) error {
	if x.objects == nil || (x.versions != nil && x.clock.Now().Sub(x.listedAt) < templateTTL) {
		return nil
	}

	objs, err := x.objects.List(ctx, TemplatesPrefix)
	if err != nil {
		return fmt.Errorf("list templates: %w", err)
	}

	versions, keys, complete := map[string][]int{}, map[string][]string{}, map[string]bool{}

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
		complete[k] = complete[k] || file == templateMeta
	}

	x.versions, x.keys, x.listedAt = completeVersions(versions, complete), keys, x.clock.Now()

	return nil
}

// completeVersions keeps the versions whose template.yaml is there, ascending: one without it
// is still being pushed, or its push failed.
func completeVersions(versions map[string][]int, complete map[string]bool) map[string][]int {
	for name, vs := range versions {
		vs = slices.DeleteFunc(vs, func(v int) bool { return !complete[templateKey(name, v)] })
		if len(vs) == 0 {
			delete(versions, name)

			continue
		}

		slices.Sort(vs)
		versions[name] = vs
	}

	return versions
}

// resolve returns the version of name to read: the latest when version is 0.
func (x *TemplateCatalog) resolve(ctx context.Context, name string, version int) (int, error) {
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
func (x *TemplateCatalog) template(ctx context.Context, name string, version int) (Template, map[string][]byte, error) {
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
	return s.templates.List(ctx)
}

// List returns the latest version of every template, by name.
func (x *TemplateCatalog) List(ctx context.Context) ([]TemplateSummary, error) {
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

// TemplateUpload is one version of a template for the catalog, as the operator's folder holds
// it: template.yaml beside the definition under factory/.
type TemplateUpload struct {
	Name    string
	Version int
	// Meta is template.yaml: title (required), summary, description, connectors, changed.
	Meta []byte
	// Files is the definition by path, factory.yaml at its root.
	Files map[string][]byte
}

// ErrTemplateExists refuses a version the catalog has already: a template version never changes.
var ErrTemplateExists = errors.New("template version exists")

// Problems of a template Push refuses.
const (
	CheckTemplate          = "template"
	CodeTemplateMeta       = "template-meta-invalid"
	CodeTemplateID         = "template-id"
	CodeTemplateCredential = "template-credential" //nolint:gosec // a problem code, not a credential
)

// TemplateInvalidError is a template Push refuses. Problems are located in the template's
// folder: template.yaml, factory/factory.yaml, factory/agents/….
type TemplateInvalidError struct {
	Problems []domain.DefinitionProblem
}

func (e *TemplateInvalidError) Error() string {
	return fmt.Sprintf("the template has %d problems", len(e.Problems))
}

var templateName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// maxTemplateName bounds a template's name.
const maxTemplateName = 63

// Push adds one version of a template to the catalog (B §7, goal catalog templates push). It
// refuses a name that is not lowercase words joined by dashes, a version below 1
// (ErrInvalidRequest), files outside the path rules or limits, a template.yaml without a title,
// a definition with error problems, a metadata.id other than the nil UUID and any credential:
// reference (*TemplateInvalidError: templates never include credentials), and a version the
// catalog has (ErrTemplateExists). Files are written once each, template.yaml last, so the
// version is listed only when it is complete.
func (x *TemplateCatalog) Push(ctx context.Context, t TemplateUpload) error {
	if len(t.Name) > maxTemplateName || !templateName.MatchString(t.Name) || t.Version < 1 {
		return fmt.Errorf("template %q version %d: a name is lowercase words joined by dashes, a version a number from 1: %w",
			t.Name, t.Version, ErrInvalidRequest)
	}

	if err := checkFiles(t.Files); err != nil {
		return err
	}

	if problems := templateProblems(t); len(problems) > 0 {
		return &TemplateInvalidError{Problems: problems}
	}

	base := TemplatesPrefix + t.Name + "/" + strconv.Itoa(t.Version) + "/"

	existing, err := x.objects.List(ctx, base)
	if err != nil {
		return fmt.Errorf("list %s: %w", base, err)
	}

	if len(existing) > 0 {
		return fmt.Errorf("template %s v%d: %w", t.Name, t.Version, ErrTemplateExists)
	}

	for _, p := range slices.Sorted(maps.Keys(t.Files)) {
		if err := x.putNew(ctx, base+templateFiles+p, t.Files[p]); err != nil {
			return err
		}
	}

	return x.putNew(ctx, base+templateMeta, t.Meta)
}

// putNew writes an object of a version being pushed: ErrTemplateExists when another push wrote
// it first.
func (x *TemplateCatalog) putNew(ctx context.Context, key string, body []byte) error {
	_, created, err := x.objects.PutIfAbsent(ctx, key, body)
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}

	if !created {
		return fmt.Errorf("%s: %w", key, ErrTemplateExists)
	}

	return nil
}

// templateProblems are the reasons a template may not join the catalog.
func templateProblems(t TemplateUpload) []domain.DefinitionProblem {
	var out []domain.DefinitionProblem

	var meta templateMetaFile

	dec := yaml.NewDecoder(bytes.NewReader(t.Meta))
	dec.KnownFields(true)

	switch err := dec.Decode(&meta); {
	case errors.Is(err, io.EOF):
		out = append(out, metaProblem(templateMeta+" is empty; it needs a title"))
	case err != nil:
		out = append(out, metaProblem(fmt.Sprintf("%s: %v", templateMeta, err)))
	case strings.TrimSpace(meta.Title) == "":
		out = append(out, metaProblem(templateMeta+" has no title"))
	}

	f, problems := config.Check((&tree{manifest: domain.Manifest{}, data: map[string][]byte{}}).with(t.Files, nil))
	for _, p := range problems {
		if p.Blocking() {
			out = append(out, inFactoryDir(p))
		}
	}

	if f == nil {
		return out
	}

	if f.Metadata.ID != nilFactoryID {
		out = append(out, inFactoryDir(f.Locate("/metadata/id").Problem(CheckTemplate, CodeTemplateID, domain.SeverityError,
			"a template's metadata.id is "+nilFactoryID+"; each factory made from it gets its own")))
	}

	for _, u := range f.SecretUses() {
		if strings.HasPrefix(u.Ref, config.CredentialPrefix) {
			out = append(out, inFactoryDir(f.Locate(u.Doc).Problem(CheckTemplate, CodeTemplateCredential, domain.SeverityError,
				fmt.Sprintf("%s names %s; templates never include credentials, people choose theirs", u.Path, u.Ref))))
		}
	}

	return out
}

func metaProblem(msg string) domain.DefinitionProblem {
	return domain.DefinitionProblem{
		Code: CodeTemplateMeta, Severity: domain.SeverityError, Check: CheckTemplate, Section: domain.SectionFiles,
		File: templateMeta, Message: msg,
	}
}

// inFactoryDir places a definition's problem in the template's folder.
func inFactoryDir(p domain.DefinitionProblem) domain.DefinitionProblem {
	if p.File != "" {
		p.File = templateFiles + p.File
	}

	return p
}
