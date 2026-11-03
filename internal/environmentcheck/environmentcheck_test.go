package environmentcheck_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
	"github.com/byFrederick/goal/internal/environmentcheck"
)

const (
	serverURL = "https://mcp.example.com/mcp"
	keyValue  = "key-for-tests-1111"
)

// opener opens secret key and refuses every other name as not found.
func opener(_ context.Context, names []string) (map[string]string, error) {
	out := map[string]string{}

	for _, n := range names {
		if n != "key" {
			return nil, fmt.Errorf("secret %s: %w", n, ports.ErrNotFound)
		}

		out[n] = keyValue
	}

	return out, nil
}

// A server's headers are sent with their references opened, and its tools come back.
func TestServer_OpensHeadersAndListsTools(t *testing.T) {
	t.Parallel()

	tools := porttest.NewToolLister()
	tools.Serve(serverURL, "search", "fetch")

	r, err := environmentcheck.Server(t.Context(), tools, opener, serverURL, map[string]string{"Authorization": "Bearer {{ key }}"}, nil)
	if err != nil || !r.OK || !slices.Equal(r.Tools, []string{"fetch", "search"}) || r.Error != "" {
		t.Fatalf("Server = %+v, %v; want its tools", r, err)
	}

	if got := tools.Called()[0].Headers["Authorization"]; got != "Bearer "+keyValue {
		t.Errorf("Authorization sent %q; want the opened value", got)
	}
}

// A failed tools/list is the test's error, with no opened value in it.
func TestServer_ErrorScrubbed(t *testing.T) {
	t.Parallel()

	tools := porttest.NewToolLister()
	tools.Fail(serverURL, errors.New("401: bad key "+keyValue))

	r, err := environmentcheck.Server(t.Context(), tools, opener, serverURL, map[string]string{"X-Key": "{{key}}"}, nil)
	if err != nil || r.OK || r.Error != "401: bad key [secret]" {
		t.Errorf("Server = %+v, %v; want the scrubbed error", r, err)
	}
}

// A secret that does not exist fails the test and calls no server; any other refusal to open is
// the caller's error.
func TestServer_SecretRefusals(t *testing.T) {
	t.Parallel()

	tools := porttest.NewToolLister()

	r, err := environmentcheck.Server(t.Context(), tools, opener, serverURL, map[string]string{"X-Key": "{{other}}"}, nil)
	if err != nil || r.OK || !strings.Contains(r.Error, "secret other") || len(tools.Called()) != 0 {
		t.Errorf("Server with a missing secret = %+v, %v (%d calls); want a failed test and no call", r, err, len(tools.Called()))
	}

	locked := errors.New("no master key")

	_, err = environmentcheck.Server(t.Context(), tools, func(context.Context, []string) (map[string]string, error) { return nil, locked },
		serverURL, map[string]string{"X-Key": "{{key}}"}, nil)
	if !errors.Is(err, locked) {
		t.Errorf("Server without a master key = %v; want its error", err)
	}
}

// OneLine puts a host's reason on one line, scrubbed, and Cut keeps what a test keeps.
func TestOneLine_ScrubsAndCuts(t *testing.T) {
	t.Parallel()

	if got := environmentcheck.OneLine("token "+keyValue+"\n  has no\tpush", map[string]string{"key": keyValue}); got != "token [secret] has no push" {
		t.Errorf("OneLine = %q", got)
	}

	if got := environmentcheck.Cut(strings.Repeat("é", domain.MaxEnvironmentTestError+5)); utf8.RuneCountInString(got) != domain.MaxEnvironmentTestError {
		t.Errorf("Cut kept %d characters; want %d", utf8.RuneCountInString(got), domain.MaxEnvironmentTestError)
	}
}
