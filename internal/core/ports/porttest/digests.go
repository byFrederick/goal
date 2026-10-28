package porttest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var _ ports.Digests = (*Digests)(nil)

// DigestRow is one digest of a Digests: the claim's view of it and its delivery state.
type DigestRow struct {
	ports.Digest
	State         string
	NextAttemptAt time.Time
	LeaseUntil    time.Time // zero: no lease
	LastError     string
	SentAt        time.Time
}

// Digests keeps the daily digests in memory with the database's rules: one digest per person,
// local date, channel and organization, a period from the end of the last sent one (else 24
// hours), the outbox's claim rules and one lock shared by every view, as goalds share one
// database. A test sets each organization's recipients and each person's summary in it.
type Digests struct {
	lock sync.Mutex

	mu         sync.Mutex
	recipients map[domain.OrgID][]ports.DigestRecipient
	summaries  map[domain.OrgID]map[domain.UserID]ports.DigestSummary
	rows       []*DigestRow
	nextID     int64
}

// NewDigests returns digests with no recipients and no rows.
func NewDigests() *Digests {
	return &Digests{
		recipients: map[domain.OrgID][]ports.DigestRecipient{},
		summaries:  map[domain.OrgID]map[domain.UserID]ports.DigestSummary{},
	}
}

// SetRecipients makes rs the digest recipients of org.
func (d *Digests) SetRecipients(org domain.OrgID, rs ...ports.DigestRecipient) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.recipients[org] = slices.Clone(rs)
}

// SetSummary makes s what user's digest says of org, whatever the period; s.Member is set.
func (d *Digests) SetSummary(org domain.OrgID, user domain.UserID, s ports.DigestSummary) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.summaries[org] == nil {
		d.summaries[org] = map[domain.UserID]ports.DigestSummary{}
	}

	s.Member = true
	d.summaries[org][user] = s
}

// Rows returns every digest as it is now, in the order queued.
func (d *Digests) Rows() []DigestRow {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]DigestRow, 0, len(d.rows))
	for _, r := range d.rows {
		out = append(out, *r)
	}

	return out
}

// WithDigestLock implements ports.Digests.
func (d *Digests) WithDigestLock(ctx context.Context, fn func(ctx context.Context) error) (bool, error) {
	if !d.lock.TryLock() {
		return false, nil
	}
	defer d.lock.Unlock()

	return true, fn(ctx)
}

// DigestRecipients implements ports.Digests.
func (d *Digests) DigestRecipients(ctx context.Context) ([]ports.DigestRecipient, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.recipients[org]), nil
}

// QueueDigests implements ports.Digests.
func (d *Digests) QueueDigests(_ context.Context, ds []ports.NewDigest, at time.Time) (int, error) {
	for _, n := range ds {
		if _, err := time.Parse(time.DateOnly, n.LocalDate); err != nil || n.UserID == "" ||
			(n.Channel == ports.ChannelSlack) == (n.OrgID == "") {
			return 0, domain.ErrInvalid
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	queued := 0

	for _, n := range ds {
		if slices.ContainsFunc(d.rows, func(r *DigestRow) bool {
			return r.UserID == n.UserID && r.LocalDate == n.LocalDate && r.Channel == n.Channel && r.OrgID == n.OrgID
		}) {
			continue
		}

		d.nextID++
		d.rows = append(d.rows, &DigestRow{
			Digest: ports.Digest{
				ID: d.nextID, UserID: n.UserID, LocalDate: n.LocalDate, Channel: n.Channel, OrgID: n.OrgID,
				PeriodStart: d.periodStart(n, at), PeriodEnd: at, DueAt: at,
			},
			State: ports.NotificationPending, NextAttemptAt: at,
		})
		queued++
	}

	return queued, nil
}

// periodStart is where n's period starts when queued at at: the end of the person's last sent
// digest of its channel and organization before at, or 24 hours before at. d.mu is held.
func (d *Digests) periodStart(n ports.NewDigest, at time.Time) time.Time {
	var last time.Time

	for _, r := range d.rows {
		if r.UserID == n.UserID && r.Channel == n.Channel && r.OrgID == n.OrgID && r.State == ports.NotificationSent &&
			r.PeriodEnd.Before(at) && r.PeriodEnd.After(last) {
			last = r.PeriodEnd
		}
	}

	if last.IsZero() {
		return at.Add(-24 * time.Hour)
	}

	return last
}

// ClaimDigests implements ports.Digests.
func (d *Digests) ClaimDigests(_ context.Context, channel string, now time.Time, lease time.Duration, limit int) ([]ports.Digest, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var due []*DigestRow

	for _, r := range d.rows {
		if r.Channel == channel && r.State == ports.NotificationPending && !r.NextAttemptAt.After(now) &&
			(r.LeaseUntil.IsZero() || !r.LeaseUntil.After(now)) {
			due = append(due, r)
		}
	}

	slices.SortStableFunc(due, func(a, b *DigestRow) int {
		if c := a.NextAttemptAt.Compare(b.NextAttemptAt); c != 0 {
			return c
		}

		return int(a.ID - b.ID)
	})

	out := make([]ports.Digest, 0, min(limit, len(due)))

	for _, r := range due[:min(limit, len(due))] {
		r.LeaseUntil = now.Add(lease)
		r.Attempts++
		r.DueAt = r.NextAttemptAt
		out = append(out, r.Digest)
	}

	return out, nil
}

// FinishDigest implements ports.Digests.
func (d *Digests) FinishDigest(_ context.Context, id int64, attempts int, o ports.NotificationOutcome) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, r := range d.rows {
		if r.ID != id {
			continue
		}

		if r.State != ports.NotificationPending || r.Attempts != attempts {
			return false, nil
		}

		r.State, r.LeaseUntil, r.LastError = o.State, time.Time{}, o.Reason

		switch o.State {
		case ports.NotificationSent:
			r.SentAt = o.At
		case ports.NotificationPending:
			r.NextAttemptAt = o.RetryAt
		}

		return true, nil
	}

	return false, nil
}

// DigestSummary implements ports.Digests.
func (d *Digests) DigestSummary(ctx context.Context, user domain.UserID, _, _ time.Time) (ports.DigestSummary, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return ports.DigestSummary{}, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	s := d.summaries[org][user]
	s.Spend = slices.Clone(s.Spend)

	return s, nil
}
