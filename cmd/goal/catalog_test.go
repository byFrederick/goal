package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/builder"
	"github.com/byFrederick/goal/internal/ports"
	"github.com/byFrederick/goal/internal/ports/porttest"
)

func sourcesFile(t *testing.T, body string) string {
	t.Helper()

	p := filepath.Join(t.TempDir(), "sources.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestCatalogSkills_SyncPrintsEachSkillAndFailsOnAny(t *testing.T) {
	t.Parallel()

	commit := strings.Repeat("a", 40)
	src := porttest.NewSkillSource()
	src.Add(ports.SkillRef{Owner: "acme", Repo: "skills", Skill: "lint", Commit: commit}, 0, map[string]string{
		"SKILL.md": "---\ndescription: Lints the code.\n---\n",
	})

	catalog := builder.New(porttest.NewObjectStore(), porttest.NewClock(time.Now()), builder.Config{})

	var buf bytes.Buffer

	err := syncSkills(t.Context(), &printer{w: &buf}, catalog, src, "goal-test",
		syncArgs{file: sourcesFile(t, "- acme/skills@lint\n"), top: 5})
	if err != nil || !strings.Contains(buf.String(), "added     acme/skills/lint") ||
		!strings.Contains(buf.String(), "1 skills in s3://goal-test/catalog/skills/index.json: 1 added, 0 unchanged, 0 failed") {
		t.Fatalf("sync = %v\n%s", err, buf.String())
	}

	buf.Reset()

	err = syncSkills(t.Context(), &printer{w: &buf}, catalog, src, "goal-test",
		syncArgs{file: sourcesFile(t, "- acme/skills@lint\n- acme/skills@missing\n"), top: 5})
	if err == nil || !strings.Contains(buf.String(), "unchanged acme/skills/lint") || !strings.Contains(buf.String(), "failed    acme/skills/missing: ") {
		t.Errorf("a sync with a missing skill = %v\n%s; want a failure line and an error (exit 1)", err, buf.String())
	}

	buf.Reset()

	if err := listSkills(t.Context(), &printer{w: &buf}, catalog, "lint"); err != nil ||
		!strings.Contains(buf.String(), "acme/skills/lint") || !strings.Contains(buf.String(), "aaaaaaa        0  Lints the code.") { // installs come from searches only
		t.Errorf("list = %v\n%s", err, buf.String())
	}
}

func TestCatalogSkills_RefusesBadInput(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		"", "skills", "templates list", "skills sync", "skills sync --top 0 --search x", "skills sync --top 51 --search x",
		"skills sync --file f extra", "skills list a b", "skills remove",
	} {
		args := strings.Fields(line)

		err := catalogCommand(t.Context(), &printer{w: &bytes.Buffer{}}, func(string) string { return "" }, args)
		if !errors.Is(err, errUsage) {
			t.Errorf("goal catalog %v = %v, want a usage error (exit 2)", args, err)
		}
	}

	for _, body := range []string{"acme/skills@lint", "- acme/skills@../x\n", "- acme/skills@lint#abc\n"} {
		if _, err := readSources(sourcesFile(t, body)); err == nil {
			t.Errorf("sources %q were accepted", body)
		}
	}

	if refs, err := readSources(sourcesFile(t, "- acme/skills@lint\n- skills.sh:acme/skills@fmt#"+strings.Repeat("b", 40)+"\n")); err != nil ||
		len(refs) != 2 || refs[1].Commit != strings.Repeat("b", 40) {
		t.Errorf("sources = %+v, %v", refs, err)
	}
}
