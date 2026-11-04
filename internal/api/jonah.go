package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/jonah"
)

// JonahDeps serve the Jonah routes (ADR 0034). Store reads reviews, proposals and observations
// (a ports.JonahStore satisfies it), Decisions decides proposals and queues reviews
// (*jonah.Decider satisfies it, over NewJonahAuthoring), and Watches reads the watch on every
// published change (ADR 0039 decision 14) for versions, proposals and the inbox; nil shows none.
// Tests keeps the kept tests (kepttests.go); while it is nil those routes answer 503
// jonah-unavailable.
type JonahDeps struct {
	Store     JonahReads
	Decisions JonahDecisions
	Watches   ports.Watches
	// Models makes Try another model's proposals (ADR 0039 decision 13); *jonah.ModelProposals
	// satisfies it. nil answers the route with 503 jonah-unavailable.
	Models JonahModels
	// Tests keeps the kept tests (kepttests.go); while it is nil those routes answer 503.
	Tests KeptTestStore
	// Tries reads a proposal's tries of past items (ADR 0039 decision 11) for its tried; nil
	// shows none.
	Tries PastTryReads
	// KeptDrafts starts and reads the open draft's kept tests (ADR 0039 decision 12);
	// *jonah.KeptDrafts satisfies it. nil answers those routes with 503 jonah-unavailable.
	KeptDrafts DraftKeptTests
	// TryAgain adds tries to a proposal (ADR 0039 decision 11); *jonah.TryAgain satisfies it. nil
	// answers the route with 503 jonah-unavailable.
	TryAgain JonahTryAgain
}

// DraftKeptTests starts and reads the kept tests the open draft reaches; an error wrapping
// authoring.ErrNoDraft when none is open.
type DraftKeptTests interface {
	Start(ctx context.Context, f domain.FactoryID) (jonah.DraftTests, error)
	Read(ctx context.Context, f domain.FactoryID) (jonah.DraftTests, error)
}

// JonahTryAgain estimates and queues Try again of a proposal as the person in ctx; its refusals
// are ports.ErrNotFound, ErrProposalDecided, domain.ErrProposalStale, ErrProposalNotTriable,
// ErrProposalNotTried and ErrLearningBudgetExhausted, which comes with the estimate.
type JonahTryAgain interface {
	Try(ctx context.Context, f domain.FactoryID, number int64, policy domain.JonahPolicy, confirm bool) (jonah.TryAgainEstimate, error)
}

// PastTryReads are the reads of ports.PastTries a proposal's tried makes.
type PastTryReads interface {
	ProposalPastTries(ctx context.Context, proposal domain.ProposalID) ([]domain.PastTry, error)
	AgentDoneItems(ctx context.Context, factory domain.FactoryID, agent string, limit int) ([]domain.AgentItem, error)
	ProposalGraderChecks(ctx context.Context, proposal domain.ProposalID) ([]domain.GraderCheck, error)
}

// JonahModels estimates and makes a model proposal as the person in ctx; its refusals are
// domain.ErrAgentNotFound, ErrModelNotAllowed, ErrAgentNotTriable, ErrNoFinishedItems and
// ErrLearningBudgetExhausted, which comes with the estimate.
type JonahModels interface {
	Propose(ctx context.Context, f domain.FactoryID, r jonah.ModelRequest) (jonah.ModelEstimate, domain.Proposal, error)
}

// JonahReads are the reads of ports.JonahStore the Jonah routes make.
type JonahReads interface {
	JonahReviews(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.JonahReview, error)
	Proposals(ctx context.Context, q ports.ProposalQuery) ([]domain.Proposal, error)
	Proposal(ctx context.Context, factory domain.FactoryID, number int64) (domain.Proposal, error)
	Observations(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.Observation, error)
	JonahSpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (domain.JonahSpend, error)
}

// JonahDecisions decide proposals and queue manual reviews as the person in ctx; their refusals
// are domain.ErrProposalDraftOpen, ErrProposalStale, ErrProposalDraftConflict, ErrReviewInFlight,
// ErrLearningBudgetExhausted and ports.ErrProposalDecided, and Undo's ErrProposalNotPublished,
// ErrUndoConflict and ports.ErrConflict.
type JonahDecisions interface {
	Approve(ctx context.Context, f domain.FactoryID, number int64) (domain.Proposal, domain.DefinitionVersion, error)
	Edit(ctx context.Context, f domain.FactoryID, number int64) (domain.Proposal, int64, error)
	Reject(ctx context.Context, f domain.FactoryID, number int64, reason string) (domain.Proposal, error)
	Undo(ctx context.Context, f domain.FactoryID, number int64) (domain.Proposal, error)
	QueueReview(ctx context.Context, f domain.FactoryID, version domain.DefinitionVersionID, policy domain.JonahPolicy) (domain.JonahReview, error)
}

// Jonah pages and reads.
const (
	defaultProposalLimit = 50
	maxProposalLimit     = 100
	// overviewObservations and overviewProposals bound what the overview reads.
	overviewObservations = 200
	overviewProposals    = 1000
)

var (
	errJonahUnavailable = errors.New("this goald serves no Jonah store")
	errProposalNotFound = errors.New("no such proposal")
)

// jonahProblem answers the Jonah routes' own errors.
func jonahProblem(err error) (gen.Problem, bool) {
	var status int

	var code string

	switch {
	case errors.Is(err, errJonahUnavailable):
		status, code = http.StatusServiceUnavailable, "jonah-unavailable"
	case errors.Is(err, errProposalNotFound):
		status, code = http.StatusNotFound, "proposal-not-found"
	case errors.Is(err, domain.ErrProposalDraftOpen):
		status, code = http.StatusConflict, "draft-open"
	case errors.Is(err, domain.ErrProposalStale):
		status, code = http.StatusConflict, "proposal-stale"
	case errors.Is(err, domain.ErrProposalDraftConflict):
		status, code = http.StatusConflict, "draft-conflict"
	case errors.Is(err, ports.ErrProposalDecided):
		status, code = http.StatusConflict, "proposal-decided"
	case errors.Is(err, domain.ErrReviewInFlight):
		status, code = http.StatusConflict, "review-in-flight"
	case errors.Is(err, domain.ErrLearningBudgetExhausted):
		status, code = http.StatusConflict, "learning-budget-exhausted"
	default:
		i := slices.IndexFunc(modelRefusals, func(r modelRefusal) bool { return errors.Is(err, r.err) })
		if i < 0 {
			return gen.Problem{}, false
		}

		status, code = modelRefusals[i].status, modelRefusals[i].code
	}

	p := detailed(status, err)
	p.Code = &code

	if e, ok := errors.AsType[*modelBudgetError](err); ok {
		p.ModelEstimate = &e.estimate
		p.Estimate = &gen.EvalEstimate{Usd: e.estimate.EstimateUsd, Unpriced: []string{}}
	}

	if e, ok := errors.AsType[*tryBudgetError](err); ok {
		p.TryEstimate = &e.estimate
		p.Estimate = &gen.EvalEstimate{Usd: e.estimate.EstimateUsd, Unpriced: []string{}}
	}

	return p, true
}

// modelRefusal is a refusal of Try another model and its answer.
type modelRefusal struct {
	err    error
	status int
	code   string
}

// modelRefusals are Try another model's and Try again's refusals but the budget's.
var modelRefusals = []modelRefusal{
	{domain.ErrProposalNotTriable, http.StatusUnprocessableEntity, "proposal-not-triable"},
	{domain.ErrProposalNotTried, http.StatusConflict, "proposal-not-tried"},
	{domain.ErrModelNotAllowed, http.StatusUnprocessableEntity, "model-not-allowed"},
	{domain.ErrAgentNotTriable, http.StatusUnprocessableEntity, "agent-not-triable"},
	{domain.ErrNoFinishedItems, http.StatusUnprocessableEntity, "no-finished-items"},
	{domain.ErrAgentNotFound, http.StatusNotFound, "agent-not-found"},
}

// modelBudgetError is a model proposal refused over Jonah's budget, with the estimate it refused.
type modelBudgetError struct {
	estimate gen.ModelProposalEstimate
	err      error
}

func (e *modelBudgetError) Error() string { return e.err.Error() }
func (e *modelBudgetError) Unwrap() error { return e.err }

// tryBudgetError is Try again refused over Jonah's budget, with the estimate it refused.
type tryBudgetError struct {
	estimate gen.TryProposalEstimate
	err      error
}

func (e *tryBudgetError) Error() string { return e.err.Error() }
func (e *tryBudgetError) Unwrap() error { return e.err }

func (s *server) GetJonahOverview(ctx context.Context, req gen.GetJonahOverviewRequestObject) (gen.GetJonahOverviewResponseObject, error) {
	rec, err := s.jonahFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	policy := s.jonahPolicy(ctx, rec.ID)
	now := s.clock.Now().UTC()

	waiting, err := s.jonah.Store.Proposals(ctx, ports.ProposalQuery{Factory: rec.ID, State: domain.ProposalProposed, Limit: overviewProposals})
	if err != nil {
		return nil, fmt.Errorf("read waiting proposals: %w", err)
	}

	month, err := s.jonah.Store.JonahSpendSince(ctx, rec.ID, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return nil, fmt.Errorf("read this month's jonah spend: %w", err)
	}

	week, err := s.jonah.Store.JonahSpendSince(ctx, rec.ID, now.Add(-domain.JonahBudgetWindow))
	if err != nil {
		return nil, fmt.Errorf("read the week's spend: %w", err)
	}

	observations, err := s.weekObservations(ctx, rec.ID, policy, weekStart(now))
	if err != nil {
		return nil, err
	}

	out := gen.GetJonahOverview200JSONResponse{
		Policy: gen.JonahPolicy{
			Enabled: policy.Enabled, ProposeAfter: policy.ProposeAfter, Share: policy.Share, BudgetUsd: policy.Budget(week).BudgetUSD,
			ApproverRoles: rolesHolding("definition:publish"),
		},
		Waiting: len(waiting), MonthCostUsd: month.JonahUSD, Observations: observations,
	}
	if policy.Model != "" {
		out.Policy.Model = &policy.Model
	}

	reviews, err := s.jonah.Store.JonahReviews(ctx, rec.ID, 1)
	if err != nil {
		return nil, fmt.Errorf("read the last review: %w", err)
	}

	if len(reviews) > 0 {
		r := reviewOut(reviews[0])
		out.LastReview = &r
	}

	return out, nil
}

// weekStart is Monday 00:00 UTC of now's week.
func weekStart(now time.Time) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

// weekObservations are f's observations a review updated since since, the latest updated first,
// each with the number of the proposal it became.
func (s *server) weekObservations(ctx context.Context, f domain.FactoryID, policy domain.JonahPolicy, since time.Time) ([]gen.JonahObservation, error) {
	obs, err := s.jonah.Store.Observations(ctx, f, overviewObservations)
	if err != nil {
		return nil, fmt.Errorf("read observations: %w", err)
	}

	numbers := map[domain.ProposalID]int64{}

	out := []gen.JonahObservation{}

	for _, o := range obs {
		if o.UpdatedAt.Before(since) {
			continue
		}

		if o.ProposalID != "" && len(numbers) == 0 {
			if numbers, err = s.proposalNumbers(ctx, f); err != nil {
				return nil, err
			}
		}

		e := gen.JonahObservation{
			Id: string(o.ID), Summary: o.Summary, Detail: o.Detail, ItemRefs: s.itemRefs(ctx, f, o.ItemIDs),
			State: gen.JonahObservationState(o.State), Count: o.Count, ProposeAfter: policy.ProposeAfter, CreatedAt: o.CreatedAt,
			UpdatedAt: o.UpdatedAt,
		}
		if n, ok := numbers[o.ProposalID]; ok {
			e.Proposal = &n
		}

		out = append(out, e)
	}

	return out, nil
}

// proposalNumbers are the numbers of f's newest proposals, by id.
func (s *server) proposalNumbers(ctx context.Context, f domain.FactoryID) (map[domain.ProposalID]int64, error) {
	ps, err := s.jonah.Store.Proposals(ctx, ports.ProposalQuery{Factory: f, Limit: overviewProposals})
	if err != nil {
		return nil, fmt.Errorf("read proposals: %w", err)
	}

	out := make(map[domain.ProposalID]int64, len(ps))
	for _, p := range ps {
		out[p.ID] = p.Number
	}

	return out, nil
}

// itemRefs are ids as <factory name>#<number>; an item that is gone keeps its id.
func (s *server) itemRefs(ctx context.Context, f domain.FactoryID, ids []domain.ItemID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		ref, _ := s.itemRef(ctx, f, id)
		out = append(out, ref)
	}

	return out
}

// itemRef is id's ref and title; an item that cannot be read keeps its id and no title.
func (s *server) itemRef(ctx context.Context, f domain.FactoryID, id domain.ItemID) (string, string) {
	it, err := s.items.Item(ctx, id)
	if err != nil {
		return string(id), ""
	}

	return s.ref(ctx, f, it.Number), it.Title
}

func (s *server) ListProposals(ctx context.Context, req gen.ListProposalsRequestObject) (gen.ListProposalsResponseObject, error) {
	rec, err := s.jonahFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	q := ports.ProposalQuery{Factory: rec.ID, Before: deref(req.Params.After), Limit: defaultProposalLimit}
	if req.Params.State != nil {
		if q.State = domain.ProposalState(*req.Params.State); !q.State.Valid() {
			return nil, fmt.Errorf("%w: state %q is not a proposal state", errInvalid, q.State)
		}
	}

	if req.Params.Limit != nil {
		if *req.Params.Limit < 1 || *req.Params.Limit > maxProposalLimit {
			return nil, fmt.Errorf("%w: limit must be 1 to %d", errInvalid, maxProposalLimit)
		}

		q.Limit = *req.Params.Limit
	}

	if q.Before < 0 {
		return nil, fmt.Errorf("%w: after must be positive", errInvalid)
	}

	// One more than the page says whether there is a next one.
	want := q.Limit
	q.Limit++

	ps, err := s.jonah.Store.Proposals(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}

	out := gen.ListProposals200JSONResponse{Proposals: []gen.ProposalEntry{}}

	if len(ps) > want {
		ps = ps[:want]
		next := ps[want-1].Number
		out.Next = &next
	}

	for _, p := range ps {
		out.Proposals = append(out.Proposals, proposalEntry(p))
	}

	return out, nil
}

func (s *server) GetProposal(ctx context.Context, req gen.GetProposalRequestObject) (gen.GetProposalResponseObject, error) {
	rec, err := s.jonahFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	p, err := s.proposal(ctx, rec.ID, req.Number)
	if err != nil {
		return nil, err
	}

	out, err := s.proposalOut(ctx, rec, p)
	if err != nil {
		return nil, err
	}

	return gen.GetProposal200JSONResponse(out), nil
}

// proposalOut is p of rec with its files, evidence, versions, the proposal it undoes and the
// watch of its own number.
func (s *server) proposalOut(ctx context.Context, rec ports.FactoryRecord, p domain.Proposal) (gen.Proposal, error) {
	made, err := s.versions.Version(ctx, p.VersionID)
	if err != nil {
		return gen.Proposal{}, fmt.Errorf("read version %s of proposal %d: %w", p.VersionID, p.Number, err)
	}

	e := proposalEntry(p)
	out := gen.Proposal{
		Number: e.Number, Title: e.Title, State: e.State, CreatedAt: e.CreatedAt, Paths: e.Paths, Agents: e.Agents,
		Estimate: e.Estimate, EvidenceCount: e.EvidenceCount, Summary: p.Summary,
		Evidence:           make([]gen.ProposalEvidence, 0, len(p.Evidence)),
		MadeAgainstVersion: made.Number, PublishesAsVersion: rec.LastVersionNumber + 1,
	}

	out.Edits = proposalEditsOut(p.Edits)
	if out.Files, err = s.proposalFilesOut(ctx, rec.ID, p, made.Number); err != nil {
		return gen.Proposal{}, err
	}

	for _, ev := range p.Evidence {
		ref, title := s.itemRef(ctx, rec.ID, ev.ItemID)

		e := gen.ProposalEvidence{ItemRef: ref, ItemTitle: title, Quote: ev.Quote}
		if ev.Station != "" {
			e.Station = &ev.Station
		}

		out.Evidence = append(out.Evidence, e)
	}

	if p.PublishedVersionID != "" {
		v, err := s.versions.Version(ctx, p.PublishedVersionID)
		if err != nil {
			return gen.Proposal{}, fmt.Errorf("read the version proposal %d published: %w", p.Number, err)
		}

		out.PublishedVersion = &v.Number
	}

	if p.RejectReason != "" {
		out.RejectReason = &p.RejectReason
	}

	if p.DecidedBy != "" {
		out.DecidedBy, out.DecidedAt = &p.DecidedBy, &p.DecidedAt
	}

	if p.Undoes != "" {
		numbers, err := s.proposalNumbers(ctx, rec.ID)
		if err != nil {
			return gen.Proposal{}, err
		}

		if n, ok := numbers[p.Undoes]; ok {
			out.Undoes = &n
		}
	}

	if err := s.withOutcomes(ctx, rec.ID, p, &out); err != nil {
		return gen.Proposal{}, err
	}

	return out, nil
}

// withOutcomes sets out's watch of p's own number and how p did on past items.
func (s *server) withOutcomes(ctx context.Context, f domain.FactoryID, p domain.Proposal, out *gen.Proposal) error {
	var err error

	if out.Watch, err = s.proposalWatch(ctx, p); err != nil {
		return err
	}

	out.Tried, err = s.proposalTried(ctx, f, p)

	return err
}

// triedLineItems bounds the agent's done items read for a model proposal's line results: as many
// as Try another model reads to pick its items.
const triedLineItems = 4 * domain.MaxPastTryItems

// proposalTried is how p did on past items, nil when it has no tries or this goald reads none. A
// model proposal's items carry how each went on the line while it is among the agent's newest.
func (s *server) proposalTried(ctx context.Context, f domain.FactoryID, p domain.Proposal) (*gen.ProposalTried, error) {
	if s.jonah.Tries == nil {
		return nil, nil
	}

	tries, err := s.jonah.Tries.ProposalPastTries(ctx, p.ID)
	if err != nil {
		return nil, fmt.Errorf("read the tries of proposal %d: %w", p.Number, err)
	}

	if len(tries) == 0 {
		return nil, nil
	}

	spend, err := s.jonah.Store.JonahSpendSince(ctx, f, s.clock.Now().UTC().Add(-domain.JonahBudgetWindow))
	if err != nil {
		return nil, fmt.Errorf("read jonah's spend: %w", err)
	}

	tried, _ := jonah.TriedOf(tries, s.jonahPolicy(ctx, f).Budget(spend))

	lines := map[domain.ItemID]string{}

	if p.Agent != "" {
		items, err := s.jonah.Tries.AgentDoneItems(ctx, f, p.Agent, triedLineItems)
		if err != nil {
			return nil, fmt.Errorf("read the items of agent %s: %w", p.Agent, err)
		}

		for _, it := range items {
			lines[it.ItemID] = it.LineResult()
		}
	}

	out := &gen.ProposalTried{
		State: gen.ProposalTriedState(tried.State), Items: make([]gen.TriedItem, 0, len(tried.Items)), CostUsd: tried.CostUSD,
		KeptTests: []gen.TriedKeptTest{},
	}

	if tried.State == jonah.TriedDone {
		secs := tried.Duration.Seconds()
		out.Seconds = &secs
	}

	if err := s.triedGrader(ctx, p, out); err != nil {
		return nil, err
	}

	for _, k := range tried.KeptTests {
		out.KeptTests = append(out.KeptTests, s.triedKeptTestOut(ctx, f, k))
	}

	for _, it := range tried.Items {
		out.Items = append(out.Items, s.triedItemOut(ctx, f, it, lines))
	}

	return out, nil
}

// triedItemOut is it as a proposal's tried shows it, with its line result from lines when it has one.
func (s *server) triedItemOut(ctx context.Context, f domain.FactoryID, it jonah.TriedItem, lines map[domain.ItemID]string) gen.TriedItem {
	ref, title := s.itemRef(ctx, f, it.ItemID)
	e := gen.TriedItem{ItemRef: ref, ItemTitle: title, Station: it.Station, Tries: make([]gen.TriedTry, 0, len(it.Tries))}

	if line, ok := lines[it.ItemID]; ok {
		e.LineResult = &line
	}

	for _, t := range it.Tries {
		e.Tries = append(e.Tries, triedTryOut(t))
	}

	return e
}

// triedGrader sets out's grader from p's checks of the grader (ADR 0039 decision 10), once it has
// any, and adds their cost to out's: each hit rate is its hits over its checks, 0 without checks.
func (s *server) triedGrader(ctx context.Context, p domain.Proposal, out *gen.ProposalTried) error {
	checks, err := s.jonah.Tries.ProposalGraderChecks(ctx, p.ID)
	if err != nil {
		return fmt.Errorf("read the grader's checks of proposal %d: %w", p.Number, err)
	}

	if len(checks) == 0 {
		return nil
	}

	rate := func(hits, total int) float64 {
		if total == 0 {
			return 0
		}

		return float64(hits) / float64(total)
	}

	r := domain.HitRatesOf(checks)
	out.Grader = &gen.TriedGrader{OriginalHitRate: rate(r.OriginalHits, r.OriginalTotal), FinalHitRate: rate(r.FinalHits, r.FinalTotal)}

	for _, c := range checks {
		out.CostUsd += c.CostUSD
	}

	return nil
}

// triedKeptTestOut is a proposal's try of a kept test as its tried shows it: the test by its
// item's number.
func (s *server) triedKeptTestOut(ctx context.Context, f domain.FactoryID, t domain.PastTry) gen.TriedKeptTest {
	var number int64
	if it, err := s.items.Item(ctx, t.ItemID); err == nil && it.FactoryID == f {
		number = it.Number
	}

	return gen.TriedKeptTest{
		Test: number, Station: t.Station, Attempt: t.Attempt, Result: gen.KeptTestResult(t.KeptResult()), Reason: optional(t.Reason),
		Error: optional(t.Error),
	}
}

// triedTryOut is t as a proposal's tried shows it.
func triedTryOut(t domain.PastTry) gen.TriedTry {
	out := gen.TriedTry{Attempt: t.Attempt, State: gen.TriedTryState(t.State), CostUsd: t.CostUSD, Reason: optional(t.Reason), Error: optional(t.Error)}
	if t.Verdict != "" {
		v := gen.TriedTryVerdict(t.Verdict)
		out.Verdict = &v
	}

	return out
}

// proposalEditsOut are edits as the API gives them.
func proposalEditsOut(edits []domain.ProposalEdit) []gen.ProposalEdit {
	out := make([]gen.ProposalEdit, 0, len(edits))

	for _, e := range edits {
		g := gen.ProposalEdit{Path: e.Path}

		if e.Whole() {
			g.Content = &e.Content
			if e.BaseSHA256 != "" {
				g.BaseSha256 = &e.BaseSHA256
			}
		} else {
			reps := make([]gen.Replacement, 0, len(e.Replacements))
			for _, r := range e.Replacements {
				reps = append(reps, gen.Replacement{Old: r.Old, New: r.New})
			}

			g.Replacements = &reps
		}

		out = append(out, g)
	}

	return out
}

// proposalFilesOut are p's edited files in version made of f: each file's content there (empty
// for a new file) and that content with its edit applied. An edit that does not apply there,
// which a review's check refuses, shows the file unchanged.
func (s *server) proposalFilesOut(ctx context.Context, f domain.FactoryID, p domain.Proposal, made int) ([]gen.ProposalFile, error) {
	out := make([]gen.ProposalFile, 0, len(p.Edits))

	for _, e := range p.Edits {
		b, err := s.defs.VersionFile(ctx, f, authoring.VersionSelector{Kind: authoring.SelectNumber, Number: made}, e.Path)
		if err != nil && !errors.Is(err, authoring.ErrFileNotFound) {
			return nil, fmt.Errorf("read %s of v%d: %w", e.Path, made, err)
		}

		present := err == nil
		base, sha := "", ""

		if present {
			base, sha = string(b.Content), string(domain.HashBlob(b.Content))
		}

		content, err := e.Apply([]byte(base), present)
		if err != nil {
			content = base
		}

		out = append(out, gen.ProposalFile{Path: e.Path, BaseSha256: sha, BaseContent: base, Content: content})
	}

	return out, nil
}

func (s *server) ApproveProposal(ctx context.Context, req gen.ApproveProposalRequestObject) (gen.ApproveProposalResponseObject, error) {
	rec, err := s.jonahWrite(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if _, err := s.proposal(ctx, rec.ID, req.Number); err != nil {
		return nil, err
	}

	p, v, err := s.jonah.Decisions.Approve(ctx, rec.ID, req.Number)
	if err != nil {
		return nil, err
	}

	return gen.ApproveProposal200JSONResponse{Number: p.Number, State: gen.ProposalState(p.State), Version: &v.Number}, nil
}

func (s *server) EditProposal(ctx context.Context, req gen.EditProposalRequestObject) (gen.EditProposalResponseObject, error) {
	rec, err := s.jonahWrite(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if _, err := s.proposal(ctx, rec.ID, req.Number); err != nil {
		return nil, err
	}

	p, revision, err := s.jonah.Decisions.Edit(ctx, rec.ID, req.Number)
	if err != nil {
		return nil, err
	}

	return gen.EditProposal200JSONResponse{Number: p.Number, State: gen.ProposalState(p.State), DraftRevision: &revision}, nil
}

func (s *server) RejectProposal(ctx context.Context, req gen.RejectProposalRequestObject) (gen.RejectProposalResponseObject, error) {
	rec, err := s.jonahWrite(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(req.Body.Reason) == "" {
		return nil, fmt.Errorf("%w: a rejection needs a reason", errInvalid)
	}

	if _, err := s.proposal(ctx, rec.ID, req.Number); err != nil {
		return nil, err
	}

	p, err := s.jonah.Decisions.Reject(ctx, rec.ID, req.Number, req.Body.Reason)
	if err != nil {
		return nil, err
	}

	return gen.RejectProposal200JSONResponse{Number: p.Number, State: gen.ProposalState(p.State)}, nil
}

func (s *server) QueueJonahReview(ctx context.Context, req gen.QueueJonahReviewRequestObject) (gen.QueueJonahReviewResponseObject, error) {
	rec, err := s.jonahWrite(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if rec.Current == nil {
		return nil, fmt.Errorf("review of a factory never published: %w", authoring.ErrVersionNotFound)
	}

	r, err := s.jonah.Decisions.QueueReview(ctx, rec.ID, rec.Current.ID, s.jonahPolicy(ctx, rec.ID))
	if err != nil {
		return nil, err
	}

	return gen.QueueJonahReview202JSONResponse(reviewOut(r)), nil
}

func (s *server) ProposeAgentModel(ctx context.Context, req gen.ProposeAgentModelRequestObject) (gen.ProposeAgentModelResponseObject, error) {
	rec, err := s.jonahWrite(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if s.jonah.Models == nil {
		return nil, errJonahUnavailable
	}

	if rec.Current == nil {
		return nil, fmt.Errorf("model proposal of a factory never published: %w", authoring.ErrVersionNotFound)
	}

	if req.Body == nil {
		return nil, fmt.Errorf("%w: a model proposal needs a model", errInvalid)
	}

	confirm := req.Body.Confirm != nil && *req.Body.Confirm

	est, p, err := s.jonah.Models.Propose(ctx, rec.ID, jonah.ModelRequest{Version: rec.Current.ID, Agent: req.Agent, Model: req.Body.Model, Confirm: confirm})

	switch {
	case errors.Is(err, domain.ErrLearningBudgetExhausted):
		return nil, &modelBudgetError{estimate: s.modelEstimateOut(ctx, rec.ID, est), err: err}
	case err != nil:
		return nil, err
	}

	out := gen.ModelProposalResult{Estimate: s.modelEstimateOut(ctx, rec.ID, est)}
	if !confirm {
		return gen.ProposeAgentModel200JSONResponse(out), nil
	}

	proposal, err := s.proposalOut(ctx, rec, p)
	if err != nil {
		return nil, err
	}

	out.Proposal = &proposal

	return gen.ProposeAgentModel201JSONResponse(out), nil
}

func (s *server) TryJonahProposal(ctx context.Context, req gen.TryJonahProposalRequestObject) (gen.TryJonahProposalResponseObject, error) {
	rec, err := s.jonahWrite(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if s.jonah.TryAgain == nil {
		return nil, errJonahUnavailable
	}

	if _, err := s.proposal(ctx, rec.ID, req.Number); err != nil {
		return nil, err
	}

	confirm := req.Body != nil && req.Body.Confirm != nil && *req.Body.Confirm

	est, err := s.jonah.TryAgain.Try(ctx, rec.ID, req.Number, s.jonahPolicy(ctx, rec.ID), confirm)

	switch {
	case errors.Is(err, domain.ErrLearningBudgetExhausted):
		return nil, &tryBudgetError{estimate: s.tryEstimateOut(ctx, rec.ID, est), err: err}
	case err != nil:
		return nil, err
	}

	out := gen.TryProposalResult{Estimate: s.tryEstimateOut(ctx, rec.ID, est)}
	if !confirm {
		return gen.TryJonahProposal200JSONResponse(out), nil
	}

	p, err := s.proposal(ctx, rec.ID, req.Number)
	if err != nil {
		return nil, err
	}

	proposal, err := s.proposalOut(ctx, rec, p)
	if err != nil {
		return nil, err
	}

	out.Proposal = &proposal

	return gen.TryJonahProposal201JSONResponse(out), nil
}

// tryEstimateOut is est as the API answers it, each item with its ref and title in f.
func (s *server) tryEstimateOut(ctx context.Context, f domain.FactoryID, est jonah.TryAgainEstimate) gen.TryProposalEstimate {
	out := gen.TryProposalEstimate{
		Number: est.Number, Items: make([]gen.TryProposalItem, 0, len(est.Items)), Tries: est.Tries, EstimateUsd: est.EstimateUSD,
		BudgetLeftUsd: est.BudgetLeftUSD,
	}

	for _, it := range est.Items {
		ref, title := s.itemRef(ctx, f, it.ItemID)
		out.Items = append(out.Items, gen.TryProposalItem{ItemRef: ref, ItemTitle: title, Station: it.Station, Tries: it.Tries, RunCostUsd: it.RunCostUSD})
	}

	return out
}

// modelEstimateOut is est as the API answers it, each item with its ref in f.
func (s *server) modelEstimateOut(ctx context.Context, f domain.FactoryID, est jonah.ModelEstimate) gen.ModelProposalEstimate {
	out := gen.ModelProposalEstimate{
		Agent: est.Agent, CurrentModel: est.CurrentModel, Model: est.Model, Items: []gen.ModelProposalItem{}, Tries: est.Tries,
		EstimateUsd: est.EstimateUSD, BudgetLeftUsd: est.BudgetLeftUSD,
	}

	for _, it := range est.Items {
		out.Items = append(out.Items, gen.ModelProposalItem{
			ItemRef: s.ref(ctx, f, it.ItemNumber), ItemTitle: it.ItemTitle, Station: it.Run.Station, RunId: string(it.Run.ID),
			LineResult: it.LineResult(), SentBack: len(it.SentBackAt), CostUsd: it.CostUSD, RunCostUsd: it.Run.Result.CostUSD,
		})
	}

	return out
}

// jonahFactory resolves the path's factory, then refuses with 503 when this goald serves no
// Jonah store: the factory is resolved and authorized first, so the answer tells nothing of a
// factory the caller may not see.
func (s *server) jonahFactory(ctx context.Context, ref string) (ports.FactoryRecord, error) {
	rec, err := s.definition(ctx, ref)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	if s.jonah == nil {
		return ports.FactoryRecord{}, errJonahUnavailable
	}

	return rec, nil
}

// jonahWrite is jonahFactory for a decision, which an archived factory refuses.
func (s *server) jonahWrite(ctx context.Context, ref string) (ports.FactoryRecord, error) {
	rec, err := s.jonahFactory(ctx, ref)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	if err := s.writable(ctx, rec.ID); err != nil {
		return ports.FactoryRecord{}, err
	}

	return rec, nil
}

// proposal is f's proposal of number: errProposalNotFound when it has none.
func (s *server) proposal(ctx context.Context, f domain.FactoryID, number int64) (domain.Proposal, error) {
	p, err := s.jonah.Store.Proposal(ctx, f, number)
	if errors.Is(err, ports.ErrNotFound) {
		return domain.Proposal{}, fmt.Errorf("proposal %d: %w", number, errProposalNotFound)
	}

	if err != nil {
		return domain.Proposal{}, fmt.Errorf("read proposal %d: %w", number, err)
	}

	return p, nil
}

// jonahPolicy is f's learning.jonah as goald runs it, the defaults before the first publish.
func (s *server) jonahPolicy(ctx context.Context, f domain.FactoryID) domain.JonahPolicy {
	if fc, err := s.factories.Lookup(ctx, string(f)); err == nil {
		return fc.Line().Jonah
	}

	return domain.DefaultJonahPolicy()
}

// factoryLadder is every factory role, lowest first.
var factoryLadder = []domain.FactoryRole{domain.FactoryViewer, domain.FactoryApprover, domain.FactoryEditor}

// rolesHolding are the roles that hold permission: organization roles first (owners and admins
// hold every factory permission), then the factory roles at or above its lowest.
func rolesHolding(permission string) []string {
	r := permissionRules[permission]
	if r.level != levelFactory {
		return append([]string{}, r.orgRoles...)
	}

	out := []string{string(domain.OrgOwner), string(domain.OrgAdmin)}

	for _, role := range factoryLadder {
		if role.Includes(domain.FactoryRole(r.factoryRole)) {
			out = append(out, string(role))
		}
	}

	return out
}

// proposalEntry is p as the list shows it.
func proposalEntry(p domain.Proposal) gen.ProposalEntry {
	e := gen.ProposalEntry{
		Number: p.Number, Title: p.Title, State: gen.ProposalState(p.State), CreatedAt: p.CreatedAt, Paths: p.Paths(),
		Agents: []string{}, EvidenceCount: len(p.Evidence),
	}

	for _, path := range e.Paths {
		if name, ok := strings.CutPrefix(path, "agents/"); ok && strings.HasSuffix(name, ".md") && !strings.Contains(name, "/") {
			e.Agents = append(e.Agents, strings.TrimSuffix(name, ".md"))
		}
	}

	if est := p.Estimate; est.Metric != "" {
		e.Estimate = &gen.ProposalEstimate{
			Metric: gen.ProposalEstimateMetric(est.Metric), Station: est.Station, Before: est.Before, After: est.After,
			CostPerItemUsd: est.CostPerItemUSD,
		}
	}

	return e
}

// reviewOut is r as the API shows it.
func reviewOut(r domain.JonahReview) gen.JonahReview {
	out := gen.JonahReview{Id: string(r.ID), State: gen.JonahReviewState(r.State), Trigger: gen.JonahReviewTrigger(r.Trigger), CreatedAt: r.CreatedAt}
	if r.Error != "" {
		out.Error = &r.Error
	}

	if !r.FinishedAt.IsZero() {
		out.FinishedAt = &r.FinishedAt
	}

	return out
}

// JonahAuthoring is the definition authoring Jonah's decisions work through (jonah.Authoring),
// over the API's Definitions: approve and edit go through the same drafts, validation and gates
// as the builder.
type JonahAuthoring struct {
	defs Definitions
}

// NewJonahAuthoring returns Jonah's authoring over defs (*authoring.Service).
func NewJonahAuthoring(defs Definitions) *JonahAuthoring { return &JonahAuthoring{defs: defs} }

// Head is f's current version and whether a draft is open.
func (a *JonahAuthoring) Head(ctx context.Context, f domain.FactoryID) (current domain.DefinitionVersionID, draftOpen bool, err error) {
	rec, err := a.defs.Factory(ctx, string(f))
	if err != nil {
		return "", false, fmt.Errorf("read factory %s: %w", f, err)
	}

	if rec.Current != nil {
		current = rec.Current.ID
	}

	return current, rec.Draft != nil, nil
}

// CurrentFile is path in f's current version.
func (a *JonahAuthoring) CurrentFile(ctx context.Context, f domain.FactoryID, path string) ([]byte, bool, error) {
	return a.file(ctx, f, authoring.SelectCurrent, path)
}

// VersionFile is path in f's published version.
func (a *JonahAuthoring) VersionFile(ctx context.Context, f domain.FactoryID, version domain.DefinitionVersionID, path string) ([]byte, bool, error) {
	file, err := a.defs.VersionFileByID(ctx, f, version, path)
	if errors.Is(err, authoring.ErrFileNotFound) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, err
	}

	return file.Content, true, nil
}

// DraftFile is path in f's open draft.
func (a *JonahAuthoring) DraftFile(ctx context.Context, f domain.FactoryID, path string) ([]byte, bool, error) {
	return a.file(ctx, f, authoring.SelectDraft, path)
}

func (a *JonahAuthoring) file(ctx context.Context, f domain.FactoryID, kind authoring.SelectorKind, path string) ([]byte, bool, error) {
	file, err := a.defs.VersionFile(ctx, f, authoring.VersionSelector{Kind: kind}, path)
	if errors.Is(err, authoring.ErrFileNotFound) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, err
	}

	return file.Content, true, nil
}

// OpenDraft opens f's draft from its current version, or returns the open one.
func (a *JonahAuthoring) OpenDraft(ctx context.Context, f domain.FactoryID) (int64, bool, error) {
	d, created, err := a.defs.OpenDraft(ctx, f, authoring.OpenDraftRequest{})
	if err != nil {
		return 0, false, err
	}

	return d.Draft.Revision, created, nil
}

// WriteFile writes path into f's draft.
func (a *JonahAuthoring) WriteFile(ctx context.Context, f domain.FactoryID, path string, content []byte) (int64, error) {
	saved, err := a.defs.PutFile(ctx, f, authoring.FileWrite{Path: path, Content: content})
	if err != nil {
		return 0, err
	}

	return saved.Revision, nil
}

// Publish publishes f's draft at revision; the eval gate's refusal keeps the draft.
func (a *JonahAuthoring) Publish(ctx context.Context, f domain.FactoryID, revision int64, expected domain.DefinitionVersionID, message string) (domain.DefinitionVersion, bool, error) {
	v, err := a.defs.Publish(ctx, f, authoring.PublishRequest{Revision: revision, ExpectedCurrent: expected, Message: message})
	_, gated := errors.AsType[*authoring.EvalGateError](err)

	return v, gated, err
}

// DraftChange is f's open draft as a change of its current version (jonah.Drafts): each file it
// changes or adds, whole, with the current file's hash when it changes one, the paths it deletes,
// and domain.ContentKey of its files, read from their hashes. An error wrapping
// authoring.ErrNoDraft when none is open; no version before the first publish.
func (a *JonahAuthoring) DraftChange(ctx context.Context, f domain.FactoryID) (jonah.DraftChange, error) {
	draft, err := a.defs.DraftFiles(ctx, f)
	if err != nil {
		return jonah.DraftChange{}, err
	}

	current, _, err := a.Head(ctx, f)
	if err != nil || current == "" {
		return jonah.DraftChange{}, err
	}

	cur, err := a.defs.Version(ctx, f, authoring.VersionSelector{Kind: authoring.SelectCurrent})
	if err != nil {
		return jonah.DraftChange{}, fmt.Errorf("read the current version of %s: %w", f, err)
	}

	was := make(map[string]domain.BlobHash, len(cur.Files))
	for _, fi := range cur.Files {
		was[fi.Path] = fi.SHA256
	}

	out := jonah.DraftChange{Version: current}
	m := make(domain.Manifest, len(draft))

	for _, fi := range draft {
		m[fi.Path] = fi.SHA256

		before, ok := was[fi.Path]
		if ok && before == fi.SHA256 {
			continue
		}

		content, _, err := a.DraftFile(ctx, f, fi.Path)
		if err != nil {
			return jonah.DraftChange{}, fmt.Errorf("read %s of the draft: %w", fi.Path, err)
		}

		out.Edits = append(out.Edits, domain.ProposalEdit{Path: fi.Path, Content: string(content), BaseSHA256: string(before)})
	}

	for _, fi := range cur.Files {
		if _, ok := m[fi.Path]; !ok {
			out.Deleted = append(out.Deleted, fi.Path)
		}
	}

	slices.SortFunc(out.Edits, func(a, b domain.ProposalEdit) int { return strings.Compare(a.Path, b.Path) })
	slices.Sort(out.Deleted)
	out.ContentKey = string(domain.ManifestSHA256(m))

	return out, nil
}

// DiscardDraft discards f's draft at revision.
func (a *JonahAuthoring) DiscardDraft(ctx context.Context, f domain.FactoryID, revision int64) error {
	return a.defs.DiscardDraft(ctx, f, revision)
}
