package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// AuthMode is how goald authenticates requests (GOAL_AUTH).
type AuthMode string

// The modes. Under AuthNone (one developer's laptop, a loopback listener) every request is the
// local user and the Authorizer is AllowAll; under AuthSession every operation but the public ones
// needs a credential the Identity resolves, and the Authorizer is Permissions.
const (
	AuthNone    AuthMode = "none"
	AuthSession AuthMode = "session"
)

// AuthConfig is the install's sign-in, as goald was started with it.
type AuthConfig struct {
	Mode AuthMode
	// Google: people may sign in with Google too (GOAL_AUTH_GOOGLE=on).
	Google bool
	// SecureCookies: people open goald over https (GOAL_PUBLIC_URL), so Better Auth's cookie is
	// __Secure-goal.session_token and the plain name is ignored.
	SecureCookies bool
}

// Identity resolves credentials to principals, per request, from the database; ports.Identity
// (the PostgreSQL adapter) satisfies it.
type Identity interface {
	Session(ctx context.Context, token string, org domain.OrgID) (domain.Principal, error)
	APIKey(ctx context.Context, hash string) (domain.Principal, error)
	OrgsFor(ctx context.Context, user domain.UserID) ([]domain.OrgMembership, error)
}

var _ Identity = ports.Identity(nil)

// localUserBeforeSignIn names the local user's rows from before sign-in once sign-in is on
// (spec 3 §14); the rows themselves are never rewritten.
const localUserBeforeSignIn = "Local user (before sign-in)"

// bindRequest makes the request's principal for its operation and binds the request to the path's
// organization (ref, "" outside one), in the order of answers of spec 3 §8.3. Under
// GOAL_AUTH=session: the credential first (missing, malformed, unknown, expired or revoked is 401
// whatever the path, so a stranger learns nothing about organizations), then the organization
// (unknown or deleted is 404), then, in the Authorizer, membership; the session is resolved for
// that organization, per request and never cached. A public operation reads no credential: its
// principal is nobody. Under GOAL_AUTH=none the principal is the local user's, set by
// withCredential.
func (s *server) bindRequest(ctx context.Context, op, ref string) (context.Context, error) {
	if s.auth.Mode == AuthNone {
		return s.bindOrg(ctx, ref, nil)
	}

	if operationScopes[op] == string(permPublic) {
		return s.bindOrg(domain.WithPrincipal(ctx, domain.Principal{}), ref, nil)
	}

	c, err := credentialFrom(ctx)
	if err != nil {
		return nil, err
	}

	// The organization is looked up first, for the session's roles there, but answered only once
	// the credential holds.
	var prior found
	if ref != "" {
		prior.org, prior.err = s.orgs.Org(ctx, ref)
	}

	var p domain.Principal

	switch c.kind {
	case credentialAPIKey:
		p, err = s.identity.APIKey(ctx, apiKeyHash(c.secret))
	case credentialSession:
		p, err = s.identity.Session(ctx, c.secret, prior.org.ID)
	case credentialNone:
		err = domain.ErrUnauthenticated
	}

	if err != nil {
		return nil, err
	}

	ctx = domain.WithActor(domain.WithPrincipal(ctx, p), p.Actor())

	return s.bindOrg(ctx, ref, &prior)
}

// found is an organization lookup made before its answer may be given.
type found struct {
	org domain.Org
	err error
}

// bindOrg binds ctx to the organization ref names (domain.WithOrg, so row-level security scopes
// every store call), looking it up unless prior did already; "" binds nothing. An unknown or
// deleted organization is errOrgNotFound.
func (s *server) bindOrg(ctx context.Context, ref string, prior *found) (context.Context, error) {
	if ref == "" {
		return ctx, nil
	}

	if prior == nil {
		o, err := s.orgs.Org(ctx, ref)
		prior = &found{org: o, err: err}
	}

	if errors.Is(prior.err, ports.ErrNotFound) {
		return nil, errOrgNotFound
	}

	if prior.err != nil {
		return nil, prior.err
	}

	return context.WithValue(domain.WithOrg(ctx, prior.org.ID), orgKey{}, prior.org), nil
}

// principalOf is the request's principal: the local user under GOAL_AUTH=none.
func principalOf(ctx context.Context) domain.Principal {
	p, _ := domain.PrincipalFrom(ctx)

	return p
}

// GetAuthConfig tells the sign-in page how this install signs people in: by email code, and with
// Google when it is on, under GOAL_AUTH=session; not at all under none (the UI shows no sign-in
// page).
func (s *server) GetAuthConfig(context.Context, gen.GetAuthConfigRequestObject) (gen.GetAuthConfigResponseObject, error) {
	if s.auth.Mode == AuthNone {
		return gen.GetAuthConfig200JSONResponse{Mode: gen.None, Methods: []gen.SignInMethod{}}, nil
	}

	out := gen.GetAuthConfig200JSONResponse{Mode: gen.Session, Methods: []gen.SignInMethod{gen.EmailCode}}
	if s.auth.Google {
		out.Methods = append(out.Methods, gen.Google)
	}

	return out, nil
}

// GetMe answers who the request acts for: the local user under GOAL_AUTH=none (mode none, no
// user), a signed-in person with their session, or an API key.
func (s *server) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	p, ok := domain.PrincipalFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("no principal: %w", domain.ErrUnauthenticated)
	}

	switch {
	case s.auth.Mode == AuthNone:
		return gen.GetMe200JSONResponse{Kind: gen.MeKindUser, Mode: gen.None}, nil
	case p.Kind == domain.PrincipalUser:
		me := gen.GetMe200JSONResponse{
			Kind:          gen.MeKindUser,
			Mode:          gen.Session,
			User:          &gen.User{Id: string(p.User), Name: p.Name, Email: p.Email},
			PlatformAdmin: p.PlatformAdmin,
		}
		if p.Session != nil {
			me.Session = &gen.MeSession{
				CreatedAt:  p.Session.CreatedAt,
				ExpiresAt:  p.Session.ExpiresAt,
				FreshUntil: p.Session.CreatedAt.Add(domain.FreshFor),
			}
		}

		return me, nil
	case p.Kind == domain.PrincipalService:
		return gen.GetMe200JSONResponse{
			Kind: gen.MeKindService,
			Mode: gen.Session,
			Key:  &gen.MeKey{Id: string(p.Key), Name: p.Name, OrgId: string(p.Org)},
		}, nil
	default:
		return nil, fmt.Errorf("principal of kind %q: %w", p.Kind, domain.ErrUnauthenticated)
	}
}

// actorOut is an actor as the API shows it: the local user's rows from before sign-in say so once
// sign-in is on.
func (s *server) actorOut(a domain.Actor) gen.Actor {
	if a == domain.LocalUser && s.auth.Mode == AuthSession {
		a.Name = localUserBeforeSignIn
	}

	return gen.Actor{Kind: gen.ActorKind(a.Kind), Id: a.ID, Name: a.Name}
}

// eventActor is an item event's actor name as the API shows it: the local user's, from before
// sign-in, says so once sign-in is on. An event keeps the name of who acted and their ref
// (data.actor).
func (s *server) eventActor(e domain.Event) string {
	if ref, _ := e.Data["actor"].(string); s.auth.Mode == AuthSession && ref == domain.LocalUser.Ref() && e.Actor == domain.LocalUser.Name {
		return localUserBeforeSignIn
	}

	return e.Actor
}

// orgRole is the role a person holds in an organization as the API shows it: absent under
// GOAL_AUTH=none (the local user may do everything) and for an API key.
func (s *server) orgRole(p domain.Principal, role domain.OrgRole) *gen.OrgRole {
	if s.auth.Mode == AuthNone || p.Kind != domain.PrincipalUser || role == "" {
		return nil
	}

	r := gen.OrgRole(role)

	return &r
}

// factoryRole is the caller's effective role on factory f: editor for owners and admins, a key's
// ceiling, absent without one and under GOAL_AUTH=none.
func (s *server) factoryRole(ctx context.Context, f domain.FactoryID) *gen.FactoryRole {
	role := principalOf(ctx).RoleOn(f)
	if s.auth.Mode == AuthNone || role == "" {
		return nil
	}

	r := gen.FactoryRole(role)

	return &r
}

// permissionsOut is a sorted permission list as the contract has it.
func permissionsOut(ps []domain.Permission) []gen.Permission {
	out := make([]gen.Permission, 0, len(ps))
	for _, p := range ps {
		out = append(out, string(p))
	}

	return out
}
