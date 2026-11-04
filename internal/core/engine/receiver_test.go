package engine_test

import (
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
)

// A delivery is evaluated by a loaded factory the webhook is open to with an enabled trigger that
// names it; not by a factory it is closed to, nor in another organization, and GitHub's ping by
// none.
func TestEvaluations_LoadedFactoriesItIsOpenTo(t *testing.T) {
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
		{"ping", open, "ping", false},
	} {
		got := h.e.Evaluations(inOrg(t), tc.w, tc.event, []byte(`{}`))
		if (len(got) == 1) != tc.want || tc.want && (got[0].FactoryID != factoryID || got[0].Triggers[0] != "on-check-run") {
			t.Errorf("%s: Evaluations = %+v; want one for the factory: %v", tc.name, got, tc.want)
		}
	}

	if got := h.e.Evaluations(domain.WithOrg(t.Context(), "019a0000-0000-7000-8000-0000000000ff"), open, "check_run", nil); len(got) != 0 {
		t.Errorf("another organization's Evaluations = %+v; want none", got)
	}
}
