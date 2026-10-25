package knowledge

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

var _ ports.SourceReader = (*Uploads)(nil)

// Uploads keeps uploaded documents: their content in the bucket once per organization and
// content (domain.KnowledgeKey, written with PutIfAbsent, so an object is never rewritten), their
// rows in the knowledge base, pending until the next sync of their source indexes them. An
// object goes when the last document holding its content does, inside that deletion's
// transaction (ports.DropBlobs), and an upload writes its object only once its row holds the
// content, so the two never cross. It is also the source reader of upload sources.
type Uploads struct {
	kb      ports.Knowledge
	objects ports.ObjectStore
	max     int64
}

// NewUploads returns uploads into kb and objects of at most maxBytes each (domain.DefaultMaxUpload
// when not positive).
func NewUploads(kb ports.Knowledge, objects ports.ObjectStore, maxBytes int64) *Uploads {
	if maxBytes <= 0 {
		maxBytes = domain.DefaultMaxUpload
	}

	return &Uploads{kb: kb, objects: objects, max: maxBytes}
}

// Upload stores a file named name in the upload source named source, for the organization in
// ctx, and returns its document (pending; the same document when that content is there
// already). ports.ErrTooLarge over the size limit, domain.ErrUnsupportedDocument for a type the
// knowledge base does not index, and the Knowledge.AddUpload refusals.
func (u *Uploads) Upload(ctx context.Context, source, name string, body []byte, by domain.Actor) (domain.KnowledgeDocument, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.KnowledgeDocument{}, domain.ErrNoOrg
	}

	mime, err := u.check(name, body)
	if err != nil {
		return domain.KnowledgeDocument{}, err
	}

	// The row first: it holds the content against a deletion of another holder, and a refusal
	// leaves no object behind. A sync between the two finds no object and tries again once the
	// source is due.
	h := domain.HashBlob(body)

	d, err := u.kb.AddUpload(ctx, source, domain.KnowledgeDocument{Title: name, MIME: mime, Bytes: int64(len(body)), Blob: h}, by)
	if err != nil {
		return domain.KnowledgeDocument{}, err
	}

	if _, _, err := u.objects.PutIfAbsent(ctx, domain.KnowledgeKey(org, h), body); err != nil {
		return domain.KnowledgeDocument{}, fmt.Errorf("upload %q: store: %w", name, err)
	}

	if d.Status == domain.DocumentPending {
		if err := u.kb.SyncNow(ctx, source); err != nil {
			return domain.KnowledgeDocument{}, err
		}
	}

	return d, nil
}

// Check refuses a file Upload would refuse for itself, before anything is stored:
// ports.ErrTooLarge over the size limit, domain.ErrUnsupportedDocument for a type the knowledge
// base does not index.
func (u *Uploads) Check(name string, body []byte) error {
	_, err := u.check(name, body)

	return err
}

// check is Check that also returns the file's type.
func (u *Uploads) check(name string, body []byte) (string, error) {
	if int64(len(body)) > u.max {
		return "", fmt.Errorf("upload %q has %d bytes, more than %d: %w", name, len(body), u.max, ports.ErrTooLarge)
	}

	mime, err := domain.DocumentType(name, body)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}

	return mime, nil
}

// Original is an uploaded document with its file as it was uploaded. ports.ErrNotFound when
// there is none, domain.ErrNotUploadSource for a synced document, whose original stays at its
// source.
func (u *Uploads) Original(ctx context.Context, id domain.KnowledgeDocumentID) (domain.KnowledgeDocument, []byte, error) {
	d, _, err := u.kb.Document(ctx, id)
	if err != nil {
		return domain.KnowledgeDocument{}, nil, err
	}

	if d.Blob == "" {
		return domain.KnowledgeDocument{}, nil, fmt.Errorf("knowledge document %s: %w", id, domain.ErrNotUploadSource)
	}

	doc, err := u.Fetch(ctx, domain.SyncSpec{}, string(d.Blob))
	if err != nil {
		return domain.KnowledgeDocument{}, nil, err
	}

	return d, doc.Body, nil
}

// DeleteDocument deletes an uploaded document and, when no other document holds its content,
// its object.
func (u *Uploads) DeleteDocument(ctx context.Context, id domain.KnowledgeDocumentID, by domain.Actor) error {
	return u.kb.DeleteDocument(ctx, id, by, u.drop)
}

// DeleteSource deletes a source with its documents and the objects no other document holds.
func (u *Uploads) DeleteSource(ctx context.Context, name string, by domain.Actor) error {
	return u.kb.DeleteSource(ctx, name, by, u.drop)
}

// drop deletes the objects of contents no document holds any more (ports.DropBlobs): a failure
// undoes the deletion, which can be tried again.
func (u *Uploads) drop(ctx context.Context, hashes []domain.BlobHash) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	var errs []error

	for _, h := range hashes {
		if h == "" {
			continue
		}

		if err := u.objects.Delete(ctx, domain.KnowledgeKey(org, h)); err != nil {
			errs = append(errs, fmt.Errorf("delete upload %s: %w", h, err))
		}
	}

	return errors.Join(errs...)
}

// documentPage is how many documents List reads at once.
const documentPage = 500

// List implements ports.SourceReader: the upload source's documents with their content hashes as
// versions.
func (u *Uploads) List(ctx context.Context, s domain.SyncSpec) iter.Seq2[domain.SourceItem, error] {
	return func(yield func(domain.SourceItem, error) bool) {
		var after domain.KnowledgeDocumentID

		for {
			docs, err := u.kb.Documents(ctx, s.Source.Name, after, documentPage)
			if err != nil {
				yield(domain.SourceItem{}, err)

				return
			}

			for _, d := range docs {
				if !yield(domain.SourceItem{ID: d.ExternalID, Version: string(d.Blob), Title: d.Title, MIME: d.MIME}, nil) {
					return
				}
			}

			if len(docs) < documentPage {
				return
			}

			after = docs[len(docs)-1].ID
		}
	}
}

// Fetch implements ports.SourceReader: an upload's content from the bucket (its external id is
// its hash).
func (u *Uploads) Fetch(ctx context.Context, _ domain.SyncSpec, id string) (domain.SourceDoc, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.SourceDoc{}, domain.ErrNoOrg
	}

	h := domain.BlobHash(id)
	if !h.Valid() {
		return domain.SourceDoc{}, fmt.Errorf("upload %q is not a content hash: %w", id, domain.ErrInvalid)
	}

	obj, err := u.objects.Get(ctx, domain.KnowledgeKey(org, h))
	if err != nil {
		return domain.SourceDoc{}, err
	}

	if domain.HashBlob(obj.Body) != h {
		return domain.SourceDoc{}, fmt.Errorf("upload %s does not match its hash: %w", h, ports.ErrBlobCorrupt)
	}

	return domain.SourceDoc{SourceItem: domain.SourceItem{ID: id, Version: id}, Body: obj.Body}, nil
}
