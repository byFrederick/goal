package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/knowledge"
)

// The factory of the in-use test and the source it may search.
const (
	factoryBuilder = "builder"
	kbHandbook     = "handbook"
)

// A source a factory's current version may search is listed as used by it, filtered by it, and
// not deleted (409 knowledge-source-in-use, naming the factory).
func TestKnowledge_SourceInUseByAFactory(t *testing.T) {
	t.Parallel()

	tenancy, ctx := builderOrg(t)
	kb := porttest.NewKnowledge(porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tenancy), tenancy)

	f := &config.Factory{}
	f.Metadata.ID, f.Metadata.Name = string(mxF), factoryBuilder
	f.Spec.Knowledge = &config.Knowledge{Sources: []string{kbHandbook}}

	s := &server{
		factories: oneFactory{f}, defs: definitionsOf(ctx, t, f, false), knowledge: kb, uploads: knowledge.NewUploads(kb, porttest.NewObjectStore(), 0),
		authz: AllowAll{}, clock: porttest.NewClock(mxNow),
	}

	for _, n := range []string{kbHandbook, "archive"} {
		if _, err := kb.CreateSource(ctx, domain.NewKnowledgeSource{Name: n, Title: n, Kind: domain.SourceUpload}, domain.LocalUser); err != nil {
			t.Fatal(err)
		}
	}

	list, err := s.ListKnowledgeSources(ctx, gen.ListKnowledgeSourcesRequestObject{Params: gen.ListKnowledgeSourcesParams{Factory: &f.Metadata.Name}})
	if got, ok := list.(gen.ListKnowledgeSources200JSONResponse); err != nil || !ok || len(got.Sources) != 1 ||
		len(got.Sources[0].UsedBy.Factories) != 1 || got.Sources[0].UsedBy.Factories[0].Name != factoryBuilder {
		t.Errorf("sources of the factory = %+v, %v; want handbook, used by builder", list, err)
	}

	_, err = s.DeleteKnowledgeSource(ctx, gen.DeleteKnowledgeSourceRequestObject{Source: kbHandbook})
	if p := problemOf(err); p.Status != http.StatusConflict || deref(p.Code) != "knowledge-source-in-use" || !errors.Is(err, errSourceInUse) {
		t.Errorf("delete of a source in use = %v (%d %s); want 409 knowledge-source-in-use", err, p.Status, deref(p.Code))
	}

	if _, err := s.DeleteKnowledgeSource(ctx, gen.DeleteKnowledgeSourceRequestObject{Source: "archive"}); err != nil {
		t.Errorf("delete of a source no factory searches = %v", err)
	}
}

// An archived factory may search no source: a source only it names is listed as used by no
// factory and is deleted.
func TestKnowledge_AnArchivedFactoryUsesNoSource(t *testing.T) {
	t.Parallel()

	tenancy, ctx := builderOrg(t)
	kb := porttest.NewKnowledge(porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tenancy), tenancy)

	f := &config.Factory{}
	f.Metadata.ID, f.Metadata.Name = string(mxF), factoryBuilder
	f.Spec.Knowledge = &config.Knowledge{Sources: []string{kbHandbook}}

	s := &server{
		factories: oneFactory{f}, defs: definitionsOf(ctx, t, f, true), knowledge: kb, uploads: knowledge.NewUploads(kb, porttest.NewObjectStore(), 0),
		authz: AllowAll{}, clock: porttest.NewClock(mxNow),
	}

	if _, err := kb.CreateSource(ctx, domain.NewKnowledgeSource{Name: kbHandbook, Title: kbHandbook, Kind: domain.SourceUpload}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListKnowledgeSources(ctx, gen.ListKnowledgeSourcesRequestObject{})
	if got, ok := list.(gen.ListKnowledgeSources200JSONResponse); err != nil || !ok || len(got.Sources) != 1 || len(got.Sources[0].UsedBy.Factories) != 0 {
		t.Errorf("sources = %+v, %v; want handbook, used by no factory", list, err)
	}

	if _, err := s.DeleteKnowledgeSource(ctx, gen.DeleteKnowledgeSourceRequestObject{Source: kbHandbook}); err != nil {
		t.Errorf("delete of a source only an archived factory searches = %v; want it deleted", err)
	}
}

// The knowledge errors of ports.Knowledge and knowledge.Uploads answer their codes.
func TestKnowledgeProblem_Codes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err    error
		status int
		code   string
	}{
		{sourceError(ports.ErrNotFound, kbHandbook), http.StatusNotFound, "knowledge-source-not-found"},
		{sourceError(ports.ErrNameTaken, kbHandbook), http.StatusConflict, "knowledge-source-name-taken"},
		{sourceError(domain.ErrConnectionCannotSync, kbHandbook), http.StatusBadRequest, "connection-cannot-sync"},
		{sourceError(domain.ErrInvalid, kbHandbook), http.StatusBadRequest, ""},
		{sourceError(domain.ErrNotUploadSource, kbHandbook), http.StatusConflict, "not-an-upload-source"},
		{documentError(ports.ErrNotFound, "d"), http.StatusNotFound, "knowledge-document-not-found"},
		{uploadError(ports.ErrTooLarge), http.StatusRequestEntityTooLarge, "upload-too-large"},
		{uploadError(domain.ErrUnsupportedDocument), http.StatusUnsupportedMediaType, "unsupported-document-type"},
	}

	for _, tt := range tests {
		if p := problemOf(tt.err); p.Status != tt.status || deref(p.Code) != tt.code {
			t.Errorf("%v = %d %q, want %d %q", tt.err, p.Status, deref(p.Code), tt.status, tt.code)
		}
	}
}
