package api_test

import (
	"encoding/base64"
	"encoding/hex"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The routes the webhook tests call and the endpoint most of them create.
const (
	webhooksPath = "/v1/orgs/acme/factories/acme/webhooks"
	lineWebhooks = "/v1/orgs/acme/factories/line/webhooks"
	hookName     = "ci-hooks"
	hookBody     = `{"name":"` + hookName + `","kind":"generic"}`
	// codeHookNotFound answers an endpoint the factory does not have.
	codeHookNotFound = "webhook-not-found"
	codeHookInvalid  = "webhook-invalid"
	contentType      = "Content-Type"
)

var hexSecret = regexp.MustCompile(`^[0-9a-f]{64}$`)

// createHook creates ci-hooks on factory acme as the harness's local user.
func (h *harness) createHook(t *testing.T) gen.CreatedWebhookEndpoint {
	t.Helper()

	return decode[gen.CreatedWebhookEndpoint](t, h.request(t, http.MethodPost, webhooksPath, hookBody, nil), http.StatusCreated)
}

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

// Create and rotate answer a 64-character hex secret, the only answers that hold one: the list,
// the delivery log and the audit log never do, and after a rotate only the new secret opens.
func TestWebhooks_SecretShownOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	created := h.createHook(t)
	if !hexSecret.MatchString(created.Secret) || created.Name != hookName || created.Kind != gen.WebhookKindGeneric || created.Sender != gen.WebhookSenderStandard ||
		created.Address != "/hooks/"+created.Id || created.SecretName != hookName+"-hook-secret" || created.RotatedAt != nil {
		t.Fatalf("created = %+v; want ci-hooks with a 64-character hex secret", created)
	}

	d := h.deliver(t, created.Id, domain.NewWebhookDelivery{
		ReceivedAt: t0, Headers: map[string]string{"X-Goal-Signature": "sha256=" + created.Secret, contentType: "application/json"},
		Body: []byte(`{"event":"deploy"}`),
	})

	rotated := h.rotateHook(t, created)

	h.expectNoSecret(t, []string{created.Secret, rotated.Secret},
		webhooksPath, webhooksPath+"/"+created.Id+"/deliveries", webhooksPath+"/"+created.Id+"/deliveries/"+string(d.ID), "/v1/orgs/acme/audit")

	list := decode[gen.WebhookEndpointList](t, h.request(t, http.MethodGet, webhooksPath, "", nil), http.StatusOK)
	if len(list.Endpoints) != 1 || list.Endpoints[0].Id != created.Id || list.Endpoints[0].RotatedAt == nil {
		t.Errorf("list = %+v; want ci-hooks, rotated", list.Endpoints)
	}
}

// rotateHook rotates created's secret and checks the answer holds a new one, the one that opens.
func (h *harness) rotateHook(t *testing.T, created gen.CreatedWebhookEndpoint) gen.CreatedWebhookEndpoint {
	t.Helper()

	res := h.request(t, http.MethodPost, webhooksPath+"/"+created.Id+"/rotate", "", nil)
	rotated := decode[gen.CreatedWebhookEndpoint](t, res, http.StatusOK)

	if !hexSecret.MatchString(rotated.Secret) || rotated.Secret == created.Secret || rotated.RotatedAt == nil || rotated.Id != created.Id {
		t.Fatalf("rotated = %+v; want a new 64-character hex secret", rotated)
	}

	if strings.Contains(string(res.body), created.Secret) {
		t.Error("the rotate answer holds the old secret")
	}

	opened, err := h.intake.OpenSecret(inOrg(t), domain.WebhookEndpointID(created.Id))
	if err != nil || hex.EncodeToString(opened) != rotated.Secret {
		t.Errorf("OpenSecret after rotate = %x, %v; want the new secret", opened, err)
	}

	return rotated
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

// Requests the webhook rules refuse answer their codes, and create nothing.
func TestWebhooks_CreateRefusals(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	created := h.createHook(t)

	tests := []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{"a taken name", http.MethodPost, webhooksPath, `{"name":"ci-hooks","kind":"github"}`, http.StatusConflict, "webhook-name-taken"},
		{"not a slug", http.MethodPost, webhooksPath, `{"name":"CI Hooks","kind":"generic"}`, http.StatusBadRequest, codeHookInvalid},
		{"unknown kind", http.MethodPost, webhooksPath, `{"name":"deploys","kind":"gitlab"}`, http.StatusBadRequest, codeHookInvalid},
		{"rotate of no endpoint", http.MethodPost, webhooksPath + "/019a0000-0000-7000-8000-000000000000/rotate", "", http.StatusNotFound, codeHookNotFound},
		{"another factory's endpoint", http.MethodPost, lineWebhooks + "/" + created.Id + "/rotate", "", http.StatusNotFound, codeHookNotFound},
		{"delete through another factory", http.MethodDelete, lineWebhooks + "/" + created.Id, "", http.StatusNotFound, codeHookNotFound},
		{"deliveries of no endpoint", http.MethodGet, webhooksPath + "/nothing/deliveries", "", http.StatusNotFound, codeHookNotFound},
		{
			"no such delivery", http.MethodGet, webhooksPath + "/" + created.Id + "/deliveries/019a0000-0000-7000-8000-000000000000", "",
			http.StatusNotFound, "webhook-delivery-not-found",
		},
		{"a cursor of nothing", http.MethodGet, webhooksPath + "/" + created.Id + "/deliveries?after=nope", "", http.StatusBadRequest, codeHookInvalid},
	}

	for _, tc := range tests {
		refused(t, tc.name, h.request(t, tc.method, tc.path, tc.body, nil), tc.status, tc.code)
	}

	list := decode[gen.WebhookEndpointList](t, h.request(t, http.MethodGet, webhooksPath, "", nil), http.StatusOK)
	if len(list.Endpoints) != 1 {
		t.Errorf("endpoints after the refusals = %+v; want ci-hooks alone", list.Endpoints)
	}

	if r := h.request(t, http.MethodDelete, webhooksPath+"/"+created.Id, "", nil); r.status != http.StatusNoContent {
		t.Fatalf("delete = %d %s; want 204", r.status, r.body)
	}

	refused(t, "deliveries after delete", h.request(t, http.MethodGet, webhooksPath+"/"+created.Id+"/deliveries", "", nil), http.StatusNotFound, codeHookNotFound)
}

// The delivery log lists newest first a page at a time; one delivery's request has its headers
// without a signature and its body, base64 when it is not UTF-8; a created delivery names its
// item; the list counts the last 7 UTC days.
func TestWebhooks_DeliveriesPage(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	created := h.createHook(t)
	it := h.submit(t)

	signed := map[string]string{"X-Goal-Signature": "sha256=00", "X-Hub-Signature-256": "sha256=00", contentType: "application/json"}
	first := h.deliver(t, created.Id, domain.NewWebhookDelivery{ReceivedAt: t0.Add(-3 * 24 * time.Hour), Headers: signed, Body: []byte{0xff, 0xfe}})
	second := h.deliver(t, created.Id, domain.NewWebhookDelivery{
		ReceivedAt: t0.Add(-time.Hour), Outcome: domain.DeliveryRefused, Reason: "signature did not match",
	})
	third := h.deliver(t, created.Id, domain.NewWebhookDelivery{
		ReceivedAt: t0, Headers: signed, Body: []byte(`{"event":"deploy.failed"}`), Outcome: domain.DeliveryCreated, ItemID: domain.ItemID(it.Id),
	})
	h.deliver(t, created.Id, domain.NewWebhookDelivery{ReceivedAt: t0.Add(-8 * 24 * time.Hour)}) // before the 7 days

	deliveries := webhooksPath + "/" + created.Id + "/deliveries"
	page := decode[gen.WebhookDeliveryPage](t, h.request(t, http.MethodGet, deliveries+"?limit=2", "", nil), http.StatusOK)

	if got := deliveryIDs(page); !slices.Equal(got, []string{string(third.ID), string(second.ID)}) || page.NextAfter == nil {
		t.Fatalf("first page = %v, next %v; want the two newest and a cursor", got, page.NextAfter)
	}

	if item := page.Deliveries[0].Item; item == nil || item.Id != it.Id || item.Number != it.Number {
		t.Errorf("created delivery's item = %+v; want #%d", item, it.Number)
	}

	if d := page.Deliveries[1]; d.Outcome != gen.WebhookDeliveryOutcome(domain.DeliveryRefused) || d.Reason != "signature did not match" || d.Item != nil {
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

	list := decode[gen.WebhookEndpointList](t, h.request(t, http.MethodGet, webhooksPath, "", nil), http.StatusOK)
	if len(list.Endpoints) != 1 || len(list.Endpoints[0].Days) != 7 {
		t.Fatalf("list = %+v; want ci-hooks with 7 days", list.Endpoints)
	}

	want := map[int]gen.WebhookDeliveryDay{3: {Delivered: 1}, 6: {Delivered: 1, Refused: 1}}

	for i, d := range list.Endpoints[0].Days {
		w := want[i]
		if day := time.Date(2026, 10, 17+i, 0, 0, 0, 0, time.UTC); !d.Day.Equal(day) || d.Delivered != w.Delivered || d.Refused != w.Refused {
			t.Errorf("day %d = %+v; want %s with %d delivered, %d refused", i, d, day.Format(time.DateOnly), w.Delivered, w.Refused)
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

// Anyone who sees the factory reads its endpoints and their delivery log; only the organization's
// owners and admins, with a sign-in from the last 10 minutes, create, rotate and delete them and
// read a delivery's headers and body (webhook:manage, ADR 0040 decision 12). A factory's editor
// holds none of it, a member reaches no organization route, and another organization reaches
// nothing.
func TestWebhooks_Permissions(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	created := decode[gen.CreatedWebhookEndpoint](t, h.as(t, freshToken, http.MethodPost, webhooksPath, hookBody), http.StatusCreated)
	endpoint := webhooksPath + "/" + created.Id
	d := h.deliver(t, created.Id, domain.NewWebhookDelivery{ReceivedAt: t0, Body: []byte(`{"event":"deploy"}`)})

	decode[gen.WebhookEndpointList](t, h.as(t, freshViewerToken, http.MethodGet, webhooksPath, ""), http.StatusOK)
	decode[gen.WebhookDeliveryPage](t, h.as(t, freshViewerToken, http.MethodGet, endpoint+"/deliveries", ""), http.StatusOK)
	decode[gen.WebhookDeliveryRequest](t, h.as(t, freshToken, http.MethodGet, endpoint+"/deliveries/"+string(d.ID), ""), http.StatusOK)

	if err := h.identity.GrantFactoryRole(testOrg, viewerID, acmeID, domain.FactoryEditor); err != nil {
		t.Fatal(err)
	}

	for _, w := range []struct{ method, path, body string }{
		{http.MethodPost, webhooksPath, `{"name":"mine","kind":"generic"}`},
		{http.MethodPost, endpoint + "/rotate", ""},
		{http.MethodDelete, endpoint, ""},
		{http.MethodGet, endpoint + "/deliveries/" + string(d.ID), ""},
		{http.MethodGet, orgHooksPath, ""},
		{http.MethodPost, orgHooksPath, `{"name":"mine","sender":"github"}`},
	} {
		refused(t, "a factory editor's "+w.method+" "+w.path, h.as(t, freshViewerToken, w.method, w.path, w.body), http.StatusForbidden, "forbidden")
	}

	// The viewer of factory line sees nothing of acme's: line is not theirs.
	refused(t, "a viewer's list of another factory", h.as(t, freshViewerToken, http.MethodGet, lineWebhooks, ""), http.StatusNotFound, "factory-not-found")

	for _, path := range []string{endpoint + "/deliveries", orgHooksPath} {
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
	decode[gen.CreatedWebhook](t, h.as(t, freshViewerToken, http.MethodPost, orgHooksPath, `{"name":"mine","sender":"github"}`), http.StatusCreated)

	if r := h.as(t, freshViewerToken, http.MethodDelete, endpoint, ""); r.status != http.StatusNoContent {
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
		{"rotating its secret on the factory route", http.MethodPost, webhooksPath + "/" + w.Id + "/rotate", "", http.StatusBadRequest, codeHookInvalid},
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

// Opening lists a webhook for the factory, limited or not, with its deliveries; closing one
// stops that. A webhook acme's published trigger deploy-failed uses (on webhook.ci-hooks.deploy)
// refuses a close of acme, a rename and a delete with 409 webhook-in-use naming the trigger; one
// no trigger uses is renamed, closed and deleted.
func TestWebhooks_OrgOpenCloseAndInUse(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	w := h.createOrgHook(t, `{"name":"ci-hooks","sender":"github"}`)
	path := orgHooksPath + "/ci-hooks"

	if l := decode[gen.WebhookEndpointList](t, h.request(t, http.MethodGet, webhooksPath, "", nil), http.StatusOK); len(l.Endpoints) != 0 {
		t.Fatalf("acme's endpoints before an opening = %+v; want none", l.Endpoints)
	}

	use := gen.WebhookTrigger{Factory: acmeID, FactoryName: acmeName, Trigger: "deploy-failed"}
	h.openToAcmeAndLine(t, path, use)

	l := decode[gen.WebhookEndpointList](t, h.request(t, http.MethodGet, webhooksPath, "", nil), http.StatusOK)
	if len(l.Endpoints) != 1 || l.Endpoints[0].Sender != gen.WebhookSenderGithub || l.Endpoints[0].Kind != gen.WebhookKindGithub ||
		l.Endpoints[0].SecretName != "webhook-ci-hooks" || !slices.Equal(l.Endpoints[0].Triggers, []string{"deploy-failed"}) {
		t.Errorf("acme's endpoints = %+v; want ci-hooks as github, signed by webhook-ci-hooks, feeding deploy-failed", l.Endpoints)
	}

	h.expectOpenDeliveries(t, w.Id)
	h.expectInUse(t, w.Id, use)

	closed := decode[gen.Webhook](t, h.request(t, http.MethodPatch, path, `{"close":["`+lineID+`"],"enabled":false}`, nil), http.StatusOK)
	if len(closed.Openings) != 1 || closed.Openings[0].Factory != acmeID || closed.Enabled {
		t.Errorf("after closing line = %+v; want open to acme alone, disabled", closed)
	}

	refused(t, "line's deliveries once closed", h.request(t, http.MethodGet, lineWebhooks+"/"+w.Id+"/deliveries", "", nil), http.StatusNotFound, codeHookNotFound)
}

// openToAcmeAndLine opens the webhook at path to acme, limited to acme/api, and to line, and
// checks acme's trigger use is its one use.
func (h *harness) openToAcmeAndLine(t *testing.T, path string, use gen.WebhookTrigger) {
	t.Helper()

	opened := decode[gen.Webhook](t, h.request(t, http.MethodPatch, path,
		`{"open":[{"factory":"`+acmeID+`","repositories":["acme/api"]},{"factory":"`+lineID+`"}]}`, nil), http.StatusOK)

	if len(opened.Openings) != 2 || !slices.Equal(opened.Triggers, []gen.WebhookTrigger{use}) {
		t.Fatalf("opened = %+v; want open to acme and line, used by acme's deploy-failed", opened)
	}

	for _, o := range opened.Openings {
		if o.Factory == acmeID && (o.Repositories == nil || !slices.Equal(*o.Repositories, []string{"acme/api"})) || o.Factory == lineID && o.Repositories != nil {
			t.Errorf("opening = %+v; want acme limited to acme/api and line to none", o)
		}
	}
}

// expectOpenDeliveries checks a delivery to webhook id is listed on its route and on the routes
// of both factories it is open to, and read with its body.
func (h *harness) expectOpenDeliveries(t *testing.T, id string) {
	t.Helper()

	d := h.deliver(t, id, domain.NewWebhookDelivery{ReceivedAt: t0, Body: []byte(`{"action":"completed"}`)})
	path := orgHooksPath + "/" + id

	for _, deliveries := range []string{path + "/deliveries", webhooksPath + "/" + id + "/deliveries", lineWebhooks + "/" + id + "/deliveries"} {
		if got := deliveryIDs(decode[gen.WebhookDeliveryPage](t, h.request(t, http.MethodGet, deliveries, "", nil), http.StatusOK)); !slices.Equal(got, []string{string(d.ID)}) {
			t.Errorf("GET %s = %v; want the delivery", deliveries, got)
		}
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
		{"a delete through the factory route", http.MethodDelete, webhooksPath + "/" + id, ""},
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
