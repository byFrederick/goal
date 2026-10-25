// Package openaiembed implements ports.Embedder with an OpenAI-compatible embeddings API: POST
// <URL>/embeddings with the model, the texts and dimensions 1024, a bearer key when one is set.
// Workers AI (https://api.cloudflare.com/client/v4/accounts/<account>/ai/v1), Ollama
// (http://127.0.0.1:11434/v1) and OpenAI itself serve it, so the same adapter serves every
// deployment. The key travels only in the Authorization header of requests to the configured URL;
// redirects are not followed and no error carries it or the texts.
package openaiembed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Embedder = (*Client)(nil)

// Config is where the embeddings come from.
type Config struct {
	// URL is the API root the path /embeddings is under (GOAL_EMBED_URL): https, or http on a
	// loopback address or a name without dots (a container on the same network, like ollama).
	URL string
	// Model is the embedding model (GOAL_EMBED_MODEL).
	Model string
	// Key is the bearer key (GOAL_EMBED_KEY); "" sends none.
	Key string
	// Timeout bounds one request. Default: 60s.
	Timeout time.Duration
	// Transport carries the requests; nil is http.DefaultTransport. goald's dials public
	// addresses only unless GOAL_ALLOW_PRIVATE_UPSTREAMS (egress.PublicDialer).
	Transport http.RoundTripper
}

// maxBodyBytes bounds a response: 64 embeddings of 1024 floats as JSON are about 1.5 MiB.
const maxBodyBytes = 16 << 20

// Client calls the embeddings API. It is safe for concurrent use and keeps no state.
type Client struct {
	endpoint, model, key string
	http                 *http.Client
}

// New returns a client for cfg, refusing a URL that is not http(s), names credentials or would
// send the key in the clear to another machine, and an empty model.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(strings.TrimSuffix(cfg.URL, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("openaiembed: %q is not an http(s) URL without credentials, query or fragment", cfg.URL)
	}

	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && localHost(u.Hostname()):
	default:
		return nil, fmt.Errorf("openaiembed: %q must be https (http only on a loopback address or a local name)", cfg.URL)
	}

	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("openaiembed: a model is required")
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}

	return &Client{
		endpoint: u.String() + "/embeddings", model: cfg.Model, key: cfg.Key,
		http: &http.Client{
			Timeout: cfg.Timeout, Transport: cfg.Transport,
			// The key is never sent anywhere but the configured URL.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// localHost reports whether host is this machine or a name on a local network (no dots, as a
// compose service's), where plain http does not cross the internet.
func localHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || !strings.Contains(host, ".")
}

// Model implements ports.Embedder.
func (c *Client) Model() string { return c.model }

type request struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions"`
}

type response struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed implements ports.Embedder: one request for every text, embeddings in their order.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(request{Model: c.model, Input: texts, Dimensions: domain.EmbeddingDimensions})
	if err != nil {
		return nil, fmt.Errorf("openaiembed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openaiembed: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openaiembed: %s: %w", c.model, c.scrub(err))
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("openaiembed: %s: read: %w", c.model, c.scrub(err))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openaiembed: %s: %s: %s", c.model, resp.Status, c.snippet(raw))
	}

	return c.embeddings(raw, len(texts))
}

// embeddings decodes a response for n texts.
func (c *Client) embeddings(raw []byte, n int) ([][]float32, error) {
	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("openaiembed: %s: the response is not an embeddings list: %w", c.model, err)
	}

	if len(r.Data) != n {
		return nil, fmt.Errorf("openaiembed: %s: %d embeddings for %d texts", c.model, len(r.Data), n)
	}

	out := make([][]float32, n)

	for _, d := range r.Data {
		if d.Index < 0 || d.Index >= n || out[d.Index] != nil {
			return nil, fmt.Errorf("openaiembed: %s: embedding index %d is out of place", c.model, d.Index)
		}

		if len(d.Embedding) != domain.EmbeddingDimensions {
			return nil, fmt.Errorf("openaiembed: %s returns %d dimensions, not %d: choose a model of %d (or one that takes dimensions)",
				c.model, len(d.Embedding), domain.EmbeddingDimensions, domain.EmbeddingDimensions)
		}

		out[d.Index] = d.Embedding
	}

	return out, nil
}

// snippet is the start of an error response, without the key.
func (c *Client) snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		s = strings.ToValidUTF8(s[:300], "") + "…" // the cut may split a character
	}

	if c.key != "" {
		s = strings.ReplaceAll(s, c.key, "[key]")
	}

	return s
}

// scrub keeps the key out of a transport error.
func (c *Client) scrub(err error) error {
	if c.key == "" || !strings.Contains(err.Error(), c.key) {
		return err
	}

	return errors.New(strings.ReplaceAll(err.Error(), c.key, "[key]"))
}
