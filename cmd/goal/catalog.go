package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/adapters/envsecrets"
	"github.com/byFrederick/goal/internal/adapters/skillssh"
	"github.com/byFrederick/goal/internal/adapters/sysclock"
	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/ports"
)

const (
	// maxSourcesBytes bounds the sources file of goal catalog skills sync.
	maxSourcesBytes = 1 << 20

	subSync = "sync"
	subList = "list"
)

// syncArgs are the flags of goal catalog skills sync.
type syncArgs struct {
	file   string
	search []string
	top    int
}

// catalogCommand runs goal catalog skills sync|list, the operator's commands over the bucket's
// skills catalog (catalog/skills/). They use the bucket directly, like goal storage init, and
// act in no organization: the catalog is the install's.
func catalogCommand(ctx context.Context, out *printer, getenv func(string) string, args []string) error {
	if len(args) < 2 || args[0] != "skills" {
		return errUsage
	}

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
// GOAL_GITHUB_API_URL), with the optional tokens GOAL_SKILLS_DIRECTORY_TOKEN and
// GOAL_GITHUB_TOKEN resolved through the Secrets port.
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

	ghToken, err := token("GOAL_GITHUB_TOKEN")
	if err != nil {
		return nil, err
	}

	return skillssh.New(skillssh.Config{
		DirectoryURL: getenv("GOAL_SKILLS_DIRECTORY_URL"), DirectoryToken: dirToken,
		GitHubAPIURL: getenv("GOAL_GITHUB_API_URL"), GitHubToken: ghToken,
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
