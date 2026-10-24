package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Personal tokens (spec 3 §4.5, PR 3-12b): a member's own credential in one organization, for the
// CLI or a script on their machine. A request with one acts as the member, with the token's
// scopes within the member's roles at that moment (Permissions.Authorize).

// personalTokenHintLength is how much of a secret a token's hint shows: goal_pat_ and four
// characters.
const personalTokenHintLength = len(personalTokenPrefix) + 4

// errTokensNeedSignIn answers creating a personal token under GOAL_AUTH=none, where nobody signs
// in (403 forbidden).
var errTokensNeedSignIn = fmt.Errorf("%w: personal tokens need sign-in (GOAL_AUTH=session)", errForbidden)

// personalTokenProblem answers the personal token rules with their codes.
func personalTokenProblem(err error) (gen.Problem, bool) {
	switch {
	case errors.Is(err, errBrowserSessionRequired):
		p := detailed(http.StatusForbidden, err)
		p.Code = optional("browser-session-required")

		return p, true
	case errors.Is(err, domain.ErrPersonalTokenNameTaken):
		p := detailed(http.StatusConflict, err)
		p.Code = optional("personal-token-name-taken")

		return p, true
	default:
		return gen.Problem{}, false
	}
}

// newPersonalTokenSecret makes a token: goal_pat_ and base64url of 32 bytes from crypto/rand.
// Only its hash (apiKeyHash) and hint are kept.
func newPersonalTokenSecret() (secret, hint, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("personal token secret: %w", err)
	}

	secret = personalTokenPrefix + base64.RawURLEncoding.EncodeToString(b)

	return secret, secret[:personalTokenHintLength], apiKeyHash(secret), nil
}

// tokenOwner is the signed-in person whose own tokens a request reaches; false under
// GOAL_AUTH=none, where the local user has none.
func (s *server) tokenOwner(ctx context.Context) (domain.UserID, bool) {
	p := principalOf(ctx)
	if s.auth.Mode == AuthNone || p.Kind != domain.PrincipalUser || p.User == "" {
		return "", false
	}

	return p.User, true
}

// ListPersonalTokens lists the caller's own tokens in the organization.
func (s *server) ListPersonalTokens(ctx context.Context, _ gen.ListPersonalTokensRequestObject) (gen.ListPersonalTokensResponseObject, error) {
	owner, ok := s.tokenOwner(ctx)
	if !ok {
		return gen.ListPersonalTokens200JSONResponse{Tokens: []gen.PersonalToken{}}, nil
	}

	tokens, err := s.access.PersonalTokens(ctx, &owner)
	if err != nil {
		return nil, err
	}

	return gen.ListPersonalTokens200JSONResponse{Tokens: s.personalTokensOut(tokens)}, nil
}

// CreatePersonalToken makes a token of the caller's (spec 3 §4.5): only in the browser, signed in
// within the last 10 minutes (the Authorizer refuses anything else), each scope a permission
// tokens may carry and held by the caller now. The secret is answered once, never stored and
// never logged.
func (s *server) CreatePersonalToken(ctx context.Context, req gen.CreatePersonalTokenRequestObject) (gen.CreatePersonalTokenResponseObject, error) {
	owner, ok := s.tokenOwner(ctx)
	if !ok {
		return nil, errTokensNeedSignIn
	}

	if p := principalOf(ctx); p.Token != nil || p.Session == nil || p.Session.Bearer {
		return nil, errBrowserSessionRequired
	}

	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("create personal token: the request has no actor")
	}

	n, err := s.newPersonalToken(ctx, owner, req.Body)
	if err != nil {
		return nil, err
	}

	secret, hint, hash, err := newPersonalTokenSecret()
	if err != nil {
		return nil, err
	}

	n.Hint, n.Hash = hint, hash

	t, err := s.access.CreatePersonalToken(ctx, by, n)
	if err != nil {
		return nil, err
	}

	t.Owner = domain.User{ID: owner, Name: principalOf(ctx).Name, Email: principalOf(ctx).Email}

	return gen.CreatePersonalToken201JSONResponse{
		Body:    gen.CreatedPersonalToken{Token: s.personalTokenOut(t), Secret: secret},
		Headers: gen.CreatePersonalToken201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

// newPersonalToken checks a request for a token: its name, expiry and scopes.
func (s *server) newPersonalToken(ctx context.Context, owner domain.UserID, b *gen.NewPersonalToken) (domain.NewPersonalToken, error) {
	name, ok := credentialName(b.Name)
	if !ok {
		return domain.NewPersonalToken{}, fmt.Errorf("%w: a token's name is 1 to %d characters without control characters",
			errInvalid, domain.MaxAPIKeyName)
	}

	days := domain.DefaultPersonalTokenDays
	if b.ExpiresInDays != nil {
		days = *b.ExpiresInDays
	}

	if days < 1 || days > domain.MaxPersonalTokenDays {
		return domain.NewPersonalToken{}, fmt.Errorf("%w: a token expires in 1 to %d days", errInvalid, domain.MaxPersonalTokenDays)
	}

	scopes := slices.Compact(slices.Sorted(slices.Values(b.Scopes)))
	if len(scopes) == 0 || len(scopes) > domain.MaxAPIKeyScopes {
		return domain.NewPersonalToken{}, fmt.Errorf("%w: a token has 1 to %d scopes", errInvalid, domain.MaxAPIKeyScopes)
	}

	n := domain.NewPersonalToken{Owner: owner, Name: name, ExpiresInDays: days}

	for _, scope := range scopes {
		if err := s.tokenScopeGrantable(ctx, domain.Permission(scope)); err != nil {
			return domain.NewPersonalToken{}, err
		}

		n.Scopes = append(n.Scopes, domain.Permission(scope))
	}

	return n, nil
}

// tokenScopeGrantable refuses a scope a token may not get: one tokens never carry, or one its
// creator does not hold now (an org permission by their role, a factory permission on at least
// one factory they see, or on every factory as an owner or admin, before there is any). A token
// could never use either.
func (s *server) tokenScopeGrantable(ctx context.Context, scope domain.Permission) error {
	ru, ok := permissionRules[string(scope)]
	if !ok || !ru.pat {
		return fmt.Errorf("%w: %s is not a permission personal tokens may carry", errScopeNotAllowed, scope)
	}

	held := func(f domain.FactoryID) bool {
		err := s.ask(ctx, scope, f)

		return err == nil || errors.Is(err, domain.ErrReauthRequired)
	}

	if ru.level != levelFactory {
		if !held("") {
			return fmt.Errorf("%w: you do not hold %s", errScopeNotAllowed, scope)
		}

		return nil
	}

	// Owners and admins act as editors on every factory, those made later included.
	if principalOf(ctx).RoleOn("").Includes(domain.FactoryRole(ru.factoryRole)) {
		return nil
	}

	for _, f := range s.visibleFactories(ctx) {
		if held(domain.FactoryID(f.Metadata.ID)) {
			return nil
		}
	}

	return fmt.Errorf("%w: you hold %s on no factory", errScopeNotAllowed, scope)
}

// RevokePersonalToken revokes one of the caller's own tokens, without a recent sign-in. Another
// member's token is not found, as an unknown one.
func (s *server) RevokePersonalToken(ctx context.Context, req gen.RevokePersonalTokenRequestObject) (gen.RevokePersonalTokenResponseObject, error) {
	owner, ok := s.tokenOwner(ctx)
	if !ok {
		return nil, fmt.Errorf("personal token %q: %w", req.Token, ports.ErrNotFound)
	}

	if err := s.revokePersonalToken(ctx, req.Token, &owner); err != nil {
		return nil, err
	}

	return gen.RevokePersonalToken204Response{}, nil
}

// ListMemberTokens lists every member's tokens in the organization, for owners and admins.
func (s *server) ListMemberTokens(ctx context.Context, _ gen.ListMemberTokensRequestObject) (gen.ListMemberTokensResponseObject, error) {
	tokens, err := s.access.PersonalTokens(ctx, nil)
	if err != nil {
		return nil, err
	}

	return gen.ListMemberTokens200JSONResponse{Tokens: s.personalTokensOut(tokens)}, nil
}

// RevokeMemberToken revokes any member's token, for owners and admins.
func (s *server) RevokeMemberToken(ctx context.Context, req gen.RevokeMemberTokenRequestObject) (gen.RevokeMemberTokenResponseObject, error) {
	if err := s.revokePersonalToken(ctx, req.Token, nil); err != nil {
		return nil, err
	}

	return gen.RevokeMemberToken204Response{}, nil
}

// revokePersonalToken revokes token id of owner (nil: any member's) as the request's actor.
func (s *server) revokePersonalToken(ctx context.Context, id string, owner *domain.UserID) error {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return errors.New("revoke personal token: the request has no actor")
	}

	err := s.access.RevokePersonalToken(ctx, by, domain.PersonalTokenID(id), owner)
	if errors.Is(err, ports.ErrNotFound) {
		return fmt.Errorf("personal token %q: %w", id, ports.ErrNotFound)
	}

	return err
}

func (s *server) personalTokensOut(tokens []domain.PersonalToken) []gen.PersonalToken {
	out := make([]gen.PersonalToken, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, s.personalTokenOut(t))
	}

	return out
}

// personalTokenOut is a token as the contract has it.
func (s *server) personalTokenOut(t domain.PersonalToken) gen.PersonalToken {
	out := gen.PersonalToken{
		Id: string(t.ID), Name: t.Name, Hint: t.Hint, Scopes: permissionsOut(t.Scopes),
		Owner:     gen.User{Id: string(t.Owner.ID), Name: t.Owner.Name, Email: t.Owner.Email},
		CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt,
	}

	if t.RevokedBy != nil {
		by := s.actorOut(*t.RevokedBy)
		out.RevokedBy = &by
	}

	return out
}
