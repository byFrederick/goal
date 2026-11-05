package mcp

import (
	"net/http"
	"time"
)

// NewWithTimeout is New with timeout in place of Timeout, so a test of a server that never
// answers takes a second, not ten.
func NewWithTimeout(transport http.RoundTripper, timeout time.Duration) *Lister {
	return &Lister{transport: transport, timeout: timeout}
}
