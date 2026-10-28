package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// connGitHub is the connection the in-use test's factory names, and fieldToken its secret.
const (
	connGitHub = "github"
	fieldToken = "token"
)

// oneFactory is the organization's one factory, whose definition names the connection github.
type oneFactory struct{ f *config.Factory }

func (o oneFactory) Factories(context.Context) []*config.Factory { return []*config.Factory{o.f} }

func (o oneFactory) Lookup(_ context.Context, ref string) (*config.Factory, error) {
	if ref != o.f.Metadata.Name && ref != o.f.Metadata.ID {
		return nil, ports.ErrNotFound
	}

	return o.f, nil
}

// builderOrg is the tests' organization, Acme Builders, and a local user's request in it.
func builderOrg(t *testing.T) (*porttest.Tenancy, context.Context) {
	t.Helper()

	tenancy := porttest.NewTenancy(porttest.NewClock(mxNow).Now)

	org, err := tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme-builders", Name: "Acme Builders"})
	if err != nil {
		t.Fatal(err)
	}

	return tenancy, domain.WithActor(domain.WithPrincipal(domain.WithOrg(t.Context(), org.ID), domain.LocalPrincipal), domain.LocalUser)
}

// githubConnections are the connections of tenancy's organizations, with the platform kind github.
func githubConnections(t *testing.T, tenancy *porttest.Tenancy) *porttest.Connections {
	t.Helper()

	conns := porttest.NewConnections(porttest.NewMasterKeys().Key("k1"), tenancy)
	if err := conns.SyncPlatformKinds(t.Context(), []domain.ConnectionKind{{Name: connGitHub, Title: "GitHub", Platform: true, Spec: domain.KindSpec{
		Fields: []domain.KindField{{Name: fieldToken, Title: "Token", Secret: true, Required: true}},
	}}}); err != nil {
		t.Fatal(err)
	}

	return conns
}

// definitionsOf is an authoring service over a definition store that holds factory f of the
// organization in ctx, published once, and archived when archived is set.
func definitionsOf(ctx context.Context, t *testing.T, f *config.Factory, archived bool) *authoring.Service {
	t.Helper()

	store := porttest.NewDefinitions(porttest.DefinitionsDeps{Objects: porttest.NewObjectStore()})
	id := domain.FactoryID(f.Metadata.ID)

	if _, err := porttest.Publish(ctx, store, id, f.Metadata.Name, map[string][]byte{"factory.yaml": []byte("kind: Factory\n")}); err != nil {
		t.Fatal(err)
	}

	if archived {
		if _, err := store.SetPaused(ctx, id, true, domain.LocalUser); err != nil {
			t.Fatal(err)
		}

		if _, err := store.ArchiveFactory(ctx, id, domain.LocalUser); err != nil {
			t.Fatal(err)
		}
	}

	return authoring.New(authoring.Deps{Store: store, Templates: porttest.NewObjectStore(), Clock: porttest.NewClock(mxNow)})
}

// A connection a factory's current version names is listed as used by it, and is neither
// deleted nor renamed (409 connection-in-use, naming the factory); its description still changes.
func TestConnections_InUseByAFactory(t *testing.T) {
	t.Parallel()

	tenancy, ctx := builderOrg(t)
	conns := githubConnections(t, tenancy)

	f := &config.Factory{}
	f.Metadata.ID, f.Metadata.Name = string(mxF), "builder"
	f.Spec.Connections = []config.Connection{{Name: connGitHub}}

	s := &server{
		factories: oneFactory{f}, defs: definitionsOf(ctx, t, f, false), connections: conns, knowledge: porttest.NewKnowledge(conns, tenancy),
		authz: AllowAll{}, clock: porttest.NewClock(mxNow), items: porttest.NewStore(),
	}

	if _, err := conns.Create(ctx, domain.NewConnection{Name: connGitHub, Kind: connGitHub, Secrets: map[string]string{fieldToken: "t"}}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListConnections(ctx, gen.ListConnectionsRequestObject{Params: gen.ListConnectionsParams{Factory: &f.Metadata.Name}})
	if got, ok := list.(gen.ListConnections200JSONResponse); err != nil || !ok || len(got.Connections) != 1 ||
		len(got.Connections[0].UsedBy.Factories) != 1 || got.Connections[0].UsedBy.Factories[0].Name != "builder" {
		t.Errorf("connections of the factory = %+v, %v; want github, used by builder", list, err)
	}

	_, err = s.DeleteConnection(ctx, gen.DeleteConnectionRequestObject{Connection: connGitHub})
	refusedInUse(t, "delete", err)

	renamed := "repos"
	_, err = s.UpdateConnection(ctx, gen.UpdateConnectionRequestObject{Connection: connGitHub, Body: &gen.ConnectionPatch{Name: &renamed}})
	refusedInUse(t, "rename", err)

	desc := "acme's repositories"
	if _, err := s.UpdateConnection(ctx, gen.UpdateConnectionRequestObject{Connection: connGitHub, Body: &gen.ConnectionPatch{Description: &desc}}); err != nil {
		t.Errorf("a description change of a connection in use = %v; want it changed", err)
	}

	if _, err := conns.Get(ctx, connGitHub); err != nil {
		t.Errorf("github after the refusals: %v; want it kept", err)
	}
}

// An archived factory names no connection: its connections list it in no usedBy, and are
// deleted and renamed as if no factory named them.
func TestConnections_AnArchivedFactoryUsesNothing(t *testing.T) {
	t.Parallel()

	tenancy, ctx := builderOrg(t)
	conns := githubConnections(t, tenancy)

	f := &config.Factory{}
	f.Metadata.ID, f.Metadata.Name = string(mxF), "builder"
	f.Spec.Connections = []config.Connection{{Name: connGitHub}}

	s := &server{
		factories: oneFactory{f}, defs: definitionsOf(ctx, t, f, true), connections: conns, knowledge: porttest.NewKnowledge(conns, tenancy),
		authz: AllowAll{}, clock: porttest.NewClock(mxNow), items: porttest.NewStore(),
	}

	for _, n := range []string{connGitHub, "repos"} {
		if _, err := conns.Create(ctx, domain.NewConnection{Name: n, Kind: connGitHub, Secrets: map[string]string{fieldToken: "t"}}, domain.LocalUser); err != nil {
			t.Fatal(err)
		}
	}

	list, err := s.ListConnections(ctx, gen.ListConnectionsRequestObject{})
	if got, ok := list.(gen.ListConnections200JSONResponse); err != nil || !ok || len(got.Connections) != 2 ||
		len(got.Connections[0].UsedBy.Factories) != 0 || len(got.Connections[1].UsedBy.Factories) != 0 {
		t.Errorf("connections = %+v, %v; want github and repos, used by no factory", list, err)
	}

	renamed := "github-old"
	if _, err := s.UpdateConnection(ctx, gen.UpdateConnectionRequestObject{Connection: connGitHub, Body: &gen.ConnectionPatch{Name: &renamed}}); err != nil {
		t.Errorf("rename of a connection only an archived factory names = %v; want it renamed", err)
	}

	if _, err := s.DeleteConnection(ctx, gen.DeleteConnectionRequestObject{Connection: renamed}); err != nil {
		t.Errorf("delete of a connection only an archived factory names = %v; want it deleted", err)
	}
}

func refusedInUse(t *testing.T, what string, err error) {
	t.Helper()

	if p := problemOf(err); p.Status != http.StatusConflict || deref(p.Code) != "connection-in-use" || !errors.Is(err, errConnectionInUse) {
		t.Errorf("%s of a connection in use = %v (%d %s); want 409 connection-in-use", what, err, p.Status, deref(p.Code))
	}
}

// The connection errors of ports.Connections answer their codes; a missing master key is 503.
func TestConnectionProblem_Codes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err    error
		status int
		code   string
	}{
		{connectionError(ports.ErrNoMasterKey, connGitHub, errConnectionNotFound), http.StatusServiceUnavailable, "connections-unavailable"},
		{connectionError(ports.ErrNotFound, connGitHub, errConnectionNotFound), http.StatusNotFound, "connection-not-found"},
		{connectionError(ports.ErrNotFound, connGitHub, errKindNotFound), http.StatusNotFound, "connection-kind-not-found"},
		{connectionError(ports.ErrNameTaken, connGitHub, errConnectionNotFound), http.StatusConflict, "connection-name-taken"},
		{connectionError(ports.ErrConflict, connGitHub, errConnectionNotFound), http.StatusConflict, "connection-in-use"},
		{connectionError(domain.ErrInvalid, connGitHub, errConnectionNotFound), http.StatusBadRequest, ""},
		{connectionError(errors.Join(domain.ErrFieldRequired, domain.ErrInvalid), connGitHub, errConnectionNotFound), http.StatusBadRequest, "field-required"},
		{fmt.Errorf("%w: kind docs", errNotCodeHost), http.StatusUnprocessableEntity, "connection-not-code-host"},
		{fmt.Errorf("%w: gitlab", errReposUnsupported), http.StatusUnprocessableEntity, "repositories-not-supported"},
		{fmt.Errorf("%w: nobody", errOwnerNotFound), http.StatusNotFound, "owner-not-found"},
		{fmt.Errorf("%w: GitHub answered 401", errCodeHost), http.StatusBadGateway, "code-host-error"},
	}

	for _, tt := range tests {
		if p := problemOf(tt.err); p.Status != tt.status || deref(p.Code) != tt.code {
			t.Errorf("problemOf(%v) = %d %q; want %d %q", tt.err, p.Status, deref(p.Code), tt.status, tt.code)
		}
	}
}
