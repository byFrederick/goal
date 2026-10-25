package awssts_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/adapters/awssts"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/egress"
)

const (
	storedKey    = "AKIASTOREDKEY0000001"
	storedSecret = "stored-secret-never-in-a-box"
	role         = "arn:aws:iam::123456789012:role/agents"
	run          = domain.RunID("0b8f7a52-5c43-4c9b-9d0e-2f1f3e2a8c11")
	expiration   = "2026-10-24T14:15:00Z"
)

// call is one request the stub STS received.
type call struct {
	host string
	form url.Values
	// signer is the access key id and credential scope of the request's SigV4 signature.
	signer string
}

// stub is an STS endpoint: it answers every request with reply's status and body, in order (the
// last one repeats), and records the requests.
type stub struct {
	mu      sync.Mutex
	calls   []call
	replies []reply
}

type reply struct {
	status int
	body   string
}

// answer records r, sent to host, and returns its reply.
func (s *stub) answer(r *http.Request, host string) (reply, error) {
	if err := r.ParseForm(); err != nil {
		return reply{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, call{host: host, form: r.PostForm, signer: signer(r.Header.Get("Authorization"))})

	return s.replies[min(len(s.calls), len(s.replies))-1], nil
}

// RoundTrip makes the stub a client's transport: every request reaches it, whatever its host.
func (s *stub) RoundTrip(r *http.Request) (*http.Response, error) {
	rep, err := s.answer(r, r.URL.Host)
	if err != nil {
		return nil, err
	}

	return &http.Response{
		StatusCode: rep.status, Body: io.NopCloser(strings.NewReader(rep.body)), Request: r,
		Header: http.Header{"Content-Type": {"text/xml"}},
	}, nil
}

// ServeHTTP makes the stub a server, for an endpoint override.
func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rep, err := s.answer(r, r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(rep.status)
	_, _ = io.WriteString(w, rep.body)
}

func (s *stub) recorded() []call {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]call(nil), s.calls...)
}

// signer is "<access key id>/<date>/<region>/<service>/aws4_request" without the date.
func signer(auth string) string {
	_, cred, ok := strings.Cut(auth, "Credential=")
	if !ok {
		return ""
	}

	cred, _, _ = strings.Cut(cred, ",")
	parts := strings.Split(cred, "/")

	if len(parts) != 5 {
		return cred
	}

	return strings.Join([]string{parts[0], parts[2], parts[3]}, "/")
}

func credentialsXML(action string) string {
	return fmt.Sprintf(`<%[1]sResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <%[1]sResult>
    <Credentials>
      <AccessKeyId>ASIAMINTED0000000001</AccessKeyId>
      <SecretAccessKey>minted-secret</SecretAccessKey>
      <SessionToken>minted-session-token</SessionToken>
      <Expiration>%[2]s</Expiration>
    </Credentials>
  </%[1]sResult>
  <ResponseMetadata><RequestId>req-1</RequestId></ResponseMetadata>
</%[1]sResponse>`, action, expiration)
}

func errorXML(code, message string) string {
	return fmt.Sprintf(`<ErrorResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <Error><Type>Sender</Type><Code>%s</Code><Message>%s</Message></Error>
  <RequestId>req-2</RequestId>
</ErrorResponse>`, code, message)
}

func stored(more map[string]string) map[string]string {
	v := map[string]string{"access_key_id": storedKey, "secret_access_key": storedSecret, "region": "eu-west-1"}
	maps.Copy(v, more)

	return v
}

func wantMinted(t *testing.T, minted map[string]string, expires time.Time) {
	t.Helper()

	want := map[string]string{
		"access_key_id": "ASIAMINTED0000000001", "secret_access_key": "minted-secret", "session_token": "minted-session-token",
	}

	for k, v := range want {
		if minted[k] != v {
			t.Errorf("minted[%s] = %q, want %q", k, minted[k], v)
		}
	}

	if len(minted) != len(want) {
		t.Errorf("minted = %v, want exactly %v", minted, want)
	}

	if at, _ := time.Parse(time.RFC3339, expiration); !expires.Equal(at) {
		t.Errorf("expires = %s, want %s", expires, at)
	}
}

func TestMinter_Mint_GetSessionTokenWithoutRole(t *testing.T) {
	t.Parallel()

	s := &stub{replies: []reply{{http.StatusOK, credentialsXML("GetSessionToken")}}}
	m := awssts.New(awssts.Config{HTTPClient: &http.Client{Transport: s}})

	minted, expires, err := m.Mint(t.Context(), stored(nil), run, 2*time.Hour+15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	wantMinted(t, minted, expires)

	calls := s.recorded()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}

	c := calls[0]
	if c.form.Get("Action") != "GetSessionToken" || c.form.Get("DurationSeconds") != "8100" || c.form.Has("RoleArn") {
		t.Errorf("form = %v, want GetSessionToken for 8100 seconds", c.form)
	}

	// The connection's region picks STS's regional endpoint and the signature's scope; the stored
	// key signs.
	if c.host != "sts.eu-west-1.amazonaws.com" {
		t.Errorf("host = %s, want sts.eu-west-1.amazonaws.com", c.host)
	}

	if c.signer != storedKey+"/eu-west-1/sts" {
		t.Errorf("signed by %q, want %s/eu-west-1/sts", c.signer, storedKey)
	}
}

func TestMinter_Mint_AssumeRoleWithRole(t *testing.T) {
	t.Parallel()

	s := &stub{replies: []reply{{http.StatusOK, credentialsXML("AssumeRole")}}}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	// An endpoint override on loopback (this stub) is dialed with a plain client; goald's own
	// client dials public addresses only.
	m := awssts.New(awssts.Config{HTTPClient: srv.Client(), Endpoint: srv.URL})

	values := stored(map[string]string{"role_arn": role, "external_id": "goal-acme"})

	minted, expires, err := m.Mint(t.Context(), values, run, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	wantMinted(t, minted, expires)

	calls := s.recorded()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}

	f := calls[0].form
	want := map[string]string{
		"Action": "AssumeRole", "RoleArn": role, "RoleSessionName": "goal-" + string(run),
		"ExternalId": "goal-acme", "DurationSeconds": "3600",
	}

	for k, v := range want {
		if f.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, f.Get(k), v)
		}
	}

	if calls[0].signer != storedKey+"/eu-west-1/sts" {
		t.Errorf("signed by %q, want the stored key in eu-west-1", calls[0].signer)
	}
}

func TestMinter_Mint_AssumeRoleCappedByTheRolesMaximum(t *testing.T) {
	t.Parallel()

	s := &stub{replies: []reply{
		{http.StatusBadRequest, errorXML("ValidationError", "The requested DurationSeconds exceeds the MaxSessionDuration set for this role.")},
		{http.StatusOK, credentialsXML("AssumeRole")},
	}}
	m := awssts.New(awssts.Config{HTTPClient: &http.Client{Transport: s}})

	_, _, err := m.Mint(t.Context(), stored(map[string]string{"role_arn": role}), run, 2*time.Hour+15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	calls := s.recorded()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2 (the asked duration, then a role's default maximum)", len(calls))
	}

	if got := []string{calls[0].form.Get("DurationSeconds"), calls[1].form.Get("DurationSeconds")}; got[0] != "8100" || got[1] != "3600" {
		t.Errorf("durations = %v, want [8100 3600]", got)
	}
}

func TestMinter_Mint_DurationWithinTheProvidersBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		role   string
		ttl    time.Duration
		action string
		want   string
	}{
		{name: "session token at least 15 minutes", ttl: time.Minute, action: "GetSessionToken", want: "900"},
		{name: "session token at most 36 hours", ttl: 48 * time.Hour, action: "GetSessionToken", want: "129600"},
		{name: "role at least 15 minutes", role: role, ttl: time.Second, action: "AssumeRole", want: "900"},
		{name: "role at most 12 hours", role: role, ttl: 24 * time.Hour, action: "AssumeRole", want: "43200"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &stub{replies: []reply{{http.StatusOK, credentialsXML(tt.action)}}}
			m := awssts.New(awssts.Config{HTTPClient: &http.Client{Transport: s}})

			values := stored(nil)
			if tt.role != "" {
				values["role_arn"] = tt.role
			}

			if _, _, err := m.Mint(t.Context(), values, run, tt.ttl); err != nil {
				t.Fatal(err)
			}

			if got := s.recorded()[0].form.Get("DurationSeconds"); got != tt.want {
				t.Errorf("DurationSeconds = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMinter_Mint_EndpointOfTheRegionsPartition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, region, host string
	}{
		{name: "aws", region: "us-west-2", host: "sts.us-west-2.amazonaws.com"},
		{name: "china", region: "cn-north-1", host: "sts.cn-north-1.amazonaws.com.cn"},
		{name: "no region is the global endpoint's", region: "", host: "sts.us-east-1.amazonaws.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &stub{replies: []reply{{http.StatusOK, credentialsXML("GetSessionToken")}}}
			m := awssts.New(awssts.Config{HTTPClient: &http.Client{Transport: s}})

			values := stored(nil)
			values["region"] = tt.region

			if _, _, err := m.Mint(t.Context(), values, run, time.Hour); err != nil {
				t.Fatal(err)
			}

			if got := s.recorded()[0].host; got != tt.host {
				t.Errorf("host = %s, want %s", got, tt.host)
			}
		})
	}
}

func TestMinter_Mint_RefusalNamesTheCodeAndNoValue(t *testing.T) {
	t.Parallel()

	s := &stub{replies: []reply{{http.StatusForbidden, errorXML("AccessDenied", "User is not authorized to perform: sts:AssumeRole")}}}
	m := awssts.New(awssts.Config{HTTPClient: &http.Client{Transport: s}})

	_, _, err := m.Mint(t.Context(), stored(map[string]string{"role_arn": role}), run, time.Hour)
	if err == nil {
		t.Fatal("Mint succeeded, want AccessDenied")
	}

	if !strings.Contains(err.Error(), "AccessDenied") {
		t.Errorf("error %q does not name AccessDenied", err)
	}

	for _, v := range []string{storedKey, storedSecret} {
		if strings.Contains(err.Error(), v) {
			t.Errorf("error %q holds a stored value", err)
		}
	}

	if n := len(s.recorded()); n != 1 {
		t.Errorf("calls = %d, want 1 (a refusal is not retried)", n)
	}
}

func TestMinter_Mint_WithoutAKeyAsksNothing(t *testing.T) {
	t.Parallel()

	s := &stub{replies: []reply{{http.StatusOK, credentialsXML("GetSessionToken")}}}
	m := awssts.New(awssts.Config{HTTPClient: &http.Client{Transport: s}})

	_, _, err := m.Mint(t.Context(), map[string]string{"access_key_id": storedKey, "region": "eu-west-1"}, run, time.Hour)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("err = %v, want domain.ErrInvalid", err)
	}

	if n := len(s.recorded()); n != 0 {
		t.Errorf("calls = %d, want none", n)
	}
}

func TestMinter_Mint_PublicDialerRefusesAPrivateEndpoint(t *testing.T) {
	t.Parallel()

	s := &stub{replies: []reply{{http.StatusOK, credentialsXML("GetSessionToken")}}}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	var dials atomic.Int32

	dialer := egress.PublicDialer{}
	public := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)

		return dialer.DialContext(ctx, network, addr)
	}}}
	m := awssts.New(awssts.Config{HTTPClient: public, Endpoint: srv.URL})

	_, _, err := m.Mint(t.Context(), stored(nil), run, time.Hour)
	if !errors.Is(err, egress.ErrNotPublic) {
		t.Errorf("err = %v, want egress.ErrNotPublic", err)
	}

	if n := dials.Load(); n != 1 {
		t.Errorf("dials = %d, want 1 (a refused address is not retried)", n)
	}

	if n := len(s.recorded()); n != 0 {
		t.Errorf("the loopback endpoint got %d requests, want none", n)
	}
}
