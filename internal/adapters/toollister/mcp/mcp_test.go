package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/byFrederick/goal/internal/adapters/toollister/mcp"
	"github.com/byFrederick/goal/internal/core/ports"
)

// server is an MCP server with tools names that answers only requests with the bearer key.
func server(t *testing.T, key string, names ...string) *httptest.Server {
	t.Helper()

	s := sdk.NewServer(&sdk.Implementation{Name: "acme", Version: "1"}, nil)
	for _, n := range names {
		sdk.AddTool(s, &sdk.Tool{Name: n}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{}, nil, nil
		})
	}

	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "unauthorized", http.StatusUnauthorized)

			return
		}

		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	return srv
}

// ListTools sends the headers and answers the tool names, sorted.
func TestListTools_NamesSorted(t *testing.T) {
	t.Parallel()

	srv := server(t, "test-token", "list_issues", toolCreate, "delete_issue")

	got, err := mcp.New(http.DefaultTransport).ListTools(t.Context(),
		ports.MCPEndpoint{URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer test-token"}})
	if err != nil {
		t.Fatal(err)
	}

	if want := []ports.Tool{{Name: toolCreate}, {Name: "delete_issue"}, {Name: "list_issues"}}; !slices.Equal(got, want) {
		t.Errorf("tools = %v; want %v", got, want)
	}
}

// Each tool's annotations.readOnlyHint comes back; a tool without annotations is not read-only.
func TestListTools_ReadOnlyHints(t *testing.T) {
	t.Parallel()

	s := sdk.NewServer(&sdk.Implementation{Name: "acme", Version: "1"}, nil)
	for _, tool := range []*sdk.Tool{
		{Name: toolGet, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true}},
		{Name: "merge_pull_request", Annotations: &sdk.ToolAnnotations{ReadOnlyHint: false}},
		{Name: toolCreate},
	} {
		sdk.AddTool(s, tool, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
			return &sdk.CallToolResult{}, nil, nil
		})
	}

	srv := httptest.NewServer(sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s }, nil))
	t.Cleanup(srv.Close)

	got, err := mcp.New(http.DefaultTransport).ListTools(t.Context(), ports.MCPEndpoint{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	want := []ports.Tool{{Name: toolCreate}, {Name: toolGet, ReadOnly: true}, {Name: "merge_pull_request"}}
	if !slices.Equal(got, want) {
		t.Errorf("tools = %+v; want %+v", got, want)
	}
}

// A refused key is an error naming the server's answer.
func TestListTools_Refused(t *testing.T) {
	t.Parallel()

	srv := server(t, "test-token", "list_issues")

	_, err := mcp.New(http.DefaultTransport).ListTools(t.Context(),
		ports.MCPEndpoint{URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer wrong"}})
	if err == nil || !strings.Contains(err.Error(), "401") && !strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
		t.Errorf("ListTools with a wrong key = %v; want the refusal", err)
	}
}

// A redirect is not followed: the headers never go to another URL.
func TestListTools_NoRedirects(t *testing.T) {
	t.Parallel()

	reached := false
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	t.Cleanup(other.Close)

	srv := httptest.NewServer(http.RedirectHandler(other.URL, http.StatusTemporaryRedirect))
	t.Cleanup(srv.Close)

	_, err := mcp.New(http.DefaultTransport).ListTools(t.Context(),
		ports.MCPEndpoint{URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer test-token"}})
	if err == nil || reached {
		t.Errorf("ListTools through a redirect = %v, reached the other server: %v", err, reached)
	}
}
