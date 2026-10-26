package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Platform keeps the install's platform admins and its security log; ports.Platform (the
// PostgreSQL adapter) satisfies it.
type Platform interface {
	PlatformAdmins(ctx context.Context) ([]domain.PlatformAdmin, error)
	AddPlatformAdmin(ctx context.Context, by domain.Actor, email string) (domain.PlatformAdmin, error)
	RemovePlatformAdmin(ctx context.Context, by domain.Actor, user domain.UserID) error
	AuthEvents(ctx context.Context, q ports.AuthEventQuery) ([]domain.AuthEvent, error)
}

var _ Platform = ports.Platform(nil)

// Security log pages.
const (
	defaultAuthEventLimit = 100
	maxAuthEventLimit     = 500
)

// Refusals of the platform routes.
var (
	errLastPlatformAdmin = fmt.Errorf("the install keeps at least one platform admin (goal db recover-admin adds one): %w", domain.ErrInvalid)
	errNoSuchUser        = fmt.Errorf("no one has signed in with that email; invite them to an organization first: %w", ports.ErrNotFound)
	errNotPlatformAdmin  = fmt.Errorf("that person is not a platform admin: %w", ports.ErrNotFound)
)

// ListPlatformAdmins lists the install's platform admins, earliest first.
func (s *server) ListPlatformAdmins(ctx context.Context, _ gen.ListPlatformAdminsRequestObject) (gen.ListPlatformAdminsResponseObject, error) {
	admins, err := s.platform.PlatformAdmins(ctx)
	if err != nil {
		return nil, err
	}

	out := gen.ListPlatformAdmins200JSONResponse{Admins: make([]gen.PlatformAdmin, 0, len(admins))}
	for _, a := range admins {
		out.Admins = append(out.Admins, s.platformAdminOut(a))
	}

	return out, nil
}

// AddPlatformAdmin makes the person who signs in with the email a platform admin.
func (s *server) AddPlatformAdmin(ctx context.Context, req gen.AddPlatformAdminRequestObject) (gen.AddPlatformAdminResponseObject, error) {
	by, err := actorOf(ctx, "add platform admin")
	if err != nil {
		return nil, err
	}

	email := strings.TrimSpace(req.Body.Email)
	if email == "" {
		return nil, fmt.Errorf("%w: an email is required", errInvalid)
	}

	a, err := s.platform.AddPlatformAdmin(ctx, by, email)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, errNoSuchUser
	}

	if err != nil {
		return nil, err
	}

	return gen.AddPlatformAdmin201JSONResponse(s.platformAdminOut(a)), nil
}

// RemovePlatformAdmin ends a person's being a platform admin, unless they are the last one.
func (s *server) RemovePlatformAdmin(ctx context.Context, req gen.RemovePlatformAdminRequestObject) (gen.RemovePlatformAdminResponseObject, error) {
	by, err := actorOf(ctx, "remove platform admin")
	if err != nil {
		return nil, err
	}

	switch err := s.platform.RemovePlatformAdmin(ctx, by, domain.UserID(req.User)); {
	case errors.Is(err, ports.ErrConflict):
		return nil, errLastPlatformAdmin
	case errors.Is(err, ports.ErrNotFound):
		return nil, errNotPlatformAdmin
	case err != nil:
		return nil, err
	}

	return gen.RemovePlatformAdmin204Response{}, nil
}

// ListAuthEvents is one page of the install's security log, newest first.
func (s *server) ListAuthEvents(ctx context.Context, req gen.ListAuthEventsRequestObject) (gen.ListAuthEventsResponseObject, error) {
	p := req.Params

	limit := defaultAuthEventLimit
	if p.Limit != nil {
		limit = *p.Limit
	}

	if limit < 1 || limit > maxAuthEventLimit {
		return nil, fmt.Errorf("%w: limit %d is not 1 to %d", errInvalid, limit, maxAuthEventLimit)
	}

	// One more than the page, to know whether another follows.
	events, err := s.platform.AuthEvents(ctx, ports.AuthEventQuery{Before: deref(p.Before), Limit: limit + 1, Kind: deref(p.Kind)})
	if err != nil {
		return nil, err
	}

	out := gen.ListAuthEvents200JSONResponse{Events: make([]gen.AuthEvent, 0, min(len(events), limit))}

	if len(events) > limit {
		events = events[:limit]
		out.NextBefore = &events[limit-1].ID
	}

	for _, e := range events {
		data := e.Data
		if data == nil {
			data = map[string]any{}
		}

		out.Events = append(out.Events, gen.AuthEvent{
			Id: e.ID, At: e.At, Kind: e.Kind, UserId: optional(string(e.User)), Subject: optional(e.Subject),
			Ip: optional(e.IP), UserAgent: optional(e.UserAgent), Data: data,
		})
	}

	return out, nil
}

func (s *server) platformAdminOut(a domain.PlatformAdmin) gen.PlatformAdmin {
	return gen.PlatformAdmin{
		User:    gen.User{Id: string(a.User.ID), Name: a.User.Name, Email: a.User.Email},
		AddedBy: s.actorOut(a.AddedBy), AddedAt: a.AddedAt,
	}
}
