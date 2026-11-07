package postgres

import (
	"context"
	"fmt"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ConnectionKeyIDs are the master key ids some secret (its value or its previous one) of any
// organization is sealed under (goal_connection_key_ids, as 00135 replaced it):
// goald refuses to start without each of them.
func ConnectionKeyIDs(ctx context.Context, s *Store) ([]string, error) {
	var ids []string

	err := s.systemTx(ctx, func(q *db.Queries) error {
		var err error

		ids, err = q.ConnectionKeyIDs(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("connection key ids: %w", err)
	}

	return ids, nil
}

// rekeyBatch is how many secrets one transaction of Rekey reseals.
const rekeyBatch = 100

// Rekey reseals every secret (its previous value too, dropping the ones that have lapsed) of every
// organization that is sealed under another master key than key's active one (`goal db rekey`,
// spec 4 §5.2, ADR 0017), rekeyBatch per transaction, and returns how many it resealed. s's pool
// connects as goal_owner, which row-level security does not bind: rekeying is the install's, not
// an organization's. Values never leave this function; a row that does not open stops it with an error naming its id.
func Rekey(ctx context.Context, s *Store, key ports.MasterKey) (int, error) {
	if key.Active() == "" {
		return 0, ports.ErrNoMasterKey
	}

	total := 0

	for _, batch := range []func(context.Context, *db.Queries, ports.MasterKey) (int, error){
		resealSecrets, resealPreviousSecrets,
	} {
		for {
			var n int

			err := s.systemTx(ctx, func(q *db.Queries) error {
				var err error

				n, err = batch(ctx, q, key)

				return err
			})
			if err != nil {
				return total, fmt.Errorf("rekey: %w", err)
			}

			total += n

			if n < rekeyBatch {
				break
			}
		}
	}

	return total, nil
}
