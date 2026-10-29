package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// JonahDeps serve the Jonah routes (ADR 0034). Store reads reviews, proposals and observations
// (a ports.JonahStore satisfies it) and Decisions decides proposals and queues reviews
// (*jonah.Decider satisfies it, over NewJonahAuthoring).
type JonahDeps struct {
	Store     JonahReads
	Decisions JonahDecisions
}

// JonahReads are the reads of ports.JonahStore the Jonah routes make.
type JonahReads interface {
	JonahReviews(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.JonahReview, error)
	Proposals(ctx context.Context, q ports.ProposalQuery) ([]domain.Proposal, error)
	Proposal(ctx context.Context, factory domain.FactoryID, number int64) (domain.Proposal, error)
	Observations(ctx context.Context, factory domain.FactoryID, limit int) ([]domain.Observation, error)
	JonahSpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (float64, error)
}

// JonahDecisions decide proposals and queue manual reviews as the person in ctx; their refusals
// are domain.ErrProposalDraftOpen, ErrProposalStale, ErrProposalDraftConflict, ErrReviewInFlight,
// ErrLearningBudgetExhausted and ports.ErrProposalDecided.
type JonahDecisions interface {
	Approve(ctx context.Context, f domain.FactoryID, number int64) (domain.Proposal, domain.DefinitionVersion, error)
	Edit(ctx context.Context, f domain.FactoryID, number int64) (domain.Proposal, int64, error)
	Reject(ctx context.Context, f domain.FactoryID, number int64, reason string) (domain.Proposal, error)
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

// reviewLoopRunning says whether goald runs Jonah's review loop (software#344). Until it does, a
// queued review waits for a claim that never comes, so QueueJonahReview refuses with
// review-loop-not-running. software#344 sets this to true or deletes the gate.
var reviewLoopRunning = false

var (
	errJonahUnavailable     = errors.New("this goald serves no Jonah store")
	errProposalNotFound     = errors.New("no such proposal")
	errReviewLoopNotRunning = errors.New("review loop not running")
)

// reviewLoopNotRunning is the 409 a review gets while no loop runs. Its detail is the sentence a
// person reads, not the sentinel's text.
func reviewLoopNotRunning() gen.Problem {
	code := "review-loop-not-running"
	detail := "Reviews start when Jonah's review loop ships"

	return gen.Problem{Title: http.StatusText(http.StatusConflict), Status: http.StatusConflict, Code: &code, Detail: &detail}
}

// jonahProblem answers the Jonah routes' own errors.
func jonahProblem(err error) (gen.Problem, bool) {
	var status int

	var code string

	switch {
	case errors.Is(err, errReviewLoopNotRunning):
		return reviewLoopNotRunning(), true
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
		return gen.Problem{}, false
	}

	p := detailed(status, err)
	p.Code = &code

	return p, true
}

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

	observations, err := s.weekObservations(ctx, rec.ID, policy, weekStart(now))
	if err != nil {
		return nil, err
	}

	out := gen.GetJonahOverview200JSONResponse{
		Policy: gen.JonahPolicy{
			Enabled: policy.Enabled, ProposeAfter: policy.ProposeAfter, WeeklyUsd: policy.WeeklyUSD, ApproverRoles: rolesHolding("definition:publish"),
		},
		Waiting: len(waiting), MonthCostUsd: month, Observations: observations,
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

// weekObservations are f's observations made since since, newest first, each with the number
// of the proposal it became.
func (s *server) weekObservations(ctx context.Context, f domain.FactoryID, policy domain.JonahPolicy, since time.Time) ([]gen.JonahObservation, error) {
	obs, err := s.jonah.Store.Observations(ctx, f, overviewObservations)
	if err != nil {
		return nil, fmt.Errorf("read observations: %w", err)
	}

	numbers := map[domain.ProposalID]int64{}

	out := []gen.JonahObservation{}

	for _, o := range obs {
		if o.CreatedAt.Before(since) {
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

	made, err := s.versions.Version(ctx, p.VersionID)
	if err != nil {
		return nil, fmt.Errorf("read version %s of proposal %d: %w", p.VersionID, p.Number, err)
	}

	e := proposalEntry(p)
	out := gen.GetProposal200JSONResponse{
		Number: e.Number, Title: e.Title, State: e.State, CreatedAt: e.CreatedAt, Paths: e.Paths, Agents: e.Agents,
		Estimate: e.Estimate, EvidenceCount: e.EvidenceCount, Summary: p.Summary,
		Evidence:           make([]gen.ProposalEvidence, 0, len(p.Evidence)),
		MadeAgainstVersion: made.Number, PublishesAsVersion: rec.LastVersionNumber + 1,
	}

	if out.Files, err = s.proposalFilesOut(ctx, rec.ID, p, made.Number); err != nil {
		return nil, err
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
			return nil, fmt.Errorf("read the version proposal %d published: %w", p.Number, err)
		}

		out.PublishedVersion = &v.Number
	}

	if p.RejectReason != "" {
		out.RejectReason = &p.RejectReason
	}

	if p.DecidedBy != "" {
		out.DecidedBy, out.DecidedAt = &p.DecidedBy, &p.DecidedAt
	}

	return out, nil
}

// proposalFilesOut are p's files, each with its content in version made of f (empty for a new
// file).
func (s *server) proposalFilesOut(ctx context.Context, f domain.FactoryID, p domain.Proposal, made int) ([]gen.ProposalFile, error) {
	out := make([]gen.ProposalFile, 0, len(p.Files))

	for _, file := range p.Files {
		base := ""

		if file.BaseSHA256 != "" {
			b, err := s.defs.VersionFile(ctx, f, authoring.VersionSelector{Kind: authoring.SelectNumber, Number: made}, file.Path)
			if err != nil && !errors.Is(err, authoring.ErrFileNotFound) {
				return nil, fmt.Errorf("read %s of v%d: %w", file.Path, made, err)
			}

			base = string(b.Content)
		}

		out = append(out, gen.ProposalFile{Path: file.Path, BaseSha256: file.BaseSHA256, BaseContent: base, Content: file.Content})
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

	if !reviewLoopRunning {
		return nil, errReviewLoopNotRunning
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

// DiscardDraft discards f's draft at revision.
func (a *JonahAuthoring) DiscardDraft(ctx context.Context, f domain.FactoryID, revision int64) error {
	return a.defs.DiscardDraft(ctx, f, revision)
}
