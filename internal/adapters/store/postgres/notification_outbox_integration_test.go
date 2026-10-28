//go:build integration

package postgres

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// outboxOrg is an organization with one member of each kind that matters to the recipients: an
// owner, an admin, an approver, an editor and a viewer of factory, a plain member without a role
// and an approver of another factory.
type outboxOrg struct {
	d                                              identityDB
	org                                            domain.OrgID
	factory, other                                 domain.FactoryID
	owner, admin, approver, editor, viewer, member string
	otherApprover                                  string
}

func newOutboxOrg(t *testing.T) outboxOrg {
	t.Helper()

	d := newIdentityDB(t)
	o := outboxOrg{d: d, org: d.org("acme-" + randomHex(t)).ID}
	o.factory, o.other = saveFactory(o.ctx(t), t, d.s), saveFactory(o.ctx(t), t, d.s)

	seed := pgSeed{d}

	for _, m := range []struct {
		id      *string
		name    string
		role    string
		factory domain.FactoryID
		frole   domain.FactoryRole
	}{
		{&o.owner, "Olive", string(domain.OrgOwner), "", ""},
		{&o.admin, "Adam", string(domain.OrgAdmin), "", ""},
		{&o.approver, "Ann", roleMember, o.factory, domain.FactoryApprover},
		{&o.editor, "Ed", roleMember, o.factory, domain.FactoryEditor},
		{&o.viewer, "Vic", roleMember, o.factory, domain.FactoryViewer},
		{&o.member, "Mo", roleMember, "", ""},
		{&o.otherApprover, "Oz", roleMember, o.other, domain.FactoryApprover},
	} {
		*m.id = d.user(m.name)
		d.member(o.org, *m.id, m.role)

		if m.frole != "" {
			seed.SetFactoryRole(t, o.org, domain.UserID(*m.id), m.factory, m.frole)
		}
	}

	return o
}

// ctx is the test's context in the organization.
func (o outboxOrg) ctx(t *testing.T) context.Context {
	t.Helper()

	return domain.WithOrg(t.Context(), o.org)
}

// slackConnection gives the organization a connection of a kind named slack.
func (o outboxOrg) slackConnection(t *testing.T) {
	t.Helper()

	if err := inOrgTx(t, o.d.app, o.org, func(tx pgx.Tx) error {
		kind := newUUID(t)
		if _, err := tx.Exec(t.Context(), `INSERT INTO connection_kinds (id, name, spec, spec_sha256, created_by)
			VALUES ($1, 'slack', '{}', repeat('0', 64), 'system:test')`, kind); err != nil {
			return err
		}

		_, err := tx.Exec(t.Context(), `INSERT INTO connections (id, name, kind_id, created_by, updated_by)
			VALUES ($1, 'team-slack', $2, 'system:test', 'system:test')`, newUUID(t), kind)

		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// prefer saves user's channels for event, the other events keeping their defaults.
func (o outboxOrg) prefer(t *testing.T, user string, event domain.NotificationEvent, ch domain.NotificationChannels) {
	t.Helper()

	p := domain.DefaultNotificationPreferences()
	p.Events[event] = ch

	if err := NewNotificationPreferences(o.d.s).SetNotificationPreferences(t.Context(), domain.UserID(user), p); err != nil {
		t.Fatal(err)
	}
}

// waiting creates an item of factory at a station, queued.
func (o outboxOrg) item(t *testing.T) domain.Item {
	t.Helper()

	it, err := o.d.s.CreateItem(o.ctx(t), domain.NewItem{
		FactoryID: o.factory, Type: "feature", Title: "needs a person", Station: "review", State: domain.StateQueued,
		Class: domain.ClassStandard, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return it
}

// toWaiting moves it to waiting_human with the events kinds, the last of each kind of them
// carrying data.
func (o outboxOrg) toWaiting(t *testing.T, it domain.Item, data map[string]any, kinds ...domain.EventKind) error {
	t.Helper()

	at := time.Now().UTC()

	var events []domain.Event
	for _, k := range kinds {
		events = append(events, domain.Event{FactoryID: it.FactoryID, ItemID: it.ID, At: at, Kind: k, Station: it.Station, Actor: agWorker, Data: data})
	}

	_, err := o.d.s.Transition(o.ctx(t), ports.Transition{
		ItemID: it.ID, From: it.Position(), To: domain.Position{Station: it.Station, State: domain.StateWaitingHuman}, At: at,
		Events: events,
	})

	return err
}

// outboxRow is one row of the outbox as a test compares it.
type outboxRow struct {
	EventID, Event, UserID, Channel, State string
}

// rows are the organization's outbox rows, as the superuser reads them, by user and channel.
func (o outboxOrg) rows(t *testing.T) []outboxRow {
	t.Helper()

	rs, err := o.d.admin.Query(t.Context(), `SELECT event_id, event, user_id::text, channel, state FROM notification_outbox
		WHERE org_id = $1 ORDER BY event_id, user_id, channel`, string(o.org))
	if err != nil {
		t.Fatal(err)
	}

	got, err := pgx.CollectRows(rs, pgx.RowToStructByPos[outboxRow])
	if err != nil {
		t.Fatal(err)
	}

	return got
}

// want lists the pending rows of event eventID for users on channel, sorted as rows sorts them.
func want(eventID, event, channel string, users ...string) []outboxRow {
	out := make([]outboxRow, 0, len(users))
	for _, u := range users {
		out = append(out, outboxRow{EventID: eventID, Event: event, UserID: u, Channel: channel, State: "pending"})
	}

	return out
}

func sortRows(rows []outboxRow) []outboxRow {
	slices.SortFunc(rows, func(a, b outboxRow) int {
		for _, c := range [][2]string{{a.EventID, b.EventID}, {a.UserID, b.UserID}, {a.Channel, b.Channel}} {
			if c[0] != c[1] {
				if c[0] < c[1] {
					return -1
				}

				return 1
			}
		}

		return 0
	})

	return rows
}

// latestEvent is the id of item's newest event of kind.
func (o outboxOrg) latestEvent(t *testing.T, it domain.Item, kind domain.EventKind) string {
	t.Helper()

	var id int64
	if err := o.d.admin.QueryRow(t.Context(), `SELECT max(id) FROM item_events WHERE item_id = $1 AND kind = $2`,
		string(it.ID), string(kind)).Scan(&id); err != nil {
		t.Fatal(err)
	}

	return "event/" + strconv.FormatInt(id, 10)
}

// An item that starts waiting for a person notifies who may decide on it: approvers and editors
// of its factory, owners and admins, never a viewer, a plain member or another factory's
// approver. Without saved preferences approvals go to Slack only (the defaults), and a saved
// preference turns email on for its person.
func TestNotificationOutbox_ApprovalRecipients(t *testing.T) {
	t.Parallel()

	o := newOutboxOrg(t)
	o.slackConnection(t)
	o.prefer(t, o.approver, domain.NotifyApproval, domain.NotificationChannels{InGoal: true, Email: true, Slack: true})
	o.prefer(t, o.admin, domain.NotifyApproval, domain.NotificationChannels{InGoal: true})

	it := o.item(t)
	if err := o.toWaiting(t, it, nil, domain.EventMoved, domain.EventWaitingHuman); err != nil {
		t.Fatal(err)
	}

	id := o.latestEvent(t, it, domain.EventWaitingHuman)
	wantRows := sortRows(append(want(id, "approval", "slack", o.owner, o.approver, o.editor),
		want(id, "approval", "email", o.approver)...))

	if got := o.rows(t); !slices.Equal(got, wantRows) {
		t.Fatalf("outbox = %+v\nwant %+v", got, wantRows)
	}

	var payload map[string]any
	if err := o.d.admin.QueryRow(t.Context(), `SELECT payload FROM notification_outbox WHERE event_id = $1 LIMIT 1`, id).
		Scan(&payload); err != nil {
		t.Fatal(err)
	}

	if payload["item_id"] != string(it.ID) || payload["factory_id"] != string(it.FactoryID) || payload["station"] != it.Station {
		t.Errorf("payload = %v; want the item, its factory and station", payload)
	}
}

// Without a Slack connection no Slack row is queued, so approvals at their defaults queue none;
// and the person whose decision put the item there (data.actor) hears nothing of it.
func TestNotificationOutbox_NoSlackWithoutAConnectionAndNotTheActor(t *testing.T) {
	t.Parallel()

	o := newOutboxOrg(t)

	it := o.item(t)
	if err := o.toWaiting(t, it, nil, domain.EventWaitingHuman); err != nil {
		t.Fatal(err)
	}

	if got := o.rows(t); len(got) != 0 {
		t.Fatalf("outbox = %+v; want no rows without a Slack connection", got)
	}

	for _, u := range []string{o.owner, o.editor} {
		o.prefer(t, u, domain.NotifyApproval, domain.NotificationChannels{InGoal: true, Email: true})
	}

	next := o.item(t)
	if err := o.toWaiting(t, next, map[string]any{"actor": "user:" + o.owner}, domain.EventApproved, domain.EventWaitingHuman); err != nil {
		t.Fatal(err)
	}

	id := o.latestEvent(t, next, domain.EventWaitingHuman)
	if got, w := o.rows(t), want(id, "approval", "email", o.editor); !slices.Equal(got, w) {
		t.Fatalf("outbox = %+v\nwant %+v", got, w)
	}
}

// A question puts the item in waiting_human too; its recipients get one question notice, not an
// approval besides.
func TestNotificationOutbox_QuestionNotApproval(t *testing.T) {
	t.Parallel()

	o := newOutboxOrg(t)
	o.slackConnection(t)

	it := o.item(t)
	if err := o.toWaiting(t, it, nil, domain.EventLeaseTaken, domain.EventQuestionAsked, domain.EventWaitingHuman); err != nil {
		t.Fatal(err)
	}

	id := o.latestEvent(t, it, domain.EventQuestionAsked)
	if got, w := o.rows(t), sortRows(want(id, "question", "slack", o.owner, o.admin, o.approver, o.editor)); !slices.Equal(got, w) {
		t.Fatalf("outbox = %+v\nwant %+v", got, w)
	}
}

// A pulled andon notifies the factory's editors, owners and admins (email and Slack on by
// default), not its approvers, and not the editor who pulled it.
func TestNotificationOutbox_AndonRecipients(t *testing.T) {
	t.Parallel()

	o := newOutboxOrg(t)
	o.slackConnection(t)

	// A second editor, who pulls the line.
	puller := o.d.user("Pia")
	o.d.member(o.org, puller, roleMember)
	pgSeed{o.d}.SetFactoryRole(t, o.org, domain.UserID(puller), o.factory, domain.FactoryEditor)

	pull, err := o.d.s.PullAndon(o.ctx(t), domain.AndonPull{FactoryID: o.factory, Kind: domain.AndonManual, Reason: "stop", PulledAt: time.Now()},
		domain.Actor{Kind: domain.ActorUser, ID: puller, Name: "Pia"})
	if err != nil {
		t.Fatal(err)
	}

	id := "andon/" + strconv.FormatInt(pull.ID, 10)
	w := sortRows(append(want(id, "andon", "email", o.owner, o.admin, o.editor), want(id, "andon", "slack", o.owner, o.admin, o.editor)...))

	if got := o.rows(t); !slices.Equal(got, w) {
		t.Fatalf("outbox = %+v\nwant %+v", got, w)
	}
}

// Queueing an event again queues nothing more, and a transition that fails queues nothing.
func TestNotificationOutbox_EnqueueIsIdempotent(t *testing.T) {
	t.Parallel()

	o := newOutboxOrg(t)
	o.slackConnection(t)

	it := o.item(t)
	if err := o.toWaiting(t, it, nil, domain.EventWaitingHuman); err != nil {
		t.Fatal(err)
	}

	before := o.rows(t)
	if len(before) == 0 {
		t.Fatal("the transition queued nothing")
	}

	var e domain.Event

	if err := o.d.s.inTx(o.ctx(t), func(q *db.Queries) error {
		e = domain.Event{FactoryID: it.FactoryID, ItemID: it.ID, At: time.Now(), Kind: domain.EventWaitingHuman, Station: it.Station}
		return itemNotifications(o.ctx(t), q, []domain.Event{e})
	}); err != nil {
		t.Fatal(err)
	}

	if got := o.rows(t); !slices.Equal(got, before) {
		t.Fatalf("outbox after queueing again = %+v\nwant %+v", got, before)
	}

	// The item is no longer queued, so this transition fails its guard and rolls back.
	if err := o.toWaiting(t, it, nil, domain.EventWaitingHuman); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("stale transition = %v; want ErrConflict", err)
	}

	if got := o.rows(t); !slices.Equal(got, before) {
		t.Fatalf("outbox after a failed transition = %+v\nwant %+v", got, before)
	}
}

// The budget event goes to owners and admins only, once per factory per budget window, however
// often and from however many instances it is asked for.
func TestNotificationOutbox_BudgetOncePerWindow(t *testing.T) {
	t.Parallel()

	o := newOutboxOrg(t)
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

	errs := make(chan error, 4)

	for range 4 {
		go func() { errs <- o.d.s.QueueBudgetNotification(o.ctx(t), o.factory, 8.5, 10, at) }()
	}

	for range 4 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}

	if err := o.d.s.QueueBudgetNotification(o.ctx(t), o.factory, 9, 10, at.Add(domain.BudgetWindow)); err != nil {
		t.Fatal(err)
	}

	id := "budget/" + string(o.factory) + "/" + at.Format(time.RFC3339Nano)
	if got, w := o.rows(t), sortRows(want(id, "budget", "email", o.owner, o.admin)); !slices.Equal(got, w) {
		t.Fatalf("outbox = %+v\nwant %+v", got, w)
	}

	later := at.Add(domain.BudgetWindow + time.Second)
	if err := o.d.s.QueueBudgetNotification(o.ctx(t), o.factory, 8.5, 10, later); err != nil {
		t.Fatal(err)
	}

	if err := o.d.s.QueueBudgetNotification(o.ctx(t), o.other, 8.5, 10, at); err != nil {
		t.Fatal(err)
	}

	if n := o.d.count(`SELECT count(DISTINCT event_id) FROM notification_outbox WHERE org_id = $1 AND event = 'budget'`, string(o.org)); n != 3 {
		t.Errorf("%d budget events; want 3 (the first, the one after its window, the other factory's)", n)
	}
}
