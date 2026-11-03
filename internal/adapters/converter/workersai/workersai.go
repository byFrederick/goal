// Package workersai implements ports.Converter with Workers AI's toMarkdown: POST <URL>/tomarkdown
// with the file as a multipart part named files, which answers its Markdown. It converts PDF and
// Office files (ADR 0038 decision 2); goald's built-in converter handles text, Markdown, HTML, JSON
// and YAML before it. The key travels only in the Authorization header of requests to the
// configured URL; redirects are not followed and no error carries it or a file's content.
package workersai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Converter = (*Client)(nil)

// Config is where the conversions come from.
type Config struct {
	// URL is the API root the path /tomarkdown is under (GOAL_WORKERS_AI_URL, such as
	// https://api.cloudflare.com/client/v4/accounts/<account>/ai): https, or http on a loopback
	// address or a name without dots.
	URL string
	// Key is the bearer key (GOAL_WORKERS_AI_KEY); "" sends none.
	Key string
	// Timeout bounds one request. Default: 120s.
	Timeout time.Duration
	// Transport carries the requests; nil is http.DefaultTransport. goald's dials public
	// addresses only unless GOAL_ALLOW_PRIVATE_UPSTREAMS (egress.PublicDialer).
	Transport http.RoundTripper
}

// maxBodyBytes bounds a response: a converted file is at most a few MiB of Markdown.
const maxBodyBytes = 32 << 20

// Client calls toMarkdown. It is safe for concurrent use and keeps no state.
type Client struct {
	endpoint, key string
	http          *http.Client
}

// New returns a client for cfg, refusing a URL that is not http(s), names credentials or would
// send the key in the clear to another machine.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(strings.TrimSuffix(cfg.URL, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("workersai: %q is not an http(s) URL without credentials, query or fragment", cfg.URL)
	}

	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1" || !strings.Contains(u.Hostname(), ".")):
	default:
		return nil, fmt.Errorf("workersai: %q would send its key in the clear: use https", cfg.URL)
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	return &Client{
		endpoint: u.String() + "/tomarkdown",
		key:      cfg.Key,
		http: &http.Client{
			Timeout:       timeout,
			Transport:     cfg.Transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// supported are the media types toMarkdown converts besides the ones goald converts itself.
var supported = map[string]bool{
	"application/pdf":    true,
	"application/msword": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
	"application/vnd.ms-excel":                       true,
	"application/vnd.ms-powerpoint":                  true,
	"application/vnd.oasis.opendocument.text":        true,
	"application/vnd.oasis.opendocument.spreadsheet": true,
	"text/csv": true,
}

type answer struct {
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Result []struct {
		Format string `json:"format"`
		Data   string `json:"data"`
		Error  string `json:"error"`
	} `json:"result"`
}

// Convert implements ports.Converter: body, a file named name of mediaType, as Markdown.
// domain.ErrUnsupportedDocument for a media type toMarkdown does not convert.
func (c *Client) Convert(ctx context.Context, name, mediaType string, body []byte) ([]byte, error) {
	mediaType, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(mediaType)), ";")
	if !supported[mediaType] {
		return nil, fmt.Errorf("workersai: %q is not a type Workers AI converts: %w", mediaType, domain.ErrUnsupportedDocument)
	}

	var buf bytes.Buffer

	w := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename=%q`, name))
	hdr.Set("Content-Type", mediaType)

	part, err := w.CreatePart(hdr)
	if err != nil {
		return nil, fmt.Errorf("workersai: %w", err)
	}

	if _, err := part.Write(body); err != nil {
		return nil, fmt.Errorf("workersai: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("workersai: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, &buf)
	if err != nil {
		return nil, fmt.Errorf("workersai: %w", err)
	}

	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// The URL error names the endpoint, never the key or the file.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}

		return nil, fmt.Errorf("workersai: convert %s: %w", name, err)
	}

	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("workersai: read the answer: %w", err)
	}

	if len(raw) > maxBodyBytes {
		return nil, fmt.Errorf("workersai: the answer for %s is over %d bytes", name, maxBodyBytes)
	}

	var out answer
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("workersai: status %d with an answer that is not JSON", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK || !out.Success {
		msg := "no reason given"
		if len(out.Errors) > 0 && out.Errors[0].Message != "" {
			msg = out.Errors[0].Message
		}

		return nil, fmt.Errorf("workersai: status %d: %s", resp.StatusCode, msg)
	}

	if len(out.Result) != 1 {
		return nil, fmt.Errorf("workersai: %d results for one file", len(out.Result))
	}

	if r := out.Result[0]; r.Error != "" || r.Format == "error" {
		return nil, fmt.Errorf("workersai: %s could not be converted: %s", name, r.Error)
	}

	return []byte(out.Result[0].Data), nil
}
