package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// KeyPrefixes start the values of tokens and keys that are never plain text (ADR 0037's
// plain-key patterns): Anthropic and OpenAI keys, GitHub classic and fine-grained tokens, GitLab
// tokens and Slack bot and user tokens.
var KeyPrefixes = []string{"sk-", "ghp_", "github_pat_", "glpat-", "xoxb-", "xoxp-"}

// minKeyChars is how many token characters follow a prefix in a key: real ones carry far more,
// and fewer is prose or an example ("sk-...").
const minKeyChars = 20

// keyPattern finds a key prefix at a word boundary followed by minKeyChars token characters.
var keyPattern = func() *regexp.Regexp {
	quoted := make([]string, len(KeyPrefixes))
	for i, p := range KeyPrefixes {
		quoted[i] = regexp.QuoteMeta(p)
	}

	return regexp.MustCompile(fmt.Sprintf(`(?:^|[^A-Za-z0-9_-])(?:%s)[A-Za-z0-9_-]{%d,}`, strings.Join(quoted, "|"), minKeyChars))
}()

// LooksLikeKey reports whether text holds what looks like a key: one of KeyPrefixes, not inside
// a word, followed by at least 20 letters, digits, dashes or underscores. A knowledge sync leaves
// such a file out (ADR 0038 decision 4).
func LooksLikeKey(text string) bool {
	return keyPattern.MatchString(text)
}
