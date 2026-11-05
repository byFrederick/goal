package config

import (
	"fmt"
	"maps"
	"path"
	"slices"

	"github.com/byFrederick/goal/internal/core/domain"
)

// CheckPolicy is the check of a definition against its organization's policy (ADR 0024 decision
// 3): authoring runs it on publish and validate, since the policy is not in the definition.
const CheckPolicy = "policy"

// CodeModelNotAllowed is the code of a spec.runtime.model or an agent's model that is not one the
// organization's policy allows.
const CodeModelNotAllowed = "model-not-allowed"

// ModelProblems reports each model f names, in spec.runtime.model and in its agents' model, that
// is not in allowed: an error at that key naming the agent (or runtime) and the model. A factory
// that names no model gets the policy's default at run start, so it has none.
func ModelProblems(f *Factory, allowed []string) []domain.DefinitionProblem {
	var out []domain.DefinitionProblem

	refuse := func(path []string, who, model string) {
		out = append(out, f.doc.locate(path).Problem(CheckPolicy, CodeModelNotAllowed, domain.SeverityError,
			fmt.Sprintf("%s uses model %s, which the organization policy does not allow; allowed: %v", who, model, allowed)))
	}

	if m := f.Spec.Runtime.Model; m != "" && !slices.Contains(allowed, m) {
		refuse(at("spec", keyRuntime, "model"), keyRuntime, m)
	}

	for i, a := range f.Spec.Agents {
		if a.Model != "" && !slices.Contains(allowed, a.Model) {
			refuse(at("spec", keyAgents, i, "model"), "agent "+a.Name, a.Model)
		}
	}

	return out
}

// CodeMCPToolModeCapped is the code of an environment file's mcp.<server>.tools entry whose mode
// the organization policy's MCP tool mode cap makes stricter in every run.
const CodeMCPToolModeCapped = "mcp-tool-mode-capped"

// MCPCapProblems reports each mcp.<server>.tools entry of f's environment files that limit, the
// organization policy's MCP tool mode cap, makes stricter (ADR 0037 decisions 8 and 12): a warning
// at that entry naming the server, the tool, the file's mode and the effective one. The cap
// applies in every run anyway, so nothing here blocks publishing. The effective mode is the
// entry's in domain.ServerToolModes with the cap and no standing rule, as the run bundle sends it;
// a cap entry for one tool name that a file glob covers is reported at that glob.
func MCPCapProblems(f *Factory, limit map[string]domain.ConnectionToolMode) []domain.DefinitionProblem {
	if len(limit) == 0 {
		return nil
	}

	var out []domain.DefinitionProblem

	for _, e := range f.environments {
		for _, server := range slices.Sorted(maps.Keys(e.Spec.MCP)) {
			file := map[string]domain.ConnectionToolMode{}
			for p, m := range e.Spec.MCP[server].Tools {
				file[p] = domain.ConnectionToolMode(m)
			}

			patterns := slices.SortedFunc(maps.Keys(file), domain.CompareToolPatterns)

			for _, m := range domain.ServerToolModes(file, limit, nil, "", e.Name, server, "") {
				entry, ok := m.Tool, true
				if _, written := file[m.Tool]; !written {
					// A cap pattern the file does not write: an exact tool name under one of the
					// file's globs takes the cap's mode in place of that glob's.
					entry, ok = governing(patterns, m.Tool)
				}

				if !ok || file[entry] == m.Mode {
					continue // a tool the file does not list, or an entry the cap leaves alone
				}

				what := "tool " + m.Tool
				if entry != m.Tool {
					what += ", which " + entry + " covers,"
				}

				out = append(out, e.Locate(keySpec, "mcp", server, "tools", entry).Problem(CheckPolicy, CodeMCPToolModeCapped, domain.SeverityWarning,
					fmt.Sprintf("%s: %s of MCP server %s is %s here, but the organization policy caps it at %s, which every run uses",
						e.file, what, server, file[entry], m.Mode)))
			}
		}
	}

	return out
}

// governing is the most specific of patterns, sorted by domain.CompareToolPatterns, that matches
// tool, an exact name; a glob is never governed.
func governing(patterns []string, tool string) (string, bool) {
	if domain.IsToolGlob(tool) {
		return "", false
	}

	for _, p := range patterns {
		if ok, err := path.Match(p, tool); err == nil && ok {
			return p, true
		}
	}

	return "", false
}
