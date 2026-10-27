package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Spend = (*Store)(nil)

// OrgSpend implements ports.Spend: five reads of one snapshot (db/queries/spend.sql), laid out
// by domain.NewOrgSpend.
func (s *Store) OrgSpend(ctx context.Context, w domain.SpendWindow) (domain.OrgSpend, error) {
	if err := w.Check(); err != nil {
		return domain.OrgSpend{}, err
	}

	since, until := w.Bounds()
	from := w.ReadSince()

	var f domain.SpendFacts

	err := s.orgTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(q *db.Queries) error {
		days, err := q.OrgSpendByDay(ctx, db.OrgSpendByDayParams{Since: from, Until: until})
		if err != nil {
			return fmt.Errorf("by day: %w", err)
		}

		for _, d := range days {
			f.Days = append(f.Days, domain.FactoryDaySpend{
				Day: d.Day.UTC(), FactoryID: domain.FactoryID(d.FactoryID.String()), USD: d.SpendUsd,
			})
		}

		agents, err := q.OrgSpendByAgent(ctx, db.OrgSpendByAgentParams{Since: since, Until: until})
		if err != nil {
			return fmt.Errorf("by agent: %w", err)
		}

		for _, a := range agents {
			f.ByAgent = append(f.ByAgent, domain.AgentSpend{
				FactoryID: domain.FactoryID(a.FactoryID.String()), Agent: a.Agent, USD: a.SpendUsd,
			})
		}

		models, err := q.OrgSpendByModel(ctx, db.OrgSpendByModelParams{Since: since, Until: until})
		if err != nil {
			return fmt.Errorf("by model: %w", err)
		}

		for _, m := range models {
			f.ByModel = append(f.ByModel, domain.ModelSpend{Model: m.Model, USD: m.SpendUsd})
		}

		if f.ReworkUSD, err = q.OrgReworkSpend(ctx, db.OrgReworkSpendParams{Since: since, Until: until}); err != nil {
			return fmt.Errorf("rework: %w", err)
		}

		merged, err := q.OrgMergedByDay(ctx, db.OrgMergedByDayParams{Since: from, Until: until})
		if err != nil {
			return fmt.Errorf("merged items: %w", err)
		}

		for _, m := range merged {
			f.Merged = append(f.Merged, domain.DayCount{Day: m.Day.UTC(), Count: int(m.Merged)})
		}

		return nil
	})
	if err != nil {
		return domain.OrgSpend{}, fmt.Errorf("organization spend this %s: %w", w.Period, err)
	}

	return domain.NewOrgSpend(w, f), nil
}
