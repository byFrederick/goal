package api

import (
	"encoding/json"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
)

// GatewayPath is where goald serves the agents' MCP gateway.
const GatewayPath = "/mcp"

// healthPath is the liveness check, which load balancers call by address.
const healthPath = "/v1/health"

// CheckHost refuses, with 421 Misdirected Request, a request whose Host names none of hosts
// (host names or IP addresses, without ports; compared case-insensitively), so a page that
// rebinds its own name to goald's address cannot use the API or the SPA: its requests would
// otherwise pass cross-origin protection as same-origin (DNS rebinding). The port is not
// compared: the name is what a rebinding page controls, and the web dev server's proxy keeps
// the browser's Host (localhost:5173). The health check and the agents' gateway are exempt:
// agent containers reach the gateway by another name (host.docker.internal), and every gateway
// request must carry a task token, which a rebinding page cannot present.
func CheckHost(hosts []string, next http.Handler) http.Handler {
	allowed := make([]string, 0, len(hosts))
	for _, h := range hosts {
		allowed = append(allowed, hostname(h))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == healthPath || r.URL.Path == GatewayPath {
			next.ServeHTTP(w, r)
			return
		}

		if h := hostname(r.Host); h == "" || !slices.Contains(allowed, h) {
			misdirected(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// hostname is host without its port and IPv6 brackets, lower-cased, without a trailing dot.
func hostname(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")

	return strings.TrimSuffix(strings.ToLower(host), ".")
}

// misdirected answers the Host check's refusal as a problem without detail.
func misdirected(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusMisdirectedRequest)

	// The client is gone or the connection broke; there is nothing left to tell it.
	_ = json.NewEncoder(w).Encode(gen.Problem{Title: http.StatusText(http.StatusMisdirectedRequest), Status: http.StatusMisdirectedRequest})
}
