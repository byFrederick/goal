package api

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The organization's knowledge base (spec 4 §4.8, ADR 0038 decision 13): members, keys and
// personal tokens list and read its sources, files and reads (knowledge:read); owners, admins and
// keys add, check, change, sync and delete sources, upload files and delete them
// (knowledge:manage).

// KnowledgeUploads keeps the files uploaded to upload sources (*knowledge.Uploads): Check refuses
// a file Upload would (ports.ErrTooLarge, domain.ErrUnsupportedDocument) before anything is
// stored; the deletions also delete the originals no other upload holds.
type KnowledgeUploads interface {
	Check(name string, body []byte) error
	Upload(ctx context.Context, source, name string, body []byte, by domain.Actor) (domain.KnowledgeUpload, error)
	DeleteUpload(ctx context.Context, source, name string, by domain.Actor) error
	DeleteSource(ctx context.Context, name string, by domain.Actor) error
}

// Limits of the knowledge routes.
const (
	// uploadSlack is what an upload request may carry over its largest file: multipart's headers
	// and boundaries.
	uploadSlack = 1 << 20
)

// Errors of the knowledge routes, answered with their codes by knowledgeProblem.
var (
	errSourceNotFound = errors.New("knowledge source not found")
	errSourceTaken    = errors.New("knowledge source name taken")
	errSourceInUse    = errors.New("knowledge source in use")
	errSourceLive     = errors.New("a live knowledge source never syncs")
	errUploadTooLarge = errors.New("upload too large")
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
		{errFileNotFound, http.StatusNotFound, "knowledge-file-not-found"},
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

func (s *server) ListKnowledgeSources(ctx context.Context, req gen.ListKnowledgeSourcesRequestObject) (gen.ListKnowledgeSourcesResponseObject, error) {
	var named []string

	ref := deref(req.Params.Factory)
	if ref != "" {
		f, err := s.factory(ctx, ref)
		if err != nil {
			return nil, err
		}

		named = listedSources(f)
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
			o, err := s.sourceOut(ctx, src, users[src.Name])
			if err != nil {
				return nil, err
			}

			out.Sources = append(out.Sources, o)
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

	// A new synced source reads with a secret (ADR 0037 decision 14); only a live one still names
	// a connection, and the sources made before keep theirs until moving over.
	if b.Kind == gen.KnowledgeSourceKindSynced && b.Connection != nil {
		return nil, fmt.Errorf("%w: a synced source reads with a secret, not connection %q", errSourceInvalid, *b.Connection)
	}

	if err := s.sourceSecret(ctx, b.Secret); err != nil {
		return nil, err
	}

	src, err := s.knowledge.CreateSource(ctx, domain.NewKnowledgeSource{
		Name: b.Name, Title: b.Title, Kind: domain.KnowledgeSourceKind(b.Kind), Connection: deref(b.Connection), Secret: deref(b.Secret),
		Config: deref(b.Config), ReadWhen: deref(b.ReadWhen), SyncEvery: seconds(b.SyncEverySeconds),
	}, by)

	switch {
	case errors.Is(err, ports.ErrNotFound) && b.Connection == nil:
		// A new source names a connection or a secret, which sourceSecret found a moment ago.
		return nil, secretError(err)
	case errors.Is(err, ports.ErrNotFound):
		return nil, fmt.Errorf("%w: %q: %w", errConnectionNotFound, deref(b.Connection), err)
	case err != nil:
		return nil, sourceError(err, b.Name)
	}

	o, err := s.sourceOut(ctx, src, nil)
	if err != nil {
		return nil, err
	}

	return gen.CreateKnowledgeSource201JSONResponse(o), nil
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

	o, err := s.sourceOut(ctx, src, users[src.Name])
	if err != nil {
		return nil, err
	}

	return gen.GetKnowledgeSource200JSONResponse(o), nil
}

func (s *server) UpdateKnowledgeSource(ctx context.Context, req gen.UpdateKnowledgeSourceRequestObject) (gen.UpdateKnowledgeSourceResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("update knowledge source: the request has no actor")
	}

	b := req.Body
	p := domain.KnowledgeSourcePatch{Title: b.Title, ReadWhen: b.ReadWhen, Secret: b.Secret}

	if err := s.sourceSecret(ctx, b.Secret); err != nil {
		return nil, err
	}

	if b.Config != nil {
		p.Config = *b.Config
		if p.Config == nil {
			p.Config = map[string]string{}
		}
	}

	p.SyncEvery = seconds(b.SyncEverySeconds)

	src, err := s.knowledge.UpdateSource(ctx, req.Source, p, by)
	if err != nil {
		return nil, sourceError(err, req.Source)
	}

	users, err := s.sourceUsers(ctx)
	if err != nil {
		return nil, err
	}

	o, err := s.sourceOut(ctx, src, users[src.Name])
	if err != nil {
		return nil, err
	}

	return gen.UpdateKnowledgeSource200JSONResponse(o), nil
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

		return nil, fmt.Errorf("%w: knowledge source %q is listed by the published version of %s; publish them without it first",
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

// uploadedFile is one file of an upload request.
type uploadedFile struct {
	name string
	body []byte
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

// sourceUsers are the factories goald runs for the organization, by the knowledge sources their
// current versions may search; archived factories search none.
func (s *server) sourceUsers(ctx context.Context) (map[string][]factoryUse, error) {
	users, err := s.usersBy(ctx, listedSources)
	if err != nil {
		return nil, err
	}

	// Each factory's entry for the source says who reads it (ADR 0038 decision 5).
	for _, f := range s.factories.Factories(ctx) {
		if f.Spec.Knowledge == nil {
			continue
		}

		for _, e := range f.Spec.Knowledge.Sources {
			for i, u := range users[e.Name] {
				if u.id == domain.FactoryID(f.Metadata.ID) {
					users[e.Name][i].scope = e.AppliesTo
				}
			}
		}
	}

	return users, nil
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

// sourceSecret refuses a secret a source would read with that does not exist (404
// secret-not-found), before the source is changed.
func (s *server) sourceSecret(ctx context.Context, name *string) error {
	if name == nil {
		return nil
	}

	if _, err := s.secrets.Get(ctx, *name); err != nil {
		return secretError(err)
	}

	return nil
}

// sourceOut is a source as the contract has it, with the factories the caller sees among by.
func (s *server) sourceOut(ctx context.Context, src domain.KnowledgeSource, by []factoryUse) (gen.KnowledgeSource, error) {
	snaps, err := s.knowledge.Snapshots(ctx, src.Name)
	if err != nil {
		return gen.KnowledgeSource{}, err
	}

	out := gen.KnowledgeSource{
		Id: string(src.ID), Name: src.Name, Title: src.Title, Kind: gen.KnowledgeSourceKind(src.Kind), Connection: optional(src.Connection),
		Secret: optional(src.Secret), Config: map[string]string{}, ReadWhen: src.ReadWhen, SyncEverySeconds: int(src.SyncEvery / time.Second), Status: gen.KnowledgeSourceStatus(src.Status),
		LastError: optional(src.LastError), LastSyncedAt: src.LastSyncedAt, NextSyncAt: src.NextSyncAt,
		UsedBy:    gen.KnowledgeSourceUsers{Factories: []gen.ConnectionFactory{}, Readers: []gen.KnowledgeSourceReader{}},
		CreatedBy: s.actorOut(src.CreatedBy), CreatedAt: src.CreatedAt, UpdatedAt: src.UpdatedAt,
	}

	maps.Copy(out.Config, src.Config)

	if src.SyncFailures > 0 {
		out.SyncFailures = &src.SyncFailures
	}

	if len(snaps) > 0 {
		n := snaps[0]
		out.LatestSnapshot = &gen.KnowledgeSnapshotSummary{Files: n.Files, Failed: n.Failed, Bytes: n.Bytes, SyncedAt: n.CreatedAt}
	}

	for _, f := range by {
		if s.visible(ctx, f.id) {
			out.UsedBy.Factories = append(out.UsedBy.Factories, gen.ConnectionFactory{Id: string(f.id), Name: f.name})
			out.UsedBy.Readers = append(out.UsedBy.Readers, gen.KnowledgeSourceReader{
				Id: string(f.id), Name: f.name, Agents: nonNil(f.scope.Agents), Stations: nonNil(f.scope.Stations),
				Lanes: nonNil(f.scope.Lanes), WorkTypes: nonNil(f.scope.WorkTypes),
			})
		}
	}

	return out, nil
}

// seconds is a schedule in seconds as a duration: nil when not given, and 0 on demand
// (domain.SyncOnDemand).
func seconds(n *int) *time.Duration {
	if n == nil {
		return nil
	}

	d := time.Duration(*n) * time.Second

	return &d
}

// listedSources are the knowledge sources a factory's agents read.
func listedSources(f *config.Factory) []string {
	return f.Spec.Knowledge.Names()
}
