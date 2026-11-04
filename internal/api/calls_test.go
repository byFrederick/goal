package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

const (
	// writerAgent is acme's one agent.
	writerAgent = "writer"
	// backendAgent is the backend agent of the model tests' line, on the runtime's model
	// backendModel.
	backendAgent = "backend"
	backendModel = "claude-fable-5-1"
)

// acmeMember is a member of testOrg with role on acme, signed in a minute ago.
func acmeMember(role domain.FactoryRole) domain.Principal {
	return domain.Principal{
		Kind: domain.PrincipalUser, User: "019a0000-0000-7000-8000-0000000000aa", Name: "Ada", Org: testOrg, OrgRole: domain.OrgMember,
		FactoryRoles: map[domain.FactoryID]domain.FactoryRole{acmeID: role},
		Session:      &domain.Session{ID: "s", CreatedAt: t0.Add(-time.Minute), ExpiresAt: t0.Add(time.Hour), Browser: true},
	}
}

// as makes every authorization the real permission table's answer for p, in place of the
// harness's local user.
func (h *harness) as(p domain.Principal) {
	perms := api.NewPermissions(h.clock)

	h.seam.deny(func(r api.AccessRequest) error {
		r.Principal = p

		return perms.Authorize(context.Background(), r)
	})
}

// problemOfCall makes a call and returns its status and its problem.
func problemOfCall(t *testing.T, h *harness, method, path, body string) (int, gen.Problem) {
	t.Helper()

	return callWithProblem(t, h, method, path, body, nil)
}

// callWithProblem makes a call and returns its status and its problem, when it answered one;
// out, when set, takes the answer instead.
func callWithProblem(t *testing.T, h *harness, method, path, body string, out any) (int, gen.Problem) {
	t.Helper()

	var p gen.Problem

	if out == nil {
		out = &p
	}

	code := h.call(t, method, path, body, out)

	return code, p
}

func wantProblem(t *testing.T, what string, code int, p gen.Problem, status int, pcode string) {
	t.Helper()

	if code != status || deref(p.Code) != pcode {
		t.Errorf("%s = %d %q, want %d %q", what, code, deref(p.Code), status, pcode)
	}
}

// authors is an authoring service over h's definitions, acting as the local user.
func (h *harness) authors(t *testing.T) (*authoring.Service, context.Context) {
	t.Helper()

	return authoring.New(authoring.Deps{Store: h.defs, Loads: h.store, Templates: h.objects, Clock: h.clock}),
		domain.WithActor(inOrg(t), domain.LocalUser)
}

// acmeRecord is acme as the definition store has it.
func (h *harness) acmeRecord(t *testing.T) ports.FactoryRecord {
	t.Helper()

	rec, err := h.defs.FactoryRecord(inOrg(t), acmeID)
	if err != nil {
		t.Fatal(err)
	}

	return rec
}

// publishedFile is path in acme's version number.
func (h *harness) publishedFile(t *testing.T, number int, path string) string {
	t.Helper()

	ctx := inOrg(t)

	v, err := h.defs.VersionByNumber(ctx, acmeID, number)
	if err != nil {
		t.Fatal(err)
	}

	hash, ok := v.Manifest[path]
	if !ok {
		t.Fatalf("version %d has no %s", number, path)
	}

	b, err := h.defs.Blob(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// wantNoDraft fails unless acme has no open draft.
func (h *harness) wantNoDraft(t *testing.T, after string) {
	t.Helper()

	if d := h.acmeRecord(t).Draft; d != nil {
		t.Errorf("after %s acme has draft %+v, want none", after, d)
	}
}
