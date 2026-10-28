package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestConnectionToolMode_ParsesOnlyAllowAskAndOff(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]ConnectionToolMode{"allow": ToolModeAllow, "ask": ToolModeAsk, "off": ToolModeOff} {
		got, err := ParseConnectionToolMode(in)
		if err != nil || got != want {
			t.Errorf("ParseConnectionToolMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	for _, in := range []string{"", "Allow", "deny", "ask first", "required"} {
		if _, err := ParseConnectionToolMode(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseConnectionToolMode(%q) = %v; want ErrInvalid", in, err)
		}
	}
}

func TestConnectionToolMode_CheckToolModes(t *testing.T) {
	t.Parallel()

	ok := map[string]ConnectionToolMode{"create_comment": ToolModeAsk, "get_issue": ToolModeAllow, "delete.repo-1": ToolModeOff}
	if err := CheckToolModes(ok); err != nil {
		t.Errorf("CheckToolModes(%v) = %v; want nil", ok, err)
	}

	if err := CheckToolModes(map[string]ConnectionToolMode{}); err != nil {
		t.Errorf("CheckToolModes of no modes = %v; want nil", err)
	}

	for name, modes := range map[string]map[string]ConnectionToolMode{
		"an unknown mode":     {"create_comment": "deny"},
		"an empty tool name":  {"": ToolModeAsk},
		"a long tool name":    {strings.Repeat("t", MaxToolName+1): ToolModeAsk},
		"a tool with a space": {"create comment": ToolModeAsk},
	} {
		if err := CheckToolModes(modes); !errors.Is(err, ErrInvalid) {
			t.Errorf("CheckToolModes with %s = %v; want ErrInvalid", name, err)
		}
	}
}

func TestNewStandingRule_Check(t *testing.T) {
	t.Parallel()

	valid := NewStandingRule{Connection: "acme-git", Tool: "create_comment", Agent: "architect", Item: "00000000-0000-7000-8000-0000000000aa"}
	if err := valid.Check(); err != nil {
		t.Fatalf("Check(%+v) = %v; want nil", valid, err)
	}

	for name, change := range map[string]func(*NewStandingRule){
		"an empty tool":          func(n *NewStandingRule) { n.Tool = "" },
		"an over-long tool":      func(n *NewStandingRule) { n.Tool = strings.Repeat("t", MaxToolName+1) },
		"an empty agent":         func(n *NewStandingRule) { n.Agent = "" },
		"an over-long agent":     func(n *NewStandingRule) { n.Agent = "a" + strings.Repeat("b", 40) },
		"an agent in capitals":   func(n *NewStandingRule) { n.Agent = "Architect" },
		"a malformed connection": func(n *NewStandingRule) { n.Connection = "Acme Git" },
		"no item":                func(n *NewStandingRule) { n.Item = "" },
		"an item that is no id":  func(n *NewStandingRule) { n.Item = "item-1" },
	} {
		n := valid
		change(&n)

		if err := n.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("Check with %s = %v; want ErrInvalid", name, err)
		}
	}
}

func TestNewStandingRule_ActiveUntilRevoked(t *testing.T) {
	t.Parallel()

	r := StandingRule{ID: "r1", Tool: "create_comment", Agent: "architect"}
	if !r.Active() {
		t.Errorf("a rule without revoked_at is not active")
	}

	r.RevokedBy = &LocalUser
	if r.Active() {
		t.Errorf("a revoked rule is still active")
	}
}
