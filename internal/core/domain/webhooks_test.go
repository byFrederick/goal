package domain

import (
	"strings"
	"testing"
)

// A webhook's secret is webhook-<name>, cut to a secret name's 40 characters without a trailing
// hyphen.
func TestWebhookSecretName(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, webhook, want string }{
		{"short", "ci-hooks", "webhook-ci-hooks"},
		{"cut", strings.Repeat("b", 40), "webhook-" + strings.Repeat("b", 32)},
		{"cut to a hyphen", strings.Repeat("c", 31) + "-d", "webhook-" + strings.Repeat("c", 31)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := WebhookSecretName(tc.webhook)
			if got != tc.want {
				t.Errorf("WebhookSecretName(%q) = %q; want %q", tc.webhook, got, tc.want)
			}

			if err := CheckSecretName(got); err != nil {
				t.Errorf("the secret name %q is refused: %v", got, err)
			}
		})
	}
}

// A trigger's on: gives the event a webhook's sender must send: by its name or its sender, every
// event for its bare name, and nothing for another webhook, another sender, a sender without an
// event or a schedule.
func TestParseOn_SetupEvent(t *testing.T) {
	t.Parallel()

	w := WebhookEndpoint{Name: "acme-ci", Sender: SenderGitHub}

	cases := []struct {
		on    string
		event string
		all   bool
		ok    bool
	}{
		{on: "acme-ci.check_run", event: "check_run", ok: true},
		{on: "webhook.acme-ci.workflow_run", event: "workflow_run", ok: true},
		{on: "github.issues", event: "issues", ok: true},
		{on: OnCheckRunFailed, event: "check_run", ok: true},
		{on: "acme-ci", all: true, ok: true},
		{on: "webhook.acme-ci", all: true, ok: true},
		{on: "other.push"},
		{on: "gitlab.push"},
		{on: string(SenderGitHub)},
		{on: OnSchedule},
	}

	for _, c := range cases {
		event, all, ok := SetupEvent(c.on, w)
		if event != c.event || all != c.all || ok != c.ok {
			t.Errorf("SetupEvent(%q) = %q, %v, %v; want %q, %v, %v", c.on, event, all, ok, c.event, c.all, c.ok)
		}
	}
}
