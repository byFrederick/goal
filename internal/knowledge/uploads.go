package knowledge

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.SourceReader = (*Uploads)(nil)

// Uploads keeps uploaded files: their originals in the bucket once per organization and content
// (domain.KnowledgeKey, written with PutIfAbsent, so an object is never rewritten), their names
// and content hashes in the knowledge base. An object goes when the last upload holding its
// content does, inside that deletion's transaction (ports.DropBlobs), and an upload writes its
// object only once its row holds the content, so the two never cross. It is also the source
// reader of upload sources: the next sync turns the files into a snapshot.
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
// ctx, and returns it (the same file when that content is there under that name already). A file
// of the same name is replaced. ports.ErrTooLarge over the size limit, domain.ErrUnsupportedDocument
// for a type the knowledge base does not take, and the Knowledge.AddUpload refusals.
func (u *Uploads) Upload(ctx context.Context, source, name string, body []byte, by domain.Actor) (domain.KnowledgeUpload, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.KnowledgeUpload{}, domain.ErrNoOrg
	}

	mime, err := u.check(name, body)
	if err != nil {
		return domain.KnowledgeUpload{}, err
	}

	// The row first: it holds the content against a deletion of another holder, and a refusal
	// leaves no object behind. A sync between the two finds no object, fails that file and tries
	// again once the source is due.
	h := domain.HashBlob(body)

	f, err := u.kb.AddUpload(ctx, source, domain.KnowledgeUpload{Name: name, MIME: mime, Bytes: int64(len(body)), Blob: h}, by, u.drop)
	if err != nil {
		return domain.KnowledgeUpload{}, err
	}

	if _, _, err := u.objects.PutIfAbsent(ctx, domain.KnowledgeKey(org, h), body); err != nil {
		return domain.KnowledgeUpload{}, fmt.Errorf("upload %q: store: %w", name, err)
	}

	if err := u.kb.SyncNow(ctx, source); err != nil {
		return domain.KnowledgeUpload{}, err
	}

	return f, nil
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

// DeleteUpload deletes the file named name of an upload source and, when no other upload holds
// its content, its object.
func (u *Uploads) DeleteUpload(ctx context.Context, source, name string, by domain.Actor) error {
	return u.kb.DeleteUpload(ctx, source, name, by, u.drop)
}

// DeleteSource deletes a source with its uploads and the objects no other upload holds. Its
// snapshots' zips (domain.KnowledgeSourcePrefix) go first: a failure leaves the source, whose
// rows without zips read as not synced, to be deleted again.
func (u *Uploads) DeleteSource(ctx context.Context, name string, by domain.Actor) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	src, err := u.kb.Source(ctx, name)
	if err != nil {
		return err
	}

	if _, err := u.objects.DeletePrefix(ctx, domain.KnowledgeSourcePrefix(org, src.ID)); err != nil {
		return fmt.Errorf("delete knowledge source %q: its snapshots: %w", name, err)
	}

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

// List implements ports.SourceReader: the upload source's files, each with its content hash as
// its id and version and its name as its title.
func (u *Uploads) List(ctx context.Context, s domain.SyncSpec) iter.Seq2[domain.SourceItem, error] {
	return func(yield func(domain.SourceItem, error) bool) {
		files, err := u.kb.Uploads(ctx, s.Source.Name)
		if err != nil {
			yield(domain.SourceItem{}, err)

			return
		}

		for _, f := range files {
			if !yield(domain.SourceItem{ID: string(f.Blob), Version: string(f.Blob), Title: f.Name, MIME: f.MIME}, nil) {
				return
			}
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
