package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

func TestCron_Next(t *testing.T) {
	t.Parallel()

	at := func(s string) time.Time {
		t.Helper()

		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}

		return v
	}

	tests := []struct {
		name, cron, zone, after, want string
	}{
		{"every minute", "* * * * *", "", "2026-10-27T10:00:30Z", "2026-10-27T10:01:00Z"},
		{"strictly after an instant it matches", "0 8 * * *", "", "2026-10-27T08:00:00Z", "2026-10-28T08:00:00Z"},
		{"steps", "*/15 * * * *", "", "2026-10-27T10:16:00Z", "2026-10-27T10:30:00Z"},
		{"weekday names in a time zone", "0 8 * * MON-FRI", "Europe/Berlin", "2026-10-30T07:00:00Z", "2026-11-02T07:00:00Z"},
		{"month names roll the year", "0 0 1 JAN *", "", "2026-10-27T00:00:00Z", "2027-01-01T00:00:00Z"},
		{"day of month or day of week", "0 12 15 * SUN", "", "2026-10-27T00:00:00Z", "2026-11-01T12:00:00Z"},
		{"a list", "5,45 9 * * *", "", "2026-10-27T09:06:00Z", "2026-10-27T09:45:00Z"},
		{"across a clock change", "30 2 * * *", "Europe/Berlin", "2026-10-24T12:00:00Z", "2026-10-25T00:30:00Z"},
		{"never", "0 0 30 FEB *", "", "2026-10-27T00:00:00Z", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c, err := domain.ParseCron(tc.cron, tc.zone)
			if err != nil {
				t.Fatal(err)
			}

			got := c.Next(at(tc.after))

			switch {
			case tc.want == "" && !got.IsZero():
				t.Errorf("Next = %s; want none", got)
			case tc.want != "" && !got.Equal(at(tc.want)):
				t.Errorf("Next = %s; want %s", got, tc.want)
			}
		})
	}
}

func TestCron_LatestIsTheNewestInTheWindow(t *testing.T) {
	t.Parallel()

	c, err := domain.ParseCron("*/2 * * * *", "")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 27, 10, 5, 30, 0, time.UTC)
	if got, want := c.Latest(now.Add(-5*time.Minute), now), now.Truncate(time.Minute).Add(-time.Minute); !got.Equal(want) {
		t.Errorf("Latest = %s; want %s", got, want)
	}

	hourly, err := domain.ParseCron("0 * * * *", "")
	if err != nil {
		t.Fatal(err)
	}

	if got := hourly.Latest(now.Add(-5*time.Minute), now); !got.IsZero() {
		t.Errorf("Latest = %s; want none within five minutes", got)
	}
}

func TestCron_ParseRefusesWhatConfigRefuses(t *testing.T) {
	t.Parallel()

	for _, expr := range []string{"* * * *", "60 * * * *", "5/2 * * * *", "* * * * 7", "10-5 * * * *", "@daily", "*/0 * * * *"} {
		if _, err := domain.ParseCron(expr, ""); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("ParseCron(%q) = %v; want ErrInvalid", expr, err)
		}
	}

	if _, err := domain.ParseCron("* * * * *", "Mars/Olympus"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("an unknown time zone = %v; want ErrInvalid", err)
	}
}
