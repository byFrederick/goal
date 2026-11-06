package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// oneFactory is the organization's one factory.
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

// usersBy lists a live factory under the names its current version uses, and an archived one
// under none.
func TestUsersBy_AnArchivedFactoryUsesNothing(t *testing.T) {
	t.Parallel()

	for _, archived := range []bool{false, true} {
		_, ctx := builderOrg(t)

		f := &config.Factory{}
		f.Metadata.ID, f.Metadata.Name = string(mxF), "builder"

		s := &server{factories: oneFactory{f}, defs: definitionsOf(ctx, t, f, archived)}

		users, err := s.usersBy(ctx, func(*config.Factory) []string { return []string{"model-key"} })
		if err != nil {
			t.Fatalf("usersBy (archived %t) = %v", archived, err)
		}

		want := 1
		if archived {
			want = 0
		}

		if got := len(users["model-key"]); got != want {
			t.Errorf("usersBy (archived %t) = %+v; want %d factory using model-key", archived, users, want)
		}
	}
}

// An unknown connection is 404 connection-not-found; a missing master key is 503.
func TestConnectionProblem_Codes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("%w: %q: %w", errConnectionNotFound, "github", ports.ErrNotFound), http.StatusNotFound, "connection-not-found"},
		{fmt.Errorf("open: %w", ports.ErrNoMasterKey), http.StatusServiceUnavailable, "connections-unavailable"},
	}

	for _, tt := range tests {
		if p := problemOf(tt.err); p.Status != tt.status || deref(p.Code) != tt.code {
			t.Errorf("problemOf(%v) = %d %q; want %d %q", tt.err, p.Status, deref(p.Code), tt.status, tt.code)
		}
	}
}
