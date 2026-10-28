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
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
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
	if code := h.call(t, http.MethodPost, orgsPath, `{"name":"Acme Two","slug":"acme-two"}`, &created); code != http.StatusCreated ||
		created.Slug != "acme-two" || created.Mark != "AT" || created.FactoryCount != 0 {
		t.Fatalf("create = %d %+v, want 201 with the default mark", code, created)
	}

	for _, body := range []string{`{"name":"Again","slug":"acme-two"}`, `{"name":"Reserved","slug":"settings"}`} {
		var p gen.Problem
		if code := h.call(t, http.MethodPost, orgsPath, body, &p); code != http.StatusConflict {
			t.Errorf("create %s = %d %+v, want 409", body, code, p)
		}
	}

	var list gen.OrgList
	if code := h.call(t, http.MethodGet, orgsPath, "", &list); code != http.StatusOK {
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
	// header is sent with the request (a write's If-Match).
	header http.Header
}

// probes has a request for every operation of contracts/api/openapi.yaml; the item is acme#1.
// submitFix is a valid submission to acme; noDocument is the id of no knowledge document.
const (
	submitFix  = `{"title":"t","type":"fix"}`
	noDocument = "00000000-0000-7000-8000-fffffffffffd"
	// probeHooks are acme's webhook endpoints.
	probeHooks = "/v1/orgs/acme/factories/acme/webhooks"
	// probeViews are acme's saved views.
	probeViews = "/v1/orgs/acme/factories/acme/views"
)

var probes = map[string]probe{
	"getHealth":        {method: http.MethodGet, path: "/v1/health"},
	"getAuthConfig":    {method: http.MethodGet, path: "/v1/auth/config"},
	"getMe":            {method: http.MethodGet, path: "/v1/me"},
	"getDeviceRequest": {method: http.MethodGet, path: "/v1/auth/device/ABCD-EFGH"},
	"listOrgs":         {method: http.MethodGet, path: orgsPath},
	"createOrg":        {method: http.MethodPost, path: orgsPath, body: `{"name":"Acme Two","slug":"acme-two"}`},

	"listPlatformAdmins":  {method: http.MethodGet, path: "/v1/platform/admins"},
	"addPlatformAdmin":    {method: http.MethodPost, path: "/v1/platform/admins", body: `{"email":"nobody@example.com"}`},
	"removePlatformAdmin": {method: http.MethodDelete, path: "/v1/platform/admins/019a3002-0000-7000-8000-000000000001"},
	"listAuthEvents":      {method: http.MethodGet, path: "/v1/platform/auth-events?kind=sign_in.succeeded"},

	"getOrg": {method: http.MethodGet, path: "/v1/orgs/acme", org: testOrg},
	// A wrong confirmation, so the probe deletes nothing the later probes need.
	"deleteOrg":           {method: http.MethodDelete, path: "/v1/orgs/acme", body: `{"confirm":"nope"}`, org: testOrg},
	"getOrgSettings":      {method: http.MethodGet, path: "/v1/orgs/acme/settings", org: testOrg},
	"listMembers":         {method: http.MethodGet, path: "/v1/orgs/acme/members", org: testOrg},
	"listInvitations":     {method: http.MethodGet, path: "/v1/orgs/acme/invitations", org: testOrg},
	"setFactoryRole":      {method: http.MethodPut, path: "/v1/orgs/acme/members/x/factory-roles/acme", body: viewerRole, org: testOrg, factory: acmeID},
	"removeFactoryRole":   {method: http.MethodDelete, path: "/v1/orgs/acme/members/x/factory-roles/acme", org: testOrg, factory: acmeID},
	"transferOwnership":   {method: http.MethodPost, path: "/v1/orgs/acme/transfer", body: `{"to":"x","confirm":"nope"}`, org: testOrg},
	"updateOrgSettings":   {method: http.MethodPatch, path: "/v1/orgs/acme/settings", body: `{"mark":"AC"}`, org: testOrg},
	"getOrgPolicy":        {method: http.MethodGet, path: "/v1/orgs/acme/policy", org: testOrg},
	"putOrgPolicy":        {method: http.MethodPut, path: "/v1/orgs/acme/policy", body: `{}`, org: testOrg, header: staleMatch},
	"listAudit":           {method: http.MethodGet, path: "/v1/orgs/acme/audit?factory=acme", org: testOrg, factory: acmeID},
	"listFactories":       {method: http.MethodGet, path: "/v1/orgs/acme/factories", org: testOrg},
	"getOrgAttention":     {method: http.MethodGet, path: "/v1/orgs/acme/attention", org: testOrg},
	"getFactoryOverview":  {method: http.MethodGet, path: "/v1/orgs/acme/overview", org: testOrg},
	"searchOrg":           {method: http.MethodGet, path: "/v1/orgs/acme/search?q=acme", org: testOrg},
	"getOrgSpend":         {method: http.MethodGet, path: "/v1/orgs/acme/spend", org: testOrg},
	"getInbox":            {method: http.MethodGet, path: "/v1/orgs/acme/inbox?factory=acme", org: testOrg, factory: acmeID},
	"listRuns":            {method: http.MethodGet, path: "/v1/orgs/acme/runs?factory=acme", org: testOrg, factory: acmeID},
	"exportRuns":          {method: http.MethodGet, path: "/v1/orgs/acme/runs/export?factory=acme", org: testOrg, factory: acmeID},
	"exportSpend":         {method: http.MethodGet, path: "/v1/orgs/acme/spend/export", org: testOrg},
	"listItems":           {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/items", org: testOrg, factory: acmeID},
	"submitItem":          {method: http.MethodPost, path: acmeItems, body: submitFix, org: testOrg, factory: acmeID},
	"getBoard":            {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/board", org: testOrg, factory: acmeID},
	"getMetrics":          {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/metrics", org: testOrg, factory: acmeID},
	"listFactoryActivity": {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/activity", org: testOrg, factory: acmeID},
	"getConstraintReport": {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/constraint", org: testOrg, factory: acmeID},
	"getLeadTimeBridge":   {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/metrics/lead-time", org: testOrg, factory: acmeID},
	"getWeeklyThroughput": {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/metrics/weekly", org: testOrg, factory: acmeID},
	"getTouchMetrics":     {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/metrics/touches", org: testOrg, factory: acmeID},
	"getIntake":           {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/intake", org: testOrg, factory: acmeID},
	"getIntakeTrigger":    {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/intake/triggers/nightly-audit", org: testOrg, factory: acmeID},
	"tryIntakeTrigger": {
		method: http.MethodPost, path: "/v1/orgs/acme/factories/acme/intake/triggers/nightly-audit/try", body: `{"firing":"no-such-firing"}`,
		org: testOrg, factory: acmeID,
	},
	"getOrgTouchMetrics": {method: http.MethodGet, path: "/v1/orgs/acme/metrics/touches", org: testOrg},
	"setTouchReason": {
		method: http.MethodPut, path: "/v1/orgs/acme/items/acme%231/touches/1/reason", body: `{"reason":"other"}`, org: testOrg, factory: acmeID,
	},
	"streamEvents":       {method: http.MethodGet, path: "/v1/orgs/acme/stream?factory=acme", org: testOrg, factory: acmeID},
	"getItem":            {method: http.MethodGet, path: "/v1/orgs/acme/items/acme%231", org: testOrg, factory: acmeID},
	"listItemEvents":     {method: http.MethodGet, path: "/v1/orgs/acme/items/acme%231/events", org: testOrg, factory: acmeID},
	"listItemParts":      {method: http.MethodGet, path: "/v1/orgs/acme/items/acme%231/parts", org: testOrg, factory: acmeID},
	"addItemPart":        {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/parts", body: `{"title":"a","brief":"b"}`, org: testOrg, factory: acmeID},
	"approveItem":        {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/approve", org: testOrg, factory: acmeID},
	"answerItem":         {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/answer", body: `{"choice":0}`, org: testOrg, factory: acmeID},
	"decideToolApproval": {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/tool-approval", body: `{"decision":"deny"}`, org: testOrg, factory: acmeID},
	"rejectItem":         {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/reject", body: `{"reason":"no"}`, org: testOrg, factory: acmeID},
	"retryItem":          {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/retry", org: testOrg, factory: acmeID},
	"moveItem":           {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/move", body: `{"to":"approve"}`, org: testOrg, factory: acmeID},
	"setItemLane":        {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/lane", body: `{"lane":"docs"}`, org: testOrg, factory: acmeID},
	"pauseItem":          {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/pause", org: testOrg, factory: acmeID},
	"cancelItem":         {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/cancel", org: testOrg, factory: acmeID},
	"getAgentStats":      {method: http.MethodGet, path: "/v1/orgs/acme/factories/acme/agents/writer/stats", org: testOrg, factory: acmeID},
	"listEvals":          {method: http.MethodGet, path: acmeEvals, org: testOrg, factory: acmeID},
	"startEvalRun":       {method: http.MethodPost, path: acmeEvals + "/runs", body: `{}`, org: testOrg, factory: acmeID},
	"estimateEvalRun":    {method: http.MethodPost, path: acmeEvals + "/estimate", body: `{}`, org: testOrg, factory: acmeID},
	"getEvalRun":         {method: http.MethodGet, path: acmeEvals + "/runs/probe-run", org: testOrg, factory: acmeID},
	"cancelEvalRun":      {method: http.MethodPost, path: acmeEvals + "/runs/probe-run/cancel", org: testOrg, factory: acmeID},
	"addEvalCase":        {method: http.MethodPost, path: acmeEvals + "/cases", body: `{"run":"probe-run","name":"probe-case"}`, org: testOrg, factory: acmeID},
	"listJudges":         {method: http.MethodGet, path: acmeJudges, org: testOrg, factory: acmeID},
	"createJudge":        {method: http.MethodPost, path: acmeJudges, body: `{"slug":"probe-judge"}`, org: testOrg, factory: acmeID},
	"getJudge":           {method: http.MethodGet, path: acmeJudges + "/probe-judge", org: testOrg, factory: acmeID},
	"updateJudge":        {method: http.MethodPut, path: acmeJudges + "/probe-judge", body: `{}`, org: testOrg, factory: acmeID},
	"tryJudge":           {method: http.MethodPost, path: acmeJudges + "/probe-judge/try", org: testOrg, factory: acmeID},
	"mergeItem":          {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/merge", org: testOrg, factory: acmeID},
	"noteItem":           {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/notes", body: `{"text":"hi"}`, org: testOrg, factory: acmeID},
	"linkItem":           {method: http.MethodPost, path: "/v1/orgs/acme/items/acme%231/needs", body: `{"needs":"acme#1"}`, org: testOrg, factory: acmeID},
	"unlinkItem":         {method: http.MethodDelete, path: "/v1/orgs/acme/items/acme%231/needs/acme%231", org: testOrg, factory: acmeID},
	"listItemRuns":       {method: http.MethodGet, path: "/v1/orgs/acme/items/acme%231/runs", org: testOrg, factory: acmeID},
	"getAndon":           {method: http.MethodGet, path: acmeAndon, org: testOrg, factory: acmeID},
	"pullAndon":          {method: http.MethodPost, path: acmeAndon, body: `{"reason":"probe"}`, org: testOrg, factory: acmeID},
	"clearAndon":         {method: http.MethodPost, path: acmeAndon + "/clear", org: testOrg, factory: acmeID},
	// No run exists here; TestAPI_RunsOfAnItem checks that the run resolver passes the seam with its factory.
	"getRun":           {method: http.MethodGet, path: "/v1/orgs/acme/runs/00000000-0000-7000-8000-ffffffffffff", org: testOrg},
	"listRunKnowledge": {method: http.MethodGet, path: "/v1/orgs/acme/runs/00000000-0000-7000-8000-ffffffffffff/knowledge", org: testOrg},
	"getBuilderMeta":   {method: http.MethodGet, path: "/v1/orgs/acme/builder/meta", org: testOrg},
	"searchSkills":     {method: http.MethodGet, path: "/v1/orgs/acme/builder/skills?q=lint", org: testOrg},
	"getSkillFiles":    {method: http.MethodGet, path: "/v1/orgs/acme/builder/skills/acme/skills/lint/files", org: testOrg},
	"listAPIKeys":      {method: http.MethodGet, path: "/v1/orgs/acme/api-keys", org: testOrg},

	"listConnectionKinds":  {method: http.MethodGet, path: "/v1/orgs/acme/connection-kinds", org: testOrg},
	"putConnectionKind":    {method: http.MethodPut, path: "/v1/orgs/acme/connection-kinds/acme-api", body: `{}`, org: testOrg},
	"deleteConnectionKind": {method: http.MethodDelete, path: "/v1/orgs/acme/connection-kinds/acme-api", org: testOrg},
	"listConnections":      {method: http.MethodGet, path: "/v1/orgs/acme/connections?factory=acme", org: testOrg, factory: acmeID},
	"createConnection": {
		method: http.MethodPost, path: "/v1/orgs/acme/connections", org: testOrg, body: `{"name":"docs","kind":"mcp-server"}`,
	},
	"getConnection":              {method: http.MethodGet, path: "/v1/orgs/acme/connections/docs", org: testOrg},
	"updateConnection":           {method: http.MethodPatch, path: "/v1/orgs/acme/connections/docs", body: `{"description":"d"}`, org: testOrg},
	"testConnection":             {method: http.MethodPost, path: "/v1/orgs/acme/connections/docs/test", org: testOrg},
	"listConnectionRepositories": {method: http.MethodGet, path: "/v1/orgs/acme/connections/docs/repositories", org: testOrg},
	"deleteConnection":           {method: http.MethodDelete, path: "/v1/orgs/acme/connections/docs", org: testOrg},
	"listStandingRules":          {method: http.MethodGet, path: "/v1/orgs/acme/standing-rules", org: testOrg},
	"revokeStandingRule":         {method: http.MethodDelete, path: "/v1/orgs/acme/standing-rules/019a4000-0000-7000-8000-0000000000ff", org: testOrg},
	"listCredentials":            {method: http.MethodGet, path: "/v1/orgs/acme/credentials", org: testOrg},
	"createCredential": {
		method: http.MethodPost, path: "/v1/orgs/acme/credentials", org: testOrg, body: `{"name":"acme-token","type":"api_key","value":"v"}`,
	},
	"getCredential":     {method: http.MethodGet, path: "/v1/orgs/acme/credentials/acme-token", org: testOrg},
	"patchCredential":   {method: http.MethodPatch, path: "/v1/orgs/acme/credentials/acme-token", body: `{"description":"rotated"}`, org: testOrg},
	"deleteCredential":  {method: http.MethodDelete, path: "/v1/orgs/acme/credentials/acme-token", org: testOrg},
	"importCredentials": {method: http.MethodPost, path: "/v1/orgs/acme/credentials/import", body: `{"format":"env","text":"A_B=c","dry_run":true}`, org: testOrg},

	"listWebhookEndpoints": {method: http.MethodGet, path: probeHooks, org: testOrg, factory: acmeID},
	"createWebhookEndpoint": {
		method: http.MethodPost, path: probeHooks, body: `{"name":"ci-hooks","kind":"generic"}`, org: testOrg, factory: acmeID,
	},
	"rotateWebhookSecret":   {method: http.MethodPost, path: probeHooks + "/" + noDocument + "/rotate", org: testOrg, factory: acmeID},
	"deleteWebhookEndpoint": {method: http.MethodDelete, path: probeHooks + "/" + noDocument, org: testOrg, factory: acmeID},
	"listWebhookDeliveries": {method: http.MethodGet, path: probeHooks + "/" + noDocument + "/deliveries", org: testOrg, factory: acmeID},
	"getWebhookDelivery": {
		method: http.MethodGet, path: probeHooks + "/" + noDocument + "/deliveries/" + noDocument, org: testOrg, factory: acmeID,
	},
	"listSavedViews": {method: http.MethodGet, path: probeViews, org: testOrg, factory: acmeID},
	"createSavedView": {
		method: http.MethodPost, path: probeViews, body: `{"name":"mine","shared":false,"filters":{"status":["open"]}}`, org: testOrg, factory: acmeID,
	},
	"updateSavedView": {
		method: http.MethodPatch, path: probeViews + "/" + noDocument, body: `{"name":"mine","filters":{}}`, org: testOrg, factory: acmeID,
	},
	"deleteSavedView":      {method: http.MethodDelete, path: probeViews + "/" + noDocument, org: testOrg, factory: acmeID},
	"listKnowledgeSources": {method: http.MethodGet, path: "/v1/orgs/acme/knowledge/sources?factory=acme", org: testOrg, factory: acmeID},
	"createKnowledgeSource": {
		method: http.MethodPost, path: "/v1/orgs/acme/knowledge/sources", org: testOrg, body: `{"name":"notes","title":"Notes","kind":"upload"}`,
	},
	"getKnowledgeSource":     {method: http.MethodGet, path: "/v1/orgs/acme/knowledge/sources/notes", org: testOrg},
	"updateKnowledgeSource":  {method: http.MethodPatch, path: "/v1/orgs/acme/knowledge/sources/notes", body: `{"title":"t"}`, org: testOrg},
	"syncKnowledgeSource":    {method: http.MethodPost, path: "/v1/orgs/acme/knowledge/sources/notes/sync", org: testOrg},
	"listKnowledgeDocuments": {method: http.MethodGet, path: "/v1/orgs/acme/knowledge/sources/notes/documents", org: testOrg},
	"uploadKnowledgeDocuments": {
		method: http.MethodPost, path: "/v1/orgs/acme/knowledge/sources/notes/documents", org: testOrg,
		header: http.Header{"Content-Type": {"multipart/form-data; boundary=x"}},
		body:   "--x\r\nContent-Disposition: form-data; name=\"files\"; filename=\"a.md\"\r\n\r\n# A\r\n--x--\r\n",
	},
	"getKnowledgeDocument":        {method: http.MethodGet, path: "/v1/orgs/acme/knowledge/documents/" + noDocument, org: testOrg},
	"getKnowledgeDocumentContent": {method: http.MethodGet, path: "/v1/orgs/acme/knowledge/documents/" + noDocument + "/content", org: testOrg},
	"deleteKnowledgeDocument":     {method: http.MethodDelete, path: "/v1/orgs/acme/knowledge/documents/" + noDocument, org: testOrg},
	"searchKnowledge":             {method: http.MethodGet, path: "/v1/orgs/acme/knowledge/search?q=notes", org: testOrg},
	"getKnowledgeUsage":           {method: http.MethodGet, path: "/v1/orgs/acme/knowledge/usage?source=notes", org: testOrg},
	"listKnowledgeDocumentUses":   {method: http.MethodGet, path: "/v1/orgs/acme/knowledge/documents/" + noDocument + "/uses", org: testOrg},
	"giveKnowledgeVerdict": {
		method: http.MethodPut, path: "/v1/orgs/acme/knowledge/documents/" + noDocument + "/verdict", org: testOrg, body: `{"verdict":"helped"}`,
	},
	"deleteKnowledgeSource": {method: http.MethodDelete, path: "/v1/orgs/acme/knowledge/sources/notes", org: testOrg},
	// The key's factory passes the resolver; no key has this id.
	"createAPIKey": {
		method: http.MethodPost, path: "/v1/orgs/acme/api-keys", org: testOrg, factory: acmeID,
		body: `{"name":"ci","scopes":["factory:read"],"role_ceiling":"viewer","factories":["acme"]}`,
	},
	"revokeAPIKey":       {method: http.MethodDelete, path: "/v1/orgs/acme/api-keys/00000000-0000-7000-8000-ffffffffffff", org: testOrg},
	"listPersonalTokens": {method: http.MethodGet, path: "/v1/orgs/acme/personal-tokens", org: testOrg},
	"createPersonalToken": {
		method: http.MethodPost, path: "/v1/orgs/acme/personal-tokens", org: testOrg, body: `{"name":"laptop","scopes":["org:read"]}`,
	},
	"revokePersonalToken": {method: http.MethodDelete, path: "/v1/orgs/acme/personal-tokens/00000000-0000-7000-8000-ffffffffffff", org: testOrg},
	"listMemberTokens":    {method: http.MethodGet, path: "/v1/orgs/acme/member-tokens", org: testOrg},
	"revokeMemberToken":   {method: http.MethodDelete, path: "/v1/orgs/acme/member-tokens/00000000-0000-7000-8000-ffffffffffff", org: testOrg},

	// Definitions: writes carry a (stale) If-Match, so the request reaches the resolver.
	"createFactory":      {method: http.MethodPost, path: "/v1/orgs/acme/factories", body: `{"name":"probe","start":{"kind":"blank"}}`, org: testOrg},
	"getFactory":         {method: http.MethodGet, path: acmeFactory, org: testOrg, factory: acmeID},
	opDelete:             {method: http.MethodDelete, path: acmeFactory, org: testOrg, factory: acmeID},
	"pauseFactory":       {method: http.MethodPost, path: acmeFactory + "/pause", org: testOrg, factory: acmeID},
	"resumeFactory":      {method: http.MethodPost, path: acmeFactory + "/resume", org: testOrg, factory: acmeID},
	opArchive:            {method: http.MethodPost, path: acmeFactory + "/archive", org: testOrg, factory: acmeID},
	opUnarchive:          {method: http.MethodPost, path: acmeFactory + "/unarchive", org: testOrg, factory: acmeID},
	opPurge:              {method: http.MethodPost, path: acmeFactory + "/purge", org: testOrg, factory: acmeID},
	"getDraft":           {method: http.MethodGet, path: acmeDraft, org: testOrg, factory: acmeID},
	"openDraft":          {method: http.MethodPost, path: acmeDraft, body: `{}`, org: testOrg, factory: acmeID},
	"discardDraft":       {method: http.MethodDelete, path: acmeDraft, org: testOrg, factory: acmeID, header: staleMatch},
	"getDraftModel":      {method: http.MethodGet, path: acmeDraft + "/model", org: testOrg, factory: acmeID},
	"saveDraftSection":   {method: http.MethodPut, path: acmeDraft + "/model/basics", body: `{"value":{}}`, org: testOrg, factory: acmeID, header: staleMatch},
	"revertDraftSection": {method: http.MethodPost, path: acmeDraft + "/model/basics/revert", body: `{"pointers":[]}`, org: testOrg, factory: acmeID, header: staleMatch},
	"listDraftFiles":     {method: http.MethodGet, path: acmeDraft + "/files", org: testOrg, factory: acmeID},
	"getDraftFile":       {method: http.MethodGet, path: acmeDraft + "/files/factory.yaml", org: testOrg, factory: acmeID},
	"putDraftFile":       {method: http.MethodPut, path: acmeDraft + "/files/factory.yaml", body: "x", org: testOrg, factory: acmeID, header: staleMatch},
	"deleteDraftFile":    {method: http.MethodDelete, path: acmeDraft + "/files/factory.yaml", org: testOrg, factory: acmeID, header: staleMatch},
	"replaceDraftTree":   {method: http.MethodPut, path: acmeDraft + "/tree", body: `{"files":{}}`, org: testOrg, factory: acmeID},
	"validateDraft":      {method: http.MethodPost, path: acmeDraft + "/validate", body: `{}`, org: testOrg, factory: acmeID},
	"publishDraft":       {method: http.MethodPost, path: acmeDraft + "/publish", body: `{"revision":1,"expected_current_version_id":null}`, org: testOrg, factory: acmeID},
	"listVersions":       {method: http.MethodGet, path: acmeFactory + "/versions", org: testOrg, factory: acmeID},
	"getVersion":         {method: http.MethodGet, path: acmeFactory + "/versions/1", org: testOrg, factory: acmeID},
	"getVersionModel":    {method: http.MethodGet, path: acmeFactory + "/versions/1/model", org: testOrg, factory: acmeID},
	"getVersionFile":     {method: http.MethodGet, path: acmeFactory + "/versions/1/files/factory.yaml", org: testOrg, factory: acmeID},
	"restoreVersion":     {method: http.MethodPost, path: acmeFactory + "/versions/1/restore", body: `{"expected_current_version_id":"x"}`, org: testOrg, factory: acmeID},
	"compareVersions":    {method: http.MethodGet, path: acmeFactory + "/compare?from=1&to=current", org: testOrg, factory: acmeID},
	"listTemplates":      {method: http.MethodGet, path: "/v1/orgs/acme/templates", org: testOrg},
	"getTemplate":        {method: http.MethodGet, path: "/v1/orgs/acme/templates/blank", org: testOrg},
	"validateDefinition": {method: http.MethodPost, path: "/v1/orgs/acme/definitions/validate", body: `{"files":{}}`, org: testOrg},
	"formatDefinition":   {method: http.MethodPost, path: "/v1/orgs/acme/definitions/format", body: "a: b\n", org: testOrg, header: yamlBody},
}

// Paths and headers of the definitions probes.
const (
	acmeFactory = "/v1/orgs/acme/factories/acme"
	acmeDraft   = acmeFactory + "/draft"
)

var (
	staleMatch = http.Header{ifMatchHeader: {`"1"`}}
	yamlBody   = http.Header{"Content-Type": {"text/yaml"}}
)

// operations reads every operationId of the contract, failing for one without an x-goal-scope
// (the permission it needs, contracts/api/permissions.yaml).
func operations(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile("../../contracts/api/openapi.yaml")
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
					if scope, _ := m["x-goal-scope"].(string); scope == "" {
						t.Errorf("operation %s has no x-goal-scope: give it one of contracts/api/permissions.yaml", id)
					}

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
			h.request(t, p.method, p.path, p.body, p.header)
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

	if first := *steps[0].access; first.Operation != op || first.Org != p.org || first.Factory != "" || first.Principal.Actor() != domain.LocalUser {
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
