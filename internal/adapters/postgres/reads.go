package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/postgres/db"
	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// ItemByNumber implements ports.Store.
func (s *Store) ItemByNumber(ctx context.Context, factory domain.FactoryID, number int64) (domain.Item, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return domain.Item{}, err
	}

	row, err := fetch(ctx, s.read, func(q *db.Queries) (db.WorkItem, error) {
		return q.GetItemByNumber(ctx, db.GetItemByNumberParams{FactoryID: fid, Number: number})
	})
	if notFound(err) {
		return domain.Item{}, fmt.Errorf("item %d of factory %s: %w", number, factory, ports.ErrNotFound)
	}

	if err != nil {
		return domain.Item{}, fmt.Errorf("item %d of factory %s: %w", number, factory, err)
	}

	return itemFrom(row)
}

// ListItems implements ports.Store.
func (s *Store) ListItems(ctx context.Context, q ports.ItemQuery) ([]domain.Item, error) {
	fid, err := parseID("factory", string(q.FactoryID))
	if err != nil {
		return nil, err
	}

	states := make([]string, 0, len(q.States))
	for _, st := range q.States {
		states = append(states, string(st))
	}

	rows, err := fetch(ctx, s.read, func(dq *db.Queries) ([]db.WorkItem, error) {
		return dq.ListItems(ctx, db.ListItemsParams{
			FactoryID: fid, States: states, Station: q.Station, Before: q.Before, MaxItems: int64(q.Limit),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list items of factory %s: %w", q.FactoryID, err)
	}

	return convertAll(rows, itemFrom)
}

// Assemblies implements ports.Store. Ids that are not UUIDs name no item.
func (s *Store) Assemblies(ctx context.Context, factory domain.FactoryID, ids []domain.ItemID) (map[domain.ItemID]domain.Assembly, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	out := map[domain.ItemID]domain.Assembly{}
	if len(ids) == 0 {
		return out, nil
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.AssemblyPartsRow, error) {
		return q.AssemblyParts(ctx, db.AssemblyPartsParams{Ids: uuids(ids), FactoryID: fid})
	})
	if err != nil {
		return nil, fmt.Errorf("assemblies of factory %s: %w", factory, err)
	}

	for _, r := range rows {
		id := domain.ItemID(r.AssemblyID.String())

		a, ok := out[id]
		if !ok {
			a = domain.Assembly{ID: id, Number: r.AssemblyNumber, Title: r.AssemblyTitle}
		}

		a.Parts = append(a.Parts, domain.Part{
			ID: domain.ItemID(r.ID.String()), Number: r.Number, Station: r.Station, State: domain.ItemState(r.State),
		})
		out[id] = a
	}

	return out, nil
}

// StationLoads implements ports.Store.
func (s *Store) StationLoads(ctx context.Context, factory domain.FactoryID) ([]domain.StationLoad, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return nil, err
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.StationLoadsRow, error) {
		return q.StationLoads(ctx, fid)
	})
	if err != nil {
		return nil, fmt.Errorf("station loads of factory %s: %w", factory, err)
	}

	out := make([]domain.StationLoad, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.StationLoad{
			Station: r.Station, Waiting: int(r.Waiting), InProgress: int(r.InProgress), WaitingHuman: int(r.WaitingHuman),
		})
	}

	return out, nil
}

func seconds(s float64) time.Duration { return time.Duration(math.Round(s * float64(time.Second))) }

// SpendSince implements ports.Store.
func (s *Store) SpendSince(ctx context.Context, factory domain.FactoryID, since time.Time) (float64, error) {
	fid, err := parseID("factory", string(factory))
	if err != nil {
		return 0, err
	}

	usd, err := fetch(ctx, s.read, func(q *db.Queries) (float64, error) {
		return q.FactorySpendSince(ctx, db.FactorySpendSinceParams{FactoryID: fid, Since: since})
	})
	if err != nil {
		return 0, fmt.Errorf("spend of factory %s since %s: %w", factory, since.Format(time.RFC3339), err)
	}

	return usd, nil
}

// Attention implements ports.Store. Its reads share one snapshot.
func (s *Store) Attention(ctx context.Context) ([]domain.Attention, error) {
	var out []domain.Attention

	err := s.orgTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(q *db.Queries) error {
		rows, err := q.FactoryAttention(ctx)
		if err != nil {
			return fmt.Errorf("factories: %w", err)
		}

		byType, err := q.WaitingByType(ctx)
		if err != nil {
			return fmt.Errorf("waiting by type: %w", err)
		}

		types := map[uuid.UUID][]domain.TypeCount{}
		for _, r := range byType {
			types[r.FactoryID] = append(types[r.FactoryID], domain.TypeCount{Type: r.Type, Count: int(r.Waiting)})
		}

		out = make([]domain.Attention, 0, len(rows))
		for _, r := range rows {
			a := domain.Attention{
				Factory: domain.FactoryID(r.FactoryID.String()), WaitingHuman: int(r.WaitingHuman),
				WaitingByType: types[r.FactoryID], Blocked: int(r.Blocked),
			}

			if r.AndonID != nil {
				p, err := andonFrom(db.AndonPull{
					ID: *r.AndonID, FactoryID: r.FactoryID, Kind: deref(r.AndonKind), Station: r.AndonStation, Reason: deref(r.AndonReason),
					PulledBy: deref(r.AndonPulledBy), PulledAt: *r.AndonPulledAt, Data: r.AndonData,
				})
				if err != nil {
					return err
				}

				a.Andon = &p
			}

			out = append(out, a)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("attention: %w", err)
	}

	return out, nil
}

// InboxItems implements ports.FlowStats. A factory id that is not a UUID names no factory.
func (s *Store) InboxItems(ctx context.Context, factories []domain.FactoryID, limit int) ([]domain.AttentionItem, error) {
	ids := make([]uuid.UUID, 0, len(factories))

	for _, f := range factories {
		if id, err := uuid.Parse(string(f)); err == nil {
			ids = append(ids, id)
		}
	}

	rows, err := fetch(ctx, s.read, func(q *db.Queries) ([]db.InboxItemsRow, error) {
		return q.InboxItems(ctx, db.InboxItemsParams{Factories: ids, MaxItems: int64(limit)})
	})
	if err != nil {
		return nil, fmt.Errorf("items that may need a person: %w", err)
	}

	return convertAll(rows, attentionItemFrom)
}

func attentionItemFrom(r db.InboxItemsRow) (domain.AttentionItem, error) {
	it, err := itemFrom(r.WorkItem)
	if err != nil {
		return domain.AttentionItem{}, err
	}

	var data struct {
		Reason string `json:"reason"`
	}

	if err := json.Unmarshal(r.BlockerData, &data); err != nil {
		return domain.AttentionItem{}, fmt.Errorf("item %s: blocking event: %w", it.ID, err)
	}

	return domain.AttentionItem{
		Item: it, BlockedReason: data.Reason, Paused: r.BlockerKind == string(domain.EventPaused),
	}, nil
}
