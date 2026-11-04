package domain

import (
	"slices"
	"strings"
	"testing"
)

// A converted endpoint keeps its name unless it is reserved or taken, and then takes the first
// free suffix; a base too long for the suffix is cut, without a trailing hyphen.
func TestConvertedWebhookName(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 37) + "-bc" // 40 characters, a hyphen at 38

	tests := []struct {
		name, from string
		taken      []string
		want       string
	}{
		{"free", "ci-hooks", nil, "ci-hooks"},
		{"reserved", "github", nil, "github-2"},
		{"reserved and suffix taken", "github", []string{"github-2"}, "github-3"},
		{"taken", "ci-hooks", []string{"ci-hooks", "ci-hooks-2", "ci-hooks-3"}, "ci-hooks-4"},
		{"long and taken", strings.Repeat("a", 40), []string{strings.Repeat("a", 40)}, strings.Repeat("a", 38) + "-2"},
		{"cut at a hyphen", long, []string{long}, strings.Repeat("a", 37) + "-2"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ConvertedWebhookName(tc.from, func(n string) bool { return slices.Contains(tc.taken, n) })
			if got != tc.want {
				t.Errorf("ConvertedWebhookName(%q) = %q; want %q", tc.from, got, tc.want)
			}

			if err := CheckWebhookName(got); err != nil {
				t.Errorf("the converted name %q is refused: %v", got, err)
			}
		})
	}
}

// A converted endpoint's secret is webhook-<name> cut to a secret's name, or with the first free
// suffix when another secret has it.
func TestConvertedSecretName(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("b", 40)

	tests := []struct {
		name, webhook string
		taken         []string
		want          string
	}{
		{"free", "ci-hooks", nil, "webhook-ci-hooks"},
		{"taken", "ci-hooks", []string{"webhook-ci-hooks", "webhook-ci-hooks-2"}, "webhook-ci-hooks-3"},
		{"cut", long, nil, "webhook-" + strings.Repeat("b", 32)},
		{"cut and taken", long, []string{"webhook-" + strings.Repeat("b", 32)}, "webhook-" + strings.Repeat("b", 30) + "-2"},
		{"cut to a hyphen", strings.Repeat("c", 31) + "-d", nil, "webhook-" + strings.Repeat("c", 31)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ConvertedSecretName(tc.webhook, func(n string) bool { return slices.Contains(tc.taken, n) })
			if got != tc.want {
				t.Errorf("ConvertedSecretName(%q) = %q; want %q", tc.webhook, got, tc.want)
			}

			if err := CheckSecretName(got); err != nil {
				t.Errorf("the secret name %q is refused: %v", got, err)
			}
		})
	}
}
