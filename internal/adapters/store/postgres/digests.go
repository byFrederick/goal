package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Digests = (*Digests)(nil)

// Digests implements ports.Digests over 00082's digests table, which belongs to the person and
// has no row-level security, so its own statements run in transactions bound to no organization
// (as NotificationPreferences does); the recipients and the content run in the organization's
// transaction, under its row-level security (db/queries/digests.sql).
type Digests struct {
	s *Store
}

// NewDigests returns the daily digests on s's pool.
func NewDigests(s *Store) *Digests { return &Digests{s: s} }

// userTx runs fn in a transaction bound to no organization.
func (d *Digests) userTx(ctx context.Context, fn func(q *db.Queries) error) error {
	return pgx.BeginTxFunc(ctx, d.s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error { return fn(d.s.q.WithTx(tx)) })
}

// digestLock is the session advisory lock key of the digest job, per schema as the schedules' is.
const digestLock = "SELECT pg_try_advisory_lock(hashtext('goal.notify.digest.' || current_schema()))"

// WithDigestLock implements ports.Digests. As Intake.WithScheduleLock, the lock is a session
// advisory lock on a connection of its own, straight to PostgreSQL and not from the pool, so a
// pooler cannot drop it; closing that connection frees it, even if goald dies while fn runs.
func (d *Digests) WithDigestLock(ctx context.Context, fn func(ctx context.Context) error) (ran bool, err error) {
	conn, err := pgx.ConnectConfig(ctx, d.s.pool.Config().ConnConfig.Copy())
	if err != nil {
		return false, fmt.Errorf("digest lock: connect: %w", err)
	}

	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
		defer cancel()

		if cerr := conn.Close(closeCtx); cerr != nil {
			err = errors.Join(err, fmt.Errorf("digest lock: close: %w", cerr))
		}
	}()

	var held bool
	if err := conn.QueryRow(ctx, digestLock).Scan(&held); err != nil {
		return false, fmt.Errorf("digest lock: %w", err)
	}

	if !held {
		return false, nil
	}

	return true, fn(ctx)
}

// DigestRecipients implements ports.Digests.
func (d *Digests) DigestRecipients(ctx context.Context) ([]ports.DigestRecipient, error) {
	def := domain.DefaultNotificationPreferences().Events[domain.NotifyDigest]

	rows, err := fetch(ctx, d.s.read, func(q *db.Queries) ([]db.DigestRecipientsRow, error) {
		return q.DigestRecipients(ctx, db.DigestRecipientsParams{DefaultEmail: def.Email, DefaultSlack: def.Slack})
	})
	if err != nil {
		return nil, fmt.Errorf("digest recipients: %w", err)
	}

	out := make([]ports.DigestRecipient, 0, len(rows))
	for _, r := range rows {
		out = append(out, ports.DigestRecipient{
			UserID: domain.UserID(r.UserID.String()), TimeZone: r.TimeZone, Email: r.Email, Slack: r.Slack && r.SlackConnected,
		})
	}

	return out, nil
}

// queuedDigest is one element of QueueDigests' JSON array.
type queuedDigest struct {
	UserID    string `json:"user_id"`
	LocalDate string `json:"local_date"`
	Channel   string `json:"channel"`
	SlackOrg  string `json:"slack_org"`
}

// QueueDigests implements ports.Digests in one statement; the unique key digests_once_per_date
// keeps what an earlier round, or another instance, queued.
func (d *Digests) QueueDigests(ctx context.Context, ds []ports.NewDigest, at time.Time) (int, error) {
	if len(ds) == 0 {
		return 0, nil
	}

	items := make([]queuedDigest, 0, len(ds))

	for _, n := range ds {
		user, err := parseID("user", string(n.UserID))
		if err != nil {
			return 0, err
		}

		if _, err := time.Parse(time.DateOnly, n.LocalDate); err != nil {
			return 0, fmt.Errorf("digest local date %q: %w", n.LocalDate, domain.ErrInvalid)
		}

		qd := queuedDigest{UserID: user.String(), LocalDate: n.LocalDate, Channel: n.Channel}

		if n.OrgID != "" {
			org, err := parseID("organization", string(n.OrgID))
			if err != nil {
				return 0, err
			}

			qd.SlackOrg = org.String()
		}

		items = append(items, qd)
	}

	raw, err := json.Marshal(items)
	if err != nil {
		return 0, fmt.Errorf("queue digests: %w", err)
	}

	var n int64

	err = d.userTx(ctx, func(q *db.Queries) error {
		n, err = q.QueueDigests(ctx, db.QueueDigestsParams{At: at.UTC(), Digests: raw})

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("queue digests: %w", err)
	}

	return int(n), nil
}

// ClaimDigests implements ports.Digests.
func (d *Digests) ClaimDigests(ctx context.Context, channel string, now time.Time, lease time.Duration, limit int) ([]ports.Digest, error) {
	var rows []db.ClaimDigestsRow

	err := d.userTx(ctx, func(q *db.Queries) error {
		var err error

		rows, err = q.ClaimDigests(ctx, db.ClaimDigestsParams{
			Channel: channel, Now: now, Until: now.Add(lease), Max: int32(min(limit, maxClaim)), //nolint:gosec // bounded by maxClaim
		})

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("claim %s digests: %w", channel, err)
	}

	// RETURNING keeps no order: oldest due first, as the claim chose them.
	slices.SortFunc(rows, func(a, b db.ClaimDigestsRow) int {
		if c := a.NextAttemptAt.Compare(b.NextAttemptAt); c != 0 {
			return c
		}

		return int(a.ID - b.ID)
	})

	out := make([]ports.Digest, 0, len(rows))

	for _, r := range rows {
		g := ports.Digest{
			ID: r.ID, UserID: domain.UserID(r.UserID.String()), LocalDate: r.LocalDate, Channel: r.Channel,
			PeriodStart: r.PeriodStart, PeriodEnd: r.PeriodEnd, Attempts: int(r.Attempts), DueAt: r.NextAttemptAt,
		}
		if r.SlackOrgID != nil {
			g.OrgID = domain.OrgID(r.SlackOrgID.String())
		}

		out = append(out, g)
	}

	return out, nil
}

// FinishDigest implements ports.Digests.
func (d *Digests) FinishDigest(ctx context.Context, id int64, attempts int, o ports.NotificationOutcome) (bool, error) {
	p := db.FinishDigestParams{State: o.State, ID: id, Attempts: int32(attempts)} //nolint:gosec // attempts come from a claim's int32

	switch o.State {
	case ports.NotificationSent:
		at := o.At
		p.SentAt = &at
	case ports.NotificationPending:
		at := o.RetryAt
		p.NextAttemptAt = &at
	}

	if o.Reason != "" {
		reason := o.Reason
		p.LastError = &reason
	}

	var n int64

	err := d.userTx(ctx, func(q *db.Queries) error {
		var err error

		n, err = q.FinishDigest(ctx, p)

		return err
	})
	if err != nil {
		return false, fmt.Errorf("finish digest %d: %w", id, err)
	}

	return n == 1, nil
}

// DigestSummary implements ports.Digests from one read-only transaction of the organization in
// ctx.
func (d *Digests) DigestSummary(ctx context.Context, user domain.UserID, since, until time.Time) (ports.DigestSummary, error) {
	id, err := parseID("user", string(user))
	if err != nil {
		return ports.DigestSummary{}, err
	}

	var out ports.DigestSummary

	err = d.s.read(ctx, func(q *db.Queries) error {
		members, err := q.DigestMember(ctx, id)
		if err != nil || len(members) == 0 {
			return err
		}

		m := members[0]
		role := domain.OrgRole(m.Role)
		out = ports.DigestSummary{Member: true, OrgRole: role, Address: m.Address, OrgSlug: m.OrgSlug, OrgName: m.OrgName}

		needs, err := q.DigestNeedsYou(ctx, db.DigestNeedsYouParams{
			UserID: id, EveryFactory: role == domain.OrgOwner || role == domain.OrgAdmin,
		})
		if err != nil {
			return err
		}

		activity, err := q.DigestActivity(ctx, db.DigestActivityParams{Since: since, Until: until})
		if err != nil {
			return err
		}

		spend, err := q.DigestSpend(ctx, db.DigestSpendParams{Since: since, Until: until})
		if err != nil {
			return err
		}

		out.NeedsYouDecisions, out.NeedsYouAndons = int(needs.Decisions), int(needs.Andons)
		out.Done, out.AndonsPulled = int(activity.Done), int(activity.Pulled)

		for _, s := range spend {
			out.Spend = append(out.Spend, ports.FactorySpend{FactoryName: s.FactoryName, FactoryTitle: s.FactoryTitle, USD: s.SpendUsd})
		}

		return nil
	})
	if err != nil {
		return ports.DigestSummary{}, fmt.Errorf("digest of %s: %w", user, err)
	}

	return out, nil
}
