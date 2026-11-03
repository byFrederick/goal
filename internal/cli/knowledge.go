package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	iofs "io/fs"
	"maps"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
)

// knowledgeCommand runs goal knowledge sources | add | check | sync | files | cat | upload | rm |
// reads: the organization's knowledge sources, folders of Markdown files every run of the
// factories that list them reads under /knowledge (ADR 0038 decision 14). Members list sources,
// files and reads; adding, checking, syncing, uploading and removing need an owner or admin, or a
// key with knowledge:manage.
func knowledgeCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{
		"sources": knowledgeSources, "add": knowledgeAdd, "check": knowledgeCheck, "sync": knowledgeSync, "files": knowledgeFiles,
		"cat": knowledgeCat, "upload": knowledgeUpload, "rm": knowledgeRemove, "reads": knowledgeReads,
	}[args[0]]
	if !ok {
		return errUsage
	}

	return handler(ctx, out, getenv, args[1:])
}

// knowledgeSources lists the sources: kind, status, files, last and next sync, users.
func knowledgeSources(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge sources")
	orgFlag := addOrgFlag(fs)
	factory := fs.String("factory", "", "only the sources this factory's current version lists")

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
		out.printf("no knowledge sources in %s; add one with `goal knowledge add <name> --kind upload --read-when \"…\"`\n", c.org)

		return nil
	}

	now := time.Now()

	for _, s := range res.JSON200.Sources {
		out.printf("%s\n", sourceLine(s, now))
	}

	return nil
}

// sourceLine is one source as goal knowledge sources prints it, its read-when line under it.
func sourceLine(s gen.KnowledgeSource, now time.Time) string {
	kind := string(s.Kind)

	switch {
	case s.Secret != nil:
		kind += " (secret " + *s.Secret + ")"
	case s.Connection != nil:
		kind += " (" + *s.Connection + ")"
	}

	users := make([]string, 0, len(s.UsedBy.Factories))
	for _, f := range s.UsedBy.Factories {
		users = append(users, f.Name)
	}

	used := "listed by no factory"
	if len(users) > 0 {
		used = "listed by " + strings.Join(users, ",")
	}

	line := fmt.Sprintf("%-24s %-24s %-8s last sync %s  next %s  %s",
		s.Name, kind, s.Status, lastUsed(s.LastSyncedAt, now), nextSync(s.NextSyncAt), used)
	if s.ReadWhen != "" {
		line += "\n  read when: " + s.ReadWhen
	}

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

// sourceFlags are the settings goal knowledge add and check take.
type sourceFlags struct {
	kind, title, secret, readWhen, factories *string
	every                                    *time.Duration
	config                                   valuesFlag
}

func addSourceFlags(fs *flag.FlagSet) *sourceFlags {
	f := &sourceFlags{
		kind:     fs.String("kind", "", "upload, repository or confluence"),
		title:    fs.String("title", "", "what people call it (default: its name)"),
		secret:   fs.String("secret", "", "the secret a repository or Confluence source reads with"),
		readWhen: fs.String("read-when", "", "one line that tells agents when to open it, at most 160 characters"),
		factories: fs.String("factories", "", "the factories that read it, comma-separated: add lists it in each one's draft, "+
			"check warns about each"),
		every:  fs.Duration("every", 0, "how often it syncs, at least 15m (default 1h)"),
		config: valuesFlag{},
	}
	fs.Var(f.config, "config", "what it reads: <key>=<value>, such as repository=…, branch=main, include=docs/**/*.md, "+
		"or site=…, email=…, spaces=ENG (repeatable)")

	return f
}

// factoryNames are the --factories names.
func (f *sourceFlags) factoryNames() []string {
	var out []string

	for n := range strings.SplitSeq(*f.factories, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}

	return out
}

// knowledgeAdd adds a source, then lists it in each --factories factory's draft, never publishing.
func knowledgeAdd(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge add")
	orgFlag := addOrgFlag(fs)
	sf := addSourceFlags(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || *sf.kind == "" {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.CreateKnowledgeSourceWithResponse(ctx, c.org, sf.newSource(pos[0]))
	if err != nil {
		return apiDown(err)
	}

	if res.JSON201 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ added knowledge source %s (%s) to %s\n", res.JSON201.Name, res.JSON201.Kind, c.org)

	return listInDrafts(ctx, out, c, sf.factoryNames(), pos[0])
}

// newSource is the source named name that the flags describe.
func (f *sourceFlags) newSource(name string) gen.NewKnowledgeSource {
	body := gen.NewKnowledgeSource{Name: name, Title: name, Kind: gen.KnowledgeSourceKind(*f.kind), Config: (*map[string]string)(&f.config)}
	if *f.title != "" {
		body.Title = *f.title
	}

	if *f.secret != "" {
		body.Secret = f.secret
	}

	if *f.readWhen != "" {
		body.ReadWhen = f.readWhen
	}

	if *f.every > 0 {
		seconds := int(f.every.Seconds())
		body.SyncEverySeconds = &seconds
	}

	return body
}

// listInDrafts lists source in each factory's draft, saying which listed it already.
func listInDrafts(ctx context.Context, out *printer, c orgClient, factories []string, source string) error {
	for _, f := range factories {
		listed, name, err := listInDraft(ctx, c, f, source)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}

		if listed {
			out.printf("✓ listed %s in %s's draft; publish it with `goal factory publish %s`\n", source, name, name)
		} else {
			out.printf("  %s's draft lists %s already\n", name, source)
		}
	}

	return nil
}

// listInDraft appends source to knowledge.sources in the factory ref's draft factory.yaml, keeping
// the file's comments and order; false when the draft lists it already. It returns the factory's
// name.
func listInDraft(ctx context.Context, c orgClient, ref, source string) (bool, string, error) {
	d, err := openDraftFile(ctx, c, ref, config.FileName)
	if err != nil {
		return false, "", err
	}

	doc, err := d.node()
	if err != nil {
		return false, d.factory.Name, err
	}

	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return false, d.factory.Name, fmt.Errorf("the draft's %s is not a mapping", config.FileName)
	}

	sources := mappingAt(mappingAt(doc.Content[0], "spec", yaml.MappingNode), "knowledge", yaml.MappingNode)
	list := mappingAt(sources, "sources", yaml.SequenceNode)

	for _, e := range list.Content {
		if e.Value == source || (e.Kind == yaml.MappingNode && mappingAt(e, "name", yaml.ScalarNode).Value == source) {
			return false, d.factory.Name, nil
		}
	}

	list.Content = append(list.Content, scalar(source))

	return true, d.factory.Name, d.saveNode(ctx, c, doc)
}

// mappingAt is the value of key in the mapping m, added as an empty node of kind when m lacks it.
func mappingAt(m *yaml.Node, key string, kind yaml.Kind) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}

	v := &yaml.Node{Kind: kind}
	m.Content = append(m.Content, scalar(key), v)

	return v
}

// knowledgeCheck asks goald to check a source's settings as add would save them, storing nothing.
func knowledgeCheck(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge check")
	orgFlag := addOrgFlag(fs)
	sf := addSourceFlags(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || *sf.kind == "" {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	body := gen.KnowledgeCheckInput{Name: pos[0], Kind: gen.KnowledgeSourceKind(*sf.kind), Config: (*map[string]string)(&sf.config)}
	if *sf.secret != "" {
		body.Secret = sf.secret
	}

	if *sf.readWhen != "" {
		body.ReadWhen = sf.readWhen
	}

	if names := sf.factoryNames(); len(names) > 0 {
		body.Factories = &names
	}

	res, err := c.CheckKnowledgeSourceWithResponse(ctx, c.org, body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	printCheck(out, res.JSON200)

	return nil
}

// printCheck prints a Check's answers: access, size, limits, warnings and the agent line.
func printCheck(out *printer, got *gen.KnowledgeCheck) {
	if got.Access.Ok {
		out.printf("✓ access: goald lists %d files of at most %d\n", got.Files, got.Limits.Files)
	} else {
		out.printf("✗ access: %s\n", deref(got.Access.Error))
	}

	if got.Bytes != nil {
		out.printf("  size: %d bytes of at most %d\n", *got.Bytes, got.Limits.Bytes)
	}

	for _, l := range got.OverLimit {
		out.printf("✗ over the %s limit: a sync would fail\n", l)
	}

	for _, w := range got.Warnings {
		where := ""
		if f := deref(w.Factory); f != "" {
			where = f + ": "
		}

		out.printf("! %s%s (%s)\n", where, w.Message, w.Code)
	}

	out.printf("agents will see:\n  %s\n", got.Line)
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

// knowledgeFiles lists a source's current files, or with --find those of every source whose path
// or title starts with it.
func knowledgeFiles(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge files")
	orgFlag := addOrgFlag(fs)
	find := fs.String("find", "", "only the files whose path or title starts with this; without a source, across every source")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) > 1 || (len(pos) == 0 && *find == "") {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	if len(pos) == 0 {
		return findFiles(ctx, out, c, *find)
	}

	params := &gen.ListKnowledgeFilesParams{}
	if *find != "" {
		params.Find = find
	}

	for {
		res, err := c.ListKnowledgeFilesWithResponse(ctx, c.org, pos[0], params)
		if err != nil {
			return apiDown(err)
		}

		if res.JSON200 == nil {
			return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
		}

		printFiles(out, res.JSON200.Files)

		if res.JSON200.NextAfter == nil {
			return nil
		}

		params.After = res.JSON200.NextAfter
	}
}

// findFiles prints the files of every source whose path or title starts with find.
func findFiles(ctx context.Context, out *printer, c orgClient, find string) error {
	res, err := c.FindKnowledgeFilesWithResponse(ctx, c.org, &gen.FindKnowledgeFilesParams{Find: find})
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	printFiles(out, res.JSON200.Files)

	return nil
}

// printFiles prints each file as <source>/<path>, its size and its title.
func printFiles(out *printer, files []gen.KnowledgeFile) {
	for _, f := range files {
		out.printf("%-48s %8d bytes  %s\n", f.Source+"/"+f.Path, f.Bytes, f.Title)
	}
}

// knowledgeCat prints one file's Markdown as runs read it.
func knowledgeCat(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge cat")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	res, err := c.GetKnowledgeFileWithResponse(ctx, c.org, pos[0], pos[1])
	if err != nil {
		return apiDown(err)
	}

	if res.StatusCode() != http.StatusOK {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("%s", res.Body)

	if !bytes.HasSuffix(res.Body, []byte("\n")) {
		out.printf("\n")
	}

	return nil
}

// upload is one file goal knowledge upload sends: its path in the source and its content.
type upload struct {
	path string
	body []byte
}

// knowledgeUpload uploads files, and the files under folders at their paths below the folder, to
// an upload source, one request each: goald bounds a request by one file's size limit. Every file
// is read and its type checked first, so a missing or unsupported one uploads nothing; a refusal
// from goald stops at its file, those before it uploaded.
func knowledgeUpload(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge upload")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) < 2 {
		return errUsage
	}

	var uploads []upload

	for _, p := range pos[1:] {
		more, err := uploadsOf(p)
		if err != nil {
			return err
		}

		uploads = append(uploads, more...)
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	for _, u := range uploads {
		body, contentType, err := uploadForm(u.path, u.body)
		if err != nil {
			return err
		}

		res, err := c.UploadKnowledgeFilesWithBodyWithResponse(ctx, c.org, pos[0], contentType, body)
		if err != nil {
			return apiDown(err)
		}

		if res.JSON202 == nil {
			return fmt.Errorf("%s: %w", u.path, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault))
		}

		for _, f := range res.JSON202.Files {
			out.printf("✓ uploaded %s/%s (%d bytes); the next sync of %s takes it\n", pos[0], f.Path, f.Bytes, pos[0])
		}
	}

	return nil
}

// uploadsOf is the file at p, at its name, or every file under the folder p, at its path below
// it, in path order, each read and its type checked.
func uploadsOf(p string) ([]upload, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		raw, err := readUpload(p, filepath.Base(p))
		if err != nil {
			return nil, err
		}

		return []upload{{path: filepath.Base(p), body: raw}}, nil
	}

	var out []upload

	err = iofs.WalkDir(os.DirFS(p), ".", func(rel string, d iofs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && rel != "." && strings.HasPrefix(d.Name(), "."):
			return iofs.SkipDir
		case d.IsDir() || strings.HasPrefix(d.Name(), "."):
			return nil
		}

		raw, err := readUpload(filepath.Join(p, filepath.FromSlash(rel)), rel)
		if err != nil {
			return err
		}

		out = append(out, upload{path: path.Clean(rel), body: raw})

		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("%s holds no files to upload", p)
	}

	return out, nil
}

// readUpload reads the file at p, to upload at rel, refusing a type the source does not take.
func readUpload(p, rel string) ([]byte, error) {
	raw, err := os.ReadFile(p) //nolint:gosec // a file the user names to upload
	if err != nil {
		return nil, err
	}

	if _, err := domain.DocumentType(path.Base(rel), raw); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}

	return raw, nil
}

// uploadForm is the multipart form of one file at path, and its content type.
func uploadForm(path string, raw []byte) (*bytes.Buffer, string, error) {
	var body bytes.Buffer

	mw := multipart.NewWriter(&body)

	part, err := mw.CreateFormFile("files", path)
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

// knowledgeRemove deletes a source, or with a path one of an upload source's files.
func knowledgeRemove(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge rm")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) < 1 || len(pos) > 2 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	if len(pos) == 2 {
		res, err := c.DeleteKnowledgeUploadWithResponse(ctx, c.org, pos[0], &gen.DeleteKnowledgeUploadParams{Path: pos[1]})
		if err != nil {
			return apiDown(err)
		}

		if err := deleted(res.StatusCode(), res.HTTPResponse, res.ApplicationproblemJSONDefault); err != nil {
			return err
		}

		out.printf("✓ removed %s/%s; the next sync of %s leaves it out\n", pos[0], pos[1], pos[0])

		return nil
	}

	res, err := c.DeleteKnowledgeSourceWithResponse(ctx, c.org, pos[0])
	if err != nil {
		return apiDown(err)
	}

	if err := deleted(res.StatusCode(), res.HTTPResponse, res.ApplicationproblemJSONDefault); err != nil {
		return err
	}

	out.printf("✓ removed knowledge source %s\n", pos[0])

	return nil
}

// knowledgeReads prints how often runs read each file and searched each folder, summed over the
// window by source, path and factory, the most read first.
func knowledgeReads(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("knowledge reads")
	orgFlag := addOrgFlag(fs)
	source := fs.String("source", "", "only this source's reads")
	days := fs.Int("days", 0, "the window in days, counting today, 1 to 90 (default 7)")

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	params := &gen.ListKnowledgeReadsParams{}
	if *source != "" {
		params.Source = source
	}

	if *days > 0 {
		params.Days = days
	}

	res, err := c.ListKnowledgeReadsWithResponse(ctx, c.org, params)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	type key struct{ path, factory string }

	sums := map[key]*[2]int{}

	for _, r := range res.JSON200.Reads {
		k := key{r.Source + "/" + r.Path, r.Factory}
		if sums[k] == nil {
			sums[k] = &[2]int{}
		}

		sums[k][0] += r.Reads
		sums[k][1] += r.Searches
	}

	if len(sums) == 0 {
		out.printf("no reads under /knowledge in the window\n")

		return nil
	}

	keys := slices.SortedFunc(maps.Keys(sums), func(a, b key) int {
		if d := sums[b][0] + sums[b][1] - sums[a][0] - sums[a][1]; d != 0 {
			return d
		}

		return strings.Compare(a.path+a.factory, b.path+b.factory)
	})

	for _, k := range keys {
		out.printf("%-48s %-20s %4d reads %4d searches\n", k.path, k.factory, sums[k][0], sums[k][1])
	}

	return nil
}

// valuesFlag collects repeated k=v flags.
type valuesFlag map[string]string

func (v valuesFlag) String() string { return strings.Join(slices.Sorted(maps.Keys(v)), ",") }

func (v valuesFlag) Set(s string) error {
	k, val, ok := strings.Cut(s, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return fmt.Errorf("%q is not <field>=<value>", s)
	}

	v[strings.TrimSpace(k)] = val

	return nil
}
