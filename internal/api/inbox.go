package api

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/domain"
)

// workInbox is the inbox's built-in source (E §8.4): the items that may need a person
// (FlowStats.InboxItems) as decisions with their approval brief, blocked items with why, and
// expedite items; each factory's open andon pull; and each gate one rejection away from its
// andon policy's rework rate while the line runs. GetInbox keeps the factories the caller sees,
// and decisions only where they may decide.
type workInbox struct {
	s *server
}

// Inbox implements InboxSource.
func (w workInbox) Inbox(ctx context.Context, q InboxQuery) ([]gen.InboxEntry, error) {
	byID := make(map[domain.FactoryID]*config.Factory, len(q.Factories))
	ids := make([]domain.FactoryID, 0, len(q.Factories))

	for _, f := range q.Factories {
		id := domain.FactoryID(f.Metadata.ID)
		byID[id] = f
		ids = append(ids, id)
	}

	items, err := w.s.items.InboxItems(ctx, ids, q.Limit+1)
	if err != nil {
		return nil, err
	}

	out, err := w.itemEntries(ctx, byID, items)
	if err != nil {
		return nil, err
	}

	for _, f := range q.Factories {
		lines, err := w.lineEntries(ctx, f)
		if err != nil {
			return nil, err
		}

		out = append(out, lines...)
	}

	// The most recent, as every source returns them.
	slices.SortFunc(out, func(a, b gen.InboxEntry) int { return cmp.Or(b.Since.Compare(a.Since), cmp.Compare(a.Id, b.Id)) })

	return out[:min(len(out), q.Limit+1)], nil
}

// itemEntries turns the items that may need a person into entries, their cards built factory by
// factory.
func (w workInbox) itemEntries(ctx context.Context, byID map[domain.FactoryID]*config.Factory, items []domain.AttentionItem) ([]gen.InboxEntry, error) {
	byFactory := map[domain.FactoryID][]domain.AttentionItem{}
	for _, a := range items {
		byFactory[a.Item.FactoryID] = append(byFactory[a.Item.FactoryID], a)
	}

	out := make([]gen.InboxEntry, 0, len(items))

	for id, list := range byFactory {
		f := byID[id]
		l := f.Line()

		plain := make([]domain.Item, 0, len(list))
		for _, a := range list {
			plain = append(plain, a.Item)
		}

		cards, err := w.s.cards(ctx, f, l, "", plain) // the buffer is the board's concern
		if err != nil {
			return nil, err
		}

		for i, a := range list {
			out = append(out, itemEntry(f, l, a, cards[i]))
		}
	}

	return out, nil
}

// itemEntry is the one entry of an item: a decision, else blocked, else expedite.
func itemEntry(f *config.Factory, l domain.Line, a domain.AttentionItem, card gen.ItemCard) gen.InboxEntry {
	it := a.Item
	e := entryOf(f, gen.InboxEntryKindExpedite, string(it.ID), it.CreatedAt)
	e.Item, e.Station = &card, optional(it.Station)

	switch it.State {
	case domain.StateWaitingHuman:
		e.Kind, e.Id, e.Since = gen.InboxEntryKindDecision, string(gen.InboxEntryKindDecision)+":"+string(it.ID), it.UpdatedAt
		e.Approval = briefOf(it.Brief(l))

		if st, ok := l.Station(it.Station); ok {
			e.Action = actionOf(string(st.Action))
		}
	case domain.StateBlocked:
		e.Kind, e.Id, e.Since = gen.InboxEntryKindBlocked, string(gen.InboxEntryKindBlocked)+":"+string(it.ID), it.UpdatedAt
		e.Blocked = &gen.InboxBlocked{Reason: optional(a.BlockedReason), Paused: a.Paused}
	case domain.StateQueued, domain.StateReady, domain.StateInProgress, domain.StateDone, domain.StateCancelled:
		// An expedite item that waits on nothing else (InboxItems returns no closed item).
	}

	return e
}

// lineEntries are the entries of a factory's line: its open andon pull or, while it runs, the gates
// one rejection away from the policy's rework rate.
func (w workInbox) lineEntries(ctx context.Context, f *config.Factory) ([]gen.InboxEntry, error) {
	l := f.Line()

	open, err := w.s.items.OpenAndon(ctx, l.FactoryID)
	if err != nil {
		return nil, err
	}

	if open != nil {
		e := entryOf(f, gen.InboxEntryKindAndon, strconv.FormatInt(open.ID, 10), open.PulledAt)
		e.Station, e.Andon = optional(open.Station), andonOf(open)

		return []gen.InboxEntry{e}, nil
	}

	policy := l.Flow.Andon
	if policy == nil {
		return nil, nil
	}

	pulls, err := w.s.items.AndonPulls(ctx, l.FactoryID, 1) // none is open: the latest is the last clear
	if err != nil {
		return nil, err
	}

	var after time.Time
	if len(pulls) > 0 {
		after = pulls[0].ClearedAt
	}

	tallies, err := w.s.gateTallies(ctx, l, after)
	if err != nil {
		return nil, err
	}

	now := w.s.clock.Now()
	out := []gen.InboxEntry{}

	for _, t := range tallies {
		if !t.Warning {
			continue
		}

		e := entryOf(f, gen.InboxEntryKindReworkWarning, f.Metadata.ID+":"+t.Station, now)
		e.Station = optional(t.Station)
		e.Rework = &gen.ReworkWarning{
			Station: t.Station, Rejected: t.Rejected, Verdicts: t.Verdicts, Window: policy.Window, Threshold: policy.ReworkRate,
			TopReason: t.TopReason,
		}
		out = append(out, e)
	}

	return out, nil
}

// entryOf is an entry of kind about what (an id) of factory f, waiting since.
func entryOf(f *config.Factory, kind gen.InboxEntryKind, what string, since time.Time) gen.InboxEntry {
	e := gen.InboxEntry{Id: string(kind) + ":" + what, Kind: kind, Since: since}
	e.Factory.Id, e.Factory.Name, e.Factory.Title = f.Metadata.ID, f.Metadata.Name, optional(f.Metadata.Title)

	return e
}

func briefOf(b domain.ApprovalBrief) *gen.ApprovalBrief {
	out := &gen.ApprovalBrief{
		Summary: optional(b.Summary), PullRequest: optional(b.PullRequest), SpendUsd: b.SpendUSD,
		Facts: make([]struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}, 0, len(b.Facts)),
	}

	for _, f := range b.Facts {
		out.Facts = append(out.Facts, struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}{Key: f.Key, Value: f.Value})
	}

	return out
}
