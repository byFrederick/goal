package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStart_LogsStartingBeforeSettings(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	getenv := func(name string) string {
		if name == "GOAL_TICK_EVERY" {
			return "not a duration"
		}

		return ""
	}
	read := func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}}, true
	}

	if code := start(&stdout, &stderr, getenv, read); code != 2 {
		t.Errorf("exit code %d, want 2", code)
	}

	first, _, _ := bytes.Cut(stdout.Bytes(), []byte("\n"))

	var line struct {
		Msg     string `json:"msg"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(first, &line); err != nil {
		t.Fatalf("%v: %q", err, stdout.String())
	}

	if line.Msg != "goald starting" || line.Version != "v1.2.3" {
		t.Errorf("first line msg %q version %q, want goald starting and v1.2.3", line.Msg, line.Version)
	}

	if !strings.Contains(stderr.String(), "GOAL_TICK_EVERY") {
		t.Errorf("stderr %q, want the settings error", stderr.String())
	}
}

func TestBuildVersion_PrefersLinkedThenModuleThenRevision(t *testing.T) {
	t.Parallel()

	info := func(version string, settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: version}, Settings: settings}, true
		}
	}
	rev := debug.BuildSetting{Key: "vcs.revision", Value: "abc123"}
	dirty := debug.BuildSetting{Key: "vcs.modified", Value: strconv.FormatBool(true)}

	for _, tc := range []struct {
		name   string
		linked string
		read   func() (*debug.BuildInfo, bool)
		want   string
	}{
		{"linked wins", "v9.9.9", info("v1.2.3", rev), "v9.9.9"},
		{"module version", "", info("v1.2.3", rev), "v1.2.3"},
		{"revision when devel", "", info("(devel)", rev), "abc123"},
		{"modified revision", "", info("(devel)", rev, dirty), "abc123 (modified)"},
		{"devel without revision", "", info("(devel)"), "(devel)"},
		{"no build info", "", func() (*debug.BuildInfo, bool) { return nil, false }, "(devel)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := buildVersion(tc.linked, tc.read); got != tc.want {
				t.Errorf("buildVersion = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStartup_PhaseAndTotalDurations(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return at }
	st := newStartup(now)

	var out bytes.Buffer

	log := slog.New(slog.NewJSONHandler(&out, nil))

	at = at.Add(time.Second) // before the first phase: counts in the total only
	owner := st.begin("owner_pool")
	at = at.Add(2 * time.Second)

	log.Info("startup phase", owner.done()...)

	migrate := st.begin("migrate")
	at = at.Add(3 * time.Second)

	log.Info("database ready", migrate.done()...)

	listening := st.begin("listening")
	at = at.Add(4 * time.Second)

	log.Info("goald listening", append(listening.done(), st.total())...)

	type phaseLine struct {
		Msg     string         `json:"msg"`
		Phase   string         `json:"phase"`
		Took    time.Duration  `json:"took"`
		Startup *time.Duration `json:"startup"`
	}

	var lines []phaseLine

	for sc := bufio.NewScanner(&out); sc.Scan(); {
		var l phaseLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("%v: %s", err, sc.Text())
		}

		lines = append(lines, l)
	}

	want := []struct {
		phase string
		took  time.Duration
	}{{"owner_pool", 2 * time.Second}, {"migrate", 3 * time.Second}, {"listening", 4 * time.Second}}

	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d", len(lines), len(want))
	}

	for i, w := range want {
		if lines[i].Phase != w.phase || lines[i].Took != w.took {
			t.Errorf("line %d: phase %q took %v, want %q and %v", i, lines[i].Phase, lines[i].Took, w.phase, w.took)
		}
	}

	if got := lines[2].Startup; got == nil || *got != 10*time.Second {
		t.Errorf("startup %v, want 10s from start to listening", got)
	}
}
