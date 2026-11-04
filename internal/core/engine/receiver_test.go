package engine_test

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// A delivery is named by an enabled trigger of a loaded factory the webhook is open to; not by a
// factory it is closed to, nor in another organization.
func TestNamesDelivery_LoadedFactoriesItIsOpenTo(t *testing.T) {
	t.Parallel()

	h := intakeHarness(t,
		domain.Trigger{Name: "on-check-run", Enabled: true, On: "github.check_run"},
		domain.Trigger{Name: "on-push", Enabled: false, On: "github.push"},
	)
	open := domain.WebhookEndpoint{Name: "github-acme", Sender: domain.SenderGitHub, Openings: []domain.WebhookOpening{{FactoryID: factoryID}}}
	closed := domain.WebhookEndpoint{Name: "github-acme", Sender: domain.SenderGitHub}

	for _, tc := range []struct {
		name  string
		w     domain.WebhookEndpoint
		event string
		want  bool
	}{
		{"named", open, "check_run", true},
		{"disabled trigger", open, "push", false},
		{"other event", open, "issues", false},
		{"closed", closed, "check_run", false},
	} {
		if got := h.e.NamesDelivery(inOrg(t), tc.w, tc.event); got != tc.want {
			t.Errorf("%s: NamesDelivery = %v; want %v", tc.name, got, tc.want)
		}
	}

	if h.e.NamesDelivery(domain.WithOrg(t.Context(), "019a0000-0000-7000-8000-0000000000ff"), open, "check_run") {
		t.Error("another organization's NamesDelivery = true; want false")
	}
}
