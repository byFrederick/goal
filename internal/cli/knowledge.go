package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// knowledgeCommand runs goal knowledge sources | add | sync | upload | documents | rm | search |
// usage | uses | verdict: the organization's knowledge base (spec 4 §9) and where agents used it
// (ADR 0027). Members list and search it, read its usage and give verdicts; adding, syncing,
// uploading and removing needs an owner or admin, or a key with knowledge:manage.
func knowledgeCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{
		"sources": knowledgeSources, "add": knowledgeAdd, "sync": knowledgeSync, "upload": knowledgeUpload,
		"documents": knowledgeDocuments, "rm": knowledgeRemove, "search": knowledgeSearch,
		"usage": knowledgeUsage, "uses": knowledgeUses, "verdict": knowledgeVerdict,
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

// knowledgeUsage prints each document's use over the window: its runs and the verdicts on it, of
// one source or, without --source, of every source in turn.
func knowledgeUsage(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge usage")
	orgFlag := addOrgFlag(fs)
	source := fs.String("source", "", "only this source (default: every source)")
	days := fs.Int("days", 0, "the window in days, 1 to 90 (default 7)")
	asJSON := fs.Bool("json", false, "print goald's answer as it came, one per source")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 0 || *days < 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	sources, err := usageSources(ctx, c, *source)
	if err != nil || len(sources) == 0 {
		if err == nil {
			out.printf("no knowledge sources in %s; add one with `goal knowledge add <name> --kind upload`\n", c.org)
		}

		return err
	}

	answers, err := sourceUsages(ctx, c, sources, *days)
	if err != nil {
		return err
	}

	var docs []gen.KnowledgeDocumentUsage

	for _, res := range answers {
		if *asJSON {
			out.printf("%s\n", bytes.TrimSuffix(res.Body, []byte("\n")))
		}

		docs = append(docs, res.JSON200.Documents...)
	}

	switch {
	case *asJSON:
	case len(docs) == 0:
		out.printf("no run used a document of %s in the last %d days\n", strings.Join(sources, ", "), answers[0].JSON200.Days)
	default:
		printUsage(out, docs)
	}

	return nil
}

// sourceUsages is goald's usage answer for each of sources, in their order, over days (goald's
// default when 0).
func sourceUsages(ctx context.Context, c orgClient, sources []string, days int) ([]*gen.GetKnowledgeUsageResponse, error) {
	params := &gen.GetKnowledgeUsageParams{}
	if days > 0 {
		params.Days = &days
	}

	answers := make([]*gen.GetKnowledgeUsageResponse, 0, len(sources))

	for _, name := range sources {
		params.Source = name

		res, err := c.GetKnowledgeUsageWithResponse(ctx, c.org, params)
		if err != nil {
			return nil, apiDown(err)
		}

		if res.JSON200 == nil {
			return nil, fmt.Errorf("%s: %w", name, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault))
		}

		answers = append(answers, res)
	}

	return answers, nil
}

// usageSources is source, or without one the name of every source of the organization.
func usageSources(ctx context.Context, c orgClient, source string) ([]string, error) {
	if source != "" {
		return []string{source}, nil
	}

	res, err := c.ListKnowledgeSourcesWithResponse(ctx, c.org, &gen.ListKnowledgeSourcesParams{})
	if err != nil {
		return nil, apiDown(err)
	}

	if res.JSON200 == nil {
		return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	names := make([]string, 0, len(res.JSON200.Sources))
	for _, s := range res.JSON200.Sources {
		names = append(names, s.Name)
	}

	return names, nil
}

// printUsage prints one row per document: helped and misled count agents and people, with the
// people's share in brackets.
func printUsage(out *printer, docs []gen.KnowledgeDocumentUsage) {
	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "DOCUMENT\tTITLE\tSOURCE\tRUNS\tHELPED\tMISLED")

	for _, d := range docs {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", d.Id, d.Title, d.Source, d.Runs,
			verdictCount(d.AgentsHelped, d.PeopleHelped), verdictCount(d.AgentsMisled, d.PeopleMisled))
	}

	_ = tw.Flush()

	out.printf("%s", b.String())
}

// verdictCount is a total of verdicts, "3 (1 people)" when people gave some of them.
func verdictCount(agents, people int) string {
	if people == 0 {
		return strconv.Itoa(agents)
	}

	return fmt.Sprintf("%d (%d people)", agents+people, people)
}

// knowledgeUses lists the runs that used a document, newest first, page by page.
func knowledgeUses(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge uses")
	orgFlag := addOrgFlag(fs)
	asJSON := fs.Bool("json", false, "print every page's uses as one JSON object")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	uses := []gen.KnowledgeDocumentUse{}
	params := &gen.ListKnowledgeDocumentUsesParams{}

	for {
		res, err := c.ListKnowledgeDocumentUsesWithResponse(ctx, c.org, pos[0], params)
		if err != nil {
			return apiDown(err)
		}

		if res.JSON200 == nil {
			return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
		}

		uses = append(uses, res.JSON200.Uses...)

		if res.JSON200.NextAfter == nil {
			break
		}

		params.After = res.JSON200.NextAfter
	}

	if *asJSON {
		raw, err := json.Marshal(gen.KnowledgeDocumentUsePage{Uses: uses})
		if err != nil {
			return fmt.Errorf("knowledge document %s uses: %w", pos[0], err)
		}

		out.printf("%s\n", raw)

		return nil
	}

	if len(uses) == 0 {
		out.printf("no run used knowledge document %s\n", pos[0])

		return nil
	}

	var b bytes.Buffer

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "RUN\tITEM\tTITLE\tFACTORY\tKINDS\tFIRST USE")

	for _, u := range uses {
		kinds := make([]string, 0, len(u.Kinds))
		for _, k := range u.Kinds {
			kinds = append(kinds, string(k))
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", u.RunId, u.Item.Ref, u.Item.Title, u.Factory.Name,
			strings.Join(kinds, ","), u.At.Local().Format(time.DateTime))
	}

	_ = tw.Flush()

	out.printf("%s", b.String())

	return nil
}

// knowledgeVerdict gives the caller's verdict on a document, of a run that used it with --run.
func knowledgeVerdict(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge verdict")
	orgFlag := addOrgFlag(fs)
	run := fs.String("run", "", "the run that used the document")
	note := fs.String("note", "", "why, in a sentence or two")
	asJSON := fs.Bool("json", false, "print goald's answer as it came")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 {
		return errUsage
	}

	verdict := gen.KnowledgeVerdictInputVerdict(pos[1])
	if verdict != gen.KnowledgeVerdictInputVerdictHelped && verdict != gen.KnowledgeVerdictInputVerdictMisled {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.GiveKnowledgeVerdictWithResponse(ctx, c.org, pos[0],
		gen.KnowledgeVerdictInput{Verdict: verdict, RunId: optional(*run), Note: optional(*note)})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if *asJSON {
		out.printf("%s", res.Body)

		return nil
	}

	line := fmt.Sprintf("✓ %s: knowledge document %s of %s", res.JSON200.Verdict, res.JSON200.Document, res.JSON200.Source)
	if r := deref(res.JSON200.RunId); r != "" {
		line += ", run " + r
	}

	out.printf("%s\n", line)

	return nil
}
