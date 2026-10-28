// Package notificationtest is the conformance suite for ports.NotificationPreferences (ADR 0023,
// decision 1). The in-memory fake in porttest and the PostgreSQL adapter run it, so tests on the
// fake hold for the database's rules: the defaults when nothing is saved, a save replacing
// everything, invalid preferences refused and people kept apart.
package notificationtest

import (
	"errors"
	"maps"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Harness is one NotificationPreferences under test.
type Harness struct {
	Prefs ports.NotificationPreferences
	// NewUser returns a new person.
	NewUser func(t *testing.T) domain.UserID
}

// Run runs the suite. Tests run in parallel and call newHarness once each; harnesses must be
// isolated from each other.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(t *testing.T, h Harness)
	}{
		{"Get_NothingSavedIsTheDefault", testDefault},
		{"Set_ThenGetRoundTrips", testRoundTrip},
		{"Set_ReplacesEverything", testReplace},
		{"Set_InvalidRefused", testInvalid},
		{"Set_OtherUserUnchanged", testIsolation},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.test(t, newHarness(t))
		})
	}
}

func get(t *testing.T, h Harness, user domain.UserID) domain.NotificationPreferences {
	t.Helper()

	p, err := h.Prefs.NotificationPreferences(t.Context(), user)
	if err != nil {
		t.Fatalf("NotificationPreferences: %v", err)
	}

	return p
}

func set(t *testing.T, h Harness, user domain.UserID, p domain.NotificationPreferences) {
	t.Helper()

	if err := h.Prefs.SetNotificationPreferences(t.Context(), user, p); err != nil {
		t.Fatalf("SetNotificationPreferences: %v", err)
	}
}

func equal(a, b domain.NotificationPreferences) bool {
	return a.Email == b.Email && a.TimeZone == b.TimeZone && maps.Equal(a.Events, b.Events)
}

func want(t *testing.T, got, want domain.NotificationPreferences) {
	t.Helper()

	if !equal(got, want) {
		t.Fatalf("preferences = %+v, want %+v", got, want)
	}
}

// changed is the defaults with every channel flipped where it may be, an email and a zone.
func changed() domain.NotificationPreferences {
	p := domain.DefaultNotificationPreferences()
	for e, c := range p.Events {
		p.Events[e] = domain.NotificationChannels{InGoal: !c.InGoal && e != domain.NotifyDigest, Email: !c.Email, Slack: !c.Slack}
	}

	p.Email, p.TimeZone = "ada@example.com", "Europe/Berlin"

	return p
}

func testDefault(t *testing.T, h Harness) {
	want(t, get(t, h, h.NewUser(t)), domain.DefaultNotificationPreferences())
}

func testRoundTrip(t *testing.T, h Harness) {
	user := h.NewUser(t)
	p := changed()
	set(t, h, user, p)

	got := get(t, h, user)
	want(t, got, p)

	// What a read returns is the caller's own.
	got.Events[domain.NotifyAndon] = domain.NotificationChannels{}
	want(t, get(t, h, user), p)
}

func testReplace(t *testing.T, h Harness) {
	user := h.NewUser(t)
	set(t, h, user, changed())

	// Back to the defaults, the blank email included.
	set(t, h, user, domain.DefaultNotificationPreferences())
	want(t, get(t, h, user), domain.DefaultNotificationPreferences())
}

func testInvalid(t *testing.T, h Harness) {
	user := h.NewUser(t)
	set(t, h, user, changed())

	for name, edit := range map[string]func(p *domain.NotificationPreferences){
		"digest in Goal": func(p *domain.NotificationPreferences) { p.Events[domain.NotifyDigest] = domain.NotificationChannels{InGoal: true} },
		"a bad email":    func(p *domain.NotificationPreferences) { p.Email = "not an email" },
		"a bad zone":     func(p *domain.NotificationPreferences) { p.TimeZone = "Mars/Olympus" },
	} {
		p := domain.DefaultNotificationPreferences()
		edit(&p)

		if err := h.Prefs.SetNotificationPreferences(t.Context(), user, p); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: SetNotificationPreferences = %v, want ErrInvalid", name, err)
		}
	}

	want(t, get(t, h, user), changed())
}

func testIsolation(t *testing.T, h Harness) {
	ada, bob := h.NewUser(t), h.NewUser(t)
	set(t, h, bob, domain.DefaultNotificationPreferences())
	set(t, h, ada, changed())

	want(t, get(t, h, bob), domain.DefaultNotificationPreferences())
	want(t, get(t, h, ada), changed())
}
