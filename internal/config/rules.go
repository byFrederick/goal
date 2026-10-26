package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/domain"
)

// RulesDir is the folder of a definition that holds its rule files.
const RulesDir = "rules"

// ruleFront is a rule file's frontmatter. Its keys are all a rule may have: a misspelt one is
// an error rather than a scope silently dropped.
type ruleFront struct {
	Description string `yaml:"description"`
	AppliesTo   *struct {
		Agents    []string `yaml:"agents"`
		Stations  []string `yaml:"stations"`
		Lanes     []string `yaml:"lanes"`
		WorkTypes []string `yaml:"workTypes"`
	} `yaml:"appliesTo"`
	Paths []string `yaml:"paths"`
}

// errNoFrontMatter marks a rule file that does not start with a --- frontmatter block.
var errNoFrontMatter = errors.New("has no frontmatter: start the file with ---, description: and ---")

// rules reads each rule file spec.rules declares, records what it says on the factory and
// reports what is wrong with it at the rule's source in factory.yaml.
func (c *checker) rules() {
	for i := range c.f.Spec.Rules {
		r := &c.f.Spec.Rules[i]
		p := at("spec", "rules", i, "source")

		if code, why := c.readRule(r); code != "" {
			c.errorf(checkReferences, code, p, "rule %s: %s", r.Name, why)
		}

		c.ruleScope(r, p)
	}
}

// readRule fills r from its file. When the file is not a valid rule it returns the problem's code
// and why, rule-not-found or rule-invalid; "" when it is.
func (c *checker) readRule(r *Rule) (code, why string) {
	file := path.Clean(r.Source)
	if !strings.HasPrefix(file, RulesDir+"/") || path.Ext(file) != ".md" {
		return codeRuleInvalid, r.Source + " is not a markdown file in " + RulesDir + "/"
	}

	raw, err := fs.ReadFile(c.f.files, file)
	if err != nil {
		return codeRuleNotFound, r.Source + " not found"
	}

	front, body, err := parseRule(raw)

	switch {
	case errors.Is(err, errNoFrontMatter):
		return codeRuleInvalid, fmt.Sprintf("%s %v", r.Source, err)
	case err != nil:
		return codeRuleInvalid, fmt.Sprintf("frontmatter does not parse: %v", err)
	case strings.TrimSpace(front.Description) == "":
		return codeRuleInvalid, "description is required in the frontmatter: one line saying what the rule is for"
	case strings.ContainsAny(strings.TrimSpace(front.Description), "\r\n"):
		return codeRuleInvalid, "description must be one line"
	case slices.Contains(front.Paths, ""):
		return codeRuleInvalid, "paths has an empty glob"
	}

	r.Description, r.Paths, r.Body = strings.TrimSpace(front.Description), front.Paths, body

	if a := front.AppliesTo; a != nil {
		r.AppliesTo = domain.RuleScope{Agents: a.Agents, Stations: a.Stations, Lanes: a.Lanes, WorkTypes: a.WorkTypes}

		for _, l := range []struct {
			key  string
			list []string
		}{{keyAgents, a.Agents}, {keyStations, a.Stations}, {"lanes", a.Lanes}, {"workTypes", a.WorkTypes}} {
			if l.list != nil && len(l.list) == 0 {
				return codeRuleInvalid, "appliesTo." + l.key + " is empty: omit it to apply the rule whatever the " + l.key
			}
		}
	}

	return "", ""
}

// ruleScope reports each name of a rule's appliesTo that the definition does not declare.
func (c *checker) ruleScope(r *Rule, p []string) {
	var lanes []string
	for _, s := range c.f.Spec.Board.Stations {
		lanes = append(lanes, sortedKeys(s.Route)...)
	}

	for _, l := range []struct {
		key, code, what, hint string
		list, known           []string
	}{
		{keyAgents, codeUnknownAgent, "agent", "", r.AppliesTo.Agents, c.agents},
		{keyStations, codeUnknownStation, "station", "", r.AppliesTo.Stations, c.stations},
		{"lanes", codeUnknownLane, "lane", "; lanes are the keys of the stations' routes", r.AppliesTo.Lanes, lanes},
		{"workTypes", codeUnknownWorkType, "work type", "; work types are board.workTypes", r.AppliesTo.WorkTypes, c.f.Spec.Board.WorkTypes},
	} {
		for _, v := range l.list {
			if !slices.Contains(l.known, v) {
				c.errorf(checkReferences, l.code, p, "rule %s: appliesTo.%s: unknown %s %q%s", r.Name, l.key, l.what, v, l.hint)
			}
		}
	}
}

// parseRule splits a rule file into its frontmatter and its body, the markdown after it.
func parseRule(raw []byte) (ruleFront, string, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")

	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return ruleFront{}, "", errNoFrontMatter
	}

	var front, body string

	switch {
	case strings.HasPrefix(rest, "---\n") || rest == "---":
		body = strings.TrimPrefix(rest, "---")
	default:
		end := strings.Index(rest, "\n---\n")
		if end < 0 {
			if !strings.HasSuffix(rest, "\n---") {
				return ruleFront{}, "", errNoFrontMatter
			}

			end = len(rest) - len("\n---")
		}

		front, body = rest[:end], rest[end+len("\n---"):]
	}

	var fm ruleFront

	dec := yaml.NewDecoder(bytes.NewReader([]byte(front)))
	dec.KnownFields(true)

	if err := dec.Decode(&fm); err != nil && !errors.Is(err, io.EOF) {
		return ruleFront{}, "", err
	}

	return fm, strings.TrimLeft(body, "\n"), nil
}
