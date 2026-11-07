package intaketest

import (
	"errors"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The organization's webhooks (ADR 0040 decisions 12 and 15) the suite creates.
const (
	orgHook   = "github-acme"
	hookToken = "webhook-github-acme" //nolint:gosec // a secret's name, not a value
	hookValue = "whsec-conformance-value"
)

// webhookTests are the suite's organization webhook cases, run beside the endpoint ones.
var webhookTests = []struct {
	name string
	test func(f *fixture)
}{
	{"Webhook_OpenedClosedAndRenamed", testWebhookPatch},
	{"Webhook_RulesRefused", testWebhookRules},
	{"Webhook_SecretFollowsItsRenameAndStaysWhileUsed", testWebhookSecret},
	{"Webhook_DeliveriesCountForTheFactoriesItIsOpenTo", testWebhookDays},
	{"Webhook_OtherOrgSeesNone", testWebhookIsolation},
}

// secret creates the organization secret name the fixture's webhooks are signed by.
func (f *fixture) secret(name string) domain.Secret {
	f.t.Helper()

	s, err := f.secrets.Create(f.ctx(), domain.NewSecret{Name: name, Value: hookValue}, domain.LocalUser)
	if err != nil {
		f.t.Fatalf("Create secret %s: %v", name, err)
	}

	return s
}

// webhook creates the organization's webhook name, signed by a new secret of its own.
func (f *fixture) webhook(name string) domain.WebhookEndpoint {
	f.t.Helper()

	sec := f.secret(domain.WebhookSecretName(name))

	w, err := f.in.CreateWebhook(f.ctx(), domain.NewWebhook{Name: name, Sender: domain.SenderGitHub, Secret: sec.Name})
	if err != nil {
		f.t.Fatalf("CreateWebhook %s: %v", name, err)
	}

	return w
}

func (f *fixture) patch(id domain.WebhookEndpointID, p domain.WebhookPatch) domain.WebhookEndpoint {
	f.t.Helper()

	w, err := f.in.PatchWebhook(f.ctx(), id, p)
	if err != nil {
		f.t.Fatalf("PatchWebhook: %v", err)
	}

	return w
}

func (f *fixture) listed(factory domain.FactoryID) []string {
	f.t.Helper()

	es, err := f.in.ListEndpoints(f.ctx(), factory)
	if err != nil {
		f.t.Fatalf("ListEndpoints: %v", err)
	}

	names := []string{}
	for _, e := range es {
		names = append(names, e.Name)
	}

	return names
}

// expectNew checks the new webhook w is enabled, open to no factory and signed by its secret.
func (f *fixture) expectNew(w domain.WebhookEndpoint) {
	f.t.Helper()

	if w.Sender != domain.SenderGitHub || !w.Enabled || len(w.Openings) != 0 || w.Secret != hookToken || w.SecretID == "" {
		f.t.Fatalf("created = %+v; want an enabled github webhook, open to none, signed by %s", w, hookToken)
	}

	if got := f.listed(f.factory); len(got) != 0 {
		f.t.Errorf("the factory's endpoints before an opening = %v; want none", got)
	}
}

// A new webhook is enabled and open to no factory; an opening, limited to repositories or not,
// lists it for that factory; a rename, turning it off and closing it change what reads it.
func testWebhookPatch(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)

	f.expectNew(w)

	other := f.NewFactory(t, f.org)
	repos := []string{"acme/api", "acme/web"}
	w = f.patch(w.ID, domain.WebhookPatch{Open: []domain.WebhookOpening{{FactoryID: f.factory, Repositories: repos}, {FactoryID: other}}})

	if o, ok := w.Opening(f.factory); !ok || !slices.Equal(o.Repositories, repos) {
		t.Errorf("opening of the factory = %+v, %v; want limited to %v", o, ok, repos)
	}

	if o, ok := w.Opening(other); !ok || o.Repositories != nil {
		t.Errorf("opening of the other factory = %+v, %v; want every repository", o, ok)
	}

	if got := f.listed(f.factory); !slices.Equal(got, []string{orgHook}) {
		t.Errorf("the factory's endpoints = %v; want %s", got, orgHook)
	}

	f.renameAndClose(w, other)
}

// renameAndClose renames w, turns it off and closes it to other, and checks every read sees it.
func (f *fixture) renameAndClose(w domain.WebhookEndpoint, other domain.FactoryID) {
	t := f.t
	renamed, off := "github-labs", false
	w = f.patch(w.ID, domain.WebhookPatch{Name: &renamed, Enabled: &off, Close: []domain.FactoryID{other}})

	got, err := f.in.WebhookByName(f.ctx(), renamed)
	if err != nil || got.ID != w.ID || got.Enabled || len(got.Openings) != 1 || got.Openings[0].FactoryID != f.factory {
		t.Errorf("WebhookByName after the patch = %+v, %v; want it renamed, off and open to the factory alone", got, err)
	}

	if _, err := f.in.WebhookByName(f.ctx(), orgHook); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("WebhookByName of the old name = %v; want ErrNotFound", err)
	}

	byID, err := f.in.Endpoint(f.ctx(), w.ID)
	if err != nil || byID.Name != renamed || len(byID.Openings) != 1 || byID.Secret != hookToken {
		t.Errorf("Endpoint = %+v, %v; want the renamed webhook with its opening and secret", byID, err)
	}

	f.onlyWebhook(w.ID)
}

// onlyWebhook checks the organization's one webhook is id.
func (f *fixture) onlyWebhook(id domain.WebhookEndpointID) {
	f.t.Helper()

	list, err := f.in.Webhooks(f.ctx())
	if err != nil || len(list) != 1 || list[0].ID != id {
		f.t.Errorf("Webhooks = %+v, %v; want the one webhook", list, err)
	}
}

// Reserved and taken names and a secret or factory the organization lacks are refused.
func testWebhookRules(f *fixture) {
	t := f.t
	f.webhook(hooks)
	w := f.webhook(orgHook)
	sec := f.secret("pagerduty-signing")

	creates := []struct {
		name string
		n    domain.NewWebhook
		want error
	}{
		{"a reserved name", domain.NewWebhook{Name: "github", Sender: domain.SenderGitHub, Secret: sec.Name}, domain.ErrInvalid},
		{"an unknown sender", domain.NewWebhook{Name: "pages", Sender: "slack", Secret: sec.Name}, domain.ErrInvalid},
		{"token settings on github", domain.NewWebhook{Name: "pages", Sender: domain.SenderGitHub, Secret: sec.Name, Token: domain.TokenSettings{Header: "X-Token"}}, domain.ErrInvalid},
		{"another webhook's name", domain.NewWebhook{Name: orgHook, Sender: domain.SenderPagerDuty, Secret: sec.Name}, ports.ErrNameTaken},
		{"no such secret", domain.NewWebhook{Name: "pages", Sender: domain.SenderPagerDuty, Secret: "ghost-secret"}, ports.ErrNotFound},
	}

	for _, c := range creates {
		if _, err := f.in.CreateWebhook(f.ctx(), c.n); !errors.Is(err, c.want) {
			t.Errorf("CreateWebhook with %s = %v; want %v", c.name, err, c.want)
		}
	}

	taken, reserved := hooks, "schedule"
	patches := []struct {
		name string
		id   domain.WebhookEndpointID
		p    domain.WebhookPatch
		want error
	}{
		{"a taken name", w.ID, domain.WebhookPatch{Name: &taken}, ports.ErrNameTaken},
		{"a reserved name", w.ID, domain.WebhookPatch{Name: &reserved}, domain.ErrInvalid},
		{"no such factory", w.ID, domain.WebhookPatch{Open: []domain.WebhookOpening{{FactoryID: "00000000-0000-4000-8000-ffffffffffff"}}}, ports.ErrNotFound},
		{"a factory both opened and closed", w.ID, domain.WebhookPatch{Open: []domain.WebhookOpening{{FactoryID: f.factory}}, Close: []domain.FactoryID{f.factory}}, domain.ErrInvalid},
		{"no such webhook", "00000000-0000-7000-8000-00000000dead", domain.WebhookPatch{Close: []domain.FactoryID{f.factory}}, ports.ErrNotFound},
	}

	for _, c := range patches {
		if _, err := f.in.PatchWebhook(f.ctx(), c.id, c.p); !errors.Is(err, c.want) {
			t.Errorf("PatchWebhook with %s = %v; want %v", c.name, err, c.want)
		}
	}

	if list, err := f.in.Webhooks(f.ctx()); err != nil || len(list) != 2 {
		t.Errorf("Webhooks after the refusals = %+v, %v; want the two", list, err)
	}
}

// Renaming the secret keeps the webhook signed by it; the secret cannot be deleted while the
// webhook uses it, and can once the webhook is gone. WebhookSecretUses lists each pair.
func testWebhookSecret(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)

	renamed := "github-signing"
	if _, err := f.secrets.Patch(f.ctx(), hookToken, domain.SecretPatch{Name: &renamed}, domain.LocalUser); err != nil {
		t.Fatal(err)
	}

	if got, err := f.in.Endpoint(f.ctx(), w.ID); err != nil || got.Secret != renamed || got.SecretID != w.SecretID {
		t.Errorf("Endpoint after the secret's rename = %+v, %v; want it signed by %s", got, err, renamed)
	}

	uses, err := f.in.WebhookSecretUses(f.ctx())
	if err != nil || !slices.Equal(uses, []domain.WebhookSecretUse{{Secret: renamed, Webhook: orgHook}}) {
		t.Errorf("WebhookSecretUses = %+v, %v; want %s signing %s", uses, err, renamed, orgHook)
	}

	if err := f.secrets.Delete(f.ctx(), renamed, domain.LocalUser); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("Delete of a secret a webhook uses = %v; want ErrConflict", err)
	}

	if err := f.in.DeleteEndpoint(f.ctx(), w.ID); err != nil {
		t.Fatal(err)
	}

	if err := f.secrets.Delete(f.ctx(), renamed, domain.LocalUser); err != nil {
		t.Errorf("Delete once the webhook is gone = %v", err)
	}
}

// A webhook's deliveries count for each factory it is open to, and for the organization ("").
func testWebhookDays(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)
	f.deliver(w.ID, t0)

	days := func(factory domain.FactoryID) int {
		t.Helper()

		got, err := f.in.DeliveryDays(f.ctx(), factory, t0.Add(-24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}

		n := 0
		for _, d := range got {
			n += d.Delivered
		}

		return n
	}

	if days(f.factory) != 0 || days("") != 1 {
		t.Errorf("days before an opening: factory %d, organization %d; want 0 and 1", days(f.factory), days(""))
	}

	f.patch(w.ID, domain.WebhookPatch{Open: []domain.WebhookOpening{{FactoryID: f.factory}}})

	if days(f.factory) != 1 {
		t.Errorf("the factory's days once open = %d; want 1", days(f.factory))
	}
}

// Another organization lists, reads and patches none of the webhooks.
func testWebhookIsolation(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)
	labs := domain.WithOrg(t.Context(), f.NewOrg(t))

	if list, err := f.in.Webhooks(labs); err != nil || len(list) != 0 {
		t.Errorf("another organization's Webhooks = %+v, %v; want none", list, err)
	}

	if _, err := f.in.WebhookByName(labs, orgHook); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("another organization's WebhookByName = %v; want ErrNotFound", err)
	}

	off := false
	if _, err := f.in.PatchWebhook(labs, w.ID, domain.WebhookPatch{Enabled: &off}); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("another organization's PatchWebhook = %v; want ErrNotFound", err)
	}

	if uses, err := f.in.WebhookSecretUses(labs); err != nil || len(uses) != 0 {
		t.Errorf("another organization's WebhookSecretUses = %+v, %v; want none", uses, err)
	}
}
