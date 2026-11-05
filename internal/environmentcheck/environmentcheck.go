// Package environmentcheck tests an MCP server of an environment file (ADR 0037 decision 9): it
// opens the secrets a remote server's headers or a stdio server's env name, calls tools/list with
// them and scrubs every error of the opened values. The environment test route (internal/api) and the conversion at
// goald's start (internal/moveover, decision 19) both test servers with it. Opened values reach
// the ToolLister only.
package environmentcheck

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// secretRef is one {{secret}} reference in a value; its group is the name.
var secretRef = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// Resolver opens secrets by name for a use: ports.OrgSecrets.
type Resolver interface {
	Resolve(ctx context.Context, names []string, use domain.SecretUse) ([]domain.ResolvedSecret, error)
}

// Opener opens the named secrets: their values by name. Its errors name a secret, never a value.
type Opener func(ctx context.Context, names []string) (map[string]string, error)

// Opens is the Opener over secrets for use: each name once, audited as secret.used.
func Opens(secrets Resolver, use domain.SecretUse) Opener {
	return func(ctx context.Context, names []string) (map[string]string, error) {
		out := map[string]string{}
		if len(names) == 0 {
			return out, nil
		}

		resolved, err := secrets.Resolve(ctx, slices.Compact(slices.Sorted(slices.Values(names))), use)
		if err != nil {
			return nil, err
		}

		for _, r := range resolved {
			out[r.Secret.Name] = r.Value
		}

		return out, nil
	}
}

// Result is one server's test: OK with its tools' names and the names of those the server marked
// read-only (annotations.readOnlyHint), both sorted, or Error, scrubbed of every value and cut to
// domain.MaxEnvironmentTestError characters.
type Result struct {
	OK       bool
	Tools    []string
	ReadOnly []string
	Error    string
}

// Server tests the MCP server e, as its file has it: remote at e.URL, or stdio from e.Command,
// which the caller checked is one the tool catalog ships. It opens the secrets e's headers and env
// refer to with open (or takes slots' values, when slots is not nil), and lists its tools with
// them rendered. A secret that does not exist or is not open to the use fails the test; err is
// any other refusal to open, such as a missing master key, and nothing is tested then.
func Server(ctx context.Context, tools ports.ToolLister, open Opener, e ports.MCPEndpoint, slots map[string]string) (Result, error) {
	values := slots
	if values == nil {
		var err error
		names := slices.Sorted(slices.Values(append(Referenced(e.Headers), Referenced(e.Env)...)))
		if values, err = open(ctx, slices.Compact(names)); err != nil {
			if !errors.Is(err, ports.ErrNotFound) && !errors.Is(err, ports.ErrSecretNotAllowed) {
				return Result{}, err
			}

			return Result{Error: Cut(err.Error())}, nil
		}
	}

	e.Headers, e.Env = Render(e.Headers, values), Render(e.Env, values)

	listed, err := tools.ListTools(ctx, e)
	if err != nil {
		return Result{Error: Scrub(err.Error(), values)}, nil
	}

	r := Result{OK: true, Tools: make([]string, 0, len(listed))}

	for _, t := range listed {
		r.Tools = append(r.Tools, t.Name)
		if t.ReadOnly {
			r.ReadOnly = append(r.ReadOnly, t.Name)
		}
	}

	return r, nil
}

// Referenced are the secrets values refer to, sorted, each once.
func Referenced(values map[string]string) []string {
	var out []string

	for _, v := range values {
		for _, m := range secretRef.FindAllStringSubmatch(v, -1) {
			out = append(out, m[1])
		}
	}

	slices.Sort(out)

	return slices.Compact(out)
}

// Render is values with each {{name}} replaced by opened[name].
func Render(values, opened map[string]string) map[string]string {
	out := make(map[string]string, len(values))

	for k, v := range values {
		out[k] = secretRef.ReplaceAllStringFunc(v, func(ref string) string { return opened[secretRef.FindStringSubmatch(ref)[1]] })
	}

	return out
}

// Scrub replaces each opened value in msg, the longest first, and cuts it to what a test keeps:
// an error reaches an answer, a log and the database, which never hold a value.
func Scrub(msg string, opened map[string]string) string {
	values := slices.SortedFunc(maps.Values(opened), func(a, b string) int { return cmp.Compare(len(b), len(a)) })

	for _, v := range values {
		if v != "" {
			msg = strings.ReplaceAll(msg, v, "[secret]")
		}
	}

	return Cut(msg)
}

// OneLine is a host's reason scrubbed of opened's values, on one line and cut to what a test keeps.
func OneLine(reason string, opened map[string]string) string {
	return Scrub(strings.Join(strings.Fields(reason), " "), opened)
}

// Cut keeps the first domain.MaxEnvironmentTestError characters of msg.
func Cut(msg string) string {
	if utf8.RuneCountInString(msg) <= domain.MaxEnvironmentTestError {
		return msg
	}

	return string([]rune(msg)[:domain.MaxEnvironmentTestError-1]) + "…"
}
