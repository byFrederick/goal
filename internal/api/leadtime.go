package api

import (
	"context"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
)

// bridgeMinItems is how many items done the lead-time bridge needs (domain.BridgeLeadTime).
const bridgeMinItems = 5

func (s *server) GetLeadTimeBridge(ctx context.Context, req gen.GetLeadTimeBridgeRequestObject) (gen.GetLeadTimeBridgeResponseObject, error) {
	p := req.Params
	now := s.clock.Now().UTC()

	r, err := rangeOf(nil, p.From, p.To, now)
	if err != nil {
		return nil, err
	}

	f, err := s.factory(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	typ, err := workTypeOf(f, p.Type)
	if err != nil {
		return nil, err
	}

	l := f.Line()

	items, err := s.items.ItemTimes(ctx, r.query(l.FactoryID, typ, now))
	if err != nil {
		return nil, err
	}

	out := gen.GetLeadTimeBridge200JSONResponse{
		From: openapi_types.Date{Time: r.from}, To: openapi_types.Date{Time: r.to}, Type: optional(typ),
		ItemsDone: len(items), Steps: []gen.BridgeStep{},
	}

	target := l.Flow.Targets.LeadTime
	if target > 0 {
		out.TargetHours = new(target.Hours())
	}

	leads := make([]time.Duration, 0, len(items))
	for _, it := range items {
		leads = append(leads, it.LeadTime)
	}

	actual := domain.Percentile(leads, 0.85)
	if len(items) > 0 {
		out.ActualP85Hours = new(actual.Hours())
	}

	switch {
	case target <= 0:
		out.Status = gen.NoTarget
	case len(items) < bridgeMinItems:
		out.Status = gen.NotEnoughData
	case actual <= target:
		out.Status = gen.OnTarget
	default:
		overTarget(&out, items, target, actual)
	}

	if target > 0 {
		c, err := s.constraintBusy(ctx, l, r, now)
		if err != nil {
			return nil, err
		}

		out.Constraint = c
	}

	return out, nil
}

// overTarget fills out with the gap between actual and target, the items over the target and
// the bridge's steps.
func overTarget(out *gen.GetLeadTimeBridge200JSONResponse, items []domain.ItemTime, target, actual time.Duration) {
	out.Status = gen.OverTarget
	out.GapHours = new((actual - target).Hours())

	for _, it := range items {
		if it.LeadTime > target {
			out.OverTargetItems++
		}
	}

	for _, st := range domain.BridgeLeadTime(items, target, actual).Steps {
		out.Steps = append(out.Steps, gen.BridgeStep{
			Component: gen.BridgeStepComponent(st.Component.Kind), Station: optional(st.Component.Station),
			Hours: st.Duration.Hours(), Share: st.Share, Items: st.Items,
		})
	}
}

// constraintBusy is the line's constraint (drum) with its busy share over r, for the bridge's
// insight; nil without a constraint, a nil share before the factory's lease history.
func (s *server) constraintBusy(ctx context.Context, l domain.Line, r dayRange, now time.Time) (*struct {
	BusyRatio *float64 `json:"busy_ratio"`
	Station   string   `json:"station"`
}, error,
) {
	drum, err := s.drum(ctx, l)
	if err != nil || drum == "" {
		return nil, err
	}

	out := &struct {
		BusyRatio *float64 `json:"busy_ratio"`
		Station   string   `json:"station"`
	}{Station: drum}

	until := r.until(now)

	times, history, err := s.items.StationTimes(ctx, l.FactoryID, r.from, until)
	if err != nil || history.IsZero() || !history.Before(until) {
		return out, err
	}

	st, _ := l.Station(drum)

	for _, t := range times {
		if t.Station == drum {
			out.BusyRatio = new(t.BusyRatio(st.Capacity(), until.Sub(maxTime(r.from, history))))
		}
	}

	if out.BusyRatio == nil {
		out.BusyRatio = new(0.0)
	}

	return out, nil
}
