package ports

import (
	"context"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
)

// NotificationPreferences keeps each person's notification preferences (ADR 0023, decision 1).
// They belong to the person, not to an organization, so no method needs one in ctx; every call
// names the person whose preferences it reads or writes, and touches no one else's.
type NotificationPreferences interface {
	// NotificationPreferences are user's preferences: what they saved last, or
	// domain.DefaultNotificationPreferences when they never saved any.
	NotificationPreferences(ctx context.Context, user domain.UserID) (domain.NotificationPreferences, error)
	// SetNotificationPreferences replaces all of user's preferences with p, which must pass
	// Validate (domain.ErrInvalid, changing nothing). ErrNotFound when user is no person.
	SetNotificationPreferences(ctx context.Context, user domain.UserID, p domain.NotificationPreferences) error
}

// NotificationOutbox queues the notifications people hear about outside the app (ADR 0023,
// decision 3): one outbox row per recipient and per channel that is on. The store queues the
// approval, question and andon rows itself, in the transaction that records their event; this
// port is for the one event the engine notices on its tick, a weekly budget passing 80%.
type NotificationOutbox interface {
	// QueueBudgetNotification queues the budget event of factory, in the organization of ctx,
	// whose spend in the window that ends at the time at is spentUSD of weeklyUSD, for the organization's
	// owners and admins. It queues at most once per factory per domain.BudgetWindow, whatever
	// the instance that calls: a call within the window of the last queued one does nothing.
	QueueBudgetNotification(ctx context.Context, factory domain.FactoryID, spentUSD, weeklyUSD float64, at time.Time) error
}

// The outbox's channels and the states a claimed row ends in.
const (
	ChannelEmail = "email"
	ChannelSlack = "slack"

	NotificationPending = "pending"
	NotificationSent    = "sent"
	NotificationFailed  = "failed"
	NotificationSkipped = "skipped"
)

// NotificationDeliveries is the sender job's side of the outbox (ADR 0023, decision 3), for the
// organization in ctx. A claim leases due rows; a finish ends the claim under the attempt it
// made, so a worker whose lease ran out and whose row another claim took changes nothing.
type NotificationDeliveries interface {
	// ClaimNotifications leases at most limit due rows of channel (pending, next attempt at or
	// before now, no lease live at now), oldest due first, skipping rows another transaction
	// holds: each gets a lease until now+lease and one more attempt. Each comes with what the
	// message needs and whether its recipient still qualifies.
	ClaimNotifications(ctx context.Context, channel string, now time.Time, lease time.Duration, limit int) ([]Notification, error)
	// FinishNotification ends the claim of row id that made attempt attempts with o, and
	// releases its lease. It reports false, changing nothing, when the row is no longer pending
	// at that attempt: a later claim took it.
	FinishNotification(ctx context.Context, id int64, attempts int, o NotificationOutcome) (bool, error)
}

// Notification is one claimed outbox row, with the names and facts its message shows, read when
// it was claimed. Payload facts an event lacks are zero.
type Notification struct {
	ID       int64
	EventID  string
	Event    domain.NotificationEvent
	Channel  string
	UserID   domain.UserID
	Attempts int // this claim's attempt, from 1
	At       time.Time
	// Qualifies is whether the recipient is still a member who would receive the event (by the
	// queue's recipient rule) with the channel on. Address is their notification email, else
	// their sign-in email; blank when they are no member.
	Qualifies bool
	Address   string

	OrgSlug      string
	FactoryID    domain.FactoryID
	FactoryName  string // the factory's name, which its pages' paths carry
	FactoryTitle string // its title, or its name without one
	ItemNumber   int64  // 0 without an item
	ItemTitle    string
	Station      string
	AndonKind    string
	SpentUSD     float64
	WeeklyUSD    float64
}

// NotificationOutcome is how a claimed row ends: sent at At, back to pending until RetryAt, or
// failed or skipped with Reason, a short code that names no address.
type NotificationOutcome struct {
	State   string
	At      time.Time
	RetryAt time.Time
	Reason  string
}

// Digests keeps the daily digests (ADR 0023, decision 6) in a table that belongs to the person,
// like their preferences: QueueDigests, ClaimDigests and FinishDigest need no organization in
// ctx. DigestRecipients and DigestSummary read the organization in ctx, under its row-level
// security, so a digest reads each organization's content on its own.
type Digests interface {
	// WithDigestLock runs fn and reports true when it takes the install's digest lock, which at
	// most one caller holds at a time across every goald; while another holds it, it reports
	// false without running fn.
	WithDigestLock(ctx context.Context, fn func(ctx context.Context) error) (bool, error)
	// DigestRecipients lists the members of the organization in ctx with the digest on for
	// email or Slack (their saved choice, else domain.DefaultNotificationPreferences).
	DigestRecipients(ctx context.Context) ([]DigestRecipient, error)
	// QueueDigests queues ds as pending digests due at at, each covering [the end of the
	// person's last sent digest of its channel and organization, or at minus 24 hours, at). A
	// digest already queued for the same person, local date, channel and organization is left
	// as it is. It reports how many it queued.
	QueueDigests(ctx context.Context, ds []NewDigest, at time.Time) (int, error)
	// ClaimDigests leases at most limit due digests of channel, by the outbox's claim rules
	// (NotificationDeliveries.ClaimNotifications).
	ClaimDigests(ctx context.Context, channel string, now time.Time, lease time.Duration, limit int) ([]Digest, error)
	// FinishDigest ends the claim of digest id that made attempt attempts with o, as
	// NotificationDeliveries.FinishNotification does.
	FinishDigest(ctx context.Context, id int64, attempts int, o NotificationOutcome) (bool, error)
	// DigestSummary is what user's digest says of the organization in ctx over [since, until).
	// Member is false, and the rest zero, when user is no member of it.
	DigestSummary(ctx context.Context, user domain.UserID, since, until time.Time) (DigestSummary, error)
}

// DigestRecipient is a member with the digest on for at least one channel.
type DigestRecipient struct {
	UserID domain.UserID
	// TimeZone is their IANA time zone, UTC when they saved none. It may be one Go cannot load.
	TimeZone string
	Email    bool
	// Slack is true only while their digest Slack is on and the organization has a Slack
	// connection.
	Slack bool
}

// NewDigest is one digest to queue.
type NewDigest struct {
	UserID    domain.UserID
	LocalDate string // the person's local date, YYYY-MM-DD
	Channel   string
	// OrgID is the organization of a Slack digest; blank for the email digest, which covers every
	// organization.
	OrgID domain.OrgID
}

// Digest is one claimed digest, covering [PeriodStart, PeriodEnd).
type Digest struct {
	ID          int64
	UserID      domain.UserID
	LocalDate   string
	Channel     string
	OrgID       domain.OrgID // blank for the email digest
	PeriodStart time.Time
	PeriodEnd   time.Time
	Attempts    int // this claim's attempt, from 1
	DueAt       time.Time
}

// DigestSummary is one organization's lines of a person's digest.
type DigestSummary struct {
	// Member is whether the person is a member of the organization; OrgRole is their role in it.
	Member  bool
	OrgRole domain.OrgRole
	// Address is their notification email, else their sign-in email.
	Address string
	OrgSlug string
	OrgName string
	// NeedsYou counts what Needs you lists for them now: the items waiting for a decision they
	// may make and the open andon pulls of the factories they edit.
	NeedsYouDecisions int
	NeedsYouAndons    int
	// Done and AndonsPulled count the organization's items done and andons pulled in the period.
	Done         int
	AndonsPulled int
	// Spend is the period's spend by factory, the largest first, without the factories that
	// spent nothing.
	Spend []FactorySpend
}

// FactorySpend is one factory's spend in a digest's period.
type FactorySpend struct {
	FactoryName  string // the name its pages' paths carry
	FactoryTitle string // its title, or its name without one
	USD          float64
}

// Empty reports whether s has nothing to report on any line.
func (s DigestSummary) Empty() bool {
	return s.NeedsYouDecisions == 0 && s.NeedsYouAndons == 0 && s.Done == 0 && s.AndonsPulled == 0 && len(s.Spend) == 0
}
