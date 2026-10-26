package authoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/factorymodel"
)

// CheckStatus is how one validation step went.
type CheckStatus string

// Check statuses.
const (
	CheckPassed  CheckStatus = "passed"
	CheckFailed  CheckStatus = "failed"  // it found error problems
	CheckWarning CheckStatus = "warning" // it found warnings only
)

// CheckResult is one line of Review & publish's checks list. Checks that did not run (those
// after a factory.yaml that fails the schema, and those of phases not wired yet) are omitted,
// never shown as passed.
type CheckResult struct {
	ID      string
	Section string
	Status  CheckStatus
	Summary string
}

// Counts sums a definition up.
type Counts struct {
	Stations, Agents, Skills, Connections, Contracts int
	// Changes is the number of changes from the draft's base (or starting tree); 0 when there
	// is none to compare with.
	Changes int
}

// Validation is the result of validating a definition at one revision.
type Validation struct {
	// OK reports that no problem is an error: the definition can be published.
	OK bool
	// Revision is the draft revision validated; 0 for files outside a draft.
	Revision int64
	Counts   Counts
	Checks   []CheckResult
	Problems []domain.DefinitionProblem
}

// Checks of this package, besides config's (yaml, schema, line, references, tools, removed,
// availability).
const (
	CheckName  = "name"
	CheckItems = "items"
	// CheckConnections is the Credentials check: what a definition signs in with exists in the
	// organization and fits (spec 4 §4.4).
	CheckConnections = "connections"
)

// Problem codes of this package's checks.
const (
	// CodeNameTakenProblem: another factory of the organization has the name.
	CodeNameTakenProblem = "name-taken"
	// CodeFactoryIDChanged: metadata.id is not the factory's id; it never changes.
	CodeFactoryIDChanged = "factory-id-changed"
	// CodeItemsAtRemovedStation: open items sit at a station the draft removes. Items in
	// progress must finish or be stopped first; the others need a station move, offered as
	// fixes on the publish request.
	CodeItemsAtRemovedStation = "items-at-removed-station"
)

// FixSectionPublish is the Section of a fix that patches the publish request instead of the
// model: its patch adds /station_moves/<removed station> = "<station>".
const FixSectionPublish = "publish"

// check ids of config, in the order it runs them.
const (
	checkYAML   = "yaml"
	checkSchema = "schema"
)

var configChecks = []string{"line", "references", "tools", "contracts", "removed", "availability"}

var checkSections = map[string]string{
	checkYAML: domain.SectionFiles, checkSchema: domain.SectionFiles, "line": domain.SectionLine,
	"references": domain.SectionAgents, CheckName: domain.SectionBasics, CheckItems: domain.SectionLine,
	"tools": domain.SectionBasics, "contracts": domain.SectionFiles, "removed": domain.SectionBasics,
	"availability": domain.SectionConnections, CheckConnections: domain.SectionConnections,
}

// candidate is a definition to validate and what it is validated against.
type candidate struct {
	// factory is the factory the definition belongs to; "" for files outside any factory.
	factory  domain.FactoryID
	revision int64
	files    *tree
	// current is the version running now, whose stations open items may sit at; nil when none.
	current *tree
	// reference is what changes are counted from (the base, or the starting tree); nil when none.
	reference *tree
	// moves are the station moves a publish carries.
	moves map[string]string
}

// checked is a validated definition with what was read on the way.
type checked struct {
	Validation

	factory    *config.Factory
	model      Model
	ranges     Ranges
	comparison Comparison
}

// validate runs every check, in order: config's (yaml, schema, line, references, tools,
// removed, availability), name, items, then credentials (connections).
func (s *Service) validate(ctx context.Context, c candidate) (checked, error) {
	f, problems := config.Check(c.files)
	if c.files.err != nil {
		return checked{}, c.files.err
	}

	out := checked{factory: f}

	var err error
	if out.model, out.ranges, err = lenientModel(c.files); err != nil {
		return checked{}, err
	}

	ran := configRan(f, problems)

	if f != nil {
		more, err := s.ownChecks(ctx, c, f, out.model)
		if err != nil {
			return checked{}, err
		}

		problems = append(problems, more.problems...)
		ran = append(ran, more.ran...)
	}

	if c.files.err != nil {
		return checked{}, c.files.err
	}

	if c.reference != nil {
		out.comparison = factorymodel.Diff(
			factorymodel.Snapshot{Manifest: c.reference.manifest, Files: c.reference},
			factorymodel.Snapshot{Manifest: c.files.manifest, Files: c.files},
		)
		if err := errors.Join(c.reference.err, c.files.err); err != nil {
			return checked{}, err
		}
	}

	if problems == nil {
		problems = []domain.DefinitionProblem{}
	}

	out.Validation = Validation{
		OK:       !slices.ContainsFunc(problems, domain.DefinitionProblem.Blocking),
		Revision: c.revision,
		Counts:   counts(f, out.model, out.comparison),
		Checks:   results(ran, problems, f, out.model),
		Problems: problems,
	}

	return out, nil
}

// lenientModel reads the model of a tree's factory.yaml; an empty model when it has none or it
// does not parse (yaml-invalid is config's problem then).
func lenientModel(t *tree) (Model, Ranges, error) {
	doc, ok, err := t.file(config.FileName)
	if err != nil {
		return Model{}, nil, err
	}

	if ok {
		if m, r, err := factorymodel.FromYAML(doc, t); err == nil {
			return m, r, nil
		}
	}

	return Model{Connections: []any{}}, Ranges{}, nil
}

// configRan lists the checks of config.Check that ran: yaml always, schema once factory.yaml
// parses, the others once it satisfies the schema.
func configRan(f *config.Factory, problems []domain.DefinitionProblem) []string {
	ran := []string{checkYAML}
	if f != nil || slices.ContainsFunc(problems, func(p domain.DefinitionProblem) bool { return p.Check == checkSchema }) {
		ran = append(ran, checkSchema)
	}

	if f != nil {
		ran = append(ran, configChecks...)
	}

	return ran
}

type ownResult struct {
	problems []domain.DefinitionProblem
	ran      []string
}

// ownChecks runs the name, items and credentials checks on a factory that loaded.
func (s *Service) ownChecks(ctx context.Context, c candidate, f *config.Factory, m Model) (ownResult, error) {
	var out ownResult

	p, err := s.nameProblems(ctx, c.factory, f)
	if err != nil {
		return ownResult{}, err
	}

	out.problems, out.ran = append(out.problems, p...), append(out.ran, CheckName)

	if c.current != nil && c.factory != "" && s.loads != nil {
		p, err := s.itemProblems(ctx, c, f, m)
		if err != nil {
			return ownResult{}, err
		}

		out.problems, out.ran = append(out.problems, p...), append(out.ran, CheckItems)
	}

	if s.credentials != nil {
		p, err := s.credentials.Check(ctx, f)
		if err != nil {
			return ownResult{}, fmt.Errorf("check connections: %w", err)
		}

		out.problems, out.ran = append(out.problems, p...), append(out.ran, CheckConnections)
	}

	return out, nil
}

// nameProblems reports a name another factory of the organization has, and a metadata.id that
// is not the factory's.
func (s *Service) nameProblems(ctx context.Context, factory domain.FactoryID, f *config.Factory) ([]domain.DefinitionProblem, error) {
	var out []domain.DefinitionProblem

	if factory != "" && domain.FactoryID(f.Metadata.ID) != factory {
		p := f.Locate("/metadata/id").Problem(CheckName, CodeFactoryIDChanged, domain.SeverityError,
			fmt.Sprintf("metadata.id must stay %s, the factory's id", factory))
		p.Fixes = []domain.ProblemFix{replaceFix("Restore the factory's id", domain.SectionBasics, "/id", string(factory))}
		out = append(out, p)
	}

	recs, err := s.store.FactoryRecords(ctx)
	if err != nil {
		return nil, fmt.Errorf("list factories: %w", err)
	}

	// Files outside a draft (factory empty) are the factory their metadata.id names, if any: its
	// name is not another factory's.
	for _, r := range recs {
		if r.ID != factory && (factory != "" || string(r.ID) != f.Metadata.ID) && strings.EqualFold(r.Name, f.Metadata.Name) {
			out = append(out, f.Locate("/metadata/name").Problem(CheckName, CodeNameTakenProblem, domain.SeverityError,
				"another factory of the organization is named "+r.Name))

			break
		}
	}

	return out, nil
}

// itemProblems reports the open items at stations of the running version that the draft
// removes. The loads count items waiting in a queue and in progress; publish itself refuses
// any other open item a move does not map.
func (s *Service) itemProblems(ctx context.Context, c candidate, f *config.Factory, m Model) ([]domain.DefinitionProblem, error) {
	removed, err := removedStations(c.current, m)
	if err != nil || len(removed) == 0 {
		return nil, err
	}

	loads, err := s.loads.StationLoads(ctx, c.factory)
	if err != nil {
		return nil, fmt.Errorf("read station loads: %w", err)
	}

	kept := stationIDs(m)
	loc := f.Locate("/spec/board/stations")

	var out []domain.DefinitionProblem

	for _, ld := range loads {
		if !slices.Contains(removed, ld.Station) {
			continue
		}

		if ld.InProgress > 0 {
			out = append(out, loc.Problem(CheckItems, CodeItemsAtRemovedStation, domain.SeverityError,
				fmt.Sprintf("the draft removes station %s, where %s in progress; let the runs finish or stop them first",
					ld.Station, plural(ld.InProgress, "item is", "items are"))))
		}

		if to, mapped := c.moves[ld.Station]; ld.Waiting > 0 && (!mapped || !slices.Contains(kept, to)) {
			p := loc.Problem(CheckItems, CodeItemsAtRemovedStation, domain.SeverityError,
				fmt.Sprintf("the draft removes station %s, where %s waiting; choose a station to move them to",
					ld.Station, plural(ld.Waiting, "item is", "items are")))
			p.Fixes = moveFixes(ld.Station, kept)
			out = append(out, p)
		}
	}

	return out, nil
}

// moveFixes offers one station move per station of the new line.
func moveFixes(from string, to []string) []domain.ProblemFix {
	fixes := make([]domain.ProblemFix, 0, len(to))

	for _, st := range to {
		v, _ := json.Marshal(st)
		fixes = append(fixes, domain.ProblemFix{
			Label: "Move them to " + st, Section: FixSectionPublish,
			Patch: []domain.PatchOp{{Op: domain.PatchAdd, Path: "/station_moves/" + escapePointer(from), Value: v}},
		})
	}

	return fixes
}

// removedStations lists the stations of the running version that m no longer has.
func removedStations(current *tree, m Model) ([]string, error) {
	doc, ok, err := current.file(config.FileName)
	if err != nil || !ok {
		return nil, err
	}

	cm, _, err := factorymodel.FromYAML(doc, nil)
	if err != nil {
		return nil, nil //nolint:nilerr // a running version that no longer parses has no stations to compare
	}

	kept := stationIDs(m)

	var out []string

	for _, id := range stationIDs(cm) {
		if !slices.Contains(kept, id) {
			out = append(out, id)
		}
	}

	return out, nil
}

// stationIDs lists the ids of a model's stations, in line order.
func stationIDs(m Model) []string {
	list, _ := m.Line["stations"].([]any)
	out := make([]string, 0, len(list))

	for _, st := range list {
		if sm, ok := st.(map[string]any); ok {
			if id, ok := sm["id"].(string); ok && id != "" {
				out = append(out, id)
			}
		}
	}

	return out
}

func counts(f *config.Factory, m Model, c Comparison) Counts {
	length := func(v any) int {
		l, _ := v.([]any)

		return len(l)
	}

	out := Counts{
		Stations:    length(m.Line["stations"]),
		Agents:      length(m.Agents["agents"]),
		Skills:      length(m.Agents["skills"]),
		Connections: len(m.Connections),
		Changes:     c.Counts.Changes,
	}

	if f != nil {
		out.Contracts = len(f.Spec.Contracts)
	}

	return out
}

// results builds the checks list from the checks that ran and their problems.
func results(ran []string, problems []domain.DefinitionProblem, f *config.Factory, m Model) []CheckResult {
	out := make([]CheckResult, 0, len(ran))

	for _, id := range ran {
		var errs, warns int

		section := checkSections[id]

		for _, p := range problems {
			if p.Check != id {
				continue
			}

			if section == "" {
				section = p.Section
			}

			switch p.Severity {
			case domain.SeverityError:
				errs++
			case domain.SeverityWarning:
				warns++
			case domain.SeverityInfo:
			}
		}

		if section == "" {
			section = domain.SectionFiles
		}

		r := CheckResult{ID: id, Section: section, Status: CheckPassed, Summary: passedSummary(id, f, m)}

		switch {
		case errs > 0:
			r.Status, r.Summary = CheckFailed, plural(errs, "error", "errors")
			if warns > 0 {
				r.Summary += ", " + plural(warns, "warning", "warnings")
			}
		case warns > 0:
			r.Status, r.Summary = CheckWarning, plural(warns, "warning", "warnings")
		}

		out = append(out, r)
	}

	return out
}

// staticSummaries are the green lines of checks whose summary needs no data.
var staticSummaries = map[string]string{
	checkYAML:      "Every file parses",
	checkSchema:    "factory.yaml matches the schema",
	"references":   "Every agent, skill, contract, connection and prompt it names exists",
	"tools":        "Every tool is a mise tool spec",
	"removed":      "Uses none of the removed forms",
	CheckItems:     "No open items at a removed station",
	"availability": "Everything it declares is available on this install",
}

// passedSummary is the green line of a check that found nothing.
func passedSummary(id string, f *config.Factory, m Model) string {
	if s, ok := staticSummaries[id]; ok {
		return s
	}

	switch id {
	case "line":
		ids := stationIDs(m)
		if len(ids) == 0 {
			return "The line is well formed"
		}

		return fmt.Sprintf("%s, ends with %s, every agent station has an agent",
			plural(len(ids), "station", "stations"), ids[len(ids)-1])
	case CheckName:
		return fmt.Sprintf("The name %s is free", f.Metadata.Name)
	case CheckConnections:
		return connectionsSummary(f)
	}

	return "Passed"
}

// connectionsSummary counts what the connections check found: the organization's connections
// and knowledge sources the definition names.
func connectionsSummary(f *config.Factory) string {
	names := f.ConnectionNames()

	var parts []string
	if len(names) > 0 {
		parts = append(parts, plural(len(names), "connection exists", "connections exist"))
	}

	if k := f.Spec.Knowledge; k != nil && len(k.Sources) > 0 {
		parts = append(parts, plural(len(k.Sources), "knowledge source exists", "knowledge sources exist"))
	}

	if len(parts) == 0 {
		return "Names no connection"
	}

	return strings.Join(parts, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}

	return strconv.Itoa(n) + " " + many
}

// replaceFix is a fix replacing one value of a model section.
func replaceFix(label, section, pointer string, v any) domain.ProblemFix {
	b, _ := json.Marshal(v)

	return domain.ProblemFix{Label: label, Section: section, Patch: []domain.PatchOp{{Op: domain.PatchReplace, Path: pointer, Value: b}}}
}

func escapePointer(tok string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(tok)
}
