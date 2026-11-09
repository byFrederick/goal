package main

import (
	"log/slog"
	"runtime/debug"
	"strconv"
	"time"
)

// version is goald's release, linked in with -ldflags "-X main.version=<version>". Without it,
// buildVersion reads the build info.
var version string

// develVersion is the version of a build with no linked version, module version or VCS revision.
const develVersion = "(devel)"

// buildVersion is the version "goald starting" logs: the linked-in version, else the module's
// version, else the VCS revision (" (modified)" for a dirty tree), else "(devel)". An image built
// with -trimpath and no .git has none of the last three, so it reads "(devel)".
func buildVersion(linked string, read func() (*debug.BuildInfo, bool)) string {
	if linked != "" {
		return linked
	}

	info, ok := read()
	if !ok || info == nil {
		return develVersion
	}

	if v := info.Main.Version; v != "" && v != develVersion {
		return v
	}

	var rev, modified string

	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}

	if rev == "" {
		return develVersion
	}

	if dirty, _ := strconv.ParseBool(modified); dirty {
		rev += " (modified)"
	}

	return rev
}

// startup times goald's start: each phase's line carries "phase" and "took", and "goald
// listening" carries the total, "startup", from the moment goald began.
type startup struct {
	now     func() time.Time
	started time.Time
}

func newStartup(now func() time.Time) *startup {
	return &startup{now: now, started: now()}
}

// begin starts timing the phase name.
func (s *startup) begin(name string) startupPhase {
	return startupPhase{name: name, began: s.now(), now: s.now}
}

// total is the time from goald's start to now.
func (s *startup) total() slog.Attr {
	return slog.Duration("startup", s.now().Sub(s.started))
}

// startupPhase is one timed step of goald's start.
type startupPhase struct {
	name  string
	began time.Time
	now   func() time.Time
}

// done returns the phase's log attrs: its name and how long it took since begin.
func (p startupPhase) done() []any {
	return []any{slog.String("phase", p.name), slog.Duration("took", p.now().Sub(p.began))}
}
