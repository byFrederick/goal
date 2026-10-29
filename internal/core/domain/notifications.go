package domain

import (
	"errors"
	"fmt"
	"maps"
	"net/mail"
	"slices"
	"time"
)

// NotificationEvent is something Goal tells a person about (ADR 0023, decision 1).
type NotificationEvent string

// The notification events.
const (
	// NotifyApproval: an agent needs the person's OK for a write.
	NotifyApproval NotificationEvent = "approval"
	// NotifyQuestion: an agent asks the person a question.
	NotifyQuestion NotificationEvent = "question"
	// NotifyAndon: a factory pulls the andon.
	NotifyAndon NotificationEvent = "andon"
	// NotifyBudget: a budget passes 80%.
	NotifyBudget NotificationEvent = "budget"
	// NotifyDigest: the daily digest at 08:00 in the person's time zone. It has no In Goal channel.
	NotifyDigest NotificationEvent = "digest"
)

// NotificationEvents are the events, in the order the account screen lists them.
var NotificationEvents = []NotificationEvent{NotifyApproval, NotifyQuestion, NotifyAndon, NotifyBudget, NotifyDigest}

// NotificationChannels are the channels one event is sent on: In Goal (the app's badge and
// toast), email and Slack.
type NotificationChannels struct {
	InGoal, Email, Slack bool
}

// DefaultTimeZone is a person's time zone until they set one.
const DefaultTimeZone = "UTC"

// NotificationPreferences are one person's notification preferences, which hold in every
// organization they belong to: the channels of every event, the notification email (blank: the
// sign-in email) and an IANA time zone for the digest.
type NotificationPreferences struct {
	Events   map[NotificationEvent]NotificationChannels
	Email    string
	TimeZone string
}

// Errors Validate wraps, each with ErrInvalid, so callers can say which part was refused.
var (
	ErrNotificationEmail       = fmt.Errorf("notification email is not a plain address: %w", ErrInvalid)
	ErrTimeZone                = fmt.Errorf("time zone is not an IANA name: %w", ErrInvalid)
	ErrNotificationPreferences = fmt.Errorf("notification preferences: %w", ErrInvalid)
)

// DefaultNotificationPreferences are a person's preferences until they save any: the boxes
// account.html checks, a blank email and UTC. Each call returns its own copy.
func DefaultNotificationPreferences() NotificationPreferences {
	return NotificationPreferences{
		Events: map[NotificationEvent]NotificationChannels{
			NotifyApproval: {InGoal: true, Slack: true},
			NotifyQuestion: {InGoal: true, Slack: true},
			NotifyAndon:    {InGoal: true, Email: true, Slack: true},
			NotifyBudget:   {InGoal: true, Email: true},
			NotifyDigest:   {Email: true},
		},
		TimeZone: DefaultTimeZone,
	}
}

// Clone returns a copy of p that shares no map with it.
func (p NotificationPreferences) Clone() NotificationPreferences {
	p.Events = maps.Clone(p.Events)

	return p
}

// Validate reports what makes p impossible to save: an event missing or unknown, digest In Goal
// (ErrNotificationPreferences), an email net/mail does not parse to exactly itself
// (ErrNotificationEmail; blank is allowed), and a time zone time.LoadLocation refuses, blank or
// Local (ErrTimeZone).
func (p NotificationPreferences) Validate() error {
	var errs []error

	for _, e := range NotificationEvents {
		if _, ok := p.Events[e]; !ok {
			errs = append(errs, fmt.Errorf("event %s is missing: %w", e, ErrNotificationPreferences))
		}
	}

	for _, e := range slices.Sorted(maps.Keys(p.Events)) {
		if !slices.Contains(NotificationEvents, e) {
			errs = append(errs, fmt.Errorf("event %q is unknown: %w", e, ErrNotificationPreferences))
		}
	}

	if p.Events[NotifyDigest].InGoal {
		errs = append(errs, fmt.Errorf("the digest has no In Goal channel: %w", ErrNotificationPreferences))
	}

	if p.Email != "" {
		if a, err := mail.ParseAddress(p.Email); err != nil || a.Name != "" || a.Address != p.Email {
			errs = append(errs, fmt.Errorf("%q: %w", p.Email, ErrNotificationEmail))
		}
	}

	if p.TimeZone == "" || p.TimeZone == "Local" {
		errs = append(errs, fmt.Errorf("%q: %w", p.TimeZone, ErrTimeZone))
	} else if _, err := time.LoadLocation(p.TimeZone); err != nil {
		errs = append(errs, fmt.Errorf("%q: %w", p.TimeZone, ErrTimeZone))
	}

	return errors.Join(errs...)
}
