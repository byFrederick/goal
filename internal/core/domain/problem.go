package domain

import "encoding/json"

// ProblemSeverity says whether a problem blocks publishing a definition.
type ProblemSeverity string

// Problem severities.
const (
	SeverityError   ProblemSeverity = "error"   // blocks publish
	SeverityWarning ProblemSeverity = "warning" // shown, does not block
	SeverityInfo    ProblemSeverity = "info"
)

// Sections of a factory definition: the Visual editor's tabs. A problem outside them (a key
// edited in YAML mode only, or another file) is in SectionFiles.
const (
	SectionBasics      = "basics"
	SectionLine        = "line"
	SectionAgents      = "agents"
	SectionConnections = "connections"
	SectionReview      = "review"
	SectionFiles       = "files"
)

// DefinitionProblem is one validation finding, located for both views of a definition: the
// Visual editor (Section and Pointer) and the YAML editor (File and the line range).
type DefinitionProblem struct {
	// Code is stable and kebab-case, e.g. "unknown-agent", "literal-secret".
	Code     string
	Severity ProblemSeverity
	// Check is the validation step that found it: yaml, schema, line, references, images,
	// secrets, or one another phase registers.
	Check   string
	Section string
	// Pointer is an RFC 6901 pointer into the factory model ("/line/stations/3/agent"); empty
	// when the problem has no Visual field.
	Pointer string
	// File is the factory-relative path the problem is in ("factory.yaml", "agents/engineer.md").
	File string
	// Line, Column, EndLine and EndColumn are 1-based; 0 when the problem is not in a file.
	Line, Column, EndLine, EndColumn int
	Message                          string
	Fixes                            []ProblemFix
}

// Blocking reports whether the problem stops the definition from being published or run.
func (p DefinitionProblem) Blocking() bool { return p.Severity == SeverityError }

// ProblemFix is a machine-applicable fix: a JSON Patch on one section of the factory model.
type ProblemFix struct {
	Label   string // "Set WIP to 2"
	Section string
	Patch   []PatchOp
}

// PatchOp is one RFC 6902 operation (add, remove or replace) on a section's value; Path is
// relative to that section.
type PatchOp struct {
	Op    string
	Path  string
	Value json.RawMessage
}

// Patch operations a ProblemFix uses.
const (
	PatchAdd     = "add"
	PatchRemove  = "remove"
	PatchReplace = "replace"
)
