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
	if !hexSecret.MatchString(created.Secret) || created.Name != hookName || created.Kind != gen.WebhookKindGeneric ||
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
		{"not a slug", http.MethodPost, webhooksPath, `{"name":"CI Hooks","kind":"generic"}`, http.StatusBadRequest, "webhook-invalid"},
		{"unknown kind", http.MethodPost, webhooksPath, `{"name":"deploys","kind":"gitlab"}`, http.StatusBadRequest, "webhook-invalid"},
		{"rotate of no endpoint", http.MethodPost, webhooksPath + "/019a0000-0000-7000-8000-000000000000/rotate", "", http.StatusNotFound, codeHookNotFound},
		{"another factory's endpoint", http.MethodPost, lineWebhooks + "/" + created.Id + "/rotate", "", http.StatusNotFound, codeHookNotFound},
		{"delete through another factory", http.MethodDelete, lineWebhooks + "/" + created.Id, "", http.StatusNotFound, codeHookNotFound},
		{"deliveries of no endpoint", http.MethodGet, webhooksPath + "/nothing/deliveries", "", http.StatusNotFound, codeHookNotFound},
		{
			"no such delivery", http.MethodGet, webhooksPath + "/" + created.Id + "/deliveries/019a0000-0000-7000-8000-000000000000", "",
			http.StatusNotFound, "webhook-delivery-not-found",
		},
		{"a cursor of nothing", http.MethodGet, webhooksPath + "/" + created.Id + "/deliveries?after=nope", "", http.StatusBadRequest, "webhook-invalid"},
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

// Anyone who sees the factory reads its endpoints and deliveries; only its editors, with a sign-in
// from the last 10 minutes, create, rotate and delete them; another organization reaches none.
func TestWebhooks_Permissions(t *testing.T) {
	t.Parallel()

	h := keyed(t)

	created := decode[gen.CreatedWebhookEndpoint](t, h.as(t, freshToken, http.MethodPost, webhooksPath, hookBody), http.StatusCreated)
	endpoint := webhooksPath + "/" + created.Id

	decode[gen.WebhookEndpointList](t, h.as(t, freshViewerToken, http.MethodGet, webhooksPath, ""), http.StatusOK)
	decode[gen.WebhookDeliveryPage](t, h.as(t, freshViewerToken, http.MethodGet, endpoint+"/deliveries", ""), http.StatusOK)

	for _, w := range []struct{ method, path, body string }{
		{http.MethodPost, webhooksPath, `{"name":"mine","kind":"generic"}`},
		{http.MethodPost, endpoint + "/rotate", ""},
		{http.MethodDelete, endpoint, ""},
	} {
		refused(t, "a viewer's "+w.method+" "+w.path, h.as(t, freshViewerToken, w.method, w.path, w.body), http.StatusForbidden, "forbidden")
	}

	// The viewer of factory line sees nothing of acme's: line is not theirs.
	refused(t, "a viewer's list of another factory", h.as(t, freshViewerToken, http.MethodGet, lineWebhooks, ""), http.StatusNotFound, "factory-not-found")

	if res := h.as(t, strangerToken, http.MethodGet, endpoint+"/deliveries", ""); res.status != http.StatusNotFound {
		t.Errorf("another organization's owner reading deliveries = %d; want 404", res.status)
	}

	if err := h.identity.GrantFactoryRole(testOrg, viewerID, acmeID, domain.FactoryEditor); err != nil {
		t.Fatal(err)
	}

	refused(t, "an editor's stale create", h.as(t, viewerToken, http.MethodPost, webhooksPath, `{"name":"mine","kind":"generic"}`),
		http.StatusForbidden, "reauth-required")
	decode[gen.CreatedWebhookEndpoint](t, h.as(t, freshViewerToken, http.MethodPost, webhooksPath, `{"name":"mine","kind":"generic"}`), http.StatusCreated)

	if r := h.as(t, freshViewerToken, http.MethodDelete, endpoint, ""); r.status != http.StatusNoContent {
		t.Errorf("an editor's delete = %d %s; want 204", r.status, r.body)
	}
}
