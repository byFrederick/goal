package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
)

const notificationsPath = "/v1/me/notifications"

// defaultNotifications are ADR 0023's defaults, the boxes account.html checks.
var defaultNotifications = gen.NotificationPreferences{
	Events: gen.NotificationEvents{
		Approval: gen.NotificationChannels{InGoal: true, Slack: true},
		Question: gen.NotificationChannels{InGoal: true, Slack: true},
		Andon:    gen.NotificationChannels{InGoal: true, Email: true, Slack: true},
		Budget:   gen.NotificationChannels{InGoal: true, Email: true},
		Digest:   gen.NotificationChannels{Email: true},
	},
	TimeZone: "UTC",
}

// ownNotifications is a person's own choice: the andon by Slack only, everything else off, the
// digest by email in Berlin at a notification email of their own.
const ownNotifications = `{"email":"ada@example.com","time_zone":"Europe/Berlin","events":{
	"approval":{"in_goal":false,"email":false,"slack":false},
	"question":{"in_goal":false,"email":false,"slack":false},
	"andon":{"in_goal":false,"email":false,"slack":true},
	"budget":{"in_goal":false,"email":false,"slack":false},
	"digest":{"in_goal":false,"email":true,"slack":false}}}`

// notificationsWith is ownNotifications with old replaced by replacement.
func notificationsWith(old, replacement string) string {
	return strings.Replace(ownNotifications, old, replacement, 1)
}

func TestNotifications_DefaultsWhenNothingSaved(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	if got := decode[gen.NotificationPreferences](t, h.as(t, viewerToken, http.MethodGet, notificationsPath, ""), http.StatusOK); got != defaultNotifications {
		t.Errorf("preferences = %+v, want the defaults %+v", got, defaultNotifications)
	}
}

// A save replaces every field and answers what a read then answers; it is the caller's own, so
// another person still reads the defaults.
func TestNotifications_PutThenGetRoundTrips(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	saved := decode[gen.NotificationPreferences](t, h.as(t, viewerToken, http.MethodPut, notificationsPath, ownNotifications), http.StatusOK)

	want := gen.NotificationPreferences{
		Email: "ada@example.com", TimeZone: "Europe/Berlin",
		Events: gen.NotificationEvents{Andon: gen.NotificationChannels{Slack: true}, Digest: gen.NotificationChannels{Email: true}},
	}
	if saved != want {
		t.Errorf("saved = %+v, want %+v", saved, want)
	}

	if got := decode[gen.NotificationPreferences](t, h.as(t, viewerToken, http.MethodGet, notificationsPath, ""), http.StatusOK); got != want {
		t.Errorf("read back = %+v, want %+v", got, want)
	}

	if got := decode[gen.NotificationPreferences](t, h.as(t, ownerToken, http.MethodGet, notificationsPath, ""), http.StatusOK); got != defaultNotifications {
		t.Errorf("another person reads %+v, want the defaults", got)
	}
}

// Each refused save answers its code and changes nothing.
func TestNotifications_PutInvalidRefused(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	for _, c := range []struct {
		what, body, code string
	}{
		{"a bad email", notificationsWith(`"ada@example.com"`, `"Ada <ada@example.com>"`), "invalid-notification-email"},
		{"not an email", notificationsWith(`"ada@example.com"`, `"ada"`), "invalid-notification-email"},
		{"a bad time zone", notificationsWith(`"Europe/Berlin"`, `"Mars/Olympus"`), "invalid-time-zone"},
		{"the local time zone", notificationsWith(`"Europe/Berlin"`, `"Local"`), "invalid-time-zone"},
		{"the digest in Goal", notificationsWith(`"digest":{"in_goal":false`, `"digest":{"in_goal":true`), "invalid-notification-preferences"},
	} {
		wantStatus(t, c.what, h.as(t, viewerToken, http.MethodPut, notificationsPath, c.body), http.StatusBadRequest, c.code)
	}

	if got := decode[gen.NotificationPreferences](t, h.as(t, viewerToken, http.MethodGet, notificationsPath, ""), http.StatusOK); got != defaultNotifications {
		t.Errorf("after the refusals = %+v, want the defaults unchanged", got)
	}
}

// Only a signed-in person has preferences. An API key and a personal token never hold
// notifications:manage, so authorization refuses them; GOAL_AUTH=none's local user passes it but
// is no person.
func TestNotifications_OnlyAPersonHasThem(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	key, _ := h.create(t, `{"name":"notify","scopes":["org:read"],"role_ceiling":"viewer","expires_in_days":30}`)
	token, _ := h.createToken(t, freshToken, `{"name":"laptop","scopes":["org:read"]}`)

	for who, header := range map[string]http.Header{"API key": bearer(key.Secret), "personal token": bearer(token.Secret)} {
		wantStatus(t, who+" reads", h.request(t, http.MethodGet, notificationsPath, "", header), http.StatusForbidden, codeRefused)
		wantStatus(t, who+" saves", h.request(t, http.MethodPut, notificationsPath, ownNotifications, header), http.StatusForbidden, codeRefused)
	}

	none := newHarness(t)

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		code, p := problemOfCall(t, none, method, notificationsPath, ownNotifications)
		wantProblem(t, method+" without a sign-in", code, p, http.StatusForbidden, codeNoPerson)
	}
}
