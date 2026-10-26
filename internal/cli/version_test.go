package cli

import (
	"bytes"
	"context"
	"errors"
	"runtime/debug"
	"testing"
)

func TestVersion(t *testing.T) {
	t.Parallel()

	info := func(settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{
				GoVersion: "go1.26.0",
				Main:      debug.Module{Path: "github.com/byFrederick/goal", Version: "(devel)"},
				Settings:  settings,
			}, true
		}
	}

	revision := debug.BuildSetting{Key: "vcs.revision", Value: "abc123"}
	vcsTime := debug.BuildSetting{Key: "vcs.time", Value: "2026-10-23T10:00:00Z"}

	tests := []struct {
		name    string
		read    func() (*debug.BuildInfo, bool)
		args    []string
		want    string
		wantErr string
		usage   bool
	}{
		{
			name: "full info with vcs settings",
			read: info(revision, vcsTime, debug.BuildSetting{Key: "vcs.modified", Value: "false"}),
			want: "github.com/byFrederick/goal (devel)\ngo go1.26.0\nvcs.revision abc123\nvcs.time 2026-10-23T10:00:00Z\n",
		},
		{
			name: "no vcs settings",
			read: info(),
			want: "github.com/byFrederick/goal (devel)\ngo go1.26.0\n",
		},
		{
			name: "modified tree",
			read: info(revision, vcsTime, debug.BuildSetting{Key: "vcs.modified", Value: "true"}),
			want: "github.com/byFrederick/goal (devel)\ngo go1.26.0\nvcs.revision abc123 (modified)\nvcs.time 2026-10-23T10:00:00Z\n",
		},
		{
			name:    "no build info",
			read:    func() (*debug.BuildInfo, bool) { return nil, false },
			wantErr: "build info is unavailable",
		},
		{
			name:  "extra args",
			read:  info(),
			args:  []string{"extra"},
			usage: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			out := &printer{w: &buf}
			err := run(context.Background(), append([]string{"version"}, tt.args...), out, func(string) string { return "" }, tt.read)

			switch {
			case tt.usage:
				if !errors.Is(err, errUsage) {
					t.Fatalf("err = %v, want errUsage", err)
				}
			case tt.wantErr != "":
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
			case err != nil:
				t.Fatalf("unexpected error: %v", err)
			}

			if buf.String() != tt.want {
				t.Errorf("output = %q, want %q", buf.String(), tt.want)
			}
		})
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }

func TestVersionWriteError(t *testing.T) {
	t.Parallel()

	out := &printer{w: failWriter{}}
	read := func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{GoVersion: "go1.26.0", Main: debug.Module{Path: "m", Version: "v1"}}, true
	}

	if err := run(context.Background(), []string{"version"}, out, func(string) string { return "" }, read); err != nil {
		t.Fatalf("run: %v", err)
	}

	if out.err == nil {
		t.Fatal("printer kept no write error")
	}
}
