package api

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/google/uuid"
)

// Versions reads definition versions, for the number a run ran on; ports.DefinitionStore
// satisfies it.
type Versions interface {
	Version(ctx context.Context, id domain.DefinitionVersionID) (domain.DefinitionVersion, error)
}

// maxItemRuns caps an item's run list; an item with more says it is truncated.
const maxItemRuns = 200

func (s *server) ListItemRuns(ctx context.Context, req gen.ListItemRunsRequestObject) (gen.ListItemRunsResponseObject, error) {
	it, err := s.item(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	runs, err := s.items.Runs(ctx, it.ID, maxItemRuns+1)
	if err != nil {
		return nil, err
	}

	out := gen.ListItemRuns200JSONResponse{Runs: make([]gen.Run, 0, min(len(runs), maxItemRuns)), Truncated: len(runs) > maxItemRuns}
	numbers := versionNumbers{s: s}

	for _, r := range runs[:min(len(runs), maxItemRuns)] {
		run, err := numbers.runOf(ctx, r)
		if err != nil {
			return nil, err
		}

		out.Runs = append(out.Runs, run)
	}

	return out, nil
}

func (s *server) GetRun(ctx context.Context, req gen.GetRunRequestObject) (gen.GetRunResponseObject, error) {
	r, err := s.run(ctx, req.Run)
	if err != nil {
		return nil, err
	}

	run, err := (&versionNumbers{s: s}).runOf(ctx, r)
	if err != nil {
		return nil, err
	}

	return gen.GetRun200JSONResponse(run), nil
}

// Limits of the organization's run list.
const (
	defaultOrgRunsLimit = 50
	maxOrgRunsLimit     = 200
	maxOrgRunsQuery     = 200
)

func (s *server) ListRuns(ctx context.Context, req gen.ListRunsRequestObject) (gen.ListRunsResponseObject, error) {
	q, err := s.runQuery(req.Params)
	if err != nil {
		return nil, err
	}

	byID, err := s.runFactories(ctx, &q, deref(req.Params.Factory))
	if err != nil {
		return nil, err
	}

	limit := q.Limit
	q.Limit++ // one more tells whether a next page exists

	runs, sum, err := s.items.OrgRuns(ctx, q)
	if err != nil {
		return nil, err
	}

	out := gen.ListRuns200JSONResponse{Runs: make([]gen.OrgRun, 0, min(len(runs), limit)), Summary: runSummaryOf(sum, byID)}
	numbers := versionNumbers{s: s}

	for _, r := range runs[:min(len(runs), limit)] {
		run, err := numbers.runOf(ctx, r.Run)
		if err != nil {
			return nil, err
		}

		f := byID[r.FactoryID]
		out.Runs = append(out.Runs, gen.OrgRun{
			Id: run.Id, ItemId: run.ItemId, Station: run.Station, Agent: run.Agent, Model: run.Model, Status: run.Status,
			Turns: run.Turns, InputTokens: run.InputTokens, OutputTokens: run.OutputTokens, CostUsd: run.CostUsd,
			Error: run.Error, StartedAt: run.StartedAt, EndedAt: run.EndedAt,
			DefinitionVersionId: run.DefinitionVersionId, DefinitionVersion: run.DefinitionVersion,
			Factory: factoryHit(f),
			Item: gen.RunItem{
				Id: string(r.ItemID), Ref: domain.ItemRef{Factory: f.Metadata.Name, Number: r.ItemNumber}.String(), Title: r.ItemTitle,
			},
		})
	}

	if len(runs) > limit {
		last := runs[limit-1]
		out.NextCursor = new(encodeRunCursor(ports.RunCursor{StartedAt: last.StartedAt, ID: last.ID}))
	}

	return out, nil
}

// runFactories sets q's factories, the one ref names (404 when the caller does not see it) or
// every factory the caller sees, and the item number q's text names among them; it returns them
// by id.
func (s *server) runFactories(ctx context.Context, q *ports.RunQuery, ref string) (map[domain.FactoryID]*config.Factory, error) {
	var fs []*config.Factory

	if ref != "" {
		f, err := s.factory(ctx, ref)
		if err != nil {
			return nil, err
		}

		fs = []*config.Factory{f}
	} else {
		fs = s.visibleFactories(ctx)
	}

	byID := make(map[domain.FactoryID]*config.Factory, len(fs))
	for _, f := range fs {
		id := domain.FactoryID(f.Metadata.ID)
		byID[id], q.Factories = f, append(q.Factories, id)
	}

	q.Number, q.NumberOf = refNumber(q.Text, fs)

	return byID, nil
}

// runsHeader is the runs export's header row: the list's JSON fields, the factory and the item
// flattened.
var runsHeader = strings.Fields("id factory item_ref item_title station agent model status started_at ended_at turns cost_usd")

func (s *server) ExportRuns(ctx context.Context, req gen.ExportRunsRequestObject) (gen.ExportRunsResponseObject, error) {
	p := req.Params

	q, err := s.runQuery(gen.ListRunsParams{Factory: p.Factory, Status: p.Status, Since: p.Since, Until: p.Until, Q: p.Q})
	if err != nil {
		return nil, err
	}

	byID, err := s.runFactories(ctx, &q, deref(p.Factory))
	if err != nil {
		return nil, err
	}

	rows := func(ctx context.Context, maxRows int, emit func([]string) error) error {
		return s.items.EachOrgRun(ctx, q, maxRows, func(r domain.ListedRun) error {
			name := byID[r.FactoryID].Metadata.Name

			ended := ""
			if !r.EndedAt.IsZero() {
				ended = r.EndedAt.UTC().Format(time.RFC3339)
			}

			return emit([]string{
				string(r.ID), name, domain.ItemRef{Factory: name, Number: r.ItemNumber}.String(), r.ItemTitle, r.Station, r.Agent, r.Model,
				string(r.Result.Status), r.StartedAt.UTC().Format(time.RFC3339), ended, strconv.Itoa(r.Result.Turns), usd(r.Result.CostUSD),
			})
		})
	}

	return csvExport{ctx: ctx, log: s.log, what: "runs", day: s.clock.Now(), header: runsHeader, max: s.exportMax, rows: rows}, nil
}

// runQuery reads the list's filters, its window (the clock's UTC day by default), its limit
// and its cursor; anything out of range is errInvalid.
func (s *server) runQuery(p gen.ListRunsParams) (ports.RunQuery, error) {
	q := ports.RunQuery{Limit: defaultOrgRunsLimit}
	if p.Limit != nil {
		q.Limit = *p.Limit
	}

	if q.Limit < 1 || q.Limit > maxOrgRunsLimit {
		return q, fmt.Errorf("%w: limit must be in [1, %d]", errInvalid, maxOrgRunsLimit)
	}

	if p.Status != nil {
		q.Status = domain.RunStatus(*p.Status)
		if !slices.Contains([]domain.RunStatus{domain.RunRunning, domain.RunSucceeded, domain.RunFailed, domain.RunCancelled}, q.Status) {
			return q, fmt.Errorf("%w: unknown status %q", errInvalid, q.Status)
		}
	}

	day := s.clock.Now().UTC().Truncate(24 * time.Hour)
	q.Since, q.Until = day, day.Add(24*time.Hour)

	if p.Since != nil {
		q.Since = *p.Since
	}

	if p.Until != nil {
		q.Until = *p.Until
	}

	if !q.Since.Before(q.Until) {
		return q, fmt.Errorf("%w: since must be before until", errInvalid)
	}

	q.Text = strings.TrimSpace(deref(p.Q))
	if utf8.RuneCountInString(q.Text) > maxOrgRunsQuery {
		return q, fmt.Errorf("%w: q must have at most %d characters", errInvalid, maxOrgRunsQuery)
	}

	if c := deref(p.Cursor); c != "" {
		after, err := decodeRunCursor(c)
		if err != nil {
			return q, err
		}

		q.After = &after
	}

	return q, nil
}

// refNumber is the item number q names as a ref ("147", "#147" or "goal#147") and the factory
// a ref's name or id prefix names among fs; 0 when q is no ref, or names a factory not in fs.
func refNumber(q string, fs []*config.Factory) (int64, domain.FactoryID) {
	prefix, digits, ok := strings.Cut(q, "#")
	if !ok {
		digits = prefix
	}

	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n < 1 {
		return 0, ""
	}

	if prefix == "" || !ok {
		return n, ""
	}

	for _, f := range fs {
		if f.Metadata.Name == prefix || f.Metadata.ID == prefix {
			return n, domain.FactoryID(f.Metadata.ID)
		}
	}

	return 0, ""
}

// runSummaryOf is the list's summary, the factories that spent most first, then by name.
func runSummaryOf(sum domain.RunSummary, byID map[domain.FactoryID]*config.Factory) gen.RunSummary {
	out := gen.RunSummary{
		Total: sum.Total, Running: sum.Running, Failed: sum.Failed, Finished: sum.Finished, SpendUsd: sum.SpendUSD,
		SpendByFactory: make([]gen.FactorySpend, 0, len(sum.ByFactory)),
	}

	for _, f := range sum.ByFactory {
		if def, ok := byID[f.FactoryID]; ok {
			out.SpendByFactory = append(out.SpendByFactory, gen.FactorySpend{Id: def.Metadata.ID, Name: def.Metadata.Name, SpendUsd: f.USD})
		}
	}

	slices.SortFunc(out.SpendByFactory, func(a, b gen.FactorySpend) int {
		return cmp.Or(cmp.Compare(b.SpendUsd, a.SpendUsd), cmp.Compare(a.Name, b.Name))
	})

	return out
}

// factoryHit names a factory as a search hit or a run's row does.
func factoryHit(f *config.Factory) gen.FactoryHit {
	return gen.FactoryHit{Id: f.Metadata.ID, Name: f.Metadata.Name, Title: cmp.Or(f.Metadata.Title, f.Metadata.Name)}
}

// encodeRunCursor writes a run's place in the list as an opaque cursor.
func encodeRunCursor(c ports.RunCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.StartedAt.UTC().Format(time.RFC3339Nano) + " " + string(c.ID)))
}

// decodeRunCursor reads a cursor encodeRunCursor wrote; anything else is errInvalid.
func decodeRunCursor(s string) (ports.RunCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return ports.RunCursor{}, fmt.Errorf("%w: cursor is not one a run list returned", errInvalid)
	}

	at, id, ok := strings.Cut(string(raw), " ")

	startedAt, err := time.Parse(time.RFC3339Nano, at)
	if _, bad := uuid.Parse(id); !ok || err != nil || bad != nil {
		return ports.RunCursor{}, fmt.Errorf("%w: cursor is not one a run list returned", errInvalid)
	}

	return ports.RunCursor{StartedAt: startedAt, ID: domain.RunID(id)}, nil
}

// versionNumbers looks up the numbers of the versions runs ran on, each version once.
type versionNumbers struct {
	s     *server
	known map[domain.DefinitionVersionID]*int
}

func (v *versionNumbers) runOf(ctx context.Context, r domain.Run) (gen.Run, error) {
	number, err := v.number(ctx, r.DefinitionVersionID)
	if err != nil {
		return gen.Run{}, err
	}

	run := gen.Run{
		Id: string(r.ID), ItemId: string(r.ItemID), Station: r.Station, Agent: r.Agent, Model: r.Model,
		Status: gen.RunStatus(r.Result.Status), Turns: r.Result.Turns, InputTokens: r.Result.InputTokens,
		OutputTokens: r.Result.OutputTokens, CostUsd: r.Result.CostUSD, Error: optional(r.Result.Error), StartedAt: r.StartedAt,
		DefinitionVersionId: optional(string(r.DefinitionVersionID)), DefinitionVersion: number,
	}

	if !r.EndedAt.IsZero() {
		run.EndedAt = &r.EndedAt
	}

	return run, nil
}

// number is the version's number, nil for a run without a version or one whose version is gone.
func (v *versionNumbers) number(ctx context.Context, id domain.DefinitionVersionID) (*int, error) {
	if id == "" {
		return nil, nil
	}

	if n, ok := v.known[id]; ok {
		return n, nil
	}

	var n *int

	version, err := v.s.versions.Version(ctx, id)

	switch {
	case errors.Is(err, ports.ErrNotFound):
	case err != nil:
		return nil, fmt.Errorf("version %s of a run: %w", id, err)
	default:
		n = &version.Number
	}

	if v.known == nil {
		v.known = map[domain.DefinitionVersionID]*int{}
	}

	v.known[id] = n

	return n, nil
}
