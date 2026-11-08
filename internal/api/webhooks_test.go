package api_test

import (
	"cmp"
	"encoding/base64"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The codes the webhook tests expect.
const (
	// codeHookNotFound answers a webhook the organization does not have.
	codeHookNotFound = "webhook-not-found"
	codeHookInvalid  = "webhook-invalid"
	contentType      = "Content-Type"
)

var hexSecret = regexp.MustCompile(`^[0-9a-f]{64}$`)

// deliver records a delivery to endpoint id at at, as POST /hooks/{id} would.
func (h *harness) deliver(t *testing.T, id string, n domain.NewWebhookDelivery) domain.WebhookDelivery {
	t.Helper()

	n.EndpointID = domain.WebhookEndpointID(id)
	if n.Outcome == "" {
		n.Outcome = domain.DeliveryAccepted
	}

	d, err := h.intake.RecordDelivery(inOrg(t), n)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

// receiveUnnamed accepts a delivery id to endpoint id at at that no enabled trigger names, as POST
// /hooks/{id} would: only its UTC day's count keeps it.
func (h *harness) receiveUnnamed(t *testing.T, id, deliveryID string, at time.Time) {
	t.Helper()

	if _, err := h.intake.Receive(inOrg(t), domain.NewReceipt{
		WebhookID: domain.WebhookEndpointID(id), ReceivedAt: at, Receipt: domain.ReceiptAccepted, DeliveryID: deliveryID, Event: "push",
	}); err != nil {
		t.Fatal(err)
	}
}

// The factory webhook routes are gone (ADR 0040 decision 24): each answers 404, even for an
// organization webhook open to the factory, and the webhook stays.
func TestWebhooks_FactoryRoutesAreGone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	w := h.createOrgHook(t, `{"name":"ci-hooks","sender":"github"}`)
	h.request(t, http.MethodPatch, orgHooksPath+"/ci-hooks", `{"open":[{"factory":"`+acmeID+`"}]}`, nil)
	d := h.deliver(t, w.Id, domain.NewWebhookDelivery{ReceivedAt: t0, Body: []byte(`{"action":"completed"}`)})

	factory := "/v1/orgs/acme/factories/acme/webhooks"
	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, factory, ""},
		{http.MethodPost, factory, `{"name":"deploys","kind":"generic"}`},
		{http.MethodPost, factory + "/" + w.Id + "/rotate", ""},
		{http.MethodDelete, factory + "/" + w.Id, ""},
		{http.MethodGet, factory + "/" + w.Id + "/deliveries", ""},
		{http.MethodGet, factory + "/" + w.Id + "/deliveries/" + string(d.ID), ""},
	} {
		if res := h.request(t, r.method, r.path, r.body, nil); res.status != http.StatusNotFound {
			t.Errorf("%s %s = %d %s; want 404", r.method, r.path, res.status, res.body)
		}
	}

	if got := decode[gen.Webhook](t, h.request(t, http.MethodGet, orgHooksPath+"/"+w.Id, "", nil), http.StatusOK); got.Name != "ci-hooks" || len(got.Openings) != 1 {
		t.Errorf("webhook after the factory routes = %+v; want ci-hooks open to acme", got)
	}
}

// expectNoSecret checks that GET of each path answers 200 with none of secrets.
func (h *harness) expectNoSecret(t *testing.T, secrets []string, paths ...string) {
	t.Helper()

	for _, path := range paths {
		res := h.request(t, http.MethodGet, path, "", nil)
		if res.status != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, res.status, res.body)
		}

		for _, secret := range secrets {
			if strings.Contains(string(res.body), secret) {
				t.Errorf("GET %s holds a signing secret", path)
			}
		}
	}
}

// The delivery log lists newest first a page at a time; one delivery's request has its headers
// without a signature and its body, base64 when it is not UTF-8; a created delivery names its
// item; the list counts the last 7 UTC days.
func TestWebhooks_DeliveriesPage(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	w := h.createOrgHook(t, `{"name":"ci-hooks","sender":"github"}`)
	it := h.submit(t)

	signed := map[string]string{"X-Goal-Signature": "sha256=00", "X-Hub-Signature-256": "sha256=00", contentType: "application/json"}
	first := h.deliver(t, w.Id, domain.NewWebhookDelivery{ReceivedAt: t0.Add(-3 * 24 * time.Hour), Headers: signed, Body: []byte{0xff, 0xfe}})
	second := h.deliver(t, w.Id, domain.NewWebhookDelivery{
		ReceivedAt: t0.Add(-time.Hour), Outcome: domain.DeliveryRefused, Reason: signatureMismatch,
	})
	third := h.deliver(t, w.Id, domain.NewWebhookDelivery{
		ReceivedAt: t0, Headers: signed, Body: []byte(`{"event":"deploy.failed"}`), Outcome: domain.DeliveryCreated, ItemID: domain.ItemID(it.Id),
	})
	h.deliver(t, w.Id, domain.NewWebhookDelivery{ReceivedAt: t0.Add(-8 * 24 * time.Hour)}) // before the 7 days

	deliveries := orgHooksPath + "/" + w.Id + "/deliveries"
	page := decode[gen.WebhookDeliveryPage](t, h.request(t, http.MethodGet, deliveries+"?limit=2", "", nil), http.StatusOK)

	if got := deliveryIDs(page); !slices.Equal(got, []string{string(third.ID), string(second.ID)}) || page.NextAfter == nil {
		t.Fatalf("first page = %v, next %v; want the two newest and a cursor", got, page.NextAfter)
	}

	if item := page.Deliveries[0].Item; item == nil || item.Id != it.Id || item.Number != it.Number {
		t.Errorf("created delivery's item = %+v; want #%d", item, it.Number)
	}

	if d := page.Deliveries[1]; d.Outcome != gen.WebhookDeliveryOutcome(domain.DeliveryRefused) || d.Reason != signatureMismatch || d.Item != nil {
		t.Errorf("refused delivery = %+v", d)
	}

	rest := decode[gen.WebhookDeliveryPage](t, h.request(t, http.MethodGet, deliveries+"?limit=2&after="+url.QueryEscape(*page.NextAfter), "", nil), http.StatusOK)
	if got := deliveryIDs(rest); len(got) != 2 || got[0] != string(first.ID) || rest.NextAfter != nil {
		t.Errorf("last page = %v, next %v; want the two oldest and no cursor", got, rest.NextAfter)
	}

	h.expectRequests(t, deliveries, first, third)
	h.expectDays(t)
}

// expectRequests checks text's and binary's requests: headers without signatures, the body as
// text or in base64.
func (h *harness) expectRequests(t *testing.T, deliveries string, binary, text domain.WebhookDelivery) {
	t.Helper()

	got := decode[gen.WebhookDeliveryRequest](t, h.request(t, http.MethodGet, deliveries+"/"+string(text.ID), "", nil), http.StatusOK)
	if !maps.Equal(got.Headers, map[string]string{"content-type": "application/json"}) || got.Body != string(text.Body) ||
		got.BodyEncoding != gen.BodyText || got.Item == nil {
		t.Errorf("delivery = %+v; want its headers without signatures and its body as text", got)
	}

	raw := decode[gen.WebhookDeliveryRequest](t, h.request(t, http.MethodGet, deliveries+"/"+string(binary.ID), "", nil), http.StatusOK)
	if raw.BodyEncoding != gen.BodyBase64 || raw.Body != base64.StdEncoding.EncodeToString(binary.Body) {
		t.Errorf("non-UTF-8 delivery = %+v; want its body in base64", raw)
	}
}

// expectDays checks ci-hooks's 7 days in the list: Oct 17 to 23, one delivered 3 days ago, one
// delivered and one refused today, none otherwise.
func (h *harness) expectDays(t *testing.T) {
	t.Helper()

	list := decode[gen.WebhookList](t, h.request(t, http.MethodGet, orgHooksPath, "", nil), http.StatusOK)
	if len(list.Webhooks) != 1 || len(list.Webhooks[0].Days) != 7 {
		t.Fatalf("list = %+v; want ci-hooks with 7 days", list.Webhooks)
	}

	want := map[int]gen.WebhookDeliveryDay{3: {Delivered: 1}, 6: {Delivered: 1, Refused: 1}}

	for i, d := range list.Webhooks[0].Days {
		w := want[i]
		if day := time.Date(2026, 10, 17+i, 0, 0, 0, 0, time.UTC); !d.Day.Equal(day) || d.Delivered != w.Delivered || d.Refused != w.Refused {
			t.Errorf("day %d = %+v; want %s with %d delivered, %d refused", i, d, day.Format(time.DateOnly), w.Delivered, w.Refused)
		}
	}
}

// Every page of the delivery log carries the webhook's counts of deliveries no trigger names over
// the log's 30 days, by UTC day.
func TestWebhooks_DeliveriesPageUnnamedDays(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	w := h.createOrgHook(t, `{"name":"ci-hooks","sender":"github"}`)

	for i := range 3 {
		h.deliver(t, w.Id, domain.NewWebhookDelivery{ReceivedAt: t0.Add(-time.Duration(i) * time.Hour)})
		h.receiveUnnamed(t, w.Id, fmt.Sprintf("unnamed-%d", i), t0.Add(-time.Duration(i)*time.Minute))
	}

	h.receiveUnnamed(t, w.Id, "unnamed-old", t0.Add(-domain.DeliveryRetention-24*time.Hour)) // before the log's days

	want := []gen.WebhookUnnamedDay{{Day: openapi_types.Date{Time: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}, Count: 3}}
	same := func(got []gen.WebhookUnnamedDay) bool {
		return slices.EqualFunc(got, want, func(a, b gen.WebhookUnnamedDay) bool { return a.Day.Equal(b.Day.Time) && a.Count == b.Count })
	}

	deliveries := orgHooksPath + "/" + w.Id + "/deliveries"

	page := decode[gen.WebhookDeliveryPage](t, h.request(t, http.MethodGet, deliveries+"?limit=2", "", nil), http.StatusOK)
	if page.NextAfter == nil || !same(page.UnnamedDays) {
		t.Fatalf("first page unnamed_days = %+v, next %v; want %+v and a cursor", page.UnnamedDays, page.NextAfter, want)
	}

	rest := decode[gen.WebhookDeliveryPage](t, h.request(t, http.MethodGet, deliveries+"?limit=2&after="+url.QueryEscape(*page.NextAfter), "", nil), http.StatusOK)
	if !same(rest.UnnamedDays) {
		t.Errorf("next page unnamed_days = %+v; want %+v", rest.UnnamedDays, want)
	}
}

// A webhook's 7-day delivered counts include the deliveries no trigger names, on their UTC day,
// and leave refused as it is.
func TestWebhooks_ListCountsUnnamedDeliveries(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	w := h.createOrgHook(t, `{"name":"ci-hooks","sender":"github"}`)

	h.deliver(t, w.Id, domain.NewWebhookDelivery{ReceivedAt: t0.Add(-time.Hour), Outcome: domain.DeliveryRefused, Reason: signatureMismatch})
	h.deliver(t, w.Id, domain.NewWebhookDelivery{ReceivedAt: t0})
	h.receiveUnnamed(t, w.Id, "unnamed-1", t0.Add(-time.Minute))
	h.receiveUnnamed(t, w.Id, "unnamed-2", t0.Add(-2*time.Minute))
	h.receiveUnnamed(t, w.Id, "unnamed-3", t0.Add(-2*24*time.Hour)) // a day with only unnamed ones

	list := decode[gen.WebhookList](t, h.request(t, http.MethodGet, orgHooksPath, "", nil), http.StatusOK)
	if len(list.Webhooks) != 1 || len(list.Webhooks[0].Days) != 7 {
		t.Fatalf("list = %+v; want ci-hooks with 7 days", list.Webhooks)
	}

	want := map[int]gen.WebhookDeliveryDay{4: {Delivered: 1}, 6: {Delivered: 3, Refused: 1}}

	for i, d := range list.Webhooks[0].Days {
		if w := want[i]; d.Delivered != w.Delivered || d.Refused != w.Refused {
			t.Errorf("day %d = %+v; want %d delivered, %d refused", i, d, w.Delivered, w.Refused)
		}
	}
}

func deliveryIDs(p gen.WebhookDeliveryPage) []string {
	out := []string{}
	for _, d := range p.Deliveries {
		out = append(out, d.Id)
	}

	return out
}

// Only the organization's owners and admins list webhooks and read their delivery log
// (webhook:read), and, with a sign-in from the last 10 minutes, create and delete them and read a
// delivery's headers and body (webhook:manage, ADR 0040 decision 12). A factory's editor holds
// none of it, and another organization reaches nothing.
func TestWebhooks_Permissions(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	created := decode[gen.CreatedWebhook](t, h.as(t, freshToken, http.MethodPost, orgHooksPath, `{"name":"ci-hooks","sender":"github"}`), http.StatusCreated)
	hook := orgHooksPath + "/" + created.Id
	d := h.deliver(t, created.Id, domain.NewWebhookDelivery{ReceivedAt: t0, Body: []byte(`{"event":"deploy"}`)})

	decode[gen.WebhookDeliveryPage](t, h.as(t, freshToken, http.MethodGet, hook+"/deliveries", ""), http.StatusOK)
	decode[gen.WebhookDeliveryRequest](t, h.as(t, freshToken, http.MethodGet, hook+"/deliveries/"+string(d.ID), ""), http.StatusOK)

	if err := h.identity.GrantFactoryRole(testOrg, viewerID, acmeID, domain.FactoryEditor); err != nil {
		t.Fatal(err)
	}

	for _, w := range []struct{ method, path, body string }{
		{http.MethodGet, orgHooksPath, ""},
		{http.MethodPost, orgHooksPath, `{"name":"mine","sender":"github"}`},
		{http.MethodGet, hook + "/deliveries", ""},
		{http.MethodGet, hook + "/deliveries/" + string(d.ID), ""},
		{http.MethodDelete, hook, ""},
	} {
		refused(t, "a factory editor's "+w.method+" "+w.path, h.as(t, freshViewerToken, w.method, w.path, w.body), http.StatusForbidden, "forbidden")
	}

	for _, path := range []string{hook + "/deliveries", orgHooksPath} {
		if res := h.as(t, strangerToken, http.MethodGet, path, ""); res.status != http.StatusNotFound {
			t.Errorf("another organization's owner reading %s = %d; want 404", path, res.status)
		}
	}

	if err := h.identity.SetOrgRole(testOrg, viewerID, domain.OrgAdmin); err != nil {
		t.Fatal(err)
	}

	refused(t, "an admin's stale create", h.as(t, viewerToken, http.MethodPost, orgHooksPath, `{"name":"mine","sender":"github"}`),
		http.StatusForbidden, "reauth-required")
	decode[gen.WebhookList](t, h.as(t, viewerToken, http.MethodGet, orgHooksPath, ""), http.StatusOK)
	decode[gen.WebhookDeliveryPage](t, h.as(t, viewerToken, http.MethodGet, hook+"/deliveries", ""), http.StatusOK)
	decode[gen.WebhookDeliveryRequest](t, h.as(t, freshViewerToken, http.MethodGet, hook+"/deliveries/"+string(d.ID), ""), http.StatusOK)
	decode[gen.CreatedWebhook](t, h.as(t, freshViewerToken, http.MethodPost, orgHooksPath, `{"name":"mine","sender":"github"}`), http.StatusCreated)

	if r := h.as(t, freshViewerToken, http.MethodDelete, hook, ""); r.status != http.StatusNoContent {
		t.Errorf("an admin's delete = %d %s; want 204", r.status, r.body)
	}
}

// orgHooksPath is the organization's webhook routes.
const orgHooksPath = "/v1/orgs/acme/webhooks"

// createOrgHook creates the organization webhook of body as the local user.
func (h *harness) createOrgHook(t *testing.T, body string) gen.CreatedWebhook {
	t.Helper()

	return decode[gen.CreatedWebhook](t, h.request(t, http.MethodPost, orgHooksPath, body, nil), http.StatusCreated)
}

// New webhook without a secret creates webhook-<name>, cut to 40 characters without a trailing
// hyphen, open to no factory, and answers its value once: no other answer holds it, and the
// secret's uses name the webhook. With an existing secret it answers no value.
func TestWebhooks_OrgCreate(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	w := h.createOrgHook(t, `{"name":"ci-hooks","sender":"github"}`)
	if w.SecretValue == nil || !hexSecret.MatchString(*w.SecretValue) || w.Secret != "webhook-ci-hooks" || !w.Enabled ||
		len(w.Openings) != 0 || w.Address != "/hooks/"+w.Id || w.Sender != gen.WebhookSenderGithub || w.Token != nil || len(w.Days) != 7 {
		t.Fatalf("created = %+v; want an enabled github webhook, open to none, with webhook-ci-hooks's value", w)
	}

	sec := decode[gen.Secret](t, h.request(t, http.MethodGet, secretsPath+"/webhook-ci-hooks", "", nil), http.StatusOK)
	if sec.Scope.AllFactories || len(sec.Scope.Factories) != 0 || !slices.Contains(sec.Uses, gen.SecretUse{Kind: gen.SecretUseWebhook, Name: "ci-hooks"}) {
		t.Errorf("secret = %+v; want it open to no factory and used by the webhook", sec)
	}

	h.expectNoSecret(t, []string{*w.SecretValue}, orgHooksPath, orgHooksPath+"/ci-hooks", orgHooksPath+"/"+w.Id, secretsPath, "/v1/orgs/acme/audit")
}

// A long name's secret is cut without a trailing hyphen; a standard sender's value is whsec_; an
// existing secret answers no value and cannot be deleted while the webhook uses it; a token
// sender reads its default header.
func TestWebhooks_OrgCreateNamesAndSecrets(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	long := h.createOrgHook(t, `{"name":"abcdefghijklmnopqrstuvwxyzabcde-fgh","sender":"standard"}`)
	if long.Secret != "webhook-abcdefghijklmnopqrstuvwxyzabcde" || long.SecretValue == nil || !strings.HasPrefix(*long.SecretValue, "whsec_") {
		t.Errorf("long name's secret = %q, value %v; want webhook-abcdefghijklmnopqrstuvwxyzabcde and a whsec_ value", long.Secret, long.SecretValue)
	}

	if _, err := h.secrets.Create(inOrg(t), domain.NewSecret{Name: "pagerduty-key", Value: "test-value-pagerduty"}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	pages := h.createOrgHook(t, `{"name":"pages","sender":"pagerduty","secret":"pagerduty-key"}`)
	if pages.SecretValue != nil || pages.Secret != "pagerduty-key" {
		t.Errorf("with an existing secret = %+v; want it signed by pagerduty-key and no value", pages)
	}

	token := h.createOrgHook(t, `{"name":"datadog","sender":"token","token":{"event_path":"event.type"}}`)
	if token.Token == nil || deref(token.Token.Header) != domain.DefaultTokenHeader || deref(token.Token.EventPath) != "event.type" {
		t.Errorf("token webhook's settings = %+v; want the default header and its event path", token.Token)
	}

	p := refused(t, "deleting a webhook's secret", h.request(t, http.MethodDelete, secretsPath+"/pagerduty-key", "", nil), http.StatusConflict, "secret-in-use")
	if p.Uses == nil || !slices.Contains(*p.Uses, gen.SecretUse{Kind: gen.SecretUseWebhook, Name: "pages"}) {
		t.Errorf("secret-in-use uses = %v; want the webhook pages", p.Uses)
	}

	list := decode[gen.WebhookList](t, h.request(t, http.MethodGet, orgHooksPath, "", nil), http.StatusOK)
	if names := webhookNames(list); !slices.Equal(names, []string{"abcdefghijklmnopqrstuvwxyzabcde-fgh", "datadog", "pages"}) {
		t.Errorf("webhooks = %v; want the three by name", names)
	}
}

func webhookNames(l gen.WebhookList) []string {
	out := []string{}
	for _, w := range l.Webhooks {
		out = append(out, w.Name)
	}

	return out
}

// Requests the organization webhook rules refuse answer their codes and change nothing.
func TestWebhooks_OrgRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	w := h.createOrgHook(t, `{"name":"builds","sender":"github"}`)
	h.request(t, http.MethodPatch, orgHooksPath+"/builds", `{"open":[{"factory":"`+acmeID+`"}]}`, nil)

	tests := []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{"a reserved name", http.MethodPost, orgHooksPath, `{"name":"schedule","sender":"github"}`, http.StatusBadRequest, codeHookInvalid},
		{"an unknown sender", http.MethodPost, orgHooksPath, `{"name":"pings","sender":"slack"}`, http.StatusBadRequest, codeHookInvalid},
		{"token settings on github", http.MethodPost, orgHooksPath, `{"name":"pings","sender":"github","token":{"header":"X-Token"}}`, http.StatusBadRequest, codeHookInvalid},
		{"a taken name", http.MethodPost, orgHooksPath, `{"name":"builds","sender":"gitlab"}`, http.StatusConflict, "webhook-name-taken"},
		{"no such secret", http.MethodPost, orgHooksPath, `{"name":"pings","sender":"sentry","secret":"ghost-secret"}`, http.StatusNotFound, "secret-not-found"},
		{"no such webhook", http.MethodGet, orgHooksPath + "/ghost", "", http.StatusNotFound, codeHookNotFound},
		{"no such factory", http.MethodPatch, orgHooksPath + "/builds", `{"open":[{"factory":"00000000-0000-4000-8000-ffffffffffff"}]}`, http.StatusNotFound, codeFactoryNotFound},
		{"opened and closed", http.MethodPatch, orgHooksPath + "/builds", `{"open":[{"factory":"` + lineID + `"}],"close":["` + lineID + `"]}`, http.StatusBadRequest, codeHookInvalid},
		{"a reserved rename", http.MethodPatch, orgHooksPath + "/builds", `{"name":"webhook"}`, http.StatusBadRequest, codeHookInvalid},
		{"deliveries of no webhook", http.MethodGet, orgHooksPath + "/ghost/deliveries", "", http.StatusNotFound, codeHookNotFound},
		{
			"no such delivery", http.MethodGet, orgHooksPath + "/builds/deliveries/019a0000-0000-7000-8000-000000000000", "",
			http.StatusNotFound, "webhook-delivery-not-found",
		},
		{"a cursor of nothing", http.MethodGet, orgHooksPath + "/builds/deliveries?after=nope", "", http.StatusBadRequest, codeHookInvalid},
	}

	for _, tc := range tests {
		refused(t, tc.name, h.request(t, tc.method, tc.path, tc.body, nil), tc.status, tc.code)
	}

	got := decode[gen.Webhook](t, h.request(t, http.MethodGet, orgHooksPath+"/"+w.Id, "", nil), http.StatusOK)
	if got.Name != "builds" || len(got.Openings) != 1 {
		t.Errorf("webhook after the refusals = %+v; want builds open to acme alone", got)
	}

	list := decode[gen.WebhookList](t, h.request(t, http.MethodGet, orgHooksPath, "", nil), http.StatusOK)
	if len(list.Webhooks) != 1 {
		t.Errorf("webhooks after the refusals = %v; want builds alone", webhookNames(list))
	}
}

// Opening a webhook to a factory, limited or not, lists its triggers; closing one stops that. A
// webhook acme's published trigger deploy-failed uses (on webhook.ci-hooks.deploy)
// refuses a close of acme, a rename and a delete with 409 webhook-in-use naming the trigger; one
// no trigger uses is renamed, closed and deleted.
func TestWebhooks_OrgOpenCloseAndInUse(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	w := h.createOrgHook(t, `{"name":"ci-hooks","sender":"github"}`)
	path := orgHooksPath + "/ci-hooks"

	use := gen.WebhookTrigger{Factory: acmeID, FactoryName: acmeName, Trigger: "deploy-failed"}
	h.openToAcmeAndLine(t, path, use)

	h.expectOpenDeliveries(t, w.Id)
	h.expectInUse(t, w.Id, use)

	closed := decode[gen.Webhook](t, h.request(t, http.MethodPatch, path, `{"close":["`+lineID+`"],"enabled":false}`, nil), http.StatusOK)
	if len(closed.Openings) != 1 || closed.Openings[0].Factory != acmeID || closed.Enabled {
		t.Errorf("after closing line = %+v; want open to acme alone, disabled", closed)
	}
}

// openToAcmeAndLine opens the webhook at path to acme, limited to acme/api, and to line, and
// checks its triggers are acme's use, which names it, and flaky-test-on-main, which names its
// sender.
func (h *harness) openToAcmeAndLine(t *testing.T, path string, use gen.WebhookTrigger) {
	t.Helper()

	opened := decode[gen.Webhook](t, h.request(t, http.MethodPatch, path,
		`{"open":[{"factory":"`+acmeID+`","repositories":["acme/api"]},{"factory":"`+lineID+`"}]}`, nil), http.StatusOK)

	flaky := gen.WebhookTrigger{Factory: acmeID, FactoryName: acmeName, Trigger: "flaky-test-on-main"}
	if len(opened.Openings) != 2 || !slices.Equal(opened.Triggers, []gen.WebhookTrigger{use, flaky}) {
		t.Fatalf("opened = %+v; want open to acme and line, used by acme's deploy-failed and flaky-test-on-main", opened)
	}

	for _, o := range opened.Openings {
		if o.Factory == acmeID && (o.Repositories == nil || !slices.Equal(*o.Repositories, []string{"acme/api"})) || o.Factory == lineID && o.Repositories != nil {
			t.Errorf("opening = %+v; want acme limited to acme/api and line to none", o)
		}
	}
}

// expectOpenDeliveries checks a delivery to webhook id is listed on its route and read with its
// body.
func (h *harness) expectOpenDeliveries(t *testing.T, id string) {
	t.Helper()

	d := h.deliver(t, id, domain.NewWebhookDelivery{ReceivedAt: t0, Body: []byte(`{"action":"completed"}`)})
	path := orgHooksPath + "/" + id

	if got := deliveryIDs(decode[gen.WebhookDeliveryPage](t, h.request(t, http.MethodGet, path+"/deliveries", "", nil), http.StatusOK)); !slices.Equal(got, []string{string(d.ID)}) {
		t.Errorf("GET %s/deliveries = %v; want the delivery", path, got)
	}

	req := decode[gen.WebhookDeliveryRequest](t, h.request(t, http.MethodGet, path+"/deliveries/"+string(d.ID), "", nil), http.StatusOK)
	if req.Body != `{"action":"completed"}` {
		t.Errorf("delivery = %+v; want its body", req)
	}
}

// expectInUse checks that closing acme, renaming and deleting webhook id answer 409 naming use.
func (h *harness) expectInUse(t *testing.T, id string, use gen.WebhookTrigger) {
	t.Helper()

	path := orgHooksPath + "/" + id

	for _, r := range []struct{ name, method, path, body string }{
		{"closing acme", http.MethodPatch, path, `{"close":["` + acmeID + `"]}`},
		{"a rename", http.MethodPatch, path, `{"name":"github-acme"}`},
		{"a delete", http.MethodDelete, path, ""},
	} {
		p := refused(t, r.name, h.request(t, r.method, r.path, r.body, nil), http.StatusConflict, "webhook-in-use")
		if p.Triggers == nil || !slices.Equal(*p.Triggers, []gen.WebhookTrigger{use}) {
			t.Errorf("%s: triggers = %v; want acme's deploy-failed", r.name, p.Triggers)
		}
	}
}

// A webhook no published trigger uses is renamed, closed and deleted, and then its secret is
// deleted too.
func TestWebhooks_OrgUnusedChanges(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.createOrgHook(t, `{"name":"builds","sender":"gitlab"}`)
	h.request(t, http.MethodPatch, orgHooksPath+"/builds", `{"open":[{"factory":"`+acmeID+`"}]}`, nil)

	renamed := decode[gen.Webhook](t, h.request(t, http.MethodPatch, orgHooksPath+"/builds", `{"name":"gitlab-builds","close":["`+acmeID+`"]}`, nil), http.StatusOK)
	if renamed.Name != "gitlab-builds" || len(renamed.Openings) != 0 || renamed.Secret != "webhook-builds" {
		t.Errorf("renamed = %+v; want gitlab-builds, open to none, still signed by webhook-builds", renamed)
	}

	if r := h.request(t, http.MethodDelete, orgHooksPath+"/gitlab-builds", "", nil); r.status != http.StatusNoContent {
		t.Errorf("delete of an unused webhook = %d %s; want 204", r.status, r.body)
	}

	if r := h.request(t, http.MethodDelete, secretsPath+"/webhook-builds", "", nil); r.status != http.StatusNoContent {
		t.Errorf("delete of its secret once it is gone = %d %s; want 204", r.status, r.body)
	}
}

// getOrgHook reads the organization's webhook ref, with its raw answer.
func (h *harness) getOrgHook(t *testing.T, ref string) (gen.Webhook, string) {
	t.Helper()

	r := h.request(t, http.MethodGet, orgHooksPath+"/"+ref, "", nil)

	return decode[gen.Webhook](t, r, http.StatusOK), string(r.body)
}

// A webhook's setup events are the events of the enabled triggers that name it or its sender, in
// the factories it is open to, sorted and distinct; a trigger naming it with no event asks for
// every event. A webhook open to none answers an empty list, never null. GitHub's ping time reads
// null until a ping is accepted, then that time.
func TestWebhooks_SetupEventsAndPing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	w := h.createOrgHook(t, `{"name":"ci-hooks","sender":"github"}`)

	got, raw := h.getOrgHook(t, "ci-hooks")
	if len(got.SetupEvents) != 0 || got.SetupAllEvents || !strings.Contains(raw, `"setup_events":[]`) || !strings.Contains(raw, `"pinged_at":null`) {
		t.Fatalf("webhook open to none = %s; want no setup events, as [], and pinged_at null", raw)
	}

	h.request(t, http.MethodPatch, orgHooksPath+"/ci-hooks", `{"open":[{"factory":"`+acmeID+`"}]}`, nil)

	// acme's flaky-test-on-main names the sender (github.check_run.failed); deploy-failed is disabled.
	h.expectSetup(t, w.Id, []string{checkRunEvent}, false)

	h.openDraft(t)
	h.saveYAML(t, strings.Replace(acme, "        enabled: false\n        on: webhook.ci-hooks.deploy\n",
		"        on: webhook.ci-hooks.deploy\n        create: { type: fix, station: inbox }\n      - name: every-hook\n        on: ci-hooks\n"+
			"        create: { type: fix, station: inbox }\n      - name: other-check\n        on: github.check_run\n", 1))
	h.publish(t, "use ci-hooks")

	if _, err := h.versions.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	h.expectSetup(t, w.Id, []string{checkRunEvent, "deploy"}, true)

	if _, err := h.intake.Receive(inOrg(t), domain.NewReceipt{
		WebhookID: domain.WebhookEndpointID(w.Id), ReceivedAt: t0, Receipt: domain.ReceiptAccepted, DeliveryID: "p-1", Event: "ping", Ping: true,
	}); err != nil {
		t.Fatal(err)
	}

	if got, _ = h.getOrgHook(t, "ci-hooks"); got.PingedAt == nil || !got.PingedAt.Equal(t0) {
		t.Errorf("pinged_at = %v; want %v", got.PingedAt, t0)
	}
}

// A webhook's triggers are those that name it, enabled or not, and the enabled ones that name its
// sender with an event (ADR 0040 decision 14), by factory name then trigger. Sender triggers do
// not hold the webhook: one only they use is renamed, closed and deleted. An archived factory's
// triggers are left out, as [].
func TestWebhooks_TriggersNameSender(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	w := h.createOrgHook(t, `{"name":"ci-hooks","sender":"github"}`)
	h.createOrgHook(t, `{"name":"builds","sender":"github"}`)

	h.openDraft(t)
	h.saveYAML(t, strings.Replace(acme, "      - name: new-incidents\n",
		"      - name: a-push-off\n        enabled: false\n        on: github.push\n        create: { type: fix, station: inbox }\n"+
			"      - name: gitlab-push\n        on: gitlab.push\n        create: { type: fix, station: inbox }\n"+
			"      - name: a-pull-request\n        on: github.pull_request\n        create: { type: fix, station: inbox }\n"+
			"      - name: new-incidents\n", 1))
	h.publish(t, "sender triggers")

	if _, err := h.versions.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"ci-hooks", "builds"} {
		h.request(t, http.MethodPatch, orgHooksPath+"/"+name, `{"open":[{"factory":"`+acmeID+`"},{"factory":"`+lineID+`"}]}`, nil)
	}

	use := func(trigger string) gen.WebhookTrigger {
		return gen.WebhookTrigger{Factory: acmeID, FactoryName: acmeName, Trigger: trigger}
	}

	h.expectTriggers(t, "ci-hooks", []gen.WebhookTrigger{use("a-pull-request"), use("deploy-failed"), use("flaky-test-on-main")})
	h.expectTriggers(t, "builds", []gen.WebhookTrigger{use("a-pull-request"), use("flaky-test-on-main")})

	renamed := decode[gen.Webhook](t, h.request(t, http.MethodPatch, orgHooksPath+"/builds", `{"name":"github-builds","close":["`+acmeID+`"]}`, nil), http.StatusOK)
	if renamed.Name != "github-builds" || len(renamed.Openings) != 1 || len(renamed.Triggers) != 0 {
		t.Errorf("renamed and closed = %+v; want github-builds, open to line, with no triggers", renamed)
	}

	if r := h.request(t, http.MethodDelete, orgHooksPath+"/github-builds", "", nil); r.status != http.StatusNoContent {
		t.Errorf("delete of a webhook only sender triggers match = %d %s; want 204", r.status, r.body)
	}

	h.expectInUse(t, w.Id, use("deploy-failed"))

	h.archive(t, acmeID)

	if got, raw := h.getOrgHook(t, "ci-hooks"); len(got.Triggers) != 0 || !strings.Contains(raw, `"triggers":[]`) {
		t.Errorf("ci-hooks once acme is archived = %s; want no triggers, as []", raw)
	}
}

// expectTriggers checks the organization's webhook name has triggers, as read and as listed.
func (h *harness) expectTriggers(t *testing.T, name string, triggers []gen.WebhookTrigger) {
	t.Helper()

	if got, _ := h.getOrgHook(t, name); !slices.Equal(got.Triggers, triggers) {
		t.Errorf("%s: triggers = %+v; want %+v", name, got.Triggers, triggers)
	}

	list := decode[gen.WebhookList](t, h.request(t, http.MethodGet, orgHooksPath, "", nil), http.StatusOK)
	for _, w := range list.Webhooks {
		if w.Name == name && !slices.Equal(w.Triggers, triggers) {
			t.Errorf("%s listed: triggers = %+v; want %+v", name, w.Triggers, triggers)
		}
	}
}

// expectSetup checks webhook id's setup events are events, and all, as read and as listed.
func (h *harness) expectSetup(t *testing.T, id string, events []string, all bool) {
	t.Helper()

	got, _ := h.getOrgHook(t, id)
	if !slices.Equal(got.SetupEvents, events) || got.SetupAllEvents != all {
		t.Errorf("setup events = %v, all %v; want %v, all %v", got.SetupEvents, got.SetupAllEvents, events, all)
	}

	list := decode[gen.WebhookList](t, h.request(t, http.MethodGet, orgHooksPath, "", nil), http.StatusOK)
	if len(list.Webhooks) != 1 || !slices.Equal(list.Webhooks[0].SetupEvents, events) || list.Webhooks[0].SetupAllEvents != all {
		t.Errorf("listed = %+v; want the same setup events", list.Webhooks)
	}
}

// logPage reads a delivery log at path, with its raw answer.
func (h *harness) logPage(t *testing.T, path string) (gen.WebhookDeliveryPage, string) {
	t.Helper()

	r := h.request(t, http.MethodGet, path, "", nil)

	return decode[gen.WebhookDeliveryPage](t, r, http.StatusOK), string(r.body)
}

// The organization's delivery log shows each delivery's event, receipt and duplicates, and its
// evaluation by each factory, by factory name: created with the item's ref, skipped with its
// reason. The log holds no body or headers.
func TestWebhooks_DeliveryLogReceiptsAndEvaluations(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	w := h.createOrgHook(t, `{"name":"ci-hooks","sender":"github"}`)
	h.request(t, http.MethodPatch, orgHooksPath+"/ci-hooks", `{"open":[{"factory":"`+acmeID+`"},{"factory":"`+lineID+`"}]}`, nil)

	accepted, item := h.evaluatedDelivery(t, domain.WebhookEndpointID(w.Id))

	if _, err := h.intake.Receive(inOrg(t), domain.NewReceipt{
		WebhookID: domain.WebhookEndpointID(w.Id), ReceivedAt: t0.Add(time.Minute), Receipt: domain.ReceiptRefused, Reason: signatureMismatch,
		Headers: map[string]string{"x-github-event": checkRunEvent},
	}); err != nil {
		t.Fatal(err)
	}

	created := gen.WebhookDeliveryEvaluation{
		Factory: acmeID, FactoryName: acmeName, State: gen.WebhookDeliveryEvaluationStateDone,
		Outcome: new(gen.WebhookDeliveryEvaluationOutcomeCreated),
		Item:    &gen.WebhookEvaluationItem{Id: string(item.ID), Number: item.Number, Ref: fmt.Sprintf("%s#%d", acmeName, item.Number)},
	}
	skipped := gen.WebhookDeliveryEvaluation{
		Factory: lineID, FactoryName: lineSlug, State: gen.WebhookDeliveryEvaluationStateDone,
		Outcome: new(gen.WebhookDeliveryEvaluationOutcomeSkipped), Reason: lineSkip,
	}

	// line's id sorts before acme's, so this order is by name.
	h.expectLog(t, orgHooksPath+"/ci-hooks/deliveries", string(accepted), []gen.WebhookDeliveryEvaluation{created, skipped})

	one := decode[gen.WebhookDeliveryRequest](t, h.request(t, http.MethodGet, orgHooksPath+"/ci-hooks/deliveries/"+string(accepted), "", nil), http.StatusOK)
	if one.Duplicates != 2 || !reflect.DeepEqual(one.Evaluations, []gen.WebhookDeliveryEvaluation{created, skipped}) {
		t.Errorf("one delivery = %+v; want its duplicates and both evaluations", one)
	}
}

// expectLog checks the log at path: no headers or body, the refused delivery newest with its
// reason and no evaluations, then the accepted one with its event, two duplicates and evals.
func (h *harness) expectLog(t *testing.T, path, accepted string, evals []gen.WebhookDeliveryEvaluation) {
	t.Helper()

	page, raw := h.logPage(t, path)
	if strings.Contains(raw, `"headers"`) || strings.Contains(raw, `"body"`) {
		t.Errorf("%s = %s; want no headers or body", path, raw)
	}

	if len(page.Deliveries) != 2 {
		t.Fatalf("%s = %+v; want the refused and the accepted delivery", path, page.Deliveries)
	}

	expectRefusedEntry(t, path, page.Deliveries[0])

	got := page.Deliveries[1]
	if got.Id != accepted || got.Event != checkRunEvent || got.Receipt == nil || *got.Receipt != gen.WebhookDeliveryReceiptAccepted ||
		got.Duplicates != 2 || !reflect.DeepEqual(got.Evaluations, evals) {
		t.Errorf("%s: accepted = %+v; want check_run, accepted, 2 duplicates and evaluations %+v", path, got, evals)
	}
}

// expectRefusedEntry checks d, in the log at path, is refused with its reason and no evaluations.
func expectRefusedEntry(t *testing.T, path string, d gen.WebhookDelivery) {
	t.Helper()

	if d.Receipt == nil || *d.Receipt != gen.WebhookDeliveryReceiptRefused || d.Reason != signatureMismatch || d.Evaluations == nil || len(d.Evaluations) != 0 {
		t.Errorf("%s: refused = %+v; want refused with its reason and no evaluations, as []", path, d)
	}
}

// lineSkip is why line skipped the evaluated delivery.
const lineSkip = "acme/legacy is not one of line's repositories"

// evaluatedDelivery receives d-1 to webhook w three times, evaluated by acme, which creates an
// item, and by line, which skips it at once, and returns its row and the item.
func (h *harness) evaluatedDelivery(t *testing.T, w domain.WebhookEndpointID) (domain.WebhookDeliveryID, domain.Item) {
	t.Helper()

	var row domain.WebhookDeliveryID

	for range 3 {
		r, err := h.intake.Receive(inOrg(t), domain.NewReceipt{
			WebhookID: w, ReceivedAt: t0, Receipt: domain.ReceiptAccepted, DeliveryID: firstDeliveryID, Event: checkRunEvent, Named: true,
			Headers: map[string]string{"x-github-event": checkRunEvent}, Body: []byte(`{"action":"completed"}`),
			Evaluations: []domain.NewDeliveryEvaluation{
				{FactoryID: acmeID, Triggers: []string{"flaky-test-on-main"}},
				{FactoryID: lineID, Triggers: []string{"flaky"}, Skip: lineSkip},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		row = cmp.Or(row, r.Row)
	}

	item := h.intakeItem(t, "flaky-test-on-main", t0)

	claimed, err := h.intake.ClaimEvaluations(inOrg(t), "goald-a", t0, time.Minute, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimEvaluations = %+v, %v; want acme's", claimed, err)
	}

	c := claimed[0]
	if ok, err := h.intake.FinishEvaluation(inOrg(t), domain.EvaluationFinish{
		ID: c.ID, Holder: c.LeaseHolder, Attempts: c.Attempts, State: domain.EvaluationDone, Outcome: domain.EvaluationCreated, ItemID: item.ID, At: t0,
	}); err != nil || !ok {
		t.Fatalf("FinishEvaluation = %v, %v", ok, err)
	}

	return row, item
}
