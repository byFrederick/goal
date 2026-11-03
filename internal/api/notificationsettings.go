package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The organization's notification settings (ADR 0037, Consequences on ADR 0023's Slack
// notifications): GET and PUT /v1/orgs/{org}/settings/notifications read and choose the secret
// whose Slack bot token sends the organization's Slack notifications, by name, and POST .../test
// asks Slack whether a secret's token works without saving anything. Notifications open the
// secret without a factory, so it must be open to all factories. The token never leaves goald: a
// test answers a fixed code, and the store audits a change with the secret's name only.

// slackNotSet is a test's error when no secret is given and none is saved.
const slackNotSet = "slack-not-set"

// slackTokenUnavailable is a test's error when Slack could not say whether the token works.
const slackTokenUnavailable = "slack-token-unavailable"

func (s *server) GetNotificationSettings(ctx context.Context, _ gen.GetNotificationSettingsRequestObject) (gen.GetNotificationSettingsResponseObject, error) {
	ns, err := s.notifySettings.NotificationSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("get the notification settings: %w", err)
	}

	return gen.GetNotificationSettings200JSONResponse(notificationSettingsOut(ns)), nil
}

// SetNotificationSettings saves the Slack secret, or turns Slack off with null. A secret limited
// to some factories is refused before anything is saved, since notifications open it without one.
func (s *server) SetNotificationSettings(ctx context.Context, req gen.SetNotificationSettingsRequestObject) (gen.SetNotificationSettingsResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("set the notification settings: the request has no actor")
	}

	ns := domain.NotificationSettings{SlackSecret: deref(req.Body.SlackSecret)}

	if ns.SlackSecret != "" {
		sec, err := s.secrets.Get(ctx, ns.SlackSecret)
		if err != nil {
			return nil, secretError(err)
		}

		if !sec.Scope.All {
			return nil, fmt.Errorf("%w: secret %q is open to some factories only; notifications open it without a factory, so open it to all factories first",
				errSecretNotAllowed, sec.Name)
		}
	}

	if err := s.notifySettings.SetNotificationSettings(ctx, ns, by); err != nil {
		return nil, secretError(err)
	}

	return gen.SetNotificationSettings200JSONResponse(notificationSettingsOut(ns)), nil
}

// TestNotificationSettings opens the given secret, or the saved setting's, and asks Slack whether
// its token works. A token that fails is an answer with a fixed code, not an error.
func (s *server) TestNotificationSettings(ctx context.Context, req gen.TestNotificationSettingsRequestObject) (gen.TestNotificationSettingsResponseObject, error) {
	var name string
	if req.Body != nil {
		name = deref(req.Body.SlackSecret)
	}

	if name == "" {
		ns, err := s.notifySettings.NotificationSettings(ctx)
		if err != nil {
			return nil, fmt.Errorf("test the notification settings: %w", err)
		}

		name = ns.SlackSecret
	}

	if name == "" {
		return gen.TestNotificationSettings200JSONResponse(gen.NotificationTestResult{Error: optional(slackNotSet)}), nil
	}

	opened, err := s.secrets.Resolve(ctx, []string{name}, domain.SecretUse{})
	if err != nil {
		return nil, secretError(err)
	}

	workspace, err := s.slack.Check(ctx, opened[0].Value)
	if err != nil {
		// A DMError's code holds no token; anything else is not repeated.
		code := slackTokenUnavailable
		if de, ok := errors.AsType[*ports.DMError](err); ok {
			code = de.Code
		}

		s.log.InfoContext(ctx, "notification test failed", slog.String("secret", name), slog.String("code", code))

		return gen.TestNotificationSettings200JSONResponse(gen.NotificationTestResult{Error: optional(code)}), nil
	}

	return gen.TestNotificationSettings200JSONResponse(gen.NotificationTestResult{Ok: true, Workspace: optional(workspace)}), nil
}

// notificationSettingsOut is ns as the contract has it: Slack off is null.
func notificationSettingsOut(ns domain.NotificationSettings) gen.NotificationSettings {
	if ns.SlackSecret == "" {
		return gen.NotificationSettings{}
	}

	return gen.NotificationSettings{SlackSecret: optional(ns.SlackSecret)}
}
