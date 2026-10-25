package knowledge

import (
	"strings"
	"testing"
)

// A batch stays under the model's request limit however long or dense its texts: at most 64
// texts and, padded to the longest, about 40,000 tokens (Workers AI's bge-m3 refuses a request
// over 60,000).
func TestBatchEnd_BoundsCountAndPaddedTokens(t *testing.T) {
	t.Parallel()

	repeat := func(n int, s string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = s
		}

		return out
	}

	for name, tc := range map[string]struct {
		texts []string
		want  int
	}{
		"short texts":           {repeat(100, "a short text"), embedBatch},
		"full ASCII pieces":     {repeat(100, strings.Repeat("abc ", pieceChars/4)), 58},
		"box drawing":           {repeat(100, strings.Repeat("─", pieceChars)), 9},
		"one long among short":  {append([]string{strings.Repeat("─", pieceChars)}, repeat(99, "a")...), 9},
		"a text over the limit": {repeat(3, strings.Repeat("─", 30000)), 1},
		"fewer than a batch":    {repeat(3, "a"), 3},
	} {
		if got := batchEnd(tc.texts, 0); got != tc.want {
			t.Errorf("%s: batch of %d, want %d", name, got, tc.want)
		}
	}
}
