package api_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/catalog/senders"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// The values the receiver's tests sign with; obviously fake.
const (
	hookSecret   = "test-hook-secret"
	hookPrevious = "test-hook-previous"
	receiverHook = "github-acme"
	hookJSON     = "application/json"
)

// hookRig is a receiver over port fakes and the real sender catalog, with one organization webhook.
type hookRig struct {
	h       http.Handler
	intake  *porttest.Intake
	secrets *porttest.OrgSecrets
	clock   *porttest.Clock
	hook    domain.WebhookEndpoint
	names   *namesStub
	logs    *bytes.Buffer
}

// namesStub has the acme factory evaluate every delivery when named is true, as a loaded trigger
// would, skipped at once when skip is set.
type namesStub struct {
	named bool
	skip  string
}

func (n *namesStub) Evaluations(context.Context, domain.WebhookEndpoint, string, []byte) []domain.NewDeliveryEvaluation {
	if !n.named {
		return nil
	}

	return []domain.NewDeliveryEvaluation{{FactoryID: acmeID, Triggers: []string{"ci"}, Skip: n.skip}}
}

func newHookRig(t *testing.T, sender domain.WebhookSender, token domain.TokenSettings) *hookRig {
	t.Helper()

	key := porttest.NewMasterKeys().Key("k1")
	store := porttest.NewStore()
	store.AddFactory(testOrg, acmeID)

	r := &hookRig{
		intake: porttest.NewIntake(key, store), secrets: porttest.NewOrgSecrets(key, newTenancy(t), nil),
		clock: porttest.NewClock(time.Now()), names: &namesStub{named: true}, logs: &bytes.Buffer{},
	}
	r.intake.UseSecrets(r.secrets)

	ctx := domain.WithOrg(t.Context(), testOrg)
	if _, err := r.secrets.Create(ctx, domain.NewSecret{Name: domain.WebhookSecretName(receiverHook), Value: hookSecret}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	w, err := r.intake.CreateWebhook(ctx, domain.NewWebhook{Name: receiverHook, Sender: sender, Secret: domain.WebhookSecretName(receiverHook), Token: token})
	if err != nil {
		t.Fatal(err)
	}

	if r.hook, err = r.intake.PatchWebhook(ctx, w.ID, domain.WebhookPatch{Open: []domain.WebhookOpening{{FactoryID: acmeID}}}); err != nil {
		t.Fatal(err)
	}

	catalog, err := senders.Load()
	if err != nil {
		t.Fatal(err)
	}

	r.h = api.Receiver(api.ReceiverDeps{
		Intake: r.intake, Secrets: r.secrets, Senders: catalog, Names: r.names, Clock: r.clock,
		Logger: slog.New(slog.NewJSONHandler(r.logs, nil)),
	})

	return r
}

func (r *hookRig) ctx(t *testing.T) context.Context {
	t.Helper()

	return domain.WithOrg(t.Context(), testOrg)
}

// post posts body to path with headers, from addr.
func (r *hookRig) post(t *testing.T, path, body string, headers map[string]string, addr string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
	req.RemoteAddr = cmpOr(addr, "203.0.113.7:4000")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)

	return rec
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}

	return b
}

// github posts body to the webhook as GitHub does, signed with secret.
func (r *hookRig) github(t *testing.T, body, event, delivery, secret string) *httptest.ResponseRecorder {
	t.Helper()

	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))

	return r.post(t, api.HooksPath+string(r.hook.ID), body, map[string]string{
		"X-Hub-Signature-256": "sha256=" + hex.EncodeToString(m.Sum(nil)), "X-GitHub-Event": event, "X-GitHub-Delivery": delivery,
		"Content-Type": hookJSON,
	}, "")
}

func (r *hookRig) deliveries(t *testing.T) []domain.WebhookDelivery {
	t.Helper()

	ds, err := r.intake.ListDeliveries(r.ctx(t), r.hook.ID, domain.DeliveryCursor{}, 100)
	if err != nil {
		t.Fatal(err)
	}

	return ds
}

type receipt struct {
	ID      string `json:"id"`
	Receipt string `json:"receipt"`
}

func receiptOf(t *testing.T, rec *httptest.ResponseRecorder) receipt {
	t.Helper()

	var got receipt
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("answer %q: %v", rec.Body.String(), err)
	}

	return got
}

// A signed delivery is accepted with its id (202); its id again is a duplicate (200) that creates
// nothing and counts on the first row.
func TestHooks_AcceptedThenDuplicate(t *testing.T) {
	t.Parallel()

	r := newHookRig(t, domain.SenderGitHub, domain.TokenSettings{})
	body := `{"action":"completed","check_run":{"name":"test"}}`

	rec := r.github(t, body, "check_run", "d-1", hookSecret)
	if rec.Code != http.StatusAccepted || receiptOf(t, rec) != (receipt{ID: "d-1", Receipt: "accepted"}) {
		t.Fatalf("POST = %d %s; want 202 with d-1", rec.Code, rec.Body)
	}

	rec = r.github(t, body, "check_run", "d-1", hookSecret)
	if rec.Code != http.StatusOK || receiptOf(t, rec) != (receipt{ID: "d-1", Receipt: "duplicate"}) {
		t.Fatalf("POST again = %d %s; want 200, a duplicate", rec.Code, rec.Body)
	}

	ds := r.deliveries(t)
	if len(ds) != 1 || ds[0].DeliveryID != "d-1" || ds[0].Event != "check_run" || ds[0].Duplicates != 1 || string(ds[0].Body) != body {
		t.Fatalf("deliveries = %+v; want d-1 with its body and 1 duplicate", ds)
	}

	if _, ok := ds[0].Headers["x-hub-signature-256"]; ok {
		t.Errorf("the delivery keeps its signature header: %v", ds[0].Headers)
	}
}

// An accepted delivery a factory's trigger names gets that factory's evaluation with its row,
// pending or skipped at once; its duplicate adds none.
func TestReceiver_AddsEvaluations(t *testing.T) {
	t.Parallel()

	r := newHookRig(t, domain.SenderGitHub, domain.TokenSettings{})

	for i, skip := range []string{"", "acme/legacy is not one of acme's repositories"} {
		r.names.skip = skip
		id := fmt.Sprintf("d-%d", i)

		if rec := r.github(t, `{}`, "check_run", id, hookSecret); rec.Code != http.StatusAccepted {
			t.Fatalf("POST = %d %s; want 202", rec.Code, rec.Body)
		}

		if rec := r.github(t, `{}`, "check_run", id, hookSecret); rec.Code != http.StatusOK {
			t.Fatalf("POST again = %d %s; want 200", rec.Code, rec.Body)
		}
	}

	ds := r.deliveries(t)
	if len(ds) != 2 {
		t.Fatalf("deliveries = %+v; want two", ds)
	}

	for _, d := range ds {
		es, err := r.intake.DeliveryEvaluations(r.ctx(t), d.ID)
		if err != nil || len(es) != 1 || es[0].FactoryID != acmeID || es[0].Event != "check_run" {
			t.Fatalf("evaluations of %s = %+v, %v; want one for acme", d.DeliveryID, es, err)
		}

		want := domain.EvaluationPending
		if strings.HasSuffix(d.DeliveryID, "1") {
			want = domain.EvaluationDone
		}

		if es[0].State != want || (want == domain.EvaluationDone) != (es[0].Outcome == domain.EvaluationSkipped) {
			t.Errorf("evaluation of %s = %s %s; want %s", d.DeliveryID, es[0].State, es[0].Outcome, want)
		}
	}
}

// A delivery no trigger names is accepted and keeps only its ledger id and its day's count.
func TestHooks_UnnamedKeepsOnlyACount(t *testing.T) {
	t.Parallel()

	r := newHookRig(t, domain.SenderGitHub, domain.TokenSettings{})
	r.names.named = false

	if rec := r.github(t, `{}`, "issues", "d-1", hookSecret); rec.Code != http.StatusAccepted {
		t.Fatalf("POST = %d %s; want 202", rec.Code, rec.Body)
	}

	if ds := r.deliveries(t); len(ds) != 0 {
		t.Errorf("deliveries = %+v; want none", ds)
	}

	if got, err := r.intake.UnnamedDays(r.ctx(t), r.hook.ID, r.clock.Now().Add(-48*time.Hour)); err != nil || len(got) != 1 || got[0].Count != 1 {
		t.Errorf("UnnamedDays = %+v, %v; want one", got, err)
	}
}

// GitHub's ping is accepted and keeps its row with the reason the webhook shows, though no trigger
// names it.
func TestHooks_GitHubPing(t *testing.T) {
	t.Parallel()

	r := newHookRig(t, domain.SenderGitHub, domain.TokenSettings{})
	r.names.named = false

	if rec := r.github(t, `{"zen":"Design for failure."}`, "ping", "p-1", hookSecret); rec.Code != http.StatusAccepted {
		t.Fatalf("POST ping = %d %s; want 202", rec.Code, rec.Body)
	}

	if ds := r.deliveries(t); len(ds) != 1 || ds[0].Reason != "Received GitHub's ping" || ds[0].Receipt != domain.ReceiptAccepted {
		t.Errorf("deliveries = %+v; want the ping", ds)
	}
}

// A missing or wrong signature is 401 and is recorded as refused with metadata only; no answer,
// stored header or log line holds the secret or the signature.
func TestHooks_RefusedSignature(t *testing.T) {
	t.Parallel()

	r := newHookRig(t, domain.SenderGitHub, domain.TokenSettings{})
	body := `{"ref":"main"}`

	wrong := r.github(t, body, "push", "d-1", "not-the-secret")
	missing := r.post(t, api.HooksPath+string(r.hook.ID), body, map[string]string{"X-GitHub-Event": "push"}, "")

	for _, rec := range []*httptest.ResponseRecorder{wrong, missing} {
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("Content-Type") != problemType {
			t.Errorf("POST = %d %s; want 401 problem", rec.Code, rec.Body)
		}
	}

	ds := r.deliveries(t)
	if len(ds) != 2 {
		t.Fatalf("deliveries = %+v; want 2 refused", ds)
	}

	for _, d := range ds {
		if d.Receipt != domain.ReceiptRefused || d.Outcome != domain.DeliveryRefused || len(d.Body) != 0 || d.DeliveryID != "" || d.Reason == "" {
			t.Errorf("delivery = %+v; want refused, metadata only", d)
		}

		if _, ok := d.Headers["x-hub-signature-256"]; ok {
			t.Errorf("a refused delivery keeps its signature header: %v", d.Headers)
		}
	}

	r.expectNoLeak(t, body, wrong.Body.String()+missing.Body.String())

	// A signed delivery with the same id after the refusals is accepted: a refused one takes none.
	if rec := r.github(t, body, "push", "d-1", hookSecret); rec.Code != http.StatusAccepted {
		t.Errorf("POST signed = %d; want 202", rec.Code)
	}
}

// expectNoLeak checks that neither answers nor the log hold the secret or the wrong signature of
// body.
func (r *hookRig) expectNoLeak(t *testing.T, body, answers string) {
	t.Helper()

	m := hmac.New(sha256.New, []byte("not-the-secret"))
	m.Write([]byte(body))

	for _, leak := range []string{hookSecret, hex.EncodeToString(m.Sum(nil))} {
		if strings.Contains(answers+r.logs.String(), leak) {
			t.Errorf("an answer or the log holds %q", leak)
		}
	}
}

// For 24 hours after the secret's value is replaced, a delivery signed with the previous value
// passes too.
func TestHooks_PreviousSecretPasses(t *testing.T) {
	t.Parallel()

	r := newHookRig(t, domain.SenderGitHub, domain.TokenSettings{})
	value := hookPrevious

	if _, err := r.secrets.Patch(r.ctx(t), domain.WebhookSecretName(receiverHook), domain.SecretPatch{Replace: &value}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	for i, secret := range []string{hookSecret, hookPrevious} {
		if rec := r.github(t, `{}`, "push", "d-"+strconv.Itoa(i), secret); rec.Code != http.StatusAccepted {
			t.Errorf("POST signed with value %d = %d %s; want 202", i, rec.Code, rec.Body)
		}
	}
}

// An unknown id, an id that is no UUID, a disabled webhook and a factory's endpoint are 404;
// another method is 405.
func TestHooks_UnknownDisabledAndOtherMethods(t *testing.T) {
	t.Parallel()

	r := newHookRig(t, domain.SenderGitHub, domain.TokenSettings{})
	ctx := r.ctx(t)

	endpoint, err := r.intake.CreateEndpoint(ctx, domain.NewWebhookEndpoint{FactoryID: acmeID, Name: "alerts", Kind: domain.WebhookGitHub})
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"019a0000-0000-7000-8000-000000000000", "not-a-uuid", string(endpoint.Endpoint.ID)} {
		if rec := r.post(t, api.HooksPath+id, `{}`, nil, ""); rec.Code != http.StatusNotFound {
			t.Errorf("POST /hooks/%s = %d; want 404", id, rec.Code)
		}
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, api.HooksPath+string(r.hook.ID), nil)
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
		t.Errorf("GET = %d, Allow %q; want 405, POST", rec.Code, rec.Header().Get("Allow"))
	}

	off := false
	if _, err := r.intake.PatchWebhook(ctx, r.hook.ID, domain.WebhookPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}

	if rec := r.github(t, `{}`, "push", "d-1", hookSecret); rec.Code != http.StatusNotFound {
		t.Errorf("POST to a disabled webhook = %d; want 404", rec.Code)
	}
}

// A body over 1 MiB is 413, and nothing is recorded.
func TestHooks_TooLarge(t *testing.T) {
	t.Parallel()

	r := newHookRig(t, domain.SenderGitHub, domain.TokenSettings{})

	if rec := r.github(t, strings.Repeat("a", domain.MaxDeliveryBody+1), "push", "d-1", hookSecret); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("POST 1 MiB + 1 = %d; want 413", rec.Code)
	}

	if rec := r.github(t, strings.Repeat("a", domain.MaxDeliveryBody), "push", "d-2", hookSecret); rec.Code != http.StatusAccepted {
		t.Errorf("POST 1 MiB = %d; want 202", rec.Code)
	}

	if ds := r.deliveries(t); len(ds) != 1 || ds[0].DeliveryID != "d-2" {
		t.Errorf("deliveries = %d; want only d-2", len(ds))
	}
}

// Past 30 refused deliveries a minute the webhook answers 429 with Retry-After and records nothing
// more; a signed delivery still passes, and the next minute refusals are answered again.
func TestHooks_RefusedLimitedPerWebhook(t *testing.T) {
	t.Parallel()

	r := newHookRig(t, domain.SenderGitHub, domain.TokenSettings{})

	for i := range 30 {
		if rec := r.github(t, `{}`, "push", "x", "wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("refusal %d = %d; want 401", i+1, rec.Code)
		}
	}

	rec := r.github(t, `{}`, "push", "x", "wrong")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "60" {
		t.Errorf("refusal 31 = %d, Retry-After %q; want 429, 60", rec.Code, rec.Header().Get("Retry-After"))
	}

	if rec := r.github(t, `{}`, "push", "d-1", hookSecret); rec.Code != http.StatusAccepted {
		t.Errorf("signed after the limit = %d; want 202", rec.Code)
	}

	if ds := r.deliveries(t); len(ds) != 31 {
		t.Errorf("deliveries = %d; want 30 refused and the signed one", len(ds))
	}

	r.clock.Advance(time.Minute)

	if rec := r.github(t, `{}`, "push", "x", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Errorf("refusal the next minute = %d; want 401", rec.Code)
	}
}

// Past 60 requests a minute from one address to unknown webhooks the receiver answers 429; another
// address, and the next minute, are answered 404 again.
func TestHooks_UnknownLimitedPerAddress(t *testing.T) {
	t.Parallel()

	r := newHookRig(t, domain.SenderGitHub, domain.TokenSettings{})
	unknown := api.HooksPath + "019a0000-0000-7000-8000-000000000000"

	for i := range 60 {
		if rec := r.post(t, unknown, `{}`, nil, "203.0.113.9:1"); rec.Code != http.StatusNotFound {
			t.Fatalf("request %d = %d; want 404", i+1, rec.Code)
		}
	}

	rec := r.post(t, unknown, `{}`, nil, "203.0.113.9:2")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("request 61 = %d, Retry-After %q; want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}

	if rec := r.post(t, unknown, `{}`, nil, "198.51.100.4:1"); rec.Code != http.StatusNotFound {
		t.Errorf("another address = %d; want 404", rec.Code)
	}

	if rec := r.github(t, `{}`, "push", "d-1", hookSecret); rec.Code != http.StatusAccepted {
		t.Errorf("signed from the limited address's peer = %d; want 202", rec.Code)
	}

	r.clock.Advance(time.Minute)

	if rec := r.post(t, unknown, `{}`, nil, "203.0.113.9:3"); rec.Code != http.StatusNotFound {
		t.Errorf("the next minute = %d; want 404", rec.Code)
	}
}

// A token webhook with a header of its own is checked against it and never keeps it; the id and the
// event come from its body paths.
func TestHooks_TokenWebhook(t *testing.T) {
	t.Parallel()

	r := newHookRig(t, domain.SenderToken, domain.TokenSettings{Header: "X-Acme-Token", EventPath: "alert.type", IDPath: "alert.id"})
	body := `{"alert":{"id":"a-1","type":"monitor.alert"}}`
	path := api.HooksPath + string(r.hook.ID)

	if rec := r.post(t, path, body, map[string]string{"X-Acme-Token": "wrong"}, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("POST with a wrong token = %d; want 401", rec.Code)
	}

	rec := r.post(t, path, body, map[string]string{"X-Acme-Token": hookSecret}, "")
	if rec.Code != http.StatusAccepted || receiptOf(t, rec) != (receipt{ID: "a-1", Receipt: "accepted"}) {
		t.Fatalf("POST = %d %s; want 202 with a-1", rec.Code, rec.Body)
	}

	for _, d := range r.deliveries(t) {
		if _, ok := d.Headers["x-acme-token"]; ok {
			t.Errorf("a delivery keeps the token header: %v", d.Headers)
		}
	}
}
