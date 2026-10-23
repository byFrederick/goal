// Package s3store implements ports.ObjectStore on Amazon S3 or any S3-compatible server
// (RustFS on a laptop) with the AWS SDK for Go v2.
//
// Credentials come from the standard AWS chain (environment, SSO profile, ECS task role). A
// local S3-compatible server instead takes the static keys in Config, which are never used
// unless both are set.
package s3store

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/byFrederick/goal/internal/ports"
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

// EnsureBucket creates the bucket when it is missing and turns versioning on. It reports
// whether it created the bucket. Safe to run any number of times.
func (s *Store) EnsureBucket(ctx context.Context) (bool, error) {
	created, err := s.createIfMissing(ctx)
	if err != nil {
		return false, err
	}

	_, err = s.client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket:                  aws.String(s.bucket),
		VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled},
	})
	if err != nil {
		return created, fmt.Errorf("enable versioning on %s: %w", s.bucket, err)
	}

	return created, nil
}

func (s *Store) createIfMissing(ctx context.Context) (bool, error) {
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
func (s *Store) Get(ctx context.Context, key, version string) (ports.Object, error) {
	in := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}
	if version != "" {
		in.VersionId = aws.String(version)
	}

	out, err := s.client.GetObject(ctx, in)
	if err != nil {
		if isNotFound(err) || (version != "" && isBadVersion(err)) {
			return ports.Object{}, fmt.Errorf("get s3://%s/%s %s: %w", s.bucket, key, version, ports.ErrNotFound)
		}

		return ports.Object{}, fmt.Errorf("get s3://%s/%s %s: %w", s.bucket, key, version, err)
	}
	defer func() { _ = out.Body.Close() }() // a read-only body: nothing to flush

	body, err := io.ReadAll(out.Body)
	if err != nil {
		return ports.Object{}, fmt.Errorf("read s3://%s/%s: %w", s.bucket, key, err)
	}

	return ports.Object{
		ObjectInfo: ports.ObjectInfo{Key: key, Version: aws.ToString(out.VersionId), Size: int64(len(body))},
		Body:       body,
	}, nil
}

// Put implements ports.ObjectStore.
func (s *Store) Put(ctx context.Context, key string, body []byte) (ports.ObjectInfo, error) {
	out, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   aws.String(contentType(key)),
	})
	if err != nil {
		return ports.ObjectInfo{}, fmt.Errorf("put s3://%s/%s: %w", s.bucket, key, err)
	}

	return ports.ObjectInfo{Key: key, Version: aws.ToString(out.VersionId), Size: int64(len(body))}, nil
}

// List implements ports.ObjectStore. It lists versions so each result carries the id of the
// current one; keys whose current version is a delete marker are absent.
func (s *Store) List(ctx context.Context, prefix string) ([]ports.ObjectInfo, error) {
	var out []ports.ObjectInfo

	pages := s3.NewListObjectVersionsPaginator(s.client, &s3.ListObjectVersionsInput{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list s3://%s/%s: %w", s.bucket, prefix, err)
		}

		for _, v := range page.Versions {
			if aws.ToBool(v.IsLatest) {
				out = append(out, ports.ObjectInfo{
					Key: aws.ToString(v.Key), Version: aws.ToString(v.VersionId), Size: aws.ToInt64(v.Size),
				})
			}
		}
	}

	slices.SortFunc(out, func(a, b ports.ObjectInfo) int { return cmp.Compare(a.Key, b.Key) })

	return out, nil
}

// Delete implements ports.ObjectStore. On a versioned bucket it adds a delete marker.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("delete s3://%s/%s: %w", s.bucket, key, err)
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

// isNotFound matches a missing bucket, key or version, and a key whose current version is a
// delete marker (S3 answers both with 404).
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

	return ok && slices.Contains([]string{"NoSuchKey", "NotFound", "NoSuchBucket", "NoSuchVersion"}, api.ErrorCode())
}

// isBadVersion matches the errors S3 returns for a version id that does not exist (400
// InvalidArgument) or that is a delete marker (405 MethodNotAllowed).
func isBadVersion(err error) bool {
	api, ok := errors.AsType[smithy.APIError](err)

	return ok && slices.Contains([]string{"InvalidArgument", "MethodNotAllowed"}, api.ErrorCode())
}
