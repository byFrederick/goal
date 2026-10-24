package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Access keeps the organization's people and keys; ports.Access (the PostgreSQL adapter)
// satisfies it.
type Access interface {
	APIKeys(ctx context.Context) ([]domain.APIKey, error)
	CreateAPIKey(ctx context.Context, by domain.Actor, k domain.NewAPIKey) (domain.APIKey, error)
	RevokeAPIKey(ctx context.Context, by domain.Actor, id domain.APIKeyID) error
}

var _ Access = ports.Access(nil)

// apiKeyHintLength is how much of a secret a key's hint shows: goal_sk_ and four characters.
const apiKeyHintLength = len(apiKeyPrefix) + 4

// errScopeNotAllowed marks an API key scope keys may not carry, or that the key's creator or its
// ceiling does not hold (400 scope-not-allowed).
var errScopeNotAllowed = errors.New("scope not allowed")

// apiKeyProblem answers the API key rules with their codes.
func apiKeyProblem(err error) (gen.Problem, bool) {
	switch {
	case errors.Is(err, errScopeNotAllowed):
		p := detailed(http.StatusBadRequest, err)
		p.Code = optional("scope-not-allowed")

		return p, true
	case errors.Is(err, domain.ErrAPIKeyNameTaken):
		p := detailed(http.StatusConflict, err)
		p.Code = optional("api-key-name-taken")

		return p, true
	default:
		return gen.Problem{}, false
	}
}

// newAPIKeySecret makes a key: goal_sk_ and base64url of 32 bytes from crypto/rand. Only its
// hash (apiKeyHash) and hint are kept.
func newAPIKeySecret() (secret, hint, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("api key secret: %w", err)
	}

	secret = apiKeyPrefix + base64.RawURLEncoding.EncodeToString(b)

	return secret, secret[:apiKeyHintLength], apiKeyHash(secret), nil
}

func (s *server) ListAPIKeys(ctx context.Context, _ gen.ListAPIKeysRequestObject) (gen.ListAPIKeysResponseObject, error) {
	keys, err := s.access.APIKeys(ctx)
	if err != nil {
		return nil, err
	}

	out := gen.ListAPIKeys200JSONResponse{Keys: make([]gen.ApiKey, 0, len(keys))}
	for _, k := range keys {
		out.Keys = append(out.Keys, s.apiKeyOut(k))
	}

	return out, nil
}

// CreateAPIKey makes a key of the organization (spec 3 §4.3): its scopes must be permissions
// keys may carry, held by the creator now, and a factory scope held by the key's ceiling. The
// secret is answered once, never stored and never logged.
func (s *server) CreateAPIKey(ctx context.Context, req gen.CreateAPIKeyRequestObject) (gen.CreateAPIKeyResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("create api key: the request has no actor")
	}

	n, err := s.newAPIKey(ctx, req.Body)
	if err != nil {
		return nil, err
	}

	secret, hint, hash, err := newAPIKeySecret()
	if err != nil {
		return nil, err
	}

	n.Hint, n.Hash = hint, hash

	k, err := s.access.CreateAPIKey(ctx, by, n)
	if err != nil {
		return nil, err
	}

	return gen.CreateAPIKey201JSONResponse{
		Body:    gen.CreatedApiKey{Key: s.apiKeyOut(k), Secret: secret},
		Headers: gen.CreateAPIKey201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

// newAPIKey checks a request for a key: its name, expiry, ceiling, factories (resolved like a
// path's, so an unknown or invisible one is factory-not-found) and scopes.
func (s *server) newAPIKey(ctx context.Context, b *gen.NewApiKey) (domain.NewAPIKey, error) {
	n, err := keyShape(b)
	if err != nil {
		return domain.NewAPIKey{}, err
	}

	if n.Factories, err = s.keyFactories(ctx, b.Factories); err != nil {
		return domain.NewAPIKey{}, err
	}

	scopes := slices.Compact(slices.Sorted(slices.Values(b.Scopes)))
	if len(scopes) == 0 || len(scopes) > domain.MaxAPIKeyScopes {
		return domain.NewAPIKey{}, fmt.Errorf("%w: a key has 1 to %d scopes", errInvalid, domain.MaxAPIKeyScopes)
	}

	for _, scope := range scopes {
		if err := s.grantable(ctx, domain.Permission(scope), n.Ceiling, n.Factories); err != nil {
			return domain.NewAPIKey{}, err
		}

		n.Scopes = append(n.Scopes, domain.Permission(scope))
	}

	return n, nil
}

// keyShape checks a key's name, expiry and ceiling.
func keyShape(b *gen.NewApiKey) (domain.NewAPIKey, error) {
	name := strings.TrimSpace(b.Name)
	if n := utf8.RuneCountInString(name); n == 0 || n > domain.MaxAPIKeyName || strings.ContainsFunc(name, unicode.IsControl) {
		return domain.NewAPIKey{}, fmt.Errorf("%w: a key's name is 1 to %d characters without control characters", errInvalid, domain.MaxAPIKeyName)
	}

	days := domain.DefaultAPIKeyDays
	if b.ExpiresInDays != nil {
		days = *b.ExpiresInDays
	}

	if days < 1 || days > domain.MaxAPIKeyDays {
		return domain.NewAPIKey{}, fmt.Errorf("%w: a key expires in 1 to %d days", errInvalid, domain.MaxAPIKeyDays)
	}

	ceiling := domain.FactoryRole(b.RoleCeiling)
	if !ceiling.Includes(ceiling) {
		return domain.NewAPIKey{}, fmt.Errorf("%w: role_ceiling %q is not viewer, approver or editor", errInvalid, ceiling)
	}

	return domain.NewAPIKey{Name: name, Ceiling: ceiling, ExpiresInDays: days}, nil
}

// keyFactories resolves the factories a key may reach, each like a path's factory; nil (every
// factory) when none are named.
func (s *server) keyFactories(ctx context.Context, refs *[]string) ([]domain.FactoryID, error) {
	if refs == nil {
		return nil, nil
	}

	if len(*refs) == 0 {
		return nil, fmt.Errorf("%w: name at least one factory, or leave factories out for every factory", errInvalid)
	}

	out := []domain.FactoryID{}

	for _, ref := range *refs {
		f, err := s.factory(ctx, ref)
		if err != nil {
			return nil, err
		}

		if id := domain.FactoryID(f.Metadata.ID); !slices.Contains(out, id) {
			out = append(out, id)
		}
	}

	return out, nil
}

// grantable refuses a scope a key may not get: one keys never carry, a factory permission above
// the key's ceiling (the key could never use it), or one its creator does not hold now, on each
// factory the key reaches (every factory they see for an unrestricted key) for a factory
// permission.
func (s *server) grantable(ctx context.Context, scope domain.Permission, ceiling domain.FactoryRole, factories []domain.FactoryID) error {
	ru, ok := permissionRules[string(scope)]
	if !ok || !ru.keys {
		return fmt.Errorf("%w: %s is not a permission API keys may carry", errScopeNotAllowed, scope)
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

	if !ceiling.Includes(domain.FactoryRole(ru.factoryRole)) {
		return fmt.Errorf("%w: %s needs a ceiling of at least %s", errScopeNotAllowed, scope, ru.factoryRole)
	}

	reach := factories
	if reach == nil {
		for _, f := range s.visibleFactories(ctx) {
			reach = append(reach, domain.FactoryID(f.Metadata.ID))
		}
	}

	for _, f := range reach {
		if !held(f) {
			return fmt.Errorf("%w: you do not hold %s on every factory the key reaches", errScopeNotAllowed, scope)
		}
	}

	return nil
}

func (s *server) RevokeAPIKey(ctx context.Context, req gen.RevokeAPIKeyRequestObject) (gen.RevokeAPIKeyResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("revoke api key: the request has no actor")
	}

	if err := s.access.RevokeAPIKey(ctx, by, domain.APIKeyID(req.Key)); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return nil, fmt.Errorf("api key %q: %w", req.Key, ports.ErrNotFound)
		}

		return nil, err
	}

	return gen.RevokeAPIKey204Response{}, nil
}

// apiKeyOut is a key as the contract has it.
func (s *server) apiKeyOut(k domain.APIKey) gen.ApiKey {
	out := gen.ApiKey{
		Id: string(k.ID), Name: k.Name, Hint: k.Hint, Scopes: permissionsOut(k.Scopes), RoleCeiling: gen.FactoryRole(k.Ceiling),
		CreatedBy: s.actorOut(k.CreatedBy), CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt, RevokedAt: k.RevokedAt,
	}

	if k.Factories != nil {
		fs := make([]string, 0, len(k.Factories))
		for _, f := range k.Factories {
			fs = append(fs, string(f))
		}

		out.Factories = &fs
	}

	if k.RevokedBy != nil {
		by := s.actorOut(*k.RevokedBy)
		out.RevokedBy = &by
	}

	return out
}
