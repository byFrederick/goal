// Command goal-egress is the egress proxy of every sandbox (internal/egress, ADR 0009 and its
// phase 4 amendment): compose.yaml's goal-egress service, on the agents' network and on one with
// a route out. Flags:
//
//	-listen :3128     the proxy's address
//	-gateway URL      goald's gateway listener, an origin whose /mcp and /run boxes reach
//	                  (default $GOAL_EGRESS_GATEWAY; empty serves no gateway)
//	-health           exit 0 when the proxy at -listen accepts connections (the image's health check)
//
// It logs JSON lines on stderr and stops on SIGTERM.
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/byFrederick/goal/internal/egress"
)

// gatewayEnv is the -gateway default: compose.yaml sets it.
const gatewayEnv = "GOAL_EGRESS_GATEWAY"

func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stderr)) }

func run(args []string, getenv func(string) string, stderr io.Writer) int {
	fs := flag.NewFlagSet("goal-egress", flag.ContinueOnError)
	fs.SetOutput(stderr)

	listen := fs.String("listen", ":3128", "the proxy's address")
	gateway := fs.String("gateway", getenv(gatewayEnv), "goald's gateway listener, an origin whose /mcp and /run boxes reach (default $"+gatewayEnv+")")
	health := fs.Bool("health", false, "exit 0 when the proxy at -listen accepts connections")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *health {
		return probe(*listen)
	}

	log := slog.New(slog.NewJSONHandler(stderr, nil))

	var cfg egress.Config

	if *gateway != "" {
		u, err := url.Parse(*gateway)
		if err != nil {
			log.Error("goal-egress: bad -gateway", slog.Any("error", err))
			return 2
		}

		cfg.Gateway = u
	}

	cfg.Log = log

	proxy, err := egress.New(cfg)
	if err != nil {
		log.Error("goal-egress: refused its configuration", slog.Any("error", err))
		return 2
	}

	if err := serve(*listen, proxy, log); err != nil {
		log.Error("goal-egress stopped", slog.Any("error", err))
		return 1
	}

	return 0
}

func serve(listen string, h http.Handler, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	srv := &http.Server{
		Addr: listen, Handler: h,
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 64 << 10,
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelDebug),
	}

	errc := make(chan error, 1)

	go func() { errc <- srv.ListenAndServe() }()

	log.Info("goal-egress listening", slog.String("addr", listen))

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return nil
}

// probe connects to the proxy's own port on loopback.
func probe(listen string) int {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return 1
	}

	c, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return 1
	}

	_ = c.Close()

	return 0
}
