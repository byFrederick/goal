// Package awssts mints a run's AWS credentials with AWS STS (spec 4 §5.4): a connection's stored
// access key signs one AssumeRole (with a role) or GetSessionToken (without), and only the
// short-lived credentials it returns reach the run's box.
package awssts

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/aws/smithy-go"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/egress"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.Minter = (*Minter)(nil)

// The connection fields a minter reads (the platform kinds aws and bedrock) and the values it
// mints, which the kinds render as {{minted.<name>}}: every one a secret, scrubbed like the stored
// ones; when they expire is Mint's own result.
const (
	fieldAccessKeyID     = "access_key_id"
	fieldSecretAccessKey = "secret_access_key"
	fieldRoleARN         = "role_arn"
	fieldExternalID      = "external_id"
	fieldRegion          = "region"

	mintedAccessKeyID     = "access_key_id"
	mintedSecretAccessKey = "secret_access_key"
	mintedSessionToken    = "session_token"
)

// STS's bounds on a credential's lifetime: AssumeRole's 12 hours is the most a role's
// MaxSessionDuration allows, and a role that sets none allows defaultRoleMax.
const (
	minDuration     = 15 * time.Minute
	maxRoleDuration = 12 * time.Hour
	defaultRoleMax  = time.Hour
	maxTokenSession = 36 * time.Hour
	// globalRegion signs for STS's global endpoint, for a connection without a region.
	globalRegion = "us-east-1"
	// requestTimeout bounds one mint, its retries included.
	requestTimeout = 30 * time.Second
)

// Config is how a Minter reaches STS.
type Config struct {
	// HTTPClient carries every request: goald's, which dials public addresses only
	// (egress.PublicDialer, threat row 4). Required.
	HTTPClient *http.Client
	// Endpoint replaces the regional endpoint STS has in the connection's region's partition
	// (https://sts.<region>.amazonaws.com): a stub STS in tests.
	Endpoint string
}

// Minter mints with AWS STS. It is safe for concurrent use.
type Minter struct {
	cfg Config
}

// New returns a Minter.
func New(cfg Config) *Minter { return &Minter{cfg: cfg} }

// Mint signs with values' stored access key in values' region. With a role_arn it assumes that
// role as goal-<run> (with external_id when set) for ttl, retrying once at a role's default
// maximum of an hour when the role allows less than asked; without one it asks for a session token
// of the key itself. STS's bounds clamp ttl: 15 minutes to 12 hours for a role, to 36 hours for a
// session token (an account's root key gets at most an hour).
func (m *Minter) Mint(ctx context.Context, values map[string]string, run domain.RunID, ttl time.Duration) (map[string]string, time.Time, error) {
	key, secret := values[fieldAccessKeyID], values[fieldSecretAccessKey]
	if key == "" || secret == "" {
		return nil, time.Time{}, fmt.Errorf("aws sts: the connection has no %s and %s: %w", fieldAccessKeyID, fieldSecretAccessKey, domain.ErrInvalid)
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	client := m.client(values[fieldRegion], key, secret)

	var (
		creds *types.Credentials
		err   error
	)

	if role := values[fieldRoleARN]; role != "" {
		creds, err = assumeRole(ctx, client, role, values[fieldExternalID], run, ttl)
	} else {
		creds, err = sessionToken(ctx, client, ttl)
	}

	if err != nil {
		return nil, time.Time{}, err
	}

	if creds == nil || creds.AccessKeyId == nil || creds.SecretAccessKey == nil || creds.SessionToken == nil || creds.Expiration == nil {
		return nil, time.Time{}, errors.New("aws sts: the response holds no credentials")
	}

	expires := creds.Expiration.UTC()

	return map[string]string{
		mintedAccessKeyID:     *creds.AccessKeyId,
		mintedSecretAccessKey: *creds.SecretAccessKey,
		mintedSessionToken:    *creds.SessionToken,
	}, expires, nil
}

// client is an STS client signing with the stored key in region. It reads nothing from goald's
// environment or files: only the connection's values sign.
func (m *Minter) client(region, key, secret string) *sts.Client {
	if region == "" {
		region = globalRegion
	}

	opts := sts.Options{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(key, secret, ""),
		HTTPClient:  m.cfg.HTTPClient,
		// Throttling and server errors are retried, an address the dialer refuses is not.
		Retryer: retry.NewStandard(func(o *retry.StandardOptions) {
			o.Retryables = append([]retry.IsErrorRetryable{retry.IsErrorRetryableFunc(func(err error) aws.Ternary {
				if errors.Is(err, egress.ErrNotPublic) {
					return aws.FalseTernary
				}

				return aws.UnknownTernary
			})}, o.Retryables...)
		}),
	}

	if m.cfg.Endpoint != "" {
		opts.BaseEndpoint = aws.String(m.cfg.Endpoint)
	}

	return sts.New(opts)
}

// assumeRole assumes role for ttl or, when the role allows less, for a role's default maximum.
func assumeRole(ctx context.Context, client *sts.Client, role, externalID string, run domain.RunID, ttl time.Duration) (*types.Credentials, error) {
	in := &sts.AssumeRoleInput{
		RoleArn:         aws.String(role),
		RoleSessionName: aws.String("goal-" + string(run)),
		DurationSeconds: seconds(ttl, maxRoleDuration),
	}

	if externalID != "" {
		in.ExternalId = aws.String(externalID)
	}

	out, err := client.AssumeRole(ctx, in)
	if exceedsRoleMax(err) && *in.DurationSeconds > int32(defaultRoleMax/time.Second) {
		in.DurationSeconds = seconds(defaultRoleMax, maxRoleDuration)
		out, err = client.AssumeRole(ctx, in)
	}

	if err != nil {
		return nil, fmt.Errorf("aws sts AssumeRole: %w", err)
	}

	return out.Credentials, nil
}

// sessionToken asks for a session token of the stored key for ttl.
func sessionToken(ctx context.Context, client *sts.Client, ttl time.Duration) (*types.Credentials, error) {
	out, err := client.GetSessionToken(ctx, &sts.GetSessionTokenInput{DurationSeconds: seconds(ttl, maxTokenSession)})
	if err != nil {
		return nil, fmt.Errorf("aws sts GetSessionToken: %w", err)
	}

	return out.Credentials, nil
}

// seconds is ttl in whole seconds within [minDuration, most].
func seconds(ttl, most time.Duration) *int32 {
	return aws.Int32(int32(min(max(ttl, minDuration), most).Seconds()))
}

// exceedsRoleMax reports STS's refusal of a duration beyond the role's MaxSessionDuration.
func exceedsRoleMax(err error) bool {
	var apiErr smithy.APIError

	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "ValidationError" &&
		strings.Contains(apiErr.ErrorMessage(), "MaxSessionDuration")
}
