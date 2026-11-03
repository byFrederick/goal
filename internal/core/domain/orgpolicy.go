package domain

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ToolSetting is the value of one of the organization policy's tool defaults (ADR 0024 decision
// 2). Each default takes only some of the values: OrgPolicy.Validate checks which.
type ToolSetting string

// The tool settings.
const (
	ToolOff            ToolSetting = "off"
	ToolAsk            ToolSetting = "ask"
	ToolAllowed        ToolSetting = "allowed"
	ToolReviewApproval ToolSetting = "review_approval" // only a review station with merge on approval merges
	ToolEnvironment    ToolSetting = "environment"     // Use secrets: an agent gets what its environment names (ADR 0037 decision 12)
	// ToolAttachedOnly is the deprecated alias of ToolEnvironment, the Use credentials value
	// before ADR 0037. Validate accepts it and Normalized saves it as ToolEnvironment.
	ToolAttachedOnly ToolSetting = "attached_only"
)

// The values each tool default takes, as 00069's CHECK constraints list them (use_credentials as
// 00092 widens it).
var (
	WriteOutsideBranchSettings = []ToolSetting{ToolOff, ToolAsk, ToolAllowed}
	MergePullRequestSettings   = []ToolSetting{ToolOff, ToolReviewApproval}
	ReadWebSettings            = []ToolSetting{ToolOff, ToolAsk, ToolAllowed}
	UseCredentialsSettings     = []ToolSetting{ToolOff, ToolEnvironment, ToolAttachedOnly}
)

// DefaultBoxTimeLimitMinutes is the default policy's box time limit: 3 hours.
const DefaultBoxTimeLimitMinutes = 180

// OrgPolicy is an organization's policy (ADR 0024): a ceiling over every factory definition, which
// may narrow it and never widen it. An organization that never saved one has DefaultOrgPolicy.
type OrgPolicy struct {
	// AllowedModels are the model ids agents may use, a non-empty subset of the platform's models
	// (GOAL_MODELS); DefaultModel, one of them, is the model of an agent that names none.
	AllowedModels []string
	DefaultModel  string
	// DeniedCommands are Claude Code Bash permission patterns, such as `Bash(terraform apply:*)`,
	// that no agent in the organization may run.
	DeniedCommands []string
	// The tool policy defaults.
	WriteOutsideBranch ToolSetting // comments, issues, tickets and messages
	MergePullRequest   ToolSetting
	ReadWeb            ToolSetting // search, fetch and documentation tools
	UseCredentials     ToolSetting // Use secrets (ADR 0037 decision 12): environment or off
	// MCPToolModes is the organization's cap on MCP tool modes (ADR 0037 decision 12): a tool name
	// or glob (*, ?) to allow, ask or off, the form domain.ServerToolModes takes as its limit. The
	// stricter of a tool's mode and the cap's wins. Empty means no cap.
	MCPToolModes map[string]ConnectionToolMode
	// BoxTimeLimitMinutes is how long a box may run before the engine stops it.
	BoxTimeLimitMinutes int
	// Revision counts the saves: 0 for the default policy, then one more for each Put. A Put
	// names the revision it read, so a concurrent change is refused instead of lost.
	Revision  int64
	UpdatedBy Actor
	UpdatedAt time.Time
}

// AuditOrgPolicyUpdated is the audit action of a saved organization policy. The audit log takes
// actions of the form <noun>.<past_verb> (00005), so the noun is org_policy.
const AuditOrgPolicyUpdated = "org_policy.updated"

// AuditTargetOrgPolicy is the audit target kind of the organization policy; its id is the
// organization's.
const AuditTargetOrgPolicy = "org_policy"

// DefaultOrgPolicy is the policy of an organization without one, over the platform's models:
// every model allowed, the first the default, no denied commands, the tool defaults ask,
// review_approval, allowed and environment, no MCP tool mode cap and a 3-hour box time limit.
func DefaultOrgPolicy(models []string) OrgPolicy {
	p := OrgPolicy{
		AllowedModels:      slices.Clone(models),
		WriteOutsideBranch: ToolAsk, MergePullRequest: ToolReviewApproval, ReadWeb: ToolAllowed, UseCredentials: ToolEnvironment,
		MCPToolModes:        map[string]ConnectionToolMode{},
		BoxTimeLimitMinutes: DefaultBoxTimeLimitMinutes,
	}

	if len(models) > 0 {
		p.DefaultModel = models[0]
	}

	return p
}

// Normalized is p with each deprecated value replaced by the one it stands for: Use secrets
// attached_only becomes environment, so a save from an older client is stored and read back as
// environment.
func (p OrgPolicy) Normalized() OrgPolicy {
	if p.UseCredentials == ToolAttachedOnly {
		p.UseCredentials = ToolEnvironment
	}

	return p
}

// Validate enforces the policy's rules: at least one allowed model, none blank or repeated, a
// default among them, each tool default one of its values, no blank denied command, each MCP cap
// pattern a tool name or glob with a mode of allow, ask or off, and a positive time limit. The error wraps ErrInvalid and lists every broken rule. Whether the platform offers
// each model is not checked here.
func (p OrgPolicy) Validate() error {
	var errs []error

	if len(p.AllowedModels) == 0 {
		errs = append(errs, fmt.Errorf("a policy allows at least one model: %w", ErrInvalid))
	}

	for i, m := range p.AllowedModels {
		switch {
		case strings.TrimSpace(m) == "":
			errs = append(errs, fmt.Errorf("allowed model %d is blank: %w", i+1, ErrInvalid))
		case slices.Contains(p.AllowedModels[:i], m):
			errs = append(errs, fmt.Errorf("model %s is allowed twice: %w", m, ErrInvalid))
		}
	}

	if !slices.Contains(p.AllowedModels, p.DefaultModel) {
		errs = append(errs, fmt.Errorf("the default model %q is not one of the allowed models: %w", p.DefaultModel, ErrInvalid))
	}

	for i, c := range p.DeniedCommands {
		if strings.TrimSpace(c) == "" {
			errs = append(errs, fmt.Errorf("denied command %d is blank: %w", i+1, ErrInvalid))
		}
	}

	errs = append(errs,
		checkToolSetting("write outside the item's branch", p.WriteOutsideBranch, WriteOutsideBranchSettings),
		checkToolSetting("merge a pull request", p.MergePullRequest, MergePullRequestSettings),
		checkToolSetting("read the web and docs", p.ReadWeb, ReadWebSettings),
		checkToolSetting("use secrets", p.UseCredentials, UseCredentialsSettings))

	errs = append(errs, CheckMCPToolCap(p.MCPToolModes))

	if p.BoxTimeLimitMinutes <= 0 {
		errs = append(errs, fmt.Errorf("the box time limit is a positive number of minutes, not %d: %w", p.BoxTimeLimitMinutes, ErrInvalid))
	}

	return errors.Join(errs...)
}

// CheckMCPToolCap enforces an MCP tool mode cap: each pattern 1 to MaxToolName letters, digits,
// underscores, dots, dashes, * or ?, each mode allow, ask or off. The error wraps ErrInvalid.
func CheckMCPToolCap(modes map[string]ConnectionToolMode) error {
	var errs []error

	for _, pattern := range slices.Sorted(maps.Keys(modes)) {
		if !mcpToolPattern.MatchString(pattern) {
			errs = append(errs, fmt.Errorf("MCP tool pattern %q must be 1 to %d letters, digits, underscores, dots, dashes, * or ?: %w",
				pattern, MaxToolName, ErrInvalid))
		}

		if _, err := ParseConnectionToolMode(string(modes[pattern])); err != nil {
			errs = append(errs, fmt.Errorf("MCP tool pattern %q: %w", pattern, err))
		}
	}

	return errors.Join(errs...)
}

// mcpToolPattern is an MCP cap's pattern: a tool name (CheckToolName) that may hold the globs * and ?.
var mcpToolPattern = regexp.MustCompile(`^[A-Za-z0-9_.*?-]{1,128}$`)

func checkToolSetting(name string, v ToolSetting, valid []ToolSetting) error {
	if slices.Contains(valid, v) {
		return nil
	}

	return fmt.Errorf("%s is %q, not one of %v: %w", name, v, valid, ErrInvalid)
}
