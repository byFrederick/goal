// Command goal-egress is the egress proxy of agent, sidecar and check containers (internal/egress,
// ADR 0009). goald's Docker adapter runs it as a container of its own on the agents' and tools'
// networks and on one with a route out; nothing else runs it. Flags:
//
//	-listen :3128                 the proxy's address
//	-gateway URL                  goald's MCP gateway, served to agents at its path
//	-agents CIDR, -tools CIDR     the client networks of each policy (repeatable)
//	-agent-host host:port         a destination agents may reach (repeatable)
//	-trust host:port              a destination dialed without the public-address check (repeatable)
//	-health                       exit 0 when the proxy at -listen accepts connections (the image's health check)
//
// It logs JSON lines on stderr and stops on SIGTERM.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/byFrederick/goal/internal/egress"
)

func main() { os.Exit(run(os.Args[1:], os.Stderr)) }

// list is a repeatable string flag.
type list []string

func (l *list) String() string { return strings.Join(*l, ",") }

func (l *list) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// prefixes is a repeatable CIDR flag.
type prefixes []netip.Prefix

func (p *prefixes) String() string { return fmt.Sprint(*p) }

func (p *prefixes) Set(v string) error {
	n, err := netip.ParsePrefix(v)
	if err != nil {
		return err
	}

	*p = append(*p, n.Masked())

	return nil
}

func run(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("goal-egress", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		cfg                 egress.Config
		agents, tools       prefixes
		agentHosts, trusted list
	)

	listen := fs.String("listen", ":3128", "the proxy's address")
	gateway := fs.String("gateway", "", "goald's MCP gateway, served to agents at its path")
	health := fs.Bool("health", false, "exit 0 when the proxy at -listen accepts connections")

	fs.Var(&agents, "agents", "an agents' network (CIDR, repeatable)")
	fs.Var(&tools, "tools", "a tools' network: sidecars and checks (CIDR, repeatable)")
	fs.Var(&agentHosts, "agent-host", "a destination agents may reach (host:port, repeatable)")
	fs.Var(&trusted, "trust", "a destination dialed without the public-address check (host:port, repeatable)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *health {
		return probe(*listen)
	}

	log := slog.New(slog.NewJSONHandler(stderr, nil))

	if *gateway != "" {
		u, err := url.Parse(*gateway)
		if err != nil {
			log.Error("goal-egress: bad -gateway", slog.Any("error", err))
			return 2
		}

		cfg.Gateway = u
	}

	cfg.Agents, cfg.Tools, cfg.AgentHosts, cfg.Trusted, cfg.Log = agents, tools, agentHosts, trusted, log

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
