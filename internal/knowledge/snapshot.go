package knowledge

import (
	"archive/zip"
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// kindReaders names the reader of each source kind that reads with a secret (ADR 0038 decision
// 2): a repository through gitdocs, Confluence through confluence.
var kindReaders = map[domain.KnowledgeSourceKind]string{
	domain.SourceRepository: "gitdocs",
	domain.SourceConfluence: "confluence",
}

// writesSnapshots reports whether a source of kind k syncs into snapshots: upload, repository and
// Confluence sources; synced and live ones keep their chunks until the cut-over (decision 16).
func writesSnapshots(k domain.KnowledgeSourceKind) bool {
	return k == domain.SourceUpload || k == domain.SourceRepository || k == domain.SourceConfluence
}

// byteLimits bound a source's Markdown, all its files' together and each file's:
// domain.MaxSourceBytes and domain.MaxFileBytes (tests lower them, export_test.go).
type byteLimits struct {
	source, file int64
}

var defaultLimits = byteLimits{source: domain.MaxSourceBytes, file: domain.MaxFileBytes}

// archiveFrom is how many changed items make a sync read them from its reader's archive, when the
// reader has one (ports.ArchiveReader), instead of fetching them one by one (ADR 0038 decision 2).
const archiveFrom = 50

type snapshotStats struct {
	// fetched counts the items read, archived those of them read from an archive.
	files, fetched, archived, failed int
	written                          bool
}

// snapshot is the snapshot pass of one sync (ADR 0038 decisions 3 and 4): it lists every item of
// spec's source, copies the entries whose version is unchanged from the current snapshot's zip
// without fetching or recompressing them, reads and converts the others, and when something
// was added, changed or removed writes one zip on local disk, stores it and records it. A file
// that fails keeps its last good entry or stays out, one that looks like it holds a key stays
// out, and both are listed; only a source past a limit, the bucket, the database or ctx fail the
// pass, and then nothing is stored.
func (in *Ingestor) snapshot(ctx context.Context, c domain.SourceClaim, spec domain.SyncSpec, reader ports.SourceReader) (snapshotStats, error) {
	var stats snapshotStats

	src := c.Source

	items, err := listItems(ctx, reader, spec)
	if err != nil {
		return stats, err
	}

	prev, err := in.currentFiles(ctx, src.Name)
	if err != nil {
		return stats, err
	}

	old, closeOld, err := in.previousZip(ctx, c)
	if err != nil {
		return stats, err
	}
	defer closeOld()

	out, done, err := in.tempFile("goal-snapshot-*.zip")
	if err != nil {
		return stats, err
	}
	defer done()

	b := &snapshotBuilder{zw: zip.NewWriter(out), limits: in.limits, prev: prev, old: old, paths: map[string]bool{}}

	var changed []changedItem

	for _, item := range items {
		p, read, err := in.entry(ctx, b, spec, item)
		if err != nil {
			return stats, err
		}

		if read {
			changed = append(changed, changedItem{item: item, path: p})
		}
	}

	if err := in.read(ctx, b, spec, reader, changed); err != nil {
		return stats, err
	}

	stats.files, stats.fetched, stats.archived, stats.failed = len(b.files), b.fetched, b.archived, b.failed

	if !b.changed(src.CurrentSnapshot == "") {
		return stats, nil
	}

	if err := b.zw.Close(); err != nil {
		return stats, fmt.Errorf("snapshot: write the zip: %w", err)
	}

	if err := in.store(ctx, c, b, out); err != nil {
		return stats, err
	}

	stats.written = true

	return stats, nil
}

// tempFile creates a temporary file in TempDir; done closes and removes it.
func (in *Ingestor) tempFile(pattern string) (*os.File, func(), error) {
	f, err := os.CreateTemp(in.cfg.TempDir, pattern)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot: %w", err)
	}

	return f, func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}, nil
}

// store puts the zip in out in the bucket and records its snapshot. A lease lost after the zip
// is stored leaves it unrecorded (ports.ErrLeaseLost).
func (in *Ingestor) store(ctx context.Context, c domain.SourceClaim, b *snapshotBuilder, out *os.File) error {
	size, err := out.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	id, err := newSnapshotID()
	if err != nil {
		return err
	}

	key := domain.KnowledgeSnapshotKey(c.Org, c.Source.ID, id)
	if _, err := in.cfg.Objects.PutStream(ctx, key, out); err != nil {
		return fmt.Errorf("snapshot: store %s: %w", key, err)
	}

	snap := domain.KnowledgeSnapshot{
		ID: id, Source: c.Source.ID, Files: len(b.files), Failed: b.failed, Bytes: b.bytes, ZipBytes: size, Changes: b.changes,
		ReadWhen: c.Source.ReadWhen, CreatedAt: in.cfg.Clock.Now(),
	}

	if _, err := in.cfg.Knowledge.PutSnapshot(ctx, c.Lease, snap, b.files); err != nil {
		return fmt.Errorf("snapshot: record: %w", err)
	}

	return nil
}

// listItems is every item reader lists of spec's source; more than domain.MaxSourceFiles fails
// before any is fetched.
func listItems(ctx context.Context, reader ports.SourceReader, spec domain.SyncSpec) ([]domain.SourceItem, error) {
	var items []domain.SourceItem

	for item, err := range reader.List(ctx, spec) {
		if err != nil {
			return nil, fmt.Errorf("list: %w", err)
		}

		if len(items) == domain.MaxSourceFiles {
			return nil, fmt.Errorf("source %s lists more than %d files, over the limit of %d files", spec.Source.Name, domain.MaxSourceFiles, domain.MaxSourceFiles)
		}

		items = append(items, item)
	}

	return items, nil
}

// currentFiles are the source's current files by path.
func (in *Ingestor) currentFiles(ctx context.Context, source string) (map[string]domain.KnowledgeFile, error) {
	out := map[string]domain.KnowledgeFile{}

	for after := ""; ; {
		page, err := in.cfg.Knowledge.Files(ctx, domain.KnowledgeFileQuery{Source: source, After: after, Limit: domain.MaxFilePage})
		if err != nil {
			return nil, fmt.Errorf("snapshot: current files: %w", err)
		}

		for _, f := range page {
			out[f.Path] = f
		}

		if len(page) < domain.MaxFilePage {
			return out, nil
		}

		after = page[len(page)-1].Path
	}
}

// previousZip is the current snapshot's zip, read from the bucket into a temporary file, by
// entry name; nil without one. A zip gone from the bucket or unreadable is logged and nil: every
// file is fetched again. The returned func removes the temporary file.
func (in *Ingestor) previousZip(ctx context.Context, c domain.SourceClaim) (map[string]*zip.File, func(), error) {
	none := func() {}

	if c.Source.CurrentSnapshot == "" {
		return nil, none, nil
	}

	key := domain.KnowledgeSnapshotKey(c.Org, c.Source.ID, c.Source.CurrentSnapshot)

	rc, err := in.cfg.Objects.GetRange(ctx, key, 0, -1)
	if errors.Is(err, ports.ErrNotFound) {
		in.cfg.Log.WarnContext(ctx, "knowledge snapshot missing", slog.String("source", c.Source.Name), slog.String("key", key))

		return nil, none, nil
	}

	if err != nil {
		return nil, none, fmt.Errorf("snapshot: read %s: %w", key, err)
	}

	defer func() { _ = rc.Close() }()

	f, cleanup, err := in.tempFile("goal-snapshot-prev-*.zip")
	if err != nil {
		return nil, none, err
	}

	n, err := io.Copy(f, rc)
	if err != nil {
		cleanup()

		return nil, none, fmt.Errorf("snapshot: read %s: %w", key, err)
	}

	zr, err := zip.NewReader(f, n)
	if err != nil {
		in.cfg.Log.WarnContext(ctx, "knowledge snapshot unreadable", slog.String("source", c.Source.Name), slog.String("key", key), slog.Any("error", err))
		cleanup()

		return nil, none, nil
	}

	entries := make(map[string]*zip.File, len(zr.File))
	for _, e := range zr.File {
		entries[e.Name] = e
	}

	return entries, cleanup, nil
}

// snapshotBuilder is a snapshot being written: its zip, its files, what changed and what failed
// against the current snapshot's files (prev) and zip entries (old).
type snapshotBuilder struct {
	zw       *zip.Writer
	limits   byteLimits
	prev     map[string]domain.KnowledgeFile
	old      map[string]*zip.File
	paths    map[string]bool
	files    []domain.KnowledgeFile
	bytes    int64
	changes  domain.SnapshotChanges
	failed   int
	fetched  int
	archived int
}

// changedItem is an item the snapshot reads, at its file's path.
type changedItem struct {
	item domain.SourceItem
	path string
}

// entry gives item its file's path p in the snapshot and copies its entry when its version is
// unchanged; read reports that it changed and is to be read. An item whose path is not a file's
// is left out and counted as failed.
func (in *Ingestor) entry(ctx context.Context, b *snapshotBuilder, spec domain.SyncSpec, item domain.SourceItem) (p string, read bool, err error) {
	p = b.path(spec.Source.Kind, item)
	if err := domain.CheckFilePath(p); err != nil {
		in.cfg.Log.WarnContext(ctx, "knowledge file left out: its path is not a file's", slog.String("source", spec.Source.Name), slog.Any("error", err))

		b.failed++

		return "", false, nil
	}

	b.paths[p] = true

	if f, ok := b.prev[p]; ok && f.Version == item.Version {
		copied, err := b.copy(p)
		if err != nil || copied {
			return p, false, err
		}
	}

	return p, true, nil
}

// read adds the changed items to the snapshot: from one archive when reader has one and at least
// archiveFrom changed, then those the archive did not hand over fetched one by one.
func (in *Ingestor) read(ctx context.Context, b *snapshotBuilder, spec domain.SyncSpec, reader ports.SourceReader, changed []changedItem) error {
	if a, ok := reader.(ports.ArchiveReader); ok && len(changed) >= archiveFrom {
		left, err := in.fromArchive(ctx, b, spec, a, changed)
		if err != nil {
			return err
		}

		changed = left
	}

	for _, c := range changed {
		doc, err := reader.Fetch(ctx, spec, c.item.ID)
		if err != nil {
			err = fmt.Errorf("%w: %w", errFetch, err)
		}

		if err := in.put(ctx, b, c, doc, err); err != nil {
			return err
		}
	}

	return nil
}

// fromArchive adds the changed items a's archive hands over, one at a time, and returns the
// others. An archive that fails as a whole is logged, and leaves the items it did not hand over
// to be fetched one by one.
func (in *Ingestor) fromArchive(ctx context.Context, b *snapshotBuilder, spec domain.SyncSpec, a ports.ArchiveReader, changed []changedItem,
) ([]changedItem, error) {
	at := make(map[string]int, len(changed))
	items := make([]domain.SourceItem, len(changed))

	for i, c := range changed {
		at[c.item.ID], items[i] = i, c.item
	}

	done := make([]bool, len(changed))

	for doc, err := range a.Archive(ctx, spec, items) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if err != nil {
			in.cfg.Log.WarnContext(ctx, "knowledge archive failed", slog.String("source", spec.Source.Name), slog.Any("error", err))

			break
		}

		i, ok := at[doc.ID]
		if !ok || done[i] {
			continue
		}

		done[i] = true
		b.archived++

		if err := in.put(ctx, b, changed[i], doc, nil); err != nil {
			return nil, err
		}
	}

	var left []changedItem

	for i, c := range changed {
		if !done[i] {
			left = append(left, c)
		}
	}

	return left, nil
}

// put converts c's content doc and adds it to the snapshot; when its read failed (readErr) or its
// conversion fails, it keeps the entry it had, or leaves it out, and is listed.
func (in *Ingestor) put(ctx context.Context, b *snapshotBuilder, c changedItem, doc domain.SourceDoc, readErr error) error {
	b.fetched++

	md, title, url, err := "", "", "", readErr
	if err == nil {
		md, title, url, err = in.markdown(ctx, c.item, doc, c.path)
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err != nil {
		return b.keep(c.path, err)
	}

	return b.add(c.path, c.item.Version, md, title, url)
}

// keep lists p as failed with err and keeps its entry from the current snapshot, or leaves it
// out without one.
func (b *snapshotBuilder) keep(p string, err error) error {
	b.fail(domain.FileFailure{Path: p, Error: err.Error()})

	kept, err := b.copy(p)
	if !kept {
		delete(b.paths, p)
	}

	return err
}

// add writes p's Markdown md, with its source line, as a new or changed file: past the limit of
// a file it fails the snapshot, and when it looks like it holds a key it stays out, flagged.
func (b *snapshotBuilder) add(p, version, md, title, url string) error {
	body := withSource(cmp.Or(url, p), md)
	if int64(len(body)) > b.limits.file {
		return fmt.Errorf("file %s has %d bytes of Markdown, over the limit of %d MB per file", p, len(body), b.limits.file>>20)
	}

	if domain.LooksLikeKey(md) {
		b.fail(domain.FileFailure{Path: p, Error: "it looks like it holds a key", Key: true})
		delete(b.paths, p)

		return nil
	}

	if version == "" || utf8.RuneCountInString(version) > domain.MaxFileVersion {
		version = string(domain.HashBlob([]byte(body)))
	}

	f := domain.KnowledgeFile{Path: p, Title: clip(cmp.Or(title, path.Base(p)), domain.MaxFileTitle), Bytes: int64(len(body)), Version: version}
	if err := b.write(f, body); err != nil {
		return err
	}

	if _, ok := b.prev[p]; ok {
		b.changes.Changed++
	} else {
		b.changes.Added++
	}

	return nil
}

// changed counts the current files the snapshot no longer has as removed and reports whether it
// is worth writing: something was added, changed or removed, or first, the source's first, has
// failed files to list.
func (b *snapshotBuilder) changed(first bool) bool {
	for p := range b.prev {
		if !b.paths[p] {
			b.changes.Removed++
		}
	}

	return b.changes.Added+b.changes.Changed+b.changes.Removed > 0 || (first && b.failed > 0)
}

// path is item's file path in the snapshot: an upload's file name, a repository file's path, a
// Confluence page's title and id, as Markdown (.md); another item at the same path gets its
// id's short hash before the extension.
func (b *snapshotBuilder) path(kind domain.KnowledgeSourceKind, item domain.SourceItem) string {
	var name string

	switch kind {
	case domain.SourceUpload:
		name = item.ID
		if domain.CheckFilePath(item.Title) == nil {
			name = item.Title
		}
	case domain.SourceConfluence:
		name = slug(item.Title) + "-" + item.ID
	case domain.SourceRepository, domain.SourceSynced, domain.SourceLive:
		name = item.ID
	}

	p := markdownName(name)
	if !b.paths[p] {
		return p
	}

	h := sha256.Sum256([]byte(item.ID))
	ext := path.Ext(p)

	return strings.TrimSuffix(p, ext) + "-" + hex.EncodeToString(h[:4]) + ext
}

// copy copies p's entry from the current snapshot's zip as it was stored, with its file, and
// reports whether there was one.
func (b *snapshotBuilder) copy(p string) (bool, error) {
	f, ok := b.prev[p]
	e := b.old[p]

	if !ok || e == nil {
		return false, nil
	}

	size := e.FileInfo().Size()
	if err := b.count(p, size); err != nil {
		return false, err
	}

	raw, err := e.OpenRaw()
	if err != nil {
		return false, fmt.Errorf("snapshot: copy %s: %w", p, err)
	}

	h := e.FileHeader

	w, err := b.zw.CreateRaw(&h)
	if err != nil {
		return false, fmt.Errorf("snapshot: copy %s: %w", p, err)
	}

	if _, err := io.Copy(w, raw); err != nil {
		return false, fmt.Errorf("snapshot: copy %s: %w", p, err)
	}

	f.Bytes = size
	b.files = append(b.files, f)

	return true, nil
}

// write adds f with body to the zip, compressed.
func (b *snapshotBuilder) write(f domain.KnowledgeFile, body string) error {
	if err := b.count(f.Path, f.Bytes); err != nil {
		return err
	}

	w, err := b.zw.CreateHeader(&zip.FileHeader{Name: f.Path, Method: zip.Deflate})
	if err != nil {
		return fmt.Errorf("snapshot: write %s: %w", f.Path, err)
	}

	if _, err := io.WriteString(w, body); err != nil {
		return fmt.Errorf("snapshot: write %s: %w", f.Path, err)
	}

	b.files = append(b.files, f)

	return nil
}

// count adds n bytes of Markdown to the snapshot's, failing past domain.MaxSourceBytes.
func (b *snapshotBuilder) count(p string, n int64) error {
	b.bytes += n
	if b.bytes > b.limits.source {
		return fmt.Errorf("the source's files reach %d bytes at %s, over the limit of %d MB of Markdown", b.bytes, p, b.limits.source>>20)
	}

	return nil
}

// fail lists f among the snapshot's failed files, the first domain.MaxSnapshotFailures of them.
func (b *snapshotBuilder) fail(f domain.FileFailure) {
	b.failed++

	if len(b.changes.Failed) < domain.MaxSnapshotFailures {
		f.Error = clip(f.Error, domain.MaxKnowledgeError)
		b.changes.Failed = append(b.changes.Failed, f)
	}
}

// markdown converts item's content doc to Markdown, returning its title and URL as the read tells
// them. name is its file's path, which names its type when neither the listing nor the read does.
func (in *Ingestor) markdown(ctx context.Context, item domain.SourceItem, doc domain.SourceDoc, name string) (md, title, url string, err error) {
	mediaType := cmp.Or(doc.MIME, item.MIME)
	if mediaType == "" {
		if mediaType, err = domain.DocumentType(cmp.Or(item.Title, item.ID), doc.Body); err != nil {
			return "", "", "", err
		}
	}

	out, err := converter{next: in.cfg.Converter}.Convert(ctx, path.Base(name), mediaType, doc.Body)
	if err != nil {
		return "", "", "", err
	}

	return string(out), cmp.Or(doc.Title, item.Title), cmp.Or(doc.URL, item.URL), nil
}

// withSource starts md with its citation, the frontmatter line source: <url> (ADR 0038 decision
// 2), in md's own frontmatter when it has one.
func withSource(url, md string) string {
	line := "source: " + strings.NewReplacer("\r", "", "\n", "").Replace(url) + "\n"

	if rest, ok := strings.CutPrefix(md, "---\n"); ok {
		return "---\n" + line + rest
	}

	return "---\n" + line + "---\n\n" + md
}

// markdownName is p as a Markdown file's path: .md and .markdown stay, .html and .htm become .md,
// anything else gets .md after it.
func markdownName(p string) string {
	ext := path.Ext(p)

	switch strings.ToLower(ext) {
	case ".md", ".markdown":
		return p
	case ".html", ".htm":
		return strings.TrimSuffix(p, ext) + ".md"
	}

	return p + ".md"
}

// slug is a page title as a file name: lower-case letters and digits, the rest dashes; "page"
// for none.
func slug(title string) string {
	var sb strings.Builder

	dash := false

	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)

			dash = false
		} else if !dash && sb.Len() > 0 {
			sb.WriteByte('-')

			dash = true
		}
	}

	s := strings.TrimSuffix(sb.String(), "-")
	if s == "" {
		return "page"
	}

	return clip(s, 200)
}

// clip is s cut to at most n characters.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}

	return string([]rune(s)[:n])
}

// newSnapshotID is a random UUID (version 4).
func newSnapshotID() (domain.KnowledgeSnapshotID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("snapshot: id: %w", err)
	}

	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])

	return domain.KnowledgeSnapshotID(h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]), nil
}
