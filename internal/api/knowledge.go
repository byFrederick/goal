package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// The organization's knowledge base (spec 4 §4.8, §8.2): members and keys list, read and search
// it (knowledge:read); owners, admins and keys add, change, sync and delete sources, upload files
// and delete them (knowledge:manage). A search here is the one agents run through the gateway.

// KnowledgeUploads keeps the files uploaded to upload sources (*knowledge.Uploads): Check refuses
// a file Upload would (ports.ErrTooLarge, domain.ErrUnsupportedDocument) before anything is
// stored; Original is an upload's file as it came; the deletions also delete the files no other
// document holds.
type KnowledgeUploads interface {
	Check(name string, body []byte) error
	Upload(ctx context.Context, source, name string, body []byte, by domain.Actor) (domain.KnowledgeDocument, error)
	Original(ctx context.Context, id domain.KnowledgeDocumentID) (domain.KnowledgeDocument, []byte, error)
	DeleteDocument(ctx context.Context, id domain.KnowledgeDocumentID, by domain.Actor) error
	DeleteSource(ctx context.Context, name string, by domain.Actor) error
}

// KnowledgeSearch searches the named sources of the knowledge base (*knowledge.Searcher), as the
// gateway's knowledge__search does.
type KnowledgeSearch interface {
	Search(ctx context.Context, text string, sources []string, limit int) ([]domain.KnowledgeHit, error)
}

// Limits of the knowledge routes.
const (
	// uploadSlack is what an upload request may carry over its largest file: multipart's headers
	// and boundaries.
	uploadSlack       = 1 << 20
	defaultDocuments  = 100
	maxDocuments      = 500
	defaultSearchHits = 8
	maxSearchHits     = 20
	maxQueryRunes     = 1000
)

// Errors of the knowledge routes, answered with their codes by knowledgeProblem.
var (
	errSourceNotFound   = errors.New("knowledge source not found")
	errDocumentNotFound = errors.New("knowledge document not found")
	errSourceTaken      = errors.New("knowledge source name taken")
	errSourceInUse      = errors.New("knowledge source in use")
	errSourceLive       = errors.New("a live knowledge source never syncs")
	errUploadTooLarge   = errors.New("upload too large")
	// errSourceInvalid is a source the rules refuse (400).
	errSourceInvalid = errors.New("knowledge source invalid")
)

// knowledgeProblem answers the knowledge rules with their codes.
func knowledgeProblem(err error) (gen.Problem, bool) {
	answers := []struct {
		err    error
		status int
		code   string
	}{
		{errSourceNotFound, http.StatusNotFound, "knowledge-source-not-found"},
		{errDocumentNotFound, http.StatusNotFound, "knowledge-document-not-found"},
		{errSourceTaken, http.StatusConflict, "knowledge-source-name-taken"},
		{errSourceInUse, http.StatusConflict, "knowledge-source-in-use"},
		{errSourceLive, http.StatusConflict, "knowledge-source-live"},
		{domain.ErrNotUploadSource, http.StatusConflict, "not-an-upload-source"},
		{errUploadTooLarge, http.StatusRequestEntityTooLarge, "upload-too-large"},
		{domain.ErrUnsupportedDocument, http.StatusUnsupportedMediaType, "unsupported-document-type"},
		{domain.ErrConnectionCannotSync, http.StatusBadRequest, "connection-cannot-sync"},
		{errSourceInvalid, http.StatusBadRequest, ""},
	}

	for _, a := range answers {
		if errors.Is(err, a.err) {
			p := detailed(a.status, err)
			p.Code = optional(a.code)

			return p, true
		}
	}

	return gen.Problem{}, false
}

// sourceError is err of a change of the source name as the API answers it: an unknown source, a
// taken name, a rule broken (ports.Knowledge's domain.ErrInvalid is a 400 here).
func sourceError(err error, name string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrNotFound):
		return fmt.Errorf("%w: %q: %w", errSourceNotFound, name, err)
	case errors.Is(err, ports.ErrNameTaken):
		return fmt.Errorf("%w: %w", errSourceTaken, err)
	case errors.Is(err, domain.ErrConnectionCannotSync):
		return err
	case errors.Is(err, domain.ErrInvalid):
		return fmt.Errorf("%w: %w", errSourceInvalid, err)
	default:
		return err
	}
}

// documentError is err of a document's read or deletion as the API answers it.
func documentError(err error, id string) error {
	if errors.Is(err, ports.ErrNotFound) {
		return fmt.Errorf("%w: %s: %w", errDocumentNotFound, id, err)
	}

	return err
}

func (s *server) ListKnowledgeSources(ctx context.Context, req gen.ListKnowledgeSourcesRequestObject) (gen.ListKnowledgeSourcesResponseObject, error) {
	var named []string

	ref := deref(req.Params.Factory)
	if ref != "" {
		f, err := s.factory(ctx, ref)
		if err != nil {
			return nil, err
		}

		named = searchable(f)
	}

	sources, err := s.knowledge.Sources(ctx)
	if err != nil {
		return nil, err
	}

	users, err := s.sourceUsers(ctx)
	if err != nil {
		return nil, err
	}

	out := gen.ListKnowledgeSources200JSONResponse{Sources: []gen.KnowledgeSource{}}

	for _, src := range sources {
		if ref == "" || slices.Contains(named, src.Name) {
			out.Sources = append(out.Sources, s.sourceOut(ctx, src, users[src.Name]))
		}
	}

	return out, nil
}

func (s *server) CreateKnowledgeSource(ctx context.Context, req gen.CreateKnowledgeSourceRequestObject) (gen.CreateKnowledgeSourceResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("create knowledge source: the request has no actor")
	}

	b := req.Body

	src, err := s.knowledge.CreateSource(ctx, domain.NewKnowledgeSource{
		Name: b.Name, Title: b.Title, Kind: domain.KnowledgeSourceKind(b.Kind), Connection: deref(b.Connection), Config: deref(b.Config),
		SyncEvery: seconds(b.SyncEverySeconds),
	}, by)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		// The only thing a new source names is its connection.
		return nil, fmt.Errorf("%w: %q: %w", errConnectionNotFound, deref(b.Connection), err)
	case err != nil:
		return nil, sourceError(err, b.Name)
	}

	return gen.CreateKnowledgeSource201JSONResponse(s.sourceOut(ctx, src, nil)), nil
}

func (s *server) GetKnowledgeSource(ctx context.Context, req gen.GetKnowledgeSourceRequestObject) (gen.GetKnowledgeSourceResponseObject, error) {
	src, err := s.knowledge.Source(ctx, req.Source)
	if err != nil {
		return nil, sourceError(err, req.Source)
	}

	users, err := s.sourceUsers(ctx)
	if err != nil {
		return nil, err
	}

	return gen.GetKnowledgeSource200JSONResponse(s.sourceOut(ctx, src, users[src.Name])), nil
}

func (s *server) UpdateKnowledgeSource(ctx context.Context, req gen.UpdateKnowledgeSourceRequestObject) (gen.UpdateKnowledgeSourceResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("update knowledge source: the request has no actor")
	}

	b := req.Body
	p := domain.KnowledgeSourcePatch{Title: b.Title}

	if b.Config != nil {
		p.Config = *b.Config
		if p.Config == nil {
			p.Config = map[string]string{}
		}
	}

	if b.SyncEverySeconds != nil {
		every := seconds(b.SyncEverySeconds)
		p.SyncEvery = &every
	}

	src, err := s.knowledge.UpdateSource(ctx, req.Source, p, by)
	if err != nil {
		return nil, sourceError(err, req.Source)
	}

	users, err := s.sourceUsers(ctx)
	if err != nil {
		return nil, err
	}

	return gen.UpdateKnowledgeSource200JSONResponse(s.sourceOut(ctx, src, users[src.Name])), nil
}

// DeleteKnowledgeSource deletes a source no current version may search, with its documents and
// the uploaded files no other document holds.
func (s *server) DeleteKnowledgeSource(ctx context.Context, req gen.DeleteKnowledgeSourceRequestObject) (gen.DeleteKnowledgeSourceResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("delete knowledge source: the request has no actor")
	}

	all, err := s.sourceUsers(ctx)
	if err != nil {
		return nil, err
	}

	if users := all[req.Source]; len(users) > 0 {
		names := make([]string, 0, len(users))
		for _, f := range users {
			names = append(names, f.name)
		}

		return nil, fmt.Errorf("%w: knowledge source %q may be searched by the current version of %s; publish them without it first",
			errSourceInUse, req.Source, strings.Join(names, ", "))
	}

	if err := s.uploads.DeleteSource(ctx, req.Source, by); err != nil {
		return nil, sourceError(err, req.Source)
	}

	return gen.DeleteKnowledgeSource204Response{}, nil
}

func (s *server) SyncKnowledgeSource(ctx context.Context, req gen.SyncKnowledgeSourceRequestObject) (gen.SyncKnowledgeSourceResponseObject, error) {
	switch err := s.knowledge.SyncNow(ctx, req.Source); {
	case errors.Is(err, domain.ErrInvalid):
		return nil, fmt.Errorf("%w: %w", errSourceLive, err)
	case err != nil:
		return nil, sourceError(err, req.Source)
	}

	return gen.SyncKnowledgeSource202Response{}, nil
}

func (s *server) ListKnowledgeDocuments(ctx context.Context, req gen.ListKnowledgeDocumentsRequestObject) (gen.ListKnowledgeDocumentsResponseObject, error) {
	limit := defaultDocuments
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

	if limit < 1 || limit > maxDocuments {
		return nil, fmt.Errorf("%w: limit must be in [1, %d]", errInvalid, maxDocuments)
	}

	// One more than the page says whether there is a next one.
	docs, err := s.knowledge.Documents(ctx, req.Source, domain.KnowledgeDocumentID(deref(req.Params.After)), limit+1)
	if err != nil {
		return nil, sourceError(err, req.Source)
	}

	out := gen.ListKnowledgeDocuments200JSONResponse{Documents: []gen.KnowledgeDocument{}}

	if len(docs) > limit {
		docs = docs[:limit]
		out.NextAfter = optional(string(docs[limit-1].ID))
	}

	for _, d := range docs {
		out.Documents = append(out.Documents, documentOut(d, req.Source))
	}

	return out, nil
}

// UploadKnowledgeDocuments stores the request's files in an upload source once every one of them
// passes: the source must be an upload source, and each file of a size and type the knowledge
// base takes.
func (s *server) UploadKnowledgeDocuments(ctx context.Context, req gen.UploadKnowledgeDocumentsRequestObject) (gen.UploadKnowledgeDocumentsResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("upload knowledge documents: the request has no actor")
	}

	files, err := s.uploadedFiles(req.Body)
	if err != nil {
		return nil, err
	}

	src, err := s.knowledge.Source(ctx, req.Source)
	if err != nil {
		return nil, sourceError(err, req.Source)
	}

	if src.Kind != domain.SourceUpload {
		return nil, fmt.Errorf("knowledge source %q is %s: %w", req.Source, src.Kind, domain.ErrNotUploadSource)
	}

	for _, f := range files {
		if err := s.uploads.Check(f.name, f.body); err != nil {
			return nil, uploadError(err)
		}
	}

	out := gen.UploadKnowledgeDocuments202JSONResponse{Documents: make([]gen.KnowledgeDocument, 0, len(files))}

	for _, f := range files {
		d, err := s.uploads.Upload(ctx, req.Source, f.name, f.body, by)
		if err != nil {
			return nil, uploadError(sourceError(err, req.Source))
		}

		out.Documents = append(out.Documents, documentOut(d, req.Source))
	}

	return out, nil
}

// uploadedFile is one file of an upload request.
type uploadedFile struct {
	name string
	body []byte
}

// uploadedFiles reads the files parts of an upload request, each at most the upload limit (the
// request's body limit bounds them all together); other parts are ignored.
func (s *server) uploadedFiles(r *multipart.Reader) ([]uploadedFile, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: the body is multipart/form-data with files", errInvalid)
	}

	var files []uploadedFile

	for {
		part, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, bodyError(err)
		}

		if part.FormName() != "files" {
			continue
		}

		name := part.FileName()
		if name == "" {
			return nil, fmt.Errorf("%w: every file has a file name", errInvalid)
		}

		body, err := io.ReadAll(io.LimitReader(part, s.uploadMax+1))
		if err != nil {
			return nil, bodyError(err)
		}

		if int64(len(body)) > s.uploadMax {
			return nil, fmt.Errorf("%w: %q has more than %d bytes", errUploadTooLarge, name, s.uploadMax)
		}

		files = append(files, uploadedFile{name: name, body: body})
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("%w: no files to upload", errInvalid)
	}

	return files, nil
}

// bodyError is a failure to read an upload's body: over its limit, or malformed.
func bodyError(err error) error {
	if tooLarge := new(http.MaxBytesError); errors.As(err, &tooLarge) {
		return fmt.Errorf("%w: %w", errUploadTooLarge, err)
	}

	return fmt.Errorf("%w: %w", errInvalid, err)
}

func uploadError(err error) error {
	if errors.Is(err, ports.ErrTooLarge) {
		return fmt.Errorf("%w: %w", errUploadTooLarge, err)
	}

	return err
}

func (s *server) GetKnowledgeDocument(ctx context.Context, req gen.GetKnowledgeDocumentRequestObject) (gen.GetKnowledgeDocumentResponseObject, error) {
	d, text, err := s.knowledge.Document(ctx, domain.KnowledgeDocumentID(req.Document))
	if err != nil {
		return nil, documentError(err, req.Document)
	}

	names, err := s.sourceNames(ctx)
	if err != nil {
		return nil, err
	}

	return gen.GetKnowledgeDocument200JSONResponse{Document: documentOut(d, names[d.Source]), Text: text}, nil
}

// GetKnowledgeDocumentContent serves an uploaded original as an attachment that a browser
// downloads and never renders or sniffs (threat row 11).
func (s *server) GetKnowledgeDocumentContent(ctx context.Context, req gen.GetKnowledgeDocumentContentRequestObject) (gen.GetKnowledgeDocumentContentResponseObject, error) {
	d, body, err := s.uploads.Original(ctx, domain.KnowledgeDocumentID(req.Document))
	if err != nil {
		return nil, documentError(err, req.Document)
	}

	return gen.GetKnowledgeDocumentContent200ApplicationoctetStreamResponse{
		Body: bytes.NewReader(body), ContentLength: int64(len(body)),
		Headers: gen.GetKnowledgeDocumentContent200ResponseHeaders{
			ContentDisposition: mime.FormatMediaType("attachment", map[string]string{"filename": d.Title}), XContentTypeOptions: "nosniff",
		},
	}, nil
}

func (s *server) DeleteKnowledgeDocument(ctx context.Context, req gen.DeleteKnowledgeDocumentRequestObject) (gen.DeleteKnowledgeDocumentResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("delete knowledge document: the request has no actor")
	}

	if err := s.uploads.DeleteDocument(ctx, domain.KnowledgeDocumentID(req.Document), by); err != nil {
		return nil, documentError(err, req.Document)
	}

	return gen.DeleteKnowledgeDocument204Response{}, nil
}

// SearchKnowledge ranks the chunks of the named sources, or of every source, for the query.
func (s *server) SearchKnowledge(ctx context.Context, req gen.SearchKnowledgeRequestObject) (gen.SearchKnowledgeResponseObject, error) {
	q := strings.TrimSpace(req.Params.Q)
	if q == "" || utf8.RuneCountInString(q) > maxQueryRunes {
		return nil, fmt.Errorf("%w: q is 1 to %d characters", errInvalid, maxQueryRunes)
	}

	limit := defaultSearchHits
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

	if limit < 1 || limit > maxSearchHits {
		return nil, fmt.Errorf("%w: limit must be in [1, %d]", errInvalid, maxSearchHits)
	}

	sources, err := s.searchSources(ctx, deref(req.Params.Source))
	if err != nil {
		return nil, err
	}

	hits, err := s.search.Search(ctx, q, sources, limit)
	if err != nil {
		return nil, err
	}

	out := gen.SearchKnowledge200JSONResponse{Hits: make([]gen.KnowledgeHit, 0, len(hits))}
	for _, h := range hits {
		out.Hits = append(out.Hits, gen.KnowledgeHit{
			Document: string(h.Document), Source: h.Source, Title: h.Title, Url: optional(h.URL), Heading: h.Heading, Snippet: h.Text, Score: h.Score,
		})
	}

	return out, nil
}

// searchSources are the sources a search names, each one the organization has, or every source
// when it names none.
func (s *server) searchSources(ctx context.Context, named []string) ([]string, error) {
	all, err := s.knowledge.Sources(ctx)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(all))
	for _, src := range all {
		names = append(names, src.Name)
	}

	for _, name := range named {
		if !slices.Contains(names, name) {
			return nil, fmt.Errorf("%w: %q", errSourceNotFound, name)
		}
	}

	if len(named) > 0 {
		return named, nil
	}

	return names, nil
}

// searchable are the knowledge sources a factory's agents may search.
func searchable(f *config.Factory) []string {
	if f.Spec.Knowledge == nil {
		return nil
	}

	return f.Spec.Knowledge.Sources
}

// sourceUsers are the factories goald runs for the organization, by the knowledge sources their
// current versions may search; archived factories search none.
func (s *server) sourceUsers(ctx context.Context) (map[string][]factoryUse, error) {
	return s.usersBy(ctx, searchable)
}

// sourceNames are the organization's source names by id.
func (s *server) sourceNames(ctx context.Context) (map[domain.KnowledgeSourceID]string, error) {
	sources, err := s.knowledge.Sources(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[domain.KnowledgeSourceID]string, len(sources))
	for _, src := range sources {
		out[src.ID] = src.Name
	}

	return out, nil
}

// connectionSources are the organization's sources by the connections they read.
func (s *server) connectionSources(ctx context.Context) (map[string][]string, error) {
	sources, err := s.knowledge.Sources(ctx)
	if err != nil {
		return nil, err
	}

	out := map[string][]string{}

	for _, src := range sources {
		if src.Connection != "" {
			out[src.Connection] = append(out[src.Connection], src.Name)
		}
	}

	return out, nil
}

// sourceOut is a source as the contract has it, with the factories the caller sees among by.
func (s *server) sourceOut(ctx context.Context, src domain.KnowledgeSource, by []factoryUse) gen.KnowledgeSource {
	out := gen.KnowledgeSource{
		Id: string(src.ID), Name: src.Name, Title: src.Title, Kind: gen.KnowledgeSourceKind(src.Kind), Connection: optional(src.Connection),
		Config: map[string]string{}, SyncEverySeconds: int(src.SyncEvery / time.Second), Status: gen.KnowledgeSourceStatus(src.Status),
		LastError: optional(src.LastError), LastSyncedAt: src.LastSyncedAt, NextSyncAt: src.NextSyncAt, Documents: src.Documents,
		EmbedModel: optional(src.EmbedModel), UsedBy: gen.KnowledgeSourceUsers{Factories: []gen.ConnectionFactory{}},
		CreatedBy: s.actorOut(src.CreatedBy), CreatedAt: src.CreatedAt, UpdatedAt: src.UpdatedAt,
	}

	maps.Copy(out.Config, src.Config)

	for _, f := range by {
		if s.visible(ctx, f.id) {
			out.UsedBy.Factories = append(out.UsedBy.Factories, gen.ConnectionFactory{Id: string(f.id), Name: f.name})
		}
	}

	return out
}

// documentOut is a document of the source named source as the contract has it.
func documentOut(d domain.KnowledgeDocument, source string) gen.KnowledgeDocument {
	return gen.KnowledgeDocument{
		Id: string(d.ID), Source: source, Title: d.Title, Url: optional(d.URL), Mime: d.MIME, Bytes: d.Bytes,
		Status: gen.KnowledgeDocumentStatus(d.Status), Error: optional(d.Error), IndexedAt: d.IndexedAt,
	}
}

// seconds is a schedule in seconds as a duration; nil is 0, the default.
func seconds(n *int) time.Duration {
	if n == nil {
		return 0
	}

	return time.Duration(*n) * time.Second
}
