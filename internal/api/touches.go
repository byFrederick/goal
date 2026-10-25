package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// Human touches per item (spec 4 §4.9, §4.10), tag work.

// errTouchNotFound is an event that is not one of the item's touches (404 touch-not-found).
var errTouchNotFound = fmt.Errorf("touch not found: %w", ports.ErrNotFound)

func (s *server) GetTouchMetrics(ctx context.Context, req gen.GetTouchMetricsRequestObject) (gen.GetTouchMetricsResponseObject, error) {
	p := req.Params
	now := s.clock.Now().UTC()

	r, err := rangeOf(nil, p.From, p.To, now)
	if err != nil {
		return nil, err
	}

	weeks := defaultWeeks
	if p.Weeks != nil {
		weeks = *p.Weeks
	}

	if weeks < 1 || weeks > maxWeeks {
		return nil, fmt.Errorf("%w: weeks must be in [1, %d]", errInvalid, maxWeeks)
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	typ, err := workTypeOf(f, p.Type)
	if err != nil {
		return nil, err
	}

	q := r.query(domain.FactoryID(f.Metadata.ID), typ, now)
	out := gen.GetTouchMetrics200JSONResponse{From: openapi_types.Date{Time: r.from}, To: openapi_types.Date{Time: r.to}, Type: optional(typ)}

	if p.Version != nil {
		v, err := s.touchVersion(ctx, q.FactoryID, *p.Version)
		if err != nil {
			return nil, err
		}

		q.VersionID, out.Version = v.ID, &v.Number
	}

	st, err := s.items.Touches(ctx, q)
	if err != nil {
		return nil, err
	}

	if out.Stats, err = s.touchStatsOf(ctx, st); err != nil {
		return nil, err
	}

	weekly := q
	weekly.Until = r.to.Add(day)

	buckets, err := s.items.TouchesByBucket(ctx, weekly, week, weeks)
	if err != nil {
		return nil, err
	}

	first := weekly.Until.Add(-time.Duration(weeks) * week)
	for i, b := range buckets {
		from := first.Add(time.Duration(i) * week)
		out.Weekly = append(out.Weekly, gen.TouchWeek{
			From: openapi_types.Date{Time: from}, To: openapi_types.Date{Time: from.Add(week - day)}, Finished: b.Finished,
			PerItemMean: b.PerItem(b.Touches), TouchlessRate: b.TouchlessRate(),
		})
	}

	return out, nil
}

// touchVersion resolves the version parameter of the touch metrics: a published version's
// number, or current.
func (s *server) touchVersion(ctx context.Context, f domain.FactoryID, param string) (domain.DefinitionVersion, error) {
	sel, err := authoring.ParseVersion(param)
	if err != nil || (sel.Kind != authoring.SelectNumber && sel.Kind != authoring.SelectCurrent) {
		return domain.DefinitionVersion{}, fmt.Errorf("%w: version %q is not a number or current", errInvalid, param)
	}

	v, err := s.defs.Version(ctx, f, sel)
	if err != nil {
		return domain.DefinitionVersion{}, err
	}

	return v.DefinitionVersion, nil
}

func (s *server) GetOrgTouchMetrics(ctx context.Context, req gen.GetOrgTouchMetricsRequestObject) (gen.GetOrgTouchMetricsResponseObject, error) {
	now := s.clock.Now().UTC()

	r, err := rangeOf(nil, req.Params.From, req.Params.To, now)
	if err != nil {
		return nil, err
	}

	fs := s.visibleFactories(ctx)
	slices.SortFunc(fs, func(a, b *config.Factory) int { return cmp.Compare(a.Metadata.Name, b.Metadata.Name) })

	ids := make([]domain.FactoryID, 0, len(fs))
	for _, f := range fs {
		ids = append(ids, domain.FactoryID(f.Metadata.ID))
	}

	total, each, err := s.items.OrgTouches(ctx, ids, r.query("", "", now))
	if err != nil {
		return nil, err
	}

	out := gen.GetOrgTouchMetrics200JSONResponse{
		From: openapi_types.Date{Time: r.from}, To: openapi_types.Date{Time: r.to}, Factories: make([]gen.FactoryTouchMetrics, 0, len(fs)),
	}

	if out.Total, err = s.touchStatsOf(ctx, total); err != nil {
		return nil, err
	}

	for i, f := range fs {
		st, err := s.touchStatsOf(ctx, each[i].TouchStats)
		if err != nil {
			return nil, err
		}

		out.Factories = append(out.Factories, gen.FactoryTouchMetrics{
			FactoryId: f.Metadata.ID, Name: f.Metadata.Name, Title: optional(f.Metadata.Title), Stats: st,
		})
	}

	return out, nil
}

func (s *server) SetTouchReason(ctx context.Context, req gen.SetTouchReasonRequestObject) (gen.SetTouchReasonResponseObject, error) {
	reason := domain.TouchReason(req.Body.Reason)
	if !reason.Valid() {
		return nil, fmt.Errorf("%w: reason %q is not missing_knowledge, wrong_decision, required_gate or other", errInvalid, reason)
	}

	by, it, err := s.decide(ctx, req.Item)
	if err != nil {
		return nil, err
	}

	touches, err := s.items.ItemTouches(ctx, it.ID)
	if err != nil {
		return nil, err
	}

	if !slices.ContainsFunc(touches, func(t domain.Touch) bool { return t.EventID == req.Event }) {
		return nil, errTouchNotFound
	}

	if err := s.items.LabelTouch(ctx, req.Event, reason, by); errors.Is(err, ports.ErrNotFound) {
		return nil, errTouchNotFound
	} else if err != nil {
		return nil, err
	}

	return gen.SetTouchReason204Response{}, nil
}

// touchStatsOf is the contract's view of st: versions numbered, the newest first, the items no
// agent ran last.
func (s *server) touchStatsOf(ctx context.Context, st domain.TouchStats) (gen.TouchStats, error) {
	out := gen.TouchStats{
		Finished: st.Finished, Touches: st.Touches, Planned: st.Planned, Unplanned: st.Unplanned(),
		TouchlessRate: st.TouchlessRate(), PerItemMean: st.PerItem(st.Touches), PerItemMedian: st.Median,
		PlannedPerItem: st.PerItem(st.Planned), UnplannedPerItem: st.PerItem(st.Unplanned()),
		ByStation: touchCounts(st.ByStation), ByKind: touchCounts(st.ByKind), ByReason: make([]gen.TouchReasonCount, 0, len(st.ByReason)),
		ByVersion: make([]gen.VersionTouches, 0, len(st.ByVersion)), LineStops: st.LineStops,
	}

	for _, c := range st.ByReason {
		rc := gen.TouchReasonCount{Touches: c.Touches, Planned: c.Planned, Unplanned: c.Touches - c.Planned}
		if c.Key != "" {
			rc.Reason = new(gen.TouchReason(c.Key))
		}

		out.ByReason = append(out.ByReason, rc)
	}

	for _, v := range st.ByVersion {
		vt := gen.VersionTouches{
			Finished: v.Finished, Touches: v.Touches, TouchlessRate: v.TouchlessRate(), PerItemMean: v.PerItem(v.Touches),
			PlannedPerItem: v.PerItem(v.Planned), UnplannedPerItem: v.PerItem(v.Touches - v.Planned),
		}

		if v.VersionID != "" {
			dv, err := s.versions.Version(ctx, v.VersionID)
			if err != nil {
				return gen.TouchStats{}, fmt.Errorf("version %s: %w", v.VersionID, err)
			}

			vt.VersionId, vt.Number = optional(string(v.VersionID)), &dv.Number
		}

		out.ByVersion = append(out.ByVersion, vt)
	}

	// Numbers start at 1, so the items no agent ran (no number, 0) come last.
	slices.SortFunc(out.ByVersion, func(a, b gen.VersionTouches) int { return cmp.Compare(deref(b.Number), deref(a.Number)) })

	return out, nil
}

func touchCounts(cs []domain.TouchCount) []gen.TouchCount {
	out := make([]gen.TouchCount, 0, len(cs))
	for _, c := range cs {
		out = append(out, gen.TouchCount{Key: c.Key, Touches: c.Touches, Planned: c.Planned, Unplanned: c.Touches - c.Planned})
	}

	return out
}

// touchHeadline is st in brief.
func touchHeadline(st domain.TouchStats) gen.TouchHeadline {
	return gen.TouchHeadline{
		Finished: st.Finished, PerItemMean: st.PerItem(st.Touches), PerItemMedian: st.Median, TouchlessRate: st.TouchlessRate(),
	}
}

// itemTouchesOf is an item's touches as getItem shows them.
func (s *server) itemTouchesOf(touches []domain.Touch) *gen.ItemTouches {
	out := &gen.ItemTouches{Count: len(touches), List: make([]gen.Touch, 0, len(touches))}

	for _, t := range touches {
		if t.Planned {
			out.Planned++
		}

		a, err := domain.ActorFromRef(t.Actor)
		if err != nil {
			a = domain.Actor{Kind: domain.ActorUser, ID: t.Actor}
		}

		if a != domain.LocalUser {
			a.Name = t.ActorName
		}

		tc := gen.Touch{
			EventId: t.EventID, Kind: string(t.Kind), Station: optional(t.Station), At: t.At, Actor: s.actorOut(a),
			Planned: t.Planned, ReasonBy: optional(t.ReasonBy),
		}

		if t.Reason != "" {
			tc.Reason = new(gen.TouchReason(t.Reason))
		}

		out.List = append(out.List, tc)
	}

	out.Unplanned = out.Count - out.Planned

	return out
}
