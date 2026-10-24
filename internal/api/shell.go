package api

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// The shell's reads (tag shell): what the app's chrome shows about an organization. Phase 2.2
// has no identity, so "waiting on you" is every decision waiting on a person; phase 4 narrows it
// at the Authorizer seam.

// Limits of the shell's reads.
const (
	defaultInboxLimit = 20
	maxInboxLimit     = 500
	// An overview slower than this, or over more factories, is logged: it reads 3 queries per
	// factory, fine at the expected organization size.
	slowOverview      = 500 * time.Millisecond
	overviewFactories = 25
)

// InboxSource adds entries to the organization's inbox (GET /v1/orgs/{org}/inbox). The built-in
// source is decisions; phase 2.2-E and phase 3 register theirs in Deps.InboxSources. The request's
// actor and organization are in ctx.
type InboxSource interface {
	Inbox(ctx context.Context, q InboxQuery) ([]gen.InboxEntry, error)
}

// InboxQuery is what one read of the inbox covers.
type InboxQuery struct {
	// Factories are the factories entries may belong to: the organization's, or the one the
	// request names.
	Factories []*config.Factory
	// Limit is how many entries the inbox shows. A source returns at most Limit+1 entries, its
	// most recent, so the inbox can tell when it is truncated.
	Limit int
}

// inboxKinds orders the inbox's entries by kind; a kind not listed comes last.
var inboxKinds = []gen.InboxEntryKind{gen.Decision}

func (s *server) GetOrgAttention(ctx context.Context, _ gen.GetOrgAttentionRequestObject) (gen.GetOrgAttentionResponseObject, error) {
	attention, err := s.attention(ctx)
	if err != nil {
		return nil, err
	}

	fs := s.factories.Factories(ctx)
	out := gen.GetOrgAttention200JSONResponse{Factories: make([]gen.FactoryAttention, 0, len(fs))}

	for _, f := range fs {
		a := attention[domain.FactoryID(f.Metadata.ID)]
		out.WaitingOnYou += a.WaitingHuman
		out.Factories = append(out.Factories, gen.FactoryAttention{
			FactoryId: f.Metadata.ID, WaitingOnYou: a.WaitingHuman, BlockedItems: a.Blocked, Andon: andonOf(a.Andon),
		})
	}

	return out, nil
}

// attention is the store's Attention by factory. A factory goald runs without a row there yet
// (nothing was ever submitted to it) has zeros.
func (s *server) attention(ctx context.Context) (map[domain.FactoryID]domain.Attention, error) {
	list, err := s.items.Attention(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[domain.FactoryID]domain.Attention, len(list))
	for _, a := range list {
		out[a.Factory] = a
	}

	return out, nil
}

func (s *server) GetFactoryOverview(ctx context.Context, req gen.GetFactoryOverviewRequestObject) (gen.GetFactoryOverviewResponseObject, error) {
	started := time.Now()
	now := s.clock.Now().UTC()

	w, err := windowOf(req.Params.Window, now)
	if err != nil {
		return nil, err
	}

	attention, err := s.attention(ctx)
	if err != nil {
		return nil, err
	}

	recs, err := s.records(ctx)
	if err != nil {
		return nil, err
	}

	fs := s.factories.Factories(ctx)
	out := gen.GetFactoryOverview200JSONResponse{
		Window: w.name(), Since: openapi_types.Date{Time: w.from}, Until: openapi_types.Date{Time: w.to},
		Factories: make([]gen.FactoryOverview, 0, len(fs)),
	}

	for _, f := range fs {
		o, err := s.overview(ctx, recordOf(recs, orgOf(ctx).ID, f), f, attention[domain.FactoryID(f.Metadata.ID)], w, now)
		if err != nil {
			return nil, err
		}

		out.WaitingOnYou += o.Waiting.OnYou
		out.SpendUsd += o.SpendUsd

		if o.BudgetUsd != nil {
			out.BudgetUsd = new(deref(out.BudgetUsd) + *o.BudgetUsd)
		}

		out.Factories = append(out.Factories, o)
	}

	if took := time.Since(started); took > slowOverview || len(fs) > overviewFactories {
		s.log.WarnContext(ctx, "slow factory overview", slog.String("org_id", string(orgOf(ctx).ID)),
			slog.Int("factories", len(fs)), slog.Duration("took", took))
	}

	return out, nil
}

// overview summarizes one factory over the window w as of now: its health, throughput against
// the previous window of the same length, the constraint now, the decisions waiting and the
// spend against its budget.
func (s *server) overview(
	ctx context.Context, rec ports.FactoryRecord, f *config.Factory, a domain.Attention, w dayRange, now time.Time,
) (gen.FactoryOverview, error) {
	l := f.Line()

	loads, err := s.loads(ctx, l.FactoryID)
	if err != nil {
		return gen.FactoryOverview{}, err
	}

	cur, err := s.items.FlowMetrics(ctx, w.query(l.FactoryID, "", now), "")
	if err != nil {
		return gen.FactoryOverview{}, err
	}

	prev, err := s.items.FlowMetrics(ctx, w.before().query(l.FactoryID, "", now), "")
	if err != nil {
		return gen.FactoryOverview{}, err
	}

	days := float64(w.days())
	o := gen.FactoryOverview{
		Factory: factoryOut(rec, f), Health: gen.FactoryHealthRunning, Andon: andonOf(a.Andon), SpendUsd: cur.SpendUSD,
		Throughput: gen.Throughput{
			PerDay: float64(cur.ItemsDone) / days, PreviousPerDay: float64(prev.ItemsDone) / days, Days: donePerDay(w, cur),
		},
		Waiting: gen.WaitingSummary{OnYou: a.WaitingHuman},
	}

	if a.Andon != nil {
		o.Health = gen.FactoryHealthStopped
	}

	if station, ok := l.Constraint(loads); ok {
		o.Constraint = &gen.ConstraintSummary{Station: station}
	}

	o.Waiting.ItemsByType = make([]struct {
		Count int    `json:"count"`
		Type  string `json:"type"`
	}, 0, len(a.WaitingByType))
	for _, tc := range a.WaitingByType {
		o.Waiting.ItemsByType = append(o.Waiting.ItemsByType, struct {
			Count int    `json:"count"`
			Type  string `json:"type"`
		}{Count: tc.Count, Type: tc.Type})
	}

	o.Waiting.ToolApprovals = []struct {
		Connection string `json:"connection"`
		Count      int    `json:"count"`
	}{}

	if l.Runtime.HasWeeklyBudget() {
		o.BudgetUsd = new(math.Round(l.Runtime.BudgetFor(time.Duration(w.days())*day)*100) / 100)
	}

	return o, nil
}

func (s *server) GetInbox(ctx context.Context, req gen.GetInboxRequestObject) (gen.GetInboxResponseObject, error) {
	limit := defaultInboxLimit
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}

	if limit < 1 || limit > maxInboxLimit {
		return nil, fmt.Errorf("%w: limit must be in [1, %d]", errInvalid, maxInboxLimit)
	}

	q := InboxQuery{Factories: s.factories.Factories(ctx), Limit: limit}

	if ref := deref(req.Params.Factory); ref != "" {
		f, err := s.factory(ctx, ref)
		if err != nil {
			return nil, err
		}

		q.Factories = []*config.Factory{f}
	}

	entries := []gen.InboxEntry{}

	for _, src := range s.inbox {
		got, err := src.Inbox(ctx, q)
		if err != nil {
			return nil, err
		}

		entries = append(entries, got...)
	}

	// Keep the most recent entries, then order them by kind, oldest first.
	slices.SortFunc(entries, func(a, b gen.InboxEntry) int { return cmp.Or(b.Since.Compare(a.Since), cmp.Compare(a.Id, b.Id)) })

	out := gen.GetInbox200JSONResponse{Entries: entries[:min(limit, len(entries))], Truncated: len(entries) > limit}

	slices.SortFunc(out.Entries, func(a, b gen.InboxEntry) int {
		return cmp.Or(cmp.Compare(kindRank(a.Kind), kindRank(b.Kind)), a.Since.Compare(b.Since), cmp.Compare(a.Id, b.Id))
	})

	return out, nil
}

func kindRank(k gen.InboxEntryKind) int {
	if i := slices.Index(inboxKinds, k); i >= 0 {
		return i
	}

	return len(inboxKinds)
}

// decisions is the inbox's built-in source: every item waiting for a human decision, which in
// phase 2.2 is waiting on you. Interim: phase 2.2-E replaces it with its item source.
type decisions struct {
	s *server
}

// Inbox implements InboxSource.
func (d decisions) Inbox(ctx context.Context, q InboxQuery) ([]gen.InboxEntry, error) {
	byID := make(map[domain.FactoryID]*config.Factory, len(q.Factories))
	ids := make([]domain.FactoryID, 0, len(q.Factories))

	for _, f := range q.Factories {
		id := domain.FactoryID(f.Metadata.ID)
		byID[id] = f
		ids = append(ids, id)
	}

	items, err := d.s.items.WaitingHuman(ctx, ids, q.Limit+1)
	if err != nil {
		return nil, err
	}

	out := make([]gen.InboxEntry, 0, len(items))

	for _, it := range items {
		f := byID[it.FactoryID]
		l := f.Line()

		cards, err := d.s.cards(ctx, f, l, "", []domain.Item{it}) // decisions wait on a person, never in a buffer
		if err != nil {
			return nil, err
		}

		e := gen.InboxEntry{
			Id: string(gen.Decision) + ":" + string(it.ID), Kind: gen.Decision, Since: it.UpdatedAt,
			Item: &cards[0], Station: optional(it.Station),
		}
		e.Factory.Id, e.Factory.Name, e.Factory.Title = f.Metadata.ID, f.Metadata.Name, optional(f.Metadata.Title)

		if st, ok := l.Station(it.Station); ok {
			e.Action = actionOf(string(st.Action))
		}

		out = append(out, e)
	}

	return out, nil
}

func andonOf(p *domain.AndonPull) *gen.AndonPull {
	if p == nil {
		return nil
	}

	return &gen.AndonPull{Reason: p.Reason, PulledBy: p.PulledBy, PulledAt: p.PulledAt}
}
