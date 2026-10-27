package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The evals routes (ADR 0025): a factory's eval cases are its definition's evals/cases/<name>.yaml
// files, which authoring parses; their runs and tries are in ports.Evals, and Jonah's eval loop
// queues, prices and cancels runs. Adding a case writes its file into a draft the handler opens
// and publishes it, through the same checks and eval gate as any publish (decision 5).

// EvalDeps serve the evals routes. Runs reads eval runs (a ports.Evals satisfies it), Loop starts,
// prices and cancels them (*jonah.Evals satisfies it) and Judges names the judges cases cite.
type EvalDeps struct {
	Runs   EvalRunReads
	Loop   EvalLoop
	Judges ports.Judges
}

// EvalRunReads are the reads of ports.Evals the evals routes make.
type EvalRunReads interface {
	GetRun(ctx context.Context, id domain.EvalRunID) (domain.EvalRun, error)
	CaseHistory(ctx context.Context, factory domain.FactoryID) ([]domain.EvalCaseHistory, error)
	LatestRunForContent(ctx context.Context, factory domain.FactoryID, sha256 domain.BlobHash) (domain.EvalRun, error)
}

// EvalLoop starts, prices and cancels eval runs; *jonah.Evals satisfies it.
type EvalLoop interface {
	Start(ctx context.Context, n domain.NewEvalRun, cases []domain.EvalCaseSpec, by domain.Actor) (domain.EvalRun, domain.EvalEstimate, error)
	Estimate(ctx context.Context, factory domain.FactoryID, cases []domain.EvalCaseSpec) (domain.EvalEstimate, error)
	Cancel(ctx context.Context, id domain.EvalRunID) (domain.EvalRun, error)
}

// Limits of a case written from a past run, as evals.schema.json has them.
const (
	maxEvalDescription = 200
	maxEvalTitle       = 300
	maxEvalTries       = 10
)

// evalCaseSlug is the form of a case's name, its file name: evals.schema.json's slug.
var evalCaseSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

// Errors of the evals routes.
var (
	// errEvalsUnavailable is a goald that does not run evals (503): Deps.Evals is nil.
	errEvalsUnavailable = errors.New("this goald does not run evals yet")
	errEvalRunNotFound  = errors.New("eval run not found")
	errEvalRunFinished  = errors.New("the eval run is already finished")
	errEvalCaseUnknown  = errors.New("no such eval case")
	errEvalCaseExists   = errors.New("the current version already has an eval case of this name")
	// errEvalDraftOpen refuses adding a case while the factory has an open draft, which it would
	// otherwise publish along with the case.
	errEvalDraftOpen = errors.New("the factory has an open draft: publish or discard it first")
)

// evalProblem answers the evals routes' own errors.
func evalProblem(err error) (gen.Problem, bool) {
	var (
		status int
		code   string
	)

	switch {
	case errors.Is(err, errEvalsUnavailable):
		status, code = http.StatusServiceUnavailable, "evals-unavailable"
	case errors.Is(err, errEvalRunNotFound):
		status, code = http.StatusNotFound, "eval-run-not-found"
	case errors.Is(err, errEvalCaseUnknown):
		status, code = http.StatusBadRequest, "eval-case-unknown"
	case errors.Is(err, errEvalRunFinished):
		status, code = http.StatusConflict, "eval-run-finished"
	case errors.Is(err, errEvalCaseExists):
		status, code = http.StatusConflict, "eval-case-exists"
	case errors.Is(err, errEvalDraftOpen):
		status, code = http.StatusConflict, "draft-open"
	default:
		return gen.Problem{}, false
	}

	p := detailed(status, err)
	p.Code = &code

	return p, true
}

func (s *server) ListEvals(ctx context.Context, req gen.ListEvalsRequestObject) (gen.ListEvalsResponseObject, error) {
	rec, err := s.evalFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	out := gen.ListEvals200JSONResponse{Cases: []gen.EvalCaseEntry{}}
	if rec.Current == nil {
		return out, nil
	}

	content, err := s.defs.EvalContent(ctx, rec.ID, authoring.VersionSelector{Kind: authoring.SelectCurrent})
	if err != nil {
		return nil, err
	}

	hist, err := s.evals.Runs.CaseHistory(ctx, rec.ID)
	if err != nil {
		return nil, fmt.Errorf("read eval case history: %w", err)
	}

	byCase := make(map[string]domain.EvalCaseHistory, len(hist))
	for _, h := range hist {
		byCase[h.Case] = h
	}

	titles := map[string]string{}
	if slices.ContainsFunc(content.Cases, func(c domain.EvalCaseSpec) bool { return c.Judge != "" }) {
		if titles, err = s.judgeTitles(ctx, rec.ID, rec.Current.ID); err != nil {
			return nil, err
		}
	}

	out.Summary = gen.EvalSummary{Version: &rec.Current.Number, Cases: len(content.Cases)}

	for _, c := range content.Cases {
		h := byCase[c.Name]
		entry := gen.EvalCaseEntry{
			Name: c.Name, Description: c.Description, Agent: c.Agent, Tries: &c.Tries,
			CheckedBy: checkedByOut(c, titles), LastTries: make([]gen.EvalHistoryTry, 0, len(h.Tries)),
		}

		passed := 0

		for _, t := range h.Tries {
			entry.LastTries = append(entry.LastTries, gen.EvalHistoryTry{
				EvalRunId: string(t.EvalRunID), Try: t.Try, State: gen.EvalResultState(t.State), RunId: optional(string(t.RunID)), CostUsd: t.CostUSD,
			})

			if t.State == domain.EvalPass {
				passed++
			}
		}

		if h.LatestRunID != "" {
			entry.CostUsd = &h.LatestCostUSD
		}

		out.Summary.Tries += len(h.Tries)
		out.Summary.TriesPassed += passed

		if len(h.Tries) > 0 && passed == len(h.Tries) {
			out.Summary.CasesPassing++
		}

		out.Cases = append(out.Cases, entry)
	}

	if out.Summary.Tries > 0 {
		rate := float64(out.Summary.TriesPassed) / float64(out.Summary.Tries)
		out.Summary.FirstTryPassRate = &rate
	}

	if err := s.latestEvalRun(ctx, rec.ID, content.SHA256, &out.Summary); err != nil {
		return nil, err
	}

	return out, nil
}

// latestEvalRun sets the summary's latest run: the newest of the content hash sha256, when one
// exists.
func (s *server) latestEvalRun(ctx context.Context, factory domain.FactoryID, sha256 domain.BlobHash, sum *gen.EvalSummary) error {
	latest, err := s.evals.Runs.LatestRunForContent(ctx, factory, sha256)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read latest eval run: %w", err)
	}

	run, err := s.evals.Runs.GetRun(ctx, latest.ID)
	if err != nil {
		return fmt.Errorf("read eval run %s: %w", latest.ID, err)
	}

	at := run.CreatedAt
	if !run.FinishedAt.IsZero() {
		at = run.FinishedAt
	}

	tries := 0
	for _, r := range run.Results {
		tries = max(tries, r.Try)
	}

	id := string(run.ID)
	sum.LatestRunId, sum.LatestRunAt, sum.LatestTriesPerCase = &id, &at, &tries

	return nil
}

func (s *server) StartEvalRun(ctx context.Context, req gen.StartEvalRunRequestObject) (gen.StartEvalRunResponseObject, error) {
	rec, err := s.evalFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("start eval run: the request has no actor")
	}

	content, name, err := s.evalCases(ctx, rec, *req.Body)
	if err != nil {
		return nil, err
	}

	n := domain.NewEvalRun{FactoryID: rec.ID, ContentSHA256: content.SHA256, Trigger: domain.EvalManual, Case: name}
	if content.Version.State == domain.VersionPublished {
		n.VersionID = content.Version.ID
	}

	run, est, err := s.evals.Loop.Start(ctx, n, content.Cases, by)
	if err != nil {
		return nil, err
	}

	out, err := s.evalRunOut(ctx, run)
	if err != nil {
		return nil, err
	}

	return gen.StartEvalRun202JSONResponse{Run: out, Estimate: estimateOut(est)}, nil
}

func (s *server) EstimateEvalRun(ctx context.Context, req gen.EstimateEvalRunRequestObject) (gen.EstimateEvalRunResponseObject, error) {
	rec, err := s.evalFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	content, name, err := s.evalCases(ctx, rec, *req.Body)
	if err != nil {
		return nil, err
	}

	cases := content.Cases
	if name != "" {
		cases = slices.DeleteFunc(slices.Clone(cases), func(c domain.EvalCaseSpec) bool { return c.Name != name })
	}

	est, err := s.evals.Loop.Estimate(ctx, rec.ID, cases)
	if err != nil {
		return nil, err
	}

	return gen.EstimateEvalRun200JSONResponse(estimateOut(est)), nil
}

func (s *server) GetEvalRun(ctx context.Context, req gen.GetEvalRunRequestObject) (gen.GetEvalRunResponseObject, error) {
	rec, err := s.evalFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	run, err := s.evalRun(ctx, rec.ID, req.EvalRun)
	if err != nil {
		return nil, err
	}

	out, err := s.evalRunOut(ctx, run)
	if err != nil {
		return nil, err
	}

	return gen.GetEvalRun200JSONResponse(out), nil
}

func (s *server) CancelEvalRun(ctx context.Context, req gen.CancelEvalRunRequestObject) (gen.CancelEvalRunResponseObject, error) {
	rec, err := s.evalFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if _, err := s.evalRun(ctx, rec.ID, req.EvalRun); err != nil {
		return nil, err
	}

	if _, err := s.evals.Loop.Cancel(ctx, domain.EvalRunID(req.EvalRun)); errors.Is(err, ports.ErrConflict) {
		return nil, fmt.Errorf("eval run %s: %w", req.EvalRun, errEvalRunFinished)
	} else if err != nil {
		return nil, err
	}

	run, err := s.evalRun(ctx, rec.ID, req.EvalRun)
	if err != nil {
		return nil, err
	}

	out, err := s.evalRunOut(ctx, run)
	if err != nil {
		return nil, err
	}

	return gen.CancelEvalRun200JSONResponse(out), nil
}

func (s *server) AddEvalCase(ctx context.Context, req gen.AddEvalCaseRequestObject) (gen.AddEvalCaseResponseObject, error) {
	rec, err := s.evalFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	b := req.Body
	if !evalCaseSlug.MatchString(b.Name) {
		return nil, fmt.Errorf("%w: case name %q is not 3 to 40 lowercase letters, digits and hyphens starting with a letter", errInvalid, b.Name)
	}

	tries := deref(b.Tries)
	if b.Tries == nil {
		tries = 1
	}

	if tries < 1 || tries > maxEvalTries {
		return nil, fmt.Errorf("%w: tries %d is not 1 to %d", errInvalid, tries, maxEvalTries)
	}

	if utf8.RuneCountInString(deref(b.Description)) > maxEvalDescription {
		return nil, fmt.Errorf("%w: a case description has at most %d characters", errInvalid, maxEvalDescription)
	}

	r, err := s.run(ctx, b.Run)
	if err != nil {
		return nil, err
	}

	if r.FactoryID != rec.ID {
		return nil, errRunNotFound
	}

	it, err := s.items.Item(ctx, r.ItemID)
	if err != nil {
		return nil, fmt.Errorf("read item %s of run %s: %w", r.ItemID, r.ID, err)
	}

	if rec.Current == nil {
		return nil, fmt.Errorf("factory %s was never published: %w", rec.ID, authoring.ErrVersionNotFound)
	}

	current, err := s.defs.EvalContent(ctx, rec.ID, authoring.VersionSelector{Kind: authoring.SelectCurrent})
	if err != nil {
		return nil, err
	}

	if slices.ContainsFunc(current.Cases, func(c domain.EvalCaseSpec) bool { return c.Name == b.Name }) {
		return nil, fmt.Errorf("eval case %s: %w", b.Name, errEvalCaseExists)
	}

	judge := deref(b.Judge)
	if judge != "" {
		titles, err := s.judgeTitles(ctx, rec.ID, rec.Current.ID)
		if err != nil {
			return nil, err
		}

		if _, ok := titles[judge]; !ok {
			return nil, fmt.Errorf("%w: the current version has no judge %q", errInvalid, judge)
		}
	}

	description := deref(b.Description)
	if description == "" {
		description = truncateRunes(it.Title, maxEvalDescription)
	}

	content, err := evalCaseFile(domain.EvalCaseSpec{
		Agent: r.Agent, Description: description, Tries: tries, Contract: judge == "", Judge: judge,
		Input: domain.EvalInput{Title: truncateRunes(it.Title, maxEvalTitle), Body: it.Body, Type: it.Type, Lane: it.Lane},
	})
	if err != nil {
		return nil, err
	}

	v, err := s.publishEvalCase(ctx, rec, b.Name, content, deref(b.ExpectedCurrentVersionId))
	if err != nil {
		return nil, err
	}

	published, err := s.defs.Version(ctx, rec.ID, authoring.VersionSelector{Kind: authoring.SelectNumber, Number: v.Number})
	if err != nil {
		return nil, fmt.Errorf("read published version %d: %w", v.Number, err)
	}

	return gen.AddEvalCase201JSONResponse(s.versionOut(published.VersionInfo)), nil
}

// publishEvalCase writes the case file into a draft it opens from rec's current version and
// publishes it, refusing when another draft is open. A draft it opened that does not publish is
// discarded, unless the eval gate refused it: that draft stays open, so it publishes once a run
// of its cases passes.
func (s *server) publishEvalCase(ctx context.Context, rec ports.FactoryRecord, name string, content []byte, expected string) (domain.DefinitionVersion, error) {
	if rec.Draft != nil {
		return domain.DefinitionVersion{}, fmt.Errorf("eval case %s: %w", name, errEvalDraftOpen)
	}

	d, created, err := s.defs.OpenDraft(ctx, rec.ID, authoring.OpenDraftRequest{})
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	if !created { // another writer opened one since rec was read
		return domain.DefinitionVersion{}, fmt.Errorf("eval case %s: %w", name, errEvalDraftOpen)
	}

	revision := d.Draft.Revision

	v, err := func() (domain.DefinitionVersion, error) {
		saved, err := s.defs.PutFile(ctx, rec.ID, authoring.FileWrite{Path: config.CasesDir + "/" + name + ".yaml", Content: content, Create: true})
		if err != nil {
			return domain.DefinitionVersion{}, err
		}

		revision = saved.Revision

		if expected == "" {
			expected = string(rec.Current.ID)
		}

		return s.defs.Publish(ctx, rec.ID, authoring.PublishRequest{
			Revision: revision, ExpectedCurrent: domain.DefinitionVersionID(expected), Message: "Add eval case " + name,
		})
	}()

	_, gated := errors.AsType[*authoring.EvalGateError](err)
	if err != nil && !gated {
		if derr := s.defs.DiscardDraft(ctx, rec.ID, revision); derr != nil {
			s.log.WarnContext(ctx, "eval case draft not discarded", slog.String("factory", string(rec.ID)),
				slog.String("case", name), slog.Any("error", derr))
		}
	}

	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	return v, nil
}

// evalCaseDoc is an eval case file, with evals.schema.json's names in its order.
type evalCaseDoc struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Description string `yaml:"description"`
	} `yaml:"metadata"`
	Spec struct {
		Agent string `yaml:"agent"`
		Input struct {
			Title string `yaml:"title"`
			Body  string `yaml:"body"`
			Type  string `yaml:"type,omitempty"`
			Lane  string `yaml:"lane,omitempty"`
		} `yaml:"input"`
		Tries int            `yaml:"tries"`
		Check map[string]any `yaml:"check"`
	} `yaml:"spec"`
}

// evalCaseFile is c's evals/cases/<name>.yaml, checked by its contract or its judge.
func evalCaseFile(c domain.EvalCaseSpec) ([]byte, error) {
	var doc evalCaseDoc

	doc.APIVersion, doc.Kind, doc.Metadata.Description = "goal/v1", "EvalCase", c.Description
	doc.Spec.Agent, doc.Spec.Tries = c.Agent, c.Tries
	doc.Spec.Input.Title, doc.Spec.Input.Body, doc.Spec.Input.Type, doc.Spec.Input.Lane = c.Input.Title, c.Input.Body, c.Input.Type, c.Input.Lane

	doc.Spec.Check = map[string]any{"contract": true}
	if c.Judge != "" {
		doc.Spec.Check = map[string]any{"judge": c.Judge}
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("render eval case: %w", err)
	}

	return out, nil
}

// evalFactory resolves the path's factory, then refuses with 503 when this goald does not run
// evals: the factory is resolved and authorized first, so the answer tells nothing of a factory
// the caller may not see.
func (s *server) evalFactory(ctx context.Context, ref string) (ports.FactoryRecord, error) {
	rec, err := s.definition(ctx, ref)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	if s.evals == nil {
		return ports.FactoryRecord{}, errEvalsUnavailable
	}

	return rec, nil
}

// evalCases reads the cases a run request names: the content it runs on, and the one case it
// asks for ("" for every case).
func (s *server) evalCases(ctx context.Context, rec ports.FactoryRecord, b gen.EvalRunRequest) (authoring.EvalContent, string, error) {
	name := deref(b.Case)
	if b.Case != nil && !evalCaseSlug.MatchString(name) {
		return authoring.EvalContent{}, "", fmt.Errorf("%w: case name %q is not 3 to 40 lowercase letters, digits and hyphens starting with a letter", errInvalid, name)
	}

	sel := authoring.VersionSelector{Kind: authoring.SelectCurrent}

	switch deref(b.On) {
	case gen.EvalRunRequestOnDraft:
		sel.Kind = authoring.SelectDraft
	case gen.EvalRunRequestOnCurrent, "":
	default:
		return authoring.EvalContent{}, "", fmt.Errorf("%w: on %q is not current or draft", errInvalid, *b.On)
	}

	content, err := s.defs.EvalContent(ctx, rec.ID, sel)
	if err != nil {
		return authoring.EvalContent{}, "", err
	}

	if len(content.Cases) == 0 {
		return authoring.EvalContent{}, "", fmt.Errorf("the %s has no eval cases under %s: %w", sel, config.CasesDir, errEvalCaseUnknown)
	}

	if name != "" && !slices.ContainsFunc(content.Cases, func(c domain.EvalCaseSpec) bool { return c.Name == name }) {
		return authoring.EvalContent{}, "", fmt.Errorf("eval case %q of the %s: %w", name, sel, errEvalCaseUnknown)
	}

	return content, name, nil
}

// evalRun is eval run id of factory: errEvalRunNotFound when it is another factory's or none.
func (s *server) evalRun(ctx context.Context, factory domain.FactoryID, id string) (domain.EvalRun, error) {
	run, err := s.evals.Runs.GetRun(ctx, domain.EvalRunID(id))
	if errors.Is(err, ports.ErrNotFound) || err == nil && run.FactoryID != factory {
		return domain.EvalRun{}, fmt.Errorf("eval run %q: %w", id, errEvalRunNotFound)
	}

	if err != nil {
		return domain.EvalRun{}, fmt.Errorf("read eval run %s: %w", id, err)
	}

	return run, nil
}

// judgeTitles are the titles of a version's judges, by slug.
func (s *server) judgeTitles(ctx context.Context, factory domain.FactoryID, version domain.DefinitionVersionID) (map[string]string, error) {
	judges, err := s.evals.Judges.Judges(ctx, factory, version)
	if err != nil {
		return nil, fmt.Errorf("read judges: %w", err)
	}

	out := make(map[string]string, len(judges))
	for _, j := range judges {
		out[j.Slug] = j.Title
	}

	return out, nil
}

func checkedByOut(c domain.EvalCaseSpec, titles map[string]string) gen.EvalCheckedBy {
	switch {
	case c.Judge != "":
		out := gen.EvalCheckedBy{Kind: gen.EvalCheckedByKindJudge, Judge: &c.Judge}
		if t, ok := titles[c.Judge]; ok {
			out.Title = &t
		}

		return out
	case c.ReferencePatch != "":
		return gen.EvalCheckedBy{Kind: gen.EvalCheckedByKindReferencePatch, Path: &c.ReferencePatch}
	default:
		return gen.EvalCheckedBy{Kind: gen.EvalCheckedByKindContract}
	}
}

func estimateOut(e domain.EvalEstimate) gen.EvalEstimate {
	return gen.EvalEstimate{Usd: e.USD, Unpriced: nonNilStrings(e.Unpriced)}
}

// evalRunOut is run as the routes answer it, with its version's number and its results.
func (s *server) evalRunOut(ctx context.Context, run domain.EvalRun) (gen.EvalRun, error) {
	out := gen.EvalRun{
		Id: string(run.ID), State: gen.EvalRunState(run.State), Trigger: gen.EvalRunTrigger(run.Trigger), Case: optional(run.Case),
		ContentSha256: string(run.ContentSHA256), EstimateUsd: run.EstimateUSD, CostUsd: run.CostUSD, CreatedAt: run.CreatedAt,
		Results: make([]gen.EvalResult, 0, len(run.Results)),
	}

	if !run.FinishedAt.IsZero() {
		out.FinishedAt = &run.FinishedAt
	}

	n, err := (&versionNumbers{s: s}).number(ctx, run.VersionID)
	if err != nil {
		return gen.EvalRun{}, err
	}

	out.Version = n

	for _, r := range run.Results {
		res := gen.EvalResult{
			Case: r.Case, Try: r.Try, State: gen.EvalResultState(r.State), CheckedBy: optional(r.CheckedBy), RunId: optional(string(r.RunID)),
			CostUsd: r.CostUSD, Turns: r.Turns,
		}

		if len(r.Detail) > 0 {
			d := r.Detail
			res.Detail = &d
		}

		out.Results = append(out.Results, res)
	}

	return out, nil
}

// nonNilStrings is s, or an empty slice for nil, so the answer has [] and not null.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

// truncateRunes is s cut to at most n characters.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}

	return string([]rune(s)[:n])
}
