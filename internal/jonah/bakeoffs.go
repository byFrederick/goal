package jonah

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// ErrLearningBudgetExhausted refuses a bake-off whose estimate exceeds what is left of the
// learning budget (ADR 0029 decision 5); the API answers it with 409 learning-budget-exhausted.
var ErrLearningBudgetExhausted = errors.New("learning budget exhausted")

// LearningBudget is what is left of the organization's learning budget, for the organization in
// ctx, in USD.
type LearningBudget interface {
	RemainingUSD(ctx context.Context) (float64, error)
}

// DraftModels writes an agent's model into a factory's draft (authoring.Service.SetAgentModel):
// it opens a draft from the current version when none is open, never publishes, and refuses with
// an error wrapping ports.ErrConflict when the agent changed since version base.
type DraftModels interface {
	SetAgentModel(ctx context.Context, factory domain.FactoryID, base domain.DefinitionVersionID, agent, model string) error
}

// BakeOffContent is the published version a bake-off runs, as the caller resolved it: its line
// (factory, version, stations and agents), its content hash and its eval cases.
type BakeOffContent struct {
	Line          domain.Line
	ContentSHA256 domain.BlobHash
	Cases         []domain.EvalCaseSpec
}

// BakeOffRequest asks for a bake-off of Agent on Models (none: the agent's model and the default
// models) with Trials tries per case (0: domain.DefaultBakeOffTries).
type BakeOffRequest struct {
	Agent  string
	Models []string
	Trials int
}

// StartBakeOff queues a bake-off of c's version for the organization in ctx (ADR 0029): every case
// of r.Agent, r.Trials times on every arm, estimated at cases × tries × arms × the price Estimate
// gives a try. It returns the queued bake-off and the estimate. domain.ErrInvalid when the agent is
// not in c's line or at none of its stations, or the bake-off does not pass domain.NewBakeOff.Check
// (2 to 4 distinct arms, one of them the agent's model; 1 to 10 tries; at least one case).
// ErrLearningBudgetExhausted, with the estimate, when it exceeds what is left of the budget.
func (e *Evals) StartBakeOff(ctx context.Context, c BakeOffContent, r BakeOffRequest, by domain.Actor) (domain.BakeOff, EvalEstimate, error) {
	agent := slices.IndexFunc(c.Line.Agents, func(a domain.AgentSpec) bool { return a.Name == r.Agent })
	if agent < 0 {
		return domain.BakeOff{}, EvalEstimate{}, fmt.Errorf("start bake-off: version %s has no agent %q: %w", c.Line.VersionID, r.Agent, domain.ErrInvalid)
	}

	station := agentStation(c.Line, r.Agent)
	if station == "" {
		return domain.BakeOff{}, EvalEstimate{}, fmt.Errorf("start bake-off: agent %q works at no station: %w", r.Agent, domain.ErrInvalid)
	}

	current := c.Line.Agents[agent].Model
	if current == "" {
		current = c.Line.Runtime.Model
	}

	models := r.Models
	if len(models) == 0 {
		models = defaultArms(current, e.d.BakeOffModels)
	}

	n := domain.NewBakeOff{
		FactoryID: c.Line.FactoryID, VersionID: c.Line.VersionID, ContentSHA256: c.ContentSHA256, Station: station, Agent: r.Agent,
		TriesPerCase: r.Trials,
	}
	if n.TriesPerCase == 0 {
		n.TriesPerCase = domain.DefaultBakeOffTries
	}

	for _, m := range models {
		n.Arms = append(n.Arms, domain.NewBakeOffArm{Model: m, Current: m == current})
	}

	var cases []domain.EvalCaseSpec

	for _, s := range c.Cases {
		if s.Agent == r.Agent {
			s.Tries = n.TriesPerCase
			cases = append(cases, s)
			n.Cases = append(n.Cases, domain.EvalCase{Name: s.Name, SHA256: s.SHA256, Tries: n.TriesPerCase})
		}
	}

	if err := n.Check(); err != nil {
		return domain.BakeOff{}, EvalEstimate{}, fmt.Errorf("start bake-off: %w", err)
	}

	est, err := e.Estimate(ctx, n.FactoryID, cases)
	if err != nil {
		return domain.BakeOff{}, EvalEstimate{}, err
	}

	est.USD *= float64(len(n.Arms))
	n.EstimateUSD = est.USD

	if err := e.withinBudget(ctx, est.USD); err != nil {
		return domain.BakeOff{}, est, err
	}

	b, err := e.d.Evals.CreateBakeOff(ctx, n, by)
	if err != nil {
		return domain.BakeOff{}, EvalEstimate{}, fmt.Errorf("start bake-off: %w", err)
	}

	e.log.InfoContext(ctx, "bake-off queued", slog.String("eval_run_id", string(b.ID)), slog.Int("arms", len(n.Arms)),
		slog.Int("cases", len(n.Cases)), slog.Int("tries_per_case", n.TriesPerCase))

	return b, est, nil
}

// RerunBakeOff queues a new bake-off of bake-off id's agent with its arms and tries per case, on
// c, the factory's current version (ADR 0029 decision 7), as StartBakeOff does. ErrNotFound when
// there is no such bake-off.
func (e *Evals) RerunBakeOff(ctx context.Context, id domain.EvalRunID, c BakeOffContent, by domain.Actor) (domain.BakeOff, EvalEstimate, error) {
	b, err := e.d.Evals.GetBakeOff(ctx, id)
	if err != nil {
		return domain.BakeOff{}, EvalEstimate{}, fmt.Errorf("run bake-off %s again: %w", id, err)
	}

	r := BakeOffRequest{Agent: b.Agent, Trials: b.TriesPerCase}
	for _, a := range b.Arms {
		r.Models = append(r.Models, a.Model)
	}

	return e.StartBakeOff(ctx, c, r, by)
}

// ApplyBakeOff writes arm's model as the bake-off's agent's model into the factory's draft
// (ADR 0029 decision 7) and returns the arm. It never publishes. domain.ErrInvalid when the
// bake-off has no such arm; the draft's refusal (ports.ErrConflict when the agent changed since
// the bake-off's version) as it is.
func (e *Evals) ApplyBakeOff(ctx context.Context, id domain.EvalRunID, arm int) (domain.BakeOffArm, error) {
	if e.d.Drafts == nil {
		return domain.BakeOffArm{}, fmt.Errorf("apply bake-off %s: no draft writer is configured", id)
	}

	b, err := e.d.Evals.GetBakeOff(ctx, id)
	if err != nil {
		return domain.BakeOffArm{}, fmt.Errorf("apply bake-off %s: %w", id, err)
	}

	i := slices.IndexFunc(b.Arms, func(a domain.BakeOffArm) bool { return a.Arm == arm })
	if i < 0 {
		return domain.BakeOffArm{}, fmt.Errorf("apply bake-off %s: it has no arm %d: %w", id, arm, domain.ErrInvalid)
	}

	a := b.Arms[i]
	if err := e.d.Drafts.SetAgentModel(ctx, b.FactoryID, b.VersionID, b.Agent, a.Model); err != nil {
		return domain.BakeOffArm{}, fmt.Errorf("apply bake-off %s arm %d: %w", id, arm, err)
	}

	e.log.InfoContext(ctx, "bake-off applied", slog.String("eval_run_id", string(id)), slog.Int("arm", arm))

	return a, nil
}

// withinBudget refuses an estimate over what is left of the learning budget, when one is kept.
func (e *Evals) withinBudget(ctx context.Context, usd float64) error {
	if e.d.Budget == nil {
		return nil
	}

	left, err := e.d.Budget.RemainingUSD(ctx)
	if err != nil {
		return fmt.Errorf("start bake-off: learning budget: %w", err)
	}

	if usd > left {
		return fmt.Errorf("start bake-off: the estimate $%.2f exceeds the $%.2f left of the learning budget: %w", usd, left, ErrLearningBudgetExhausted)
	}

	return nil
}

// agentStation is the first station of l that agent works at, by its agent or a route; empty
// when there is none.
func agentStation(l domain.Line, agent string) string {
	for _, s := range l.Stations {
		if s.Agent == agent {
			return s.ID
		}

		for _, a := range s.Route {
			if a == agent {
				return s.ID
			}
		}
	}

	return ""
}

// defaultArms are the arms of a bake-off that names none: the current model, then the default
// models without it, cut at domain.MaxBakeOffArms.
func defaultArms(current string, defaults []string) []string {
	out := []string{current}

	for _, m := range defaults {
		if len(out) == domain.MaxBakeOffArms {
			break
		}

		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}

	return out
}

// armModel is the model of arm of bake-off id, read once per Work; false when it has no such arm.
func (e *Evals) armModel(ctx context.Context, id domain.EvalRunID, arm int, cache *workCache) (string, bool, error) {
	arms, ok := cache.arms[id]
	if !ok {
		b, err := e.d.Evals.GetBakeOff(ctx, id)
		if err != nil {
			// Left running: the lease ends and another claim takes the try again.
			return "", false, fmt.Errorf("bake-off %s: %w", id, err)
		}

		arms = b.Arms
		cache.arms[id] = arms
	}

	i := slices.IndexFunc(arms, func(a domain.BakeOffArm) bool { return a.Arm == arm })
	if i < 0 {
		return "", false, nil
	}

	return arms[i].Model, true, nil
}

// summarize writes each arm's summary of bake-off id from its finished tries (ADR 0029 decision
// 3). The judged share is left empty: no try is scored by the advisory judge yet.
func (e *Evals) summarize(ctx context.Context, id domain.EvalRunID) error {
	b, err := e.d.Evals.GetBakeOff(ctx, id)
	if err != nil {
		return fmt.Errorf("summarize bake-off %s: %w", id, err)
	}

	for _, a := range b.Arms {
		s := armSummary(b, a.Arm)
		if err := e.d.Evals.RecordArmSummary(ctx, id, a.Arm, s); err != nil {
			return fmt.Errorf("summarize bake-off %s arm %d: %w", id, a.Arm, err)
		}
	}

	return nil
}

// armSummary is arm's row of b's report: its tries, passes and errors, the means of what they
// spent, and the passes per case, every case of the bake-off listed.
func armSummary(b domain.BakeOff, arm int) domain.ArmSummary {
	s := domain.ArmSummary{PerCase: map[string]int{}}

	var (
		cost  float64
		took  time.Duration
		turns int
	)

	for _, r := range b.Results {
		if _, ok := s.PerCase[r.Case]; !ok {
			s.PerCase[r.Case] = 0
		}

		if r.Arm != arm {
			continue
		}

		s.Tries++
		cost, took, turns = cost+r.CostUSD, took+r.Duration, turns+r.Turns

		switch r.State {
		case domain.EvalPass:
			s.Passed++
			s.PerCase[r.Case]++
		case domain.EvalError:
			s.Errors++
		case domain.EvalPending, domain.EvalRunning, domain.EvalFail:
		}
	}

	if s.Tries > 0 {
		s.CostPerItemUSD = cost / float64(s.Tries)
		s.TimePerItem = took / time.Duration(s.Tries)
		s.MeanTurns = float64(turns) / float64(s.Tries)
	}

	return s
}
