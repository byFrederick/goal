package domain

import (
	"fmt"
	"slices"
	"time"
	"unicode/utf8"
)

// MaxEnvironmentTestError bounds a kept test's error, in characters; MaxEnvironmentTestTools the
// tool names it keeps.
const (
	MaxEnvironmentTestError = 2000
	MaxEnvironmentTestTools = 1000
)

// EnvironmentTest is the latest test of one remote MCP server of a factory's environment (ADR
// 0037 decision 9): when it ran, whether tools/list answered and the tool names it listed, or why
// not. ReadOnly are the names of the tools whose annotations.readOnlyHint was true (ADR 0041
// decision 15); MCP's default for a missing hint is false, so every other tool is not read-only.
// Error is scrubbed of every secret value the test opened before it is kept.
type EnvironmentTest struct {
	Factory     FactoryID
	Environment string
	Server      string
	At          time.Time
	OK          bool
	Error       string
	Tools       []string
	ReadOnly    []string
}

// Check refuses a test without a factory, an environment or server name, or a time, one that
// failed without an error or passed with one, an error over MaxEnvironmentTestError characters,
// more than MaxEnvironmentTestTools tools and a read-only name that is not one of its tools. The
// error wraps ErrInvalid.
func (t EnvironmentTest) Check() error {
	switch {
	case t.Factory == "" || !namePattern.MatchString(t.Environment) || !namePattern.MatchString(t.Server) || t.At.IsZero():
		return fmt.Errorf("an environment test needs a factory, an environment, a server and a time: %w", ErrInvalid)
	case t.OK == (t.Error != ""):
		return fmt.Errorf("an environment test that failed has an error, one that passed none: %w", ErrInvalid)
	case utf8.RuneCountInString(t.Error) > MaxEnvironmentTestError:
		return fmt.Errorf("an environment test's error has at most %d characters: %w", MaxEnvironmentTestError, ErrInvalid)
	case len(t.Tools) > MaxEnvironmentTestTools:
		return fmt.Errorf("an environment test keeps at most %d tools: %w", MaxEnvironmentTestTools, ErrInvalid)
	}

	for _, name := range t.ReadOnly {
		if !slices.Contains(t.Tools, name) {
			return fmt.Errorf("an environment test's read-only tool %q is not one of its tools: %w", name, ErrInvalid)
		}
	}

	return nil
}

// Clone is t with its own tools and read-only tools.
func (t EnvironmentTest) Clone() EnvironmentTest {
	t.Tools = slices.Clone(t.Tools)
	t.ReadOnly = slices.Clone(t.ReadOnly)

	return t
}
