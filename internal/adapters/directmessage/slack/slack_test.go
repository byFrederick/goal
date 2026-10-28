package slack_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/byFrederick/goal/internal/adapters/directmessage/slack"
	"github.com/byFrederick/goal/internal/core/ports"
)

const (
	token = "xoxb-test-token"
	email = "ann@example.com"
)

// call is one request the fake Slack got.
type call struct {
	method string
	auth   string
	form   map[string]string
}

// fakeSlack answers each Web API method with answer(method), or a good reply by default, and
// records every call.
type fakeSlack struct {
	mu     sync.Mutex
	calls  []call
	answer func(method string) (status int, body string)
}

func (f *fakeSlack) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	method := strings.TrimPrefix(r.URL.Path, "/api/")
	form := map[string]string{}

	for k := range r.PostForm {
		form[k] = r.PostForm.Get(k)
	}

	f.mu.Lock()
	f.calls = append(f.calls, call{method: r.Method + " " + method, auth: r.Header.Get("Authorization"), form: form})
	f.mu.Unlock()

	status, body := http.StatusOK, good(method)
	if f.answer != nil {
		status, body = f.answer(method)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

func (f *fakeSlack) Calls() []call {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]call(nil), f.calls...)
}

func good(method string) string {
	switch method {
	case "users.lookupByEmail":
		return `{"ok":true,"user":{"id":"U123"}}`
	case "conversations.open":
		return `{"ok":true,"channel":{"id":"D456"}}`
	default:
		return `{"ok":true}`
	}
}

func newSlack(t *testing.T, f *fakeSlack) *slack.Messenger {
	t.Helper()

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	return slack.New(slack.Config{BaseURL: srv.URL + "/api"})
}

// A message finds its person by email, opens a direct message with them and posts it there,
// each call with the bot token.
func TestSlack_SendsADirectMessage(t *testing.T) {
	t.Parallel()

	f := &fakeSlack{}

	if err := newSlack(t, f).Send(t.Context(), token, ports.DirectMessage{To: email, Text: "An agent needs your OK."}); err != nil {
		t.Fatal(err)
	}

	calls := f.Calls()

	want := []struct {
		method, key, value string
	}{
		{"POST users.lookupByEmail", "email", email},
		{"POST conversations.open", "users", "U123"},
		{"POST chat.postMessage", "channel", "D456"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %+v; want %d", calls, len(want))
	}

	for i, w := range want {
		c := calls[i]
		if c.method != w.method || c.form[w.key] != w.value || c.auth != "Bearer "+token {
			t.Errorf("call %d = %+v; want %s with %s=%s and the bearer token", i, c, w.method, w.key, w.value)
		}
	}

	if got := calls[2].form["text"]; got != "An agent needs your OK." {
		t.Errorf("posted text %q", got)
	}
}

// A person Slack does not find is ErrDMUserNotFound with a fixed code, and nothing is posted.
func TestSlack_UserNotFound(t *testing.T) {
	t.Parallel()

	f := &fakeSlack{answer: func(method string) (int, string) {
		if method == "users.lookupByEmail" {
			return http.StatusOK, `{"ok":false,"error":"users_not_found"}`
		}

		return http.StatusOK, good(method)
	}}

	err := newSlack(t, f).Send(t.Context(), token, ports.DirectMessage{To: email, Text: "hi"})
	if !errors.Is(err, ports.ErrDMUserNotFound) || !errors.Is(err, ports.ErrDMPermanent) {
		t.Fatalf("err = %v; want ErrDMUserNotFound, permanent", err)
	}

	if de, ok := errors.AsType[*ports.DMError](err); !ok || de.Code != "slack-user-not-found" {
		t.Fatalf("err = %v; want code slack-user-not-found", err)
	}

	if n := len(f.Calls()); n != 1 {
		t.Fatalf("made %d calls; want only the lookup", n)
	}
}

// A redirect is never followed, so the token never goes to another host.
func TestSlack_RefusesRedirects(t *testing.T) {
	t.Parallel()

	var elsewhere atomic.Int32

	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elsewhere.Add(1)
		fmt.Fprint(w, `{"ok":true,"user":{"id":"U123"}}`)
	}))
	t.Cleanup(other.Close)

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)

	err := slack.New(slack.Config{BaseURL: redirect.URL + "/api"}).Send(t.Context(), token, ports.DirectMessage{To: email, Text: "hi"})
	if err == nil {
		t.Fatal("a redirected send succeeded")
	}

	if de, ok := errors.AsType[*ports.DMError](err); !ok || de.Code != "slack-redirect" {
		t.Fatalf("err = %v; want code slack-redirect", err)
	}

	if n := elsewhere.Load(); n != 0 {
		t.Fatalf("the redirect's target got %d requests; want none", n)
	}
}

// Whatever Slack answers, the error holds no token, no email and nothing of the reply, and says
// whether a retry may succeed.
func TestSlack_ErrorsHideTheToken(t *testing.T) {
	t.Parallel()

	echo := `"` + token + ` ` + email + ` reply-secret"`

	for _, c := range []struct {
		name      string
		status    int
		body      string
		code      string
		permanent bool
	}{
		{"invalid auth", http.StatusOK, `{"ok":false,"error":"invalid_auth","detail":` + echo + `}`, "slack-invalid-auth", true},
		{"other refusal", http.StatusOK, `{"ok":false,"error":"missing_scope","needed":` + echo + `}`, "slack-missing-scope", true},
		{"odd error", http.StatusOK, `{"ok":false,"error":` + echo + `}`, "slack-error", true},
		{"rate limited", http.StatusTooManyRequests, echo, "slack-rate-limited", false},
		{"server error", http.StatusBadGateway, echo, "slack-http-502", false},
		{"slack's own failure", http.StatusOK, `{"ok":false,"error":"internal_error","x":` + echo + `}`, "slack-internal-error", false},
		{"not json", http.StatusOK, echo, "slack-bad-reply", false},
		{"refused", http.StatusForbidden, echo, "slack-http-403", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			f := &fakeSlack{answer: func(string) (int, string) { return c.status, c.body }}

			err := newSlack(t, f).Send(t.Context(), token, ports.DirectMessage{To: email, Text: "hi"})
			if err == nil {
				t.Fatal("send succeeded")
			}

			for _, private := range []string{token, email, "reply-secret", "example.com"} {
				if strings.Contains(err.Error(), private) {
					t.Errorf("err %q holds %q", err, private)
				}
			}

			de, ok := errors.AsType[*ports.DMError](err)
			if !ok || de.Code != c.code || errors.Is(err, ports.ErrDMPermanent) != c.permanent {
				t.Fatalf("err = %v (%+v); want code %s, permanent %v", err, de, c.code, c.permanent)
			}
		})
	}
}

// A Slack that cannot be reached is a temporary error that names neither the token nor the
// person.
func TestSlack_UnreachableIsTemporary(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()

	err := slack.New(slack.Config{BaseURL: srv.URL + "/api"}).Send(t.Context(), token, ports.DirectMessage{To: email, Text: "hi"})
	if de, ok := errors.AsType[*ports.DMError](err); !ok || de.Code != "slack-unreachable" || errors.Is(err, ports.ErrDMPermanent) {
		t.Fatalf("err = %v; want a temporary slack-unreachable", err)
	}

	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), email) {
		t.Fatalf("err %q holds the token or the email", err)
	}
}
