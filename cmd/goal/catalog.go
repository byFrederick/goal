package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/adapters/envsecrets"
	"github.com/byFrederick/goal/internal/adapters/skillssh"
	"github.com/byFrederick/goal/internal/adapters/sysclock"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/ports"
)

const (
	// maxSourcesBytes bounds the sources file of goal catalog skills sync.
	maxSourcesBytes = 1 << 20

	subSync = "sync"
	subList = "list"
	subPush = "push"
)

// syncArgs are the flags of goal catalog skills sync.
type syncArgs struct {
	file   string
	search []string
	top    int
}

// catalogCommand runs goal catalog skills sync|list and goal catalog templates push|list, the
// operator's commands over the bucket's skills catalog (catalog/skills/) and template catalog
// (catalog/templates/). They use the bucket directly, like goal storage init, and act in no
// organization: the catalogs are the install's.
func catalogCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) < 2 {
		return errUsage
	}

	switch args[0] {
	case "skills":
		return skillsCommand(ctx, out, getenv, args)
	case "templates":
		return templatesCommand(ctx, out, getenv, args[1:])
	default:
		return errUsage
	}
}

// skillsCommand runs goal catalog skills sync|list; args starts with "skills".
func skillsCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	var (
		sync  syncArgs
		query string
	)

	switch args[1] {
	case subSync:
		a, err := parseSyncArgs(args[2:])
		if err != nil {
			return err
		}

		sync = a
	case subList:
		rest, err := parseInterspersed(newFlags("catalog skills list"), args[2:])
		if err != nil || len(rest) > 1 {
			return errUsage
		}

		if len(rest) == 1 {
			query = rest[0]
		}
	default:
		return errUsage
	}

	objects, err := newStore(ctx, getenv)
	if err != nil {
		return err
	}

	catalog := builder.New(objects, sysclock.Clock{}, builder.Config{})

	if args[1] == subList {
		return listSkills(ctx, out, catalog, query)
	}

	src, err := newSkillSource(ctx, getenv)
	if err != nil {
		return err
	}

	return syncSkills(ctx, out, catalog, src, objects.Bucket(), sync)
}

func parseSyncArgs(args []string) (syncArgs, error) {
	var a syncArgs

	fs := newFlags("catalog skills sync")
	fs.StringVar(&a.file, "file", "", "")
	fs.IntVar(&a.top, "top", 5, "")
	fs.Func("search", "", func(q string) error {
		a.search = append(a.search, q)
		return nil
	})

	rest, err := parseInterspersed(fs, args)
	if err != nil || len(rest) > 0 || (a.file == "" && len(a.search) == 0) || a.top < 1 || a.top > 50 {
		return syncArgs{}, errUsage
	}

	return a, nil
}

// newSkillSource reads skills.sh and GitHub where the environment says (GOAL_SKILLS_DIRECTORY_URL,
// GOAL_SKILLS_GITHUB_API_URL), with the optional tokens GOAL_SKILLS_DIRECTORY_TOKEN and
// GOAL_SKILLS_GITHUB_TOKEN resolved through the Secrets port.
func newSkillSource(ctx context.Context, getenv func(string) string) (*skillssh.Source, error) {
	secrets := envsecrets.New(func(name string) (string, bool) {
		v := getenv(name)
		return v, v != ""
	})

	token := func(name string) (string, error) {
		v, err := secrets.Resolve(ctx, "env:"+name)
		if errors.Is(err, ports.ErrSecretNotFound) {
			return "", nil
		}

		return v, err
	}

	dirToken, err := token("GOAL_SKILLS_DIRECTORY_TOKEN")
	if err != nil {
		return nil, err
	}

	ghToken, err := token("GOAL_SKILLS_GITHUB_TOKEN")
	if err != nil {
		return nil, err
	}

	return skillssh.New(skillssh.Config{
		DirectoryURL: getenv("GOAL_SKILLS_DIRECTORY_URL"), DirectoryToken: dirToken,
		GitHubAPIURL: getenv("GOAL_SKILLS_GITHUB_API_URL"), GitHubToken: ghToken,
	})
}

// readSources reads a sources file: a YAML list of owner/repo@skill[#commit].
func readSources(file string) ([]ports.SkillRef, error) {
	f, err := os.Open(file) //nolint:gosec // the operator names the file to read
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(io.LimitReader(f, maxSourcesBytes+1))
	if err != nil {
		return nil, err
	}

	if len(raw) > maxSourcesBytes {
		return nil, fmt.Errorf("%s is over %d bytes", file, maxSourcesBytes)
	}

	var items []string
	if err := yaml.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("%s is not a YAML list of owner/repo@skill[#commit]: %w", file, err)
	}

	refs := make([]ports.SkillRef, 0, len(items))

	for i, item := range items {
		ref, err := builder.ParseRef(item)
		if err != nil {
			return nil, fmt.Errorf("%s, entry %d: %w", file, i+1, err)
		}

		refs = append(refs, ref)
	}

	return refs, nil
}

// syncSkills runs goal catalog skills sync and prints one line per skill; any failure exits 1.
func syncSkills(ctx context.Context, out *printer, catalog *builder.Service, src ports.SkillSource, bucket string, a syncArgs) error {
	var refs []ports.SkillRef

	if a.file != "" {
		var err error
		if refs, err = readSources(a.file); err != nil {
			return err
		}
	}

	report, err := catalog.SyncSkills(ctx, src, refs, a.search, a.top)

	counts := map[builder.SyncStatus]int{}

	for _, r := range report.Results {
		counts[r.Status]++

		switch r.Status {
		case builder.SyncFailed:
			out.printf("  %-9s %s: %v\n", r.Status, r.Key, r.Err)
		case builder.SyncAdded, builder.SyncUnchanged:
			out.printf("  %-9s %-50s %s\n", r.Status, r.Key, r.Commit[:min(7, len(r.Commit))])
		}
	}

	if err != nil {
		return err
	}

	out.printf("%d skills in s3://%s/catalog/skills/index.json: %d added, %d unchanged, %d failed\n", report.Skills, bucket,
		counts[builder.SyncAdded], counts[builder.SyncUnchanged], counts[builder.SyncFailed])

	if n := report.Failed(); n > 0 {
		return fmt.Errorf("%d of %d failed", n, len(report.Results))
	}

	return nil
}

// listSkills runs goal catalog skills list [q]: key, short commit, installs and description.
func listSkills(ctx context.Context, out *printer, catalog *builder.Service, q string) error {
	skills, err := catalog.ListSkills(ctx, q)
	if err != nil {
		return err
	}

	for _, s := range skills {
		out.printf("%-50s %s %8s  %s\n", s.String(), s.Commit[:7], strconv.FormatInt(s.Installs, 10), cut(s.Description, 80))
	}

	out.printf("%d skills\n", len(skills))

	return nil
}

// A template folder: the catalog's layout of one version (B §7).
const (
	templateMetaFile = "template.yaml"
	templateFactory  = "factory"
)

// templatesCommand runs goal catalog templates push <dir> --version N [--name NAME] | list.
func templatesCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	var (
		upload authoring.TemplateUpload
		dir    string
	)

	switch args[0] {
	case subPush:
		fs := newFlags("catalog templates push")
		name := fs.String("name", "", "the template's name (default: the folder's)")
		version := fs.Int("version", 0, "the version to add")

		pos, err := parseInterspersed(fs, args[1:])
		if err != nil || len(pos) != 1 || *version < 1 {
			return errUsage
		}

		dir = pos[0]

		if upload, err = readTemplate(dir, cmp.Or(*name, filepath.Base(filepath.Clean(dir))), *version); err != nil {
			return err
		}
	case subList:
		if len(args) != 1 {
			return errUsage
		}
	default:
		return errUsage
	}

	objects, err := newStore(ctx, getenv)
	if err != nil {
		return err
	}

	catalog := authoring.NewTemplateCatalog(objects, sysclock.Clock{})

	if args[0] == subList {
		return listTemplates(ctx, out, catalog)
	}

	return pushTemplate(ctx, out, catalog, dir, upload, objects.Bucket())
}

// readTemplate reads a template folder: template.yaml and the definition under factory/.
func readTemplate(dir, name string, version int) (authoring.TemplateUpload, error) {
	meta, err := os.ReadFile(filepath.Join(dir, templateMetaFile)) //nolint:gosec // the folder the operator names
	if err != nil {
		return authoring.TemplateUpload{}, fmt.Errorf("a template folder holds %s and the definition in %s/: %w",
			templateMetaFile, templateFactory, err)
	}

	files, err := readFolder(filepath.Join(dir, templateFactory))
	if err != nil {
		return authoring.TemplateUpload{}, err
	}

	return authoring.TemplateUpload{Name: name, Version: version, Meta: meta, Files: files}, nil
}

// pushTemplate adds one template version to the catalog, printing what keeps it out.
func pushTemplate(
	ctx context.Context, out *printer, catalog *authoring.TemplateCatalog, dir string, t authoring.TemplateUpload, bucket string,
) error {
	err := catalog.Push(ctx, t)

	if invalid, ok := errors.AsType[*authoring.TemplateInvalidError](err); ok {
		for _, p := range invalid.Problems {
			out.printf("%s", problemLine(dir, p.File, p.Line, p.Column, string(p.Severity), p.Code, p.Message))
		}

		return fmt.Errorf("%s is not added to the catalog: %d problems", dir, len(invalid.Problems))
	}

	if errors.Is(err, authoring.ErrTemplateExists) {
		return fmt.Errorf("%s v%d is in the catalog already and template versions never change; push it as a new --version",
			t.Name, t.Version)
	}

	if err != nil {
		return err
	}

	out.printf("✓ added %s v%d (%d files) to s3://%s/%s%s/%d/\n", t.Name, t.Version, len(t.Files), bucket,
		authoring.TemplatesPrefix, t.Name, t.Version)

	return nil
}

// listTemplates runs goal catalog templates list: the latest version of each template.
func listTemplates(ctx context.Context, out *printer, catalog *authoring.TemplateCatalog) error {
	templates, err := catalog.List(ctx)
	if err != nil {
		return err
	}

	for _, t := range templates {
		line := fmt.Sprintf("%-24s v%-3d %-32s %d stations, %d agents  %s", t.Name, t.Version, cut(t.Title, 32), t.Stations, t.Agents,
			strings.Join(t.Connectors, ", "))
		out.printf("%s\n", strings.TrimRight(line, " "))
	}

	out.printf("%d templates\n", len(templates))

	return nil
}
