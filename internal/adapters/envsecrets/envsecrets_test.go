package envsecrets

import (
	"errors"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/ports"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	const value = "s3cr3t-value"

	env := New(func(name string) (string, bool) {
		switch name {
		case "GOAL_TOKEN":
			return value, true
		case "GOAL_EMPTY":
			return "", true
		default:
			return "", false
		}
	})

	tests := []struct {
		name     string
		ref      string
		want     string
		notFound bool
		fails    bool
	}{
		{name: "env reference", ref: "env:GOAL_TOKEN", want: value},
		{name: "literal", ref: "application/json", want: "application/json"},
		{name: "unset variable", ref: "env:GOAL_MISSING", notFound: true},
		{name: "empty variable", ref: "env:GOAL_EMPTY", notFound: true},
		{name: "no variable name", ref: "env:", fails: true},
		{name: "1Password reference", ref: "op://vault/item/field", fails: true},
		{name: "AWS Secrets Manager ARN", ref: "arn:aws:secretsmanager:us-east-1:123456789012:secret:x", fails: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := env.Resolve(t.Context(), tt.ref)

			switch {
			case tt.notFound:
				if !errors.Is(err, ports.ErrSecretNotFound) {
					t.Fatalf("Resolve(%q) error = %v, want ErrSecretNotFound", tt.ref, err)
				}
			case tt.fails:
				if err == nil || errors.Is(err, ports.ErrSecretNotFound) {
					t.Fatalf("Resolve(%q) error = %v, want an unsupported-reference error", tt.ref, err)
				}
			case err != nil:
				t.Fatalf("Resolve(%q): %v", tt.ref, err)
			case got != tt.want:
				t.Fatalf("Resolve(%q) = %q, want %q", tt.ref, got, tt.want)
			}

			if err != nil && strings.Contains(err.Error(), value) {
				t.Fatalf("error leaks the secret value: %v", err)
			}
		})
	}
}
