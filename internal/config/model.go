package config

import (
	"strings"
	"unicode"

	"github.com/byFrederick/goal/internal/domain"
)

// ModelPointer maps a path in factory.yaml to the Visual editor's section and the RFC 6901
// pointer of the same value in the factory model (FactoryModel in api/openapi.yaml, built by
// internal/factorymodel): /spec/board/stations/3/gate/onReject is ("line",
// "/line/stations/3/gate/on_reject"). Model keys are the YAML keys in snake_case; keys that
// are data (route lanes, tool names, header and env names, connector settings) are kept as
// written. A path the model does not show (runtime.image, knowledgeBase, contracts, ...) is
// (SectionFiles, ""), edited in YAML mode only.
func ModelPointer(path []string) (section, pointer string) {
	sec, toks := modelPath(path)
	if sec == domain.SectionFiles {
		return sec, ""
	}

	return sec, joinPointer(toks)
}

// Keys that appear in several of the mappings below.
const (
	keyName   = "name"
	keyAgents = "agents"
)

func modelPath(p []string) (string, []string) {
	switch {
	case len(p) >= 1 && p[0] == "metadata":
		return metadataPath(p[1:])
	case len(p) < 2 || p[0] != "spec":
		return domain.SectionFiles, nil
	}

	return specPath(p[1], p[2:])
}

func specPath(key string, rest []string) (string, []string) {
	switch key {
	case "runtime":
		return runtimePath(rest)
	case "board":
		return boardPath(rest)
	case keyAgents:
		return domain.SectionAgents, append([]string{domain.SectionAgents, keyAgents}, agentPath(rest)...)
	case "skills":
		return domain.SectionAgents, append([]string{domain.SectionAgents, "skills"}, rest...)
	case "connections":
		return domain.SectionConnections, append([]string{domain.SectionConnections}, connectionPath(rest)...)
	case "learning":
		if len(rest) >= 2 && rest[0] == "librarian" && rest[1] == "agent" {
			return domain.SectionLine, []string{domain.SectionLine, "librarian"}
		}
	}

	return domain.SectionFiles, nil
}

func metadataPath(rest []string) (string, []string) {
	if len(rest) == 0 {
		return domain.SectionBasics, []string{domain.SectionBasics}
	}

	switch rest[0] {
	case "id", keyName, "title":
		return domain.SectionBasics, []string{domain.SectionBasics, rest[0]}
	case "purpose", "description":
		return domain.SectionBasics, []string{domain.SectionBasics, "purpose"}
	}

	return domain.SectionFiles, nil
}

func runtimePath(rest []string) (string, []string) {
	if len(rest) == 0 {
		return domain.SectionFiles, nil
	}

	switch rest[0] {
	case "repository":
		return domain.SectionBasics, append([]string{domain.SectionBasics, "repository"}, rest[1:]...)
	case "model":
		return domain.SectionAgents, []string{domain.SectionAgents, "default_model"}
	case "budget":
		return domain.SectionReview, append([]string{domain.SectionReview, "budget"}, snakeAll(rest[1:])...)
	}

	return domain.SectionFiles, nil
}

func boardPath(rest []string) (string, []string) {
	if len(rest) == 0 {
		return domain.SectionLine, []string{domain.SectionLine}
	}

	switch rest[0] {
	case "workTypes":
		return domain.SectionBasics, append([]string{domain.SectionBasics, "work_types"}, rest[1:]...)
	case "stations":
		return domain.SectionLine, append([]string{domain.SectionLine, "stations"}, stationPath(rest[1:])...)
	case "intake", "flow":
		return domain.SectionLine, append([]string{domain.SectionLine}, snakeAll(rest)...)
	}

	return domain.SectionFiles, nil
}

// stationPath maps the part of a path below board.stations: [3 route backend] stays, the
// lane being data; schema keys become snake_case.
func stationPath(rest []string) []string {
	if len(rest) < 2 {
		return rest
	}

	if rest[1] == "route" {
		return rest
	}

	return append([]string{rest[0]}, snakeAll(rest[1:])...)
}

func agentPath(rest []string) []string {
	if len(rest) < 2 {
		return rest
	}

	switch rest[1] {
	case "tools":
		return []string{rest[0], "custom_tools"}
	case "builtins":
		return rest
	}

	return append([]string{rest[0], snake(rest[1])}, rest[2:]...)
}

// connectionPath maps the part of a path below spec.connections: the model's own keys stay,
// tool entries keep their tool name, and every other key (a connector setting, or a field of
// the legacy type form) is under settings.
func connectionPath(rest []string) []string {
	if len(rest) < 2 {
		return rest
	}

	switch rest[1] {
	case keyName, "connector", "type", "credential", keyAgents, "tools":
		return rest
	}

	return append([]string{rest[0], "settings"}, rest[1:]...)
}

func snakeAll(toks []string) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = snake(t)
	}

	return out
}

// snake turns a camelCase schema key into snake_case: maxRework → max_rework.
func snake(s string) string {
	var b strings.Builder

	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}

			r = unicode.ToLower(r)
		}

		b.WriteRune(r)
	}

	return b.String()
}
