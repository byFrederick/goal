package domain

import (
	"errors"
	"maps"
	"testing"
)

// The defaults are the boxes account.html checks (ADR 0023, decision 1).
func TestDefaultNotificationPreferences_AreTheADRs(t *testing.T) {
	t.Parallel()

	p := DefaultNotificationPreferences()

	want := map[NotificationEvent]NotificationChannels{
		NotifyApproval: {InGoal: true, Slack: true},
		NotifyQuestion: {InGoal: true, Slack: true},
		NotifyAndon:    {InGoal: true, Email: true, Slack: true},
		NotifyBudget:   {InGoal: true, Email: true},
		NotifyDigest:   {Email: true},
	}

	if !maps.Equal(p.Events, want) || p.Email != "" || p.TimeZone != "UTC" {
		t.Fatalf("defaults = %+v, want events %v, a blank email and UTC", p, want)
	}

	if err := p.Validate(); err != nil {
		t.Fatalf("the defaults are invalid: %v", err)
	}

	// Each call is its own copy.
	p.Events[NotifyAndon] = NotificationChannels{}
	if DefaultNotificationPreferences().Events[NotifyAndon] != want[NotifyAndon] {
		t.Fatal("changing one default changed the next")
	}
}

func TestNotificationPreferences_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		edit func(p *NotificationPreferences)
		want error // nil: valid
	}{
		{"the defaults", func(*NotificationPreferences) {}, nil},
		{"an email and a zone", func(p *NotificationPreferences) { p.Email, p.TimeZone = "ada@example.com", "Europe/Berlin" }, nil},
		{"everything off", func(p *NotificationPreferences) {
			for e := range p.Events {
				p.Events[e] = NotificationChannels{}
			}
		}, nil},
		{"digest in Goal", func(p *NotificationPreferences) { p.Events[NotifyDigest] = NotificationChannels{InGoal: true} }, ErrNotificationPreferences},
		{"an unknown event", func(p *NotificationPreferences) { p.Events["birthday"] = NotificationChannels{Email: true} }, ErrNotificationPreferences},
		{"a missing event", func(p *NotificationPreferences) { delete(p.Events, NotifyBudget) }, ErrNotificationPreferences},
		{"an email with a name", func(p *NotificationPreferences) { p.Email = "Ada <ada@example.com>" }, ErrNotificationEmail},
		{"an email with spaces", func(p *NotificationPreferences) { p.Email = " ada@example.com" }, ErrNotificationEmail},
		{"not an email", func(p *NotificationPreferences) { p.Email = "ada" }, ErrNotificationEmail},
		{"an unknown zone", func(p *NotificationPreferences) { p.TimeZone = "Mars/Olympus" }, ErrTimeZone},
		{"the local zone", func(p *NotificationPreferences) { p.TimeZone = "Local" }, ErrTimeZone},
		{"no zone", func(p *NotificationPreferences) { p.TimeZone = "" }, ErrTimeZone},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := DefaultNotificationPreferences()
			tc.edit(&p)

			err := p.Validate()

			switch {
			case tc.want == nil && err != nil:
				t.Fatalf("Validate = %v, want nil", err)
			case tc.want != nil && (!errors.Is(err, tc.want) || !errors.Is(err, ErrInvalid)):
				t.Fatalf("Validate = %v, want %v and ErrInvalid", err, tc.want)
			}
		})
	}
}
