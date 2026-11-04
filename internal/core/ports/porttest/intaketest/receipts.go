package intaketest

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// receiptTests are the suite's cases for the receiver's receipts, ledger and daily counts (ADR 0040
// decisions 12 and 13).
var receiptTests = []struct {
	name string
	test func(f *fixture)
}{
	{"Receive_NamedKeepsItsRowAndBody", testReceiveNamed},
	{"Receive_DuplicateCreatesNothingAndCountsOnTheFirstRow", testReceiveDuplicate},
	{"Receive_UnnamedKeepsOnlyItsLedgerIDAndADailyCount", testReceiveUnnamed},
	{"Receive_RefusedKeepsMetadataOnly", testReceiveRefused},
	{"Receive_LedgerPrunedAfterAYear", testReceivePrune},
	{"Receive_RulesRefused", testReceiveRules},
	{"Receive_OtherOrgHasItsOwnLedger", testReceiveIsolation},
}

// receive records an accepted delivery id to webhook w at at, named or not.
func (f *fixture) receive(w domain.WebhookEndpointID, id string, at time.Time, named bool) domain.Received {
	f.t.Helper()

	r, err := f.in.Receive(f.ctx(), domain.NewReceipt{
		WebhookID: w, ReceivedAt: at, Receipt: domain.ReceiptAccepted, DeliveryID: id, Event: "push", Named: named,
		Headers: map[string]string{"X-GitHub-Event": "push", "X-Hub-Signature-256": "sha256=00"}, Body: []byte(`{"ref":"main"}`),
	})
	if err != nil {
		f.t.Fatalf("Receive %s: %v", id, err)
	}

	return r
}

func (f *fixture) row(w domain.WebhookEndpointID, id domain.WebhookDeliveryID) domain.WebhookDelivery {
	f.t.Helper()

	d, err := f.in.Delivery(f.ctx(), w, id)
	if err != nil {
		f.t.Fatalf("Delivery %s: %v", id, err)
	}

	return d
}

// A delivery a trigger names keeps its row with its id, event, receipt and body, without its
// signature header.
func testReceiveNamed(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)

	r := f.receive(w.ID, "d-1", t0, true)
	if r.Receipt != domain.ReceiptAccepted || r.DeliveryID != "d-1" || r.Row == "" {
		t.Fatalf("Receive = %+v; want accepted d-1 with a row", r)
	}

	d := f.row(w.ID, r.Row)
	if d.DeliveryID != "d-1" || d.Event != "push" || d.Receipt != domain.ReceiptAccepted || d.Outcome != domain.DeliveryAccepted ||
		!bytes.Equal(d.Body, []byte(`{"ref":"main"}`)) || d.Duplicates != 0 || !d.ReceivedAt.Equal(t0) {
		t.Errorf("Delivery = %+v; want d-1, push, accepted, its body", d)
	}

	if _, ok := d.Headers["x-hub-signature-256"]; ok || d.Headers["x-github-event"] != "push" {
		t.Errorf("Headers = %v; want x-github-event only", d.Headers)
	}

	if got, err := f.in.UnnamedDays(f.ctx(), w.ID, t0.Add(-48*time.Hour)); err != nil || len(got) != 0 {
		t.Errorf("UnnamedDays = %+v, %v; want none", got, err)
	}
}

// A delivery id the webhook already took creates nothing; the row it first came with counts each
// duplicate.
func testReceiveDuplicate(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)
	first := f.receive(w.ID, "d-1", t0, true)

	for range 2 {
		r := f.receive(w.ID, "d-1", t0.Add(time.Minute), true)
		if r.Receipt != domain.ReceiptDuplicate || r.Row != first.Row || r.DeliveryID != "d-1" {
			t.Errorf("Receive again = %+v; want a duplicate of %s", r, first.Row)
		}
	}

	if got := f.deliveries(w.ID); len(got) != 1 || got[0].Duplicates != 2 {
		t.Errorf("deliveries = %+v; want the first, with 2 duplicates", got)
	}

	if r := f.receive(w.ID, "d-2", t0, true); r.Receipt != domain.ReceiptAccepted {
		t.Errorf("Receive d-2 = %+v; want accepted", r)
	}
}

// A delivery no trigger names keeps no row and no body: its ledger id and its UTC day's count. Its
// id, delivered again, is a duplicate without a row.
func testReceiveUnnamed(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)
	day := time.Date(t0.Year(), t0.Month(), t0.Day(), 0, 0, 0, 0, time.UTC)

	for i, at := range []time.Time{t0, t0.Add(time.Hour), t0.Add(24 * time.Hour)} {
		if r := f.receive(w.ID, "u-"+string(rune('a'+i)), at, false); r.Receipt != domain.ReceiptAccepted || r.Row != "" {
			t.Errorf("Receive unnamed = %+v; want accepted without a row", r)
		}
	}

	if r := f.receive(w.ID, "u-a", t0.Add(2*time.Hour), false); r.Receipt != domain.ReceiptDuplicate || r.Row != "" {
		t.Errorf("Receive u-a again = %+v; want a duplicate without a row", r)
	}

	if got := f.deliveries(w.ID); len(got) != 0 {
		t.Errorf("deliveries = %+v; want none", got)
	}

	got, err := f.in.UnnamedDays(f.ctx(), w.ID, t0)
	want := []domain.DeliveryCount{{WebhookID: w.ID, Day: day, Count: 2}, {WebhookID: w.ID, Day: day.Add(24 * time.Hour), Count: 1}}

	if err != nil || !slices.Equal(got, want) {
		t.Errorf("UnnamedDays = %+v, %v; want %+v", got, err, want)
	}

	if got, err := f.in.UnnamedDays(f.ctx(), w.ID, t0.Add(24*time.Hour)); err != nil || len(got) != 1 {
		t.Errorf("UnnamedDays from the next day = %+v, %v; want its one day", got, err)
	}
}

// A refused delivery keeps its headers, without a signature, and its reason; no id, event or body,
// and nothing in the ledger.
func testReceiveRefused(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)

	for range 2 {
		r, err := f.in.Receive(f.ctx(), domain.NewReceipt{
			WebhookID: w.ID, ReceivedAt: t0, Receipt: domain.ReceiptRefused, Reason: "X-Hub-Signature-256 does not match",
			Headers: map[string]string{"X-Hub-Signature-256": "sha256=00", "Authorization": "Bearer x", "User-Agent": "GitHub-Hookshot"},
		})
		if err != nil || r.Receipt != domain.ReceiptRefused || r.Row == "" {
			t.Fatalf("Receive refused = %+v, %v; want a refused row", r, err)
		}

		d := f.row(w.ID, r.Row)
		if d.Outcome != domain.DeliveryRefused || d.Receipt != domain.ReceiptRefused || len(d.Body) != 0 || d.DeliveryID != "" ||
			d.Event != "" || d.Reason == "" || len(d.Headers) != 1 || d.Headers["user-agent"] != "GitHub-Hookshot" {
			t.Errorf("Delivery = %+v; want metadata only", d)
		}
	}

	if r := f.receive(w.ID, "d-1", t0, true); r.Receipt != domain.ReceiptAccepted {
		t.Errorf("Receive after refusals = %+v; want accepted", r)
	}
}

// PruneDeliveries deletes the ledger ids and the counts older than a year, in every organization:
// a delivery id after that is accepted again, one within it is still a duplicate.
func testReceivePrune(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)
	old := t0.Add(-domain.LedgerRetention - 24*time.Hour)
	recent := t0.Add(-domain.LedgerRetention + 24*time.Hour)

	f.receive(w.ID, "old", old, false)
	f.receive(w.ID, "recent", recent, false)

	if n, err := f.in.PruneDeliveries(context.Background(), t0); err != nil || n != 2 {
		t.Errorf("PruneDeliveries = %d, %v; want the old id and its day's count", n, err)
	}

	if got, err := f.in.UnnamedDays(f.ctx(), w.ID, old.Add(-time.Hour)); err != nil || len(got) != 1 {
		t.Errorf("UnnamedDays after the prune = %+v, %v; want the recent day only", got, err)
	}

	if r := f.receive(w.ID, "old", t0, true); r.Receipt != domain.ReceiptAccepted {
		t.Errorf("Receive old after a year = %+v; want accepted", r)
	}

	if r := f.receive(w.ID, "recent", t0, true); r.Receipt != domain.ReceiptDuplicate {
		t.Errorf("Receive recent = %+v; want a duplicate", r)
	}
}

// A receipt Check refuses is ErrInvalid, and an unknown webhook ErrNotFound.
func testReceiveRules(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)

	for _, n := range []domain.NewReceipt{
		{WebhookID: w.ID, ReceivedAt: t0, Receipt: domain.ReceiptAccepted},
		{WebhookID: w.ID, ReceivedAt: t0, Receipt: domain.ReceiptDuplicate, DeliveryID: "d"},
		{WebhookID: w.ID, ReceivedAt: t0, Receipt: domain.ReceiptRefused, Body: []byte("x")},
	} {
		if _, err := f.in.Receive(f.ctx(), n); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Receive %+v = %v; want ErrInvalid", n, err)
		}
	}

	n := domain.NewReceipt{WebhookID: "019a0000-0000-7000-8000-000000000000", ReceivedAt: t0, Receipt: domain.ReceiptAccepted, DeliveryID: "d"}
	if _, err := f.in.Receive(f.ctx(), n); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Receive to an unknown webhook = %v; want ErrNotFound", err)
	}
}

// Another organization reaches neither the webhook nor its ledger and counts.
func testReceiveIsolation(f *fixture) {
	t := f.t
	w := f.webhook(orgHook)
	f.receive(w.ID, "d-1", t0, false)

	labs := domain.WithOrg(t.Context(), f.NewOrg(t))

	n := domain.NewReceipt{WebhookID: w.ID, ReceivedAt: t0, Receipt: domain.ReceiptAccepted, DeliveryID: "d-2", Named: true}
	if _, err := f.in.Receive(labs, n); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("labs Receive to acme's webhook = %v; want ErrNotFound", err)
	}

	if got, err := f.in.UnnamedDays(labs, w.ID, t0.Add(-time.Hour)); err != nil || len(got) != 0 {
		t.Errorf("labs UnnamedDays = %+v, %v; want none", got, err)
	}

	if r := f.receive(w.ID, "d-2", t0, true); r.Receipt != domain.ReceiptAccepted {
		t.Errorf("acme Receive d-2 = %+v; want accepted, labs took nothing", r)
	}
}
