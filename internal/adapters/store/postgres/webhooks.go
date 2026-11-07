package postgres

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The organization's webhooks (00115, ADR 0040 decisions 12 and 15): rows of webhook_endpoints
// without a factory, each signed by an organization secret named by id and open to the factories
// of its webhook_openings rows.

// orgWebhookNameKey keeps an organization webhook's name unique in its organization.
const orgWebhookNameKey = "webhook_endpoints_org_name_key"

// CreateWebhook implements ports.Intake.
func (in *Intake) CreateWebhook(ctx context.Context, n domain.NewWebhook) (domain.WebhookEndpoint, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.WebhookEndpoint{}, domain.ErrNoOrg
	}

	if err := n.Check(); err != nil {
		return domain.WebhookEndpoint{}, fmt.Errorf("create webhook: %w", err)
	}

	id, err := newID()
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	e, err := fetch(ctx, in.s.inTx, func(q *db.Queries) (domain.WebhookEndpoint, error) {
		taken, err := q.WebhookNameTaken(ctx, db.WebhookNameTakenParams{Name: n.Name, ExceptID: uuid.Nil})
		if err != nil {
			return domain.WebhookEndpoint{}, err
		}

		if taken {
			return domain.WebhookEndpoint{}, fmt.Errorf("webhook %q: %w", n.Name, ports.ErrNameTaken)
		}

		_, err = q.InsertOrgWebhook(ctx, db.InsertOrgWebhookParams{
			ID: id, Name: n.Name, Sender: new(string(n.Sender)), Secret: n.Secret,
			TokenHeader: nonEmpty(n.Token.Header), TokenEventPath: nonEmpty(n.Token.EventPath), TokenIDPath: nonEmpty(n.Token.IDPath),
		})

		switch {
		case notFound(err):
			return domain.WebhookEndpoint{}, fmt.Errorf("secret %q: %w", n.Secret, ports.ErrNotFound)
		case violates(err, orgWebhookNameKey):
			return domain.WebhookEndpoint{}, fmt.Errorf("webhook %q: %w", n.Name, ports.ErrNameTaken)
		case err != nil:
			return domain.WebhookEndpoint{}, err
		}

		return endpointIn(ctx, q, id)
	})
	if err != nil {
		return domain.WebhookEndpoint{}, fmt.Errorf("create webhook %s: %w", n.Name, ruleError(err, ""))
	}

	return e, nil
}

// Webhooks implements ports.Intake.
func (in *Intake) Webhooks(ctx context.Context) ([]domain.WebhookEndpoint, error) {
	out, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]domain.WebhookEndpoint, error) {
		rows, err := q.OrgWebhooks(ctx)
		if err != nil {
			return nil, err
		}

		return withOpenings(ctx, q, convertRows(rows, func(r db.OrgWebhooksRow) db.WebhookEndpointRow { return db.WebhookEndpointRow(r) }))
	})
	if err != nil {
		return nil, fmt.Errorf("webhooks: %w", err)
	}

	return out, nil
}

// WebhookByName implements ports.Intake.
func (in *Intake) WebhookByName(ctx context.Context, name string) (domain.WebhookEndpoint, error) {
	e, err := fetch(ctx, in.s.read, func(q *db.Queries) (domain.WebhookEndpoint, error) {
		r, err := q.OrgWebhookByName(ctx, name)
		if err != nil {
			return domain.WebhookEndpoint{}, err
		}

		out, err := withOpenings(ctx, q, []db.WebhookEndpointRow{db.WebhookEndpointRow(r)})
		if err != nil {
			return domain.WebhookEndpoint{}, err
		}

		return out[0], nil
	})

	switch {
	case notFound(err):
		return domain.WebhookEndpoint{}, fmt.Errorf("webhook %q: %w", name, ports.ErrNotFound)
	case err != nil:
		return domain.WebhookEndpoint{}, fmt.Errorf("webhook %q: %w", name, err)
	default:
		return e, nil
	}
}

// PatchWebhook implements ports.Intake.
func (in *Intake) PatchWebhook(ctx context.Context, id domain.WebhookEndpointID, p domain.WebhookPatch) (domain.WebhookEndpoint, error) {
	if _, ok := domain.OrgFrom(ctx); !ok {
		return domain.WebhookEndpoint{}, domain.ErrNoOrg
	}

	if err := p.Check(); err != nil {
		return domain.WebhookEndpoint{}, fmt.Errorf("patch webhook %s: %w", id, err)
	}

	uid, err := parseID("webhook", string(id))
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	e, err := fetch(ctx, in.s.inTx, func(q *db.Queries) (domain.WebhookEndpoint, error) { return patchIn(ctx, q, uid, p) })

	switch {
	case notFound(err):
		return domain.WebhookEndpoint{}, fmt.Errorf("webhook %s: %w", id, ports.ErrNotFound)
	case err != nil:
		return domain.WebhookEndpoint{}, fmt.Errorf("patch webhook %s: %w", id, ruleError(err, ""))
	default:
		return e, nil
	}
}

// patchIn applies p to the organization's webhook id in q's transaction, locking it first.
func patchIn(ctx context.Context, q *db.Queries, id uuid.UUID, p domain.WebhookPatch) (domain.WebhookEndpoint, error) {
	if _, err := q.LockOrgWebhook(ctx, id); err != nil {
		return domain.WebhookEndpoint{}, err
	}

	cur, err := endpointIn(ctx, q, id)
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	next := p.Apply(cur)

	if next.Name != cur.Name {
		taken, err := q.WebhookNameTaken(ctx, db.WebhookNameTakenParams{Name: next.Name, ExceptID: id})
		if err != nil {
			return domain.WebhookEndpoint{}, err
		}

		if taken {
			return domain.WebhookEndpoint{}, fmt.Errorf("webhook %q: %w", next.Name, ports.ErrNameTaken)
		}
	}

	err = q.UpdateOrgWebhook(ctx, db.UpdateOrgWebhookParams{Name: next.Name, Enabled: next.Enabled, ID: id})
	if violates(err, orgWebhookNameKey) {
		return domain.WebhookEndpoint{}, fmt.Errorf("webhook %q: %w", next.Name, ports.ErrNameTaken)
	}

	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	if err := openings(ctx, q, id, p); err != nil {
		return domain.WebhookEndpoint{}, err
	}

	return endpointIn(ctx, q, id)
}

// openings writes p's openings and closings of webhook id.
func openings(ctx context.Context, q *db.Queries, id uuid.UUID, p domain.WebhookPatch) error {
	for _, f := range p.Close {
		fid, err := parseID("factory", string(f))
		if err != nil {
			return err
		}

		if err := q.DeleteWebhookOpening(ctx, db.DeleteWebhookOpeningParams{WebhookID: id, FactoryID: fid}); err != nil {
			return err
		}
	}

	for _, o := range p.Open {
		fid, err := parseID("factory", string(o.FactoryID))
		if err != nil {
			return err
		}

		var repos []string
		if len(o.Repositories) > 0 {
			repos = o.Repositories
		}

		err = q.UpsertWebhookOpening(ctx, db.UpsertWebhookOpeningParams{WebhookID: id, FactoryID: fid, Repositories: repos})
		if isForeignKeyViolation(err) {
			return fmt.Errorf("factory %s: %w", o.FactoryID, ports.ErrNotFound)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// WebhookSecretUses implements ports.Intake.
func (in *Intake) WebhookSecretUses(ctx context.Context) ([]domain.WebhookSecretUse, error) {
	rows, err := fetch(ctx, in.s.read, func(q *db.Queries) ([]db.WebhookSecretUsesRow, error) { return q.WebhookSecretUses(ctx) })
	if err != nil {
		return nil, fmt.Errorf("webhook secret uses: %w", err)
	}

	out := make([]domain.WebhookSecretUse, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.WebhookSecretUse{Secret: r.Secret, Webhook: r.Webhook})
	}

	return out, nil
}

// endpointIn is endpoint id with its openings, read in q's transaction.
func endpointIn(ctx context.Context, q *db.Queries, id uuid.UUID) (domain.WebhookEndpoint, error) {
	r, err := q.WebhookEndpoint(ctx, id)
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	out, err := withOpenings(ctx, q, []db.WebhookEndpointRow{r})
	if err != nil {
		return domain.WebhookEndpoint{}, err
	}

	return out[0], nil
}

// withOpenings are rows as endpoints, each with its openings, by factory id.
func withOpenings(ctx context.Context, q *db.Queries, rows []db.WebhookEndpointRow) ([]domain.WebhookEndpoint, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}

	opened, err := q.WebhookOpenings(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := make([]domain.WebhookEndpoint, 0, len(rows))

	for _, r := range rows {
		e := endpointFrom(r)
		e.Openings = []domain.WebhookOpening{}

		for _, o := range opened {
			if o.WebhookID == r.ID {
				e.Openings = append(e.Openings, domain.WebhookOpening{FactoryID: domain.FactoryID(o.FactoryID.String()), Repositories: slices.Clone(o.Repositories)})
			}
		}

		out = append(out, e)
	}

	return out, nil
}

// convertRows are rows converted one by one.
func convertRows[R, T any](rows []R, convert func(R) T) []T {
	out := make([]T, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert(r))
	}

	return out
}

// nonEmpty maps "" to NULL.
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
