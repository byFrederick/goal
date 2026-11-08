package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// BuildCaches is the index of the build caches of the organization in ctx (ADR 0045);
// ports.BuildCaches satisfies it.
type BuildCaches interface {
	FindBuildCache(ctx context.Context, repositoryKey, cacheKey string, at time.Time) (domain.BuildCache, error)
	ReserveBuildCache(ctx context.Context, n domain.NewBuildCache) (bool, error)
	CommitBuildCache(ctx context.Context, repositoryKey, cacheKey, objectKey string, sizeBytes int64, at time.Time) error
	PruneBuildCaches(ctx context.Context, repositoryKey string, keep int, at time.Time) ([]string, error)
}

// BuildCachePath is the folder of a run's build caches: GET and POST BuildCachePath<key>, GET and
// PUT BuildCachePath<key>/archive, POST BuildCachePath<key>/done.
const BuildCachePath = "/run/build-cache/"

// The sub-paths of a cache key.
const (
	cacheArchive = "archive"
	cacheDone    = "done"
)

const (
	// defaultMaxBuildCacheBytes caps one archive a box uploads through the gateway: 2 GiB.
	defaultMaxBuildCacheBytes = 2 << 30
	// cacheUploadTimeout bounds reading one archive upload, past the server's read timeout.
	cacheUploadTimeout = 30 * time.Minute
)

// bundleBuildCache is whether the run restores and saves the build cache of its workspace's
// repository through the routes under BuildCachePath.
type bundleBuildCache struct {
	Enabled bool `json:"enabled"`
}

// buildCacheURL is where a box downloads a ready cache (Method empty: GET) or uploads the one it
// reserved (Method PUT): a signed bucket URL for a cloud box, or a path on the gateway.
type buildCacheURL struct {
	URL    string `json:"url"`
	Method string `json:"method,omitempty"`
}

// cacheRepository is the repository key of a run's build cache: the hex SHA-256 of the normalized
// key (domain.RepositoryKey) of its workspace's repository, the first of the item's repositories
// (rows, else the line's default entry), else the legacy runtime.repository. False when the run
// has no repository or the line turns the cache off. The bundle and the routes both use it, so
// build_cache.enabled and the key the routes use agree.
func cacheRepository(line domain.Line, rows []domain.ItemRepository) (string, bool) {
	if !line.Runtime.BuildCacheEnabled() {
		return "", false
	}

	raw := line.Runtime.Repository.URL
	if repos := domain.ResolveItemRepositories(rows, line); len(repos) > 0 {
		raw = repos[0].Repo
	}

	key, ok := domain.RepositoryKey(raw)
	if !ok {
		return "", false
	}

	sum := sha256.Sum256([]byte(key))

	return hex.EncodeToString(sum[:]), true
}

// servesBuildCache reports whether the gateway has the index and the bucket the routes need.
func (g *Gateway) servesBuildCache() bool { return g.caches != nil && g.objects != nil }

// buildCacheObject is the bucket key of the organization's cache key of repository: under its
// prefix (domain.OrgPrefix), so the organization's purge removes it.
func buildCacheObject(org domain.OrgID, repository, key string) string {
	return domain.OrgPrefix(org) + "build-cache/" + repository + "/" + key + ".tar.zst"
}

// errNoBuildCache means the run in the token has no build cache: it is not a station run, has no
// repository, or its line turns the cache off.
var errNoBuildCache = errors.New("the run has no build cache")

// runCacheRepository is the repository key of the build cache of the run in c (cacheRepository),
// errNoBuildCache when it has none.
func (g *Gateway) runCacheRepository(ctx context.Context, c domain.TaskClaims) (string, error) {
	if c.Kind != "" || !g.servesBuildCache() {
		return "", errNoBuildCache
	}

	run, err := g.run(ctx, c)
	if err != nil {
		return "", err
	}

	rows, err := g.items.ItemRepositories(ctx, run.item.ID)
	if err != nil {
		return "", fmt.Errorf("repositories of item %s: %w", run.item.ID, err)
	}

	repo, ok := cacheRepository(run.line, rows)
	if !ok {
		return "", errNoBuildCache
	}

	return repo, nil
}

// cacheRoute is one request under BuildCachePath once authorized: its run's claims, the run's
// repository key and the cache key. The request's context carries the token's organization.
type cacheRoute struct {
	claims     domain.TaskClaims
	repository string
	key        string
}

func (rt cacheRoute) object() string { return buildCacheObject(rt.claims.OrgID, rt.repository, rt.key) }

func (rt cacheRoute) attrs(more ...any) []any {
	return claimAttrs(rt.claims, append([]any{"cache", rt.key}, more...)...)
}

// ServeBuildCache serves the build cache of the run's workspace repository (ADR 0045), scoped to
// the task token's organization:
//
//   - GET <key>: 404 without a ready cache, else {url} to download it from.
//   - POST <key>: reserve key; 409 when another box holds it or it is ready, else {url, method}
//     to upload the archive to.
//   - GET <key>/archive: the ready cache's object.
//   - PUT <key>/archive: store the body as the object, 413 over Config.MaxBuildCacheBytes.
//   - POST <key>/done with {size_bytes}: commit the reserved cache once its object exists (409
//     otherwise), then prune the repository's caches to domain.BuildCacheKeep.
//
// A token that does not verify is 401 and a key that is not 64 lowercase hex characters 400; a
// run with no build cache (bundle's build_cache.enabled false) is 404 on every route. No URL or
// token is logged.
func (g *Gateway) ServeBuildCache(w http.ResponseWriter, r *http.Request) {
	rt, sub, r, ok := g.authorizeCache(w, r)
	if !ok {
		return
	}

	switch {
	case sub == "" && r.Method == http.MethodGet:
		g.findCache(w, r, rt)
	case sub == "":
		g.reserveCache(w, r, rt)
	case sub == cacheArchive && r.Method == http.MethodGet:
		g.serveCacheArchive(w, r, rt)
	case sub == cacheArchive:
		g.putCacheArchive(w, r, rt)
	default:
		g.commitCache(w, r, rt)
	}
}

// authorizeCache checks r's token, key, route and method and finds the run's repository,
// answering w when one fails. It returns the route, its sub-path and r bound to the token's
// organization.
func (g *Gateway) authorizeCache(w http.ResponseWriter, r *http.Request) (cacheRoute, string, *http.Request, bool) {
	claims, err := g.authenticate(r)
	if err != nil {
		g.unauthorized(w, r, err)
		return cacheRoute{}, "", r, false
	}

	key, sub, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, BuildCachePath), "/")
	if !domain.ValidBuildCacheKey(key) {
		http.Error(w, "a build cache key is 64 lowercase hex characters", http.StatusBadRequest)
		return cacheRoute{}, "", r, false
	}

	allow := map[string][]string{
		"":           {http.MethodGet, http.MethodPost},
		cacheArchive: {http.MethodGet, http.MethodPut},
		cacheDone:    {http.MethodPost},
	}

	methods, ok := allow[sub]
	if !ok {
		http.NotFound(w, r)
		return cacheRoute{}, "", r, false
	}

	if !slices.Contains(methods, r.Method) {
		w.Header().Set("Allow", strings.Join(methods, ", "))
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return cacheRoute{}, "", r, false
	}

	ctx := domain.WithOrg(r.Context(), claims.OrgID)
	rt := cacheRoute{claims: claims, key: key}

	rt.repository, err = g.runCacheRepository(ctx, claims)

	switch {
	case errors.Is(err, errNoBuildCache):
		http.NotFound(w, r)
		return cacheRoute{}, "", r, false
	case errors.Is(err, errForbidden) || errors.Is(err, ports.ErrNotFound):
		g.log.WarnContext(ctx, "build cache request forbidden", rt.attrs("reason", err)...)
		http.Error(w, "forbidden", http.StatusForbidden)

		return cacheRoute{}, "", r, false
	case err != nil:
		g.cacheFailed(ctx, w, rt, err)
		return cacheRoute{}, "", r, false
	}

	return rt, sub, r.WithContext(ctx), true
}

// cacheFailed answers a failure of the index or the bucket with 503, logged without a URL.
func (g *Gateway) cacheFailed(ctx context.Context, w http.ResponseWriter, rt cacheRoute, err error) {
	g.log.ErrorContext(ctx, "build cache failed", rt.attrs("error", err)...)
	http.Error(w, "gateway unavailable", http.StatusServiceUnavailable)
}

// findCache answers GET <key>: where the box downloads the ready cache from, a URL signed for
// Config.KnowledgeURLTTL when it is set, else, or when signing fails, the gateway's archive path.
func (g *Gateway) findCache(w http.ResponseWriter, r *http.Request, rt cacheRoute) {
	ctx := r.Context()

	c, err := g.caches.FindBuildCache(ctx, rt.repository, rt.key, g.clock.Now())

	switch {
	case errors.Is(err, ports.ErrNotFound):
		g.log.InfoContext(ctx, "build cache missed", rt.attrs()...)
		http.NotFound(w, r)

		return
	case err != nil:
		g.cacheFailed(ctx, w, rt, err)
		return
	}

	u := buildCacheURL{URL: BuildCachePath + rt.key + "/" + cacheArchive}
	if signed, ok := g.signCache(ctx, rt, c.ObjectKey, g.objects.PresignGet); ok {
		u.URL = signed
	}

	g.log.InfoContext(ctx, "build cache found", rt.attrs("bytes", c.SizeBytes)...)
	g.writeCacheJSON(ctx, w, rt, u)
}

// reserveCache answers POST <key>: the box that gets the key uploads its archive to the URL, a
// PresignPut one when Config.KnowledgeURLTTL is set, else the gateway's archive path.
func (g *Gateway) reserveCache(w http.ResponseWriter, r *http.Request, rt cacheRoute) {
	ctx := r.Context()

	got, err := g.caches.ReserveBuildCache(ctx, domain.NewBuildCache{
		RepositoryKey: rt.repository, CacheKey: rt.key, ObjectKey: rt.object(), At: g.clock.Now(),
	})

	switch {
	case err != nil:
		g.cacheFailed(ctx, w, rt, err)
		return
	case !got:
		http.Error(w, "the build cache is being saved by another run or is ready", http.StatusConflict)
		return
	}

	u := buildCacheURL{URL: BuildCachePath + rt.key + "/" + cacheArchive, Method: http.MethodPut}
	if signed, ok := g.signCache(ctx, rt, rt.object(), g.objects.PresignPut); ok {
		u.URL = signed
	}

	g.log.InfoContext(ctx, "build cache reserved", rt.attrs()...)
	g.writeCacheJSON(ctx, w, rt, u)
}

// signCache signs object with sign for Config.KnowledgeURLTTL; false when it is not set or
// signing fails (logged, never with a URL).
func (g *Gateway) signCache(ctx context.Context, rt cacheRoute, object string, sign func(context.Context, string, time.Duration) (string, error)) (string, bool) {
	if g.cfg.KnowledgeURLTTL <= 0 {
		return "", false
	}

	u, err := sign(ctx, object, g.cfg.KnowledgeURLTTL)
	if err != nil {
		g.log.WarnContext(ctx, "build cache url unsigned", rt.attrs("error", err)...)
		return "", false
	}

	return u, true
}

func (g *Gateway) writeCacheJSON(ctx context.Context, w http.ResponseWriter, rt cacheRoute, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	if err := json.NewEncoder(w).Encode(v); err != nil {
		g.log.WarnContext(ctx, "write build cache answer", rt.attrs("error", err)...)
	}
}

// serveCacheArchive answers GET <key>/archive: the ready cache's object, streamed.
func (g *Gateway) serveCacheArchive(w http.ResponseWriter, r *http.Request, rt cacheRoute) {
	ctx := r.Context()

	c, err := g.caches.FindBuildCache(ctx, rt.repository, rt.key, g.clock.Now())

	switch {
	case errors.Is(err, ports.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		g.cacheFailed(ctx, w, rt, err)
		return
	}

	rc, err := g.objects.GetRange(ctx, c.ObjectKey, 0, -1)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		g.log.WarnContext(ctx, "build cache object missing", rt.attrs()...)
		http.NotFound(w, r)

		return
	case err != nil:
		g.cacheFailed(ctx, w, rt, err)
		return
	}

	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", "application/zstd")
	w.Header().Set("Cache-Control", "no-store")

	if c.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(c.SizeBytes, 10))
	}

	n, err := io.Copy(w, rc)
	if err != nil {
		g.log.WarnContext(ctx, "write build cache archive", rt.attrs("error", err)...)
		return
	}

	g.log.InfoContext(ctx, "build cache served", rt.attrs("bytes", n)...)
}

// putCacheArchive answers PUT <key>/archive: the body, at most Config.MaxBuildCacheBytes (413
// past it), streamed to the reserved key's object. A ready cache is 409: its object is never
// rewritten.
func (g *Gateway) putCacheArchive(w http.ResponseWriter, r *http.Request, rt cacheRoute) {
	ctx := r.Context()

	_, err := g.caches.FindBuildCache(ctx, rt.repository, rt.key, g.clock.Now())

	switch {
	case err == nil:
		http.Error(w, "the build cache is ready", http.StatusConflict)
		return
	case !errors.Is(err, ports.ErrNotFound):
		g.cacheFailed(ctx, w, rt, err)
		return
	}

	if r.ContentLength > g.cfg.MaxBuildCacheBytes {
		http.Error(w, "the archive is too large", http.StatusRequestEntityTooLarge)
		return
	}

	// An archive takes longer to read than the server's read timeout allows. The deadline is the
	// connection's, on the wall clock; a ResponseWriter without read deadlines (a test recorder)
	// keeps the server's.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(cacheUploadTimeout))

	info, err := g.objects.PutStream(ctx, rt.object(), http.MaxBytesReader(w, r.Body, g.cfg.MaxBuildCacheBytes))
	if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
		http.Error(w, "the archive is too large", http.StatusRequestEntityTooLarge)
		return
	}

	if err != nil {
		g.cacheFailed(ctx, w, rt, err)
		return
	}

	g.log.InfoContext(ctx, "build cache uploaded", rt.attrs("bytes", info.Size)...)
	w.WriteHeader(http.StatusNoContent)
}

// cacheDoneRequest is what a box posts once its archive is uploaded.
type cacheDoneRequest struct {
	SizeBytes int64 `json:"size_bytes"`
}

// commitCache answers POST <key>/done: the reserved cache is committed once its object exists
// (409 when it does not, or when the reservation was taken over), then the repository's caches
// are pruned to domain.BuildCacheKeep and the pruned objects deleted. A failed prune or delete is
// logged and never fails the request.
func (g *Gateway) commitCache(w http.ResponseWriter, r *http.Request, rt cacheRoute) {
	ctx := r.Context()

	var req cacheDoneRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, g.cfg.MaxRequestBytes)).Decode(&req); err != nil || req.SizeBytes < 0 {
		http.Error(w, `the body is {"size_bytes": <bytes at or above 0>}`, http.StatusBadRequest)
		return
	}

	objects, err := g.objects.List(ctx, rt.object())
	if err != nil {
		g.cacheFailed(ctx, w, rt, err)
		return
	}

	if !slices.ContainsFunc(objects, func(o ports.ObjectInfo) bool { return o.Key == rt.object() }) {
		http.Error(w, "the archive was not uploaded", http.StatusConflict)
		return
	}

	now := g.clock.Now()

	err = g.caches.CommitBuildCache(ctx, rt.repository, rt.key, rt.object(), req.SizeBytes, now)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		http.Error(w, "the build cache is not reserved by this run", http.StatusConflict)
		return
	case err != nil:
		g.cacheFailed(ctx, w, rt, err)
		return
	}

	g.log.InfoContext(ctx, "build cache committed", rt.attrs("bytes", req.SizeBytes)...)
	g.pruneCaches(ctx, rt, now)
	w.WriteHeader(http.StatusNoContent)
}

// pruneCaches prunes rt's repository to domain.BuildCacheKeep caches and deletes the pruned
// objects, logging what fails.
func (g *Gateway) pruneCaches(ctx context.Context, rt cacheRoute, at time.Time) {
	pruned, err := g.caches.PruneBuildCaches(ctx, rt.repository, domain.BuildCacheKeep, at)
	if err != nil {
		g.log.WarnContext(ctx, "build caches not pruned", rt.attrs("error", err)...)
		return
	}

	for _, key := range pruned {
		if err := g.objects.Delete(ctx, key); err != nil {
			g.log.WarnContext(ctx, "build cache object not deleted", rt.attrs("object", key, "error", err)...)
		}
	}
}
