package cli

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
)

// knowledgeCommand runs goal knowledge sources | add | sync | upload | documents | rm | search: the
// organization's knowledge base (spec 4 §9). Members list and search it; adding, syncing,
// uploading and removing needs an owner or admin, or a key with knowledge:manage.
func knowledgeCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{
		"sources": knowledgeSources, "add": knowledgeAdd, "sync": knowledgeSync, "upload": knowledgeUpload,
		"documents": knowledgeDocuments, "rm": knowledgeRemove, "search": knowledgeSearch,
	}[args[0]]
	if !ok {
		return errUsage
	}

	return handler(ctx, out, getenv, args[1:])
}

// knowledgeSources lists the sources: kind, status, documents, last and next sync, users.
func knowledgeSources(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge sources")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "only the sources this factory's current version may search")

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	params := &gen.ListKnowledgeSourcesParams{}
	if *factory != "" {
		params.Factory = factory
	}

	res, err := c.ListKnowledgeSourcesWithResponse(ctx, c.org, params)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if len(res.JSON200.Sources) == 0 {
		out.printf("no knowledge sources in %s; add one with `goal knowledge add <name> --kind upload`\n", c.org)

		return nil
	}

	now := time.Now()

	for _, s := range res.JSON200.Sources {
		out.printf("%s\n", sourceLine(s, now))
	}

	return nil
}

// sourceLine is one source as goal knowledge sources prints it.
func sourceLine(s gen.KnowledgeSource, now time.Time) string {
	kind := string(s.Kind)
	if s.Connection != nil {
		kind += " (" + *s.Connection + ")"
	}

	users := make([]string, 0, len(s.UsedBy.Factories))
	for _, f := range s.UsedBy.Factories {
		users = append(users, f.Name)
	}

	used := "searched by no factory"
	if len(users) > 0 {
		used = "searched by " + strings.Join(users, ",")
	}

	line := fmt.Sprintf("%-24s %-24s %-8s %d documents  last sync %s  next %s  %s",
		s.Name, kind, s.Status, s.Documents, lastUsed(s.LastSyncedAt, now), nextSync(s.NextSyncAt), used)
	if e := deref(s.LastError); e != "" {
		line += "\n  " + e
	}

	return line
}

// nextSync is when a source syncs next: never for a live source.
func nextSync(at *time.Time) string {
	if at == nil {
		return "never"
	}

	return at.Local().Format(time.DateTime)
}

// knowledgeAdd adds a source: an upload source, or a synced or live one reading a connection.
func knowledgeAdd(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge add")
	orgFlag := addOrgFlag(fs)
	kind := fs.String("kind", "", "upload, synced or live")
	title := fs.String("title", "", "what people call it (default: its name)")
	connection := fs.String("connection", "", "the connection a synced or live source reads")
	every := fs.Duration("every", 0, "how often it syncs, at least 15m (default 1h)")
	config := valuesFlag{}
	fs.Var(config, "config", "what a synced source reads: <key>=<value>, such as repository=…, paths=docs/**/*.md or space=ENG (repeatable)")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || *kind == "" {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	body := gen.NewKnowledgeSource{Name: pos[0], Title: pos[0], Kind: gen.KnowledgeSourceKind(*kind), Config: (*map[string]string)(&config)}
	if *title != "" {
		body.Title = *title
	}

	if *connection != "" {
		body.Connection = connection
	}

	if *every > 0 {
		seconds := int(every.Seconds())
		body.SyncEverySeconds = &seconds
	}

	res, err := c.CreateKnowledgeSourceWithResponse(ctx, c.org, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ added knowledge source %s (%s) to %s\n", res.JSON201.Name, res.JSON201.Kind, c.org)

	return nil
}

// knowledgeSync makes a source due now.
func knowledgeSync(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge sync")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.SyncKnowledgeSourceWithResponse(ctx, c.org, pos[0])
	if err != nil {
		return apiDown(err)
	}

	if res.StatusCode() != http.StatusAccepted {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ knowledge source %s is due: the next free goald syncs it\n", pos[0])

	return nil
}

// knowledgeUpload uploads files to an upload source, one request each: goald bounds a request
// by one file's size limit. Every file is read and its type checked first, so a missing or
// unsupported one uploads nothing; a refusal from goald stops at its file, those before it
// uploaded.
func knowledgeUpload(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge upload")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) < 2 {
		return errUsage
	}

	bodies := make([][]byte, 0, len(pos)-1)

	for _, path := range pos[1:] {
		raw, err := os.ReadFile(path) //nolint:gosec // a file the user names to upload
		if err != nil {
			return err
		}

		if _, err := domain.DocumentType(filepath.Base(path), raw); err != nil {
			return err
		}

		bodies = append(bodies, raw)
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	for i, path := range pos[1:] {
		body, contentType, err := uploadForm(filepath.Base(path), bodies[i])
		if err != nil {
			return err
		}

		res, err := c.UploadKnowledgeDocumentsWithBodyWithResponse(ctx, c.org, pos[0], contentType, body)
		if err != nil {
			return apiDown(err)
		}

		if res.JSON202 == nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), problem(res.HTTPResponse, res.ApplicationproblemJSONDefault))
		}

		for _, d := range res.JSON202.Documents {
			out.printf("✓ uploaded %s (%s), %s until the next sync of %s\n", d.Title, d.Id, d.Status, pos[0])
		}
	}

	return nil
}

// uploadForm is the multipart form of one file named name, and its content type.
func uploadForm(name string, raw []byte) (*bytes.Buffer, string, error) {
	var body bytes.Buffer

	mw := multipart.NewWriter(&body)

	part, err := mw.CreateFormFile("files", name)
	if err != nil {
		return nil, "", err
	}

	if _, err := part.Write(raw); err != nil {
		return nil, "", err
	}

	if err := mw.Close(); err != nil {
		return nil, "", err
	}

	return &body, mw.FormDataContentType(), nil
}

// knowledgeDocuments lists every document of a source, page by page.
func knowledgeDocuments(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge documents")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	params := &gen.ListKnowledgeDocumentsParams{}

	for {
		res, err := c.ListKnowledgeDocumentsWithResponse(ctx, c.org, pos[0], params)
		if err != nil {
			return apiDown(err)
		}

		if res.JSON200 == nil {
			return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
		}

		for _, d := range res.JSON200.Documents {
			line := fmt.Sprintf("%s  %-40s %-8s %d bytes", d.Id, d.Title, d.Status, d.Bytes)
			if e := deref(d.Error); e != "" {
				line += "  " + e
			}

			out.printf("%s\n", line)
		}

		if res.JSON200.NextAfter == nil {
			return nil
		}

		params.After = res.JSON200.NextAfter
	}
}

// knowledgeRemove deletes an uploaded document.
func knowledgeRemove(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge rm")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.DeleteKnowledgeDocumentWithResponse(ctx, c.org, pos[0])
	if err != nil {
		return apiDown(err)
	}

	if err := deleted(res.StatusCode(), res.HTTPResponse, res.ApplicationproblemJSONDefault); err != nil {
		return err
	}

	out.printf("✓ removed knowledge document %s\n", pos[0])

	return nil
}

// knowledgeSearch searches as agents do: the hits best first, each with its document id.
func knowledgeSearch(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge search")
	orgFlag := addOrgFlag(fs)
	limit := fs.Int("limit", 0, "how many hits, 1 to 20 (default 8)")

	var sources listFlag

	fs.Var(&sources, "source", "only this source (repeatable)")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	params := &gen.SearchKnowledgeParams{Q: pos[0]}
	if len(sources) > 0 {
		params.Source = (*[]string)(&sources)
	}

	if *limit > 0 {
		params.Limit = limit
	}

	res, err := c.SearchKnowledgeWithResponse(ctx, c.org, params)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if len(res.JSON200.Hits) == 0 {
		out.printf("nothing in %s's knowledge base matches %q\n", c.org, pos[0])

		return nil
	}

	for _, h := range res.JSON200.Hits {
		where := h.Document
		if u := deref(h.Url); u != "" {
			where = u
		}

		out.printf("%.4f  %s / %s  %s\n", h.Score, h.Source, h.Title, where)

		if h.Heading != "" {
			out.printf("  %s\n", h.Heading)
		}

		out.printf("  %s\n", snippetLine(h.Snippet))
	}

	return nil
}

// maxSnippet is how much of a hit's text goal knowledge search prints.
const maxSnippet = 200

// snippetLine is a hit's text on one line, cut at maxSnippet runes.
func snippetLine(text string) string {
	line := strings.Join(strings.Fields(text), " ")
	if r := []rune(line); len(r) > maxSnippet {
		return string(r[:maxSnippet]) + "…"
	}

	return line
}
