//go:build integration

package s3store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/byFrederick/goal/internal/ports"
)

// newTestStore returns a store on a fresh, versioned bucket on the S3-compatible server at
// GOAL_TEST_S3_ENDPOINT (RustFS from compose.yaml; `task test-integration` sets it up) and
// removes the bucket, every version included, when the test ends.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	endpoint := os.Getenv("GOAL_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Fatal("GOAL_TEST_S3_ENDPOINT is not set; run `task test-integration`")
	}

	s, err := New(t.Context(), Config{
		Bucket:    fmt.Sprintf("goal-it-%d", time.Now().UnixNano()),
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

func TestEnsureBucket_IsIdempotent(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	created, err := s.EnsureBucket(t.Context())
	if err != nil || created {
		t.Fatalf("second EnsureBucket = %v, %v; want existing bucket, no error", created, err)
	}
}

// Rollback depends on this: every write keeps the earlier content readable by version id.
func TestPut_KeepsEveryVersionReadable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newTestStore(t)
	key := "factories/acme/agents/writer.md"

	v1, err := s.Put(ctx, key, []byte("# writer v1\n"))
	if err != nil {
		t.Fatal(err)
	}

	v2, err := s.Put(ctx, key, []byte("# writer v2\n"))
	if err != nil {
		t.Fatal(err)
	}

	if v1.Version == "" || v1.Version == v2.Version {
		t.Fatalf("versions %q and %q: want two distinct ids", v1.Version, v2.Version)
	}

	for _, tc := range []struct{ version, want string }{{"", "# writer v2\n"}, {v1.Version, "# writer v1\n"}} {
		got, err := s.Get(ctx, key, tc.version)
		if err != nil || string(got.Body) != tc.want {
			t.Errorf("Get(%q) = %q, %v; want %q", tc.version, got.Body, err, tc.want)
		}
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Get(ctx, key, ""); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Get after Delete: %v, want ErrNotFound", err)
	}

	if got, err := s.Get(ctx, key, v2.Version); err != nil || string(got.Body) != "# writer v2\n" {
		t.Errorf("Get(v2) after Delete = %q, %v; want the deleted content restorable", got.Body, err)
	}
}

func TestList_ReturnsCurrentObjectsUnderThePrefix(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newTestStore(t)

	for _, key := range []string{"factories/b/factory.yaml", "factories/a/factory.yaml", "factories/a/gone.md", "other/x.md"} {
		if _, err := s.Put(ctx, key, []byte(key)); err != nil {
			t.Fatal(err)
		}
	}

	latest, err := s.Put(ctx, "factories/a/factory.yaml", []byte("changed"))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Delete(ctx, "factories/a/gone.md"); err != nil {
		t.Fatal(err)
	}

	got, err := s.List(ctx, "factories/")
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].Key != "factories/a/factory.yaml" || got[1].Key != "factories/b/factory.yaml" {
		t.Fatalf("List = %+v, want factories/a/factory.yaml and factories/b/factory.yaml", got)
	}

	if got[0].Version != latest.Version || got[0].Size != int64(len("changed")) {
		t.Errorf("List entry %+v, want the current version %s", got[0], latest.Version)
	}
}

func TestGet_MissingKeyOrVersionIsNotFound(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newTestStore(t)

	if _, err := s.Get(ctx, "factories/none/factory.yaml", ""); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("missing key: %v, want ErrNotFound", err)
	}

	if _, err := s.Put(ctx, "k", []byte("v")); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Get(ctx, "k", "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("missing version: %v, want ErrNotFound", err)
	}
}
