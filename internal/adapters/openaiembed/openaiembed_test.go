package openaiembed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/domain"
)

const testKey = "sk-test-embedding-key"

// embeddingServer answers like an OpenAI-compatible API: dims dimensions per text, the data in
// reverse order (indexes say where each goes), after checking the request.
func embeddingServer(t *testing.T, dims int) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req request
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer "+testKey || json.NewDecoder(r.Body).Decode(&req) != nil ||
			req.Model != "acme-embed" || req.Dimensions != domain.EmbeddingDimensions {
			http.Error(w, "bad request "+r.URL.Path+" "+r.Header.Get("Authorization"), http.StatusBadRequest)
			return
		}

		type datum struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}

		var out struct {
			Data []datum `json:"data"`
		}

		for i, text := range slices.Backward(req.Input) {
			v := make([]float32, dims)
			v[0] = float32(len(text))
			out.Data = append(out.Data, datum{Index: i, Embedding: v})
		}

		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func client(t *testing.T, url string) *Client {
	t.Helper()

	c, err := New(Config{URL: url, Model: "acme-embed", Key: testKey})
	if err != nil {
		t.Fatal(err)
	}

	return c
}

func TestEmbed_OneRequestEmbeddingsInOrder(t *testing.T) {
	t.Parallel()

	c := client(t, embeddingServer(t, domain.EmbeddingDimensions).URL+"/v1/")

	got, err := c.Embed(t.Context(), []string{"a", "bbb", "cc"})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 3 || got[0][0] != 1 || got[1][0] != 3 || got[2][0] != 2 || len(got[1]) != domain.EmbeddingDimensions {
		t.Errorf("Embed = %v…, want the texts' embeddings in their order", [][]float32{got[0][:1], got[1][:1], got[2][:1]})
	}

	if c.Model() != "acme-embed" {
		t.Errorf("Model = %q", c.Model())
	}
}

func TestEmbed_RefusesOtherDimensions(t *testing.T) {
	t.Parallel()

	_, err := client(t, embeddingServer(t, 768).URL+"/v1").Embed(t.Context(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "768 dimensions") {
		t.Errorf("Embed of a 768-dimension model = %v, want it refused", err)
	}
}

func TestEmbed_ErrorsCarryTheStatusNeverTheKey(t *testing.T) {
	t.Parallel()

	// The server echoes the authorization header in its error, as some do.
	_, err := client(t, embeddingServer(t, domain.EmbeddingDimensions).URL+"/elsewhere").Embed(t.Context(), []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "400") || strings.Contains(err.Error(), testKey) {
		t.Errorf("Embed = %v, want the status without the key", err)
	}
}

// Review 4 Go L13: an error reply cut at its 300th byte stays valid UTF-8 (the source's sync
// error is stored as text).
func TestEmbed_ALongErrorIsCutOnACharacter(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "x"+strings.Repeat("é", 200), http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := client(t, srv.URL).Embed(t.Context(), []string{"a"})
	if err == nil || !utf8.ValidString(err.Error()) || !strings.Contains(err.Error(), "500") {
		t.Errorf("Embed = %q, want the status in valid UTF-8", err)
	}
}

func TestEmbed_FollowsNoRedirect(t *testing.T) {
	t.Parallel()

	reached := false
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	t.Cleanup(elsewhere.Close)

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/v1/embeddings", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)

	if _, err := client(t, redirect.URL+"/v1").Embed(t.Context(), []string{"a"}); err == nil || reached {
		t.Errorf("Embed through a redirect = %v (reached %v), want an error and the key kept home", err, reached)
	}
}

func TestNew_RefusesUnsafeSettings(t *testing.T) {
	t.Parallel()

	for _, cfg := range []Config{
		{URL: "http://embeddings.example.com/v1", Model: "m"},
		{URL: "https://user@embeddings.example.com/v1", Model: "m"},
		{URL: "ftp://embeddings.example.com", Model: "m"},
		{URL: "embeddings.example.com", Model: "m"},
		{URL: "https://embeddings.example.com/v1", Model: " "},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("New(%+v) succeeded", cfg)
		}
	}

	for _, url := range []string{"https://api.cloudflare.com/client/v4/accounts/acme/ai/v1", "http://127.0.0.1:11434/v1", "http://ollama:11434/v1"} {
		if _, err := New(Config{URL: url, Model: "m"}); err != nil {
			t.Errorf("New(%s) = %v", url, err)
		}
	}
}
