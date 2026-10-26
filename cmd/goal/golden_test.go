package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

// TestRun_Golden pins what the CLI prints, and its exit code, for command lines that need no
// goald, database or bucket: the usage, each command group's usage, goal version and an offline
// validate. go test -run TestRun_Golden -update rewrites testdata/golden.
func TestRun_Golden(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no-arguments", nil},
		{"help", []string{"help"}},
		{"unknown-command", []string{"frobnicate"}},
		{"version", []string{"version"}},
		{"version-extra-argument", []string{"version", "now"}},
		{"validate-fixture", []string{"validate", "../../internal/config/testdata/line"}},
		{"validate-no-factory", []string{"validate", "../../internal/config/testdata"}},
		{"validate-no-arguments", []string{"validate"}},
		{"factory", []string{"factory"}},
		{"factory-help", []string{"factory", "-h"}},
		{"org", []string{"org"}},
		{"key", []string{"key"}},
		{"andon", []string{"andon"}},
		{"admin", []string{"admin"}},
		{"catalog", []string{"catalog"}},
		{"connection", []string{"connection"}},
		{"knowledge", []string{"knowledge"}},
		{"db", []string{"db"}},
		{"storage", []string{"storage"}},
		{"status-help", []string{"status", "-h"}},
		{"up-help", []string{"up", "-h"}},
		{"run", []string{"run"}},
		{"login-help", []string{"login", "-h"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

			env := map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
			getenv := func(k string) string { return env[k] }

			var stdout, stderr bytes.Buffer

			code := Run(context.Background(), tc.args, strings.NewReader(""), &stdout, &stderr, getenv, fixedBuildInfo)

			got := fmt.Sprintf("exit %d\n--- stdout\n%s--- stderr\n%s", code, stdout.String(), stderr.String())
			path := filepath.Join("testdata", "golden", tc.name+".txt")

			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatal(err)
				}

				if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
					t.Fatal(err)
				}

				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s (go test -run TestRun_Golden -update writes it): %v", path, err)
			}

			if got != string(want) {
				t.Errorf("goal %s printed\n%s\nwant (%s)\n%s", strings.Join(tc.args, " "), got, path, want)
			}
		})
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
