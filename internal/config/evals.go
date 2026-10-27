package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/contracts/definitions"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The folders of a definition that hold its judges and its eval cases (ADR 0023): one
// evals/judges/<slug>.md per judge, one evals/cases/<name>.yaml per case.
const (
	JudgesDir = "evals/judges"
	CasesDir  = "evals/cases"
)

// evalName is the form of a judge's slug and a case's name, its file name without the extension:
// evals.schema.json's slug.
var evalName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

// evalKinds are evals.schema.json's two kinds, compiled.
type evalKinds struct{ evalCase, judge *jsonschema.Schema }

// evalSchemas compiles evalKinds once.
var evalSchemas = sync.OnceValues(func() (evalKinds, error) {
	var s evalKinds

	var err error
	if s.evalCase, err = compileSchema(definitions.Evals, "evals.schema.json", "#/$defs/case"); err != nil {
		return s, err
	}

	s.judge, err = compileSchema(definitions.Evals, "evals.schema.json", "#/$defs/judge")

	return s, err
})

// judgeFront is what the checks read of a judge's front matter; the schema checks the rest.
type judgeFront struct {
	Agents   []string  `json:"agents"`
	Outcomes []outcome `json:"outcomes"`
}

// outcome is one answer a judge may give.
type outcome struct {
	Score float64 `json:"score"`
	Label string  `json:"label"`
}

// evalCase is what the checks read of an eval case; the schema checks the rest.
type evalCase struct {
	Spec struct {
		Agent string `json:"agent"`
		Check struct {
			Judge          string `json:"judge"`
			ReferencePatch string `json:"referencePatch"`
		} `json:"check"`
	} `json:"spec"`
}

// evalFile is a judge's front matter or a case, parsed, for locating its problems in its own file.
type evalFile struct {
	path string
	doc  *document
	// offset is the lines of the file before the YAML: a judge's opening ---.
	offset int
}

// problem is an error at path inside the file's YAML, or about the whole file when the file did
// not parse.
func (e evalFile) problem(check, code string, at []string, format string, a ...any) domain.DefinitionProblem {
	var l Location
	if e.doc != nil {
		l = e.doc.locate(at)
	}

	return e.shift(domain.DefinitionProblem{
		Code: code, Severity: domain.SeverityError, Check: check, Section: domain.SectionFiles, File: e.path,
		Line: l.Line, Column: l.Column, EndLine: l.EndLine, EndColumn: l.EndColumn, Message: fmt.Sprintf(format, a...),
	})
}

// shift moves a problem located in the YAML to its line in the file.
func (e evalFile) shift(p domain.DefinitionProblem) domain.DefinitionProblem {
	if p.Line > 0 {
		p.Line += e.offset
	}

	if p.EndLine > 0 {
		p.EndLine += e.offset
	}

	return p
}

// evals checks the definition's judges and eval cases.
func (c *checker) evals() {
	c.evalCases(c.judges())
}

// judges checks each judge file and returns the slugs of those whose file name is one.
func (c *checker) judges() []string {
	var slugs []string

	for _, p := range evalFiles(c.f.files, JudgesDir, ".md") {
		if slug, ok := c.evalName(p); ok {
			slugs = append(slugs, slug)
		}

		c.judge(p)
	}

	return slugs
}

func (c *checker) judge(p string) {
	raw, err := fs.ReadFile(c.f.files, p)
	if err != nil {
		return
	}

	front, _, err := splitFrontMatter(raw)
	if err != nil || strings.TrimSpace(front) == "" {
		missing := evalFile{path: p}.problem(checkEvals, codeJudgeFrontMissing, nil,
			"judge %s has no front matter: start the file with a --- block that holds its title, criterion, outcomes, passMark and sample, "+
				"and close it with ---; the rubric follows", p)
		if err != nil {
			missing.Line, missing.Column, missing.EndLine = 1, 1, 1
		}

		c.problems = append(c.problems, missing)

		return
	}

	doc, bad := parseYAML(p, []byte(front))
	if bad != nil {
		c.problems = append(c.problems, evalFile{path: p, offset: 1}.shift(*bad))

		return
	}

	ef := evalFile{path: p, doc: doc, offset: 1}
	if !c.evalSchema(ef, func(s evalKinds) *jsonschema.Schema { return s.judge }) {
		return
	}

	var j judgeFront
	if err := json.Unmarshal(doc.json, &j); err != nil {
		c.problems = append(c.problems, ef.problem(checkYAML, codeYAMLInvalid, nil, "judge %s: %v", p, err))

		return
	}

	for i, a := range j.Agents {
		if !slices.Contains(c.agents, a) {
			c.problems = append(c.problems, ef.problem(checkReferences, codeUnknownAgent, at(keyAgents, i),
				"judge %s: unknown agent %q", p, a))
		}
	}

	for i, o := range j.Outcomes {
		earlier := j.Outcomes[:i]

		if k := slices.IndexFunc(earlier, func(e outcome) bool {
			return e.Score == o.Score
		}); k >= 0 {
			c.problems = append(c.problems, ef.problem(checkEvals, codeJudgeOutcomeDuplicate, at("outcomes", i, "score"),
				"judge %s: outcomes %d and %d have the same score %v; each outcome scores differently", p, k+1, i+1, o.Score))
		}

		if k := slices.IndexFunc(earlier, func(e outcome) bool {
			return strings.EqualFold(e.Label, o.Label)
		}); k >= 0 {
			c.problems = append(c.problems, ef.problem(checkEvals, codeJudgeOutcomeDuplicate, at("outcomes", i, "label"),
				"judge %s: outcomes %d and %d have the same label %q", p, k+1, i+1, o.Label))
		}
	}
}

// evalCases checks each case file against the factory and judges, the slugs of its judges.
func (c *checker) evalCases(judges []string) {
	for _, p := range evalFiles(c.f.files, CasesDir, ".yaml", ".yml") {
		c.evalName(p)

		raw, err := fs.ReadFile(c.f.files, p)
		if err != nil {
			continue
		}

		var n yaml.Node
		if yaml.Unmarshal(raw, &n) != nil {
			continue // otherFileProblems reports it
		}

		doc, bad := parseYAML(p, raw)
		if bad != nil {
			c.problems = append(c.problems, *bad)

			continue
		}

		ef := evalFile{path: p, doc: doc}
		if !c.evalSchema(ef, func(s evalKinds) *jsonschema.Schema { return s.evalCase }) {
			continue
		}

		var ec evalCase
		if err := json.Unmarshal(doc.json, &ec); err != nil {
			c.problems = append(c.problems, ef.problem(checkYAML, codeYAMLInvalid, nil, "case %s: %v", p, err))

			continue
		}

		if a := ec.Spec.Agent; !slices.Contains(c.agents, a) {
			c.problems = append(c.problems, ef.problem(checkReferences, codeUnknownAgent, at("spec", "agent"),
				"case %s: unknown agent %q", p, a))
		}

		if j := ec.Spec.Check.Judge; j != "" && !slices.Contains(judges, j) {
			c.problems = append(c.problems, ef.problem(checkReferences, codeUnknownJudge, at("spec", "check", "judge"),
				"case %s: unknown judge %q; a judge is a file %s/<slug>.md", p, j, JudgesDir))
		}

		if rp := ec.Spec.Check.ReferencePatch; rp != "" && !c.f.exists(rp) {
			c.problems = append(c.problems, ef.problem(checkReferences, codePatchNotFound, at("spec", "check", "referencePatch"),
				"case %s: reference patch %s not found", p, rp))
		}
	}
}

// evalSchema validates the file against the kind pick chooses and records each failed rule. It
// reports whether the file satisfies the kind.
func (c *checker) evalSchema(ef evalFile, pick func(evalKinds) *jsonschema.Schema) bool {
	schemas, err := evalSchemas()
	if err != nil {
		c.problems = append(c.problems, ef.problem(checkSchema, codeSchemaInvalid, nil, "evals.schema.json does not compile: %v", err))

		return false
	}

	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(ef.doc.json))
	if err != nil {
		c.problems = append(c.problems, ef.problem(checkYAML, codeYAMLInvalid, nil, "%s does not parse: %v", ef.path, err))

		return false
	}

	failed, err := failedRules(pick(schemas), v)
	if err != nil {
		c.problems = append(c.problems, ef.problem(checkSchema, codeSchemaInvalid, nil, "%s: %v", ef.path, err))

		return false
	}

	for _, l := range failed {
		c.problems = appendNew(c.problems, ef.problem(checkSchema, l.code, l.path, "%s: %s", ef.path, l.message))
	}

	return len(failed) == 0
}

// evalName checks that the file name of a judge or case is a slug, and returns it.
func (c *checker) evalName(p string) (string, bool) {
	name := strings.TrimSuffix(path.Base(p), path.Ext(p))
	if evalName.MatchString(name) {
		return name, true
	}

	c.problems = append(c.problems, evalFile{path: p}.problem(checkEvals, codeEvalNameInvalid, nil,
		"%s: the file name %q is the name others refer to it by, so it must be a slug: "+
			"3 to 40 lowercase letters, digits and hyphens, starting with a letter", p, name))

	return "", false
}

// evalFiles lists the files of dir with one of exts, sorted; none when dir does not exist.
// Hidden files and subfolders are skipped.
func evalFiles(files fs.FS, dir string, exts ...string) []string {
	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		return nil
	}

	var out []string

	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !slices.Contains(exts, path.Ext(e.Name())) {
			continue
		}

		out = append(out, path.Join(dir, e.Name()))
	}

	return out
}
