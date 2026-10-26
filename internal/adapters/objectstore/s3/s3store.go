// Package s3 implements ports.ObjectStore on any S3-compatible server (RustFS on a laptop,
// R2 on Cloudflare, Amazon S3) with the AWS SDK for Go v2. It uses no object versioning API, so
// a server without versioning (R2) behaves as one with it.
//
// Credentials come from the standard AWS chain (environment, SSO profile, ECS task role). A
// local S3-compatible server instead takes the static keys in Config, which are never used
// unless both are set.
package s3

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.ObjectStore = (*Store)(nil)

// Config locates the bucket. Only Bucket is required.
type Config struct {
	Bucket string
	// Endpoint is the URL of an S3-compatible server, for example http://127.0.0.1:9000. Empty
	// means AWS S3. A custom endpoint uses path-style addressing.
	Endpoint string
	// Region defaults to the AWS chain's region, then us-east-1.
	Region string
	// AccessKey and SecretKey are for a local S3-compatible server only. Leave them empty on
	// AWS so the standard credential chain applies.
	AccessKey string
	SecretKey string
}

// ConfigFromEnv reads GOAL_S3_BUCKET, GOAL_S3_ENDPOINT, GOAL_S3_ACCESS_KEY and
// GOAL_S3_SECRET_KEY. The region comes from the AWS chain (AWS_REGION, profile).
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		Bucket:    getenv("GOAL_S3_BUCKET"),
		Endpoint:  getenv("GOAL_S3_ENDPOINT"),
		AccessKey: getenv("GOAL_S3_ACCESS_KEY"),
		SecretKey: getenv("GOAL_S3_SECRET_KEY"),
	}
	if c.Bucket == "" {
		return Config{}, errors.New("GOAL_S3_BUCKET is not set")
	}

	if (c.AccessKey == "") != (c.SecretKey == "") {
		return Config{}, errors.New("set both GOAL_S3_ACCESS_KEY and GOAL_S3_SECRET_KEY, or neither")
	}

	return c, nil
}

// Store is one bucket. It is safe for concurrent use.
type Store struct {
	client *s3.Client
	bucket string
	region string
	// unconditional is set once the server answered a conditional write with 501.
	unconditional atomic.Bool
}

// New builds a client for cfg. It makes no network calls.
func New(ctx context.Context, cfg Config) (*Store, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}

	if cfg.AccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")))
	}

	ac, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	if ac.Region == "" {
		ac.Region = "us-east-1"
	}

	client := s3.NewFromConfig(ac, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		}
	})

	return &Store{client: client, bucket: cfg.Bucket, region: ac.Region}, nil
}

// Bucket returns the bucket's name.
func (s *Store) Bucket() string { return s.bucket }

// EnsureBucket creates the bucket when it is missing and reports whether it did. It calls no
// versioning API: the bucket needs none (R2 has none), and one that has it on keeps working.
// Safe to run any number of times.
func (s *Store) EnsureBucket(ctx context.Context) (bool, error) {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return false, nil
	}

	if !isNotFound(err) {
		return false, fmt.Errorf("head bucket %s: %w", s.bucket, err)
	}

	in := &s3.CreateBucketInput{Bucket: aws.String(s.bucket)}
	if s.region != "us-east-1" {
		in.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(s.region),
		}
	}

	if _, err := s.client.CreateBucket(ctx, in); err != nil {
		if _, owned := errors.AsType[*types.BucketAlreadyOwnedByYou](err); owned {
			return false, nil
		}

		return false, fmt.Errorf("create bucket %s: %w", s.bucket, err)
	}

	return true, nil
}

// Get implements ports.ObjectStore.
func (s *Store) Get(ctx context.Context, key string) (ports.Object, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		if isNotFound(err) {
			return ports.Object{}, fmt.Errorf("get s3://%s/%s: %w", s.bucket, key, ports.ErrNotFound)
		}

		return ports.Object{}, fmt.Errorf("get s3://%s/%s: %w", s.bucket, key, err)
	}
	defer func() { _ = out.Body.Close() }() // a read-only body: nothing to flush

	body, err := io.ReadAll(out.Body)
	if err != nil {
		return ports.Object{}, fmt.Errorf("read s3://%s/%s: %w", s.bucket, key, err)
	}

	return ports.Object{
		ObjectInfo: ports.ObjectInfo{Key: key, ETag: aws.ToString(out.ETag), Size: int64(len(body))},
		Body:       body,
	}, nil
}

// Put implements ports.ObjectStore.
func (s *Store) Put(ctx context.Context, key string, body []byte) (ports.ObjectInfo, error) {
	info, err := s.put(ctx, key, body, nil)
	if err != nil {
		return ports.ObjectInfo{}, fmt.Errorf("put s3://%s/%s: %w", s.bucket, key, err)
	}

	return info, nil
}

// PutIfAbsent implements ports.ObjectStore with a conditional PutObject (If-None-Match: *),
// which S3 and RustFS refuse with 412 when a current object is at key; HeadObject then reports
// that object. A server without conditional writes (501 Not Implemented) is remembered and gets
// HeadObject, then PutObject: two writers of one key can then both write it, which is harmless
// for the content-addressed keys this is for (both write the same bytes).
func (s *Store) PutIfAbsent(ctx context.Context, key string, body []byte) (ports.ObjectInfo, bool, error) {
	if !s.unconditional.Load() {
		info, err := s.put(ctx, key, body, aws.String("*"))

		switch status := httpStatus(err); {
		case err == nil:
			return info, true, nil
		case status == http.StatusPreconditionFailed || status == http.StatusConflict:
			// 409: S3's answer to a conditional write racing another one on the same key.
			return s.existing(ctx, key)
		case status == http.StatusNotImplemented:
			s.unconditional.Store(true)
		default:
			return ports.ObjectInfo{}, false, fmt.Errorf("put s3://%s/%s if absent: %w", s.bucket, key, err)
		}
	}

	info, created, err := s.existing(ctx, key)
	if err == nil || !errors.Is(err, ports.ErrNotFound) {
		return info, created, err
	}

	info, err = s.Put(ctx, key, body)

	return info, err == nil, err
}

// existing reports the current object at key, which PutIfAbsent did not write.
func (s *Store) existing(ctx context.Context, key string) (ports.ObjectInfo, bool, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		if isNotFound(err) {
			return ports.ObjectInfo{}, false, fmt.Errorf("head s3://%s/%s: %w", s.bucket, key, ports.ErrNotFound)
		}

		return ports.ObjectInfo{}, false, fmt.Errorf("head s3://%s/%s: %w", s.bucket, key, err)
	}

	return ports.ObjectInfo{Key: key, ETag: aws.ToString(out.ETag), Size: aws.ToInt64(out.ContentLength)}, false, nil
}

func (s *Store) put(ctx context.Context, key string, body []byte, ifNoneMatch *string) (ports.ObjectInfo, error) {
	out, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   aws.String(contentType(key)),
		IfNoneMatch:   ifNoneMatch,
	})
	if err != nil {
		return ports.ObjectInfo{}, err
	}

	return ports.ObjectInfo{Key: key, ETag: aws.ToString(out.ETag), Size: int64(len(body))}, nil
}

// httpStatus is the HTTP status of a failed S3 call, or 0.
func httpStatus(err error) int {
	if resp, ok := errors.AsType[*awshttp.ResponseError](err); ok {
		return resp.HTTPStatusCode()
	}

	return 0
}

// List implements ports.ObjectStore with ListObjectsV2, which lists current objects only, so a
// bucket that still has versioning on lists the same.
func (s *Store) List(ctx context.Context, prefix string) ([]ports.ObjectInfo, error) {
	var out []ports.ObjectInfo

	err := s.pages(ctx, prefix, func(objects []types.Object) error {
		for _, o := range objects {
			out = append(out, ports.ObjectInfo{Key: aws.ToString(o.Key), ETag: aws.ToString(o.ETag), Size: aws.ToInt64(o.Size)})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(out, func(a, b ports.ObjectInfo) int { return cmp.Compare(a.Key, b.Key) })

	return out, nil
}

// Delete implements ports.ObjectStore.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("delete s3://%s/%s: %w", s.bucket, key, err)
	}

	return nil
}

// DeletePrefix implements ports.ObjectStore: each list page (at most 1000 keys) is removed with
// one DeleteObjects call. A failure stops it; what is left is removed by the next call.
func (s *Store) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	if prefix == "" {
		return 0, fmt.Errorf("delete s3://%s/: an empty prefix names the whole bucket", s.bucket)
	}

	n := 0

	err := s.pages(ctx, prefix, func(objects []types.Object) error {
		if len(objects) == 0 {
			return nil
		}

		keys := make([]types.ObjectIdentifier, 0, len(objects))
		for _, o := range objects {
			keys = append(keys, types.ObjectIdentifier{Key: o.Key})
		}

		out, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &types.Delete{Objects: keys, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return fmt.Errorf("delete s3://%s/%s: %w", s.bucket, prefix, err)
		}

		if len(out.Errors) > 0 {
			e := out.Errors[0]

			return fmt.Errorf("delete s3://%s/%s: %d of %d keys failed, first %s: %s %s", s.bucket, prefix,
				len(out.Errors), len(keys), aws.ToString(e.Key), aws.ToString(e.Code), aws.ToString(e.Message))
		}

		n += len(keys)

		return nil
	})

	return n, err
}

// pages calls fn with each page of the objects under prefix.
func (s *Store) pages(ctx context.Context, prefix string, fn func([]types.Object) error) error {
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list s3://%s/%s: %w", s.bucket, prefix, err)
		}

		if err := fn(page.Contents); err != nil {
			return err
		}
	}

	return nil
}

// contentType labels text files as text, so S3 consoles and tools (and readers that only
// return bodies of text objects) treat definitions as the text they are.
func contentType(key string) string {
	switch strings.ToLower(path.Ext(key)) {
	case ".yaml", ".yml":
		return "text/yaml; charset=utf-8"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".json":
		return "application/json"
	case ".jsonl":
		return "application/jsonl"
	case ".txt", ".patch", ".diff":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// isNotFound matches a missing bucket or key, and, on a bucket that has versioning on, a key
// whose current version is a delete marker (S3 answers both with 404).
func isNotFound(err error) bool {
	if _, ok := errors.AsType[*types.NoSuchKey](err); ok {
		return true
	}

	if _, ok := errors.AsType[*types.NotFound](err); ok {
		return true
	}

	if _, ok := errors.AsType[*types.NoSuchBucket](err); ok {
		return true
	}

	api, ok := errors.AsType[smithy.APIError](err)

	return ok && slices.Contains([]string{"NoSuchKey", "NotFound", "NoSuchBucket"}, api.ErrorCode())
}
