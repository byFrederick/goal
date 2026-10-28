package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"go.yaml.in/yaml/v3"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The judges routes (ADR 0025): a factory's judges are its current version's evals/judges/<slug>.md
// files, read through ports.Judges, with their production scores of the last 30 days. Adding or
// changing one writes its file into a draft the handler opens and publishes it as the next
// version, through the same checks as any publish (decision 5); trying one queues judge runs.

// JudgeDeps serve the judges routes. Judges reads a version's judges, Scores their production
// scores (a ports.JudgeScores satisfies it) and Tries queues tries (*jonah.Loop satisfies it).
type JudgeDeps struct {
	Judges ports.Judges
	Scores JudgeScoreReads
	Tries  JudgeTries
}

// JudgeScoreReads are the reads of ports.JudgeScores the judges routes make.
type JudgeScoreReads interface {
	Recent(ctx context.Context, factory domain.FactoryID, judge string, limit int) ([]domain.JudgeScore, error)
	Summary(ctx context.Context, factory domain.FactoryID, now time.Time) ([]domain.JudgeSummary, error)
}

// JudgeTries queues a judge's tries on the n newest finished runs of its agents; *jonah.Loop
// satisfies it.
type JudgeTries interface {
	Try(ctx context.Context, factory domain.FactoryID, version domain.DefinitionVersionID, slug string, n int) ([]domain.JudgeScore, error)
}

const (
	// judgeTries is how many of the newest finished runs a try scores.
	judgeTries = 10
	// recentJudgeScores is how many of a judge's newest scores getJudge lists.
	recentJudgeScores = 20
)

// judgeSlug is the form of a judge's slug, its file name: evals.schema.json's slug.
var judgeSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

// judgeSees is what a judge run reads of the run it scores.
var judgeSees = []gen.JudgeRunsOnSees{gen.Events, gen.Spec, gen.PullRequest}

// Errors of the judges routes.
var (
	// errJudgesUnavailable is a goald that does not serve judges (503): Deps.Judges is nil.
	errJudgesUnavailable = errors.New("this goald does not serve judges yet")
	errJudgeNotFound     = errors.New("judge not found")
	errJudgeExists       = errors.New("the current version already has a judge of this slug")
	// errDraftOpen refuses a judge write while the factory has an open draft, which it would
	// otherwise publish along with the judge.
	errDraftOpen = errors.New("the factory has an open draft: publish or discard it first")
)

// judgeProblem answers the judges routes' own errors.
func judgeProblem(err error) (gen.Problem, bool) {
	var status int

	var code string

	switch {
	case errors.Is(err, errJudgesUnavailable):
		status, code = http.StatusServiceUnavailable, "judges-unavailable"
	case errors.Is(err, errJudgeNotFound):
		status, code = http.StatusNotFound, "judge-not-found"
	case errors.Is(err, errJudgeExists):
		status, code = http.StatusConflict, "judge-exists"
	case errors.Is(err, errDraftOpen):
		status, code = http.StatusConflict, "draft-open"
	default:
		return gen.Problem{}, false
	}

	p := detailed(status, err)
	p.Code = &code

	return p, true
}

func (s *server) ListJudges(ctx context.Context, req gen.ListJudgesRequestObject) (gen.ListJudgesResponseObject, error) {
	rec, err := s.judgeFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	out := gen.ListJudges200JSONResponse{Judges: []gen.JudgeListEntry{}}
	if rec.Current == nil {
		return out, nil
	}

	judges, err := s.judges.Judges.Judges(ctx, rec.ID, rec.Current.ID)
	if err != nil {
		return nil, fmt.Errorf("list judges of version %d: %w", rec.Current.Number, err)
	}

	summaries, err := s.judgeSummaries(ctx, rec.ID)
	if err != nil {
		return nil, err
	}

	out.Version = &rec.Current.Number

	for _, sum := range summaries {
		out.JudgeCostUsd += sum.CostUSD
	}

	slices.SortFunc(judges, func(a, b domain.Judge) int { return strings.Compare(a.Slug, b.Slug) })

	for _, j := range judges {
		out.Judges = append(out.Judges, gen.JudgeListEntry{
			Slug: j.Slug, Title: j.Title, Criterion: j.Criterion, Agents: nonNil(j.Agents), Sample: j.Sample,
			PassMark: j.PassMark, Enabled: j.Enabled, Summary: judgeSummaryOut(summaries[j.Slug]),
		})
	}

	return out, nil
}

func (s *server) GetJudge(ctx context.Context, req gen.GetJudgeRequestObject) (gen.GetJudgeResponseObject, error) {
	rec, err := s.judgeFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	j, err := s.currentJudge(ctx, rec, req.Judge)
	if err != nil {
		return nil, err
	}

	out, err := s.judgeOut(ctx, rec, j, rec.Current.Number)
	if err != nil {
		return nil, err
	}

	return gen.GetJudge200JSONResponse(out), nil
}

func (s *server) CreateJudge(ctx context.Context, req gen.CreateJudgeRequestObject) (gen.CreateJudgeResponseObject, error) {
	rec, err := s.judgeFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	b := req.Body
	if !judgeSlug.MatchString(b.Slug) {
		return nil, fmt.Errorf("%w: slug %q is not 3 to 40 lowercase letters, digits and hyphens starting with a letter", errInvalid, b.Slug)
	}

	if _, err := s.currentJudge(ctx, rec, b.Slug); err == nil {
		return nil, fmt.Errorf("judge %s: %w", b.Slug, errJudgeExists)
	} else if !errors.Is(err, errJudgeNotFound) {
		return nil, err
	}

	j := judgeOf(b.Slug, gen.JudgeWrite{
		Title: b.Title, Criterion: b.Criterion, Agents: b.Agents, Outcomes: b.Outcomes, PassMark: b.PassMark,
		Sample: b.Sample, Model: b.Model, Enabled: b.Enabled, Rubric: b.Rubric,
	})

	out, err := s.publishJudge(ctx, rec, j, deref(b.ExpectedCurrentVersionId), true)
	if err != nil {
		return nil, err
	}

	return gen.CreateJudge201JSONResponse(out), nil
}

func (s *server) UpdateJudge(ctx context.Context, req gen.UpdateJudgeRequestObject) (gen.UpdateJudgeResponseObject, error) {
	rec, err := s.judgeFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if _, err := s.currentJudge(ctx, rec, req.Judge); err != nil {
		return nil, err
	}

	out, err := s.publishJudge(ctx, rec, judgeOf(req.Judge, *req.Body), deref(req.Body.ExpectedCurrentVersionId), false)
	if err != nil {
		return nil, err
	}

	return gen.UpdateJudge200JSONResponse(out), nil
}

func (s *server) TryJudge(ctx context.Context, req gen.TryJudgeRequestObject) (gen.TryJudgeResponseObject, error) {
	rec, err := s.judgeFactory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	if _, err := s.currentJudge(ctx, rec, req.Judge); err != nil {
		return nil, err
	}

	tries, err := s.judges.Tries.Try(ctx, rec.ID, rec.Current.ID, req.Judge, judgeTries)
	if err != nil {
		return nil, err
	}

	out := gen.TryJudge202JSONResponse{Tries: make([]gen.JudgeTry, 0, len(tries))}

	for _, t := range tries {
		it, err := s.items.Item(ctx, t.ItemID)
		if err != nil {
			return nil, fmt.Errorf("read item %s of try %s: %w", t.ItemID, t.ID, err)
		}

		out.Tries = append(out.Tries, gen.JudgeTry{
			Id: string(t.ID), State: gen.JudgeScoreState(t.State), ItemRef: s.ref(ctx, rec.ID, it.Number), ItemTitle: it.Title,
		})
	}

	return out, nil
}

// judgeFactory resolves the path's factory, then refuses with 503 when this goald does not serve
// judges: the factory is resolved and authorized first, so the answer tells nothing of a factory
// the caller may not see.
func (s *server) judgeFactory(ctx context.Context, ref string) (ports.FactoryRecord, error) {
	rec, err := s.definition(ctx, ref)
	if err != nil {
		return ports.FactoryRecord{}, err
	}

	if s.judges == nil {
		return ports.FactoryRecord{}, errJudgesUnavailable
	}

	return rec, nil
}

// currentJudge is the judge slug of rec's current version: errJudgeNotFound when it has none or
// there is no current version.
func (s *server) currentJudge(ctx context.Context, rec ports.FactoryRecord, slug string) (domain.Judge, error) {
	if rec.Current == nil || !judgeSlug.MatchString(slug) {
		return domain.Judge{}, fmt.Errorf("judge %q: %w", slug, errJudgeNotFound)
	}

	judges, err := s.judges.Judges.Judges(ctx, rec.ID, rec.Current.ID)
	if err != nil {
		return domain.Judge{}, fmt.Errorf("read judges of version %d: %w", rec.Current.Number, err)
	}

	i := slices.IndexFunc(judges, func(j domain.Judge) bool { return j.Slug == slug })
	if i < 0 {
		return domain.Judge{}, fmt.Errorf("judge %q: %w", slug, errJudgeNotFound)
	}

	return judges[i], nil
}

// judgeSummaries are the factory's judges' last 30 days, by slug.
func (s *server) judgeSummaries(ctx context.Context, factory domain.FactoryID) (map[string]domain.JudgeSummary, error) {
	sums, err := s.judges.Scores.Summary(ctx, factory, s.clock.Now())
	if err != nil {
		return nil, fmt.Errorf("read judge summaries: %w", err)
	}

	out := make(map[string]domain.JudgeSummary, len(sums))
	for _, sum := range sums {
		out[sum.Judge] = sum
	}

	return out, nil
}

// publishJudge writes j's file into a draft it opens from rec's current version and publishes
// it, refusing when another draft is open. A draft it opened that does not publish is discarded,
// unless the eval gate refused it: that draft stays open, so it publishes once a run of its cases
// passes.
func (s *server) publishJudge(ctx context.Context, rec ports.FactoryRecord, j domain.Judge, expected string, create bool) (gen.Judge, error) {
	if rec.Draft != nil {
		return gen.Judge{}, fmt.Errorf("judge %s: %w", j.Slug, errDraftOpen)
	}

	content, err := judgeFile(j)
	if err != nil {
		return gen.Judge{}, err
	}

	d, created, err := s.defs.OpenDraft(ctx, rec.ID, authoring.OpenDraftRequest{})
	if err != nil {
		return gen.Judge{}, err
	}

	if !created { // another writer opened one since rec was read
		return gen.Judge{}, fmt.Errorf("judge %s: %w", j.Slug, errDraftOpen)
	}

	revision := d.Draft.Revision

	v, err := func() (domain.DefinitionVersion, error) {
		saved, err := s.defs.PutFile(ctx, rec.ID, authoring.FileWrite{
			Path: config.JudgesDir + "/" + j.Slug + ".md", Content: content, Create: create,
		})
		if err != nil {
			return domain.DefinitionVersion{}, err
		}

		revision = saved.Revision

		if expected == "" && rec.Current != nil {
			expected = string(rec.Current.ID)
		}

		verb := "Change"
		if create {
			verb = "Add"
		}

		return s.defs.Publish(ctx, rec.ID, authoring.PublishRequest{
			Revision: revision, ExpectedCurrent: domain.DefinitionVersionID(expected), Message: verb + " judge " + j.Slug,
		})
	}()

	_, gated := errors.AsType[*authoring.EvalGateError](err)
	if err != nil && !gated {
		if derr := s.defs.DiscardDraft(ctx, rec.ID, revision); derr != nil {
			s.log.WarnContext(ctx, "judge draft not discarded", slog.String("factory", string(rec.ID)),
				slog.String("judge", j.Slug), slog.Any("error", derr))
		}
	}

	if err != nil {
		return gen.Judge{}, err
	}

	return s.judgeOut(ctx, rec, j, v.Number)
}

// judgeOf is the judge slug a write describes; enabled unless it says otherwise.
func judgeOf(slug string, w gen.JudgeWrite) domain.Judge {
	j := domain.Judge{
		Slug: slug, Title: w.Title, Criterion: w.Criterion, Agents: deref(w.Agents), PassMark: w.PassMark, Sample: w.Sample,
		Model: deref(w.Model), Enabled: w.Enabled == nil || *w.Enabled, Rubric: w.Rubric,
	}

	for _, o := range w.Outcomes {
		j.Outcomes = append(j.Outcomes, domain.JudgeOutcome{Score: o.Score, Label: o.Label, Meaning: o.Meaning})
	}

	return j
}

// judgeFront is a judge file's front matter, with evals.schema.json's names in its order.
type judgeFront struct {
	Title     string              `yaml:"title"`
	Criterion string              `yaml:"criterion"`
	Agents    []string            `yaml:"agents,omitempty"`
	Outcomes  []judgeFrontOutcome `yaml:"outcomes"`
	PassMark  float64             `yaml:"passMark"`
	Sample    float64             `yaml:"sample"`
	Model     string              `yaml:"model,omitempty"`
	Enabled   bool                `yaml:"enabled"`
}

type judgeFrontOutcome struct {
	Score   float64 `yaml:"score"`
	Label   string  `yaml:"label"`
	Meaning string  `yaml:"meaning"`
}

// judgeFile is j's evals/judges/<slug>.md: the YAML front matter between --- lines, then the
// rubric.
func judgeFile(j domain.Judge) ([]byte, error) {
	front := judgeFront{
		Title: j.Title, Criterion: j.Criterion, Agents: j.Agents, PassMark: j.PassMark, Sample: j.Sample,
		Model: j.Model, Enabled: j.Enabled, Outcomes: make([]judgeFrontOutcome, 0, len(j.Outcomes)),
	}

	for _, o := range j.Outcomes {
		front.Outcomes = append(front.Outcomes, judgeFrontOutcome(o))
	}

	y, err := yaml.Marshal(front)
	if err != nil {
		return nil, fmt.Errorf("render judge %s: %w", j.Slug, err)
	}

	rubric := strings.TrimLeft(j.Rubric, "\n")
	if rubric != "" && !strings.HasSuffix(rubric, "\n") {
		rubric += "\n"
	}

	return []byte("---\n" + string(y) + "---\n\n" + rubric), nil
}

// judgeOut is j of version number as getJudge answers it, with its last 30 days, its recent
// scores and where it runs.
func (s *server) judgeOut(ctx context.Context, rec ports.FactoryRecord, j domain.Judge, version int) (gen.Judge, error) {
	summaries, err := s.judgeSummaries(ctx, rec.ID)
	if err != nil {
		return gen.Judge{}, err
	}

	recent, err := s.judges.Scores.Recent(ctx, rec.ID, j.Slug, recentJudgeScores)
	if err != nil {
		return gen.Judge{}, fmt.Errorf("read recent scores of judge %s: %w", j.Slug, err)
	}

	out := gen.Judge{
		Slug: j.Slug, Title: j.Title, Criterion: j.Criterion, Agents: nonNil(j.Agents), Outcomes: make([]gen.JudgeOutcome, 0, len(j.Outcomes)),
		PassMark: j.PassMark, Sample: j.Sample, Model: optional(j.Model), Enabled: j.Enabled, Rubric: j.Rubric, Version: version,
		Summary: judgeSummaryOut(summaries[j.Slug]), Recent: make([]gen.JudgeScore, 0, len(recent)),
	}

	for _, o := range j.Outcomes {
		out.Outcomes = append(out.Outcomes, gen.JudgeOutcome{Score: o.Score, Label: o.Label, Meaning: o.Meaning})
	}

	for _, sc := range recent {
		out.Recent = append(out.Recent, judgeScoreOut(domain.ItemRef{Factory: rec.Name, Number: sc.ItemNumber}.String(), sc))
	}

	if out.RunsOn, err = s.judgeRunsOn(ctx, rec.ID); err != nil {
		return gen.Judge{}, err
	}

	return out, nil
}

// judgeRunsOn is what a judge inherits of the factory's current version: its box size (medium
// when it names none) and its connections.
func (s *server) judgeRunsOn(ctx context.Context, factory domain.FactoryID) (gen.JudgeRunsOn, error) {
	out := gen.JudgeRunsOn{Size: string(domain.SandboxMedium), Connections: []string{}, Sees: judgeSees, Inherited: true}

	f, err := s.factories.Lookup(ctx, string(factory))
	if errors.Is(err, ports.ErrNotFound) {
		return out, nil
	}

	if err != nil {
		return gen.JudgeRunsOn{}, fmt.Errorf("read factory %s: %w", factory, err)
	}

	if size := f.Spec.Runtime.Size; size != "" {
		out.Size = size
	}

	out.Connections = f.ConnectionNames()

	return out, nil
}

func judgeScoreOut(ref string, sc domain.JudgeScore) gen.JudgeScore {
	out := gen.JudgeScore{
		Id: string(sc.ID), State: gen.JudgeScoreState(sc.State), ItemRef: ref, ItemTitle: sc.ItemTitle, Reason: optional(sc.Reason),
	}

	if sc.State == domain.JudgeScoreScored {
		out.Outcome, out.Score, out.Passed = optional(sc.Outcome), &sc.Score, &sc.Passed
	}

	if !sc.ScoredAt.IsZero() {
		out.ScoredAt = &sc.ScoredAt
	}

	return out
}

func judgeSummaryOut(sum domain.JudgeSummary) gen.JudgeSummary {
	out := gen.JudgeSummary{
		Scored: sum.Scored, Passed: sum.Passed, PassRate: sum.PassRate, CostUsd: sum.CostUSD, Days: make([]gen.JudgeDay, 0, len(sum.Days)),
	}

	for _, d := range sum.Days {
		out.Days = append(out.Days, gen.JudgeDay{Day: openapi_types.Date{Time: d.Day}, Scored: d.Scored, Passed: d.Passed, PassRate: d.PassRate})
	}

	return out
}
