// Command goal is the Goal CLI. Its commands live in internal/cli; main only builds the signal
// context, calls cli.Run with the process's arguments, streams, environment and build info, and
// exits with the code it returns.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/byFrederick/goal/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv, debug.ReadBuildInfo)

	stop()
	os.Exit(code)
}
