package api_test

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/api"
	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// response is what a request got back.
type response struct {
	status int
	header http.Header
	body   []byte
}

// request sends one request with extra headers and returns the response, its body read.
func (h *harness) request(t *testing.T, method, path, body string, header http.Header) response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, h.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")

	maps.Copy(req.Header, header)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	return response{status: res.StatusCode, header: res.Header, body: raw}
}

// items counts acme's items, as the store holds them.
func (h *harness) items(t *testing.T) int {
	t.Helper()

	got, err := h.store.ListItems(inOrg(t), ports.ItemQuery{FactoryID: acmeID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}

	return len(got)
}

func TestOrgs_ListCreateAndGet(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var created gen.Org
	if code := h.call(t, http.MethodPost, "/v1/orgs", `{"name":"Acme Two","slug":"acme-two"}`, &created); code != http.StatusCreated ||
		created.Slug != "acme-two" || created.Mark != "AT" || created.FactoryCount != 0 {
		t.Fatalf("create = %d %+v, want 201 with the default mark", code, created)
	}

	for _, body := range []string{`{"name":"Again","slug":"acme-two"}`, `{"name":"Reserved","slug":"settings"}`} {
		var p gen.Problem
		if code := h.call(t, http.MethodPost, "/v1/orgs", body, &p); code != http.StatusConflict {
			t.Errorf("create %s = %d %+v, want 409", body, code, p)
		}
	}

	var list gen.OrgList
	if code := h.call(t, http.MethodGet, "/v1/orgs", "", &list); code != http.StatusOK {
		t.Fatalf("list = %d", code)
	}

	names := []string{}
	for _, o := range list.Orgs {
		names = append(names, o.Slug)
	}

	if !slices.Equal(names, []string{"acme", "acme-two", "example-labs"}) {
		t.Errorf("orgs = %v, want every organization by name", names)
	}

	for _, ref := range []string{"acme-two", created.Id} {
		var got gen.Org
		if code := h.call(t, http.MethodGet, "/v1/orgs/"+ref, "", &got); code != http.StatusOK || got.Id != created.Id {
			t.Errorf("get %s = %d %+v, want the organization", ref, code, got)
		}
	}
}

func TestOrgs_UnknownAndDeletedAreTheSame404(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	if err := h.tenancy.DeleteOrg(domain.WithOrg(t.Context(), otherOrg), domain.LocalUser, time.Now()); err != nil {
		t.Fatal(err)
	}

	var bodies []string

	for _, path := range []string{"/v1/orgs/nobody", "/v1/orgs/example-labs", "/v1/orgs/nobody/factories", "/v1/orgs/example-labs/stream"} {
		res := h.request(t, http.MethodGet, path, "", nil)
		if res.status != http.StatusNotFound || res.header.Get("Content-Type") != "application/problem+json" {
			t.Errorf("GET %s = %d %s, want a 404 problem", path, res.status, res.header.Get("Content-Type"))
		}

		bodies = append(bodies, string(res.body))
	}

	var p gen.Problem
	if err := json.Unmarshal([]byte(bodies[0]), &p); err != nil || p.Title != "Organization not found" || p.Code == nil || *p.Code != "org-not-found" {
		t.Errorf("problem = %+v (%v), want Organization not found, org-not-found", p, err)
	}

	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Errorf("body %s differs from %s; unknown and deleted organizations must look the same", b, bodies[0])
		}
	}
}

func TestAPI_AnotherOrgReachesNothingOfAcme(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := h.submit(t)

	for _, path := range []string{
		"/v1/orgs/example-labs/factories/acme/board",
		"/v1/orgs/example-labs/factories/" + acmeID + "/items",
		"/v1/orgs/example-labs/items/" + it.Id,
		"/v1/orgs/example-labs/items/acme%231/events",
	} {
		if code := h.call(t, http.MethodGet, path, "", nil); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}

	if code := h.call(t, http.MethodPost, "/v1/orgs/example-labs/factories/acme/items", submitFix, nil); code != http.StatusNotFound {
		t.Errorf("submit through another organization = %d, want 404", code)
	}

	var list gen.FactoryList
	if code := h.call(t, http.MethodGet, "/v1/orgs/example-labs/factories", "", &list); code != http.StatusOK || len(list.Factories) != 0 {
		t.Errorf("factories of example-labs = %d %+v, want none", code, list)
	}

	if code := h.call(t, http.MethodGet, "/v1/factories", "", nil); code != http.StatusNotFound {
		t.Errorf("an unscoped route = %d, want 404: the routes moved under /v1/orgs/{org}", code)
	}
}

func TestAPI_CrossOriginChangesAreRefused(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	tests := []struct {
		name   string
		method string
		header http.Header
		want   int
	}{
		{name: "another site", method: http.MethodPost, header: http.Header{"Sec-Fetch-Site": {"cross-site"}}, want: http.StatusForbidden},
		{name: "another origin", method: http.MethodPost, header: http.Header{"Origin": {"http://evil.example.com"}}, want: http.StatusForbidden},
		{name: "the same origin", method: http.MethodPost, header: http.Header{"Sec-Fetch-Site": {"same-origin"}}, want: http.StatusCreated},
		{name: "no browser (the CLI)", method: http.MethodPost, want: http.StatusCreated},
		{name: "a read from another site", method: http.MethodGet, header: http.Header{"Sec-Fetch-Site": {"cross-site"}}, want: http.StatusOK},
	}

	for _, tt := range tests {
		before := h.items(t)

		res := h.request(t, tt.method, acmeItems, submitFix, tt.header)
		if res.status != tt.want {
			t.Errorf("%s: %d %s, want %d", tt.name, res.status, res.body, tt.want)
		}

		if tt.want == http.StatusForbidden && (h.items(t) != before || !strings.Contains(string(res.body), `"forbidden"`)) {
			t.Errorf("%s: refused with %s and %d items after %d; want a forbidden problem and nothing written",
				tt.name, res.body, h.items(t), before)
		}
	}
}

func TestAPI_RequestIDsAreEchoedAndMadeUp(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	res := h.request(t, http.MethodGet, "/v1/health", "", http.Header{"X-Request-Id": {"req-42"}})
	if got := res.header.Get("X-Request-Id"); got != "req-42" {
		t.Errorf("X-Request-Id = %q, want the caller's", got)
	}

	res = h.request(t, http.MethodGet, "/v1/health", "", http.Header{"X-Request-Id": {"bad id with spaces"}})
	if got := res.header.Get("X-Request-Id"); len(got) != 32 {
		t.Errorf("X-Request-Id = %q, want a new one in place of a malformed id", got)
	}
}

// probe is one representative request of an operation.
type probe struct {
	method, path, body string
	org                domain.OrgID
	// factory is the factory the operation's resolver must pass the seam with ("" for none).
	factory domain.FactoryID
}

// probes has a request for every operation of api/openapi.yaml; the item is acme#1.
// submitFix is a valid submission to acme.
const submitFix = `{"title":"t","type":"fix"}`

var probes = map[string]probe{
	"getHealth": {method: http.MethodGet, path: "/v1/health"},
	"listOrgs":  {method: http.MethodGet, path: "/v1/orgs"},
	"createOrg": {method: http.MethodPost, path: "/v1/orgs", body: `{"name":"Acme Two","slug":"acme-two"}`},
	"getOrg":    {method: http.MethodGet, path: "/v1/orgs/acme", org: testOrg},
	// A wrong confirmation, so the probe deletes nothing the later probes need.
	"deleteOrg":          {method: http.MethodDelete, path: "/v1/orgs/acme", body: `{"confirm":"nope"}`, org: testOrg},
	"getOrgSettings":     {method: http.MethodGet, path: "/v1/orgs/acme/settings", org: testOrg},
	"updateOrgSettings":  {method: http.MethodPatch, path: "/v1/orgs/acme/settings", body: `{"mark":"AC"}`, org: testOrg},
	"listAudit":          {method: http.MethodGet, path: "/v1/orgs/acme/audit?factory=acme", org: testOrg, factory: acmeID},
	"listFactories":      {method: http.MethodGet, path: "/v1/orgs/acme/factories", org: testOrg},
	"getOrgAttention":    {method: http.MethodGet, path: "/v1/orgs/acme/attention", org: testOrg},
	"getFactoryOverview": {method: http.MethodGet, path: "/v1/orgs/acme/overview", org: testOrg},
	"getInbox":           {method: http.MethodGet, path: "/v1/orgs/acme/inbox?factory=acme", org: testOrg, factory: acmeID},
	"listItems":          {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/items", org: testOrg, factory: acmeID},
	"submitItem":         {method: http.MethodPost, path: acmeItems, body: submitFix, org: testOrg, factory: acmeID},
	"getBoard":           {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/board", org: testOrg, factory: acmeID},
	"getMetrics":         {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/metrics", org: testOrg, factory: acmeID},
	"streamEvents":       {method: http.MethodGet, path: "/v1/orgs/acme/stream?factory=acme", org: testOrg, factory: acmeID},
	"getItem":            {method: http.MethodGet, path: "/v1/orgs/acme/items/acme%231", org: testOrg, factory: acmeID},
	"listItemEvents":     {method: http.MethodGet, path: "/v1/orgs/acme/items/acme%231/events", org: testOrg, factory: acmeID},
	"approveItem":        {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/approve", org: testOrg, factory: acmeID},
	"rejectItem":         {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/reject", body: `{"reason":"no"}`, org: testOrg, factory: acmeID},
	"retryItem":          {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/retry", org: testOrg, factory: acmeID},
	"mergeItem":          {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/merge", org: testOrg, factory: acmeID},
	"getBuilderMeta":     {method: http.MethodGet, path: "/v1/orgs/acme/builder/meta", org: testOrg},
	"searchSkills":       {method: http.MethodGet, path: "/v1/orgs/acme/builder/skills?q=lint", org: testOrg},
	"getSkillFiles":      {method: http.MethodGet, path: "/v1/orgs/acme/builder/skills/acme/skills/lint/files", org: testOrg},
}

// operations reads every operationId of the contract.
func operations(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}

	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	var ids []string

	for _, ops := range doc.Paths {
		for _, op := range ops {
			if m, ok := op.(map[string]any); ok {
				if id, ok := m["operationId"].(string); ok {
					ids = append(ids, id)
				}
			}
		}
	}

	slices.Sort(ids)

	return ids
}

func TestAuthorize_EveryOperationPassesTheSeam(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.submit(t)
	h.seam.steps()

	for _, op := range operations(t) {
		p, ok := probes[op]
		if !ok {
			t.Errorf("operation %s has no probe: add one so the seam test covers it", op)
			continue
		}

		if op == "streamEvents" {
			h.stream(t, "?factory=acme")
		} else {
			h.request(t, p.method, p.path, p.body, nil)
		}

		checkSeam(t, op, p, h.seam.steps())
	}
}

// checkSeam checks what one probe of op made the seam see: the middleware's question first, before
// any store read, and the resolver's with the factory when the operation has one.
func checkSeam(t *testing.T, op string, p probe, steps []step) {
	t.Helper()

	if len(steps) == 0 || steps[0].access == nil {
		t.Errorf("%s: first step %+v, want the seam before any store read", op, steps)
		return
	}

	if first := *steps[0].access; first.Operation != op || first.Org != p.org || first.Factory != "" || first.Actor != domain.LocalUser {
		t.Errorf("%s: the middleware asked %+v, want %s in org %q by the local user", op, first, op, p.org)
	}

	resolved := slices.ContainsFunc(steps, func(s step) bool {
		return s.access != nil && s.access.Operation == op && s.access.Factory == p.factory
	})
	if p.factory != "" && !resolved {
		t.Errorf("%s: steps %+v, want the resolver to pass the seam with factory %s", op, steps, p.factory)
	}
}

func TestAuthorize_DenialStopsTheRequest(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	tests := []struct {
		name string
		deny func(api.AccessRequest) error
		want int
	}{
		{name: "refused", deny: func(api.AccessRequest) error { return errors.New("no") }, want: http.StatusForbidden},
		{name: "hidden", deny: func(api.AccessRequest) error { return ports.ErrNotFound }, want: http.StatusNotFound},
		{name: "refused on the factory", deny: func(r api.AccessRequest) error {
			if r.Factory != "" {
				return errors.New("not this factory")
			}

			return nil
		}, want: http.StatusForbidden},
	}

	for _, tt := range tests {
		h.seam.deny(tt.deny)

		res := h.request(t, http.MethodPost, acmeItems, submitFix, nil)
		if res.status != tt.want || h.items(t) != 0 {
			t.Errorf("%s: submit = %d %s with %d items, want %d and nothing written", tt.name, res.status, res.body, h.items(t), tt.want)
		}
	}
}
