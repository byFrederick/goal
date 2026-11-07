package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

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
	kbArchive      = "archive"
)

// A source a factory's published version lists, as a name or an entry, is listed as used by it,
// filtered by it, and not deleted (409 knowledge-source-in-use, naming the factory).
func TestKnowledge_DeleteRefusedWhileAPublishedVersionListsIt(t *testing.T) {
	t.Parallel()

	tenancy, ctx := builderOrg(t)
	kb := porttest.NewKnowledge(tenancy)

	f := &config.Factory{}
	f.Metadata.ID, f.Metadata.Name = string(mxF), factoryBuilder
	f.Spec.Knowledge = &config.Knowledge{Sources: []config.KnowledgeSource{{Name: kbHandbook, ReadWhen: "Before you answer about leave."}}}

	s := &server{
		factories: oneFactory{f}, defs: definitionsOf(ctx, t, f, false), knowledge: kb, uploads: knowledge.NewUploads(kb, porttest.NewObjectStore(), 0),
		authz: AllowAll{}, clock: porttest.NewClock(mxNow),
	}

	for _, n := range []string{kbHandbook, kbArchive} {
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
	if p := problemOf(err); p.Status != http.StatusConflict || deref(p.Code) != "knowledge-source-in-use" || !errors.Is(err, errSourceInUse) ||
		!strings.Contains(deref(p.Detail), factoryBuilder) {
		t.Errorf("delete of a source in use = %v (%d %s); want 409 knowledge-source-in-use", err, p.Status, deref(p.Code))
	}

	if _, err := s.DeleteKnowledgeSource(ctx, gen.DeleteKnowledgeSourceRequestObject{Source: kbArchive}); err != nil {
		t.Errorf("delete of a source no factory searches = %v", err)
	}
}

// An archived factory may search no source: a source only it names is listed as used by no
// factory and is deleted.
func TestKnowledge_AnArchivedFactoryUsesNoSource(t *testing.T) {
	t.Parallel()

	tenancy, ctx := builderOrg(t)
	kb := porttest.NewKnowledge(tenancy)

	f := &config.Factory{}
	f.Metadata.ID, f.Metadata.Name = string(mxF), factoryBuilder
	f.Spec.Knowledge = &config.Knowledge{Sources: []config.KnowledgeSource{{Name: kbHandbook}}}

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
		// A refusal the domain still wraps in ErrInvalid answers the generic invalid problem.
		{sourceError(domain.ErrConnectionCannotSync, kbHandbook), http.StatusBadRequest, ""},
		{sourceError(domain.ErrInvalid, kbHandbook), http.StatusBadRequest, ""},
		{sourceError(domain.ErrNotUploadSource, kbHandbook), http.StatusConflict, "not-an-upload-source"},
		{uploadError(ports.ErrTooLarge), http.StatusRequestEntityTooLarge, "upload-too-large"},
		{uploadError(domain.ErrUnsupportedDocument), http.StatusUnsupportedMediaType, "unsupported-document-type"},
		{fmt.Errorf("%w: db/disk.md", errFileNotFound), http.StatusNotFound, "knowledge-file-not-found"},
	}

	for _, tt := range tests {
		if p := problemOf(tt.err); p.Status != tt.status || deref(p.Code) != tt.code {
			t.Errorf("%v = %d %q, want %d %q", tt.err, p.Status, deref(p.Code), tt.status, tt.code)
		}
	}
}

// A source's readers are the visible factories whose current version lists it, each with the
// scope of its entry; a plain entry has empty lists, which means every run.
func TestKnowledge_SourceReaders(t *testing.T) {
	t.Parallel()

	tenancy, ctx := builderOrg(t)
	kb := porttest.NewKnowledge(tenancy)

	f := &config.Factory{}
	f.Metadata.ID, f.Metadata.Name = string(mxF), factoryBuilder
	f.Spec.Knowledge = &config.Knowledge{Sources: []config.KnowledgeSource{
		{Name: kbHandbook, AppliesTo: domain.RuleScope{Agents: []string{"reviewer"}, Lanes: []string{"fix"}}},
		{Name: kbArchive},
	}}

	s := &server{
		factories: oneFactory{f}, defs: definitionsOf(ctx, t, f, false), knowledge: kb, uploads: knowledge.NewUploads(kb, porttest.NewObjectStore(), 0),
		authz: AllowAll{}, clock: porttest.NewClock(mxNow),
	}

	for _, n := range []string{kbHandbook, kbArchive} {
		if _, err := kb.CreateSource(ctx, domain.NewKnowledgeSource{Name: n, Title: n, Kind: domain.SourceUpload}, domain.LocalUser); err != nil {
			t.Fatal(err)
		}
	}

	list, err := s.ListKnowledgeSources(ctx, gen.ListKnowledgeSourcesRequestObject{})

	got, ok := list.(gen.ListKnowledgeSources200JSONResponse)
	if err != nil || !ok || len(got.Sources) != 2 {
		t.Fatalf("sources = %+v, %v; want two", list, err)
	}

	byName := map[string]gen.KnowledgeSource{}
	for _, src := range got.Sources {
		byName[src.Name] = src
	}

	checkReaders(t, byName[kbHandbook].UsedBy.Readers, []string{"reviewer"}, []string{"fix"})
	checkReaders(t, byName[kbArchive].UsedBy.Readers, nil, nil)

	one, err := s.GetKnowledgeSource(ctx, gen.GetKnowledgeSourceRequestObject{Source: kbHandbook})
	if g, ok := one.(gen.GetKnowledgeSource200JSONResponse); err != nil || !ok || len(g.UsedBy.Readers) != 1 {
		t.Errorf("get = %+v, %v; want the same reader", one, err)
	}
}

// checkReaders checks that readers is the builder factory alone, scoped to agents and lanes, with
// the other lists empty and not null.
func checkReaders(t *testing.T, readers []gen.KnowledgeSourceReader, agents, lanes []string) {
	t.Helper()

	if len(readers) != 1 || readers[0].Name != factoryBuilder {
		t.Fatalf("readers = %+v; want builder alone", readers)
	}

	r := readers[0]
	if !slices.Equal(r.Agents, agents) || !slices.Equal(r.Lanes, lanes) || r.Stations == nil || len(r.Stations) != 0 || r.WorkTypes == nil || len(r.WorkTypes) != 0 {
		t.Errorf("reader = %+v; want agents %v, lanes %v, no stations or work types", r, agents, lanes)
	}
}

// sourcesOnly is a knowledge base that lists the sources it was given and nothing else.
type sourcesOnly struct {
	ports.Knowledge

	sources []domain.KnowledgeSource
}

func (k sourcesOnly) Sources(context.Context) ([]domain.KnowledgeSource, error) {
	return k.sources, nil
}

// A source whose last three syncs failed is a Needs you entry for each factory that lists it, with
// what the last sync said; a source with fewer failures, one that went through since, or one no
// factory lists, is none.
func TestInbox_AFailingKnowledgeSource(t *testing.T) {
	t.Parallel()

	f := &config.Factory{}
	f.Metadata.ID, f.Metadata.Name = string(mxF), factoryBuilder
	f.Spec.Knowledge = &config.Knowledge{Sources: []config.KnowledgeSource{{Name: kbHandbook}, {Name: "runbooks"}, {Name: "recovered"}}}

	since := mxNow.Add(-time.Hour)
	kb := sourcesOnly{sources: []domain.KnowledgeSource{
		{Name: kbHandbook, Status: domain.SyncFailed, SyncFailures: 3, LastError: "401 from the host", UpdatedAt: since},
		{Name: "runbooks", Status: domain.SyncFailed, SyncFailures: 2, LastError: "timeout"},
		{Name: "recovered", Status: domain.SyncIdle},
		{Name: kbArchive, Status: domain.SyncFailed, SyncFailures: 9, LastError: "gone"},
	}}

	w := workInbox{s: &server{knowledge: kb}}

	got, err := w.knowledgeEntries(t.Context(), []*config.Factory{f})
	if err != nil || len(got) != 1 {
		t.Fatalf("entries = %+v, %v; want the handbook's only", got, err)
	}

	e := got[0]
	if e.Id != "knowledge_source:"+string(mxF)+":"+kbHandbook || e.Kind != gen.InboxEntryKindKnowledgeSource || e.Factory.Name != factoryBuilder ||
		!e.Since.Equal(since) {
		t.Errorf("entry %+v; want knowledge_source:%s:%s of the factory, since its last failure", e, mxF, kbHandbook)
	}

	if e.KnowledgeSource == nil || e.KnowledgeSource.Source != kbHandbook || e.KnowledgeSource.Error != "401 from the host" || e.KnowledgeSource.FailedSyncs != 3 {
		t.Errorf("knowledge_source %+v", e.KnowledgeSource)
	}
}
