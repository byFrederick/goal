package identitytest

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// The platform cases' people.
const (
	adaName, adaEmail = "Ada", "ada@example.com"
	bobEmail, cyEmail = "bob@example.com", "cy@example.com"
	adminAdded        = "platform_admin.added"
	adminRemoved      = "platform_admin.removed"
)

// RunPlatform runs the cases of ports.Platform against a fresh platform and its seed from setup:
// the platform admins (the last one stays), the security log of their changes and the clean-up.
func RunPlatform(t *testing.T, setup func(t *testing.T) (ports.Platform, Seed)) {
	t.Helper()

	for name, run := range map[string]func(t *testing.T, p ports.Platform, s Seed){
		"AdminsEarliestFirst":               testAdminsEarliestFirst,
		"AddByEmailOnce":                    testAddByEmail,
		"TheLastAdminStays":                 testLastAdminStays,
		"EventsNewestFirstByPage":           testEventsByPage,
		"CleanUpForgetsLongExpiredSessions": testCleanUp,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p, s := setup(t)
			run(t, p, s)
		})
	}
}

// adminIDs lists the platform admins' ids, in order.
func adminIDs(t *testing.T, p ports.Platform) []domain.UserID {
	t.Helper()

	admins, err := p.PlatformAdmins(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	out := make([]domain.UserID, 0, len(admins))
	for _, a := range admins {
		out = append(out, a.User.ID)
	}

	return out
}

// event is an event the security log should hold: its kind, whom it is about and, when set, by
// whom.
type event struct {
	kind string
	user domain.UserID
	by   string
}

// checkEvents checks a page of the security log is want, newest first.
func checkEvents(t *testing.T, p ports.Platform, q ports.AuthEventQuery, want ...event) []domain.AuthEvent {
	t.Helper()

	got, err := p.AuthEvents(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}

	match := func(e domain.AuthEvent, w event) bool {
		return e.Kind == w.kind && e.User == w.user && (w.by == "" || e.Data["by"] == w.by) && !e.At.IsZero()
	}

	if !slices.EqualFunc(got, want, match) {
		t.Errorf("security log %+v = %+v; want %+v", q, got, want)
	}

	return got
}

// actingAda is the cases' platform admin acting.
func actingAda(id domain.UserID) domain.Actor {
	return domain.Actor{Kind: domain.ActorUser, ID: string(id), Name: adaName}
}

func testAdminsEarliestFirst(t *testing.T, p ports.Platform, s Seed) {
	t.Helper()

	first := s.User(t, adaName, adaEmail, true)
	s.User(t, "Bob", bobEmail, false)
	cy := s.User(t, "Cy", cyEmail, true)

	admins, err := p.PlatformAdmins(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(admins) != 2 || admins[0].User != (domain.User{ID: first, Name: adaName, Email: adaEmail}) || admins[1].User.ID != cy {
		t.Errorf("PlatformAdmins = %+v; want Ada, then Cy", admins)
	}
}

func testAddByEmail(t *testing.T, p ports.Platform, s Seed) {
	t.Helper()

	by := actingAda(s.User(t, adaName, adaEmail, true))
	bob := s.User(t, "Bob", "Bob@Example.com", false)

	a, err := p.AddPlatformAdmin(t.Context(), by, "  bob@EXAMPLE.com ")
	if err != nil {
		t.Fatal(err)
	}

	if a.User.ID != bob || a.User.Name != "Bob" || a.AddedBy.Ref() != by.Ref() || a.AddedAt.IsZero() {
		t.Errorf("AddPlatformAdmin = %+v; want Bob, added by Ada", a)
	}

	if got := adminIDs(t, p); !slices.Equal(got, []domain.UserID{domain.UserID(by.ID), bob}) {
		t.Errorf("admins = %v; want Ada, then Bob", got)
	}

	if _, err := p.AddPlatformAdmin(t.Context(), by, bobEmail); err != nil {
		t.Errorf("adding Bob again = %v; want nothing changed", err)
	}

	checkEvents(t, p, ports.AuthEventQuery{}, event{adminAdded, bob, by.Ref()})

	if _, err := p.AddPlatformAdmin(t.Context(), by, "nobody@example.com"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("adding an unknown email = %v; want ErrNotFound", err)
	}
}

func testLastAdminStays(t *testing.T, p ports.Platform, s Seed) {
	t.Helper()

	by := actingAda(s.User(t, adaName, adaEmail, true))
	bob := s.User(t, "Bob", bobEmail, true)
	cy := s.User(t, "Cy", cyEmail, false)

	if err := p.RemovePlatformAdmin(t.Context(), by, cy); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("removing a non-admin = %v; want ErrNotFound", err)
	}

	if err := p.RemovePlatformAdmin(t.Context(), by, bob); err != nil {
		t.Fatalf("removing Bob = %v", err)
	}

	if err := p.RemovePlatformAdmin(t.Context(), by, domain.UserID(by.ID)); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("removing the last admin = %v; want ErrConflict", err)
	}

	if got := adminIDs(t, p); !slices.Equal(got, []domain.UserID{domain.UserID(by.ID)}) {
		t.Errorf("admins = %v; want Ada alone", got)
	}

	// The refused removal leaves nothing in the log.
	checkEvents(t, p, ports.AuthEventQuery{}, event{adminRemoved, bob, by.Ref()})
}

func testEventsByPage(t *testing.T, p ports.Platform, s Seed) {
	t.Helper()

	by := actingAda(s.User(t, adaName, adaEmail, true))
	bob, cy := s.User(t, "Bob", bobEmail, false), s.User(t, "Cy", cyEmail, false)

	for _, email := range []string{bobEmail, cyEmail} {
		if _, err := p.AddPlatformAdmin(t.Context(), by, email); err != nil {
			t.Fatal(err)
		}
	}

	if err := p.RemovePlatformAdmin(t.Context(), by, bob); err != nil {
		t.Fatal(err)
	}

	first := checkEvents(t, p, ports.AuthEventQuery{Limit: 2}, event{adminRemoved, bob, ""}, event{adminAdded, cy, ""})
	if len(first) != 2 || first[0].ID <= first[1].ID {
		t.Fatalf("first page = %+v; want ids newest first", first)
	}

	checkEvents(t, p, ports.AuthEventQuery{Before: first[1].ID, Limit: 2}, event{adminAdded, bob, ""})
	checkEvents(t, p, ports.AuthEventQuery{Kind: adminAdded}, event{adminAdded, cy, ""}, event{adminAdded, bob, ""})
}

func testCleanUp(t *testing.T, p ports.Platform, s Seed) {
	t.Helper()

	user := s.User(t, adaName, adaEmail, false)
	s.Session(t, user, now().Add(-72*time.Hour), now().Add(-48*time.Hour))
	s.Session(t, user, now().Add(-2*time.Hour), now().Add(-time.Hour))
	s.Session(t, user, now(), now().Add(time.Hour))

	got, err := p.CleanUpAuth(t.Context(), now())
	if err != nil {
		t.Fatal(err)
	}

	if got.Skipped || got.Sessions != 1 {
		t.Errorf("CleanUpAuth = %+v; want the session expired two days ago deleted, not the one expired an hour ago", got)
	}

	if again, err := p.CleanUpAuth(t.Context(), now()); err != nil || again.Sessions != 0 {
		t.Errorf("CleanUpAuth again = %+v, %v; want nothing left to delete", again, err)
	}
}
