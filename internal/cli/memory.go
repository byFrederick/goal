package cli

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
)

// memoryCommand runs goal memory list | add | rm: a factory's memory, its memory.yaml (ADR 0038
// decision 6). list reads the open draft's, or the current version's, and opens no draft. Each
// change writes the factory's draft, opening one when none is open, and never publishes, but add
// --publish publishes the draft after it.
func memoryCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	handler, ok := map[string]command{subList: memoryList, subAdd: memoryAdd, "rm": memoryRemove}[args[0]]
	if !ok {
		return errUsage
	}

	return handler(ctx, out, getenv, args[1:])
}

// memoryList prints the draft's facts, numbered as goal memory rm takes them.
func memoryList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("memory list")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	d, err := readMemoryFile(ctx, c, pos[0])
	if err != nil {
		return err
	}

	var entries []config.MemoryEntry
	if err := yaml.Unmarshal(d.content, &entries); err != nil {
		return fmt.Errorf("%s's %s does not parse: %w", d.factory.Name, config.MemoryFile, err)
	}

	if len(entries) == 0 {
		out.printf("%s has no factory memory; add a fact with `goal memory add %s \"<text>\"`\n", d.factory.Name, d.factory.Name)

		return nil
	}

	for i, e := range entries {
		line := fmt.Sprintf("%d. %s", i+1, e.Text)
		if len(e.From) > 0 {
			line += " (from " + strings.Join(e.From, ", ") + ")"
		}

		if e.Added != "" {
			line += "  added " + e.Added
		}

		out.printf("%s\n", line)
	}

	return nil
}

// memoryAdd appends a fact, dated today, to the draft's memory.yaml.
func memoryAdd(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("memory add")
	orgFlag := addOrgFlag(fs)
	from := fs.String("from", "", "the item it came from, as <factory>#<number> (comma-separated for more)")
	publishToo := fs.Bool("publish", false, "publish the draft after the change")

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 || strings.TrimSpace(pos[1]) == "" {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	d, err := openDraftFile(ctx, c, pos[0], config.MemoryFile)
	if err != nil {
		return err
	}

	seq, doc, err := memorySequence(d)
	if err != nil {
		return err
	}

	entry := &yaml.Node{Kind: yaml.MappingNode}
	entry.Content = append(entry.Content, scalar("text"), scalar(strings.TrimSpace(pos[1])))

	if *from != "" {
		items := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for f := range strings.SplitSeq(*from, ",") {
			items.Content = append(items.Content, scalar(strings.TrimSpace(f)))
		}

		entry.Content = append(entry.Content, scalar("from"), items)
	}

	entry.Content = append(entry.Content, scalar("added"), scalar(time.Now().Format(time.DateOnly)))
	seq.Content = append(seq.Content, entry)

	if err := d.saveNode(ctx, c, doc); err != nil {
		return err
	}

	out.printf("✓ added fact %d to %s's factory memory in its draft\n", len(seq.Content), d.factory.Name)

	if !*publishToo {
		out.printf("  publish it with `goal factory publish %s`\n", d.factory.Name)

		return nil
	}

	return publishOpenDraft(ctx, out, c, d.factory.Id, "", nil)
}

// memoryRemove removes the n-th fact (from 1, as goal memory list numbers them).
func memoryRemove(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("memory rm")
	orgFlag := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 {
		return errUsage
	}

	n, err := strconv.Atoi(pos[1])
	if err != nil {
		return errUsage
	}

	c, err := connect(ctx, getenv, *orgFlag)
	if err != nil {
		return err
	}

	d, err := openDraftFile(ctx, c, pos[0], config.MemoryFile)
	if err != nil {
		return err
	}

	seq, doc, err := memorySequence(d)
	if err != nil {
		return err
	}

	if n < 1 || n > len(seq.Content) {
		return fmt.Errorf("%s's factory memory has %d facts; `goal memory list %s` numbers them", d.factory.Name, len(seq.Content), d.factory.Name)
	}

	seq.Content = append(seq.Content[:n-1], seq.Content[n:]...)

	if err := d.saveNode(ctx, c, doc); err != nil {
		return err
	}

	out.printf("✓ removed fact %d from %s's factory memory in its draft; publish it with `goal factory publish %s`\n", n, d.factory.Name, d.factory.Name)

	return nil
}

// memorySequence is the draft's memory.yaml as a document whose content is its list of entries,
// that list made when the file is missing or empty.
func memorySequence(d *draftFile) (*yaml.Node, *yaml.Node, error) {
	doc, err := d.node()
	if err != nil {
		return nil, nil, err
	}

	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.SequenceNode}}
	}

	seq := doc.Content[0]
	if seq.Kind != yaml.SequenceNode {
		return nil, nil, fmt.Errorf("%s's %s is not a list of facts; fix it in the definition editor", d.factory.Name, config.MemoryFile)
	}

	// A flow list ([]) reads badly once it holds mappings.
	seq.Style = 0

	return seq, doc, nil
}

func scalar(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: v} }

// draftFile is one file of a factory's open draft as the CLI edits it: its content and its ETag,
// both empty when the draft lacks the file.
type draftFile struct {
	factory *gen.Factory
	path    string
	content []byte
	etag    string
}

// openDraftFile reads path of the factory ref's draft, opening the draft from the current version
// when none is open.
func openDraftFile(ctx context.Context, c orgClient, ref, path string) (*draftFile, error) {
	f, err := getFactory(ctx, c, ref)
	if err != nil {
		return nil, err
	}

	opened, err := c.OpenDraftWithResponse(ctx, c.org, f.Id, gen.OpenDraftRequest{})
	if err != nil {
		return nil, apiDown(err)
	}

	if cmp.Or(opened.JSON201, opened.JSON200) == nil {
		return nil, problem(opened.HTTPResponse, opened.ApplicationproblemJSONDefault)
	}

	return readDraftFile(ctx, c, f, path)
}

// readMemoryFile reads memory.yaml of the factory ref's open draft, or of its current version
// when no draft is open, without opening one; empty when neither has the file.
func readMemoryFile(ctx context.Context, c orgClient, ref string) (*draftFile, error) {
	f, err := getFactory(ctx, c, ref)
	if err != nil {
		return nil, err
	}

	if f.Draft != nil {
		return readDraftFile(ctx, c, f, config.MemoryFile)
	}

	d := &draftFile{factory: f, path: config.MemoryFile}
	if f.Current == nil {
		return d, nil
	}

	res, err := c.GetVersionFileWithResponse(ctx, c.org, f.Id, strconv.Itoa(f.Current.Number), config.MemoryFile)
	if err != nil {
		return nil, apiDown(err)
	}

	switch {
	case res.StatusCode() == http.StatusOK:
		d.content = res.Body
	case problemCode(res.ApplicationproblemJSONDefault) == authoring.CodeFileNotFound:
	default:
		return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	return d, nil
}

// readDraftFile reads path of f's open draft.
func readDraftFile(ctx context.Context, c orgClient, f *gen.Factory, path string) (*draftFile, error) {
	res, err := c.GetDraftFileWithResponse(ctx, c.org, f.Id, path)
	if err != nil {
		return nil, apiDown(err)
	}

	d := &draftFile{factory: f, path: path}

	switch {
	case res.StatusCode() == http.StatusOK:
		d.content, d.etag = res.Body, res.HTTPResponse.Header.Get("ETag")
	case problemCode(res.ApplicationproblemJSONDefault) == authoring.CodeFileNotFound:
	default:
		return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	return d, nil
}

// node is the file as a YAML document node, with its comments; an empty document for a file that
// is missing or empty.
func (d *draftFile) node() (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(d.content, &doc); err != nil {
		return nil, fmt.Errorf("%s's %s does not parse: %w", d.factory.Name, d.path, err)
	}

	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
	}

	return &doc, nil
}

// saveNode writes doc as the file, refused when the draft's file changed since it was read.
func (d *draftFile) saveNode(ctx context.Context, c orgClient, doc *yaml.Node) error {
	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("write %s: %w", d.path, err)
	}

	if err := enc.Close(); err != nil {
		return fmt.Errorf("write %s: %w", d.path, err)
	}

	params := &gen.PutDraftFileParams{}
	if d.etag != "" {
		params.IfMatch = &d.etag
	} else {
		params.IfNoneMatch = new("*")
	}

	res, err := c.PutDraftFileWithBodyWithResponse(ctx, c.org, d.factory.Id, d.path, params, "application/octet-stream", &buf)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 != nil {
		return nil
	}

	if problemCode(res.ApplicationproblemJSONDefault) == authoring.CodeDraftConflict {
		return errors.New(d.factory.Name + "'s " + d.path + " changed in its draft meanwhile; nothing was written, run the command again")
	}

	return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
}
