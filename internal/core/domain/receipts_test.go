package domain

import (
	"errors"
	"maps"
	"strings"
	"testing"
	"time"
)

// The events the tests deliver.
const (
	evPush     = "push"
	evCheckRun = "check_run"
)

// A delivery keeps no sender's signature or token header, whatever its case, nor the token
// webhook's own header; every other header stays, its name lower-cased.
func TestStoredHeaders_DropEverySignatureHeader(t *testing.T) {
	t.Parallel()

	in := map[string]string{
		"X-Hub-Signature-256": "sha256=00", "X-Hub-Signature": "sha1=00", "X-Goal-Signature": "sha256=00",
		"X-Gitlab-Token": "t", "Webhook-Signature": "v1,AA==", "X-PagerDuty-Signature": "v1=00",
		"Sentry-Hook-Signature": "00", "Authorization": "Bearer t", "X-Acme-Token": "t",
		"X-GitHub-Event": evPush, "Content-Type": "application/json",
	}

	got := StoredHeaders(in, "x-acme-token")
	want := map[string]string{"x-github-event": evPush, "content-type": "application/json"}

	if !maps.Equal(got, want) {
		t.Errorf("StoredHeaders = %v; want %v", got, want)
	}
}

func TestNewReceipt_Check(t *testing.T) {
	t.Parallel()

	ok := NewReceipt{WebhookID: "w", ReceivedAt: time.Now(), Receipt: ReceiptAccepted, DeliveryID: "d1", Event: evPush, Body: []byte("{}")}
	if err := ok.Check(); err != nil {
		t.Fatalf("Check(accepted) = %v", err)
	}

	refused := NewReceipt{WebhookID: "w", Receipt: ReceiptRefused, Reason: "signature did not match"}
	if err := refused.Check(); err != nil {
		t.Fatalf("Check(refused) = %v", err)
	}

	bad := map[string]func(n *NewReceipt){
		"no webhook":         func(n *NewReceipt) { n.WebhookID = "" },
		"duplicate receipt":  func(n *NewReceipt) { n.Receipt = ReceiptDuplicate },
		"no id":              func(n *NewReceipt) { n.DeliveryID = "" },
		"long id":            func(n *NewReceipt) { n.DeliveryID = strings.Repeat("a", MaxDeliveryID+1) },
		"long event":         func(n *NewReceipt) { n.Event = strings.Repeat("e", 201) },
		"large body":         func(n *NewReceipt) { n.Body = make([]byte, MaxDeliveryBody+1) },
		"long reason":        func(n *NewReceipt) { n.Reason = strings.Repeat("r", 501) },
		"refused with body":  func(n *NewReceipt) { n.Receipt, n.DeliveryID, n.Event = ReceiptRefused, "", "" },
		"refused with an id": func(n *NewReceipt) { n.Receipt, n.Body, n.Event = ReceiptRefused, nil, "" },
	}

	for name, change := range bad {
		n := ok
		change(&n)

		if err := n.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("Check(%s) = %v; want ErrInvalid", name, err)
		}
	}
}

// A delivery is named by an enabled trigger on its webhook, with its event or none, or on its
// sender and event, with or without today's action; never by a disabled one, a schedule or another event.
func TestTrigger_NamesDelivery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		on      string
		enabled bool
		event   string
		want    bool
	}{
		{"github-acme", true, evPush, true},
		{"github-acme.check_run", true, evPush, false},
		{"github-acme.push", true, evPush, true},
		{"webhook.github-acme", true, evPush, true},
		{"github.check_run", true, evCheckRun, true},
		{"github.check_run.failed", true, evCheckRun, true},
		{"github.check_run", true, "check_suite", false},
		{"github.check_run", false, evCheckRun, false},
		{"gitlab.check_run", true, evCheckRun, false},
		{string(SenderGitHub), true, "", false},
		{OnSchedule, true, "", false},
		{"other-hook", true, evPush, false},
	}

	for _, tc := range tests {
		tr := Trigger{Name: "t", Enabled: tc.enabled, On: tc.on}
		if got := tr.NamesDelivery("github-acme", SenderGitHub, tc.event); got != tc.want {
			t.Errorf("on %q enabled %v event %q: NamesDelivery = %v; want %v", tc.on, tc.enabled, tc.event, got, tc.want)
		}
	}
}
