package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// refPrefixes start the words of a header value that are secret references: "Bearer
// env:GOAL_GITHUB_TOKEN" resolves its second word and keeps the first.
var refPrefixes = []string{"env:", "op://", "arn:"}

// minSecretLen is the shortest resolved value that is scrubbed from output; scrubbing shorter
// values would mangle ordinary text.
const minSecretLen = 6

const redacted = "[redacted]"

// resolve resolves every secret reference word in a header value through ports.Secrets and
// remembers the values so they can be scrubbed.
func (s *session) resolve(ctx context.Context, value string) (string, error) {
	words := strings.Split(value, " ")

	for i, w := range words {
		if !hasRefPrefix(w) {
			continue
		}

		v, err := s.g.secrets.Resolve(ctx, w)
		if err != nil {
			return "", err
		}

		if len(v) >= minSecretLen {
			s.secrets = append(s.secrets, v)
		}

		words[i] = v
	}

	return strings.Join(words, " "), nil
}

func hasRefPrefix(word string) bool {
	for _, p := range refPrefixes {
		if strings.HasPrefix(word, p) {
			return true
		}
	}

	return false
}

// redact removes resolved credentials from text bound for an agent, a log or an event.
func (s *session) redact(text string) string {
	for _, v := range s.secrets {
		text = strings.ReplaceAll(text, v, redacted)
	}

	return text
}

func (s *session) redactErr(err error) string {
	if err == nil {
		return ""
	}

	return s.redact(err.Error())
}

// scrubJSON removes resolved credentials from encoded JSON, in their JSON-escaped form.
func (s *session) scrubJSON(raw []byte) []byte {
	out := string(raw)

	for _, v := range s.secrets {
		enc, err := json.Marshal(v)
		if err != nil {
			continue
		}

		out = strings.ReplaceAll(out, string(enc[1:len(enc)-1]), redacted)
	}

	return []byte(out)
}

// checkURL allows https anywhere and plain http to loopback only, so credentials never cross
// a network in clear text.
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}

	switch {
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		return nil
	default:
		return fmt.Errorf("url scheme %q on host %q is not allowed", u.Scheme, u.Hostname())
	}
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// upstreamContext carries ctx's cancellation and a timeout, but none of its values: the SDK
// keeps the agent's negotiated protocol version in the request context, and each upstream
// session must negotiate its own.
func upstreamContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	up, cancel := context.WithTimeout(context.Background(), timeout)
	stop := context.AfterFunc(ctx, cancel)

	return up, func() {
		stop()
		cancel()
	}
}

// connect opens a short session with c's upstream server, credentials added per request.
func (s *session) connect(ctx context.Context, c *connection) (*mcp.ClientSession, error) {
	limit := s.g.cfg.MaxResponseBytes
	t := &mcp.StreamableClientTransport{
		Endpoint: c.url,
		HTTPClient: &http.Client{
			Transport: &upstreamTransport{base: s.g.transport, headers: c.headers, limit: limit},
			Timeout:   s.g.cfg.CallTimeout,
			// Credentials are added by the transport, so a redirect would carry them to
			// wherever it points. MCP endpoints do not redirect; the SDK reports the 3xx.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
		MaxEventSize:         int(limit),
	}

	cs, err := mcp.NewClient(implementation, nil).Connect(ctx, t, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", c.name, err)
	}

	return cs, nil
}

func (s *session) disconnect(ctx context.Context, cs *mcp.ClientSession) {
	if err := cs.Close(); err != nil {
		s.g.log.DebugContext(ctx, "gateway upstream close", "error", s.redactErr(err))
	}
}

// upstreamTransport adds a connection's resolved headers to each request and caps each
// response body.
type upstreamTransport struct {
	base    http.RoundTripper
	headers map[string]string
	limit   int64
}

func (t *upstreamTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for name, v := range t.headers {
		r.Header.Set(name, v)
	}

	resp, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}

	resp.Body = &limitedBody{body: resp.Body, left: t.limit}

	return resp, nil
}

var errResponseTooLarge = errors.New("upstream response exceeds the gateway's size limit")

// limitedBody fails a read past its limit, like http.MaxBytesReader does for requests.
type limitedBody struct {
	body io.ReadCloser
	left int64
	err  error
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}

	if len(p) == 0 {
		return 0, nil
	}

	// Read one byte past the limit to tell "exactly at the limit" from "over it".
	if int64(len(p))-1 > b.left {
		p = p[:b.left+1]
	}

	n, err := b.body.Read(p)
	if int64(n) <= b.left {
		b.left -= int64(n)
		b.err = err

		return n, err
	}

	n, b.left, b.err = int(b.left), 0, errResponseTooLarge

	return n, b.err
}

func (b *limitedBody) Close() error { return b.body.Close() }
