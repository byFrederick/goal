package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The notification preferences routes (/v1/me/notifications, ADR 0023 decision 1): a signed-in
// person reads and replaces their own, and nobody else's. Their permission, notifications:manage,
// is an install permission no API key or personal token holds; GOAL_AUTH=none's local user passes
// authorization but is no person, so it is refused (403 person-required) like a saved view's
// writes.

// errNotificationsNeedPerson refuses a caller who is not a signed-in person (403 person-required).
var errNotificationsNeedPerson = errors.New("only a signed-in person has notification preferences")

// notificationProblem answers the preferences' refusals with their codes: a caller who is no
// person, then the email and the time zone each with their own, every other rule
// invalid-notification-preferences.
func notificationProblem(err error) (gen.Problem, bool) {
	answers := []struct {
		err    error
		status int
		code   string
	}{
		{errNotificationsNeedPerson, http.StatusForbidden, "person-required"},
		{domain.ErrNotificationEmail, http.StatusBadRequest, "invalid-notification-email"},
		{domain.ErrTimeZone, http.StatusBadRequest, "invalid-time-zone"},
		{domain.ErrNotificationPreferences, http.StatusBadRequest, "invalid-notification-preferences"},
	}

	for _, a := range answers {
		if errors.Is(err, a.err) {
			p := detailed(a.status, err)
			p.Code = optional(a.code)

			return p, true
		}
	}

	return gen.Problem{}, false
}

func (s *server) GetNotificationPreferences(ctx context.Context, _ gen.GetNotificationPreferencesRequestObject) (gen.GetNotificationPreferencesResponseObject, error) {
	user, err := s.notificationsOwner(ctx)
	if err != nil {
		return nil, err
	}

	p, err := s.notifications.NotificationPreferences(ctx, user)
	if err != nil {
		return nil, err
	}

	return gen.GetNotificationPreferences200JSONResponse(notificationsOut(p)), nil
}

func (s *server) PutNotificationPreferences(ctx context.Context, req gen.PutNotificationPreferencesRequestObject) (gen.PutNotificationPreferencesResponseObject, error) {
	user, err := s.notificationsOwner(ctx)
	if err != nil {
		return nil, err
	}

	if err := s.notifications.SetNotificationPreferences(ctx, user, notificationsIn(*req.Body)); err != nil {
		return nil, err
	}

	p, err := s.notifications.NotificationPreferences(ctx, user)
	if err != nil {
		return nil, err
	}

	return gen.PutNotificationPreferences200JSONResponse(notificationsOut(p)), nil
}

func notificationsIn(b gen.NotificationPreferences) domain.NotificationPreferences {
	channels := func(c gen.NotificationChannels) domain.NotificationChannels {
		return domain.NotificationChannels{InGoal: c.InGoal, Email: c.Email, Slack: c.Slack}
	}

	return domain.NotificationPreferences{
		Events: map[domain.NotificationEvent]domain.NotificationChannels{
			domain.NotifyApproval: channels(b.Events.Approval),
			domain.NotifyQuestion: channels(b.Events.Question),
			domain.NotifyAndon:    channels(b.Events.Andon),
			domain.NotifyBudget:   channels(b.Events.Budget),
			domain.NotifyDigest:   channels(b.Events.Digest),
		},
		Email:    b.Email,
		TimeZone: b.TimeZone,
	}
}

func notificationsOut(p domain.NotificationPreferences) gen.NotificationPreferences {
	channels := func(e domain.NotificationEvent) gen.NotificationChannels {
		c := p.Events[e]
		return gen.NotificationChannels{InGoal: c.InGoal, Email: c.Email, Slack: c.Slack}
	}

	return gen.NotificationPreferences{
		Events: gen.NotificationEvents{
			Approval: channels(domain.NotifyApproval),
			Question: channels(domain.NotifyQuestion),
			Andon:    channels(domain.NotifyAndon),
			Budget:   channels(domain.NotifyBudget),
			Digest:   channels(domain.NotifyDigest),
		},
		Email:    p.Email,
		TimeZone: p.TimeZone,
	}
}

// notificationsOwner is the signed-in person whose preferences the request reads or writes.
func (s *server) notificationsOwner(ctx context.Context) (domain.UserID, error) {
	user, err := s.viewOwner(ctx)
	if err != nil {
		return "", errNotificationsNeedPerson
	}

	return user, nil
}
