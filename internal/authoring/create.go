package authoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/factorymodel"
	"github.com/byFrederick/goal/internal/ports"
)

// StartKind is how a new factory starts.
type StartKind string

// Start kinds.
const (
	StartBlank    StartKind = "blank"
	StartTemplate StartKind = "template"
	StartClone    StartKind = "clone"
	StartImport   StartKind = "import"
)

// Start is where a new factory's first draft comes from.
type Start struct {
	Kind StartKind
	// Template and TemplateVersion pick a catalog template (the latest version when 0).
	Template        string
	TemplateVersion int
	// Factory (an id or name of the same organization) and Version pick the version a clone
	// copies: the current one when zero, the draft of a factory never published.
	Factory string
	Version VersionSelector
	// Files are an import's files, path → content; KeepID keeps factory.yaml's metadata.id
	// (goal factory push of a factory the organization lacks).
	Files  map[string][]byte
	KeepID bool
}

// NewFactoryRequest creates a factory.
type NewFactoryRequest struct {
	// Name is required except for an import, where factory.yaml's is used when it is empty.
	Name  string
	Title string
	Start Start
}

// Created is a new factory and its first draft.
type Created struct {
	Factory ports.FactoryRecord
	Draft   domain.DefinitionVersion
}

// Defaults of the blank skeleton: the runner and model auth of a laptop install.
const (
	skeletonRunner = "docker"
	skeletonAuth   = "claude-subscription"
	nilFactoryID   = "00000000-0000-0000-0000-000000000000"
)

// CreateFactory creates a factory with its first draft (B §8.1): from the blank template (or,
// when the catalog has none, a skeleton naming no station or agent), a catalog template, a
// clone of another factory's version, or imported files. factory.yaml's metadata.id becomes a
// new id (kept only with KeepID), and its name and title the request's, edited in place so
// comments survive; connection credential keys are dropped. Clones share the source's blobs and
// copy no items, runs or history. Errors: ErrNameTaken, ErrTemplateNotFound,
// ErrFactoryNotFound, ErrImportInvalid, ErrFactoryExists, ErrInvalidRequest.
func (s *Service) CreateFactory(ctx context.Context, r NewFactoryRequest) (Created, error) {
	by, err := actor(ctx)
	if err != nil {
		return Created{}, err
	}

	if r.Name == "" && r.Start.Kind != StartImport {
		return Created{}, fmt.Errorf("a new factory needs a name: %w", ErrInvalidRequest)
	}

	st, err := s.start(ctx, r.Start)
	if err != nil {
		return Created{}, err
	}

	doc, m, err := s.startingModel(st.files, r.Start.Kind)
	if err != nil {
		return Created{}, err
	}

	basics, err := identity(m, r, s.newID)
	if err != nil {
		return Created{}, err
	}

	out, err := rewrite(doc, m, basics)
	if err != nil {
		return Created{}, err
	}

	set, err := s.putFiles(ctx, map[string][]byte{config.FileName: out})
	if err != nil {
		return Created{}, err
	}

	manifest := maps.Clone(st.files.manifest)
	maps.Copy(manifest, set)

	rec, d, err := s.store.CreateFactory(ctx, ports.NewFactory{
		ID: domain.FactoryID(basics.id), Name: basics.name, Title: basics.title, Manifest: manifest, Source: st.source, By: by,
	})

	switch {
	case errors.Is(err, ports.ErrConflict):
		return Created{}, fmt.Errorf("factory %s: %w", basics.id, ErrFactoryExists)
	case err != nil:
		return Created{}, fmt.Errorf("create factory %s: %w", basics.name, err)
	}

	return Created{Factory: rec, Draft: d}, nil
}

// starting is the tree a new factory starts from, stored, and where it came from.
type starting struct {
	files  *tree
	source domain.VersionSource
}

func (s *Service) start(ctx context.Context, st Start) (starting, error) {
	switch st.Kind {
	case StartBlank:
		t, files, err := s.templates.template(ctx, BlankTemplate, 0)

		switch {
		case errors.Is(err, ErrTemplateNotFound):
			return s.stored(ctx, map[string][]byte{config.FileName: s.skeleton()}, domain.VersionSource{Kind: domain.SourceBlank})
		case err != nil:
			return starting{}, err
		}

		src := domain.VersionSource{Kind: domain.SourceBlank, Template: templateKey(t.Name, t.Version)}

		return s.stored(ctx, files, src)
	case StartTemplate:
		t, files, err := s.templates.template(ctx, st.Template, st.TemplateVersion)
		if err != nil {
			return starting{}, err
		}

		return s.stored(ctx, files, domain.VersionSource{Kind: domain.SourceTemplate, Template: templateKey(t.Name, t.Version)})
	case StartClone:
		return s.cloneStart(ctx, st)
	case StartImport:
		if err := checkFiles(st.Files); err != nil {
			return starting{}, err
		}

		if _, ok := st.Files[config.FileName]; !ok {
			return starting{}, fmt.Errorf("the files have no %s: %w", config.FileName, ErrImportInvalid)
		}

		kind := domain.SourceImport
		if st.KeepID {
			kind = domain.SourceCLI
		}

		return s.stored(ctx, st.Files, domain.VersionSource{Kind: kind})
	}

	return starting{}, fmt.Errorf("start %q is not blank, template, clone or import: %w", st.Kind, ErrInvalidRequest)
}

// stored puts a starting tree's files in the store; the source keeps its manifest, since no
// version holds it.
func (s *Service) stored(ctx context.Context, files map[string][]byte, src domain.VersionSource) (starting, error) {
	m, err := s.putFiles(ctx, files)
	if err != nil {
		return starting{}, err
	}

	src.Manifest = m
	t := s.tree(ctx, m).with(maps.Clone(files), nil)

	return starting{files: t, source: src}, nil
}

func (s *Service) cloneStart(ctx context.Context, st Start) (starting, error) {
	src, err := s.Factory(ctx, st.Factory)
	if err != nil {
		return starting{}, err
	}

	sel := st.Version

	switch {
	case sel.Kind != "":
	case src.Current != nil:
		sel = VersionSelector{Kind: SelectCurrent}
	default:
		sel = VersionSelector{Kind: SelectDraft}
	}

	v, err := s.selectVersion(ctx, src.ID, sel)
	if err != nil {
		return starting{}, err
	}

	if v == nil {
		return starting{}, fmt.Errorf("clone %s of %s: %w", sel, src.Name, ErrVersionNotFound)
	}

	return starting{
		files:  s.tree(ctx, v.Manifest),
		source: domain.VersionSource{Kind: domain.SourceClone, Factory: src.ID, Version: v.ID},
	}, nil
}

// skeleton is the blank definition when the catalog has no blank template: apiVersion, kind,
// metadata and the runtime only, since a station or agent name in Go would make the code
// define factories. The Line section starts empty.
func (s *Service) skeleton() []byte {
	return []byte("apiVersion: goal/v1\nkind: Factory\nmetadata:\n  id: " + nilFactoryID + "\n  name: new-factory\n" +
		"spec:\n  runtime:\n    runner: " + skeletonRunner + "\n    image: " + strconv.Quote(s.image) +
		"\n    auth: " + skeletonAuth + "\n")
}

// startingModel reads the starting tree's factory.yaml; one that is missing or does not parse
// is ErrImportInvalid for an import.
func (s *Service) startingModel(t *tree, kind StartKind) ([]byte, Model, error) {
	doc, ok, err := t.file(config.FileName)
	if err != nil {
		return nil, Model{}, err
	}

	m, _, perr := factorymodel.FromYAML(doc, nil)

	switch {
	case (!ok || perr != nil) && kind == StartImport:
		return nil, Model{}, fmt.Errorf("%s: %w", config.FileName, errors.Join(ErrImportInvalid, perr))
	case !ok:
		return nil, Model{}, fmt.Errorf("the starting tree has no %s", config.FileName)
	case perr != nil:
		return nil, Model{}, fmt.Errorf("read the starting %s: %w", config.FileName, perr)
	}

	return doc, m, nil
}

type newIdentity struct {
	id, name, title string
}

// identity is the new factory's id, name and title.
func identity(m Model, r NewFactoryRequest, newID func() string) (newIdentity, error) {
	str := func(k string) string {
		v, _ := m.Basics[k].(string)

		return v
	}

	out := newIdentity{id: newID(), name: r.Name, title: r.Title}

	if r.Start.KeepID {
		id, err := uuid.Parse(str("id"))
		if err != nil {
			return newIdentity{}, fmt.Errorf("metadata.id %q is not a UUID: %w", str("id"), ErrImportInvalid)
		}

		out.id = id.String()
	}

	if out.name == "" {
		out.name = str("name")
	}

	if out.name == "" {
		return newIdentity{}, fmt.Errorf("factory.yaml has no metadata.name and the request none: %w", ErrImportInvalid)
	}

	if out.title == "" {
		out.title = str("title")
	}

	return out, nil
}

// rewrite gives doc the new identity and drops every connection's credential key.
func rewrite(doc []byte, m Model, id newIdentity) ([]byte, error) {
	basics := maps.Clone(m.Basics)
	if basics == nil {
		basics = map[string]any{}
	}

	basics["id"], basics["name"] = id.id, id.name
	if id.title != "" {
		basics["title"] = id.title
	}

	out, err := applyValue(doc, Basics, basics)
	if err != nil {
		return nil, err
	}

	conns, stripped := withoutCredentials(m.Connections)
	if !stripped {
		return out, nil
	}

	return applyValue(out, Connections, conns)
}

// withoutCredentials returns connections without their credential keys, and whether any had one.
func withoutCredentials(conns []any) ([]any, bool) {
	out := make([]any, 0, len(conns))
	stripped := false

	for _, c := range conns {
		if cm, ok := c.(map[string]any); ok {
			if _, has := cm["credential"]; has {
				cm = maps.Clone(cm)
				delete(cm, "credential")
				c, stripped = cm, true
			}
		}

		out = append(out, c)
	}

	return out, stripped
}

func applyValue(doc []byte, s Section, v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", s, err)
	}

	out, err := factorymodel.Apply(doc, s, b)
	if err != nil {
		return nil, fmt.Errorf("apply %s: %w", s, err)
	}

	return out, nil
}

// FormatYAML formats a factory.yaml the way Visual saves write changed lines (Define-YAML's
// "Format"): a *ParseError when it does not parse.
func FormatYAML(doc []byte) ([]byte, error) {
	out, err := factorymodel.Format(doc)
	if err != nil {
		return nil, fmt.Errorf("format: %w", err)
	}

	return out, nil
}
