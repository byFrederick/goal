package knowledge_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/knowledge"
	"github.com/byFrederick/goal/internal/ports"
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
		{name: "unsupported", file: "deck.pptx", body: "PK", want: domain.ErrUnsupportedDocument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := small.Check(tc.file, []byte(tc.body)); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Errorf("Check(%s) = %v, want %v", tc.file, err, tc.want)
			}
		})
	}
}

// Original is an uploaded document's file as it was uploaded; a synced document's original stays
// at its source.
func TestUploads_OriginalOfAnUploadOnly(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	ctx := w.ctx()

	body := []byte("# Leave\n\nAsk your lead.")

	d, err := w.uploads.Upload(ctx, srcHandbook, "leave.md", body, admin)
	if err != nil {
		t.Fatal(err)
	}

	got, raw, err := w.uploads.Original(ctx, d.ID)
	if err != nil || got.ID != d.ID || got.Title != "leave.md" || !bytes.Equal(raw, body) {
		t.Fatalf("Original = %+v, %q, %v; want leave.md as uploaded", got, raw, err)
	}

	w.docs.set(map[string]string{deployMD: "# Deploy\n\nShip it."})
	w.syncDue(w.ingestor(nil, nil))

	synced := w.documents(srcRunbooks)[deployMD]
	if _, _, err := w.uploads.Original(ctx, synced.ID); !errors.Is(err, domain.ErrNotUploadSource) {
		t.Errorf("Original of a synced document = %v, want not an upload source", err)
	}

	if _, _, err := w.uploads.Original(ctx, "019a0000-0000-7000-8000-000000000999"); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("Original of no document = %v, want not found", err)
	}
}
