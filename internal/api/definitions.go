package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Definitions is factory definition authoring (B §8): factories, their one shared draft,
// validation, publishing, versions, compare, restore and templates. *authoring.Service satisfies
// it. Every method acts in the organization and as the actor of ctx; the API takes authoring's
// request and result types, and answers its refusals with the problem code authoring.ProblemCode
// gives them.
type Definitions interface {
	Factories(ctx context.Context) ([]ports.FactoryRecord, error)
	// Factory finds a factory of the organization by id or name, published or not.
	Factory(ctx context.Context, ref string) (ports.FactoryRecord, error)
	CreateFactory(ctx context.Context, r authoring.NewFactoryRequest) (authoring.Created, error)
	DeleteFactory(ctx context.Context, f domain.FactoryID) error
	Pause(ctx context.Context, f domain.FactoryID) (ports.FactoryRecord, error)
	Resume(ctx context.Context, f domain.FactoryID) (ports.FactoryRecord, error)
	Archive(ctx context.Context, f domain.FactoryID) (ports.FactoryRecord, error)
	Unarchive(ctx context.Context, f domain.FactoryID) (ports.FactoryRecord, error)
	Purge(ctx context.Context, f domain.FactoryID) error

	Templates(ctx context.Context) ([]authoring.TemplateSummary, error)
	Template(ctx context.Context, name string, version int) (authoring.Template, error)
	ValidateFiles(ctx context.Context, files map[string][]byte) (authoring.Validation, error)

	Draft(ctx context.Context, f domain.FactoryID) (authoring.DraftView, error)
	OpenDraft(ctx context.Context, f domain.FactoryID, r authoring.OpenDraftRequest) (authoring.DraftView, bool, error)
	DiscardDraft(ctx context.Context, f domain.FactoryID, revision int64) error
	DraftModel(ctx context.Context, f domain.FactoryID) (authoring.DraftModel, error)
	SaveSection(ctx context.Context, f domain.FactoryID, save authoring.SectionSave) (authoring.SaveResult, error)
	RevertSection(ctx context.Context, f domain.FactoryID, section authoring.Section, ifMatch string, pointers []string) (authoring.SaveResult, error)
	DraftFiles(ctx context.Context, f domain.FactoryID) ([]authoring.FileInfo, error)
	PutFile(ctx context.Context, f domain.FactoryID, w authoring.FileWrite) (authoring.SaveResult, error)
	DeleteFile(ctx context.Context, f domain.FactoryID, path string, ifMatch domain.BlobHash) (authoring.SaveResult, error)
	ReplaceTree(ctx context.Context, f domain.FactoryID, files map[string][]byte, expected *int64) (authoring.SaveResult, error)
	ValidateDraft(ctx context.Context, f domain.FactoryID, r authoring.ValidateRequest) (authoring.Validation, error)
	Publish(ctx context.Context, f domain.FactoryID, r authoring.PublishRequest) (domain.DefinitionVersion, error)

	Versions(ctx context.Context, f domain.FactoryID, before, limit int) (authoring.VersionPage, error)
	Version(ctx context.Context, f domain.FactoryID, sel authoring.VersionSelector) (authoring.VersionDetail, error)
	VersionFile(ctx context.Context, f domain.FactoryID, sel authoring.VersionSelector, path string) (authoring.File, error)
	VersionModel(ctx context.Context, f domain.FactoryID, sel authoring.VersionSelector) (authoring.Model, authoring.Ranges, error)
	Compare(ctx context.Context, f domain.FactoryID, from, to authoring.VersionSelector) (authoring.CompareResult, error)
	Restore(ctx context.Context, f domain.FactoryID, r authoring.RestoreRequest) (ports.FactoryRecord, error)
}

var _ Definitions = (*authoring.Service)(nil)

// immutableCache is how long a published version's file may be cached: forever, it never changes.
const immutableCache = "private, max-age=31536000, immutable"

// ListFactories lists the factories the caller may see: the archived ones only with
// archived=true, the others without.
func (s *server) ListFactories(ctx context.Context, req gen.ListFactoriesRequestObject) (gen.ListFactoriesResponseObject, error) {
	recs, err := s.defs.Factories(ctx)
	if err != nil {
		return nil, err
	}

	loaded := s.loaded(ctx)
	archived := deref(req.Params.Archived)

	out := gen.ListFactories200JSONResponse{Factories: make([]gen.Factory, 0, len(recs))}
	for _, rec := range recs {
		if rec.Archived() == archived && s.visible(ctx, rec.ID) {
			out.Factories = append(out.Factories, s.factoryOf(ctx, rec, loaded[rec.ID]))
		}
	}

	return out, nil
}

func (s *server) GetFactory(ctx context.Context, req gen.GetFactoryRequestObject) (gen.GetFactoryResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	return gen.GetFactory200JSONResponse(s.factoryOut(ctx, rec)), nil
}

func (s *server) CreateFactory(ctx context.Context, req gen.CreateFactoryRequestObject) (gen.CreateFactoryResponseObject, error) {
	start, err := s.start(ctx, req.Body.Start)
	if err != nil {
		return nil, err
	}

	c, err := s.defs.CreateFactory(ctx, authoring.NewFactoryRequest{
		Name: deref(req.Body.Name), Title: deref(req.Body.Title), Start: start,
	})
	if err != nil {
		return nil, err
	}

	return gen.CreateFactory201JSONResponse{
		Body:    s.factoryOut(ctx, c.Factory),
		Headers: gen.CreateFactory201ResponseHeaders{Location: "/v1/orgs/" + orgOf(ctx).Slug + "/factories/" + string(c.Factory.ID)},
	}, nil
}

// start reads where a new factory starts. A clone's source passes the resolver like any other
// factory the request names.
func (s *server) start(ctx context.Context, st gen.FactoryStart) (authoring.Start, error) {
	v, err := st.ValueByDiscriminator()
	if err != nil {
		return authoring.Start{}, fmt.Errorf("%w: start: %w", errInvalid, err)
	}

	switch v := v.(type) {
	case gen.BlankStart:
		return authoring.Start{Kind: authoring.StartBlank}, nil
	case gen.TemplateStart:
		return authoring.Start{Kind: authoring.StartTemplate, Template: v.Template, TemplateVersion: deref(v.Version)}, nil
	case gen.CloneStart:
		src, err := s.definition(ctx, v.Factory)
		if err != nil {
			return authoring.Start{}, err
		}

		sel, err := selector(deref(v.Version))
		if err != nil {
			return authoring.Start{}, err
		}

		return authoring.Start{Kind: authoring.StartClone, Factory: string(src.ID), Version: sel}, nil
	case gen.ImportStart:
		files, err := filesOf(&v.Files)
		if err != nil {
			return authoring.Start{}, err
		}

		return authoring.Start{Kind: authoring.StartImport, Files: files, KeepID: deref(v.KeepId)}, nil
	default:
		return authoring.Start{}, fmt.Errorf("%w: start kind %T", errInvalid, v)
	}
}

func (s *server) DeleteFactory(ctx context.Context, req gen.DeleteFactoryRequestObject) (gen.DeleteFactoryResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if err := s.defs.DeleteFactory(ctx, rec.ID); err != nil {
		return nil, err
	}

	return gen.DeleteFactory204Response{}, nil
}

func (s *server) PauseFactory(ctx context.Context, req gen.PauseFactoryRequestObject) (gen.PauseFactoryResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if rec, err = s.defs.Pause(ctx, rec.ID); err != nil {
		return nil, err
	}

	return gen.PauseFactory200JSONResponse(s.factoryOut(ctx, rec)), nil
}

func (s *server) ResumeFactory(ctx context.Context, req gen.ResumeFactoryRequestObject) (gen.ResumeFactoryResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if rec, err = s.defs.Resume(ctx, rec.ID); err != nil {
		return nil, err
	}

	return gen.ResumeFactory200JSONResponse(s.factoryOut(ctx, rec)), nil
}

func (s *server) ArchiveFactory(ctx context.Context, req gen.ArchiveFactoryRequestObject) (gen.ArchiveFactoryResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if rec, err = s.defs.Archive(ctx, rec.ID); err != nil {
		return nil, err
	}

	return gen.ArchiveFactory200JSONResponse(s.factoryOut(ctx, rec)), nil
}

func (s *server) RestoreFactory(ctx context.Context, req gen.RestoreFactoryRequestObject) (gen.RestoreFactoryResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if rec, err = s.defs.Unarchive(ctx, rec.ID); err != nil {
		return nil, err
	}

	return gen.RestoreFactory200JSONResponse(s.factoryOut(ctx, rec)), nil
}

func (s *server) PurgeFactory(ctx context.Context, req gen.PurgeFactoryRequestObject) (gen.PurgeFactoryResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if err := s.defs.Purge(ctx, rec.ID); err != nil {
		return nil, err
	}

	return gen.PurgeFactory204Response{}, nil
}

func (s *server) ListTemplates(ctx context.Context, _ gen.ListTemplatesRequestObject) (gen.ListTemplatesResponseObject, error) {
	list, err := s.defs.Templates(ctx)
	if err != nil {
		return nil, err
	}

	out := gen.ListTemplates200JSONResponse{Templates: make([]gen.TemplateSummary, 0, len(list))}
	for _, t := range list {
		out.Templates = append(out.Templates, templateSummaryOut(t))
	}

	return out, nil
}

func (s *server) GetTemplate(ctx context.Context, req gen.GetTemplateRequestObject) (gen.GetTemplateResponseObject, error) {
	t, err := s.defs.Template(ctx, req.Name, deref(req.Params.Version))
	if err != nil {
		return nil, err
	}

	out, err := templateOut(t)
	if err != nil {
		return nil, err
	}

	return gen.GetTemplate200JSONResponse(out), nil
}

func (s *server) ValidateDefinition(ctx context.Context, req gen.ValidateDefinitionRequestObject) (gen.ValidateDefinitionResponseObject, error) {
	files, err := filesOf(&req.Body.Files)
	if err != nil {
		return nil, err
	}

	v, err := s.defs.ValidateFiles(ctx, files)
	if err != nil {
		return nil, err
	}

	return gen.ValidateDefinition200JSONResponse(validationOut(v)), nil
}

func (s *server) FormatDefinition(_ context.Context, req gen.FormatDefinitionRequestObject) (gen.FormatDefinitionResponseObject, error) {
	doc, err := readFile(req.Body)
	if err != nil {
		return nil, err
	}

	out, err := authoring.FormatYAML(doc)
	if err != nil {
		// A document to format that does not parse is the request's fault: 422, not 409.
		return nil, fmt.Errorf("%w: %w", errUnprocessable, err)
	}

	return gen.FormatDefinition200TextyamlResponse{Body: bytes.NewReader(out), ContentLength: int64(len(out))}, nil
}

// readFile reads one definition file's content from a request body: a body over the largest
// file is too large.
func readFile(body io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(body, domain.MaxBlobSize+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %w", errInvalid, err)
	}

	if len(b) > domain.MaxBlobSize {
		return nil, fmt.Errorf("a file over %d bytes: %w", domain.MaxBlobSize, authoring.ErrTooLarge)
	}

	return b, nil
}

// definition resolves a path's factory (its id or name) among the organization's factories,
// published or not, and passes the Authorizer with it. The definitions routes reach a factory
// only through here.
func (s *server) definition(ctx context.Context, ref string) (ports.FactoryRecord, error) {
	rec, err := s.defs.Factory(ctx, ref)
	if errors.Is(err, authoring.ErrFactoryNotFound) {
		return ports.FactoryRecord{}, errFactoryNotFound
	}

	if err != nil {
		return ports.FactoryRecord{}, err
	}

	if err := s.authorize(ctx, rec.ID, errFactoryNotFound); err != nil {
		return ports.FactoryRecord{}, err
	}

	return rec, nil
}

// loaded is the organization's factories goald runs, by id: what the current versions say about
// stations and work types.
func (s *server) loaded(ctx context.Context) map[domain.FactoryID]*config.Factory {
	out := map[domain.FactoryID]*config.Factory{}
	for _, f := range s.factories.Factories(ctx) {
		out[domain.FactoryID(f.Metadata.ID)] = f
	}

	return out
}

// records is the organization's factories as the definition store keeps them, by id.
func (s *server) records(ctx context.Context) (map[domain.FactoryID]ports.FactoryRecord, error) {
	recs, err := s.defs.Factories(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[domain.FactoryID]ports.FactoryRecord, len(recs))
	for _, r := range recs {
		out[r.ID] = r
	}

	return out, nil
}

// recordOf is the store's record of a factory goald runs; one the store does not list yet (a
// read racing its registration) is named from its definition.
func recordOf(recs map[domain.FactoryID]ports.FactoryRecord, org domain.OrgID, f *config.Factory) ports.FactoryRecord {
	if r, ok := recs[domain.FactoryID(f.Metadata.ID)]; ok {
		return r
	}

	return ports.FactoryRecord{ID: domain.FactoryID(f.Metadata.ID), OrgID: org, Name: f.Metadata.Name, Title: f.Metadata.Title}
}

// factoryOut is a factory with what goald runs of it.
func (s *server) factoryOut(ctx context.Context, rec ports.FactoryRecord) gen.Factory {
	return s.factoryOf(ctx, rec, s.loaded(ctx)[rec.ID])
}

// factoryOf is a factory as the store keeps it, with the stations, work types and description of
// the version goald runs (f, nil before the first publish), and the caller's role and permissions
// on it.
func (s *server) factoryOf(ctx context.Context, rec ports.FactoryRecord, f *config.Factory) gen.Factory {
	out := gen.Factory{
		Id: string(rec.ID), OrgId: string(rec.OrgID), Name: rec.Name, Title: optional(rec.Title),
		WorkTypes: []string{}, Stations: []gen.Station{},
		Current: s.publishedRef(rec.Current), Draft: s.draftRef(rec.Draft), Paused: rec.Paused(), CreatedAt: rec.CreatedAt,
		Role: s.factoryRole(ctx, rec.ID), Can: permissionsOut(s.can(ctx, rec.ID)),
	}

	if rec.Archived() {
		by := s.actorOut(rec.ArchivedBy)
		out.ArchivedAt, out.ArchivedBy = &rec.ArchivedAt, &by
	}

	if f == nil {
		return out
	}

	out.Description = optional(f.Metadata.Description)
	if f.Spec.Board.WorkTypes != nil {
		out.WorkTypes = slices.Clone(f.Spec.Board.WorkTypes)
	}

	for _, st := range f.Spec.Board.Stations {
		out.Stations = append(out.Stations, gen.Station{
			Id: st.ID, Kind: gen.StationKind(st.Kind), Action: actionOf(st.Action), Description: optional(st.Description),
		})
	}

	return out
}

// Errors of the definitions routes that are the API's own.
var (
	// errPreconditionRequired is a write without the If-Match it needs (428): without it, it
	// would overwrite whatever another writer saved.
	errPreconditionRequired = errors.New("precondition required: send If-Match with the entity tag the write is based on")
	// errUnprocessable makes a definition refusal 422: a document sent to be formatted that does
	// not parse.
	errUnprocessable = errors.New("unprocessable")
)

// definitionStatus is the status of each problem code authoring.ProblemCode gives.
var definitionStatus = map[string]int{
	authoring.CodeFactoryNotFound:    http.StatusNotFound,
	authoring.CodeNoDraft:            http.StatusNotFound,
	authoring.CodeVersionNotFound:    http.StatusNotFound,
	authoring.CodeFileNotFound:       http.StatusNotFound,
	authoring.CodeTemplateNotFound:   http.StatusNotFound,
	authoring.CodeInvalidRequest:     http.StatusBadRequest,
	authoring.CodeDraftExists:        http.StatusConflict,
	authoring.CodeBaseMoved:          http.StatusConflict,
	authoring.CodeNameTaken:          http.StatusConflict,
	authoring.CodeFactoryExists:      http.StatusConflict,
	authoring.CodeFactoryPublished:   http.StatusConflict,
	authoring.CodeDefinitionInvalid:  http.StatusConflict,
	authoring.CodeItemsAtRemoved:     http.StatusConflict,
	authoring.CodeFactoryRunning:     http.StatusConflict,
	authoring.CodeFactoryBusy:        http.StatusConflict,
	authoring.CodeFactoryNotArchived: http.StatusConflict,
	authoring.CodeYAMLInvalid:        http.StatusConflict,
	authoring.CodeDraftConflict:      http.StatusPreconditionFailed,
	authoring.CodeModelInvalid:       http.StatusUnprocessableEntity,
	authoring.CodeImportInvalid:      http.StatusUnprocessableEntity,
	authoring.CodeBadPath:            http.StatusUnprocessableEntity,
	authoring.CodeTooLarge:           http.StatusRequestEntityTooLarge,
}

// definitionProblem is the problem of a definition refusal, with what the client needs to act on
// it: the problems that block a publish, where factory.yaml stops parsing, the current value of
// what changed under a stale write, the items at removed stations. ok is false for other errors.
func definitionProblem(err error) (gen.Problem, bool) {
	code := authoring.ProblemCode(err)

	status, ok := definitionStatus[code]
	if !ok {
		return gen.Problem{}, false
	}

	if errors.Is(err, errUnprocessable) {
		status = http.StatusUnprocessableEntity
	}

	p := detailed(status, err)
	p.Code = &code

	if e, ok := errors.AsType[*authoring.InvalidError](err); ok {
		problems := problemsOut(e.Problems)
		p.Problems = &problems
	}

	if e, ok := errors.AsType[*authoring.ParseError](err); ok {
		p.ParseError = &gen.ParseError{Line: e.Line, Column: e.Column, Message: e.Message}
	}

	if e, ok := errors.AsType[*authoring.SectionConflictError](err); ok {
		section := gen.Section(e.Section)
		p.Section, p.Value, p.Etag = &section, e.Value, &e.ETag
	}

	if e, ok := errors.AsType[*authoring.FileConflictError](err); ok {
		p.Path, p.Sha256 = &e.Path, optional(string(e.SHA256))
	}

	if e, ok := errors.AsType[*ports.ItemsAtRemovedStationError](err); ok {
		p.Items = &gen.ItemsAtRemovedStations{InProgress: itemsAtOut(e.InProgress), Unmapped: itemsAtOut(e.Unmapped)}
	}

	switch code {
	case authoring.CodeFactoryRunning:
		p.Detail = optional("Pause the factory first")
	case authoring.CodeFactoryNotArchived:
		p.Detail = optional("Archive the factory first")
	case authoring.CodeFactoryBusy:
		if e, ok := errors.AsType[*ports.FactoryBusyError](err); ok {
			p.Detail = optional(e.Error())
		}
	}

	return p, true
}
