package api

import (
	"context"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The lead-time bridge needs bridgeMinItems items done (domain.BridgeLeadTime) and reads at most
// bridgeMaxItems, those done last in the range, so one request stays bounded on a busy factory.
const (
	bridgeMinItems = 5
	bridgeMaxItems = 50_000
)

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

	items, err := s.items.ItemTimes(ctx, r.query(l.FactoryID, typ, now), bridgeMaxItems)
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

// constraintBusy is the line's constraint over r with its busy share over r, for the bridge's
// insight; nil without a constraint, a nil share before the factory's lease history.
func (s *server) constraintBusy(ctx context.Context, l domain.Line, r dayRange, now time.Time) (*struct {
	BusyRatio *float64 `json:"busy_ratio"`
	Station   string   `json:"station"`
}, error,
) {
	loads, err := s.stationLoads(ctx, l.FactoryID)
	if err != nil {
		return nil, err
	}

	m, err := s.measure(ctx, l, r.from, r.until(now))
	if err != nil {
		return nil, err
	}

	drum := drumOf(l, loads, m.cands)
	if drum == "" {
		return nil, nil
	}

	return &struct {
		BusyRatio *float64 `json:"busy_ratio"`
		Station   string   `json:"station"`
	}{Station: drum, BusyRatio: m.busyOf(drum)}, nil
}
