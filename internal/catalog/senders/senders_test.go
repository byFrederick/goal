package senders_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/catalog/senders"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The values the tests sign with; obviously fake.
const (
	current  = "test-signing-secret"
	previous = "test-previous-secret"
	// whsec values: the base64 of the bytes "test-standard-key" and "test-standard-old".
	whsecCurrent  = "whsec_dGVzdC1zdGFuZGFyZC1rZXk="
	whsecPrevious = "whsec_dGVzdC1zdGFuZGFyZC1vbGQ="
)

// The cases every sender's table has.
const (
	caseGood           = "good"
	caseWrong          = "wrong"
	caseMissing        = "missing"
	casePreviousKept   = "previous within 24h"
	casePreviousLapsed = "previous after 24h"
	caseNoID           = "no id hashes the body"
)

// Ids and events the tables use.
const (
	gitlabID   = "u-1"
	standardID = "msg_1"
	// standardIDHeader is Standard Webhooks' id header.
	standardIDHeader = "webhook-id"
	sentryEvent      = "issue"
)

var now = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func load(t *testing.T) *senders.Catalog {
	t.Helper()

	c, err := senders.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return c
}

func mac(key, signed []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(signed)

	return m.Sum(nil)
}

func hexMAC(key string, body []byte) string { return hex.EncodeToString(mac([]byte(key), body)) }

func standardMAC(t *testing.T, key, id, ts string, body []byte) string {
	t.Helper()

	k, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(key, "whsec_"))
	if err != nil {
		t.Fatal(err)
	}

	return base64.StdEncoding.EncodeToString(mac(k, append([]byte(id+"."+ts+"."), body...)))
}

func bodyHash(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// keyOf is a signing secret with its previous value kept until the instant given (nil: none).
func keyOf(cur, prev string, until *time.Time) domain.SigningSecret {
	if until == nil {
		return domain.SigningSecret{Current: cur}
	}

	return domain.SigningSecret{Current: cur, Previous: prev, PreviousUntil: until}
}

func at(d time.Duration) *time.Time {
	t := now.Add(d)
	return &t
}

// check is one delivery to a sender, with what Check should make of it.
type check struct {
	name    string
	headers map[string][]string
	body    string
	key     domain.SigningSecret
	token   domain.TokenSettings
	want    domain.CheckedDelivery
	refused bool
}

func run(t *testing.T, sender domain.WebhookSender, cases []check) {
	t.Helper()

	c := load(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := c.Check(sender, tc.token, domain.InboundDelivery{Headers: tc.headers, Body: []byte(tc.body), At: now}, tc.key)

			switch {
			case tc.refused && !errors.Is(err, domain.ErrSignature):
				t.Errorf("Check = %+v, %v; want ErrSignature", got, err)
			case !tc.refused && err != nil:
				t.Errorf("Check = %v; want it to pass", err)
			case !tc.refused && got != tc.want:
				t.Errorf("Check = %+v; want %+v", got, tc.want)
			}

			if err != nil {
				for _, secret := range []string{current, previous, whsecCurrent, whsecPrevious} {
					if strings.Contains(err.Error(), secret) {
						t.Errorf("the error %q holds a secret", err)
					}
				}
			}
		})
	}
}

// Every sender of domain.WebhookSenders has an entry, with an event table for the next part to
// fill.
func TestSenders_EverySenderHasAnEntry(t *testing.T) {
	t.Parallel()

	c := load(t)

	for _, s := range domain.WebhookSenders {
		e, ok := c.Entry(s)
		if !ok {
			t.Errorf("no entry for %s", s)
			continue
		}

		if e.Spec.Events == nil {
			t.Errorf("%s has no events table", s)
		}
	}
}

// An entry with an unknown key, scheme or encoding is refused.
func TestSenders_ParseRefusesBadEntries(t *testing.T) {
	t.Parallel()

	head := "apiVersion: goal/v1\nkind: Sender\nmetadata: {name: github}\nspec:\n"

	for name, spec := range map[string]string{ //nolint:gosec // catalog entries, not credentials
		"unknown key":     "  scheme: hmac\n  signed: body\n  secret: text\n  signature: {header: X, encoding: hex}\n  extra: 1\n",
		"unknown scheme":  "  scheme: rsa\n  signature: {header: X}\n",
		"bad encoding":    "  scheme: hmac\n  signed: body\n  secret: text\n  signature: {header: X, encoding: base32}\n",
		"no header":       "  scheme: token\n  signature: {prefix: x}\n",
		"token signs":     "  scheme: token\n  signed: body\n  signature: {header: X}\n",
		"timestamp alone": "  scheme: hmac\n  signed: id.timestamp.body\n  secret: whsec\n  signature: {header: X, encoding: base64}\n",
	} {
		if _, err := senders.Parse([]byte(head + spec)); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Parse(%s) = %v; want ErrInvalid", name, err)
		}
	}

	if _, err := senders.Parse([]byte("apiVersion: goal/v1\nkind: Sender\nmetadata: {name: acme}\nspec:\n  scheme: token\n  signature: {header: X}\n")); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("Parse(unknown sender) = %v; want ErrInvalid", err)
	}
}

func TestSenders_GitHub(t *testing.T) {
	t.Parallel()

	body := `{"zen":"Keep it logically awesome."}`
	sig := "sha256=" + hexMAC(current, []byte(body))
	old := "sha256=" + hexMAC(previous, []byte(body))
	h := func(sig string) map[string][]string {
		return map[string][]string{"x-hub-signature-256": {sig}, "x-github-event": {"ping"}, "x-github-delivery": {"d-1"}}
	}
	want := domain.CheckedDelivery{ID: "d-1", Event: "ping"}

	run(t, domain.SenderGitHub, []check{
		{name: caseGood, headers: h(sig), body: body, key: keyOf(current, "", nil), want: want},
		{name: caseWrong, headers: h("sha256=" + hexMAC("other", []byte(body))), body: body, key: keyOf(current, "", nil), refused: true},
		{name: "body changed", headers: h(sig), body: body + " ", key: keyOf(current, "", nil), refused: true},
		{name: caseMissing, headers: map[string][]string{"x-github-event": {"push"}}, body: body, key: keyOf(current, "", nil), refused: true},
		{name: "no prefix", headers: h(hexMAC(current, []byte(body))), body: body, key: keyOf(current, "", nil), refused: true},
		{name: casePreviousKept, headers: h(old), body: body, key: keyOf(current, previous, at(time.Hour)), want: want},
		{name: casePreviousLapsed, headers: h(old), body: body, key: keyOf(current, previous, at(-time.Second)), refused: true},
		{
			name: caseNoID, body: body, key: keyOf(current, "", nil),
			headers: map[string][]string{"x-hub-signature-256": {sig}, "x-github-event": {"push"}},
			want:    domain.CheckedDelivery{ID: bodyHash([]byte(body)), Event: "push"},
		},
	})
}

func TestSenders_GitLab(t *testing.T) {
	t.Parallel()

	body := `{"object_kind":"pipeline","object_attributes":{"id":7}}`
	h := func(token string) map[string][]string {
		return map[string][]string{"x-gitlab-token": {token}, "x-gitlab-event-uuid": {gitlabID}}
	}
	want := domain.CheckedDelivery{ID: gitlabID, Event: "pipeline"}

	run(t, domain.SenderGitLab, []check{
		{name: caseGood, headers: h(current), body: body, key: keyOf(current, "", nil), want: want},
		{name: caseWrong, headers: h("not-the-token"), body: body, key: keyOf(current, "", nil), refused: true},
		{name: caseMissing, headers: map[string][]string{"x-gitlab-event-uuid": {gitlabID}}, body: body, key: keyOf(current, "", nil), refused: true},
		{name: casePreviousKept, headers: h(previous), body: body, key: keyOf(current, previous, at(time.Minute)), want: want},
		{name: casePreviousLapsed, headers: h(previous), body: body, key: keyOf(current, previous, at(-time.Minute)), refused: true},
		{
			name: caseNoID, body: body, key: keyOf(current, "", nil), headers: map[string][]string{"x-gitlab-token": {current}},
			want: domain.CheckedDelivery{ID: bodyHash([]byte(body)), Event: "pipeline"},
		},
		{
			name: "not JSON has no event", body: "not json", key: keyOf(current, "", nil), headers: h(current),
			want: domain.CheckedDelivery{ID: gitlabID},
		},
	})
}

func TestSenders_Standard(t *testing.T) {
	t.Parallel()

	body := `{"type":"invoice.paid","data":{}}`
	ts := strconv.FormatInt(now.Unix(), 10)
	good := "v1," + standardMAC(t, whsecCurrent, standardID, ts, []byte(body))
	old := "v1," + standardMAC(t, whsecPrevious, standardID, ts, []byte(body))
	h := func(sig, ts string) map[string][]string {
		return map[string][]string{"webhook-signature": {sig}, standardIDHeader: {standardID}, "webhook-timestamp": {ts}}
	}
	key := keyOf(whsecCurrent, "", nil)
	want := domain.CheckedDelivery{ID: standardID, Event: "invoice.paid"}
	stale := strconv.FormatInt(now.Add(-6*time.Minute).Unix(), 10)

	run(t, domain.SenderStandard, []check{
		{name: caseGood, headers: h(good, ts), body: body, key: key, want: want},
		{name: caseWrong, headers: h("v1,"+standardMAC(t, whsecPrevious, standardID, ts, []byte(body)), ts), body: body, key: key, refused: true},
		{name: caseMissing, headers: map[string][]string{standardIDHeader: {standardID}, "webhook-timestamp": {ts}}, body: body, key: key, refused: true},
		{name: "several values, one good", headers: h("v1,AAAA v1a,xyz "+good, ts), body: body, key: key, want: want},
		{name: "several values, none good", headers: h("v1,AAAA v2,"+good[3:], ts), body: body, key: key, refused: true},
		{name: "timestamp 6 minutes off", headers: h("v1,"+standardMAC(t, whsecCurrent, standardID, stale, []byte(body)), stale), body: body, key: key, refused: true},
		{name: "timestamp missing", headers: map[string][]string{"webhook-signature": {good}, standardIDHeader: {standardID}}, body: body, key: key, refused: true},
		{name: "id changed", headers: map[string][]string{"webhook-signature": {good}, standardIDHeader: {"msg_2"}, "webhook-timestamp": {ts}}, body: body, key: key, refused: true},
		{name: casePreviousKept, headers: h(old, ts), body: body, key: keyOf(whsecCurrent, whsecPrevious, at(time.Hour)), want: want},
		{name: casePreviousLapsed, headers: h(old, ts), body: body, key: keyOf(whsecCurrent, whsecPrevious, at(-time.Hour)), refused: true},
		{
			name: "event field when no type", body: `{"event":"deploy.done"}`, key: key,
			headers: h("v1,"+standardMAC(t, whsecCurrent, standardID, ts, []byte(`{"event":"deploy.done"}`)), ts),
			want:    domain.CheckedDelivery{ID: standardID, Event: "deploy.done"},
		},
	})
}

func TestSenders_PagerDuty(t *testing.T) {
	t.Parallel()

	body := `{"event":{"id":"01DEN","event_type":"incident.triggered"}}`
	good := "v1=" + hexMAC(current, []byte(body))
	h := func(sig string) map[string][]string { return map[string][]string{"x-pagerduty-signature": {sig}} }
	want := domain.CheckedDelivery{ID: "01DEN", Event: "incident.triggered"}

	run(t, domain.SenderPagerDuty, []check{
		{name: caseGood, headers: h(good), body: body, key: keyOf(current, "", nil), want: want},
		{name: caseWrong, headers: h("v1=" + hexMAC("other", []byte(body))), body: body, key: keyOf(current, "", nil), refused: true},
		{name: caseMissing, headers: map[string][]string{}, body: body, key: keyOf(current, "", nil), refused: true},
		{name: "several values, one good", headers: h("v1=00ff," + good), body: body, key: keyOf(current, "", nil), want: want},
		{name: casePreviousKept, headers: h("v1=" + hexMAC(previous, []byte(body))), body: body, key: keyOf(current, previous, at(time.Hour)), want: want},
		{name: casePreviousLapsed, headers: h("v1=" + hexMAC(previous, []byte(body))), body: body, key: keyOf(current, previous, at(-time.Hour)), refused: true},
		{
			name: caseNoID, body: `{"event":{"event_type":"incident.resolved"}}`, key: keyOf(current, "", nil),
			headers: h("v1=" + hexMAC(current, []byte(`{"event":{"event_type":"incident.resolved"}}`))),
			want:    domain.CheckedDelivery{ID: bodyHash([]byte(`{"event":{"event_type":"incident.resolved"}}`)), Event: "incident.resolved"},
		},
	})
}

func TestSenders_Sentry(t *testing.T) {
	t.Parallel()

	body := `{"action":"created","data":{sentryEvent:{"id":"1"}}}`
	h := func(sig string) map[string][]string {
		return map[string][]string{"sentry-hook-signature": {sig}, "sentry-hook-resource": {sentryEvent}, "request-id": {"r-1"}}
	}
	want := domain.CheckedDelivery{ID: "r-1", Event: sentryEvent}

	run(t, domain.SenderSentry, []check{
		{name: caseGood, headers: h(hexMAC(current, []byte(body))), body: body, key: keyOf(current, "", nil), want: want},
		{name: caseWrong, headers: h(hexMAC("other", []byte(body))), body: body, key: keyOf(current, "", nil), refused: true},
		{name: caseMissing, headers: map[string][]string{"sentry-hook-resource": {sentryEvent}}, body: body, key: keyOf(current, "", nil), refused: true},
		{name: casePreviousKept, headers: h(hexMAC(previous, []byte(body))), body: body, key: keyOf(current, previous, at(time.Hour)), want: want},
		{name: casePreviousLapsed, headers: h(hexMAC(previous, []byte(body))), body: body, key: keyOf(current, previous, at(-time.Hour)), refused: true},
		{
			name: caseNoID, body: body, key: keyOf(current, "", nil),
			headers: map[string][]string{"sentry-hook-signature": {hexMAC(current, []byte(body))}, "sentry-hook-resource": {sentryEvent}},
			want:    domain.CheckedDelivery{ID: bodyHash([]byte(body)), Event: sentryEvent},
		},
	})
}

func TestSenders_Token(t *testing.T) {
	t.Parallel()

	body := `{"alert":{"id":42,"type":"monitor.alert"}}`
	paths := domain.TokenSettings{EventPath: "alert.type", IDPath: "alert.id"}
	own := domain.TokenSettings{Header: "X-Acme-Token", EventPath: "alert.type"}
	bearer := func(v string) map[string][]string { return map[string][]string{"authorization": {v}} }

	run(t, domain.SenderToken, []check{
		{name: "good bearer", headers: bearer("Bearer " + current), body: body, key: keyOf(current, "", nil), token: paths, want: domain.CheckedDelivery{ID: "42", Event: "monitor.alert"}},
		{name: caseWrong, headers: bearer("Bearer nope"), body: body, key: keyOf(current, "", nil), token: paths, refused: true},
		{name: "no bearer prefix", headers: bearer(current), body: body, key: keyOf(current, "", nil), token: paths, refused: true},
		{name: caseMissing, headers: map[string][]string{}, body: body, key: keyOf(current, "", nil), token: paths, refused: true},
		{name: casePreviousKept, headers: bearer("Bearer " + previous), body: body, key: keyOf(current, previous, at(time.Hour)), token: paths, want: domain.CheckedDelivery{ID: "42", Event: "monitor.alert"}},
		{name: casePreviousLapsed, headers: bearer("Bearer " + previous), body: body, key: keyOf(current, previous, at(-time.Hour)), token: paths, refused: true},
		{
			name: "own header, no id path hashes the body", body: body, key: keyOf(current, "", nil), token: own,
			headers: map[string][]string{"x-acme-token": {current}},
			want:    domain.CheckedDelivery{ID: bodyHash([]byte(body)), Event: "monitor.alert"},
		},
		{name: "own header ignores Authorization", body: body, key: keyOf(current, "", nil), token: own, headers: bearer("Bearer " + current), refused: true},
	})
}

// An id over domain.MaxDeliveryID characters is replaced by its hash, so the ledger can keep it.
func TestSenders_LongIDHashed(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", domain.MaxDeliveryID+1)
	body := `{}`

	run(t, domain.SenderSentry, []check{{
		name: "long id", body: body, key: keyOf(current, "", nil),
		headers: map[string][]string{"sentry-hook-signature": {hexMAC(current, []byte(body))}, "request-id": {long}},
		want:    domain.CheckedDelivery{ID: bodyHash([]byte(long))},
	}})
}
