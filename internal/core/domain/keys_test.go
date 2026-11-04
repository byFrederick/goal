package domain

import (
	"strings"
	"testing"
)

func TestLooksLikeKey(t *testing.T) {
	t.Parallel()

	token := strings.Repeat("a1B2", 5) // 20 token characters

	for _, tc := range []struct {
		name, text string
		want       bool
	}{
		{"a GitHub token alone", "ghp_" + token, true},
		{"a key in prose", "Set it to sk-" + token + " and restart.", true},
		{"a key after a quote", `token: "glpat-` + token + `"`, true},
		{"a Slack token on its own line", "intro\nxoxb-" + token + "\n", true},
		{"a fine-grained token", "github_pat_" + token, true},
		{"too short after the prefix", "sk-" + token[:19], false},
		{"inside a word", "risk-" + token, false},
		{"prose with a prefix", "A risk-free change; ask-me-anything sessions.", false},
		{"nothing", "", false},
	} {
		if got := LooksLikeKey(tc.text); got != tc.want {
			t.Errorf("%s: LooksLikeKey(%q) = %v, want %v", tc.name, tc.text, got, tc.want)
		}
	}
}
