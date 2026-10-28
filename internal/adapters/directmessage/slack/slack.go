// Package slack implements ports.DirectMessenger over the Slack Web API (ADR 0023, decision 5): it
// finds the person by email with users.lookupByEmail, opens a direct message with
// conversations.open and posts the text there with chat.postMessage. Every call is a form POST
// with the bot token as a bearer token, so neither the token nor the email is ever in a URL, and
// the client follows no redirect, so the token goes to the configured host only. No error carries
// the token, the email or Slack's reply: each is a ports.DMError with a fixed code.
package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.DirectMessenger = (*Messenger)(nil)

const (
	// DefaultBaseURL is Slack's Web API.
	DefaultBaseURL = "https://slack.com/api"
	// defaultTimeout bounds one call; a send makes three.
	defaultTimeout = 10 * time.Second
	// maxReply is the most of a reply that is read.
	maxReply = 1 << 20
)

// Config is where the Web API is and how long a call may take.
type Config struct {
	// BaseURL is the Web API's root; "" is DefaultBaseURL.
	BaseURL string
	// Timeout bounds each call; 0 is 10 seconds.
	Timeout time.Duration
	// Transport carries the calls; nil is http.DefaultTransport.
	Transport http.RoundTripper
}

// Messenger sends direct messages with the token each send is given.
type Messenger struct {
	base   string
	client *http.Client
}

// New returns the messenger of c.
func New(c Config) *Messenger {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return &Messenger{base: base, client: &http.Client{
		Transport: c.Transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// Send implements ports.DirectMessenger.
func (m *Messenger) Send(ctx context.Context, token string, msg ports.DirectMessage) error {
	var user struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}

	if err := m.call(ctx, token, "users.lookupByEmail", url.Values{"email": {msg.To}}, &user); err != nil {
		return err
	}

	var open struct {
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}

	if err := m.call(ctx, token, "conversations.open", url.Values{"users": {user.User.ID}}, &open); err != nil {
		return err
	}

	return m.call(ctx, token, "chat.postMessage", url.Values{"channel": {open.Channel.ID}, "text": {msg.Text}}, nil)
}

// call posts form to method and decodes a good reply into out, when out is not nil.
func (m *Messenger) call(ctx context.Context, token, method string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.base+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return &ports.DMError{Code: "slack-bad-request", Permanent: true}
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := m.client.Do(req)
	if err != nil {
		// The transport's error repeats nothing private, but holds no code either.
		return &ports.DMError{Code: "slack-unreachable"}
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return &ports.DMError{Code: "slack-unreachable"}
	}

	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return &ports.DMError{Code: "slack-redirect", Permanent: true}
	case resp.StatusCode == http.StatusTooManyRequests:
		return &ports.DMError{Code: "slack-rate-limited"}
	case resp.StatusCode != http.StatusOK:
		return &ports.DMError{Code: "slack-http-" + strconv.Itoa(resp.StatusCode), Permanent: resp.StatusCode < 500}
	}

	var reply struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}

	if err := json.Unmarshal(body, &reply); err != nil {
		return &ports.DMError{Code: "slack-bad-reply"}
	}

	if !reply.OK {
		return refusal(reply.Error)
	}

	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return &ports.DMError{Code: "slack-bad-reply"}
		}
	}

	return nil
}

// errorName is the shape of Slack's own error names (invalid_auth); anything else is not
// repeated.
var errorName = regexp.MustCompile(`^[a-z][a-z_]{0,39}$`)

// temporary are the error names of a Slack that may answer on a retry.
var temporary = map[string]bool{
	"ratelimited": true, "internal_error": true, "fatal_error": true, "service_unavailable": true,
	"request_timeout": true,
}

// refusal is the DMError of an ok:false reply named name.
func refusal(name string) error {
	if name == "users_not_found" {
		return &ports.DMError{Code: "slack-user-not-found", NotFound: true}
	}

	if !errorName.MatchString(name) {
		return &ports.DMError{Code: "slack-error", Permanent: true}
	}

	return &ports.DMError{Code: "slack-" + strings.ReplaceAll(name, "_", "-"), Permanent: !temporary[name]}
}
