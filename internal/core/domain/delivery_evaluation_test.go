package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

func TestEvaluationFor(t *testing.T) {
	t.Parallel()

	line := domain.Line{
		FactoryID: "f1", Name: "shop",
		Runtime: domain.Runtime{Repository: domain.Repository{URL: "https://github.com/acme/shop.git"}},
		Triggers: []domain.Trigger{
			{Name: "ci", Enabled: true, On: "github.check_run"},
			{Name: "issues", Enabled: true, On: "github.issues"},
			{Name: "off", Enabled: false, On: "github.check_run"},
		},
	}
	open := domain.WebhookEndpoint{ID: "w1", Name: "gh-acme", Sender: domain.SenderGitHub, Openings: []domain.WebhookOpening{{FactoryID: "f1"}}}
	limited := open
	limited.Openings = []domain.WebhookOpening{{FactoryID: "f1", Repositories: []string{"acme/shop", "acme/legacy"}}}

	ev := func(event, repo string) domain.IntakeEvent {
		return domain.IntakeEvent{Event: event, Fields: map[string]string{domain.FieldRepo: repo}}
	}

	tests := []struct {
		name     string
		w        domain.WebhookEndpoint
		ev       domain.IntakeEvent
		ok       bool
		triggers []string
		skip     string
	}{
		{"the factory's repository", open, ev("check_run", "acme/shop"), true, []string{"ci"}, ""},
		{"in any case", open, ev("check_run", "Acme/Shop"), true, []string{"ci"}, ""},
		{"an event no enabled trigger names", open, ev("push", "acme/shop"), false, nil, ""},
		{"a repository the factory doesn't have", open, ev("check_run", "acme/legacy"), true, []string{"ci"},
			"acme/legacy is not one of shop's repositories"},
		{"a repository outside the opening", limited, ev("check_run", "acme/other"), false, nil, ""},
		{"no repository and a limited opening", limited, ev("check_run", ""), false, nil, ""},
		{"a webhook not open to the factory", domain.WebhookEndpoint{ID: "w2", Name: "gh-acme", Sender: domain.SenderGitHub},
			ev("check_run", "acme/shop"), false, nil, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := domain.EvaluationFor(line, tc.w, tc.ev)
			if ok != tc.ok || strings.Join(got.Triggers, ",") != strings.Join(tc.triggers, ",") || got.Skip != tc.skip {
				t.Errorf("EvaluationFor = %+v, %v; want triggers %v, skip %q, %v", got, ok, tc.triggers, tc.skip, tc.ok)
			}
		})
	}
}

func TestFinishEvaluation_BacksOffThenFails(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	boom := errors.New("boom")

	var waits []time.Duration

	for n := 1; n < domain.MaxEvaluationAttempts; n++ {
		f := domain.FinishEvaluation(domain.ClaimedEvaluation{DeliveryEvaluation: domain.DeliveryEvaluation{ID: "e", Attempts: n}}, nil, boom, at)
		if f.State != domain.EvaluationPending || f.Reason != "boom" {
			t.Fatalf("attempt %d = %+v; want pending again with the error", n, f)
		}

		waits = append(waits, f.NextAt.Sub(at))
	}

	for i := 1; i < len(waits); i++ {
		if waits[i] <= waits[i-1] {
			t.Errorf("backoff %v; want each wait longer than the one before", waits)
		}
	}

	last := domain.FinishEvaluation(domain.ClaimedEvaluation{DeliveryEvaluation: domain.DeliveryEvaluation{ID: "e", Attempts: domain.MaxEvaluationAttempts}}, nil, boom, at)
	if last.State != domain.EvaluationFailed || last.Outcome != domain.EvaluationFailedOutcome || last.Reason != "boom" {
		t.Errorf("the last attempt = %+v; want failed with the error", last)
	}

	done := domain.FinishEvaluation(domain.ClaimedEvaluation{}, []domain.TriggerFiring{
		{Trigger: "a", Outcome: domain.FiringSkipped, Reason: "limits.perHour: full"},
		{Trigger: "b", Outcome: domain.FiringCreated, ItemID: "i2"},
	}, nil, at)
	if done.State != domain.EvaluationDone || done.Outcome != domain.EvaluationCreated || done.ItemID != "i2" {
		t.Errorf("done = %+v; want created with i2", done)
	}

	skipped := domain.FinishEvaluation(domain.ClaimedEvaluation{}, []domain.TriggerFiring{
		{Trigger: "a", Outcome: domain.FiringSkipped, Reason: "limits.perHour: full"},
	}, nil, at)
	if skipped.Outcome != domain.EvaluationSkipped || skipped.Reason != "trigger a: limits.perHour: full" {
		t.Errorf("skipped = %+v; want skipped with the trigger's reason", skipped)
	}
}

func TestFireLimits_Skip(t *testing.T) {
	t.Parallel()

	l := domain.FireLimits{DedupeField: "check.name", PerDay: 5, PerHour: 2}

	tests := []struct {
		c    domain.FireCounts
		want string
	}{
		{domain.FireCounts{Today: 4, LastHour: 1}, ""},
		{domain.FireCounts{OpenDedupe: "i1"}, "only.dedupe: item i1 is open for the same check.name"},
		{domain.FireCounts{Today: 5}, "limits.perDay: 5 items created today (UTC), the limit is 5"},
		{domain.FireCounts{Today: 3, LastHour: 2}, "limits.perHour: 2 items created in the last hour, the limit is 2"},
	}

	for _, tc := range tests {
		if got := l.Skip(tc.c); got != tc.want {
			t.Errorf("Skip(%+v) = %q; want %q", tc.c, got, tc.want)
		}
	}

	if got := (domain.FireLimits{}).Skip(domain.FireCounts{OpenDedupe: "i1", Today: 99, LastHour: 99}); got != "" {
		t.Errorf("no limits skip %q; want none", got)
	}
}
