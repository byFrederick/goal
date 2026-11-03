package knowledge_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/knowledge"
)

// The world's sources.
const (
	srcHandbook = "handbook"
	srcRunbooks = "runbooks"
)

// Check refuses what Upload would, before anything is stored, so a request of several files can
// be refused whole.
func TestUploads_CheckRefusesWhatUploadWould(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	small := knowledge.NewUploads(w.kb, w.objects, 8)

	for _, tc := range []struct {
		name, file, body string
		want             error
	}{
		{name: "markdown", file: "a.md", body: "# A"},
		{name: "too large", file: "a.md", body: "# 123456789", want: ports.ErrTooLarge},
		{name: "unsupported", file: "deck.key", body: "PK", want: domain.ErrUnsupportedDocument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := small.Check(tc.file, []byte(tc.body)); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Errorf("Check(%s) = %v, want %v", tc.file, err, tc.want)
			}
		})
	}
}

// The reader of an upload source lists its files by content hash and hands over each original from
// the bucket.
func TestUploads_ListAndFetchTheOriginals(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()

	body := []byte("# Leave\n\nAsk your lead.")

	if _, err := w.uploads.Upload(ctx, srcHandbook, "leave.md", body, admin); err != nil {
		t.Fatal(err)
	}

	spec := domain.SyncSpec{Source: w.source(srcHandbook)}

	var items []domain.SourceItem

	for item, err := range w.uploads.List(ctx, spec) {
		if err != nil {
			t.Fatal(err)
		}

		items = append(items, item)
	}

	if len(items) != 1 || items[0].Title != "leave.md" || items[0].Version != string(domain.HashBlob(body)) {
		t.Fatalf("List = %+v; want leave.md at its content hash", items)
	}

	doc, err := w.uploads.Fetch(ctx, spec, items[0].ID)
	if err != nil || !bytes.Equal(doc.Body, body) {
		t.Fatalf("Fetch = %q, %v; want the original", doc.Body, err)
	}

	if _, err := w.uploads.Fetch(ctx, spec, "not-a-hash"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("Fetch of no hash = %v, want invalid", err)
	}
}
