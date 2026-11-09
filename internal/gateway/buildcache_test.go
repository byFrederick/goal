package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// buildCacheYAML is a factory whose runs work on the public repository acme/api, with field
// (indented under runtime) added to its runtime.
func buildCacheYAML(field string) string {
	return `apiVersion: goal/v1
kind: Factory
metadata: { id: ` + string(factoryID) + `, name: acme-cache }
spec:
  runtime:
    modelConnection: claude
    repository: { url: "https://github.com/acme/api.git" }
` + field + `  agents:
    - { name: engineer, prompt: ./agents/engineer.md }
  board:
    stations:
      - { id: build, kind: agent, agent: engineer }
      - { id: done, kind: done }
`
}

// cacheRepo is the repository key of acme/api: the hex SHA-256 of its normalized key.
var cacheRepo = func() string {
	sum := sha256.Sum256([]byte("github.com/acme/api"))
	return hex.EncodeToString(sum[:])
}()

// cacheKey is a valid cache key made of digit.
func cacheKey(digit string) string { return strings.Repeat(digit, 64) }

// newBuildCacheHarness is a harness on yaml with a build cache index (h.store) and a bucket
// (h.objects), and the claims of a run of a queued item.
func newBuildCacheHarness(t *testing.T, yaml string, cfg Config) (*harness, domain.TaskClaims) {
	t.Helper()

	return newBuildCacheHarnessWith(t, yaml, cfg, nil)
}

// newBuildCacheHarnessWith is newBuildCacheHarness whose gateway reads the bucket through wrap.
func newBuildCacheHarnessWith(t *testing.T, yaml string, cfg Config, wrap func(*porttest.ObjectStore) Objects) (*harness, domain.TaskClaims) {
	t.Helper()

	h := newHarnessWith(t, cfg, harnessDeps{yaml: yaml, buildCaches: true, wrapObjects: wrap})
	it := h.queue(t, "build")

	c := h.claims()
	c.ItemID, c.Connections = it.ID, nil

	return h, c
}

// cacheServer serves the gateway's build cache routes.
func (h *harness) cacheServer(t *testing.T) string {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(h.gw.ServeBuildCache))
	t.Cleanup(ts.Close)

	return ts.URL
}

// cacheRequest sends method to url with token and body, and returns the status and body.
func cacheRequest(t *testing.T, method, url, token string, body []byte) (int, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	return resp.StatusCode, got
}

// cacheURLOf decodes a lookup's or a reserve's answer.
func cacheURLOf(t *testing.T, body []byte) buildCacheURL {
	t.Helper()

	var u buildCacheURL
	if err := json.Unmarshal(body, &u); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}

	return u
}

// cacheObject is where the organization's cache of acme/api under key lives.
func (h *harness) cacheObject(key string) string {
	return "orgs/" + string(h.org) + "/build-cache/" + cacheRepo + "/" + key + ".tar.zst"
}

// saveCache reserves key with c's token, uploads body through the gateway and commits it.
func (h *harness) saveCache(t *testing.T, srv string, c domain.TaskClaims, key string, body []byte) {
	t.Helper()

	tok := h.token(t, c)

	status, got := cacheRequest(t, http.MethodPost, srv+BuildCachePath+key, tok, nil)
	if status != http.StatusOK {
		t.Fatalf("reserve %s: status = %d, want 200 (%s)", key, status, got)
	}

	u := cacheURLOf(t, got)
	if want := (buildCacheURL{URL: BuildCachePath + key + "/archive", Method: http.MethodPut}); u != want {
		t.Fatalf("reserve %s = %+v, want %+v", key, u, want)
	}

	if status, got := cacheRequest(t, http.MethodPut, srv+u.URL, tok, body); status != http.StatusNoContent {
		t.Fatalf("upload %s: status = %d, want 204 (%s)", key, status, got)
	}

	done, _ := json.Marshal(map[string]int64{"size_bytes": int64(len(body))})
	if status, got := cacheRequest(t, http.MethodPost, srv+BuildCachePath+key+"/done", tok, done); status != http.StatusNoContent {
		t.Fatalf("done %s: status = %d, want 204 (%s)", key, status, got)
	}
}

// seedCache stores a ready cache of acme/api under key, with body as its object.
func (h *harness) seedCache(t *testing.T, key string, body []byte) {
	t.Helper()

	ctx := domain.WithOrg(t.Context(), h.org)
	obj := h.cacheObject(key)

	if ok, err := h.store.ReserveBuildCache(ctx, domain.NewBuildCache{RepositoryKey: cacheRepo, CacheKey: key, ObjectKey: obj, At: h.clock.Now()}); err != nil || !ok {
		t.Fatalf("reserve = %v, %v", ok, err)
	}

	if _, err := h.objects.Put(ctx, obj, body); err != nil {
		t.Fatal(err)
	}

	if err := h.store.CommitBuildCache(ctx, cacheRepo, key, obj, int64(len(body)), h.clock.Now()); err != nil {
		t.Fatal(err)
	}
}

// A run on a repository whose factory leaves runtime.buildCache unset or true gets the cache, with
// the legacy runtime.repository and with a default entry of runtime.repositories alike.
func TestBundle_BuildCacheEnabled(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, yaml string
		files      map[string]string
	}{
		{name: "unset", yaml: buildCacheYAML("")},
		{name: "true", yaml: buildCacheYAML("    buildCache: true\n")},
		{name: "default entry", yaml: entriesBundleYAML("write"), files: secretsBundleFiles},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarnessWith(t, Config{}, harnessDeps{yaml: tt.yaml, files: tt.files, buildCaches: true})
			it := h.queue(t, "build")

			c := h.claims()
			c.ItemID, c.Connections = it.ID, nil

			if b := h.fetchBundle(t, h.runServer(t), c); !b.BuildCache.Enabled {
				t.Errorf("build_cache = %+v, want enabled", b.BuildCache)
			}
		})
	}
}

// A run gets no cache when its factory turns runtime.buildCache off or it has no repository, and
// the bundle says so.
func TestBundle_BuildCacheOffWithoutRepositoryOrWhenTurnedOff(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, yaml string }{
		{name: "turned off", yaml: buildCacheYAML("    buildCache: false\n")},
		{name: "no repository", yaml: factoryYAML},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, c := newBuildCacheHarness(t, tt.yaml, Config{})

			status, _, body := request(t, http.MethodGet, h.runServer(t), h.token(t, c))
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", status, body)
			}

			if !strings.Contains(string(body), `"build_cache":{"enabled":false}`) {
				t.Errorf("bundle lacks build_cache disabled: %s", body)
			}

			if status, got := cacheRequest(t, http.MethodPost, h.cacheServer(t)+BuildCachePath+cacheKey("a"), h.token(t, c), nil); status != http.StatusNotFound {
				t.Errorf("reserve: status = %d, want 404 (%s)", status, got)
			}
		})
	}
}

// A key that is not 64 lowercase hex characters is 400 on every route, and a request without a
// valid token 401.
func TestBuildCache_BadKey(t *testing.T) {
	t.Parallel()

	h, c := newBuildCacheHarness(t, buildCacheYAML(""), Config{})
	srv, tok := h.cacheServer(t), h.token(t, c)

	for _, tt := range []struct{ method, path string }{
		{http.MethodGet, "abc"},
		{http.MethodGet, strings.Repeat("A", 64)},
		{http.MethodPost, strings.Repeat("g", 64)},
		{http.MethodPut, "abc/archive"},
		{http.MethodPost, "../done"},
	} {
		if status, body := cacheRequest(t, tt.method, srv+BuildCachePath+tt.path, tok, nil); status != http.StatusBadRequest {
			t.Errorf("%s %s: status = %d, want 400 (%s)", tt.method, tt.path, status, body)
		}
	}

	if status, body := cacheRequest(t, http.MethodGet, srv+BuildCachePath+cacheKey("a"), "forged", nil); status != http.StatusUnauthorized {
		t.Errorf("forged token: status = %d, want 401 (%s)", status, body)
	}
}

// A key with no ready cache is 404, as is its archive.
func TestBuildCache_Miss(t *testing.T) {
	t.Parallel()

	h, c := newBuildCacheHarness(t, buildCacheYAML(""), Config{})
	srv, tok := h.cacheServer(t), h.token(t, c)

	for _, p := range []string{cacheKey("a"), cacheKey("a") + "/archive"} {
		if status, body := cacheRequest(t, http.MethodGet, srv+BuildCachePath+p, tok, nil); status != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404 (%s)", p, status, body)
		}
	}

	// A reserved cache is still a miss.
	if status, body := cacheRequest(t, http.MethodPost, srv+BuildCachePath+cacheKey("a"), tok, nil); status != http.StatusOK {
		t.Fatalf("reserve: status = %d, want 200 (%s)", status, body)
	}

	if status, body := cacheRequest(t, http.MethodGet, srv+BuildCachePath+cacheKey("a"), tok, nil); status != http.StatusNotFound {
		t.Errorf("GET a saving cache: status = %d, want 404 (%s)", status, body)
	}
}

// A local box finds a ready cache at the gateway's archive path, which streams its object.
func TestBuildCache_HitLocal(t *testing.T) {
	t.Parallel()

	h, c := newBuildCacheHarness(t, buildCacheYAML(""), Config{})
	srv, tok, key := h.cacheServer(t), h.token(t, c), cacheKey("b")
	h.seedCache(t, key, []byte("zstd archive"))

	status, body := cacheRequest(t, http.MethodGet, srv+BuildCachePath+key, tok, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	u := cacheURLOf(t, body)
	if want := (buildCacheURL{URL: BuildCachePath + key + "/archive"}); u != want {
		t.Fatalf("lookup = %+v, want %+v", u, want)
	}

	if status, body := cacheRequest(t, http.MethodGet, srv+u.URL, tok, nil); status != http.StatusOK || string(body) != "zstd archive" {
		t.Errorf("archive = %d %q, want 200 with the object", status, body)
	}
}

// A cloud box (Config.KnowledgeURLTTL set) finds a ready cache at a URL signed for its object.
func TestBuildCache_HitSigned(t *testing.T) {
	t.Parallel()

	h, c := newBuildCacheHarness(t, buildCacheYAML(""), Config{KnowledgeURLTTL: 10 * time.Minute})
	key := cacheKey("c")
	h.seedCache(t, key, []byte("zstd archive"))

	status, body := cacheRequest(t, http.MethodGet, h.cacheServer(t)+BuildCachePath+key, h.token(t, c), nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	if u := cacheURLOf(t, body); !strings.HasPrefix(u.URL, porttest.PresignBase+h.cacheObject(key)+"?") || u.Method != "" {
		t.Errorf("lookup = %+v, want a GET URL signed for %s", u, h.cacheObject(key))
	}

	if strings.Contains(h.logs.String(), porttest.PresignBase) {
		t.Errorf("a signed URL was logged:\n%s", h.logs.String())
	}
}

// A key another box holds, or one that is ready, cannot be reserved.
func TestBuildCache_ReserveConflict(t *testing.T) {
	t.Parallel()

	h, c := newBuildCacheHarness(t, buildCacheYAML(""), Config{})
	srv, key := h.cacheServer(t), cacheKey("d")

	other := c
	other.RunID = "run-2"

	if status, body := cacheRequest(t, http.MethodPost, srv+BuildCachePath+key, h.token(t, c), nil); status != http.StatusOK {
		t.Fatalf("reserve: status = %d, want 200 (%s)", status, body)
	}

	if status, body := cacheRequest(t, http.MethodPost, srv+BuildCachePath+key, h.token(t, other), nil); status != http.StatusConflict {
		t.Errorf("reserve a held key: status = %d, want 409 (%s)", status, body)
	}

	h.seedCache(t, cacheKey("e"), []byte("ready"))

	if status, body := cacheRequest(t, http.MethodPost, srv+BuildCachePath+cacheKey("e"), h.token(t, other), nil); status != http.StatusConflict {
		t.Errorf("reserve a ready key: status = %d, want 409 (%s)", status, body)
	}

	if status, body := cacheRequest(t, http.MethodPut, srv+BuildCachePath+cacheKey("e")+"/archive", h.token(t, other), []byte("x")); status != http.StatusConflict {
		t.Errorf("upload over a ready key: status = %d, want 409 (%s)", status, body)
	}
}

// A cloud box reserves a key and gets a URL signed for uploading its object to the bucket, which
// done then commits.
func TestBuildCache_ReserveSignedUpload(t *testing.T) {
	t.Parallel()

	h, c := newBuildCacheHarness(t, buildCacheYAML(""), Config{KnowledgeURLTTL: 10 * time.Minute})
	bucket := httptest.NewServer(h.objects)
	t.Cleanup(bucket.Close)
	h.objects.ServeUploadsAt(bucket.URL + "/")

	srv, tok, key := h.cacheServer(t), h.token(t, c), cacheKey("f")

	status, body := cacheRequest(t, http.MethodPost, srv+BuildCachePath+key, tok, nil)
	if status != http.StatusOK {
		t.Fatalf("reserve: status = %d, want 200 (%s)", status, body)
	}

	u := cacheURLOf(t, body)
	if !strings.HasPrefix(u.URL, bucket.URL+"/"+h.cacheObject(key)+"?") || u.Method != http.MethodPut {
		t.Fatalf("reserve = %+v, want a PUT URL signed for %s", u, h.cacheObject(key))
	}

	if status, body := cacheRequest(t, http.MethodPut, u.URL, "", []byte("archive")); status != http.StatusOK {
		t.Fatalf("signed upload: status = %d (%s)", status, body)
	}

	if status, body := cacheRequest(t, http.MethodPost, srv+BuildCachePath+key+"/done", tok, []byte(`{"size_bytes":7}`)); status != http.StatusNoContent {
		t.Fatalf("done: status = %d, want 204 (%s)", status, body)
	}

	if status, body := cacheRequest(t, http.MethodGet, srv+BuildCachePath+key, tok, nil); status != http.StatusOK {
		t.Errorf("lookup after done: status = %d, want 200 (%s)", status, body)
	}

	if strings.Contains(h.logs.String(), bucket.URL) {
		t.Errorf("a signed URL was logged:\n%s", h.logs.String())
	}
}

// The gateway refuses an archive over its limit with 413 and stores nothing.
func TestBuildCache_UploadTooLarge(t *testing.T) {
	t.Parallel()

	h, c := newBuildCacheHarness(t, buildCacheYAML(""), Config{MaxBuildCacheBytes: 8})
	srv, tok, key := h.cacheServer(t), h.token(t, c), cacheKey("1")

	if status, body := cacheRequest(t, http.MethodPost, srv+BuildCachePath+key, tok, nil); status != http.StatusOK {
		t.Fatalf("reserve: status = %d, want 200 (%s)", status, body)
	}

	// Chunked, so the limit applies to the bytes read and not only to Content-Length.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, srv+BuildCachePath+key+"/archive", io.MultiReader(strings.NewReader("12345"), strings.NewReader("6789")))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("chunked upload of 9 bytes: status = %d, want 413", resp.StatusCode)
	}

	if status, body := cacheRequest(t, http.MethodPut, srv+BuildCachePath+key+"/archive", tok, []byte("123456789")); status != http.StatusRequestEntityTooLarge {
		t.Errorf("upload of 9 bytes: status = %d, want 413 (%s)", status, body)
	}

	if h.objects.Writes(h.cacheObject(key)) != 0 {
		t.Error("an archive over the limit was stored")
	}

	if status, body := cacheRequest(t, http.MethodPut, srv+BuildCachePath+key+"/archive", tok, []byte("12345678")); status != http.StatusNoContent {
		t.Errorf("upload of 8 bytes: status = %d, want 204 (%s)", status, body)
	}
}

// Done refuses a cache whose object was never uploaded; after a reserve, a local upload and done,
// the next lookup is a hit that streams the upload.
func TestBuildCache_UploadDoneThenHit(t *testing.T) {
	t.Parallel()

	h, c := newBuildCacheHarness(t, buildCacheYAML(""), Config{})
	srv, tok, key := h.cacheServer(t), h.token(t, c), cacheKey("2")

	if status, body := cacheRequest(t, http.MethodPost, srv+BuildCachePath+key, tok, nil); status != http.StatusOK {
		t.Fatalf("reserve: status = %d, want 200 (%s)", status, body)
	}

	if status, body := cacheRequest(t, http.MethodPost, srv+BuildCachePath+key+"/done", tok, []byte(`{"size_bytes":3}`)); status != http.StatusConflict {
		t.Errorf("done before upload: status = %d, want 409 (%s)", status, body)
	}

	if status, body := cacheRequest(t, http.MethodPost, srv+BuildCachePath+key+"/done", tok, []byte(`{"size_bytes":-1}`)); status != http.StatusBadRequest {
		t.Errorf("done with a negative size: status = %d, want 400 (%s)", status, body)
	}

	h.saveCache(t, srv, c, cacheKey("3"), []byte("the archive"))

	status, body := cacheRequest(t, http.MethodGet, srv+BuildCachePath+cacheKey("3"), tok, nil)
	if status != http.StatusOK {
		t.Fatalf("lookup: status = %d, want 200 (%s)", status, body)
	}

	if status, body := cacheRequest(t, http.MethodGet, srv+cacheURLOf(t, body).URL, tok, nil); status != http.StatusOK || string(body) != "the archive" {
		t.Errorf("archive = %d %q, want 200 with the upload", status, body)
	}

	if _, err := h.objects.Get(t.Context(), h.cacheObject(cacheKey("3"))); err != nil {
		t.Errorf("the object is not under the organization's build-cache prefix: %v", err)
	}
}

// Done keeps the newest domain.BuildCacheKeep caches of the repository and deletes the others'
// objects.
func TestBuildCache_PrunesPastThree(t *testing.T) {
	t.Parallel()

	h, c := newBuildCacheHarness(t, buildCacheYAML(""), Config{})
	srv, tok := h.cacheServer(t), h.token(t, c)
	keys := []string{cacheKey("4"), cacheKey("5"), cacheKey("6"), cacheKey("7")}

	for _, k := range keys {
		h.saveCache(t, srv, c, k, []byte("archive "+k[:1]))
		h.clock.Advance(time.Minute)
	}

	if status, body := cacheRequest(t, http.MethodGet, srv+BuildCachePath+keys[0], tok, nil); status != http.StatusNotFound {
		t.Errorf("oldest cache: status = %d, want 404 (%s)", status, body)
	}

	if _, err := h.objects.Get(t.Context(), h.cacheObject(keys[0])); err == nil {
		t.Error("the oldest cache's object was not deleted")
	}

	for _, k := range keys[1:] {
		if status, body := cacheRequest(t, http.MethodGet, srv+BuildCachePath+k, tok, nil); status != http.StatusOK {
			t.Errorf("cache %s: status = %d, want 200 (%s)", k[:1], status, body)
		}
	}
}

// failingDeletes is a bucket whose deletes fail.
type failingDeletes struct{ *porttest.ObjectStore }

func (failingDeletes) Delete(_ context.Context, key string) error {
	return errors.New("delete " + key + ": bucket unavailable")
}

// A pruned object the bucket fails to delete is logged, and done still commits.
func TestBuildCache_PruneDeleteFailureStillCommits(t *testing.T) {
	t.Parallel()

	h, c := newBuildCacheHarnessWith(t, buildCacheYAML(""), Config{}, func(s *porttest.ObjectStore) Objects { return failingDeletes{s} })
	srv, tok := h.cacheServer(t), h.token(t, c)
	keys := []string{cacheKey("8"), cacheKey("9"), cacheKey("a"), cacheKey("b")}

	for _, k := range keys {
		h.saveCache(t, srv, c, k, []byte("archive"))
		h.clock.Advance(time.Minute)
	}

	if status, body := cacheRequest(t, http.MethodGet, srv+BuildCachePath+keys[3], tok, nil); status != http.StatusOK {
		t.Errorf("newest cache: status = %d, want 200 (%s)", status, body)
	}

	if status, body := cacheRequest(t, http.MethodGet, srv+BuildCachePath+keys[0], tok, nil); status != http.StatusNotFound {
		t.Errorf("pruned cache: status = %d, want 404 (%s)", status, body)
	}

	logs := h.logs.String()
	if !strings.Contains(logs, "build cache object not deleted") {
		t.Errorf("the failed delete was not logged:\n%s", logs)
	}

	if strings.Contains(logs, "Bearer") || strings.Contains(logs, tok) {
		t.Errorf("a token was logged:\n%s", logs)
	}
}
