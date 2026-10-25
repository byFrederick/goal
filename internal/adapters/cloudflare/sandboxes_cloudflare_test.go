//go:build cloudflare

package cloudflare

import (
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/byFrederick/goal/internal/ports/porttest/sandboxtest"
)

// TestSandboxes_Conformance runs sandboxtest against a control Worker's sandbox API:
// GOAL_TEST_SANDBOX_URL and GOAL_TEST_SANDBOX_KEY, its boxes started from GOAL_TEST_SANDBOX_IMAGE
// (a name in the Worker's images: sh, curl and GNU timeout). task cloudflare:sandboxtest runs it
// against wrangler dev, whose boxes are local Docker containers on Docker's bridge network, so the
// network cases run only with GOAL_TEST_SANDBOX_NETWORK=on: against Cloudflare itself, where a box
// must reach the internet and no private address.
func TestSandboxes_Conformance(t *testing.T) {
	t.Parallel()

	url, key, image := os.Getenv("GOAL_TEST_SANDBOX_URL"), os.Getenv("GOAL_TEST_SANDBOX_KEY"), os.Getenv("GOAL_TEST_SANDBOX_IMAGE")
	if url == "" || key == "" || image == "" {
		t.Skip("GOAL_TEST_SANDBOX_URL, GOAL_TEST_SANDBOX_KEY and GOAL_TEST_SANDBOX_IMAGE name the Worker under test")
	}

	s, err := New(Config{URL: url, Key: key}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}

	var network *sandboxtest.Network
	if os.Getenv("GOAL_TEST_SANDBOX_NETWORK") == "on" {
		network = &sandboxtest.Network{
			PublicURL: "https://example.com/",
			PrivateURLs: strings.Fields("https://169.254.169.254/latest/meta-data/ https://10.0.0.1/ https://192.168.0.1/ " +
				"https://172.16.0.1/ https://127.0.0.1/ https://[fd00::1]/"),
		}
	}

	sandboxtest.Run(t, func(*testing.T) sandboxtest.Harness {
		return sandboxtest.Harness{Sandboxes: s, Image: image, Network: network}
	})
}
