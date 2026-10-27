package domain

import (
	"errors"
	"slices"
	"testing"
)

var testPolicyModels = []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}

func TestOrgPolicy_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		edit func(p *OrgPolicy)
		ok   bool
	}{
		{"the default policy", func(*OrgPolicy) {}, true},
		{"a narrowed policy", func(p *OrgPolicy) {
			p.AllowedModels, p.DefaultModel = []string{"claude-sonnet-5-5"}, "claude-sonnet-5-5"
			p.DeniedCommands = []string{"Bash(terraform apply:*)", "Bash(git push --force:*)"}
			p.WriteOutsideBranch, p.MergePullRequest, p.ReadWeb, p.UseCredentials = ToolOff, ToolOff, ToolAsk, ToolOff
			p.BoxTimeLimitMinutes = 30
		}, true},
		{"no allowed models", func(p *OrgPolicy) { p.AllowedModels = nil }, false},
		{"a default outside the allowed models", func(p *OrgPolicy) { p.DefaultModel = "claude-other-1" }, false},
		{"no default", func(p *OrgPolicy) { p.DefaultModel = "" }, false},
		{"a blank model", func(p *OrgPolicy) { p.AllowedModels = append(p.AllowedModels, " ") }, false},
		{"a duplicate model", func(p *OrgPolicy) { p.AllowedModels = append(p.AllowedModels, "claude-opus-5-5") }, false},
		{"an unknown write value", func(p *OrgPolicy) { p.WriteOutsideBranch = ToolReviewApproval }, false},
		{"an unknown merge value", func(p *OrgPolicy) { p.MergePullRequest = ToolAllowed }, false},
		{"an unknown web value", func(p *OrgPolicy) { p.ReadWeb = ToolAttachedOnly }, false},
		{"an unknown credentials value", func(p *OrgPolicy) { p.UseCredentials = ToolAsk }, false},
		{"an empty tool value", func(p *OrgPolicy) { p.ReadWeb = "" }, false},
		{"an empty denied command", func(p *OrgPolicy) { p.DeniedCommands = []string{""} }, false},
		{"a blank denied command", func(p *OrgPolicy) { p.DeniedCommands = []string{"Bash(rm -rf:*)", "  "} }, false},
		{"a time limit of 0", func(p *OrgPolicy) { p.BoxTimeLimitMinutes = 0 }, false},
		{"a negative time limit", func(p *OrgPolicy) { p.BoxTimeLimitMinutes = -5 }, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := DefaultOrgPolicy(testPolicyModels)
			tc.edit(&p)

			err := p.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate() = %v; want nil", err)
			}

			if !tc.ok && !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate() = %v; want ErrInvalid", err)
			}
		})
	}
}

func TestDefaultOrgPolicy(t *testing.T) {
	t.Parallel()

	p := DefaultOrgPolicy(testPolicyModels)

	if !slices.Equal(p.AllowedModels, testPolicyModels) || p.DefaultModel != "claude-opus-5-5" {
		t.Errorf("models %v, default %q; want every platform model, the first the default", p.AllowedModels, p.DefaultModel)
	}

	if len(p.DeniedCommands) != 0 {
		t.Errorf("denied commands %v; want none", p.DeniedCommands)
	}

	if p.WriteOutsideBranch != ToolAsk || p.MergePullRequest != ToolReviewApproval || p.ReadWeb != ToolAllowed ||
		p.UseCredentials != ToolAttachedOnly {
		t.Errorf("tool defaults %q %q %q %q; want ask, review_approval, allowed, attached_only",
			p.WriteOutsideBranch, p.MergePullRequest, p.ReadWeb, p.UseCredentials)
	}

	if p.BoxTimeLimitMinutes != 180 || p.Revision != 0 {
		t.Errorf("time limit %d, revision %d; want 180 and 0", p.BoxTimeLimitMinutes, p.Revision)
	}

	if err := p.Validate(); err != nil {
		t.Errorf("the default policy is invalid: %v", err)
	}
}

func TestDefaultOrgPolicy_OwnsItsModels(t *testing.T) {
	t.Parallel()

	models := slices.Clone(testPolicyModels)
	p := DefaultOrgPolicy(models)

	models[0] = "claude-changed"

	if p.AllowedModels[0] != "claude-opus-5-5" {
		t.Error("the default policy shares the caller's slice")
	}

	if d := DefaultOrgPolicy(nil); d.DefaultModel != "" || len(d.AllowedModels) != 0 {
		t.Errorf("DefaultOrgPolicy(nil) = %+v; want no models", d)
	}
}
