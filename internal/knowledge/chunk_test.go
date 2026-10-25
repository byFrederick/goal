package knowledge

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func headingsOf(pieces []Piece) []string {
	out := make([]string, 0, len(pieces))
	for _, p := range pieces {
		out = append(out, p.Heading)
	}

	return out
}

func TestChunk_SectionsByHeadingPath(t *testing.T) {
	t.Parallel()

	doc := "Intro line.\n\n# Install\n\nGet it.\n\n## Docker\n\nRun it.\n\n```sh\n# not a heading\necho hi\n```\n\n## Binary\n\nDownload.\n\n# Use\n\nGo."

	pieces := Chunk(doc)

	want := []string{"", "Install", "Install > Docker", "Install > Binary", "Use"}
	if got := headingsOf(pieces); !slices.Equal(got, want) {
		t.Fatalf("headings = %q, want %q", got, want)
	}

	if !strings.Contains(pieces[2].Text, "# not a heading") || !strings.HasPrefix(pieces[2].Text, "## Docker") {
		t.Errorf("Docker piece = %q, want its heading line first and the fenced comment kept", pieces[2].Text)
	}

	for _, p := range pieces {
		if p.Overlap != "" {
			t.Errorf("a short section's only piece has an overlap: %+v", p)
		}
	}
}

func TestChunk_HeadingWithoutTextJoinsTheNextSection(t *testing.T) {
	t.Parallel()

	pieces := Chunk("# Guide\n\n## Start\n\nDo this.")
	if len(pieces) != 1 || pieces[0].Heading != "Guide > Start" || pieces[0].Text != "# Guide\n\n## Start\n\nDo this." {
		t.Errorf("Chunk = %+v, want one piece under Guide > Start with both heading lines", pieces)
	}

	if got := Chunk("# Only a title"); len(got) != 1 || got[0].Text != "# Only a title" {
		t.Errorf("a document of one heading = %+v", got)
	}

	if got := Chunk("  \n\n "); len(got) != 0 {
		t.Errorf("blank text = %+v, want no piece", got)
	}
}

func TestChunk_LongSectionsSplitWithOverlap(t *testing.T) {
	t.Parallel()

	para := strings.Repeat("word ", 300) // 1500 characters
	long := strings.Repeat("x", 5000)    // a line without a space
	doc := "# Big\n\n" + para + "\n\n" + para + "\n\n" + long + "\n\n" + para

	pieces := Chunk(doc)
	if len(pieces) < 4 {
		t.Fatalf("%d pieces, want the section split", len(pieces))
	}

	var joined []string

	for i, p := range pieces {
		if n := utf8.RuneCountInString(p.Text); n > pieceChars {
			t.Errorf("piece %d has %d characters, more than %d", i, n, pieceChars)
		}

		if p.Heading != "Big" {
			t.Errorf("piece %d heading = %q", i, p.Heading)
		}

		if i > 0 && (p.Overlap == "" || utf8.RuneCountInString(p.Overlap) > overlapChars+3 ||
			!strings.HasSuffix(pieces[i-1].Text, strings.TrimSuffix(p.Overlap, " … "))) {
			t.Errorf("piece %d overlap = %q, want the end of the piece before", i, p.Overlap)
		}

		if !strings.Contains(p.EmbedText(), strings.TrimSpace(p.Text)) || !strings.HasPrefix(p.EmbedText(), "Big") {
			t.Errorf("piece %d embeds %q…, want its heading, overlap and text", i, p.EmbedText()[:20])
		}

		joined = append(joined, p.Text)
	}

	// The pieces are the document again, but for where they were cut.
	squash := func(s string) string { return strings.Join(strings.Fields(s), "") }
	if squash(strings.Join(joined, "")) != squash(doc) {
		t.Error("the pieces do not join back into the document")
	}
}

// Review 4 M4: cutting a long line was quadratic (a minified JSON upload of 25 MiB took minutes
// of one worker's CPU while it held its source's lease); it is linear. A time limit failed on busy
// run boxes (7 MiB took 10 s under -race there), so the test compares sizes instead: four times the
// text takes about four times as long when cutting is linear, sixteen when it is quadratic.
func TestChunk_LongLineIsLinear(t *testing.T) {
	t.Parallel()

	squash := func(s string) string { return strings.Join(strings.Fields(s), "") }

	for name, doc := range map[string]func(size int) string{
		"one line":         func(size int) string { return strings.Repeat(`{"key":"välue",`, size/16) + strings.Repeat("y", 3000) },
		"tiny paragraphs":  func(size int) string { return strings.Repeat("a\n\n", size/3) },
		"one line, spaces": func(size int) string { return strings.Repeat("wörd ", size/6) },
	} {
		small, big := doc(1<<20), doc(4<<20)
		smallTook, _ := fastestChunk(small)
		bigTook, pieces := fastestChunk(big)

		if growth := float64(bigTook) / float64(smallTook); growth > 8 {
			t.Errorf("%s: Chunk took %v for 1 MiB and %v for 4 MiB, %.1f times as long", name, smallTook, bigTook, growth)
		}

		var b strings.Builder

		for i, p := range pieces {
			if n := utf8.RuneCountInString(p.Text); n > pieceChars {
				t.Fatalf("%s: piece %d has %d characters", name, i, n)
			}

			b.WriteString(p.Text)
		}

		if squash(b.String()) != squash(big) {
			t.Errorf("%s: the pieces do not join back into the document", name)
		}
	}
}

// fastestChunk cuts doc twice and returns the quicker time: a busy machine slows one cut, rarely both.
func fastestChunk(doc string) (time.Duration, []Piece) {
	var (
		best   time.Duration
		pieces []Piece
	)

	for i := range 2 {
		start := time.Now()
		pieces = Chunk(doc)

		if took := time.Since(start); i == 0 || took < best {
			best = took
		}
	}

	return best, pieces
}
