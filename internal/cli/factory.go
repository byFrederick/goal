package cli

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
)

// goal factory works on definitions through goald's API (B §9): the definitions live in goald,
// as drafts and published versions, and a working copy is a folder that goal factory pull wrote
// and goal factory push sends back. Writing to the bucket changes nothing goald runs.

// pulledFile is a working copy's record of what it was last pulled or pushed as. It is hidden, so
// it is never pushed.
const pulledFile = ".goal/pulled.json"

// Version selectors goal factory pull takes besides a number.
const (
	selectCurrent = "current"
	selectDraft   = "draft"
)

// pulled is .goal/pulled.json.
type pulled struct {
	// Org and FactoryID say which factory the record is for; a push of the folder to another
	// factory or organization ignores it.
	Org       string `json:"org"`
	FactoryID string `json:"factory_id"`
	// Number is the published version the folder builds on: the version pulled, or the one that
	// was current when a draft was pulled or pushed; 0 before the factory's first publish.
	Number int `json:"number"`
	// Revision is the draft revision pulled or pushed; 0 for a published version.
	Revision int64 `json:"revision"`
	// Manifest is the folder's files as pulled or pushed: path → sha256.
	Manifest map[string]string `json:"manifest"`
}

// factory runs goal factory validate|push|pull|list|publish|versions|restore|archive|delete.
func factory(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	switch cmd, rest := args[0], args[1:]; cmd {
	case "validate":
		return factoryValidate(ctx, out, getenv, rest, "factory validate")
	case "push":
		return factoryPush(ctx, out, getenv, rest)
	case "pull":
		return factoryPull(ctx, out, getenv, rest)
	case "list":
		return factoryList(ctx, out, getenv, rest)
	case "publish":
		return factoryPublish(ctx, out, getenv, rest)
	case "versions":
		return factoryVersions(ctx, out, getenv, rest)
	case "restore":
		return factoryRestore(ctx, out, getenv, rest)
	case "archive":
		return factoryArchive(ctx, out, getenv, rest)
	case "delete":
		return factoryDelete(ctx, out, getenv, rest)
	default:
		return errUsage
	}
}

// validateAlias is goal validate: goal factory validate with --offline on by default.
const validateAlias = "validate"

// factoryValidate runs goal factory validate <dir>... [--offline]: through goald, over the open
// draft for a factory it knows (the folder as overlay) and as a new definition otherwise; with
// --offline the schema and reference checks run locally. As goal validate (name validateAlias)
// it takes the same flags and --offline is the default.
func factoryValidate(ctx context.Context, out *printer, getenv func(string) string, args []string, name string) error {
	fs := newFlags(name)
	offline := fs.Bool("offline", name == validateAlias, "check schema and references locally, without goald")
	org := addOrgFlag(fs)

	dirs, err := parseInterspersed(fs, args)
	if err != nil || len(dirs) == 0 {
		return errUsage
	}

	if *offline {
		return validate(out, dirs)
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	failed := 0

	for _, dir := range dirs {
		ok, err := validateOnline(ctx, out, c, dir)
		if err != nil {
			return err
		}

		if !ok {
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d factories are invalid", failed, len(dirs))
	}

	return nil
}

// validate checks local folders offline (goal factory validate --offline, goal validate):
// config.Check's schema and reference problems, and that no two folders share an id.
func validate(out *printer, dirs []string) error {
	failed := 0
	seen := map[string]string{} // factory id → folder

	for _, dir := range dirs {
		files, err := readFolder(dir)
		if err != nil {
			failed++

			out.printf("✗ %s\n  %v\n", dir, err)

			continue
		}

		f, problems := config.Check(config.Files(files))
		blocking := slices.ContainsFunc(problems, domain.DefinitionProblem.Blocking)

		switch {
		case f == nil || blocking:
			failed++

			out.printf("✗ %s\n", dir)
		case seen[f.Metadata.ID] != "":
			failed++

			out.printf("✗ %s\n  id %s is already used by %s; copy a factory, then give it a new id (uuidgen)\n",
				dir, f.Metadata.ID, seen[f.Metadata.ID])

			continue
		default:
			seen[f.Metadata.ID] = dir
			out.printf("✓ %s (%s) — %d agents, %d stations\n",
				f.Metadata.Name, f.Metadata.ID[:8], len(f.Spec.Agents), len(f.Spec.Board.Stations))
		}

		for _, p := range problems {
			out.printf("%s", problemLine(dir, p.File, p.Line, p.Column, string(p.Severity), p.Code, p.Message))

			for _, f := range p.Fixes {
				out.printf("%s", fixLine(f.Label))
			}
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d factories are invalid", failed, len(dirs))
	}

	return nil
}

// validateOnline validates one folder through goald and prints the result; ok is false when it
// has error problems.
func validateOnline(ctx context.Context, out *printer, c orgClient, dir string) (bool, error) {
	files, err := readFolder(dir)
	if err != nil {
		out.printf("✗ %s\n  %v\n", dir, err)

		return false, nil
	}

	fm, err := fileMap(files)
	if err != nil {
		return false, err
	}

	id, name, idErr := identity(dir, files)

	v, err := validateFiles(ctx, c, fm, id, idErr == nil)
	if err != nil {
		return false, err
	}

	if v.Ok && idErr == nil {
		out.printf("✓ %s (%s) — %d agents, %d stations\n", name, id[:8], v.Counts.Agents, v.Counts.Stations)
	} else {
		out.printf("✗ %s\n", dir)
	}

	printProblems(out, dir, v.Problems)

	return v.Ok, nil
}

// validateFiles validates a folder's files: over the open draft of the factory with id when
// there is one (hasID says the folder names an id), else as a definition of their own.
func validateFiles(ctx context.Context, c orgClient, fm gen.FileMap, id string, hasID bool) (*gen.Validation, error) {
	if hasID {
		f, found, err := lookupFactory(ctx, c, id)
		if err != nil {
			return nil, err
		}

		if found && f.Draft != nil {
			res, err := c.ValidateDraftWithResponse(ctx, c.org, f.Id, gen.ValidateDraftRequest{Overlay: &fm})
			if err != nil {
				return nil, apiDown(err)
			}

			return answered(res.JSON200, res.HTTPResponse, res.ApplicationproblemJSONDefault)
		}
	}

	res, err := c.ValidateDefinitionWithResponse(ctx, c.org, gen.DefinitionFiles{Files: fm})
	if err != nil {
		return nil, apiDown(err)
	}

	return answered(res.JSON200, res.HTTPResponse, res.ApplicationproblemJSONDefault)
}

// answered is goald's answer, or the problem it answered instead.
func answered[T any](v *T, res *http.Response, p *gen.Problem) (*T, error) {
	if v == nil {
		return nil, problem(res, p)
	}

	return v, nil
}

// pushArgs are the flags of goal factory push.
type pushArgs struct {
	dir                          string
	publish, replaceDraft, force bool
	message, org                 string
	moves                        map[string]string
}

func parsePushArgs(args []string) (pushArgs, error) {
	a := pushArgs{}

	fs := newFlags("factory push")
	fs.BoolVar(&a.publish, "publish", false, "validate and publish the draft")
	fs.BoolVar(&a.replaceDraft, "replace-draft", false, "archive an open draft this folder was not pulled from")
	fs.BoolVar(&a.force, "force", false, "push although a version was published after the pull")
	fs.StringVar(&a.message, "message", "", "the publish note")
	a.moves = addMovesFlag(fs)
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 || (a.message != "" && !a.publish) {
		return pushArgs{}, errUsage
	}

	a.dir, a.org = pos[0], *org

	return a, nil
}

// factoryPush runs goal factory push <dir>: the folder becomes the factory's draft (PUT
// …/draft/tree at the revision it read), creating the factory with the folder's metadata.id when
// the organization has none. It refuses an open draft with changes the folder was not pulled
// from (--replace-draft archives it) and a version published after the pull .goal/pulled.json
// records (--force). --publish then validates and publishes the draft.
func factoryPush(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	a, err := parsePushArgs(args)
	if err != nil {
		return err
	}

	files, err := readFolder(a.dir)
	if err != nil {
		return err
	}

	id, _, err := identity(a.dir, files)
	if err != nil {
		return err
	}

	c, err := connect(ctx, getenv, a.org)
	if err != nil {
		return err
	}

	f, revision, before, err := pushTarget(ctx, c, a, id, files)
	if err != nil {
		return err
	}

	saved, err := replaceTree(ctx, c, f, files, revision)
	if err != nil {
		return err
	}

	local := manifestOf(files)
	printChanges(out, before, local)

	return pushed(ctx, out, c, a, f, saved, local)
}

// pushTarget is the factory a push writes, the revision of the draft it replaces and that
// draft's files: the factory with the folder's id, after the stale and draft checks, or a new
// one from the folder when the organization has none.
func pushTarget(ctx context.Context, c orgClient, a pushArgs, id string, files map[string][]byte) (
	*gen.Factory, int64, map[string]string, error,
) {
	f, found, err := lookupFactory(ctx, c, id)
	if err != nil {
		return nil, 0, nil, err
	}

	if !found {
		f, revision, err := importFactory(ctx, c, files)

		return f, revision, nil, err
	}

	rec, err := readPulled(a.dir)
	if err != nil {
		return nil, 0, nil, err
	}

	if rec != nil && (rec.FactoryID != f.Id || rec.Org != f.OrgId) {
		rec = nil
	}

	if err := checkStale(ctx, c, a.dir, f, rec, a.force); err != nil {
		return nil, 0, nil, err
	}

	revision, before, err := draftFor(ctx, c, f, rec, a.replaceDraft)

	return f, revision, before, err
}

// pushed reports a push saved as the draft, publishes it with --publish and records what the
// folder now is.
func pushed(ctx context.Context, out *printer, c orgClient, a pushArgs, f *gen.Factory, saved *gen.SaveResult, local map[string]string) error {
	current := 0
	if f.Current != nil {
		current = f.Current.Number
	}

	out.printf("✓ pushed %s into the draft of %s (revision %d)\n", a.dir, f.Name, saved.Revision)

	rec := &pulled{Org: f.OrgId, FactoryID: f.Id, Number: current, Revision: saved.Revision, Manifest: local}

	if !a.publish {
		if saved.ProblemCount > 0 {
			out.printf("  ⚠ %d problems; `goal factory validate %s` lists them\n", saved.ProblemCount, a.dir)
		}

		out.printf("  nothing runs it yet: publish it with `goal factory publish %s` (or push with --publish)\n", f.Name)

		return writePulled(a.dir, rec)
	}

	v, err := publish(ctx, out, c, a.dir, f, saved.Revision, a.message, a.moves)
	if err != nil {
		return errors.Join(err, writePulled(a.dir, rec))
	}

	out.printf("✓ published %s v%d; it runs now\n", f.Name, deref(v.Number))

	rec.Number, rec.Revision = deref(v.Number), 0

	return writePulled(a.dir, rec)
}

// checkStale refuses a push when a version was published after the pull the folder records,
// unless that version holds exactly the folder's last pull or push (it was pushed, then
// published in the builder) or --force says to push over it.
func checkStale(ctx context.Context, c orgClient, dir string, f *gen.Factory, rec *pulled, force bool) error {
	if rec == nil || force || f.Current == nil || f.Current.Number == rec.Number {
		return nil
	}

	cur, err := versionManifest(ctx, c, f.Id, strconv.Itoa(f.Current.Number))
	if err != nil {
		return err
	}

	if maps.Equal(cur, rec.Manifest) {
		return nil
	}

	pulledAs := "before its first publish"
	if rec.Number > 0 {
		pulledAs = fmt.Sprintf("from v%d", rec.Number)
	}

	return fmt.Errorf("%s v%d was published %s by %s, but %s was pulled %s; pull v%d into a new folder and carry "+
		"your changes over, or pass --force to make this folder the draft anyway", f.Name, f.Current.Number,
		f.Current.PublishedAt.Local().Format(time.DateTime), f.Current.PublishedBy.Name, dir, pulledAs, f.Current.Number)
}

// draftFor returns the revision of the draft a push replaces and that draft's files: the open
// draft when it holds the folder's last pull or push or nothing beyond its base, else a new
// draft on the current version (when none is open, or --replace-draft archives the open one).
func draftFor(ctx context.Context, c orgClient, f *gen.Factory, rec *pulled, replace bool) (int64, map[string]string, error) {
	if f.Draft != nil && !replace {
		res, err := c.GetDraftWithResponse(ctx, c.org, f.Id)
		if err != nil {
			return 0, nil, apiDown(err)
		}

		d := res.JSON200
		if d == nil {
			return 0, nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
		}

		files, err := draftManifest(ctx, c, f.Id)
		if err != nil {
			return 0, nil, err
		}

		if d.ChangeCount == 0 || (rec != nil && maps.Equal(files, rec.Manifest)) {
			return d.Revision, files, nil
		}

		return 0, nil, fmt.Errorf("%s has an open draft with %d changes (revision %d, saved %s by %s) this folder was not "+
			"pulled from; pull it with `goal factory pull %s <dir> --version draft` and carry your changes over, "+
			"or pass --replace-draft to archive it", f.Name, d.ChangeCount, d.Revision,
			d.UpdatedAt.Local().Format(time.DateTime), d.UpdatedBy.Name, f.Name)
	}

	res, err := c.OpenDraftWithResponse(ctx, c.org, f.Id, gen.OpenDraftRequest{Replace: &replace})
	if err != nil {
		return 0, nil, apiDown(err)
	}

	d := cmp.Or(res.JSON201, res.JSON200)
	if d == nil {
		return 0, nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	files, err := draftManifest(ctx, c, f.Id)

	return d.Revision, files, err
}

// importFactory creates the factory a push names that the organization lacks, keeping the
// folder's metadata.id, and returns it with its first draft's revision.
func importFactory(ctx context.Context, c orgClient, files map[string][]byte) (*gen.Factory, int64, error) {
	fm, err := fileMap(files)
	if err != nil {
		return nil, 0, err
	}

	var start gen.FactoryStart
	if err := start.FromImportStart(gen.ImportStart{Kind: gen.ImportStartKindImport, Files: fm, KeepId: new(true)}); err != nil {
		return nil, 0, err
	}

	res, err := c.CreateFactoryWithResponse(ctx, c.org, gen.NewFactoryRequest{Start: start})
	if err != nil {
		return nil, 0, apiDown(err)
	}

	f := res.JSON201
	if f == nil {
		return nil, 0, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	if f.Draft == nil {
		return nil, 0, fmt.Errorf("goald created %s without a draft", f.Name)
	}

	return f, f.Draft.Revision, nil
}

// replaceTree makes files the draft's whole tree, refused if the draft moved past revision.
func replaceTree(ctx context.Context, c orgClient, f *gen.Factory, files map[string][]byte, revision int64) (*gen.SaveResult, error) {
	fm, err := fileMap(files)
	if err != nil {
		return nil, err
	}

	res, err := c.ReplaceDraftTreeWithResponse(ctx, c.org, f.Id, gen.DraftTree{Files: fm, ExpectedRevision: &revision})
	if err != nil {
		return nil, apiDown(err)
	}

	if res.JSON200 != nil {
		return res.JSON200, nil
	}

	if problemCode(res.ApplicationproblemJSONDefault) == authoring.CodeDraftConflict {
		return nil, fmt.Errorf("the draft of %s changed while pushing (someone saved it); nothing was pushed, push again", f.Name)
	}

	return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
}

// publish publishes the factory's draft at revision, expecting the version current when f was
// read. Problems that block it are printed, located in root.
func publish(
	ctx context.Context, out *printer, c orgClient, root string, f *gen.Factory, revision int64, message string, moves map[string]string,
) (*gen.Version, error) {
	body := gen.PublishRequest{Revision: revision}
	if f.Current != nil {
		body.ExpectedCurrentVersionId = &f.Current.Id
	}

	if message != "" {
		body.Message = &message
	}

	if len(moves) > 0 {
		body.StationMoves = new(gen.StationMoves(moves))
	}

	res, err := c.PublishDraftWithResponse(ctx, c.org, f.Id, body)
	if err != nil {
		return nil, apiDown(err)
	}

	if res.JSON201 != nil {
		return res.JSON201, nil
	}

	return nil, refusal(out, root, f, revision, res.HTTPResponse, res.ApplicationproblemJSONDefault)
}

// refusal explains a publish or restore goald refused.
func refusal(out *printer, root string, f *gen.Factory, revision int64, res *http.Response, p *gen.Problem) error {
	if err := refusedAccess(res, p); err != nil || p == nil {
		return problem(res, p)
	}

	switch problemCode(p) {
	case authoring.CodeDefinitionInvalid:
		var problems []gen.DefinitionProblem
		if p.Problems != nil {
			problems = *p.Problems
		}

		printProblems(out, root, problems)

		return fmt.Errorf("%s is not published: the draft (revision %d) has errors to fix first", f.Name, revision)
	case authoring.CodeBaseMoved:
		return fmt.Errorf("another version of %s was published meanwhile; nothing changed: %s", f.Name, deref(p.Detail))
	case authoring.CodeItemsAtRemoved:
		if p.Items != nil {
			for _, it := range p.Items.Unmapped {
				out.printf("  %s#%d is at %s, which the new version drops\n", f.Name, it.Number, it.Station)
			}

			for _, it := range p.Items.InProgress {
				out.printf("  %s#%d is being worked at %s, which the new version drops\n", f.Name, it.Number, it.Station)
			}
		}

		return errors.New("open items sit at stations the new version drops: move them with --move <station>=<station> " +
			"(items being worked must finish or be stopped first)")
	}

	return errors.New(problemText(p))
}

// pullArgs are the flags of goal factory pull.
type pullArgs struct {
	ref, dir, version, org string
}

// factoryPull runs goal factory pull <factory> <dir> [--version N|current|draft]: a version's
// files (the current one by default, the draft before the first publish) into a new folder,
// with .goal/pulled.json recording what they are.
func factoryPull(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	a, err := parsePullArgs(args)
	if err != nil {
		return err
	}

	if err := mustBeEmpty(a.dir); err != nil {
		return err
	}

	c, err := connect(ctx, getenv, a.org)
	if err != nil {
		return err
	}

	f, err := getFactory(ctx, c, a.ref)
	if err != nil {
		return err
	}

	sel, err := pullSelector(f, a.version)
	if err != nil {
		return err
	}

	v, files, err := readVersion(ctx, c, f, sel)
	if err != nil {
		return err
	}

	if err := writeFolder(a.dir, files); err != nil {
		return err
	}

	rec, what := pulledRecord(f, v, files)

	if err := writePulled(a.dir, rec); err != nil {
		return err
	}

	for _, p := range slices.Sorted(maps.Keys(files)) {
		out.printf("  %-60s %s\n", p, rec.Manifest[p][:12])
	}

	out.printf("✓ pulled %s (%d files) into %s; edit it, then `goal factory push %s`\n", what, len(files), a.dir, a.dir)

	return nil
}

func parsePullArgs(args []string) (pullArgs, error) {
	fs := newFlags("factory pull")
	version := fs.String("version", "", "N, current or draft")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 2 {
		return pullArgs{}, errUsage
	}

	a := pullArgs{ref: pos[0], dir: pos[1], version: *version, org: *org}
	if _, err := strconv.Atoi(a.version); err != nil && a.version != "" && a.version != selectCurrent && a.version != selectDraft {
		return pullArgs{}, errUsage
	}

	return a, nil
}

// pulledRecord is the record of a pull of v, and what was pulled as the output names it.
func pulledRecord(f *gen.Factory, v *gen.VersionDetail, files map[string][]byte) (*pulled, string) {
	rec := &pulled{Org: f.OrgId, FactoryID: f.Id, Number: deref(v.Number), Manifest: manifestOf(files)}

	if v.State != gen.VersionStateDraft {
		return rec, fmt.Sprintf("%s v%d", f.Name, rec.Number)
	}

	rec.Revision = v.Revision
	if f.Current != nil {
		rec.Number = f.Current.Number
	}

	return rec, fmt.Sprintf("the draft of %s at revision %d", f.Name, v.Revision)
}

// pullSelector is the version a pull reads: a published one by number, so the files of one pull
// all come from the same version, or the draft.
func pullSelector(f *gen.Factory, version string) (string, error) {
	if version == "" {
		version = selectCurrent
		if f.Current == nil {
			version = selectDraft
		}
	}

	switch {
	case version == selectCurrent && f.Current == nil:
		return "", fmt.Errorf("%s was never published; pull its draft with --version draft", f.Name)
	case version == selectCurrent:
		return strconv.Itoa(f.Current.Number), nil
	case version == selectDraft && f.Draft == nil:
		return "", fmt.Errorf("%s has no open draft; pull a version instead", f.Name)
	}

	return version, nil
}

// readVersion reads every file of the version sel names, checking each against its sha256.
func readVersion(ctx context.Context, c orgClient, f *gen.Factory, sel string) (*gen.VersionDetail, map[string][]byte, error) {
	res, err := c.GetVersionWithResponse(ctx, c.org, f.Id, sel)
	if err != nil {
		return nil, nil, apiDown(err)
	}

	v := res.JSON200
	if v == nil {
		return nil, nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	files := make(map[string][]byte, len(v.Files))

	for _, fi := range v.Files {
		r, err := c.GetVersionFileWithResponse(ctx, c.org, f.Id, sel, fi.Path)
		if err != nil {
			return nil, nil, apiDown(err)
		}

		if r.StatusCode() != 200 {
			return nil, nil, problem(r.HTTPResponse, r.ApplicationproblemJSONDefault)
		}

		if string(domain.HashBlob(r.Body)) != fi.Sha256 {
			return nil, nil, fmt.Errorf("%s of %s changed while it was read (someone saved the draft); try again", fi.Path, f.Name)
		}

		files[fi.Path] = r.Body
	}

	return v, files, nil
}

// factoryList runs goal factory list [--validate] [--archived]: every factory of the organization
// with the version that runs, its open draft and whether it is paused; --validate checks each
// current version and exits 1 when one has errors; --archived lists only the archived factories.
func factoryList(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("factory list")
	check := fs.Bool("validate", false, "validate each factory's current version")
	archived := fs.Bool("archived", false, "list only the archived factories")
	org := addOrgFlag(fs)

	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) != 0 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	params, kind := listScope(*archived)

	res, err := c.ListFactoriesWithResponse(ctx, c.org, params)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	invalid := 0

	for _, f := range res.JSON200.Factories {
		mark, problems := " ", []gen.DefinitionProblem(nil)

		if *check && f.Current != nil {
			v, err := validateVersion(ctx, c, &f)
			if err != nil {
				return err
			}

			mark, problems = "✓", v.Problems
			if !v.Ok {
				mark = "✗"
				invalid++
			}
		}

		out.printf("%s %-24s %s  %s\n", mark, f.Name, f.Id, describeFactory(f))
		printProblems(out, "", problems)
	}

	out.printf("%d %s in %s\n", len(res.JSON200.Factories), kind, c.org)

	if invalid > 0 {
		return fmt.Errorf("%d factories' current versions are invalid", invalid)
	}

	return nil
}

// listScope is goal factory list's request and what it lists: the archived factories or the others.
func listScope(archived bool) (*gen.ListFactoriesParams, string) {
	if archived {
		return &gen.ListFactoriesParams{Archived: new(true)}, "archived factories"
	}

	return nil, "factories"
}

// describeFactory is a factory's versions as goal factory list shows them: "v3 + draft r4 · paused",
// then " · archived" for an archived one.
func describeFactory(f gen.Factory) string {
	s := "never published"
	if f.Current != nil {
		s = "v" + strconv.Itoa(f.Current.Number)
	}

	if f.Draft != nil {
		s += fmt.Sprintf(" + draft r%d", f.Draft.Revision)
	}

	if f.Paused {
		s += " · paused"
	}

	if f.ArchivedAt != nil {
		s += " · archived"
	}

	return s
}

// validateVersion validates a factory's current version as a definition of its own.
func validateVersion(ctx context.Context, c orgClient, f *gen.Factory) (*gen.Validation, error) {
	_, files, err := readVersion(ctx, c, f, strconv.Itoa(f.Current.Number))
	if err != nil {
		return nil, err
	}

	fm, err := fileMap(files)
	if err != nil {
		return nil, err
	}

	res, err := c.ValidateDefinitionWithResponse(ctx, c.org, gen.DefinitionFiles{Files: fm})
	if err != nil {
		return nil, apiDown(err)
	}

	if res.JSON200 == nil {
		return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	return res.JSON200, nil
}

// factoryPublish runs goal factory publish <factory> [--message M] [--move S=T]...: validate the
// open draft, print its problems and publish it when it has no errors.
func factoryPublish(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("factory publish")
	message := fs.String("message", "", "the publish note")
	moves := addMovesFlag(fs)
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	f, err := getFactory(ctx, c, pos[0])
	if err != nil {
		return err
	}

	if f.Draft == nil {
		return fmt.Errorf("%s has no open draft; push a folder with `goal factory push <dir>` first", f.Name)
	}

	body := gen.ValidateDraftRequest{}
	if len(moves) > 0 {
		body.StationMoves = new(gen.StationMoves(moves))
	}

	res, err := c.ValidateDraftWithResponse(ctx, c.org, f.Id, body)
	if err != nil {
		return apiDown(err)
	}

	val := res.JSON200
	if val == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	printProblems(out, "", val.Problems)

	if !val.Ok {
		return fmt.Errorf("%s is not published: the draft (revision %d) has errors to fix first", f.Name, val.Revision)
	}

	v, err := publish(ctx, out, c, "", f, val.Revision, *message, moves)
	if err != nil {
		return err
	}

	out.printf("✓ published %s v%d; it runs now\n", f.Name, deref(v.Number))

	return nil
}

// factoryVersions runs goal factory versions <factory>: its published versions, newest first.
func factoryVersions(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("factory versions")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	f, err := getFactory(ctx, c, pos[0])
	if err != nil {
		return err
	}

	params := &gen.ListVersionsParams{Limit: new(50)}

	for {
		res, err := c.ListVersionsWithResponse(ctx, c.org, f.Id, params)
		if err != nil {
			return apiDown(err)
		}

		page := res.JSON200
		if page == nil {
			return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
		}

		for _, v := range page.Versions {
			mark := " "
			if v.Current {
				mark = "*"
			}

			when, by := "", ""
			if v.PublishedAt != nil {
				when = v.PublishedAt.Local().Format("2006-01-02 15:04")
			}

			if v.PublishedBy != nil {
				by = v.PublishedBy.Name
			}

			out.printf("%s v%-4d %-16s %-16s %s\n", mark, deref(v.Number), when, cut(by, 16), oneLine(v.Message))
		}

		if page.NextBefore == nil {
			return nil
		}

		params.Before = page.NextBefore
	}
}

// factoryRestore runs goal factory restore <factory> <N> [--move S=T]...: version N runs again,
// expecting the version it read as current. With the factory alone, goal factory restore
// <factory> restores an archived factory instead (factoryUnarchive).
func factoryRestore(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("factory restore")
	moves := addMovesFlag(fs)
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err == nil && len(pos) == 1 && len(moves) == 0 {
		return factoryUnarchive(ctx, out, getenv, *org, pos[0])
	}

	if err != nil || len(pos) != 2 {
		return errUsage
	}

	n, err := strconv.Atoi(pos[1])
	if err != nil || n < 1 {
		return errUsage
	}

	return restoreVersion(ctx, out, getenv, *org, pos[0], n, moves)
}

// restoreVersion is goal factory restore <factory> <N>.
func restoreVersion(ctx context.Context, out *printer, getenv func(string) string, org, ref string, n int,
	moves map[string]string,
) error {
	c, err := connect(ctx, getenv, org)
	if err != nil {
		return err
	}

	f, err := getFactory(ctx, c, ref)
	if err != nil {
		return err
	}

	switch {
	case f.Current == nil:
		return fmt.Errorf("%s was never published; there is nothing to restore", f.Name)
	case f.Current.Number == n:
		out.printf("✓ %s v%d runs already\n", f.Name, n)

		return nil
	}

	body := gen.RestoreRequest{ExpectedCurrentVersionId: f.Current.Id}
	if len(moves) > 0 {
		body.StationMoves = new(gen.StationMoves(moves))
	}

	res, err := c.RestoreVersionWithResponse(ctx, c.org, f.Id, strconv.Itoa(n), body)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return refusal(out, "", f, 0, res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ %s runs v%d again (it ran v%d)\n", f.Name, n, f.Current.Number)

	return nil
}

// factoryArchive runs goal factory archive <factory>: retire a paused, idle factory, keeping its
// history; goald refuses a running one ("Pause the factory first") or one whose items are being
// worked.
func factoryArchive(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("factory archive")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	f, err := getFactory(ctx, c, pos[0])
	if err != nil {
		return err
	}

	res, err := c.ArchiveFactoryWithResponse(ctx, c.org, f.Id)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ archived %s (%s); `goal factory restore %s` brings it back\n", res.JSON200.Name, res.JSON200.Id, res.JSON200.Name)

	return nil
}

// factoryUnarchive is goal factory restore <factory>: the archived factory is listed again, still
// paused.
func factoryUnarchive(ctx context.Context, out *printer, getenv func(string) string, org, ref string) error {
	c, err := connect(ctx, getenv, org)
	if err != nil {
		return err
	}

	f, err := getFactory(ctx, c, ref)
	if err != nil {
		return err
	}

	res, err := c.RestoreFactoryWithResponse(ctx, c.org, f.Id)
	if err != nil {
		return apiDown(err)
	}

	if res.JSON200 == nil {
		return problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	out.printf("✓ restored %s (%s); it is still paused and takes work once it is resumed\n", res.JSON200.Name, res.JSON200.Id)

	return nil
}

// factoryDelete runs goal factory delete <factory> [--yes]: delete an archived factory for good,
// with its items, runs, events and versions, after the person types its name (--yes: no prompt).
// A factory never published is deleted with its draft, archived or not.
func factoryDelete(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	fs := newFlags("factory delete")
	yes := fs.Bool("yes", false, "delete without asking to type the name")
	org := addOrgFlag(fs)

	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		return errUsage
	}

	c, err := connect(ctx, getenv, *org)
	if err != nil {
		return err
	}

	f, err := getFactory(ctx, c, pos[0])
	if err != nil {
		return err
	}

	if f.Current != nil && f.ArchivedAt == nil {
		return notArchived(f)
	}

	if !*yes {
		out.printf("This deletes %s (%s) for good, with its items, runs, events and versions. Type its name to confirm: ", f.Name, f.Id)

		confirm, err := readLine(out.in)
		if err != nil {
			return err
		}

		if confirm != f.Name {
			return fmt.Errorf("%q is not %q; nothing was deleted", confirm, f.Name)
		}
	}

	if err := deleteRequest(ctx, c, f); err != nil {
		return err
	}

	out.printf("✓ deleted %s (%s)\n", f.Name, f.Id)

	return nil
}

// deleteRequest deletes f: purgeFactory once it was published, deleteFactory before.
func deleteRequest(ctx context.Context, c orgClient, f *gen.Factory) error {
	var (
		res *http.Response
		p   *gen.Problem
	)

	if f.Current != nil {
		r, err := c.PurgeFactoryWithResponse(ctx, c.org, f.Id)
		if err != nil {
			return apiDown(err)
		}

		res, p = r.HTTPResponse, r.ApplicationproblemJSONDefault
	} else {
		r, err := c.DeleteFactoryWithResponse(ctx, c.org, f.Id)
		if err != nil {
			return apiDown(err)
		}

		res, p = r.HTTPResponse, r.ApplicationproblemJSONDefault
	}

	switch {
	case res.StatusCode == http.StatusNoContent:
		return nil
	case problemCode(p) == authoring.CodeFactoryNotArchived:
		return notArchived(f)
	default:
		return problem(res, p)
	}
}

// notArchived is goal factory delete's refusal of a published factory that is not archived.
func notArchived(f *gen.Factory) error {
	return fmt.Errorf("%s is not archived; archive it first with `goal factory archive %s`", f.Name, f.Name)
}

// addMovesFlag adds --move <station>=<station>, repeatable: where the open items of a station
// the new version drops go.
func addMovesFlag(fs *flag.FlagSet) map[string]string {
	moves := map[string]string{}

	fs.Func("move", "where the open items of a dropped station go: <station>=<station>", func(v string) error {
		from, to, ok := strings.Cut(v, "=")
		if !ok || from == "" || to == "" {
			return errors.New("--move is <station>=<station>")
		}

		moves[from] = to

		return nil
	})

	return moves
}

// lookupFactory reads a factory of the organization by id or name; found is false when it has
// none.
func lookupFactory(ctx context.Context, c orgClient, ref string) (*gen.Factory, bool, error) {
	res, err := c.GetFactoryWithResponse(ctx, c.org, ref)
	if err != nil {
		return nil, false, apiDown(err)
	}

	if res.JSON200 != nil {
		return res.JSON200, true, nil
	}

	if problemCode(res.ApplicationproblemJSONDefault) == authoring.CodeFactoryNotFound {
		return nil, false, nil
	}

	return nil, false, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
}

// getFactory reads a factory the command names.
func getFactory(ctx context.Context, c orgClient, ref string) (*gen.Factory, error) {
	f, found, err := lookupFactory(ctx, c, ref)
	if err != nil {
		return nil, err
	}

	if !found {
		return nil, fmt.Errorf("%s has no factory %q; `goal factory list` shows them", c.org, ref)
	}

	return f, nil
}

// draftManifest is the open draft's files: path → sha256.
func draftManifest(ctx context.Context, c orgClient, factoryID string) (map[string]string, error) {
	res, err := c.ListDraftFilesWithResponse(ctx, c.org, factoryID)
	if err != nil {
		return nil, apiDown(err)
	}

	if res.JSON200 == nil {
		return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	return infoManifest(res.JSON200.Files), nil
}

// versionManifest is a version's files: path → sha256.
func versionManifest(ctx context.Context, c orgClient, factoryID, sel string) (map[string]string, error) {
	res, err := c.GetVersionWithResponse(ctx, c.org, factoryID, sel)
	if err != nil {
		return nil, apiDown(err)
	}

	if res.JSON200 == nil {
		return nil, problem(res.HTTPResponse, res.ApplicationproblemJSONDefault)
	}

	return infoManifest(res.JSON200.Files), nil
}

func infoManifest(files []gen.FileInfo) map[string]string {
	m := make(map[string]string, len(files))
	for _, f := range files {
		m[f.Path] = f.Sha256
	}

	return m
}

// manifestOf is files' manifest: path → sha256.
func manifestOf(files map[string][]byte) map[string]string {
	m := make(map[string]string, len(files))
	for p, b := range files {
		m[p] = string(domain.HashBlob(b))
	}

	return m
}

// printChanges prints what a push changed from before (the draft it replaced) to after.
func printChanges(out *printer, before, after map[string]string) {
	unchanged := 0

	for _, p := range slices.Sorted(maps.Keys(after)) {
		old, had := before[p]

		switch {
		case !had:
			out.printf("  added     %s\n", p)
		case old != after[p]:
			out.printf("  updated   %s\n", p)
		default:
			unchanged++
		}
	}

	for _, p := range slices.Sorted(maps.Keys(before)) {
		if _, kept := after[p]; !kept {
			out.printf("  deleted   %s\n", p)
		}
	}

	if unchanged > 0 {
		out.printf("  unchanged %d files\n", unchanged)
	}
}

// printProblems prints validation problems, their paths in root, each with its quick fixes.
func printProblems(out *printer, root string, problems []gen.DefinitionProblem) {
	for _, p := range problems {
		out.printf("%s", problemLine(root, deref(p.File), p.Line, p.Column, string(p.Severity), p.Code, p.Message))

		for _, f := range p.Fixes {
			out.printf("%s", fixLine(f.Label))
		}
	}
}

// fixLine is a problem's quick fix as the factory commands print it, under the problem: what the
// builder's fix button does, for whoever edits the YAML.
func fixLine(label string) string {
	return "      fix: " + label + "\n"
}

// problemLine is one problem as the factory commands print it: path:line:col severity code
// message, the path in root.
func problemLine(root, file string, line, col int, severity, code, message string) string {
	where := root
	if file != "" {
		where = filepath.Join(root, filepath.FromSlash(file))
	}

	if where != "" && line > 0 {
		where = fmt.Sprintf("%s:%d:%d", where, line, col)
	}

	if where != "" {
		where += " "
	}

	return fmt.Sprintf("  %s%s %s %s\n", where, severity, code, message)
}

func problemCode(p *gen.Problem) string {
	if p == nil {
		return ""
	}

	return deref(p.Code)
}

// identity reads the folder's metadata.id and metadata.name from its factory.yaml.
func identity(dir string, files map[string][]byte) (string, string, error) {
	raw, ok := files[config.FileName]
	if !ok {
		return "", "", fmt.Errorf("%s has no %s", dir, config.FileName)
	}

	var doc struct {
		Metadata struct {
			ID   string `yaml:"id"`
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}

	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return "", "", fmt.Errorf("%s: %w", filepath.Join(dir, config.FileName), err)
	}

	id, err := uuid.Parse(doc.Metadata.ID)
	if err != nil {
		return "", "", fmt.Errorf("%s: metadata.id %q is not a UUID; give the factory one (uuidgen)",
			filepath.Join(dir, config.FileName), doc.Metadata.ID)
	}

	return id.String(), doc.Metadata.Name, nil
}

// readFolder reads a working copy: every file by slash path, without hidden files and folders
// (.goal/, .git, .DS_Store) and knowledge/, which are not part of a definition.
func readFolder(dir string) (map[string][]byte, error) {
	files := map[string][]byte{}
	root := os.DirFS(dir)

	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case p == ".":
			return nil
		case strings.HasPrefix(d.Name(), ".") || p == "knowledge":
			if d.IsDir() {
				return fs.SkipDir
			}

			return nil
		case d.IsDir():
			return nil
		}

		b, err := fs.ReadFile(root, p)
		if err != nil {
			return err
		}

		files[p] = b

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	return files, nil
}

// writeFolder writes files into dir, which mustBeEmpty checked.
func writeFolder(dir string, files map[string][]byte) error {
	for p, b := range files {
		if !fs.ValidPath(p) || p == "." {
			return fmt.Errorf("goald sent a file at %q, which is not a relative path", p)
		}

		target := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}

		if err := os.WriteFile(target, b, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
	}

	return nil
}

// mustBeEmpty refuses to pull into a folder with anything in it, so no local edit is ever
// overwritten.
func mustBeEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("pull into %s: %w", dir, err)
	}

	if len(entries) > 0 {
		return fmt.Errorf("pull into %s: the folder is not empty; pull into a new folder", dir)
	}

	return nil
}

// fileMap is files as the API sends them: text when it is UTF-8 without NUL bytes, else base64.
func fileMap(files map[string][]byte) (gen.FileMap, error) {
	m := make(gen.FileMap, len(files))

	for p, b := range files {
		var (
			c   gen.FileContent
			err error
		)

		if utf8.Valid(b) && !bytes.ContainsRune(b, 0) {
			err = c.FromTextFile(gen.TextFile{Text: string(b)})
		} else {
			err = c.FromBase64File(gen.Base64File{Base64: b})
		}

		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", p, err)
		}

		m[p] = c
	}

	return m, nil
}

// readPulled reads the folder's .goal/pulled.json; nil when it has none.
func readPulled(dir string) (*pulled, error) {
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(pulledFile))) //nolint:gosec // the working copy the user names
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil //nolint:nilnil // no record is not an error
	}

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path.Join(dir, pulledFile), err)
	}

	var rec pulled
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (delete it to push without the check): %w", path.Join(dir, pulledFile), err)
	}

	return &rec, nil
}

// writePulled records what the folder now is.
func writePulled(dir string, rec *pulled) error {
	target := filepath.Join(dir, filepath.FromSlash(pulledFile))

	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}

	if err := os.WriteFile(target, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}

	return nil
}
