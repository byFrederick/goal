//go:build integration

package s3store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/byFrederick/goal/internal/ports"
)

// newTestStore returns a store on a fresh bucket, created by EnsureBucket and so without
// versioning (as on R2), on the S3-compatible server at GOAL_TEST_S3_ENDPOINT (RustFS from
// compose.yaml; `task test-integration` sets it up), and removes the bucket when the test ends.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	endpoint := os.Getenv("GOAL_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Fatal("GOAL_TEST_S3_ENDPOINT is not set; run `task test-integration`")
	}

	s, err := New(t.Context(), Config{
		// Random, not the time: parallel tests started in the same clock tick shared a bucket.
		Bucket:    "goal-it-" + strings.ToLower(rand.Text()),
		Endpoint:  endpoint,
		AccessKey: os.Getenv("GOAL_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("GOAL_S3_SECRET_KEY"),
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := s.EnsureBucket(t.Context())
	if err != nil || !created {
		t.Fatalf("EnsureBucket = %v, %v; want a new bucket", created, err)
	}

	t.Cleanup(func() { removeBucket(t, s) })

	return s
}

// removeBucket deletes every object version and delete marker in the bucket, so it also removes
// the bucket of the test that turns versioning on, and then the bucket.
func removeBucket(t *testing.T, s *Store) {
	t.Helper()

	ctx := context.Background()
	pages := s3.NewListObjectVersionsPaginator(s.client, &s3.ListObjectVersionsInput{Bucket: aws.String(s.bucket)})

	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			t.Errorf("cleanup: %v", err)

			return
		}

		for _, v := range page.Versions {
			s.deleteVersion(ctx, t, v.Key, v.VersionId)
		}

		for _, m := range page.DeleteMarkers {
			s.deleteVersion(ctx, t, m.Key, m.VersionId)
		}
	}

	if _, err := s.client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(s.bucket)}); err != nil {
		t.Errorf("cleanup: %v", err)
	}
}

func (s *Store) deleteVersion(ctx context.Context, t *testing.T, key, version *string) {
	t.Helper()

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: key, VersionId: version})
	if err != nil {
		t.Errorf("cleanup: %v", err)
	}
}

// versioning is the bucket's versioning status: empty when it was never turned on.
func versioning(t *testing.T, s *Store) types.BucketVersioningStatus {
	t.Helper()

	out, err := s.client.GetBucketVersioning(t.Context(), &s3.GetBucketVersioningInput{Bucket: aws.String(s.bucket)})
	if err != nil {
		t.Fatal(err)
	}

	return out.Status
}

// The bucket needs no versioning (R2 has none): EnsureBucket never turns it on.
func TestEnsureBucket_IsIdempotentAndLeavesVersioningOff(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	created, err := s.EnsureBucket(t.Context())
	if err != nil || created {
		t.Fatalf("second EnsureBucket = %v, %v; want existing bucket, no error", created, err)
	}

	if status := versioning(t, s); status != "" {
		t.Errorf("versioning = %q; want never enabled", status)
	}
}

// A write replaces the object, and its ETag says which content is there.
func TestPut_ReplacesTheObject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newTestStore(t)
	key := "catalog/skills/index.json"

	v1, err := s.Put(ctx, key, []byte("[1]"))
	if err != nil {
		t.Fatal(err)
	}

	v2, err := s.Put(ctx, key, []byte("[1,2]"))
	if err != nil {
		t.Fatal(err)
	}

	if v1.ETag == "" || v1.ETag == v2.ETag || v2.Size != int64(len("[1,2]")) {
		t.Fatalf("Put = %+v then %+v; want two distinct ETags and the size", v1, v2)
	}

	got, err := s.Get(ctx, key)
	if err != nil || string(got.Body) != "[1,2]" || got.ObjectInfo != v2 {
		t.Errorf("Get = %+v %q, %v; want the second write %+v", got.ObjectInfo, got.Body, err, v2)
	}
}

// Delete removes an object for good; deleting a missing key is not an error.
func TestDelete_RemovesTheObject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newTestStore(t)
	key := "orgs/acme/knowledge/0123"

	if _, err := s.Put(ctx, key, []byte("doc")); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := s.Delete(ctx, key); err != nil {
			t.Fatalf("Delete = %v", err)
		}
	}

	if _, err := s.Get(ctx, key); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Get after Delete: %v, want ErrNotFound", err)
	}

	if got, err := s.List(ctx, "orgs/"); err != nil || len(got) != 0 {
		t.Errorf("List after Delete = %+v, %v; want nothing", got, err)
	}
}

func TestList_ReturnsObjectsUnderThePrefix(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newTestStore(t)

	for _, key := range []string{"catalog/b/x.md", "catalog/a/x.md", "catalog/a/gone.md", "other/x.md"} {
		if _, err := s.Put(ctx, key, []byte(key)); err != nil {
			t.Fatal(err)
		}
	}

	latest, err := s.Put(ctx, "catalog/a/x.md", []byte("changed"))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Delete(ctx, "catalog/a/gone.md"); err != nil {
		t.Fatal(err)
	}

	got, err := s.List(ctx, "catalog/")
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0] != latest || got[1].Key != "catalog/b/x.md" {
		t.Fatalf("List = %+v, want %+v and catalog/b/x.md", got, latest)
	}
}

// A purge leaves nothing under the prefix, across list pages (at most 1000 keys each), and
// touches nothing outside it; an empty prefix, which would name the whole bucket, is refused.
func TestDeletePrefix_RemovesEveryObjectUnderThePrefixOnly(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newTestStore(t)

	const under = 1003

	putMany(t, s, "orgs/a/blobs/", under)

	if _, err := s.Put(ctx, "orgs/ab/z.yaml", []byte("z")); err != nil {
		t.Fatal(err)
	}

	if n, err := s.DeletePrefix(ctx, "orgs/a/"); err != nil || n != under {
		t.Fatalf("DeletePrefix = %d, %v; want %d objects removed", n, err, under)
	}

	if got, err := s.List(ctx, "orgs/a/"); err != nil || len(got) != 0 {
		t.Errorf("List after the purge = %d objects, %v; want none", len(got), err)
	}

	if _, err := s.Get(ctx, "orgs/ab/z.yaml"); err != nil {
		t.Errorf("an object outside the prefix = %v; want it untouched", err)
	}

	if n, err := s.DeletePrefix(ctx, "orgs/a/"); err != nil || n != 0 {
		t.Errorf("DeletePrefix again = %d, %v; want nothing left to remove", n, err)
	}

	if _, err := s.DeletePrefix(ctx, ""); err == nil {
		t.Error("DeletePrefix of the whole bucket = nil; want it refused")
	}
}

// putMany writes n small objects under prefix, a few at a time.
func putMany(t *testing.T, s *Store, prefix string, n int) {
	t.Helper()

	var (
		wg   sync.WaitGroup
		errs = make([]error, n)
		sem  = make(chan struct{}, 32)
	)

	for i := range n {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			_, errs[i] = s.Put(t.Context(), fmt.Sprintf("%s%04d", prefix, i), []byte("x"))
		})
	}

	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
}

func TestGet_MissingKeyIsNotFound(t *testing.T) {
	t.Parallel()

	if _, err := newTestStore(t).Get(t.Context(), "orgs/none/blobs/0123"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("missing key: %v, want ErrNotFound", err)
	}
}

// Definition blobs are written once: a second write of a key keeps the first object, on the
// server's conditional write and on the fallback for servers without one. A deleted key is
// absent and written again.
func TestPutIfAbsent_WritesOnce(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		unconditional bool
	}{
		{"conditional write", false},
		{"head then put", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := newTestStore(t)
			s.unconditional.Store(tc.unconditional)
			checkWritesOnce(t, s)

			if s.unconditional.Load() != tc.unconditional {
				t.Errorf("unconditional = %v; want %v (RustFS supports If-None-Match)", s.unconditional.Load(), tc.unconditional)
			}
		})
	}
}

func checkWritesOnce(t *testing.T, s *Store) {
	t.Helper()

	ctx := t.Context()

	const key = "orgs/acme/blobs/0123"

	first, created, err := s.PutIfAbsent(ctx, key, []byte("first"))
	if err != nil || !created || first.ETag == "" || first.Size != int64(len("first")) {
		t.Fatalf("first PutIfAbsent = %+v, %v, %v; want it created", first, created, err)
	}

	again, created, err := s.PutIfAbsent(ctx, key, []byte("second, longer"))
	if err != nil || created || again != first {
		t.Errorf("second PutIfAbsent = %+v, %v, %v; want %+v, not created", again, created, err, first)
	}

	if got, err := s.Get(ctx, key); err != nil || string(got.Body) != "first" || got.ObjectInfo != first {
		t.Errorf("Get = %+v %q, %v; want the first content %+v", got.ObjectInfo, got.Body, err, first)
	}

	checkAbsentAfterDelete(t, s, first)
}

// checkAbsentAfterDelete deletes first's key: a deleted key is written again.
func checkAbsentAfterDelete(t *testing.T, s *Store, first ports.ObjectInfo) {
	t.Helper()

	if err := s.Delete(t.Context(), first.Key); err != nil {
		t.Fatal(err)
	}

	after, created, err := s.PutIfAbsent(t.Context(), first.Key, []byte("after delete"))
	if err != nil || !created || after.ETag == first.ETag {
		t.Errorf("PutIfAbsent after Delete = %+v, %v, %v; want it written again", after, created, err)
	}
}

// A bucket from before phase 4 still has versioning on: it keeps working, and nothing the store
// lists or reads is an older version or a deleted key.
func TestStore_WorksOnABucketWithVersioning(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newTestStore(t)

	_, err := s.client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket:                  aws.String(s.bucket),
		VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Put(ctx, "orgs/a/x.md", []byte("v1")); err != nil {
		t.Fatal(err)
	}

	latest, err := s.Put(ctx, "orgs/a/x.md", []byte("v2"))
	if err != nil {
		t.Fatal(err)
	}

	if got, err := s.List(ctx, "orgs/"); err != nil || len(got) != 1 || got[0] != latest {
		t.Errorf("List = %+v, %v; want only the current %+v", got, err, latest)
	}

	checkAbsentAfterDelete(t, s, latest)

	if _, err := s.Get(ctx, "orgs/b/none.md"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Get of a missing key = %v; want ErrNotFound", err)
	}

	if n, err := s.DeletePrefix(ctx, "orgs/"); err != nil || n != 1 {
		t.Errorf("DeletePrefix = %d, %v; want the one current object", n, err)
	}

	if got, err := s.List(ctx, "orgs/"); err != nil || len(got) != 0 {
		t.Errorf("List after DeletePrefix = %+v, %v; want nothing", got, err)
	}
}
