package jonah

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// RunTargets finds the run a try of an item at a station reruns; ports.PastTries satisfies it.
type RunTargets interface {
	PastTryTarget(ctx context.Context, item domain.ItemID, station string) (domain.Run, error)
}

// KeptReads lists a factory's kept tests; ports.KeptTests satisfies it.
type KeptReads interface {
	KeptTests(ctx context.Context, factory domain.FactoryID, station string) ([]domain.KeptTest, error)
}

// keptTarget is one kept test's try: the test and the run it reruns.
type keptTarget struct {
	test domain.KeptTest
	run  domain.Run
}

// try is t's try on line, of content key, attempt attempt, on model or else its agent's model.
func (t keptTarget) try(line domain.Line, key string, attempt int, model string) domain.NewPastTry {
	a, _ := line.Agent(t.run.Agent)

	return domain.NewPastTry{
		KeptTestID: t.test.ID, ContentKey: key, ItemID: t.test.ItemID, Attempt: attempt, Station: t.test.Station, OriginalRunID: t.run.ID,
		Model: cmp.Or(model, a.Model, line.Runtime.Model),
	}
}

// keptUntried is a kept test at a reached station that can't be tried, and why.
type keptUntried struct {
	test domain.KeptTest
	why  string
}

// keptTargets are the factory's kept tests at line's reached stations (ADR 0039 decisions 11 and
// 12), each with the run its try reruns: its item's newest first run of a claim there. A test
// whose station's agents can't be tried, whose item has no such run or whose run's agent can't be
// tried is untried, with why.
func keptTargets(ctx context.Context, kept KeptReads, runs RunTargets, f domain.FactoryID, line domain.Line, reached []string,
) ([]keptTarget, []keptUntried, error) {
	if len(reached) == 0 {
		return nil, nil, nil
	}

	tests, err := kept.KeptTests(ctx, f, "")
	if err != nil {
		return nil, nil, fmt.Errorf("read the kept tests: %w", err)
	}

	var (
		out     []keptTarget
		untried []keptUntried
	)

	for _, k := range tests {
		if !slices.Contains(reached, k.Station) {
			continue
		}

		if !stationTriable(line, k.Station) {
			untried = append(untried, keptUntried{k, fmt.Sprintf("station %s's agent can't be tried: it needs MCP servers or secrets", k.Station)})

			continue
		}

		run, err := runs.PastTryTarget(ctx, k.ItemID, k.Station)

		switch {
		case errors.Is(err, ports.ErrNotFound):
			untried = append(untried, keptUntried{k, fmt.Sprintf("item #%d has no finished first run at %s to rerun", k.ItemNumber, k.Station)})
		case err != nil:
			return nil, nil, fmt.Errorf("find the run of item %s at %s: %w", k.ItemID, k.Station, err)
		case !line.TriableAgent(run.Agent):
			untried = append(untried, keptUntried{k, fmt.Sprintf("agent %s can't be tried: it needs MCP servers or secrets", run.Agent)})
		default:
			out = append(out, keptTarget{test: k, run: run})
		}
	}

	return out, untried, nil
}

// DraftChange is a factory's open draft as a change of its current version: the files it changes
// or adds as whole-file edits (each changed one with its current file's hash), the paths it
// deletes, and domain.ContentKey of its files. Version is empty before the first publish.
type DraftChange struct {
	Version    domain.DefinitionVersionID
	Edits      []domain.ProposalEdit
	Deleted    []string
	ContentKey string
}

// Drafts reads a factory's open draft as a change, for the organization in ctx; api.JonahAuthoring
// satisfies it. An error wrapping authoring.ErrNoDraft when none is open.
type Drafts interface {
	DraftChange(ctx context.Context, f domain.FactoryID) (DraftChange, error)
}

// Spend reads Jonah's spend; ports.JonahStore satisfies it.
type Spend interface {
	JonahSpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (domain.JonahSpend, error)
}

// KeptDraftDeps are the ports a publish's kept tests work through. Log may be nil.
type KeptDraftDeps struct {
	Drafts      Drafts
	Definitions TryDefinitions
	Kept        ports.KeptTests
	Tries       ports.PastTries
	Spend       Spend
	Clock       ports.Clock
	Log         *slog.Logger
}

// KeptDrafts runs a factory's kept tests on its open draft when the publish dialog opens (ADR
// 0039 decision 12): a try of each kept test at a station the draft reaches, on the current
// version with the draft's files, queued as a publish's try, which the try loop claims before a
// proposal's. A failing test warns; publishing never reads these results.
type KeptDrafts struct {
	d   KeptDraftDeps
	log *slog.Logger
}

// NewKeptDrafts returns a publish's kept tests over d.
func NewKeptDrafts(d KeptDraftDeps) *KeptDrafts {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &KeptDrafts{d: d, log: log}
}

// DraftTests is how the open draft does on the kept tests it reaches: State as a proposal's tried
// has it, over the tests' tries, and each test.
type DraftTests struct {
	State TriedState
	Tests []DraftTest
}

// DraftTest is one kept test on the draft: Result is "" until its try was queued, then running,
// passed, failed or error, with the reason of its grade, the error of a try that could not run, or
// why it can't be tried, and the try's run.
type DraftTest struct {
	Test   domain.KeptTest
	Result domain.KeptTestResult
	Reason string
	Error  string
	RunID  domain.RunID
}

// Start queues a try of each kept test the open draft reaches that has none of the draft's
// content, from a proposal or an earlier publish, and returns the tests as Read does. A test that
// can't be tried is answered error with why and queues nothing.
func (k *KeptDrafts) Start(ctx context.Context, f domain.FactoryID) (DraftTests, error) {
	return k.tests(ctx, f, true)
}

// Read is the kept tests the open draft reaches with their results, and starts nothing.
func (k *KeptDrafts) Read(ctx context.Context, f domain.FactoryID) (DraftTests, error) {
	return k.tests(ctx, f, false)
}

func (k *KeptDrafts) tests(ctx context.Context, f domain.FactoryID, start bool) (DraftTests, error) {
	ch, err := k.d.Drafts.DraftChange(ctx, f)
	if err != nil {
		return DraftTests{}, fmt.Errorf("kept tests of the draft: %w", err)
	}

	out := DraftTests{State: TriedDone, Tests: []DraftTest{}}
	if ch.Version == "" {
		return out, nil
	}

	current, err := k.currentLine(ctx, f)
	if err != nil {
		return DraftTests{}, err
	}

	targets, untried, err := k.targets(ctx, f, ch, current)
	if err != nil {
		return DraftTests{}, err
	}

	if start && len(targets) > 0 {
		if err := k.queue(ctx, f, ch, targets); err != nil {
			return DraftTests{}, err
		}
	}

	return k.results(ctx, f, ch.ContentKey, current, targets, untried)
}

// currentLine is f's current line, whose Jonah policy runs the tries.
func (k *KeptDrafts) currentLine(ctx context.Context, f domain.FactoryID) (domain.Line, error) {
	lines, err := k.d.Definitions.CurrentLines(ctx)
	if err != nil {
		return domain.Line{}, fmt.Errorf("kept tests of the draft: read the lines: %w", err)
	}

	for _, l := range lines {
		if l.FactoryID == f {
			return l, nil
		}
	}

	return domain.Line{}, fmt.Errorf("kept tests of the draft: factory %s has no current line: %w", f, ports.ErrNotFound)
}

// targets are the kept tests ch reaches, with the runs their tries rerun, and those that can't be
// tried with why: every kept test when ch deletes a file or does not load, and every reached one
// while Jonah is off.
func (k *KeptDrafts) targets(ctx context.Context, f domain.FactoryID, ch DraftChange, current domain.Line,
) ([]keptTarget, []keptUntried, error) {
	all := func(why string) ([]keptTarget, []keptUntried, error) {
		tests, err := k.d.Kept.KeptTests(ctx, f, "")
		if err != nil {
			return nil, nil, fmt.Errorf("kept tests of the draft: read the kept tests: %w", err)
		}

		untried := make([]keptUntried, len(tests))
		for i, t := range tests {
			untried[i] = keptUntried{t, why}
		}

		return nil, untried, nil
	}

	if len(ch.Deleted) > 0 {
		return all(fmt.Sprintf("the draft deletes %s, and a kept test can't be tried on a draft that deletes a file", strings.Join(ch.Deleted, ", ")))
	}

	line, reached, err := k.d.Definitions.ProposedLine(ctx, ch.Version, ch.Edits)

	switch {
	case errors.Is(err, ports.ErrNotFound) || ctx.Err() != nil:
		return nil, nil, fmt.Errorf("kept tests of the draft: read version %s with the draft: %w", ch.Version, err)
	case err != nil:
		return all(fmt.Sprintf("the draft can't be tried: %v", err))
	}

	targets, untried, err := keptTargets(ctx, k.d.Kept, k.d.Tries, f, line, reached)
	if err != nil {
		return nil, nil, fmt.Errorf("kept tests of the draft: %w", err)
	}

	if !current.Jonah.Enabled {
		for _, t := range targets {
			untried = append(untried, keptUntried{t.test, "Jonah is off for this factory, so its kept tests don't run"})
		}

		return nil, untried, nil
	}

	return targets, untried, nil
}

// queue queues a publish's try of each target that has no try of ch's content yet.
func (k *KeptDrafts) queue(ctx context.Context, f domain.FactoryID, ch DraftChange, targets []keptTarget) error {
	have, err := k.d.Tries.KeptTestTries(ctx, f, ch.ContentKey)
	if err != nil {
		return fmt.Errorf("kept tests of the draft: read the tries: %w", err)
	}

	line, _, err := k.d.Definitions.ProposedLine(ctx, ch.Version, ch.Edits)
	if err != nil {
		return fmt.Errorf("kept tests of the draft: read version %s with the draft: %w", ch.Version, err)
	}

	var due []domain.NewPastTry

	for _, t := range targets {
		if !slices.ContainsFunc(have, func(p domain.PastTry) bool { return p.KeptTestID == t.test.ID }) {
			due = append(due, t.try(line, ch.ContentKey, 1, ""))
		}
	}

	if len(due) == 0 {
		return nil
	}

	queued, err := k.d.Tries.QueuePublishTries(ctx, f, ch.Version, ch.Edits, due, k.d.Clock.Now().UTC())
	if err != nil {
		return fmt.Errorf("kept tests of the draft: queue tries: %w", err)
	}

	k.log.InfoContext(ctx, "jonah kept tests queued", slog.String("factory", string(f)), slog.Int("tries", len(queued)))

	return nil
}

// results are the targets' results from the tries of key, and the untried as error, by station
// and then as the store lists the tests, with the state of the targets' tries under current's
// Jonah budget.
func (k *KeptDrafts) results(ctx context.Context, f domain.FactoryID, key string, current domain.Line, targets []keptTarget,
	untried []keptUntried,
) (DraftTests, error) {
	tries, err := k.d.Tries.KeptTestTries(ctx, f, key)
	if err != nil {
		return DraftTests{}, fmt.Errorf("kept tests of the draft: read the tries: %w", err)
	}

	spend, err := k.d.Spend.JonahSpendSince(ctx, f, k.d.Clock.Now().UTC().Add(-BudgetWindow))
	if err != nil {
		return DraftTests{}, fmt.Errorf("kept tests of the draft: read spend: %w", err)
	}

	out := DraftTests{Tests: make([]DraftTest, 0, len(targets)+len(untried))}

	var counted []domain.PastTry

	for _, t := range targets {
		of := slices.DeleteFunc(slices.Clone(tries), func(p domain.PastTry) bool { return p.KeptTestID != t.test.ID })
		counted = append(counted, of...)

		e := DraftTest{Test: t.test}
		if r, by, ok := domain.KeptResultOf(of); ok {
			e.Result, e.Reason, e.Error, e.RunID = r, by.Reason, by.Error, by.TryRunID
		}

		out.Tests = append(out.Tests, e)
	}

	for _, u := range untried {
		out.Tests = append(out.Tests, DraftTest{Test: u.test, Result: domain.KeptTestError, Error: u.why})
	}

	out.State = TriedDone
	if len(counted) > 0 {
		out.State = triedState(counted, current.Jonah.Budget(spend))
	}

	return out, nil
}
