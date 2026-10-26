package api

import (
	"context"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
)

// drumOf is the station the line is paced by: its configured constraint (flow.constraint naming a
// station), else the busiest of the candidates measured over time, else the constraint of the
// loads now (domain.Line.ConstraintOf); "" without one.
func drumOf(l domain.Line, loads map[string]domain.StationLoad, cands []domain.Candidate) string {
	c, _ := l.ConstraintOf(cands, loads)

	return c
}

// boardLine describes line l, paced by drum, as a whole for the board: whether it runs or an
// open andon pull stops it, its work in progress at
// the stations with a wip limit, its release policy with the drum's buffer, its expedite lane and
// its weekly budget. Release policies and the expedite limit are shown, not enforced (2.2-E,
// deferred).
func (s *server) boardLine(
	ctx context.Context, l domain.Line, drum string, loads map[string]domain.StationLoad, cards map[string][]gen.ItemCard, now time.Time,
) (gen.BoardLine, error) {
	line := gen.BoardLine{
		Status:   gen.BoardLineStatusRunning,
		Wip:      lineWIP(l, loads),
		Release:  gen.LineRelease{Policy: gen.Push, InBuffer: loads[drum].Waiting},
		Expedite: lineExpedite(l, cards),
	}

	open, err := s.items.OpenAndon(ctx, l.FactoryID)
	if err != nil {
		return gen.BoardLine{}, err
	}

	if open != nil {
		line.Status, line.Andon = gen.BoardLineStatusStopped, andonOf(open)
	}

	if l.Flow.Release == domain.ReleaseDBR {
		line.Release.Policy = gen.DrumBufferRope
	}

	if drum != "" {
		line.Release.Drum = &drum
	}

	if st, ok := l.Station(drum); ok && st.Buffer > 0 {
		line.Release.Buffer = &st.Buffer
	}

	if !l.Runtime.HasWeeklyBudget() {
		return line, nil
	}

	spent, err := s.items.SpendSince(ctx, l.FactoryID, domain.BudgetSince(now))
	if err != nil {
		return gen.BoardLine{}, err
	}

	b := l.Runtime.WeeklyBudget(spent, now)
	line.Budget = &gen.LineBudget{WeeklyUsd: b.WeeklyUSD, SpentUsd: b.SpentUSD, Exhausted: b.Exhausted}

	return line, nil
}

// lineWIP counts the items in progress or waiting on a person at the stations with a wip limit,
// against the sum of those limits (none when no station has one).
func lineWIP(l domain.Line, loads map[string]domain.StationLoad) gen.LineWip {
	var wip gen.LineWip

	limit := 0

	for _, st := range l.Stations {
		if st.WIP <= 0 {
			continue
		}

		limit += st.WIP
		wip.InProgress += loads[st.ID].Busy()
	}

	if limit > 0 {
		wip.Limit = &limit
	}

	return wip
}

// lineExpedite counts the open expedite items released into the line (past its queue stations)
// against the lane's limit; nil for a line without expedite work types or a limit.
func lineExpedite(l domain.Line, cards map[string][]gen.ItemCard) *gen.LineExpedite {
	if len(l.ExpediteTypes) == 0 && l.Flow.ExpediteWIP == 0 {
		return nil
	}

	lane := &gen.LineExpedite{}
	if l.Flow.ExpediteWIP > 0 {
		lane.Limit = &l.Flow.ExpediteWIP
	}

	for _, st := range l.Stations {
		if st.Kind == domain.KindQueue {
			continue
		}

		for _, c := range cards[st.ID] {
			open := c.State != gen.ItemState(domain.StateDone) && c.State != gen.ItemState(domain.StateCancelled)
			if open && c.ClassOfService == gen.ClassOfService(domain.ClassExpedite) {
				lane.InUse++
			}
		}
	}

	return lane
}
