package gateway

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Knowledge reads a knowledge source's snapshots of the organization in ctx, newest first, an
// unknown source being ports.ErrNotFound; ports.Knowledge satisfies it.
type Knowledge interface {
	Snapshots(ctx context.Context, source string) ([]domain.KnowledgeSnapshot, error)
}

// Objects reads a knowledge snapshot's zip from the bucket by range; ports.ObjectStore satisfies it.
type Objects interface {
	GetRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error)
}

// KnowledgePath is where a run downloads one of its pinned snapshots: GET KnowledgePath<source>.
const KnowledgePath = "/run/knowledge/"

// bundleKnowledge is what a run gets of its knowledge (ADR 0038 decisions 7 and 8): its prompt's
// Knowledge section and the snapshots prepare unpacks into /knowledge/<source>/, none for a
// draft request's run.
type bundleKnowledge struct {
	Section   string              `json:"section"`
	Snapshots []knowledgeSnapshot `json:"snapshots"`
}

// knowledgeSnapshot is one pinned snapshot: its source, which is its folder, and the URL of its
// zip, a path on the gateway that the worker resolves against its gateway URL.
type knowledgeSnapshot struct {
	Source string `json:"source"`
	URL    string `json:"url"`
}

// knowledgeOf is the run's knowledge: the line's sources that apply to run, each pinned one (c's
// pins) with its snapshot's line, file count and top folders, the others named as not synced yet,
// and the factory's memory. files says whether the box gets the snapshots. It is nil for an agent
// that may not read files and when there is nothing to say. A pinned snapshot the store no longer
// has counts as not synced; one whose zip cannot be read lists no folders, and the run goes on.
func (g *Gateway) knowledgeOf(
	ctx context.Context, c domain.TaskClaims, line domain.Line, agent domain.AgentSpec, run domain.RuleTarget, files bool,
) (*bundleKnowledge, error) {
	idx, err := domain.IndexKnowledge(line, agent, run, files, func(source string) (domain.KnowledgeSnapshot, []string, bool, error) {
		snap, ok, err := g.pinned(ctx, c, source)
		if err != nil || !ok {
			return domain.KnowledgeSnapshot{}, nil, false, err
		}

		return snap, g.topFolders(ctx, c, snap), true, nil
	})
	if err != nil {
		return nil, err
	}

	k := &bundleKnowledge{Snapshots: []knowledgeSnapshot{}}

	if files {
		for _, s := range idx.Sources {
			k.Snapshots = append(k.Snapshots, knowledgeSnapshot{Source: s.Name, URL: KnowledgePath + s.Name})
		}
	}

	if k.Section = idx.Section(); k.Section == "" {
		return nil, nil
	}

	return k, nil
}

// pinned is the snapshot c pinned of source, and whether it pinned one the store still has.
func (g *Gateway) pinned(ctx context.Context, c domain.TaskClaims, source string) (domain.KnowledgeSnapshot, bool, error) {
	i := slices.IndexFunc(c.Knowledge, func(p domain.KnowledgePin) bool { return p.Source == source })
	if i < 0 || g.knowledge == nil {
		return domain.KnowledgeSnapshot{}, false, nil
	}

	snaps, err := g.knowledge.Snapshots(ctx, source)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return domain.KnowledgeSnapshot{}, false, nil
	case err != nil:
		return domain.KnowledgeSnapshot{}, false, fmt.Errorf("snapshots of knowledge source %s: %w", source, err)
	}

	j := slices.IndexFunc(snaps, func(s domain.KnowledgeSnapshot) bool { return s.ID == c.Knowledge[i].Snapshot })
	if j < 0 {
		return domain.KnowledgeSnapshot{}, false, nil
	}

	return snaps[j], true, nil
}

// topFolders are the top folders of snap's zip (domain.TopFolders), read from its central
// directory, so the prompt names the folders of the files the box gets. A zip that cannot be read
// lists none, logged.
func (g *Gateway) topFolders(ctx context.Context, c domain.TaskClaims, snap domain.KnowledgeSnapshot) []string {
	if g.objects == nil || snap.Files == 0 {
		return nil
	}

	paths, err := zipPaths(ctx, g.objects, snap.Key, snap.ZipBytes)
	if err != nil {
		g.log.WarnContext(ctx, "knowledge snapshot folders unread", claimAttrs(c, "snapshot", snap.ID, "error", err)...)
		return nil
	}

	return domain.TopFolders(paths)
}

// Sizes of a zip's end of central directory record: its fixed part and the longest comment.
const (
	eocdSize       = 22
	maxZipComment  = 1<<16 - 1
	eocdSignature  = 0x06054b50
	zip64Marker    = 0xffffffff
	eocdDirSize    = 12
	eocdDirOffset  = 16
	maxZipDirBytes = 16 << 20
)

// zipPaths are the names of the files of the zip at key, size bytes long, read in two ranges: its
// end of central directory record, then its central directory, never the files themselves.
func zipPaths(ctx context.Context, objects Objects, key string, size int64) ([]string, error) {
	tailLen := min(size, eocdSize+maxZipComment)

	tail, err := readRange(ctx, objects, key, size-tailLen, tailLen)
	if err != nil {
		return nil, err
	}

	at := bytes.LastIndex(tail, binary.LittleEndian.AppendUint32(nil, eocdSignature))
	if at < 0 || len(tail)-at < eocdSize {
		return nil, errors.New("no end of central directory record")
	}

	dirSize := int64(binary.LittleEndian.Uint32(tail[at+eocdDirSize:]))
	dirOffset := int64(binary.LittleEndian.Uint32(tail[at+eocdDirOffset:]))

	if dirOffset == zip64Marker || dirSize > maxZipDirBytes || dirOffset+dirSize > size {
		return nil, fmt.Errorf("a central directory of %d bytes at %d does not fit a %d-byte zip", dirSize, dirOffset, size)
	}

	dir, err := readRange(ctx, objects, key, dirOffset, size-dirOffset)
	if err != nil {
		return nil, err
	}

	zr, err := zip.NewReader(offsetReader{data: dir, base: dirOffset}, size)
	if err != nil {
		return nil, fmt.Errorf("read the central directory: %w", err)
	}

	paths := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		paths = append(paths, f.Name)
	}

	return paths, nil
}

func readRange(ctx context.Context, objects Objects, key string, offset, length int64) ([]byte, error) {
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

// offsetReader is a zip's bytes from base on, its central directory and end record, which is all
// a zip.Reader reads to list its files. The bytes before base read as zeros: the reader searches
// windows of the zip's end for the end record's signature, which zeros never hold.
type offsetReader struct {
	data []byte
	base int64
}

func (r offsetReader) ReadAt(p []byte, off int64) (int, error) {
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

// ServeKnowledge serves GET /run/knowledge/<source>: the zip of the snapshot of source the run's
// task token pinned (ADR 0038 decision 7). A token that does not verify is 401; a source it did not
// pin, or a pinned snapshot the store no longer has, is 404, as is any draft request's token,
// whose box gets no files.
func (g *Gateway) ServeKnowledge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	claims, err := g.authenticate(r)
	if err != nil {
		g.unauthorized(w, r, err)
		return
	}

	ctx := domain.WithOrg(r.Context(), claims.OrgID)
	source := strings.TrimPrefix(r.URL.Path, KnowledgePath)

	var snap domain.KnowledgeSnapshot

	ok := claims.Kind != domain.TaskKindDraftRequest && g.objects != nil
	if ok {
		snap, ok, err = g.pinned(ctx, claims, source)
	}

	switch {
	case err != nil:
		g.log.ErrorContext(ctx, "knowledge snapshot failed", claimAttrs(claims, "source", source, "error", err)...)
		http.Error(w, "gateway unavailable", http.StatusServiceUnavailable)

		return
	case !ok:
		g.log.WarnContext(ctx, "knowledge snapshot not pinned", claimAttrs(claims, "source", source)...)
		http.NotFound(w, r)

		return
	}

	rc, err := g.objects.GetRange(ctx, snap.Key, 0, -1)
	if err != nil {
		g.log.ErrorContext(ctx, "knowledge snapshot failed", claimAttrs(claims, "source", source, "error", err)...)
		http.Error(w, "gateway unavailable", http.StatusServiceUnavailable)

		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Cache-Control", "no-store")

	if snap.ZipBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(snap.ZipBytes, 10))
	}

	n, err := io.Copy(w, rc)
	if err != nil {
		g.log.WarnContext(ctx, "write knowledge snapshot", claimAttrs(claims, "source", source, "error", err)...)
		return
	}

	g.log.InfoContext(ctx, "knowledge snapshot served", claimAttrs(claims, "source", source, "snapshot", snap.ID, "bytes", n)...)
}
