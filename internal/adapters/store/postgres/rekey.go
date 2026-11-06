package postgres

import (
	"context"
	"fmt"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ConnectionKeyIDs are the master key ids some connection, credential or webhook endpoint of any
// organization is sealed under (goal_connection_key_ids, as 00071 replaced it): goald refuses to
// start without each of them.
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

// rekeyBatch is how many connections, credentials or webhook secrets one transaction of Rekey
// reseals.
const rekeyBatch = 100

// Rekey reseals every connection, credential (a secret's previous value too, dropping the ones
// that have lapsed) and webhook signing secret of every organization
// that is sealed under another master key than key's active one (`goal db rekey`, spec 4 §5.2,
// ADR 0017, ADR 0026), rekeyBatch per transaction, and returns how many it resealed. s's pool
// connects as goal_owner, which row-level security does not bind: rekeying is the install's, not
// an organization's. Values never leave this function; a row that does not open stops it with an error naming its id.
func Rekey(ctx context.Context, s *Store, key ports.MasterKey) (int, error) {
	if key.Active() == "" {
		return 0, ports.ErrNoMasterKey
	}

	total := 0

	for _, batch := range []func(context.Context, *db.Queries, ports.MasterKey) (int, error){
		resealConnections, resealSecrets, resealPreviousSecrets, resealWebhookSecrets,
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

// resealConnections reseals a batch of connections of every organization sealed under another
// key than key's active one, and returns how many.
func resealConnections(ctx context.Context, q *db.Queries, key ports.MasterKey) (int, error) {
	rows, err := q.StaleSealedConnections(ctx, db.StaleSealedConnectionsParams{Active: key.Active(), Batch: rekeyBatch})
	if err != nil {
		return 0, err
	}

	for _, r := range rows {
		if err := reseal(ctx, q, key, r); err != nil {
			return 0, err
		}
	}

	return len(rows), nil
}

func reseal(ctx context.Context, q *db.Queries, key ports.MasterKey, r db.StaleSealedConnectionsRow) error {
	org, id := domain.OrgID(r.OrgID.String()), domain.ConnectionID(r.ID.String())

	plain, err := key.Open(*r.KeyID, r.Nonce, r.Ciphertext, domain.ConnectionAAD(org, id, *r.KeyID))
	if err != nil {
		return fmt.Errorf("connection %s: %w", id, err)
	}

	active := key.Active()

	keyID, nonce, ct, err := key.Seal(plain, domain.ConnectionAAD(org, id, active))
	if err != nil {
		return fmt.Errorf("connection %s: %w", id, err)
	}

	return q.ResealConnection(ctx, db.ResealConnectionParams{KeyID: &keyID, Nonce: nonce, Ciphertext: ct, ID: r.ID})
}
