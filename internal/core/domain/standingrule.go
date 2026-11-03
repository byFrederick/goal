package domain

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"time"
)

// ConnectionToolMode is what a run may do with one tool of an organization connection (ADR 0031
// decision 1): run it, ask a person first, or refuse it. A tool with no mode set is ToolModeAllow.
type ConnectionToolMode string

// The modes of a connection's tool.
const (
	ToolModeAllow ConnectionToolMode = "allow"
	ToolModeAsk   ConnectionToolMode = "ask"
	ToolModeOff   ConnectionToolMode = "off"
)

// MaxToolName bounds a tool's name, as an MCP server lists it.
const MaxToolName = 128

var (
	toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	uuidPattern     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// ParseConnectionToolMode is s as a mode: allow, ask or off. Anything else wraps ErrInvalid.
func ParseConnectionToolMode(s string) (ConnectionToolMode, error) {
	switch m := ConnectionToolMode(s); m {
	case ToolModeAllow, ToolModeAsk, ToolModeOff:
		return m, nil
	default:
		return "", fmt.Errorf("tool mode %q is not allow, ask or off: %w", s, ErrInvalid)
	}
}

// CheckToolName enforces a tool's name: 1 to MaxToolName letters, digits, underscores, dots or
// dashes, without the connection prefix. The error wraps ErrInvalid.
func CheckToolName(name string) error {
	if !toolNamePattern.MatchString(name) {
		return fmt.Errorf("tool %q must be 1 to %d letters, digits, underscores, dots or dashes: %w", name, MaxToolName, ErrInvalid)
	}

	return nil
}

// CheckToolModes enforces every tool name and mode of a connection's modes. The error wraps
// ErrInvalid.
func CheckToolModes(modes map[string]ConnectionToolMode) error {
	var errs []error

	for _, tool := range slices.Sorted(maps.Keys(modes)) {
		if err := CheckToolName(tool); err != nil {
			errs = append(errs, err)
		}

		if _, err := ParseConnectionToolMode(string(modes[tool])); err != nil {
			errs = append(errs, fmt.Errorf("tool %q: %w", tool, err))
		}
	}

	return errors.Join(errs...)
}

// StandingRuleID is a standing rule's id (a UUID).
type StandingRuleID string

// StandingRule lets one agent role call one tool without asking (ADR 0031 decision 7). A rule is
// keyed by an organization connection, for every factory of the organization (ConnectionID set),
// or, on secrets, by one MCP server of one environment of one factory (ADR 0037 decision 8:
// Factory, Environment and Server set, ConnectionID ""). It is never deleted: revoking it sets
// RevokedAt and RevokedBy, and deleting its connection revokes a connection's rule.
type StandingRule struct {
	ID           StandingRuleID
	ConnectionID ConnectionID
	// Connection is the connection's name, or "" once the connection is deleted or for a rule
	// keyed by server.
	Connection string
	// Factory, Environment and Server key a rule on secrets: the environment file's name and its
	// MCP server's name (mcp.<server>).
	Factory     FactoryID
	Environment string
	Server      string
	Tool        string
	// Agent is the agent's role, its name in the definition (architect).
	Agent     string
	CreatedBy Actor
	CreatedAt time.Time
	// ItemID is the item the rule was approved on.
	ItemID    ItemID
	RevokedBy *Actor
	RevokedAt *time.Time
}

// Active reports whether r is not revoked.
func (r StandingRule) Active() bool { return r.RevokedAt == nil && r.RevokedBy == nil }

// NewStandingRule is a standing rule to create: the connection by name, the tool, the agent role
// and the item it is approved on.
type NewStandingRule struct {
	Connection, Tool, Agent string
	Item                    ItemID
}

// Check enforces n's names and item id. The error wraps ErrInvalid.
func (n NewStandingRule) Check() error {
	errs := []error{CheckConnectionName(n.Connection), CheckToolName(n.Tool)}

	if !namePattern.MatchString(n.Agent) {
		errs = append(errs, fmt.Errorf("agent %q must be an agent's name in the definition: %w", n.Agent, ErrInvalid))
	}

	if !uuidPattern.MatchString(string(n.Item)) {
		errs = append(errs, fmt.Errorf("item %q is not an item id: %w", n.Item, ErrInvalid))
	}

	return errors.Join(errs...)
}

// NewServerStandingRule is a standing rule to create on secrets (ADR 0037 decision 8): the
// factory, the environment file's name, its MCP server's name, the tool, the agent role and the
// item it is approved on.
type NewServerStandingRule struct {
	Factory                          FactoryID
	Environment, Server, Tool, Agent string
	Item                             ItemID
}

// Check enforces n's factory id, names and item id. The error wraps ErrInvalid.
func (n NewServerStandingRule) Check() error {
	errs := []error{CheckToolName(n.Tool)}

	if !uuidPattern.MatchString(string(n.Factory)) {
		errs = append(errs, fmt.Errorf("factory %q is not a factory id: %w", n.Factory, ErrInvalid))
	}

	if !namePattern.MatchString(n.Environment) {
		errs = append(errs, fmt.Errorf("environment %q must be an environment file's name: %w", n.Environment, ErrInvalid))
	}

	if !namePattern.MatchString(n.Server) {
		errs = append(errs, fmt.Errorf("server %q must be an MCP server's name: %w", n.Server, ErrInvalid))
	}

	if !namePattern.MatchString(n.Agent) {
		errs = append(errs, fmt.Errorf("agent %q must be an agent's name in the definition: %w", n.Agent, ErrInvalid))
	}

	if !uuidPattern.MatchString(string(n.Item)) {
		errs = append(errs, fmt.Errorf("item %q is not an item id: %w", n.Item, ErrInvalid))
	}

	return errors.Join(errs...)
}

// Audit actions of standing rules (ADR 0031 decision 7).
const (
	AuditStandingRuleCreated = "standing_rule.created"
	AuditStandingRuleRevoked = "standing_rule.revoked"
)

// AuditTargetStandingRule is the audit target kind of a standing rule.
const AuditTargetStandingRule = "standing_rule"
