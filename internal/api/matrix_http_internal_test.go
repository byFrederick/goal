package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

// mxReached is the status the HTTP matrix's server answers when a request got past every
// authorization question to a collaborator the matrix does not provide: the handler ran.
const mxReached = 599

// mxRequest is one operation of the contract as the HTTP matrix sends it, for org's slug.
type mxRequest struct {
	method, path, body, contentType string
}

// mxParam is a parameter of api/openapi.yaml, or a reference to one.
type mxParam struct {
	Ref      string `yaml:"$ref"`
	Name     string `yaml:"name"`
	In       string `yaml:"in"`
	Required bool   `yaml:"required"`
	Schema   struct {
		Type string `yaml:"type"`
	} `yaml:"schema"`
}

// mxOperation is what the HTTP matrix reads of an operation.
type mxOperation struct {
	ID          string    `yaml:"operationId"`
	Parameters  []mxParam `yaml:"parameters"`
	RequestBody *struct {
		Content map[string]yaml.Node `yaml:"content"`
	} `yaml:"requestBody"`
}

// mxBodies are the request bodies a handler checks before it resolves its factory or item: with
// them the request reaches the resolver, so the matrix's answer comes from authorization.
var mxBodies = map[string]string{
	"rejectItem": `{"reason":"not this way"}`, "moveItem": `{"to":"done","reason":"done"}`, "pauseItem": `{"reason":"wait"}`,
	"addItemNote": `{"text":"a note"}`, "pullAndon": `{"reason":"stop"}`, "clearAndon": `{"note":"fixed"}`,
	"submitItem": `{"title":"An item"}`, "deleteOrg": `{"confirm":"Acme"}`, "saveDraftSection": `{"value":{}}`,
	"setFactoryRole": `{"role":"viewer"}`, "transferOwnership": `{"to":"x","confirm":"Acme"}`,
	"setTouchReason": `{"reason":"other"}`,
}

// mxRequests builds every operation of api/openapi.yaml as a request: path parameters filled
// with the matrix's names (the org's factory f, item i1, run r1), a factory query for operations
// that take one, and a body of the right kind.
func mxRequests(t *testing.T, org string) map[string]mxRequest {
	t.Helper()

	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Paths      map[string]map[string]yaml.Node `yaml:"paths"`
		Components struct {
			Parameters map[string]mxParam `yaml:"parameters"`
		} `yaml:"components"`
	}

	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	resolve := func(p mxParam) mxParam {
		if name, ok := strings.CutPrefix(p.Ref, "#/components/parameters/"); ok {
			return doc.Components.Parameters[name]
		}

		return p
	}

	out := map[string]mxRequest{}

	for path, item := range doc.Paths {
		var shared []mxParam
		if n, ok := item["parameters"]; ok {
			if err := n.Decode(&shared); err != nil {
				t.Fatal(err)
			}
		}

		for method, node := range item {
			var op mxOperation
			if method == "parameters" || node.Decode(&op) != nil || op.ID == "" {
				continue
			}

			params := make([]mxParam, 0, len(shared)+len(op.Parameters))
			for _, p := range slices.Concat(shared, op.Parameters) {
				params = append(params, resolve(p))
			}

			out[op.ID] = mxRequestOf(strings.ToUpper(method), path, op, params, org)
		}
	}

	return out
}

// mxRequestOf is one operation as a request for org's slug, with its resolved parameters.
func mxRequestOf(method, path string, op mxOperation, params []mxParam, org string) mxRequest {
	req := mxRequest{method: method, path: path}
	query := url.Values{}

	for _, p := range params {
		switch {
		case p.In == "path":
			req.path = strings.ReplaceAll(req.path, "{"+p.Name+"}", url.PathEscape(mxValue(p, org)))
		case p.In == "query" && (p.Required || p.Name == "factory"):
			query.Set(p.Name, mxValue(p, org))
		}
	}

	if len(query) > 0 {
		req.path += "?" + query.Encode()
	}

	if op.RequestBody == nil {
		return req
	}

	req.contentType, req.body = "application/json", "{}"
	if _, ok := op.RequestBody.Content["application/json"]; !ok {
		req.contentType, req.body = "application/octet-stream", "x"
	}

	if b, ok := mxBodies[op.ID]; ok {
		req.body = b
	}

	return req
}

// mxValue is a parameter's value in the HTTP matrix.
func mxValue(p mxParam, org string) string {
	switch p.Name {
	case "org":
		return org
	case "factory":
		return "f"
	case "item":
		return "i1"
	case "run":
		return "r1"
	case "path":
		return "agents/a.md"
	case "section":
		return "basics"
	case "version":
		return "1"
	}

	if p.Schema.Type == "integer" {
		return "1"
	}

	return "x"
}

// errReached is what the matrix's stand-ins panic with when a handler goes further than the
// resolvers: the request passed authorization.
var errReached = errors.New("reached the handler")

// mxOrgs knows the matrix's two organizations, acme and other, by slug and id.
type mxOrgs struct{}

func (mxOrgs) Orgs(context.Context) ([]domain.OrgSummary, error) { panic(errReached) }

func (mxOrgs) CreateOrg(context.Context, domain.Actor, ports.NewOrg) (domain.Org, error) {
	panic(errReached)
}

func (mxOrgs) UpdateOrg(context.Context, domain.Actor, ports.OrgUpdate) (domain.Org, error) {
	panic(errReached)
}

func (mxOrgs) DeleteOrg(context.Context, domain.Actor, time.Time) error { panic(errReached) }

func (mxOrgs) FactorySlots(context.Context) (map[domain.FactoryID]int, error) { panic(errReached) }

func (mxOrgs) Org(_ context.Context, ref string) (domain.Org, error) {
	for _, o := range []domain.Org{{ID: mxAcme, Slug: "acme", Name: "Acme"}, {ID: mxOther, Slug: "other", Name: "Other"}} {
		if ref == o.Slug || ref == string(o.ID) {
			return o, nil
		}
	}

	return domain.Org{}, ports.ErrNotFound
}

// mxFactoryOf is the factory f of the organization in ctx: mxF in acme, mxH in the other.
func mxFactoryOf(ctx context.Context) domain.FactoryID {
	org, _ := domain.OrgFrom(ctx)

	return map[domain.OrgID]domain.FactoryID{mxAcme: mxF, mxOther: mxH}[org]
}

// mxFactories resolves factory f, item i1 and run r1 of the organization in ctx, to its factory;
// every other read reaches the nil embedded interface (the handler ran).
type mxFactories struct{}

func (mxFactories) Factories(context.Context) []*config.Factory { panic(errReached) }

func (mxFactories) Lookup(ctx context.Context, ref string) (*config.Factory, error) {
	if ref != "f" && ref != string(mxFactoryOf(ctx)) {
		return nil, ports.ErrNotFound
	}

	f := &config.Factory{}
	f.Metadata.ID, f.Metadata.Name = string(mxFactoryOf(ctx)), "f"

	return f, nil
}

type mxAudit struct{}

func (mxAudit) AuditLog(context.Context, ports.AuditQuery) ([]domain.AuditEntry, error) {
	panic(errReached)
}

// mxPlatform is every platform route's collaborator: reaching it is passing authorization.
type mxPlatform struct{ Platform }

type mxDefinitions struct{ Definitions }

func (mxDefinitions) Factory(ctx context.Context, ref string) (ports.FactoryRecord, error) {
	if ref != "f" && ref != string(mxFactoryOf(ctx)) {
		return ports.FactoryRecord{}, ports.ErrNotFound
	}

	return ports.FactoryRecord{ID: mxFactoryOf(ctx), Name: "f"}, nil
}

type mxItems struct{ Items }

func (mxItems) Item(ctx context.Context, id domain.ItemID) (domain.Item, error) {
	return domain.Item{ID: id, FactoryID: mxFactoryOf(ctx), Number: 1}, nil
}

func (mxItems) Run(ctx context.Context, id domain.RunID) (domain.Run, error) {
	return domain.Run{ID: id, FactoryID: mxFactoryOf(ctx)}, nil
}

// mxServer is the API under GOAL_AUTH=session with the real Authorizer and identity, and stand-ins
// that resolve the matrix's organizations, factory, item and run and nothing more: a request that
// passes authorization reaches one of the missing collaborators, which answers mxReached.
func mxServer(t *testing.T, identity Identity) *httptest.Server {
	t.Helper()

	clock := porttest.NewClock(mxNow)

	h, err := New(Deps{
		Engine: struct{ Engine }{}, Items: mxItems{}, Factories: mxFactories{}, Definitions: mxDefinitions{}, Versions: struct{ Versions }{},
		Orgs: mxOrgs{}, Audit: mxAudit{}, Access: struct{ Access }{}, OrgRetention: time.Hour, Authorizer: NewPermissions(clock),
		Auth: AuthConfig{Mode: AuthSession}, Identity: identity, Platform: mxPlatform{}, Feed: NewFeed(porttest.NewItemNotices(), porttest.NewDefinitionNotices(), nil),
		Builder: struct{ Builder }{}, Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				w.WriteHeader(mxReached)
			}
		}()

		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	return srv
}

// mxSend sends one matrix request with the principal's credential and returns its status and,
// for a problem, its body. A stream that opened is not read.
func mxSend(t *testing.T, srv *httptest.Server, r mxRequest, header http.Header) (int, *gen.Problem) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, r.method, srv.URL+r.path, strings.NewReader(r.body))
	if err != nil {
		t.Fatal(err)
	}

	maps.Copy(req.Header, header)

	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}

	req.Header.Set("If-Match", `"1"`)

	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", r.method, r.path, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.Header.Get("Content-Type") != "application/problem+json" {
		return res.StatusCode, nil
	}

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	var p gen.Problem
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("%s %s: %s: %v", r.method, r.path, raw, err)
	}

	return res.StatusCode, &p
}

// runHTTPMatrix sends every operation of the contract, for every principal, in both
// organizations, over HTTP to the API with identity resolving the credentials, and checks each
// answer is the one TestAuthorize_TwoOrgMatrix's golden file holds: a refusal exactly (status and
// body), a pass as a request that reached its handler and was refused by nothing.
func runHTTPMatrix(t *testing.T, identity Identity, principals []mxPrincipal) {
	t.Helper()

	srv := mxServer(t, identity)
	authz := NewPermissions(porttest.NewClock(mxNow))
	contract := contractOps(t)
	requests := map[domain.OrgID]map[string]mxRequest{"": mxRequests(t, ""), mxAcme: mxRequests(t, "acme"), mxOther: mxRequests(t, "other")}

	for _, id := range slices.Sorted(maps.Keys(contract)) {
		op := contract[id]
		orgs := []domain.OrgID{""}

		if strings.HasPrefix(op.path, "/v1/orgs/{org}") {
			orgs = []domain.OrgID{mxAcme, mxOther}
		}

		for _, p := range principals {
			for _, org := range orgs {
				status, got := mxSend(t, srv, requests[org][id], p.header)
				checkHTTPAnswer(t, fmt.Sprintf("%s by %s in %s", id, p.name, org), status, got, mxAnswer(t, authz, id, op, p, org))
			}
		}
	}
}

// checkHTTPAnswer checks one answer over HTTP against the matrix's: a refusal exactly (status and
// body), a pass as anything no authorization refusal looks like.
func checkHTTPAnswer(t *testing.T, where string, status int, got *gen.Problem, want gen.Problem) {
	t.Helper()

	if want.Status != http.StatusOK {
		if status != want.Status || got == nil || !reflect.DeepEqual(*got, want) {
			t.Errorf("%s over HTTP = %d %s, want %s", where, status, mxShow(got), mxShow(&want))
		}

		return
	}

	hidden := []gen.Problem{problemOf(errOrgNotFound), problemOf(errFactoryNotFound), problemOf(errItemNotFound), problemOf(errRunNotFound)}
	refused := status == http.StatusUnauthorized ||
		(got != nil && (slices.ContainsFunc(hidden, func(h gen.Problem) bool { return reflect.DeepEqual(*got, h) }) ||
			(status == http.StatusForbidden && (deref(got.Code) == "forbidden" || deref(got.Code) == "reauth-required"))))

	if refused {
		t.Errorf("%s over HTTP = %d %s, want it to pass authorization", where, status, mxShow(got))
	}
}

// mxShow is a problem in one line, for failures.
func mxShow(p *gen.Problem) string {
	if p == nil {
		return "no problem"
	}

	return fmt.Sprintf("%d %s %s", p.Status, deref(p.Code), deref(p.Detail))
}

// Spec 3 §13: the two-org matrix over HTTP with the Identity fake. 3-2's TestAuthorize_TwoOrgMatrix
// asks the Authorizer; this sends the credentials (session cookies, API keys) through goald's
// credential parsing, principal resolution and resolvers, and must answer the same.
func TestAPI_TwoOrgMatrixOverHTTP(t *testing.T) {
	t.Parallel()

	identity, principals := mxIdentity(t)
	runHTTPMatrix(t, identity, principals)
}
