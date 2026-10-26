package cli

import (
	"errors"
	"runtime/debug"
)

// version prints the module path and version and the build info the toolchain embedded.
func version(out *printer, read func() (*debug.BuildInfo, bool), args []string) error {
	if len(args) != 0 {
		return errUsage
	}

	info, ok := read()
	if !ok {
		return errors.New("build info is unavailable")
	}

	out.printf("%s %s\n", info.Main.Path, info.Main.Version)
	out.printf("go %s\n", info.GoVersion)

	settings := map[string]string{}
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}

	if rev := settings["vcs.revision"]; rev != "" {
		if settings["vcs.modified"] == "true" {
			rev += " (modified)"
		}

		out.printf("vcs.revision %s\n", rev)
	}

	if t := settings["vcs.time"]; t != "" {
		out.printf("vcs.time %s\n", t)
	}

	return nil
}

// buildInfoFunc reads the build info of the running binary; main passes debug.ReadBuildInfo.
type buildInfoFunc = func() (*debug.BuildInfo, bool)
