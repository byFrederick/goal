package api

import (
	"context"
	"fmt"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
)

// GetAuthConfig tells the sign-in page how this install signs people in. goald serves only
// GOAL_AUTH=none until it checks sessions (spec 3, PR 3-9): sign-in is off, and the UI shows no
// sign-in page.
func (s *server) GetAuthConfig(context.Context, gen.GetAuthConfigRequestObject) (gen.GetAuthConfigResponseObject, error) {
	return gen.GetAuthConfig200JSONResponse{Mode: gen.None, Methods: []gen.SignInMethod{}}, nil
}

// GetMe answers who the request acts for: the local user under GOAL_AUTH=none (mode none, no
// user), a signed-in person with their session, or an API key.
func (s *server) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	p, ok := domain.PrincipalFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("no principal: %w", domain.ErrUnauthenticated)
	}

	switch {
	case p.Kind == domain.PrincipalUser && p.User == domain.UserID(domain.LocalUser.ID):
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
