// Package intaketest is the conformance suite for ports.Intake (ADR 0026). The in-memory fake in
// porttest and the PostgreSQL adapter both run it, so tests on the fake hold for the database's
// rules: endpoint names unique per factory, signing secrets sealed under the master key and shown
// once, signature headers never kept, the 30-day prune, idempotent firings, the counts limits and
// checks read, Intake's tallies, and organizations kept apart.
package intaketest

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Harness is one Intake under test.
type Harness struct {
	// Intake returns the intake under test, sealing with key; every call shares one store, as
	// goalds with different keys share one database.
	Intake func(key ports.MasterKey) ports.Intake
	// Key returns master keys with ids, the first active; the same id is the same key in every
	// call. With no ids it has none (ports.ErrNoMasterKey).
	Key func(ids ...string) ports.MasterKey
	// Store is the store items are created in and moved by, the one Intake reads them from.
	Store ports.Store
	// NewOrg returns a new organization.
	NewOrg func(t *testing.T) domain.OrgID
	// NewFactory returns a factory of org the store accepts items for.
	NewFactory func(t *testing.T, org domain.OrgID) domain.FactoryID
}

// Run runs the suite. Tests run in parallel and call newHarness once each; harnesses must be
// isolated from each other, because PruneDeliveries spans every organization.
func Run(t *testing.T, newHarness func(t *testing.T) Harness) {
	t.Helper()

	tests := []struct {
		name string
		test func(f *fixture)
	}{
		{"Endpoint_SecretReturnedOnceAndOpened", testSecretOnce},
		{"Endpoint_ReadWithoutItsSecret", testEndpointReads},
		{"Endpoint_RotateChangesTheSecret", testRotate},
		{"Endpoint_DuplicateNameRefused", testDuplicateName},
		{"Endpoint_RulesRefused", testEndpointRules},
		{"Endpoint_DeleteRemovesItAndItsDeliveries", testDeleteEndpoint},
		{"Endpoint_AnotherKeyDoesNotOpen", testOtherKey},
		{"Delivery_SignatureHeadersNeverKept", testSignatureHeaders},
		{"Delivery_NewestFirstInPages", testDeliveryPages},
		{"Delivery_RulesRefused", testDeliveryRules},
		{"Delivery_PruneDeletesOnlyOlderRows", testPrune},
		{"Firing_SecondWithTheSameKeyIsANoOp", testFiringIdempotent},
		{"Firing_RulesRefused", testFiringRules},
		{"Firing_CreatedSinceCountsCreatedOnly", testCreatedSince},
		{"Firing_OpenItemByDedupeIgnoresClosedItems", testDedupe},
		{"Firing_ReadByItsKey", testFiringByKey},
		{"Firing_FailureStreakCountsSinceTheLastCreated", testFailureStreak},
		{"ScheduleLock_OneHolderAtATime", testScheduleLock},
		{"Tally_SourcesAndTriggerStatsOverAWindow", testTallies},
		{"Tally_LastItemsSources", testItemSources},
		{"Tally_TriggerDaysByUTCDay", testTriggerDays},
		{"Firing_ListNewestFirstWithItems", testFirings},
		{"OtherOrg_ReachesNoEndpoint", testIsolation},
		{"OtherOrg_CountsNothing", testIsolatedReads},
		{"OtherOrg_ReadsNoFiring", testIsolatedFirings},
		{"Methods_NoOrgIsAnError", testNoOrg},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			f := &fixture{Harness: h, t: t, in: h.Intake(h.Key("k1")), org: h.NewOrg(t)}
			f.factory = h.NewFactory(t, f.org)
			tc.test(f)
		})
	}
}

// Names the suite uses.
const (
	hooks   = "ci-hooks"
	flaky   = "flaky-test-on-main"
	nightly = "nightly-audit"
	station = "build"
	jsonCT  = "application/json"
)

// t0 is a fixed instant the suite's events happen around, at microsecond precision as
// PostgreSQL keeps it.
var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

type fixture struct {
	Harness

	t       *testing.T
	in      ports.Intake
	org     domain.OrgID
	factory domain.FactoryID
}

func (f *fixture) ctx() context.Context { return domain.WithOrg(f.t.Context(), f.org) }

func (f *fixture) endpoint(name string, kind domain.WebhookKind) domain.WebhookSecret {
	f.t.Helper()

	s, err := f.in.CreateEndpoint(f.ctx(), domain.NewWebhookEndpoint{FactoryID: f.factory, Name: name, Kind: kind})
	if err != nil {
		f.t.Fatalf("CreateEndpoint %s: %v", name, err)
	}

	return s
}

func (f *fixture) deliver(id domain.WebhookEndpointID, at time.Time) domain.WebhookDelivery {
	f.t.Helper()

	d, err := f.in.RecordDelivery(f.ctx(), domain.NewWebhookDelivery{
		EndpointID: id, ReceivedAt: at, Headers: map[string]string{"Content-Type": jsonCT}, Body: []byte(`{"event":"deploy"}`),
		Outcome: domain.DeliveryAccepted,
	})
	if err != nil {
		f.t.Fatalf("RecordDelivery: %v", err)
	}

	return d
}

func (f *fixture) deliveries(id domain.WebhookEndpointID) []domain.WebhookDelivery {
	f.t.Helper()

	got, err := f.in.ListDeliveries(f.ctx(), id, domain.DeliveryCursor{}, 100)
	if err != nil {
		f.t.Fatalf("ListDeliveries: %v", err)
	}

	return got
}

// item creates an item of the fixture's factory with source and trigger, at at.
func (f *fixture) item(source domain.ItemSource, trigger string, at time.Time) domain.Item {
	f.t.Helper()

	it, err := f.Store.CreateItem(f.ctx(), domain.NewItem{
		FactoryID: f.factory, Type: "fix", Title: "a flaky test", Station: station, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: at, Source: source, Trigger: trigger,
	})
	if err != nil {
		f.t.Fatalf("CreateItem: %v", err)
	}

	if it.Source != source || it.Trigger != trigger {
		f.t.Errorf("CreateItem's item has source %q, trigger %q; want %q, %q", it.Source, it.Trigger, source, trigger)
	}

	return it
}

// fire records a firing of trigger with key at at, created with item when item is set.
func (f *fixture) fire(trigger, key, dedupe string, item domain.ItemID, at time.Time) (domain.TriggerFiring, bool) {
	f.t.Helper()

	out := domain.FiringSkipped
	if item != "" {
		out = domain.FiringCreated
	}

	got, fresh, err := f.in.RecordFiring(f.ctx(), domain.TriggerFiring{
		FactoryID: f.factory, Trigger: trigger, Source: domain.TriggerGitHub, EventKey: "github.check_run.failed",
		IdempotencyKey: key, Dedupe: dedupe, Outcome: out, ItemID: item, At: at,
	})
	if err != nil {
		f.t.Fatalf("RecordFiring %s %s: %v", trigger, key, err)
	}

	return got, fresh
}

// close moves it to state, done or cancelled.
func (f *fixture) close(it domain.Item, state domain.ItemState) {
	f.t.Helper()

	_, err := f.Store.Transition(f.ctx(), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: "done", State: state}, At: t0, DoneAt: t0,
	})
	if err != nil {
		f.t.Fatalf("Transition to %s: %v", state, err)
	}
}

// A new endpoint's secret is 32 random bytes, returned by CreateEndpoint and opened again by
// OpenSecret.
func testSecretOnce(f *fixture) {
	t := f.t
	s := f.endpoint(hooks, domain.WebhookGitHub)
	other := f.endpoint("deploys", domain.WebhookGeneric)

	if len(s.Secret) != domain.WebhookSecretSize || bytes.Equal(s.Secret, other.Secret) {
		t.Errorf("secrets %x and %x; want two different ones of %d bytes", s.Secret, other.Secret, domain.WebhookSecretSize)
	}

	e := s.Endpoint
	if e.ID == "" || e.FactoryID != f.factory || e.Name != hooks || e.Kind != domain.WebhookGitHub || e.CreatedAt.IsZero() || e.RotatedAt != nil {
		t.Errorf("CreateEndpoint = %+v", e)
	}

	secret, err := f.in.OpenSecret(f.ctx(), e.ID)
	if err != nil || !bytes.Equal(secret, s.Secret) {
		t.Errorf("OpenSecret = %x, %v; want %x", secret, err, s.Secret)
	}
}

// An endpoint is read and listed, by name, without its secret, and EndpointOrg finds its
// organization without one in ctx.
func testEndpointReads(f *fixture) {
	t := f.t
	e := f.endpoint(hooks, domain.WebhookGitHub).Endpoint
	f.endpoint("deploys", domain.WebhookGeneric)

	got, err := f.in.Endpoint(f.ctx(), e.ID)
	if err != nil || got.ID != e.ID || got.Name != hooks || !got.CreatedAt.Equal(e.CreatedAt) {
		t.Errorf("Endpoint = %+v, %v; want %+v", got, err, e)
	}

	list, err := f.in.ListEndpoints(f.ctx(), f.factory)
	if err != nil || len(list) != 2 || list[0].Name != hooks || list[1].Name != "deploys" {
		t.Errorf("ListEndpoints = %+v, %v; want ci-hooks then deploys", list, err)
	}

	org, err := f.in.EndpointOrg(context.Background(), e.ID)
	if err != nil || org != f.org {
		t.Errorf("EndpointOrg without an org = %q, %v; want %q", org, err, f.org)
	}

	if _, err := f.in.EndpointOrg(context.Background(), "00000000-0000-7000-8000-00000000dead"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("EndpointOrg of an unknown id = %v; want ErrNotFound", err)
	}
}

// Rotating replaces the secret, which OpenSecret then returns, and sets RotatedAt.
func testRotate(f *fixture) {
	t := f.t
	s := f.endpoint(hooks, domain.WebhookGeneric)

	r, err := f.in.RotateSecret(f.ctx(), s.Endpoint.ID)
	if err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}

	if len(r.Secret) != domain.WebhookSecretSize || bytes.Equal(r.Secret, s.Secret) || r.Endpoint.RotatedAt == nil || r.Endpoint.ID != s.Endpoint.ID {
		t.Errorf("RotateSecret = %+v; want a new secret and RotatedAt", r.Endpoint)
	}

	if got, err := f.in.OpenSecret(f.ctx(), s.Endpoint.ID); err != nil || !bytes.Equal(got, r.Secret) {
		t.Errorf("OpenSecret after rotation = %x, %v; want the new %x", got, err, r.Secret)
	}

	if got, err := f.in.Endpoint(f.ctx(), s.Endpoint.ID); err != nil || got.RotatedAt == nil {
		t.Errorf("Endpoint after rotation = %+v, %v; want RotatedAt", got, err)
	}

	if _, err := f.in.RotateSecret(f.ctx(), "00000000-0000-7000-8000-00000000dead"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("RotateSecret of an unknown endpoint = %v; want ErrNotFound", err)
	}
}

// A name is unique within its factory, and another factory may use it.
func testDuplicateName(f *fixture) {
	t := f.t
	f.endpoint(hooks, domain.WebhookGeneric)

	_, err := f.in.CreateEndpoint(f.ctx(), domain.NewWebhookEndpoint{FactoryID: f.factory, Name: hooks, Kind: domain.WebhookGitHub})
	if !errors.Is(err, ports.ErrNameTaken) {
		t.Errorf("a second %s = %v; want ErrNameTaken", hooks, err)
	}

	other := f.NewFactory(t, f.org)
	if _, err := f.in.CreateEndpoint(f.ctx(), domain.NewWebhookEndpoint{FactoryID: other, Name: hooks, Kind: domain.WebhookGitHub}); err != nil {
		t.Errorf("%s in another factory: %v", hooks, err)
	}
}

// An endpoint that is not valid, or of a factory the organization lacks, is refused, and so is
// any endpoint without a master key.
func testEndpointRules(f *fixture) {
	t := f.t

	for _, n := range []domain.NewWebhookEndpoint{
		{FactoryID: f.factory, Name: "Not A Slug", Kind: domain.WebhookGeneric},
		{FactoryID: f.factory, Name: hooks, Kind: "slack"},
	} {
		if _, err := f.in.CreateEndpoint(f.ctx(), n); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("CreateEndpoint %+v = %v; want ErrInvalid", n, err)
		}
	}

	otherOrg := f.NewOrg(t)
	theirs := f.NewFactory(t, otherOrg)

	_, err := f.in.CreateEndpoint(f.ctx(), domain.NewWebhookEndpoint{FactoryID: theirs, Name: hooks, Kind: domain.WebhookGeneric})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("an endpoint on another organization's factory = %v; want ErrNotFound", err)
	}

	_, err = f.Intake(f.Key()).CreateEndpoint(f.ctx(), domain.NewWebhookEndpoint{FactoryID: f.factory, Name: hooks, Kind: domain.WebhookGeneric})
	if !errors.Is(err, ports.ErrNoMasterKey) {
		t.Errorf("CreateEndpoint without a master key = %v; want ErrNoMasterKey", err)
	}
}

// Deleting an endpoint removes it and its deliveries; a second delete finds nothing.
func testDeleteEndpoint(f *fixture) {
	t := f.t
	s := f.endpoint(hooks, domain.WebhookGeneric)
	f.deliver(s.Endpoint.ID, t0)

	if err := f.in.DeleteEndpoint(f.ctx(), s.Endpoint.ID); err != nil {
		t.Fatalf("DeleteEndpoint: %v", err)
	}

	if _, err := f.in.Endpoint(f.ctx(), s.Endpoint.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Endpoint after delete = %v; want ErrNotFound", err)
	}

	if _, err := f.in.EndpointOrg(context.Background(), s.Endpoint.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("EndpointOrg after delete = %v; want ErrNotFound", err)
	}

	if got := f.deliveries(s.Endpoint.ID); len(got) != 0 {
		t.Errorf("deliveries after delete = %d; want none", len(got))
	}

	if err := f.in.DeleteEndpoint(f.ctx(), s.Endpoint.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("a second DeleteEndpoint = %v; want ErrNotFound", err)
	}
}

// A secret sealed under k1 opens under keys that hold k1, and under no other; without a key it is
// ErrNoMasterKey. No refusal holds the secret.
func testOtherKey(f *fixture) {
	t := f.t
	s := f.endpoint(hooks, domain.WebhookGeneric)

	if got, err := f.Intake(f.Key("k2", "k1")).OpenSecret(f.ctx(), s.Endpoint.ID); err != nil || !bytes.Equal(got, s.Secret) {
		t.Errorf("OpenSecret under k2 with k1 = %x, %v; want the secret", got, err)
	}

	_, err := f.Intake(f.Key("k2")).OpenSecret(f.ctx(), s.Endpoint.ID)
	if err == nil || bytes.Contains([]byte(err.Error()), s.Secret) {
		t.Errorf("OpenSecret under k2 alone = %v; want a refusal without the secret", err)
	}

	if _, err := f.Intake(f.Key()).OpenSecret(f.ctx(), s.Endpoint.ID); !errors.Is(err, ports.ErrNoMasterKey) {
		t.Errorf("OpenSecret without a key = %v; want ErrNoMasterKey", err)
	}

	if _, err := f.Intake(f.Key()).RotateSecret(f.ctx(), s.Endpoint.ID); !errors.Is(err, ports.ErrNoMasterKey) {
		t.Errorf("RotateSecret without a key = %v; want ErrNoMasterKey", err)
	}
}

// A delivery keeps its headers with names lower-cased and without any signature header, its body,
// outcome, reason and item.
func testSignatureHeaders(f *fixture) {
	t := f.t
	s := f.endpoint(hooks, domain.WebhookGitHub)
	it := f.item(domain.ItemFromGitHub, flaky, t0)

	d, err := f.in.RecordDelivery(f.ctx(), domain.NewWebhookDelivery{
		EndpointID: s.Endpoint.ID, ReceivedAt: t0,
		Headers: map[string]string{
			"X-Hub-Signature-256": "sha256=deadbeef", "X-Hub-Signature": "sha1=beef", "x-goal-signature": "sha256=cafe",
			"X-GitHub-Event": "check_run", "Content-Type": jsonCT,
		},
		Body: []byte(`{"action":"completed"}`), Outcome: domain.DeliveryCreated, Reason: "created item", ItemID: it.ID,
	})
	if err != nil {
		t.Fatalf("RecordDelivery: %v", err)
	}

	want := map[string]string{"x-github-event": "check_run", "content-type": jsonCT}

	got := f.deliveries(s.Endpoint.ID)
	if len(got) != 1 {
		t.Fatalf("deliveries = %d; want 1", len(got))
	}

	for _, g := range []domain.WebhookDelivery{d, got[0]} {
		if !maps.Equal(g.Headers, want) {
			t.Errorf("headers = %v; want %v", g.Headers, want)
		}

		if g.ID == "" || g.EndpointID != s.Endpoint.ID || !g.ReceivedAt.Equal(t0) || string(g.Body) != `{"action":"completed"}` ||
			g.Outcome != domain.DeliveryCreated || g.Reason != "created item" || g.ItemID != it.ID {
			t.Errorf("delivery = %+v", g)
		}
	}
}

// Deliveries list newest first, a page at a time, each page after the last one's cursor.
func testDeliveryPages(f *fixture) {
	t := f.t
	s := f.endpoint(hooks, domain.WebhookGeneric)
	other := f.endpoint("deploys", domain.WebhookGeneric)

	var made []domain.WebhookDeliveryID
	for i := range 3 {
		made = append(made, f.deliver(s.Endpoint.ID, t0.Add(time.Duration(i)*time.Minute)).ID)
	}

	f.deliver(other.Endpoint.ID, t0)

	first, err := f.in.ListDeliveries(f.ctx(), s.Endpoint.ID, domain.DeliveryCursor{}, 2)
	if err != nil || len(first) != 2 || first[0].ID != made[2] || first[1].ID != made[1] {
		t.Fatalf("first page = %v, %v; want the two newest", ids(first), err)
	}

	last := first[1]

	rest, err := f.in.ListDeliveries(f.ctx(), s.Endpoint.ID, domain.DeliveryCursor{ReceivedAt: last.ReceivedAt, ID: last.ID}, 2)
	if err != nil || len(rest) != 1 || rest[0].ID != made[0] {
		t.Errorf("second page = %v, %v; want the oldest", ids(rest), err)
	}
}

func ids(ds []domain.WebhookDelivery) []domain.WebhookDeliveryID {
	out := []domain.WebhookDeliveryID{}
	for _, d := range ds {
		out = append(out, d.ID)
	}

	return out
}

// A delivery to an unknown endpoint is ErrNotFound; one with a bad outcome, an item on a refusal
// or a body over the cap is ErrInvalid.
func testDeliveryRules(f *fixture) {
	t := f.t
	s := f.endpoint(hooks, domain.WebhookGeneric)

	_, err := f.in.RecordDelivery(f.ctx(), domain.NewWebhookDelivery{
		EndpointID: "00000000-0000-7000-8000-00000000dead", ReceivedAt: t0, Outcome: domain.DeliveryRefused, Reason: "unknown endpoint",
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("a delivery to an unknown endpoint = %v; want ErrNotFound", err)
	}

	it := f.item(domain.ItemFromWebhook, "", t0)

	for _, n := range []domain.NewWebhookDelivery{
		{EndpointID: s.Endpoint.ID, ReceivedAt: t0, Outcome: "lost"},
		{EndpointID: s.Endpoint.ID, ReceivedAt: t0, Outcome: domain.DeliveryRefused, ItemID: it.ID},
		{EndpointID: s.Endpoint.ID, ReceivedAt: t0, Outcome: domain.DeliveryAccepted, Body: make([]byte, domain.MaxDeliveryBody+1)},
	} {
		if _, err := f.in.RecordDelivery(f.ctx(), n); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("RecordDelivery outcome %q item %q body %d = %v; want ErrInvalid", n.Outcome, n.ItemID, len(n.Body), err)
		}
	}
}

// PruneDeliveries deletes the deliveries received before the instant, in every organization, and
// keeps the rest, the instant itself included.
func testPrune(f *fixture) {
	t := f.t
	s := f.endpoint(hooks, domain.WebhookGeneric)
	cut := t0.Add(-domain.DeliveryRetention)

	f.deliver(s.Endpoint.ID, cut.Add(-time.Hour))
	kept := f.deliver(s.Endpoint.ID, cut)

	labs := &fixture{Harness: f.Harness, t: t, in: f.in, org: f.NewOrg(t)}
	labs.factory = f.NewFactory(t, labs.org)
	theirs := labs.endpoint(hooks, domain.WebhookGeneric)
	labs.deliver(theirs.Endpoint.ID, cut.Add(-time.Minute))

	n, err := f.in.PruneDeliveries(context.Background(), cut)
	if err != nil || n != 2 {
		t.Errorf("PruneDeliveries = %d, %v; want the 2 older rows", n, err)
	}

	if got := f.deliveries(s.Endpoint.ID); len(got) != 1 || got[0].ID != kept.ID {
		t.Errorf("acme keeps %v; want only %s", ids(got), kept.ID)
	}

	if got := labs.deliveries(theirs.Endpoint.ID); len(got) != 0 {
		t.Errorf("labs keeps %v; want none", ids(got))
	}
}

// A second firing with the same (factory, trigger, idempotency key) records nothing and returns the
// first; another trigger, or another key, records its own.
func testFiringIdempotent(f *fixture) {
	t := f.t
	it := f.item(domain.ItemFromGitHub, flaky, t0)

	first, fresh := f.fire(flaky, "check-run-1", "unit", it.ID, t0)
	if !fresh || first.ID == "" || first.ItemID != it.ID || first.Outcome != domain.FiringCreated || !first.At.Equal(t0) {
		t.Errorf("first RecordFiring = %+v, %v", first, fresh)
	}

	again, fresh := f.fire(flaky, "check-run-1", "unit", "", t0.Add(time.Minute))
	if fresh || again.ID != first.ID || again.Outcome != domain.FiringCreated || again.ItemID != it.ID || !again.At.Equal(t0) {
		t.Errorf("second RecordFiring = %+v, %v; want the first, not new", again, fresh)
	}

	if _, fresh := f.fire(nightly, "check-run-1", "", "", t0); !fresh {
		t.Error("another trigger's firing with the same key was not recorded")
	}

	if _, fresh := f.fire(flaky, "check-run-2", "", "", t0); !fresh {
		t.Error("the trigger's firing with another key was not recorded")
	}
}

// A firing from an unknown source, or naming an item it did not create, is refused.
func testFiringRules(f *fixture) {
	t := f.t

	_, _, err := f.in.RecordFiring(f.ctx(), domain.TriggerFiring{
		FactoryID: f.factory, Trigger: flaky, Source: "slack", EventKey: "slack.message", IdempotencyKey: "m1", Outcome: domain.FiringSkipped, At: t0,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a firing from slack = %v; want ErrInvalid", err)
	}

	it := f.item(domain.ItemFromGitHub, flaky, t0)

	_, _, err = f.in.RecordFiring(f.ctx(), domain.TriggerFiring{
		FactoryID: f.factory, Trigger: flaky, Source: domain.TriggerGitHub, EventKey: "github.check_run.failed", IdempotencyKey: "e1",
		Outcome: domain.FiringSkipped, ItemID: it.ID, At: t0,
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("a skipped firing naming an item = %v; want ErrInvalid", err)
	}
}

// CreatedSince counts the trigger's firings that created an item at or after the instant.
func testCreatedSince(f *fixture) {
	t := f.t
	day := t0.Truncate(24 * time.Hour)

	f.fire(flaky, "yesterday", "", f.item(domain.ItemFromGitHub, flaky, day.Add(-time.Hour)).ID, day.Add(-time.Hour))
	f.fire(flaky, "midnight", "", f.item(domain.ItemFromGitHub, flaky, day).ID, day)
	f.fire(flaky, "noon", "", f.item(domain.ItemFromGitHub, flaky, t0).ID, t0)
	f.fire(flaky, "skipped", "", "", t0)
	f.fire(nightly, "other", "", f.item(domain.ItemFromSchedule, nightly, t0).ID, t0)

	if n, err := f.in.CreatedSince(f.ctx(), f.factory, flaky, day); err != nil || n != 2 {
		t.Errorf("CreatedSince = %d, %v; want 2", n, err)
	}
}

// OpenItemByDedupe finds the item the trigger created for the value while it is open, and none
// once it is done or cancelled, for another trigger or value, or for an empty value.
func testDedupe(f *fixture) {
	t := f.t
	a := f.item(domain.ItemFromGitHub, flaky, t0)
	f.fire(flaky, "e1", "unit", a.ID, t0)
	f.fire(flaky, "e2", "", "", t0)

	check := func(trigger, value string, want domain.ItemID) {
		t.Helper()

		got, ok, err := f.in.OpenItemByDedupe(f.ctx(), f.factory, trigger, value)
		if err != nil || got != want || ok != (want != "") {
			t.Errorf("OpenItemByDedupe %s %q = %q, %v, %v; want %q", trigger, value, got, ok, err, want)
		}
	}

	check(flaky, "unit", a.ID)
	check(flaky, "lint", "")
	check(nightly, "unit", "")
	check(flaky, "", "")

	f.close(a, domain.StateDone)
	check(flaky, "unit", "")

	b := f.item(domain.ItemFromGitHub, flaky, t0.Add(time.Minute))
	f.fire(flaky, "e3", "unit", b.ID, t0.Add(time.Minute))
	check(flaky, "unit", b.ID)

	f.close(b, domain.StateCancelled)
	check(flaky, "unit", "")
}

// Firing reads the trigger's firing by its idempotency key, and none for another trigger or key.
func testFiringByKey(f *fixture) {
	t := f.t
	it := f.item(domain.ItemFromGitHub, flaky, t0)
	want, _ := f.fire(flaky, "check-run-1", "unit", it.ID, t0)

	got, ok, err := f.in.Firing(f.ctx(), f.factory, flaky, "check-run-1")
	if err != nil || !ok || got != want {
		t.Errorf("Firing = %+v, %v, %v; want %+v", got, ok, err, want)
	}

	for _, k := range []struct{ trigger, key string }{{nightly, "check-run-1"}, {flaky, "check-run-2"}} {
		if got, ok, err := f.in.Firing(f.ctx(), f.factory, k.trigger, k.key); err != nil || ok {
			t.Errorf("Firing %s %s = %+v, %v, %v; want none", k.trigger, k.key, got, ok, err)
		}
	}
}

// FailureStreak counts the trigger's firings with the dedupe value since its newest one that
// created an item for that value, or all of them when none did.
func testFailureStreak(f *fixture) {
	t := f.t

	streak := func(trigger, value string, want int) {
		t.Helper()

		if n, err := f.in.FailureStreak(f.ctx(), f.factory, trigger, value); err != nil || n != want {
			t.Errorf("FailureStreak %s %q = %d, %v; want %d", trigger, value, n, err, want)
		}
	}

	streak(flaky, "unit", 0)

	f.fire(flaky, "e1", "unit", "", t0)
	f.fire(flaky, "e2", "unit", "", t0.Add(time.Minute))
	f.fire(flaky, "e3", "lint", "", t0.Add(2*time.Minute))
	f.fire(flaky, "e4", "", "", t0.Add(3*time.Minute))
	streak(flaky, "unit", 2)
	streak(flaky, "lint", 1)
	streak(flaky, "", 1)
	streak(nightly, "unit", 0)

	it := f.item(domain.ItemFromGitHub, flaky, t0.Add(4*time.Minute))
	f.fire(flaky, "e5", "unit", it.ID, t0.Add(4*time.Minute))
	streak(flaky, "unit", 0)

	f.fire(flaky, "e6", "unit", "", t0.Add(5*time.Minute))
	streak(flaky, "unit", 1)
	streak(flaky, "lint", 1)
}

// WithScheduleLock runs its function while no one else holds the lock, and skips it while the
// lock is held, by this caller or another view of the same state; once freed it is taken again.
func testScheduleLock(f *fixture) {
	t := f.t
	other := f.Intake(f.Key("k1"))

	var inner, otherRan bool

	ran, err := f.in.WithScheduleLock(t.Context(), func(ctx context.Context) error {
		var err error

		inner, err = f.in.WithScheduleLock(ctx, func(context.Context) error { return nil })
		if err != nil {
			return err
		}

		otherRan, err = other.WithScheduleLock(ctx, func(context.Context) error { return nil })

		return err
	})
	if err != nil || !ran {
		t.Fatalf("WithScheduleLock = %v, %v; want it run", ran, err)
	}

	if inner || otherRan {
		t.Errorf("while the lock was held, a second taker ran (this view %v, another %v); want neither", inner, otherRan)
	}

	failed := errors.New("tick failed")
	if ran, err := other.WithScheduleLock(t.Context(), func(context.Context) error { return failed }); !ran || !errors.Is(err, failed) {
		t.Errorf("WithScheduleLock once freed = %v, %v; want it run and its error returned", ran, err)
	}

	if ran, err := f.in.WithScheduleLock(t.Context(), func(context.Context) error { return nil }); !ran || err != nil {
		t.Errorf("WithScheduleLock after an error = %v, %v; want the lock freed", ran, err)
	}
}

// SourceTally counts the factory's items since the instant by source, "" for none recorded;
// TriggerStats counts each trigger's items since then, those merged, and their cost.
func testTallies(f *fixture) {
	t := f.t
	since := t0.Add(-7 * 24 * time.Hour)

	f.item(domain.ItemFromGitHub, flaky, since.Add(-time.Hour)) // before the window
	a := f.item(domain.ItemFromGitHub, flaky, t0)
	b := f.item(domain.ItemFromGitHub, flaky, t0)
	f.item(domain.ItemFromSchedule, nightly, t0)
	f.item(domain.ItemFromPeople, "", t0)
	f.item("", "", t0)

	if err := f.Store.AppendEvents(f.ctx(), domain.Event{
		FactoryID: f.factory, ItemID: a.ID, At: t0, Kind: domain.EventMerged, Station: station, Actor: domain.ActorEngine,
		Data: map[string]any{"sha": "abc123"},
	}); err != nil {
		t.Fatalf("AppendEvents merged: %v", err)
	}

	f.spent(a, 1.25)
	f.spent(b, 0.5)

	otherFactory := f.NewFactory(t, f.org)
	if _, err := f.Store.CreateItem(f.ctx(), domain.NewItem{
		FactoryID: otherFactory, Type: "fix", Title: "elsewhere", Station: station, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: t0, Source: domain.ItemFromGitHub, Trigger: flaky,
	}); err != nil {
		t.Fatal(err)
	}

	tally, err := f.in.SourceTally(f.ctx(), f.factory, since)
	want := []domain.SourceCount{{Source: "", Items: 1}, {Source: domain.ItemFromGitHub, Items: 2}, {Source: domain.ItemFromPeople, Items: 1}, {Source: domain.ItemFromSchedule, Items: 1}}

	if err != nil || !slices.Equal(tally, want) {
		t.Errorf("SourceTally = %v, %v; want %v", tally, err, want)
	}

	stats, err := f.in.TriggerStats(f.ctx(), f.factory, since)
	wantStats := []domain.TriggerStat{{Trigger: flaky, Created: 2, Merged: 1, SpendUSD: 1.75}, {Trigger: nightly, Created: 1}}

	if err != nil || !slices.Equal(stats, wantStats) {
		t.Errorf("TriggerStats = %+v, %v; want %+v", stats, err, wantStats)
	}
}

// ItemSources counts the factory's newest items only, by source, with their first and last
// instants; a factory without items has none.
func testItemSources(f *fixture) {
	t := f.t

	if got, err := f.in.ItemSources(f.ctx(), f.factory, 48); err != nil || len(got.Counts) != 0 || !got.From.IsZero() || !got.To.IsZero() {
		t.Errorf("ItemSources of no items = %+v, %v; want none", got, err)
	}

	f.item(domain.ItemFromPeople, "", t0.Add(-4*time.Hour)) // older than the newest 4
	f.item(domain.ItemFromCLI, "", t0.Add(-3*time.Hour))
	f.item(domain.ItemFromGitHub, flaky, t0.Add(-2*time.Hour))
	f.item(domain.ItemFromGitHub, flaky, t0.Add(-time.Hour))
	f.item("", "", t0)

	otherFactory := f.NewFactory(t, f.org)
	if _, err := f.Store.CreateItem(f.ctx(), domain.NewItem{
		FactoryID: otherFactory, Type: "fix", Title: "elsewhere", Station: station, State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: t0.Add(time.Hour), Source: domain.ItemFromAPI,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := f.in.ItemSources(f.ctx(), f.factory, 4)
	want := []domain.SourceCount{{Source: "", Items: 1}, {Source: domain.ItemFromCLI, Items: 1}, {Source: domain.ItemFromGitHub, Items: 2}}

	if err != nil || !slices.Equal(got.Counts, want) || !got.From.Equal(t0.Add(-3*time.Hour)) || !got.To.Equal(t0) {
		t.Errorf("ItemSources(4) = %+v, %v; want %v from %v to %v", got, err, want, t0.Add(-3*time.Hour), t0)
	}
}

// TriggerDays count created firings only, at or after since, per trigger and UTC day.
func testTriggerDays(f *fixture) {
	t := f.t
	day1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	f.fire(flaky, "before", "", f.item(domain.ItemFromGitHub, flaky, t0).ID, t0.Add(-2*time.Hour))
	f.fire(flaky, "e1", "", f.item(domain.ItemFromGitHub, flaky, t0).ID, t0)
	f.fire(flaky, "e2", "", f.item(domain.ItemFromGitHub, flaky, t0).ID, t0.Add(time.Hour))
	f.fire(flaky, "e3", "", f.item(domain.ItemFromGitHub, flaky, t0).ID, t0.Add(13*time.Hour)) // 01:00 the next day
	f.fire(flaky, "skipped", "", "", t0)
	f.fire(nightly, "n1", "", f.item(domain.ItemFromSchedule, nightly, t0).ID, t0)

	got, err := f.in.TriggerDays(f.ctx(), f.factory, t0.Add(-time.Hour))
	want := []domain.TriggerDay{
		{Trigger: flaky, Day: day1, Created: 2}, {Trigger: flaky, Day: day1.AddDate(0, 0, 1), Created: 1}, {Trigger: nightly, Day: day1, Created: 1},
	}

	if err != nil || len(got) != len(want) {
		t.Fatalf("TriggerDays = %+v, %v; want %+v", got, err, want)
	}

	for i := range want {
		if got[i].Trigger != want[i].Trigger || !got[i].Day.Equal(want[i].Day) || got[i].Created != want[i].Created {
			t.Errorf("TriggerDays[%d] = %+v; want %+v", i, got[i], want[i])
		}
	}
}

// Firings are the trigger's own, newest first, with their items' numbers and merged events.
func testFirings(f *fixture) {
	t := f.t
	a := f.item(domain.ItemFromGitHub, flaky, t0)
	b := f.item(domain.ItemFromGitHub, flaky, t0)

	if err := f.Store.AppendEvents(f.ctx(), domain.Event{
		FactoryID: f.factory, ItemID: a.ID, At: t0, Kind: domain.EventMerged, Station: station, Actor: domain.ActorEngine,
		Data: map[string]any{"sha": "abc123"},
	}); err != nil {
		t.Fatalf("AppendEvents merged: %v", err)
	}

	f.fire(flaky, "e1", "", a.ID, t0)

	if _, _, err := f.in.RecordFiring(f.ctx(), domain.TriggerFiring{
		FactoryID: f.factory, Trigger: flaky, Source: domain.TriggerGitHub, EventKey: "github.check_run.failed",
		IdempotencyKey: "e2", Outcome: domain.FiringSkipped, Reason: "dedupe: an open item has this check", At: t0.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	f.fire(flaky, "e3", "", b.ID, t0.Add(2*time.Hour))
	f.fire(nightly, "n1", "", "", t0.Add(3*time.Hour))

	got, err := f.in.Firings(f.ctx(), f.factory, flaky, 10)
	if err != nil || len(got) != 3 {
		t.Fatalf("Firings = %+v, %v; want 3", got, err)
	}

	type entry struct {
		key, reason string
		number      int64
		merged      bool
	}

	want := []entry{{key: "e3", number: b.Number}, {key: "e2", reason: "dedupe: an open item has this check"}, {key: "e1", number: a.Number, merged: true}}

	for i, w := range want {
		g := entry{key: got[i].Firing.IdempotencyKey, reason: got[i].Firing.Reason, number: got[i].ItemNumber, merged: got[i].Merged}
		if g != w {
			t.Errorf("Firings[%d] = %+v; want %+v", i, g, w)
		}
	}

	if !got[2].Firing.At.Equal(t0) || got[2].Firing.ItemID != a.ID || got[2].Firing.Trigger != flaky {
		t.Errorf("Firings[2] = %+v; want e1 at %v with item %s", got[2].Firing, t0, a.ID)
	}

	if page, err := f.in.Firings(f.ctx(), f.factory, flaky, 2); err != nil || len(page) != 2 || page[0].Firing.IdempotencyKey != "e3" {
		t.Errorf("Firings(2) = %+v, %v; want e3 and e2", page, err)
	}
}

// spent records a finished run of it that cost usd.
func (f *fixture) spent(it domain.Item, usd float64) {
	f.t.Helper()

	run, err := f.Store.StartRun(f.ctx(), ports.NewRun{
		FactoryID: it.FactoryID, ItemID: it.ID, Station: station, Agent: "backend", Model: "test-model", StartedAt: t0,
	})
	if err != nil {
		f.t.Fatalf("StartRun: %v", err)
	}

	if err := f.Store.FinishRun(f.ctx(), run, domain.RunResult{Status: domain.RunSucceeded, CostUSD: usd}, t0.Add(time.Minute)); err != nil {
		f.t.Fatalf("FinishRun: %v", err)
	}
}

// Another organization reads, opens, rotates, deletes and delivers to none of the fixture's
// endpoints.
func testIsolation(f *fixture) {
	t := f.t
	s := f.endpoint(hooks, domain.WebhookGeneric)
	f.deliver(s.Endpoint.ID, t0)

	labs := domain.WithOrg(t.Context(), f.NewOrg(t))
	id := s.Endpoint.ID

	if _, err := f.in.Endpoint(labs, id); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("labs Endpoint = %v; want ErrNotFound", err)
	}

	if _, err := f.in.OpenSecret(labs, id); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("labs OpenSecret = %v; want ErrNotFound", err)
	}

	if _, err := f.in.RotateSecret(labs, id); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("labs RotateSecret = %v; want ErrNotFound", err)
	}

	if err := f.in.DeleteEndpoint(labs, id); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("labs DeleteEndpoint = %v; want ErrNotFound", err)
	}

	_, err := f.in.RecordDelivery(labs, domain.NewWebhookDelivery{EndpointID: id, ReceivedAt: t0, Outcome: domain.DeliveryAccepted})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("labs RecordDelivery on acme's endpoint = %v; want ErrNotFound", err)
	}

	if got := f.deliveries(id); len(got) != 1 {
		t.Errorf("acme's deliveries = %d; want its 1", len(got))
	}

	if _, err := f.in.OpenSecret(f.ctx(), id); err != nil {
		t.Errorf("acme OpenSecret: %v", err)
	}
}

// Another organization lists, counts and tallies none of the fixture's rows.
func testIsolatedReads(f *fixture) {
	t := f.t
	s := f.endpoint(hooks, domain.WebhookGeneric)
	f.deliver(s.Endpoint.ID, t0)
	it := f.item(domain.ItemFromGitHub, flaky, t0)
	f.fire(flaky, "e1", "unit", it.ID, t0)

	labs := domain.WithOrg(t.Context(), f.NewOrg(t))
	id := s.Endpoint.ID

	if got, err := f.in.ListEndpoints(labs, f.factory); err != nil || len(got) != 0 {
		t.Errorf("labs ListEndpoints = %v, %v; want none", got, err)
	}

	if got, err := f.in.ListDeliveries(labs, id, domain.DeliveryCursor{}, 10); err != nil || len(got) != 0 {
		t.Errorf("labs ListDeliveries = %v, %v; want none", got, err)
	}

	if n, err := f.in.CreatedSince(labs, f.factory, flaky, t0.Add(-time.Hour)); err != nil || n != 0 {
		t.Errorf("labs CreatedSince = %d, %v; want 0", n, err)
	}

	if got, ok, err := f.in.OpenItemByDedupe(labs, f.factory, flaky, "unit"); err != nil || ok {
		t.Errorf("labs OpenItemByDedupe = %q, %v, %v; want none", got, ok, err)
	}

	if got, err := f.in.SourceTally(labs, f.factory, t0.Add(-time.Hour)); err != nil || len(got) != 0 {
		t.Errorf("labs SourceTally = %v, %v; want none", got, err)
	}

	if got, err := f.in.TriggerStats(labs, f.factory, t0.Add(-time.Hour)); err != nil || len(got) != 0 {
		t.Errorf("labs TriggerStats = %v, %v; want none", got, err)
	}

	f.isolatedIntakeReads(labs)
}

// isolatedIntakeReads checks that labs reads none of the fixture's sources and trigger days.
func (f *fixture) isolatedIntakeReads(labs context.Context) {
	t := f.t

	if got, err := f.in.ItemSources(labs, f.factory, 48); err != nil || len(got.Counts) != 0 || !got.From.IsZero() {
		t.Errorf("labs ItemSources = %+v, %v; want none", got, err)
	}

	if got, err := f.in.TriggerDays(labs, f.factory, t0.Add(-time.Hour)); err != nil || len(got) != 0 {
		t.Errorf("labs TriggerDays = %v, %v; want none", got, err)
	}
}

// Another organization reads none of the fixture's firings.
func testIsolatedFirings(f *fixture) {
	t := f.t
	f.fire(flaky, "e1", "unit", "", t0)

	labs := domain.WithOrg(t.Context(), f.NewOrg(t))

	if got, ok, err := f.in.Firing(labs, f.factory, flaky, "e1"); err != nil || ok {
		t.Errorf("labs Firing = %+v, %v, %v; want none", got, ok, err)
	}

	if n, err := f.in.FailureStreak(labs, f.factory, flaky, "unit"); err != nil || n != 0 {
		t.Errorf("labs FailureStreak = %d, %v; want 0", n, err)
	}

	if got, err := f.in.Firings(labs, f.factory, flaky, 10); err != nil || len(got) != 0 {
		t.Errorf("labs Firings = %+v, %v; want none", got, err)
	}
}

// Every method but EndpointOrg and PruneDeliveries needs an organization in ctx.
func testNoOrg(f *fixture) {
	t := f.t
	ctx := t.Context()
	s := f.endpoint(hooks, domain.WebhookGeneric)
	id := s.Endpoint.ID

	calls := map[string]func() error{
		"CreateEndpoint": func() error {
			_, err := f.in.CreateEndpoint(ctx, domain.NewWebhookEndpoint{FactoryID: f.factory, Name: "other", Kind: domain.WebhookGeneric})
			return err
		},
		"ListEndpoints":  func() error { _, err := f.in.ListEndpoints(ctx, f.factory); return err },
		"Endpoint":       func() error { _, err := f.in.Endpoint(ctx, id); return err },
		"RotateSecret":   func() error { _, err := f.in.RotateSecret(ctx, id); return err },
		"DeleteEndpoint": func() error { return f.in.DeleteEndpoint(ctx, id) },
		"OpenSecret":     func() error { _, err := f.in.OpenSecret(ctx, id); return err },
		"RecordDelivery": func() error {
			_, err := f.in.RecordDelivery(ctx, domain.NewWebhookDelivery{EndpointID: id, ReceivedAt: t0, Outcome: domain.DeliveryAccepted})
			return err
		},
		"ListDeliveries": func() error { _, err := f.in.ListDeliveries(ctx, id, domain.DeliveryCursor{}, 10); return err },
		"RecordFiring": func() error {
			_, _, err := f.in.RecordFiring(ctx, domain.TriggerFiring{
				FactoryID: f.factory, Trigger: flaky, Source: domain.TriggerSchedule, EventKey: "schedule", IdempotencyKey: "k", Outcome: domain.FiringSkipped, At: t0,
			})

			return err
		},
		"CreatedSince":     func() error { _, err := f.in.CreatedSince(ctx, f.factory, flaky, t0); return err },
		"OpenItemByDedupe": func() error { _, _, err := f.in.OpenItemByDedupe(ctx, f.factory, flaky, "unit"); return err },
		"Firing":           func() error { _, _, err := f.in.Firing(ctx, f.factory, flaky, "k"); return err },
		"FailureStreak":    func() error { _, err := f.in.FailureStreak(ctx, f.factory, flaky, "unit"); return err },
		"SourceTally":      func() error { _, err := f.in.SourceTally(ctx, f.factory, t0); return err },
		"TriggerStats":     func() error { _, err := f.in.TriggerStats(ctx, f.factory, t0); return err },
		"ItemSources":      func() error { _, err := f.in.ItemSources(ctx, f.factory, 48); return err },
		"TriggerDays":      func() error { _, err := f.in.TriggerDays(ctx, f.factory, t0); return err },
		"Firings":          func() error { _, err := f.in.Firings(ctx, f.factory, flaky, 10); return err },
	}

	for name, call := range calls {
		if err := call(); !errors.Is(err, domain.ErrNoOrg) {
			t.Errorf("%s without an org = %v; want ErrNoOrg", name, err)
		}
	}
}
