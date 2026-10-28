package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

// TestRun_Golden pins what the CLI prints, and its exit code, for command lines that need no
// goald, database or bucket: the usage, each command group's usage, goal version and an offline
// validate. Other tests pin their output through checkGolden too. go test -run TestRun_Golden -update rewrites testdata/golden.
func TestRun_Golden(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"no-arguments", ""},
		{"help", "help"},
		{"unknown-command", "frobnicate"},
		{"version-extra-argument", "version now"},
		{"validate-fixture", "validate ../../internal/config/testdata/line"},
		{"validate-no-factory", "validate ../../internal/config/testdata"},
		{"validate-no-arguments", "validate"},
		{"factory-help", "factory -h"},
		{"status-help", "status -h"},
		{"up-help", "up -h"},
		{"login-help", "login -h"},
		{"jonah-help", "jonah -h"},
		{"spend-bad-window", "spend --window year"},
	}

	// Each of these runs with no arguments, and its golden file has its name.
	for group := range strings.FieldsSeq("version factory org policy key andon admin catalog connection credential judge knowledge db storage run search eval triggers webhooks") {
		cases = append(cases, struct {
			name string
			line string
		}{group, group})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

			env := map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
			getenv := func(k string) string { return env[k] }

			var stdout, stderr bytes.Buffer

			args := strings.Fields(tc.line)
			code := Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr, getenv, fixedBuildInfo)

			got := fmt.Sprintf("exit %d\n--- stdout\n%s--- stderr\n%s", code, stdout.String(), stderr.String())
			checkGolden(t, tc.name, got)
		})
	}
}

// checkGolden compares got with testdata/golden/<name>.txt, or with -update rewrites it.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", "golden", name+".txt")

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
	if err != nil {
		t.Fatalf("read %s (go test -run '%s' -update writes it): %v", path, t.Name(), err)
	}

	if got != string(want) {
		t.Errorf("%s printed\n%s\nwant (%s)\n%s", t.Name(), got, path, want)
	}
}

// fixedBuildInfo stands in for debug.ReadBuildInfo, so goal version's golden file does not
// depend on the test binary.
func fixedBuildInfo() (*debug.BuildInfo, bool) {
	return &debug.BuildInfo{
		GoVersion: "go1.0-test",
		Main:      debug.Module{Path: "github.com/byFrederick/goal", Version: "v0.0.0-test"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.modified", Value: "true"},
			{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
		},
	}, true
}
