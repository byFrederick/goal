package api_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The routes the notification settings tests call, and the Slack secret they save.
//
//nolint:gosec // routes, names and a made-up token, not secrets
const (
	notifySettingsPath = "/v1/orgs/acme/settings/notifications"
	notifyTestPath     = notifySettingsPath + "/test"
	slackSecret        = "slack-bot"
	slackTokenValue    = "xoxb-acme-test-token-91d2"
)

// createSlackSecret creates slack-bot, open to all factories, holding slackTokenValue.
func (h *harness) createSlackSecret(t *testing.T) {
	t.Helper()

	decode[gen.Secret](t, h.request(t, http.MethodPost, secretsPath,
		`{"name":"`+slackSecret+`","value":"`+slackTokenValue+`","scope":{"all_factories":true}}`, nil), http.StatusCreated)
}

// Slack is off until a PUT names a secret; GET then answers it, and a PUT of null turns it off.
func TestAPI_NotificationSettingsRoundTrip(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createSlackSecret(t)

	if got := decode[gen.NotificationSettings](t, h.request(t, http.MethodGet, notifySettingsPath, "", nil), http.StatusOK); got.SlackSecret != nil {
		t.Fatalf("settings before any save = %+v; want slack_secret null", got)
	}

	put := decode[gen.NotificationSettings](t, h.request(t, http.MethodPut, notifySettingsPath, `{"slack_secret":"`+slackSecret+`"}`, nil), http.StatusOK)
	if valueOf(put.SlackSecret) != slackSecret {
		t.Errorf("PUT answered %+v; want slack-bot", put)
	}

	if got := decode[gen.NotificationSettings](t, h.request(t, http.MethodGet, notifySettingsPath, "", nil), http.StatusOK); valueOf(got.SlackSecret) != slackSecret {
		t.Errorf("GET after the PUT = %+v; want slack-bot", got)
	}

	decode[gen.NotificationSettings](t, h.request(t, http.MethodPut, notifySettingsPath, `{"slack_secret":null}`, nil), http.StatusOK)

	if got := decode[gen.NotificationSettings](t, h.request(t, http.MethodGet, notifySettingsPath, "", nil), http.StatusOK); got.SlackSecret != nil {
		t.Errorf("GET after turning Slack off = %+v; want null", got)
	}
}

// A name that is no secret is 404, and a secret open to some factories only is 400
// secret-not-allowed, since notifications open it without a factory; neither saves anything.
func TestAPI_NotificationSettingsRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createSecret(t) // acme-token, open to factory acme only

	refused(t, "an unknown secret", h.request(t, http.MethodPut, notifySettingsPath, `{"slack_secret":"no-such-secret"}`, nil),
		http.StatusNotFound, codeSecretNotFound)
	refused(t, "a scoped secret", h.request(t, http.MethodPut, notifySettingsPath, `{"slack_secret":"`+secretName+`"}`, nil),
		http.StatusBadRequest, "secret-not-allowed")

	if got, err := h.notifySettings.NotificationSettings(inOrg(t)); err != nil || got.SlackSecret != "" {
		t.Errorf("settings after the refusals = %+v, %v; want Slack off", got, err)
	}
}

// A member reads the settings but neither saves nor tests them.
func TestAPI_NotificationSettingsMemberMayOnlyRead(t *testing.T) {
	t.Parallel()

	h := newSignedIn(t)

	if _, err := h.secrets.Create(inOrg(t), domain.NewSecret{Name: slackSecret, Value: slackTokenValue, Scope: domain.SecretScope{All: true}},
		domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	decode[gen.NotificationSettings](t, h.as(t, viewerToken, http.MethodGet, notifySettingsPath, ""), http.StatusOK)
	refused(t, "a member's PUT", h.as(t, viewerToken, http.MethodPut, notifySettingsPath, `{"slack_secret":"`+slackSecret+`"}`),
		http.StatusForbidden, "forbidden")
	refused(t, "a member's test", h.as(t, viewerToken, http.MethodPost, notifyTestPath, `{"slack_secret":"`+slackSecret+`"}`),
		http.StatusForbidden, "forbidden")

	if n := len(h.slack.Checked()); n != 0 {
		t.Errorf("a member's test checked %d tokens; want none", n)
	}
}

// A working token answers ok with its workspace; the test opens the named secret, or without a
// body the saved one, hands its token to Slack only, and saves nothing.
func TestAPI_NotificationSettingsTestOK(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.createSlackSecret(t)

	got := decode[gen.NotificationTestResult](t, h.request(t, http.MethodPost, notifyTestPath, `{"slack_secret":"`+slackSecret+`"}`, nil), http.StatusOK)
	if !got.Ok || valueOf(got.Workspace) != "acme" || got.Error != nil {
		t.Errorf("test = %+v; want ok in workspace acme", got)
	}

	if checked := h.slack.Checked(); len(checked) != 1 || checked[0] != slackTokenValue {
		t.Errorf("checked %d tokens; want slack-bot's one", len(checked))
	}

	if ns, err := h.notifySettings.NotificationSettings(inOrg(t)); err != nil || ns.SlackSecret != "" {
		t.Errorf("settings after a test = %+v, %v; want nothing saved", ns, err)
	}

	decode[gen.NotificationSettings](t, h.request(t, http.MethodPut, notifySettingsPath, `{"slack_secret":"`+slackSecret+`"}`, nil), http.StatusOK)

	if got := decode[gen.NotificationTestResult](t, h.request(t, http.MethodPost, notifyTestPath, "", nil), http.StatusOK); !got.Ok {
		t.Errorf("test of the saved setting = %+v; want ok", got)
	}

	if n := len(h.slack.Checked()); n != 2 {
		t.Errorf("checked %d tokens; want the saved secret's too", n)
	}
}

// A token Slack refuses or cannot check is a 200 answer with the fixed code, never the token;
// with no secret given and none saved, the answer is slack-not-set; a scoped or unknown secret
// is the secret rules' problem.
func TestAPI_NotificationSettingsTestNotOK(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	h := newHarnessLogged(t, nil, &logs)
	h.createSlackSecret(t)
	h.createSecret(t)

	if got := decode[gen.NotificationTestResult](t, h.request(t, http.MethodPost, notifyTestPath, `{}`, nil), http.StatusOK); got.Ok ||
		valueOf(got.Error) != "slack-not-set" {
		t.Errorf("test with nothing saved = %+v; want slack-not-set", got)
	}

	if n := len(h.slack.Checked()); n != 0 {
		t.Errorf("checked %d tokens with no secret; want none", n)
	}

	for _, code := range []string{"slack-token-invalid", "slack-token-unavailable"} {
		h.slack.CheckWith = func(string) (string, error) { return "", &ports.DMError{Code: code, Permanent: code == "slack-token-invalid"} }

		r := h.request(t, http.MethodPost, notifyTestPath, `{"slack_secret":"`+slackSecret+`"}`, nil)
		if got := decode[gen.NotificationTestResult](t, r, http.StatusOK); got.Ok || valueOf(got.Error) != code || got.Workspace != nil {
			t.Errorf("test = %+v; want not ok with %s", got, code)
		}

		if bytes.Contains(r.body, []byte(slackTokenValue)) {
			t.Errorf("the answer holds the token: %s", r.body)
		}
	}

	refused(t, "a scoped secret", h.request(t, http.MethodPost, notifyTestPath, `{"slack_secret":"`+secretName+`"}`, nil),
		http.StatusBadRequest, "secret-not-allowed")
	refused(t, "an unknown secret", h.request(t, http.MethodPost, notifyTestPath, `{"slack_secret":"no-such-secret"}`, nil),
		http.StatusNotFound, codeSecretNotFound)

	audit := h.request(t, http.MethodGet, "/v1/orgs/acme/audit", "", nil)
	for what, text := range map[string]string{"log": logs.String(), "audit": string(audit.body)} {
		if strings.Contains(text, slackTokenValue) {
			t.Errorf("the %s holds the token", what)
		}
	}
}
