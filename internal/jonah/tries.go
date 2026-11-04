package jonah

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// TryDefinitions reads the lines a try loop works on and what a proposal changes, for the
// organization in ctx; cmd/goald adapts definitions.Cache and authoring.Service to it.
type TryDefinitions interface {
	// CurrentLines are the current versions' lines of the organization's factories.
	CurrentLines(ctx context.Context) ([]domain.Line, error)
	// ProposedLine is version's line with edits applied to its files, and the agent stations of
	// it, in line order, that the change reaches (factorymodel.StationsReached). ErrNotFound when
	// the organization has no such version, domain.ErrEditDoesNotApply when an edit does not apply
	// to it.
	ProposedLine(ctx context.Context, version domain.DefinitionVersionID, edits []domain.ProposalEdit) (domain.Line, []string, error)
	// ValidateProposed checks output against contract as version with edits applied declares it
	// (ports.Contracts on the proposed files, ADR 0039 decision 10): an error wrapping
	// ports.ErrContractViolation when output does not satisfy it, and any other when it cannot be
	// checked, as for a contract the proposed version does not have.
	ValidateProposed(ctx context.Context, version domain.DefinitionVersionID, edits []domain.ProposalEdit, contract string,
		output json.RawMessage) error
	// ContentKey is domain.ContentKey of version's files with edits applied: what a kept test's
	// try records, so a publish of the same content reuses it (ADR 0039 decision 12). The errors
	// are ProposedLine's.
	ContentKey(ctx context.Context, version domain.DefinitionVersionID, edits []domain.ProposalEdit) (string, error)
}

// ItemEvents reads an item's events, paged; ports.Store satisfies it.
type ItemEvents interface {
	ItemEvents(ctx context.Context, id domain.ItemID, after int64, limit int) ([]domain.Event, error)
}

// WaitingItems reads the items waiting at a station; ports.Store satisfies it.
type WaitingItems interface {
	Waiting(ctx context.Context, factory domain.FactoryID, station string, limit int) ([]domain.Item, error)
}

// TryDeps are the ports the try loop works through. Log may be nil, and Kept nil tries no kept
// tests.
type TryDeps struct {
	Orgs        Orgs
	Jonah       ports.JonahStore
	Tries       ports.PastTries
	Kept        ports.KeptTests
	Definitions TryDefinitions
	Waiting     WaitingItems
	// Events reads the items' events a try at a gate station is graded by.
	Events ItemEvents
	Runner TaskRunner
	Clock  ports.Clock
	Log    *slog.Logger
}

// TryConfig sets the try loop's times and its cap.
type TryConfig struct {
	// Timeout bounds one try's run.
	Timeout time.Duration
	// Margin is the lease's time beyond the run, for reading and recording.
	Margin time.Duration
	// PerFactory is how many of a factory's tries run at once, on every goald together; 0 is
	// DefaultTriesPerFactory.
	PerFactory int
}

// DefaultTriesPerFactory is how many of a factory's tries run at once when TryConfig names none.
const DefaultTriesPerFactory = 2

// MaxTryAttempts is how many claims a try gets before it ends with verdict error: a try whose
// goald died twice more is not tried again.
const MaxTryAttempts = 3

// sweepProposals bounds the proposed proposals a sweep reads per factory.
const sweepProposals = 100

// Tries tries each proposal on past items before anyone sees it (ADR 0039 decisions 9 and 11):
// its sweep queues the tries, and its work runs them, each a run of kind try in a box with only
// the model key, through engine.RunTask. Any goald may run it: tries are claimed under a lease,
// and one whose goald died is claimed again when its lease ends.
type Tries struct {
	d   TryDeps
	c   TryConfig
	log *slog.Logger
}

// NewTries returns a try loop over d with the times and cap of c.
func NewTries(d TryDeps, c TryConfig) *Tries {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	if c.PerFactory <= 0 {
		c.PerFactory = DefaultTriesPerFactory
	}

	return &Tries{d: d, c: c, log: log}
}

// Sweep queues the tries of every proposed proposal of each Jonah-enabled factory of every live
// organization that has none: those that change what a triable station's agent gets
// (ProposedLine's reached stations whose agent domain.Line.TriableAgent accepts). Each such
// proposal gets one try per item it cites, at most domain.MaxPastTryItems, newest first, each of
// the item's newest run there that was a claim's first, and one try of each kept test at those
// stations (ADR 0039 decision 12) with the content key of what it runs on. A memory-only or flow-only proposal, or
// one that reaches only stations that can't be tried, gets none. Queuing is all or none per
// proposal and once, so the sweep is idempotent. A factory or organization that fails does not
// hold up the others.
func (s *Tries) Sweep(ctx context.Context) error {
	orgs, err := s.d.Orgs.ActiveOrgs(ctx)
	if err != nil {
		return fmt.Errorf("try sweep: list organizations: %w", err)
	}

	var errs []error

	for _, org := range orgs {
		octx := domain.WithOrg(ctx, org)

		lines, err := s.d.Definitions.CurrentLines(octx)
		if err != nil {
			errs = append(errs, fmt.Errorf("try sweep of org %s: list lines: %w", org, err))

			continue
		}

		for _, l := range lines {
			if !l.Jonah.Enabled {
				continue
			}

			if err := s.sweepFactory(octx, l.FactoryID); err != nil {
				errs = append(errs, fmt.Errorf("try sweep of factory %s: %w", l.FactoryID, err))
			}
		}
	}

	return errors.Join(errs...)
}

func (s *Tries) sweepFactory(ctx context.Context, f domain.FactoryID) error {
	ps, err := s.d.Jonah.Proposals(ctx, ports.ProposalQuery{Factory: f, State: domain.ProposalProposed, Limit: sweepProposals})
	if err != nil {
		return fmt.Errorf("list proposals: %w", err)
	}

	var errs []error

	for _, p := range ps {
		if err := s.sweepProposal(ctx, p); err != nil {
			errs = append(errs, fmt.Errorf("proposal %d: %w", p.Number, err))
		}
	}

	return errors.Join(errs...)
}

// target is one past item's try: the run it reruns.
type target struct {
	item domain.ItemID
	run  domain.Run
}

func (s *Tries) sweepProposal(ctx context.Context, p domain.Proposal) error {
	have, err := s.d.Tries.ProposalPastTries(ctx, p.ID)
	if err != nil {
		return fmt.Errorf("read tries: %w", err)
	}

	if len(have) > 0 {
		return nil
	}

	line, reached, err := s.d.Definitions.ProposedLine(ctx, p.VersionID, p.Edits)
	if errors.Is(err, ports.ErrNotFound) || errors.Is(err, domain.ErrEditDoesNotApply) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read what it changes: %w", err)
	}

	targets, err := s.targets(ctx, p, line, reached)
	if err != nil {
		return err
	}

	kept, err := s.keptTries(ctx, p, line, reached)
	if err != nil {
		return err
	}

	tries := make([]domain.NewPastTry, len(targets), len(targets)+len(kept))
	for i, t := range targets {
		a, _ := line.Agent(t.run.Agent)
		tries[i] = domain.NewPastTry{
			ProposalID: p.ID, ItemID: t.item, Station: t.run.Station, OriginalRunID: t.run.ID, Model: cmp.Or(a.Model, line.Runtime.Model),
		}
	}

	if tries = append(tries, kept...); len(tries) == 0 {
		return nil
	}

	queued, err := s.d.Tries.QueuePastTries(ctx, p.FactoryID, p.ID, tries, s.d.Clock.Now().UTC())
	if errors.Is(err, ports.ErrConflict) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("queue tries: %w", err)
	}

	s.log.InfoContext(ctx, "jonah tries queued", slog.String("factory", string(p.FactoryID)), slog.Int64("proposal", p.Number),
		slog.Int("tries", len(queued)))

	return nil
}

// keptTries are p's tries of the kept tests at line's reached stations that can be tried, one
// each, with the content key of p's version and edits.
func (s *Tries) keptTries(ctx context.Context, p domain.Proposal, line domain.Line, reached []string) ([]domain.NewPastTry, error) {
	if s.d.Kept == nil {
		return nil, nil
	}

	targets, _, err := keptTargets(ctx, s.d.Kept, s.d.Tries, p.FactoryID, line, reached)
	if err != nil || len(targets) == 0 {
		return nil, err
	}

	key, err := s.d.Definitions.ContentKey(ctx, p.VersionID, p.Edits)
	if err != nil {
		return nil, fmt.Errorf("read the content key: %w", err)
	}

	out := make([]domain.NewPastTry, len(targets))
	for i, t := range targets {
		out[i] = t.try(line, key, 1, "")
		out[i].ProposalID = p.ID
	}

	return out, nil
}

// targets are the runs p's tries rerun: for each item p cites, its newest first run of a claim at
// a triable reached station (the station its evidence names first, then the others in line
// order), whose agent can be tried; newest first, at most domain.MaxPastTryItems.
func (s *Tries) targets(ctx context.Context, p domain.Proposal, line domain.Line, reached []string) ([]target, error) {
	triable := slices.DeleteFunc(slices.Clone(reached), func(st string) bool { return !stationTriable(line, st) })
	if len(triable) == 0 {
		return nil, nil
	}

	var (
		items []domain.ItemID
		cited = map[domain.ItemID]string{}
	)

	for _, e := range p.Evidence {
		if _, ok := cited[e.ItemID]; !ok && e.ItemID != "" {
			items, cited[e.ItemID] = append(items, e.ItemID), e.Station
		}
	}

	var out []target

	for _, it := range items {
		stations := triable
		if st := cited[it]; slices.Contains(triable, st) {
			stations = append([]string{st}, slices.DeleteFunc(slices.Clone(triable), func(x string) bool { return x == st })...)
		}

		for _, st := range stations {
			run, err := s.d.Tries.PastTryTarget(ctx, it, st)
			if errors.Is(err, ports.ErrNotFound) {
				continue
			}

			if err != nil {
				return nil, fmt.Errorf("find the run of item %s at %s: %w", it, st, err)
			}

			if line.TriableAgent(run.Agent) {
				out = append(out, target{item: it, run: run})

				break
			}
		}
	}

	slices.SortStableFunc(out, func(a, b target) int { return b.run.StartedAt.Compare(a.run.StartedAt) })

	return out[:min(len(out), domain.MaxPastTryItems)], nil
}

// stationTriable reports whether st is an agent station of l whose agents can all be tried.
func stationTriable(l domain.Line, st string) bool {
	s, ok := l.Station(st)
	if !ok || s.Kind != domain.KindAgent {
		return false
	}

	agents := []string{s.Agent}
	for _, a := range s.Route {
		agents = append(agents, a)
	}

	return !slices.ContainsFunc(agents, func(a string) bool { return a != "" && !l.TriableAgent(a) })
}

// Work runs the claimable tries of every live organization and returns how many it finished. It
// claims fairly: each pass takes one try of each organization in turn, until a pass claims none.
// Within an organization it claims none for a factory whose Jonah budget is used (the try stays
// queued) or that has an item waiting at an agent station (tries give way to production work), and
// the store claims none past TryConfig.PerFactory running at once. An organization that fails
// does not hold up the others.
func (s *Tries) Work(ctx context.Context) (int, error) {
	orgs, err := s.d.Orgs.ActiveOrgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("try work: list organizations: %w", err)
	}

	var (
		worked int
		errs   []error
		failed = map[domain.OrgID]bool{}
	)

	for claimed := true; claimed && ctx.Err() == nil; {
		claimed = false

		for _, org := range orgs {
			if failed[org] || ctx.Err() != nil {
				continue
			}

			got, finished, err := s.workOne(domain.WithOrg(ctx, org))
			if err != nil {
				errs, failed[org] = append(errs, fmt.Errorf("try work of org %s: %w", org, err)), true
			}

			claimed = claimed || got

			if finished {
				worked++
			}
		}
	}

	return worked, errors.Join(errs...)
}

// workOne claims one try of the organization in ctx and runs it: whether it claimed one and
// whether it finished it.
func (s *Tries) workOne(ctx context.Context) (claimed, finished bool, err error) {
	lines, err := s.d.Definitions.CurrentLines(ctx)
	if err != nil {
		return false, false, fmt.Errorf("list lines: %w", err)
	}

	for _, l := range lines {
		if ok, err := s.mayTry(ctx, l); err != nil || !ok {
			if err != nil {
				return false, false, fmt.Errorf("factory %s: %w", l.FactoryID, err)
			}

			continue
		}

		try, err := s.d.Tries.ClaimPastTry(ctx, l.FactoryID, s.d.Clock.Now().UTC(), s.c.Timeout+s.c.Margin, s.c.PerFactory)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}

		if err != nil {
			return false, false, fmt.Errorf("claim a try of factory %s: %w", l.FactoryID, err)
		}

		finished, err := s.finish(ctx, try)

		return true, finished, err
	}

	return false, false, nil
}

// mayTry reports whether l's tries may be claimed now: Jonah is on, its budget is not used and
// no item waits at one of its agent stations.
func (s *Tries) mayTry(ctx context.Context, l domain.Line) (bool, error) {
	if !l.Jonah.Enabled {
		return false, nil
	}

	spend, err := s.d.Jonah.JonahSpendSince(ctx, l.FactoryID, s.d.Clock.Now().UTC().Add(-BudgetWindow))
	if err != nil {
		return false, fmt.Errorf("read spend: %w", err)
	}

	if l.Jonah.Budget(spend).Exhausted() {
		return false, nil
	}

	for _, st := range l.Stations {
		if st.Kind != domain.KindAgent {
			continue
		}

		waiting, err := s.d.Waiting.Waiting(ctx, l.FactoryID, st.ID, 1)
		if err != nil {
			return false, fmt.Errorf("read the items waiting at %s: %w", st.ID, err)
		}

		if len(waiting) > 0 {
			return false, nil
		}
	}

	return true, nil
}

// finish runs the claimed try and records how it ended: whether it finished it. A try that
// could not run is done with verdict error; a read that failed or a cancelled ctx leaves it to its
// lease.
func (s *Tries) finish(ctx context.Context, try domain.PastTry) (bool, error) {
	res, err := s.run(ctx, try)
	if err != nil {
		return false, fmt.Errorf("try %s: %w", try.ID, err)
	}

	res.At = s.d.Clock.Now().UTC()
	res.Error = truncate(res.Error, domain.MaxPastTryError)

	_, err = s.d.Tries.FinishPastTry(ctx, try, res)
	if errors.Is(err, ports.ErrLeaseLost) {
		s.log.WarnContext(ctx, "jonah try lease lost", slog.String("try_id", string(try.ID)), slog.String("factory", string(try.FactoryID)))

		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("finish try %s: %w", try.ID, err)
	}

	s.log.InfoContext(ctx, "jonah try finished", slog.String("try_id", string(try.ID)), slog.String("factory", string(try.FactoryID)),
		slog.Int64("proposal", try.ProposalNumber), slog.String("kept_test", string(try.KeptTestID)), slog.String("verdict", string(res.Verdict)),
		slog.Float64("cost_usd", res.CostUSD))

	return true, nil
}

// tryError is a try's error result: why it could not be carried out.
func tryError(format string, a ...any) domain.PastTryResult {
	return domain.PastTryResult{Verdict: domain.PastTryError, Error: fmt.Sprintf(format, a...)}
}

// run runs try and returns its result to record (At unset). Anything that keeps the run from
// being carried out is verdict error: a proposal, version or original run that is gone, a run
// with no recorded commit, an agent that can no longer be tried, or a RunTask error (a commit
// that is gone, a lost box, a prepare or setup that failed). A run that ended is done with its
// diff, its cost and its run, graded as grade says. Only a failed read or a cancelled ctx is an error.
func (s *Tries) run(ctx context.Context, try domain.PastTry) (domain.PastTryResult, error) {
	if try.Attempts > MaxTryAttempts {
		return tryError("the try did not finish in %d attempts", MaxTryAttempts), nil
	}

	sub, why, err := s.subject(ctx, try)
	if err != nil {
		return domain.PastTryResult{}, err
	}

	if why != "" {
		return tryError("%s", why), nil
	}

	line, why, err := s.proposedLine(ctx, sub)
	if err != nil {
		return domain.PastTryResult{}, err
	}

	if why != "" {
		return tryError("%s", why), nil
	}

	in, err := s.d.Tries.PastTryInput(ctx, try.OriginalRunID)
	if errors.Is(err, ports.ErrNotFound) {
		return tryError("run %s is gone", try.OriginalRunID), nil
	}

	if err != nil {
		return domain.PastTryResult{}, fmt.Errorf("read run %s: %w", try.OriginalRunID, err)
	}

	req, why := tryRequest(line, try, in.Run)
	if why != "" {
		return tryError("%s", why), nil
	}

	req.OrgID, _ = domain.OrgFrom(ctx)
	req.Timeout = s.c.Timeout

	run, err := s.d.Runner.RunTask(ctx, req)

	switch {
	case err != nil && ctx.Err() != nil:
		return domain.PastTryResult{}, fmt.Errorf("run: %w", err)
	case err != nil:
		res := tryError("the try could not run: %v", err)
		res.CostUSD, res.TryRunID = run.CostUSD, run.RunID

		return res, nil
	case run.Status == domain.RunCancelled:
		res := tryError("the try's run was cancelled: %s", run.Error)
		res.CostUSD, res.TryRunID = run.CostUSD, run.RunID

		return res, nil
	}

	return s.grade(ctx, sub, line, try, in.Run, run)
}

// tried is what a try runs on: a proposal's version and edits, or a publish's own (ADR 0039
// decision 12), named as its errors say it.
type tried struct {
	name    string
	version domain.DefinitionVersionID
	edits   []domain.ProposalEdit
}

// subject is what try runs on, or why it cannot run: its proposal is gone.
func (s *Tries) subject(ctx context.Context, try domain.PastTry) (tried, string, error) {
	if try.ProposalID == "" {
		return tried{name: "the publish's draft", version: try.VersionID, edits: try.Edits}, "", nil
	}

	p, err := s.d.Jonah.Proposal(ctx, try.FactoryID, try.ProposalNumber)
	if errors.Is(err, ports.ErrNotFound) {
		return tried{}, fmt.Sprintf("proposal %d is gone", try.ProposalNumber), nil
	}

	if err != nil {
		return tried{}, "", fmt.Errorf("read proposal %d: %w", try.ProposalNumber, err)
	}

	return tried{name: fmt.Sprintf("proposal %d", p.Number), version: p.VersionID, edits: p.Edits}, "", nil
}

// grade is the result of try's run that ended, with its diff, cost and run (ADR 0039 decision 10).
// Code grades it first when the run succeeded, as the station checks a line's run: its output
// against the station's contract as sub changes it, then the station's checks, which RunTask ran in
// the try's box. The first that fails makes it not fixed with what failed as its reason. A contract
// that cannot be checked makes it an error, and a cancelled ctx an error. A kept test's try is then
// graded to the end here (gradeKept). Any other try at a gate station is graded by people
// (gradeByPeople); any other try that passes both, or whose run failed, stays without a verdict,
// for the grader.
func (s *Tries) grade(ctx context.Context, sub tried, line domain.Line, try domain.PastTry, orig domain.Run,
	run domain.RunResult,
) (domain.PastTryResult, error) {
	res := domain.PastTryResult{Diff: run.Diff, CostUSD: run.CostUSD, TryRunID: run.RunID}
	st, _ := line.Station(try.Station)

	if run.Status == domain.RunSucceeded {
		var err error
		if res, err = s.gradeByCode(ctx, sub, st, run, res); err != nil || res.Verdict != "" {
			return res, err
		}
	}

	if try.KeptTestID != "" {
		return gradeKept(res, st, run), nil
	}

	if st.Gate == nil {
		// The model grader grades the try here (software#620); until then it waits without a verdict.
		return res, nil
	}

	return s.gradeByPeople(ctx, line, try, orig, run, res)
}

// gradeByCode is res graded by st's contract as sub changes it, then by st's checks: not fixed
// when one fails, an error when the contract cannot be checked, and unchanged when both pass.
func (s *Tries) gradeByCode(ctx context.Context, sub tried, st domain.Station, run domain.RunResult, res domain.PastTryResult,
) (domain.PastTryResult, error) {
	if st.Output != "" {
		err := s.d.Definitions.ValidateProposed(ctx, sub.version, sub.edits, st.Output, run.Output)

		switch {
		case errors.Is(err, ports.ErrContractViolation):
			res.Verdict, res.Reason = domain.PastTryNotFixed, fmt.Sprintf("the output does not satisfy contract %s: %v", st.Output, err)
		case err != nil && ctx.Err() != nil:
			return domain.PastTryResult{}, fmt.Errorf("check the output against contract %s: %w", st.Output, err)
		case err != nil:
			res.Verdict, res.Error = domain.PastTryError, fmt.Sprintf("contract %s could not be checked: %v", st.Output, err)
		}
	}

	if res.Verdict == "" && run.FailedCheck != nil {
		c := run.FailedCheck
		res.Verdict, res.Reason = domain.PastTryNotFixed, fmt.Sprintf("check `%s` exited %d; the end of its output:\n%s", c.Command, c.ExitCode, c.Output)
	}

	res.Reason = truncate(res.Reason, domain.MaxPastTryReason)

	return res, nil
}

// gradeKept finishes the grade of a kept test's try at st (ADR 0039 decision 12) that code grading
// left without a verdict, which neither the grader nor people's verdicts grade: the item passed st
// the first time, so the try passes (fixed) only when its run succeeded and, at a gate, approved as
// the item was; otherwise it fails (not fixed) with why.
func gradeKept(res domain.PastTryResult, st domain.Station, run domain.RunResult) domain.PastTryResult {
	switch {
	case run.Status != domain.RunSucceeded:
		res.Verdict, res.Reason = domain.PastTryNotFixed, "the run failed: "+cmp.Or(run.Error, string(run.Status))
	case st.Gate == nil:
		res.Verdict, res.Reason = domain.PastTryFixed, "it passed the station's contract and checks"
	default:
		v, err := domain.ParseVerdict(run.Output)

		switch {
		case err != nil:
			res.Verdict, res.Reason = domain.PastTryNotFixed, fmt.Sprintf("the output is not a gate verdict: %v", err)
		case v.Decision != domain.Approve:
			res.Verdict, res.Reason = domain.PastTryNotFixed, "the gate rejected it: "+v.Reason
		default:
			res.Verdict, res.Reason = domain.PastTryFixed, "it passed the station's contract and checks, and the gate approved it as it did the item"
		}
	}

	res.Reason = truncate(res.Reason, domain.MaxPastTryReason)

	return res
}

// gradeByPeople grades res, a try at a gate station, by what people did with its item after orig
// started (domain.GateTruthOf), never by a model nor by the gate's own reasons (ADR 0039 decision
// 10): fixed when the try's decision matches, not fixed when it differs or the try gave no gate
// verdict, and unknown when people's verdicts don't decide it.
func (s *Tries) gradeByPeople(ctx context.Context, line domain.Line, try domain.PastTry, orig domain.Run, run domain.RunResult,
	res domain.PastTryResult,
) (domain.PastTryResult, error) {
	v, err := domain.ParseVerdict(run.Output)

	switch {
	case run.Status != domain.RunSucceeded:
		res.Verdict, res.Reason = domain.PastTryNotFixed, fmt.Sprintf("the try's run %s, so it gave no gate verdict", run.Status)
	case err != nil:
		res.Verdict, res.Reason = domain.PastTryNotFixed, fmt.Sprintf("the try's output is not a gate verdict: %v", err)
	default:
		events, err := s.itemEvents(ctx, try.ItemID)
		if err != nil {
			return domain.PastTryResult{}, err
		}

		res.Verdict, res.Reason = peopleGrade(v.Decision, domain.GateTruthOf(events, line, try.Station, orig.StartedAt))
	}

	res.Reason = truncate(res.Reason, domain.MaxPastTryReason)

	return res, nil
}

// peopleGrade is the grade of a try that decided d, against what people decided.
func peopleGrade(d domain.Decision, truth domain.GateTruth) (domain.PastTryVerdict, string) {
	if truth.Decision == "" {
		return domain.PastTryUnknown,
			"no person's verdict or merge decides the item yet: it is still open, was cancelled, or only the gate agent sent it back"
	}

	if truth.Decision == d {
		return domain.PastTryFixed, fmt.Sprintf("the try decided %s, as people did: %s", d, truth.Reason)
	}

	return domain.PastTryNotFixed, fmt.Sprintf("the try decided %s, but people decided %s: %s", d, truth.Decision, truth.Reason)
}

// tryEventPage is how many of an item's events itemEvents reads at a time.
const tryEventPage = 500

// itemEvents are all of item id's events, in append order.
func (s *Tries) itemEvents(ctx context.Context, id domain.ItemID) ([]domain.Event, error) {
	var all []domain.Event

	for after := int64(0); ; {
		events, err := s.d.Events.ItemEvents(ctx, id, after, tryEventPage)
		if err != nil {
			return nil, fmt.Errorf("read the events of item %s: %w", id, err)
		}

		all = append(all, events...)

		if len(events) < tryEventPage {
			return all, nil
		}

		after = events[len(events)-1].ID
	}
}

// proposedLine is sub's version with its edits applied, or why a try of it cannot run on it: the
// version is gone or the edits no longer apply.
func (s *Tries) proposedLine(ctx context.Context, sub tried) (domain.Line, string, error) {
	line, _, err := s.d.Definitions.ProposedLine(ctx, sub.version, sub.edits)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return domain.Line{}, fmt.Sprintf("definition version %s is gone", sub.version), nil
	case errors.Is(err, domain.ErrEditDoesNotApply):
		return domain.Line{}, fmt.Sprintf("%s does not apply to version %s: %v", sub.name, sub.version, err), nil
	case err != nil:
		return domain.Line{}, "", fmt.Errorf("read version %s with %s: %w", sub.version, sub.name, err)
	}

	return line, "", nil
}

// tryRequest is try's run on line, from what orig recorded, with the checks of its station on
// line, or why it cannot run.
func tryRequest(line domain.Line, try domain.PastTry, orig domain.Run) (domain.RunRequest, string) {
	rt := line.Runtime

	a, ok := line.Agent(orig.Agent)

	switch {
	case orig.Input.BranchHead == "":
		return domain.RunRequest{}, fmt.Sprintf("run %s recorded no commit", orig.ID)
	case rt.Repository.URL == "":
		return domain.RunRequest{}, "the factory has no repository to try in"
	case !ok || !line.TriableAgent(orig.Agent):
		return domain.RunRequest{}, fmt.Sprintf("agent %s can't be tried: it needs MCP servers or secrets", orig.Agent)
	}

	st, ok := line.Station(try.Station)
	if !ok || st.Kind != domain.KindAgent {
		return domain.RunRequest{}, fmt.Sprintf("station %s is not an agent station of the proposed line", try.Station)
	}

	req := domain.RunRequest{
		FactoryID: try.FactoryID, Version: line.VersionID, Kind: domain.RunKindTry, Station: try.Station,
		Agent: a.Name, Model: try.Model, Bundle: domain.TaskKindTry, TryOf: try.ID, Commit: orig.Input.BranchHead,
		Repository: rt.Repository, ModelConnection: rt.ModelConnection, Size: rt.Size, Checks: st.Checks,
	}

	if line.OnSecrets() { // the model key alone (ADR 0037 decision 7)
		env, _ := line.Environment(a.Environment)
		req.ModelConnection, req.Repository.Connection, req.ModelKey, req.Environment = "", "", rt.ModelKey, env
		req.Size, req.Image = cmp.Or(env.Size, domain.SandboxMedium), env.Image
	}

	return req, ""
}
