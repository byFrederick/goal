package intaketest

import (
	"encoding/hex"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
)

// convertTests are the suite's cases of the conversion of factory endpoints to the organization's
// webhooks (ADR 0040 decision 15).
var convertTests = []struct {
	name string
	test func(f *fixture)
}{
	{"Convert_EndpointBecomesTheOrganizationsWebhook", testConvert},
	{"Convert_NamesTakenGetSuffixes", testConvertNames},
	{"Convert_OtherOrgUntouched", testConvertIsolation},
}

// convert runs ConvertEndpoints in the fixture's organization.
func (f *fixture) convert() []domain.ConvertedEndpoint {
	f.t.Helper()

	out, err := f.in.ConvertEndpoints(f.ctx(), domain.LocalUser)
	if err != nil {
		f.t.Fatalf("ConvertEndpoints: %v", err)
	}

	return out
}

// A generic endpoint becomes a standard webhook without a factory, signed by webhook-<name>, a
// secret open to no factory holding the value the endpoint showed, and open to its old factory
// with every repository; the factory still lists it, its deliveries stay, and a second call
// converts nothing.
func testConvert(f *fixture) {
	t := f.t
	s := f.endpoint(hooks, domain.WebhookGeneric)
	f.deliver(s.Endpoint.ID, t0)

	opened, err := f.in.OpenSecret(f.ctx(), s.Endpoint.ID)
	if err != nil {
		t.Fatalf("OpenSecret: %v", err)
	}

	want := domain.ConvertedEndpoint{ID: s.Endpoint.ID, FactoryID: f.factory, From: hooks, Name: hooks, Secret: domain.WebhookSecretName(hooks)}
	if got := f.convert(); !slices.Equal(got, []domain.ConvertedEndpoint{want}) {
		t.Fatalf("ConvertEndpoints = %+v; want %+v", got, want)
	}

	e, err := f.in.Endpoint(f.ctx(), s.Endpoint.ID)
	if err != nil {
		t.Fatalf("Endpoint: %v", err)
	}

	if !e.OrgLevel() || e.Sender != domain.SenderStandard || e.Name != hooks || e.Secret != want.Secret || e.SecretID == "" || !e.Enabled {
		t.Errorf("converted = %+v; want the organization's standard webhook %s signed by %s", e, hooks, want.Secret)
	}

	if !openToEvery(e, f.factory) {
		t.Errorf("openings = %+v; want one to the factory, every repository", e.Openings)
	}

	sec, err := f.secrets.Get(f.ctx(), want.Secret)
	if err != nil || sec.Scope.All || len(sec.Scope.Factories) != 0 {
		t.Errorf("secret %s = %+v, %v; want one open to no factory", want.Secret, sec, err)
	}

	signing, err := f.secrets.OpenSigning(f.ctx(), e.SecretID)
	if err != nil || signing.Current != hex.EncodeToString(opened) {
		t.Errorf("OpenSigning = %v; want the value the endpoint showed", err)
	}

	if got := f.listed(f.factory); !slices.Equal(got, []string{hooks}) {
		t.Errorf("the factory's endpoints = %v; want %s through its opening", got, hooks)
	}

	if ds, err := f.in.ListDeliveries(f.ctx(), e.ID, domain.DeliveryCursor{}, 10); err != nil || len(ds) != 1 {
		t.Errorf("deliveries after the conversion = %d, %v; want the one before", len(ds), err)
	}

	if again := f.convert(); len(again) != 0 {
		t.Errorf("a second ConvertEndpoints = %+v; want nothing", again)
	}
}

// A reserved name, a name another factory's older endpoint has and a name the organization's
// webhook has each take the first free suffix, and so does a secret name that is taken; the older
// endpoint keeps the name it shares.
func testConvertNames(f *fixture) {
	t := f.t
	other := f.NewFactory(t, f.org)

	f.webhook(hooks + "-2")
	f.secret(domain.WebhookSecretName(hooks))

	older := f.endpoint(hooks, domain.WebhookGeneric)

	newer, err := f.in.CreateEndpoint(f.ctx(), domain.NewWebhookEndpoint{FactoryID: other, Name: hooks, Kind: domain.WebhookGeneric})
	if err != nil {
		t.Fatalf("CreateEndpoint in the other factory: %v", err)
	}

	gh := f.endpoint("github", domain.WebhookGitHub)

	want := []domain.ConvertedEndpoint{
		{ID: older.Endpoint.ID, FactoryID: f.factory, From: hooks, Name: hooks, Secret: "webhook-ci-hooks-3"},
		{ID: newer.Endpoint.ID, FactoryID: other, From: hooks, Name: "ci-hooks-3", Secret: "webhook-ci-hooks-3-2"},
		{ID: gh.Endpoint.ID, FactoryID: f.factory, From: "github", Name: "github-2", Secret: "webhook-github-2"},
	}

	if got := f.convert(); !slices.Equal(got, want) {
		t.Fatalf("ConvertEndpoints = %+v; want %+v", got, want)
	}

	e, err := f.in.WebhookByName(f.ctx(), "github-2")
	if err != nil || e.Sender != domain.SenderGitHub || !openToEvery(e, f.factory) {
		t.Errorf("github-2 = %+v, %v; want a github webhook open to the factory", e, err)
	}

	if got := f.listed(other); !slices.Equal(got, []string{"ci-hooks-3"}) {
		t.Errorf("the other factory's endpoints = %v; want ci-hooks-3", got)
	}
}

// Converting one organization leaves another's endpoints as they are.
func testConvertIsolation(f *fixture) {
	t := f.t
	s := f.endpoint(hooks, domain.WebhookGeneric)

	labs := &fixture{Harness: f.Harness, t: t, in: f.in, secrets: f.secrets, org: f.NewOrg(t)}
	if got := labs.convert(); len(got) != 0 {
		t.Errorf("another organization's ConvertEndpoints = %+v; want nothing", got)
	}

	if e, err := f.in.Endpoint(f.ctx(), s.Endpoint.ID); err != nil || e.OrgLevel() {
		t.Errorf("endpoint after another organization's conversion = %+v, %v; want its factory's still", e, err)
	}
}

// openToEvery reports whether e has one opening, to factory, with every repository.
func openToEvery(e domain.WebhookEndpoint, factory domain.FactoryID) bool {
	return len(e.Openings) == 1 && e.Openings[0].FactoryID == factory && e.Openings[0].Repositories == nil
}
