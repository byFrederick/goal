package api

import (
	"archive/zip"
	"bytes"
	"cmp"
	"compress/flate"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"slices"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// A source's files (ADR 0038 decisions 3 and 13): its current snapshot's files, one file's
// Markdown read by range from the snapshot's zip, an upload source's uploads, a file found across
// every source, and the daily rollup of runs' reads.

// KnowledgeObjects reads a knowledge snapshot's zip from the bucket by range; ports.ObjectStore
// satisfies it.
type KnowledgeObjects interface {
	GetRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error)
}

// Limits of the files routes.
const (
	defaultFiles     = 100
	defaultFound     = 50
	maxFound         = 200
	defaultReadsDays = 7
	maxReadsDays     = 90
)

// errFileNotFound is a path the source's current snapshot does not hold (404).
var errFileNotFound = errors.New("knowledge file not found")

func (s *server) ListKnowledgeFiles(ctx context.Context, req gen.ListKnowledgeFilesRequestObject) (gen.ListKnowledgeFilesResponseObject, error) {
	limit := defaultFiles
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

	if limit < 1 || limit > domain.MaxFilePage {
		return nil, fmt.Errorf("%w: limit must be in [1, %d]", errInvalid, domain.MaxFilePage)
	}

	// One more than the page says whether there is a next one.
	files, err := s.knowledge.Files(ctx, domain.KnowledgeFileQuery{
		Source: req.Source, Prefix: deref(req.Params.Find), After: deref(req.Params.After), Limit: min(limit+1, domain.MaxFilePage),
	})
	if err != nil {
		return nil, sourceError(err, req.Source)
	}

	out := gen.ListKnowledgeFiles200JSONResponse{Files: make([]gen.KnowledgeFile, 0, min(len(files), limit))}

	if len(files) > limit {
		files = files[:limit]
		out.NextAfter = optional(files[limit-1].Path)
	}

	for _, f := range files {
		out.Files = append(out.Files, fileOut(req.Source, f))
	}

	return out, nil
}

// GetKnowledgeFile answers one file of the source's current snapshot, read from its zip in four
// ranges: the end record, the central directory, the entry's local header and the entry.
func (s *server) GetKnowledgeFile(ctx context.Context, req gen.GetKnowledgeFileRequestObject) (gen.GetKnowledgeFileResponseObject, error) {
	if err := domain.CheckFilePath(req.Path); err != nil {
		return nil, fmt.Errorf("%w: %q: %w", errFileNotFound, req.Path, err)
	}

	snap, err := s.currentSnapshot(ctx, req.Source)
	if err != nil {
		return nil, err
	}

	if snap == nil {
		return nil, fmt.Errorf("%w: knowledge source %q has no snapshot yet", errFileNotFound, req.Source)
	}

	body, err := zipEntry(ctx, s.objects, snap.Key, snap.ZipBytes, req.Path)
	if err != nil {
		return nil, err
	}

	return gen.GetKnowledgeFile200TextmarkdownResponse{Body: bytes.NewReader(body), ContentLength: int64(len(body))}, nil
}

// currentSnapshot is the source's current snapshot; nil before its first.
func (s *server) currentSnapshot(ctx context.Context, source string) (*domain.KnowledgeSnapshot, error) {
	src, err := s.knowledge.Source(ctx, source)
	if err != nil {
		return nil, sourceError(err, source)
	}

	if src.CurrentSnapshot == "" {
		return nil, nil
	}

	snaps, err := s.knowledge.Snapshots(ctx, source)
	if err != nil {
		return nil, sourceError(err, source)
	}

	i := slices.IndexFunc(snaps, func(sn domain.KnowledgeSnapshot) bool { return sn.ID == src.CurrentSnapshot })
	if i < 0 {
		return nil, fmt.Errorf("knowledge source %q: its current snapshot %s is not among its snapshots", source, src.CurrentSnapshot)
	}

	return &snaps[i], nil
}

// UploadKnowledgeFiles stores the request's files in an upload source, each at the path its file
// name gives, once every one of them passes.
func (s *server) UploadKnowledgeFiles(ctx context.Context, req gen.UploadKnowledgeFilesRequestObject) (gen.UploadKnowledgeFilesResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("upload knowledge files: the request has no actor")
	}

	files, err := s.uploadedPaths(req.Body)
	if err != nil {
		return nil, err
	}

	if err := s.uploadSource(ctx, req.Source); err != nil {
		return nil, err
	}

	for _, f := range files {
		if err := s.uploads.Check(f.name, f.body); err != nil {
			return nil, uploadError(err)
		}
	}

	out := gen.UploadKnowledgeFiles202JSONResponse{Files: make([]struct {
		Bytes int64  `json:"bytes"`
		Path  string `json:"path"`
	}, 0, len(files))}

	for _, f := range files {
		if _, err := s.uploads.Upload(ctx, req.Source, f.name, f.body, by); err != nil {
			return nil, uploadError(sourceError(err, req.Source))
		}

		out.Files = append(out.Files, struct {
			Bytes int64  `json:"bytes"`
			Path  string `json:"path"`
		}{Bytes: int64(len(f.body)), Path: f.name})
	}

	return out, nil
}

// uploadSource refuses a source that is not an upload source.
func (s *server) uploadSource(ctx context.Context, name string) error {
	src, err := s.knowledge.Source(ctx, name)
	if err != nil {
		return sourceError(err, name)
	}

	if src.Kind != domain.SourceUpload {
		return fmt.Errorf("knowledge source %q is %s: %w", name, src.Kind, domain.ErrNotUploadSource)
	}

	return nil
}

// uploadedPaths are the files of an upload request at the paths their file names give, folders
// and all: multipart's FileName keeps only the last element, so the parameter is read as sent.
func (s *server) uploadedPaths(r *multipart.Reader) ([]uploadedFile, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: the body is multipart/form-data with files", errInvalid)
	}

	var files []uploadedFile

	for {
		part, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, bodyError(err)
		}

		if part.FormName() != "files" {
			continue
		}

		_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		name := params["filename"]

		if err != nil || name == "" {
			return nil, fmt.Errorf("%w: every file has a file name", errInvalid)
		}

		// The rule's message only: domain.ErrInvalid is a 409 elsewhere, and a bad path is the request's.
		if err := domain.CheckFilePath(name); err != nil {
			return nil, fmt.Errorf("%w: %s", errInvalid, err.Error())
		}

		body, err := io.ReadAll(io.LimitReader(part, s.uploadMax+1))
		if err != nil {
			return nil, bodyError(err)
		}

		if int64(len(body)) > s.uploadMax {
			return nil, fmt.Errorf("%w: %q has more than %d bytes", errUploadTooLarge, name, s.uploadMax)
		}

		files = append(files, uploadedFile{name: name, body: body})
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("%w: no files to upload", errInvalid)
	}

	return files, nil
}

// DeleteKnowledgeUpload deletes the upload at a path and makes the source due, so its next
// snapshot leaves the file out.
func (s *server) DeleteKnowledgeUpload(ctx context.Context, req gen.DeleteKnowledgeUploadRequestObject) (gen.DeleteKnowledgeUploadResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("delete knowledge upload: the request has no actor")
	}

	if err := s.uploadSource(ctx, req.Source); err != nil {
		return nil, err
	}

	id, err := s.uploadAt(ctx, req.Source, req.Params.Path)
	if err != nil {
		return nil, err
	}

	if err := s.uploads.DeleteDocument(ctx, id, by); err != nil {
		return nil, documentError(err, string(id))
	}

	if err := s.knowledge.SyncNow(ctx, req.Source); err != nil {
		return nil, sourceError(err, req.Source)
	}

	return gen.DeleteKnowledgeUpload204Response{}, nil
}

// uploadAt is the id of the upload source's upload at path (its title); errFileNotFound for none.
func (s *server) uploadAt(ctx context.Context, source, path string) (domain.KnowledgeDocumentID, error) {
	var after domain.KnowledgeDocumentID

	for {
		docs, err := s.knowledge.Documents(ctx, source, after, maxDocuments)
		if err != nil {
			return "", sourceError(err, source)
		}

		for _, d := range docs {
			if d.Title == path {
				return d.ID, nil
			}
		}

		if len(docs) < maxDocuments {
			return "", fmt.Errorf("%w: knowledge source %q has no upload at %q", errFileNotFound, source, path)
		}

		after = docs[len(docs)-1].ID
	}
}

// FindKnowledgeFiles finds the current files whose path or title starts with find, in every
// source by name.
func (s *server) FindKnowledgeFiles(ctx context.Context, req gen.FindKnowledgeFilesRequestObject) (gen.FindKnowledgeFilesResponseObject, error) {
	limit := defaultFound
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

	if limit < 1 || limit > maxFound || req.Params.Find == "" {
		return nil, fmt.Errorf("%w: find is set and limit is in [1, %d]", errInvalid, maxFound)
	}

	sources, err := s.knowledge.Sources(ctx)
	if err != nil {
		return nil, err
	}

	out := gen.FindKnowledgeFiles200JSONResponse{Files: []gen.KnowledgeFile{}}

	for _, src := range sources {
		if len(out.Files) == limit {
			break
		}

		files, err := s.knowledge.Files(ctx, domain.KnowledgeFileQuery{Source: src.Name, Prefix: req.Params.Find, Limit: limit - len(out.Files)})
		if err != nil {
			return nil, sourceError(err, src.Name)
		}

		for _, f := range files {
			out.Files = append(out.Files, fileOut(src.Name, f))
		}
	}

	return out, nil
}

// ListKnowledgeReads answers the reads rollup of one source or every one over the last days days,
// counting today, without the factories the caller may not see.
func (s *server) ListKnowledgeReads(ctx context.Context, req gen.ListKnowledgeReadsRequestObject) (gen.ListKnowledgeReadsResponseObject, error) {
	days := cmp.Or(deref(req.Params.Days), defaultReadsDays)
	if days < 1 || days > maxReadsDays {
		return nil, fmt.Errorf("%w: days must be in [1, %d]", errInvalid, maxReadsDays)
	}

	names := []string{deref(req.Params.Source)}
	if names[0] == "" {
		sources, err := s.knowledge.Sources(ctx)
		if err != nil {
			return nil, err
		}

		names = names[:0]
		for _, src := range sources {
			names = append(names, src.Name)
		}
	}

	factories := map[domain.FactoryID]string{}
	for _, f := range s.factories.Factories(ctx) {
		factories[domain.FactoryID(f.Metadata.ID)] = f.Metadata.Name
	}

	since := domain.KnowledgeDay(s.clock.Now()).AddDate(0, 0, 1-days)
	out := gen.ListKnowledgeReads200JSONResponse{Reads: []gen.KnowledgeReadCount{}}

	for _, name := range names {
		reads, err := s.knowledge.FileReads(ctx, name, since)
		if err != nil {
			return nil, sourceError(err, name)
		}

		for _, r := range reads {
			factory, ok := factories[r.Factory]
			if !ok || !s.visible(ctx, r.Factory) {
				continue
			}

			out.Reads = append(out.Reads, gen.KnowledgeReadCount{
				Source: name, Path: r.Path, FactoryId: string(r.Factory), Factory: factory, Day: openapi_types.Date{Time: r.Day},
				Reads: r.Reads, Searches: r.Searches,
			})
		}
	}

	slices.SortStableFunc(out.Reads, func(a, b gen.KnowledgeReadCount) int { return a.Day.Compare(b.Day.Time) })

	return out, nil
}

// fileOut is a file of the source named source as the contract has it.
func fileOut(source string, f domain.KnowledgeFile) gen.KnowledgeFile {
	return gen.KnowledgeFile{Source: source, Path: f.Path, Title: f.Title, Bytes: f.Bytes, Version: f.Version}
}

// Sizes of a zip's records: the end of central directory record's fixed part, the longest
// comment, and the fields of it read; a local file header's fixed part.
const (
	eocdSize        = 22
	maxZipComment   = 1<<16 - 1
	eocdSignature   = 0x06054b50
	zip64Marker     = 0xffffffff
	eocdDirSize     = 12
	eocdDirOffset   = 16
	maxZipDirBytes  = 16 << 20
	localHeaderSize = 30
)

// zipEntry is the file at name of the zip at key, size bytes long, read by range: its end of
// central directory record, its central directory, then the entry's local header and its data. It
// never reads the whole zip, and refuses an entry over domain.MaxFileBytes. errFileNotFound when
// the zip holds no such file. It follows internal/gateway's zipPaths, which api may not import.
func zipEntry(ctx context.Context, objects KnowledgeObjects, key string, size int64, name string) ([]byte, error) {
	tailLen := min(size, eocdSize+maxZipComment)

	tail, err := readRange(ctx, objects, key, size-tailLen, tailLen)
	if err != nil {
		return nil, err
	}

	at := bytes.LastIndex(tail, binary.LittleEndian.AppendUint32(nil, eocdSignature))
	if at < 0 || len(tail)-at < eocdSize {
		return nil, fmt.Errorf("zip %s: no end of central directory record", key)
	}

	dirSize := int64(binary.LittleEndian.Uint32(tail[at+eocdDirSize:]))
	dirOffset := int64(binary.LittleEndian.Uint32(tail[at+eocdDirOffset:]))

	if dirOffset == zip64Marker || dirSize > maxZipDirBytes || dirOffset+dirSize > size {
		return nil, fmt.Errorf("zip %s: a central directory of %d bytes at %d does not fit a %d-byte zip", key, dirSize, dirOffset, size)
	}

	dir, err := readRange(ctx, objects, key, dirOffset, size-dirOffset)
	if err != nil {
		return nil, err
	}

	zr, err := zip.NewReader(zipTail{data: dir, base: dirOffset}, size)
	if err != nil {
		return nil, fmt.Errorf("zip %s: read the central directory: %w", key, err)
	}

	i := slices.IndexFunc(zr.File, func(f *zip.File) bool { return f.Name == name })
	if i < 0 {
		return nil, fmt.Errorf("%w: %q", errFileNotFound, name)
	}

	f := zr.File[i]
	if f.UncompressedSize64 > domain.MaxFileBytes || f.CompressedSize64 > domain.MaxFileBytes+domain.MaxFileBytes/2 {
		return nil, fmt.Errorf("zip %s: %s has %d bytes, over the limit of %d", key, name, f.UncompressedSize64, domain.MaxFileBytes)
	}

	return entryData(ctx, objects, key, f, entryOffset(dir, f))
}

// entryOffset is where f's local header starts: the central directory record's offset field,
// which archive/zip keeps unexported. It finds f's record in dir by its name.
func entryOffset(dir []byte, f *zip.File) int64 {
	const (
		recordSignature = 0x02014b50
		recordSize      = 46
	)

	for p := 0; p+recordSize <= len(dir); {
		if binary.LittleEndian.Uint32(dir[p:]) != recordSignature {
			return -1
		}

		nameLen := int(binary.LittleEndian.Uint16(dir[p+28:]))
		extraLen := int(binary.LittleEndian.Uint16(dir[p+30:]))
		commentLen := int(binary.LittleEndian.Uint16(dir[p+32:]))

		if end := p + recordSize + nameLen; end <= len(dir) && string(dir[p+recordSize:end]) == f.Name {
			return int64(binary.LittleEndian.Uint32(dir[p+42:]))
		}

		p += recordSize + nameLen + extraLen + commentLen
	}

	return -1
}

// entryData reads f's local header at offset, then its data in one range, and inflates it.
func entryData(ctx context.Context, objects KnowledgeObjects, key string, f *zip.File, offset int64) ([]byte, error) {
	if offset < 0 {
		return nil, fmt.Errorf("zip %s: no central directory record of %s", key, f.Name)
	}

	header, err := readRange(ctx, objects, key, offset, localHeaderSize)
	if err != nil {
		return nil, err
	}

	if len(header) < localHeaderSize || binary.LittleEndian.Uint32(header) != 0x04034b50 {
		return nil, fmt.Errorf("zip %s: no local header of %s at %d", key, f.Name, offset)
	}

	start := offset + localHeaderSize + int64(binary.LittleEndian.Uint16(header[26:])) + int64(binary.LittleEndian.Uint16(header[28:]))

	raw, err := readRange(ctx, objects, key, start, int64(f.CompressedSize64)) //nolint:gosec // zipEntry bounds it by MaxFileBytes
	if err != nil {
		return nil, err
	}

	var body []byte

	switch f.Method {
	case zip.Store:
		body = raw
	case zip.Deflate:
		fr := flate.NewReader(bytes.NewReader(raw))
		defer func() { _ = fr.Close() }()

		if body, err = io.ReadAll(io.LimitReader(fr, domain.MaxFileBytes+1)); err != nil {
			return nil, fmt.Errorf("zip %s: inflate %s: %w", key, f.Name, err)
		}
	default:
		return nil, fmt.Errorf("zip %s: %s is compressed with method %d", key, f.Name, f.Method)
	}

	if uint64(len(body)) != f.UncompressedSize64 {
		return nil, fmt.Errorf("zip %s: %s has %d bytes, its record says %d", key, f.Name, len(body), f.UncompressedSize64)
	}

	return body, nil
}

func readRange(ctx context.Context, objects KnowledgeObjects, key string, offset, length int64) ([]byte, error) {
	rc, err := objects.GetRange(ctx, key, offset, length)
	if err != nil {
		return nil, fmt.Errorf("read %d bytes of %s at %d: %w", length, key, offset, err)
	}
	defer func() { _ = rc.Close() }()

	b, err := io.ReadAll(io.LimitReader(rc, length))
	if err != nil {
		return nil, fmt.Errorf("read %d bytes of %s at %d: %w", length, key, offset, err)
	}

	return b, nil
}

// zipTail is a zip's bytes from base on, its central directory and end record, which is all a
// zip.Reader reads to list its files. The bytes before base read as zeros: the reader searches
// windows of the zip's end for the end record's signature, which zeros never hold.
type zipTail struct {
	data []byte
	base int64
}

func (r zipTail) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off > r.base+int64(len(r.data)) {
		return 0, fmt.Errorf("read at %d, outside the zip", off)
	}

	n := 0
	if off < r.base {
		n = int(min(r.base-off, int64(len(p))))
		clear(p[:n])
		off += int64(n)
	}

	n += copy(p[n:], r.data[off-r.base:])
	if n < len(p) {
		return n, io.EOF
	}

	return n, nil
}
