package gitdocs

import (
	"archive/zip"
	"context"
	"crypto/sha1" //nolint:gosec // git's blob ids are SHA-1; they check an archived file against its listing, not a secret
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.ArchiveReader = (*Reader)(nil)

// maxArchiveBytes bounds a repository's archive: four times a source's Markdown
// (domain.MaxSourceBytes), since an archive holds the code and images beside the docs too.
const maxArchiveBytes = 4 * domain.MaxSourceBytes

// Archive implements ports.ArchiveReader: the source's branch's archive, downloaded once to a
// temporary file, yields each of items that was listed, that the source's globs want and whose
// archived content has the git blob id listed. A file changed since the listing, a symbolic link
// and a file over an upload's size are not yielded, so the caller fetches them. An archive over
// maxArchiveBytes, or with an entry whose path escapes its root, fails as a whole.
func (r *Reader) Archive(ctx context.Context, s domain.SyncSpec, items []domain.SourceItem) iter.Seq2[domain.SourceDoc, error] {
	return func(yield func(domain.SourceDoc, error) bool) {
		src, err := open(s)
		if err != nil {
			yield(domain.SourceDoc{}, err)

			return
		}

		want := make(map[string]string, len(items))

		for _, item := range items {
			if src.wants(item.ID) {
				want[item.ID] = item.Version
			}
		}

		if len(want) == 0 {
			return
		}

		f, size, err := r.download(ctx, src)
		if err != nil {
			yield(domain.SourceDoc{}, fmt.Errorf("%s's archive: %w", src.repo, err))

			return
		}

		defer func() {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}()

		zr, err := zip.NewReader(f, size)
		if err != nil {
			yield(domain.SourceDoc{}, fmt.Errorf("%s's archive: %w", src.repo, err))

			return
		}

		// Every entry is checked before any is yielded, so a refused archive yields nothing.
		for _, e := range zr.File {
			if escapes(e.Name) {
				yield(domain.SourceDoc{}, fmt.Errorf("%s's archive: entry %q escapes the archive's root: %w", src.repo, e.Name, domain.ErrInvalid))

				return
			}
		}

		for _, e := range zr.File {
			doc, ok, err := entry(e, want)
			if err != nil {
				yield(domain.SourceDoc{}, fmt.Errorf("%s's archive: %w", src.repo, err))

				return
			}

			if ok {
				delete(want, doc.ID)

				if !yield(doc, nil) {
					return
				}
			}
		}
	}
}

// entry is the document in e when e is a regular file at a path in want whose content has the
// blob id want names for it; ok is false otherwise.
func entry(e *zip.File, want map[string]string) (doc domain.SourceDoc, ok bool, err error) {
	// The archive's top folder is the repository at its commit; a file is under it.
	_, p, under := strings.Cut(e.Name, "/")
	version, listed := want[p]

	if !under || !listed || !e.Mode().IsRegular() || e.UncompressedSize64 > domain.DefaultMaxUpload {
		return domain.SourceDoc{}, false, nil
	}

	rc, err := e.Open()
	if err != nil {
		return domain.SourceDoc{}, false, fmt.Errorf("open %s: %w", p, err)
	}
	defer func() { _ = rc.Close() }()

	// The header's size is the archive's word; the read is bounded whatever it says.
	body, err := io.ReadAll(io.LimitReader(rc, domain.DefaultMaxUpload+1))
	if err != nil {
		return domain.SourceDoc{}, false, fmt.Errorf("read %s: %w", p, err)
	}

	if len(body) > domain.DefaultMaxUpload || blobID(body) != version {
		return domain.SourceDoc{}, false, nil
	}

	// A type it cannot index stays "", which extraction refuses with the reason.
	mime, _ := domain.DocumentType(p, body)

	return domain.SourceDoc{SourceItem: domain.SourceItem{ID: p, MIME: mime}, Body: body}, true, nil
}

// escapes reports whether an archive entry's name leaves the archive's root: absolute, or with a
// .. segment.
func escapes(name string) bool {
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return true
	}

	for seg := range strings.SplitSeq(strings.ReplaceAll(name, `\`, "/"), "/") {
		if seg == ".." {
			return true
		}
	}

	return false
}

// blobID is git's id of a blob with content b.
func blobID(b []byte) string {
	h := sha1.New() //nolint:gosec // git's blob ids are SHA-1; see the import
	_, _ = io.WriteString(h, "blob "+strconv.Itoa(len(b))+"\x00")
	_, _ = h.Write(b)

	return hex.EncodeToString(h.Sum(nil))
}

// download streams the source's branch's archive to a temporary file and returns it with its
// size; past maxArchiveBytes it fails with ports.ErrTooLarge. A redirect, as GitHub's to its
// download host, is followed once, over https and without the token.
func (r *Reader) download(ctx context.Context, src source) (*os.File, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.host.archive(src), nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("User-Agent", "goald")
	src.host.authorize(req.Header, src.token)

	res, err := r.http.Do(req)
	if err != nil {
		return nil, 0, err
	}

	if isRedirect(res.StatusCode) {
		res, err = r.redirect(ctx, res)
		if err != nil {
			return nil, 0, err
		}
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return nil, 0, refusal(res, src)
	}

	tooLarge := fmt.Errorf("%s's archive is larger than %d bytes: %w", src.host.name(), r.maxArchive, ports.ErrTooLarge)

	if res.ContentLength > r.maxArchive {
		return nil, 0, tooLarge
	}

	f, err := os.CreateTemp("", "goal-gitdocs-*.zip")
	if err != nil {
		return nil, 0, err
	}

	n, err := io.Copy(f, io.LimitReader(res.Body, r.maxArchive+1))

	switch {
	case err != nil:
		err = fmt.Errorf("read %s's archive: %w", src.host.name(), err)
	case n > r.maxArchive:
		err = tooLarge
	}

	if err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())

		return nil, 0, err
	}

	return f, n, nil
}

// redirect follows res, a redirect, once: to an https URL, with no credential, through the same
// transport. res's body is closed.
func (r *Reader) redirect(ctx context.Context, res *http.Response) (*http.Response, error) {
	_ = res.Body.Close()

	to, err := res.Request.URL.Parse(res.Header.Get("Location"))
	if err != nil || to.Scheme != "https" || to.Host == "" {
		return nil, errors.New("the archive redirects to a URL that is not https")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, to.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "goald")

	next, err := r.http.Do(req)
	if err != nil {
		// The signed URL's query is a credential of its own, so the error leaves the URL out.
		if uerr, ok := errors.AsType[*url.Error](err); ok {
			err = uerr.Err
		}

		return nil, fmt.Errorf("read the archive at %s: %w", to.Host, err)
	}

	if isRedirect(next.StatusCode) {
		_ = next.Body.Close()

		return nil, errors.New("the archive redirects more than once")
	}

	return next, nil
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}

	return false
}
