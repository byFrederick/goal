package gateway

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Jonah reads what a Jonah review's bundle holds besides its definition files (ADR 0034
// decision 1, ADR 0039 decisions 4 and 5): the factory's sent-back events in the review's
// window, its proposals, its open observations, the items and station medians its evidence is built from (ADR 0039 decision 2), and
// what its review runs spent. ports.JonahStore satisfies it.
type Jonah interface {
	SentBackBetween(ctx context.Context, factory domain.FactoryID, since, until time.Time, limit int) ([]domain.SentBack, error)
	Proposals(ctx context.Context, q ports.ProposalQuery) ([]domain.Proposal, error)
	OpenObservations(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.Observation, error)
	EvidenceItems(ctx context.Context, factory domain.FactoryID, since, until time.Time, limit int) ([]domain.EvidenceItem, error)
	StationRunMedians(ctx context.Context, factory domain.FactoryID, since, until time.Time) ([]domain.StationMedian, error)
	JonahSpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (float64, error)
}

// errNoJonah answers a jonah token's bundle when the gateway has no Deps.Jonah: 503.
var errNoJonah = errors.New("the gateway has no jonah store")

const (
	// jonahEventLimit caps the sent-back events a review reads.
	jonahEventLimit = 200
	// decidedWithin is how far back a review reads the factory's rejected and published proposals.
	decidedWithin = 90 * 24 * time.Hour
	// proposalPage is how many proposals one Proposals call reads; proposalScan caps how many of
	// one state are read in all and proposalLimit how many of one state the bundle holds.
	proposalPage  = 50
	proposalScan  = 500
	proposalLimit = 50
	// evidenceItemLimit caps the finished items the evidence reads, the latest finished first, and
	// evidenceVersionLimit the factory's latest versions read for versions.md and the runs' numbers.
	evidenceItemLimit    = 200
	evidenceVersionLimit = 100
	// observationLimit caps the open observations the bundle holds, the latest updated first.
	observationLimit = 200
	// The bundle's folders and files, under the box's /goal (apps/worker/README.md, jonah mode):
	// the version's files, the evidence, the proposals, the index of the version's files, the
	// sent-back events a proposal cites and the open observations a review updates.
	definitionDir    = "definition/"
	evidenceDir      = "evidence/"
	proposalsDir     = "proposals/"
	filesIndex       = "files.md"
	eventsFile       = "events.md"
	observationsFile = "observations.md"
)

// bundleJonah is spec.jonah, what goal-agent's jonah mode reads (apps/worker/src/spec.ts Jonah):
// everything else a review reads is a file of the bundle.
type bundleJonah struct {
	// ProposeAfter is the policy's proposeAfter; Manual whether a person asked for the review.
	ProposeAfter int  `json:"propose_after"`
	Manual       bool `json:"manual"`
}

// jonahBundle is the bundle of a Jonah review's run (ADR 0039 decision 4), as files under the
// box's /goal: every text file of c's version under definitionDir with their sha256 in filesIndex,
// the factory's sent-back events in c's window in eventsFile, its open proposals and those
// published or rejected within decidedWithin of now under proposalsDir, its open observations in
// observationsFile, and the evidence under evidenceDir. Its spec has the line's knowledge that applies to every run, pinned by c, and the
// weekly budget left as max_usd. It names no item, repository, connection, tool or rule, and
// records nothing. The version must be c's factory's in c's organization (ctx).
func (g *Gateway) jonahBundle(ctx context.Context, c domain.TaskClaims) (runBundle, error) {
	if g.jonah == nil {
		return runBundle{}, errNoJonah
	}

	f, err := g.defs.Version(ctx, c.VersionID)
	if err != nil {
		return runBundle{}, fmt.Errorf("load factory %s version %s: %w", c.FactoryID, c.VersionID, err)
	}

	if f.Metadata.ID != string(c.FactoryID) {
		return runBundle{}, fmt.Errorf("%w: version %s is not a version of factory %s", errForbidden, c.VersionID, c.FactoryID)
	}

	line := f.Line()

	files, err := g.jonahEvidence(ctx, c, line)
	if err != nil {
		return runBundle{}, err
	}

	for _, add := range []func(context.Context, domain.TaskClaims, map[string][]byte) error{g.jonahDefinition, g.jonahEvents, g.jonahProposals, g.jonahObservations} {
		if err := add(ctx, c, files); err != nil {
			return runBundle{}, err
		}
	}

	// A review is no agent of the line: it reads the knowledge that applies to every run.
	k, err := g.knowledgeOf(ctx, c, line, domain.AgentSpec{}, domain.RuleTarget{}, true)
	if err != nil {
		return runBundle{}, fmt.Errorf("knowledge of factory %s: %w", c.FactoryID, err)
	}

	left, err := g.jonahBudgetLeft(ctx, c.FactoryID, line.Jonah)
	if err != nil {
		return runBundle{}, err
	}

	return runBundle{
		Spec: bundleSpec{
			FactoryID: c.FactoryID, RunID: c.RunID, Agent: c.Agent, Model: cmp.Or(c.Model, line.Jonah.Model), Purpose: line.Purpose,
			MaxUSD: left, Knowledge: k, Skills: []string{}, ToolsAllow: []string{}, ToolsDeny: []string{}, Connections: []bundleConnection{},
			Rules: []bundleRule{}, GrantedCalls: []grantedCall{}, Jonah: &bundleJonah{ProposeAfter: line.Jonah.ProposeAfter, Manual: c.Manual},
		},
		Files: files,
		Git:   []gitCredential{},
		Tools: []string{},
	}, nil
}

// jonahBudgetLeft is what the policy's weekly budget has left after the factory's review runs of
// the last domain.JonahBudgetWindow, this one's spend so far included; at least a cent, so the cap
// is never absent (max_usd 0 is no cap).
func (g *Gateway) jonahBudgetLeft(ctx context.Context, factory domain.FactoryID, p domain.JonahPolicy) (float64, error) {
	spent, err := g.jonah.JonahSpendSince(ctx, factory, g.clock.Now().Add(-domain.JonahBudgetWindow))
	if err != nil {
		return 0, fmt.Errorf("jonah spend of factory %s: %w", factory, err)
	}

	return max(p.WeeklyUSD-spent, 0.01), nil
}

// jonahDefinition adds every file of c's version that is UTF-8 text under definitionDir, and
// filesIndex: each one's path, its sha256 (a proposal's base_sha256) and whether a proposal may
// edit it (domain.ProposalPathAllowed). A file that is not text is left out of both.
func (g *Gateway) jonahDefinition(ctx context.Context, c domain.TaskClaims, out map[string][]byte) error {
	files, err := g.defs.VersionFiles(ctx, c.VersionID)
	if err != nil {
		return fmt.Errorf("files of version %s: %w", c.VersionID, err)
	}

	var b strings.Builder

	b.WriteString("# The definition's files\n\nEach file of the current version is under `/goal/definition/`. A proposal edits only a file marked yes: an existing file by exact replacements, whose old text occurs exactly once in it, and a new file by its whole content.\n\n")
	b.WriteString("| Path | sha256 | A proposal may edit it |\n|---|---|---|\n")

	for _, p := range slices.Sorted(maps.Keys(files)) {
		if !utf8.Valid(files[p]) {
			continue
		}

		out[definitionDir+p] = files[p]

		editable := "no"
		if domain.ProposalPathAllowed(p) {
			editable = "yes"
		}

		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", p, domain.HashBlob(files[p]), editable)
	}

	out[filesIndex] = []byte(b.String())

	return nil
}

// jonahEvents adds eventsFile: the factory's sent-back touches in c's window, oldest first, each
// with the id a proposal's evidence cites.
func (g *Gateway) jonahEvents(ctx context.Context, c domain.TaskClaims, out map[string][]byte) error {
	sent, err := g.jonah.SentBackBetween(ctx, c.FactoryID, time.Unix(c.WindowStart, 0), time.Unix(c.WindowEnd, 0), jonahEventLimit)
	if err != nil {
		return fmt.Errorf("sent-back events of factory %s: %w", c.FactoryID, err)
	}

	var b strings.Builder

	b.WriteString("# Sent-back events\n\nA person's or a gate's send-backs in the review's window, oldest first. A proposal's evidence names an event by its id.\n")

	if len(sent) == 0 {
		b.WriteString("\nThere are none in the window.\n")
	}

	for _, s := range sent {
		fmt.Fprintf(&b, "\n## Event %d\n\n- Item: #%d %s\n- Station: %s\n- Agent: %s\n- Kind: %s\n", s.EventID, s.ItemNumber,
			oneLine(s.ItemTitle), cmp.Or(s.Station, "none"), cmp.Or(s.Agent, "none"), s.Kind)

		if s.Reason != "" {
			fmt.Fprintf(&b, "- Reason:\n\n%s\n", quoted(s.Reason))
		}
	}

	out[eventsFile] = []byte(b.String())

	return nil
}

// jonahProposals adds a file under proposalsDir for each of the factory's open proposals and each
// one published or rejected within decidedWithin of now, with its reason: proposals/<state>/<number>.md.
func (g *Gateway) jonahProposals(ctx context.Context, c domain.TaskClaims, out map[string][]byte) error {
	since := g.clock.Now().Add(-decidedWithin)

	for _, state := range []domain.ProposalState{domain.ProposalProposed, domain.ProposalPublished, domain.ProposalRejected} {
		ps, err := g.proposalsOf(ctx, c.FactoryID, state, since)
		if err != nil {
			return err
		}

		for _, p := range ps {
			out[proposalsDir+string(state)+"/"+strconv.FormatInt(p.Number, 10)+".md"] = proposalFile(p)
		}
	}

	return nil
}

// proposalsOf are the factory's proposals in state, newest numbered first, at most proposalLimit of
// the latest proposalScan: an open one whatever its age, a decided one when decided at or after
// since. A proposal's number orders its creation, not its decision, so every page up to the scan
// cap is read rather than stopping at the first old decision.
func (g *Gateway) proposalsOf(ctx context.Context, factory domain.FactoryID, state domain.ProposalState, since time.Time) ([]domain.Proposal, error) {
	var out []domain.Proposal

	for before, scanned := int64(0), 0; scanned < proposalScan && len(out) < proposalLimit; {
		page, err := g.jonah.Proposals(ctx, ports.ProposalQuery{Factory: factory, State: state, Before: before, Limit: proposalPage})
		if err != nil {
			return nil, fmt.Errorf("%s proposals of factory %s: %w", state, factory, err)
		}

		for _, p := range page {
			if (state == domain.ProposalProposed || !p.DecidedAt.Before(since)) && len(out) < proposalLimit {
				out = append(out, p)
			}
		}

		if len(page) < proposalPage {
			break
		}

		scanned += len(page)
		before = page[len(page)-1].Number
	}

	return out, nil
}

// jonahObservations adds observationsFile: the factory's open (watching or proposed)
// observations, the latest updated first, each with the id a review's observation gives to
// update it, its count so far and its items (ADR 0039 decision 5).
func (g *Gateway) jonahObservations(ctx context.Context, c domain.TaskClaims, out map[string][]byte) error {
	obs, err := g.jonah.OpenObservations(ctx, c.FactoryID, observationLimit)
	if err != nil {
		return fmt.Errorf("open observations of factory %s: %w", c.FactoryID, err)
	}

	var b strings.Builder

	b.WriteString("# Open observations\n\nThe causes earlier reviews saw and still watch or propose. An observation of the same cause gives its id, and its count adds to the count so far.\n")

	if len(obs) == 0 {
		b.WriteString("\nNone yet.\n")
	}

	for _, o := range obs {
		fmt.Fprintf(&b, "\n## %s\n\n- id: `%s`\n- State: %s\n- Count so far: %d\n- Items: %s\n", oneLine(o.Summary), o.ID, o.State, o.Count,
			g.itemRefs(ctx, o.ItemIDs))

		if o.Detail != "" {
			fmt.Fprintf(&b, "\n%s\n", quoted(o.Detail))
		}
	}

	out[observationsFile] = []byte(b.String())

	return nil
}

// observationItems caps the items observationsFile names per observation.
const observationItems = 20

// itemRefs are ids as #<number>, the first observationItems of them and how many more; an item
// that cannot be read keeps its id.
func (g *Gateway) itemRefs(ctx context.Context, ids []domain.ItemID) string {
	if len(ids) == 0 {
		return "none"
	}

	refs := make([]string, 0, min(len(ids), observationItems))

	for _, id := range ids[:min(len(ids), observationItems)] {
		ref := string(id)
		if g.items != nil {
			if it, err := g.items.Item(ctx, id); err == nil {
				ref = "#" + strconv.FormatInt(it.Number, 10)
			}
		}

		refs = append(refs, ref)
	}

	if more := len(ids) - len(refs); more > 0 {
		refs = append(refs, fmt.Sprintf("and %d more", more))
	}

	return strings.Join(refs, ", ")
}

// proposalFile is p as a review reads it: its number, title, state, files and summary, and when it
// was decided, the version it published or the reason a person rejected it.
func proposalFile(p domain.Proposal) []byte {
	var b strings.Builder

	fmt.Fprintf(&b, "# Proposal #%d: %s\n\n- State: %s\n", p.Number, oneLine(p.Title), p.State)

	if !p.DecidedAt.IsZero() {
		fmt.Fprintf(&b, "- Decided: %s\n", p.DecidedAt.UTC().Format(time.DateOnly))
	}

	if p.PublishedVersionID != "" {
		fmt.Fprintf(&b, "- Published as version %s\n", p.PublishedVersionID)
	}

	fmt.Fprintf(&b, "- Files: %s\n", strings.Join(nonNil(p.Paths()), ", "))

	if p.RejectReason != "" {
		fmt.Fprintf(&b, "- Rejected because:\n\n%s\n", quoted(p.RejectReason))
	}

	if p.Summary != "" {
		fmt.Fprintf(&b, "\n## Summary\n\n%s\n", quoted(p.Summary))
	}

	return []byte(b.String())
}

// oneLine is s on one line, its line breaks spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// quoted is s as a markdown block quote, each line of it quoted.
func quoted(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}

	return strings.Join(lines, "\n")
}

// jonahEvidence is the evidence folder of c's window (domain.BuildEvidence) as bundle files under
// evidenceDir, in a new map the bundle's other files join: the items finished in the window, the stations' medians over the
// domain.EvidenceMedianWindow before its end and the factory's latest versions, read through
// Deps.Drafts (none without it).
func (g *Gateway) jonahEvidence(ctx context.Context, c domain.TaskClaims, line domain.Line) (map[string][]byte, error) {
	since, until := time.Unix(c.WindowStart, 0).UTC(), time.Unix(c.WindowEnd, 0).UTC()

	items, err := g.jonah.EvidenceItems(ctx, c.FactoryID, since, until, evidenceItemLimit)
	if err != nil {
		return nil, fmt.Errorf("evidence items of factory %s: %w", c.FactoryID, err)
	}

	medians, err := g.jonah.StationRunMedians(ctx, c.FactoryID, until.Add(-domain.EvidenceMedianWindow), until)
	if err != nil {
		return nil, fmt.Errorf("station run medians of factory %s: %w", c.FactoryID, err)
	}

	var versions []domain.DefinitionVersion
	if g.drafts != nil {
		if versions, err = g.drafts.Versions(ctx, c.FactoryID, 0, evidenceVersionLimit); err != nil {
			return nil, fmt.Errorf("versions of factory %s: %w", c.FactoryID, err)
		}
	}

	stations := make([]string, len(line.Stations))
	for i, st := range line.Stations {
		stations[i] = st.ID
	}

	evidence := domain.BuildEvidence(domain.EvidenceInput{
		Since: since, Until: until, Stations: stations, Items: items, Medians: medians, Versions: versions,
	})

	files := make(map[string][]byte, len(evidence))
	for p, body := range evidence {
		files[evidenceDir+p] = body
	}

	return files, nil
}
