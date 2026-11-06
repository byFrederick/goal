package api

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
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

	conversions, err := w.conversionEntries(ctx, q.Factories)
	if err != nil {
		return nil, err
	}

	out = append(out, conversions...)

	sources, err := w.knowledgeEntries(ctx, q.Factories)
	if err != nil {
		return nil, err
	}

	out = append(out, sources...)

	checks, err := w.checkEntries(ctx, q.Factories)
	if err != nil {
		return nil, err
	}

	out = append(out, checks...)

	worse, err := w.watchEntries(ctx, q.Factories, q.Limit+1)
	if err != nil {
		return nil, err
	}

	out = append(out, worse...)

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
			var (
				q  *gen.ItemQuestion
				ta *gen.ToolApproval
			)

			// Only an item waiting for a person may have an open question or tool approval.
			if a.Item.State == domain.StateWaitingHuman {
				if q, ta, err = w.s.openAsks(ctx, a.Item.ID); err != nil {
					return nil, err
				}
			}

			out = append(out, itemEntry(f, l, a, cards[i], q, ta))
		}
	}

	return out, nil
}

// itemEntry is the one entry of an item: its open question q, else its open tool approval ta,
// else a decision, else blocked, else expedite.
func itemEntry(f *config.Factory, l domain.Line, a domain.AttentionItem, card gen.ItemCard, q *gen.ItemQuestion, ta *gen.ToolApproval) gen.InboxEntry {
	it := a.Item
	e := entryOf(f, gen.InboxEntryKindExpedite, string(it.ID), it.CreatedAt)
	e.Item, e.Station = &card, optional(it.Station)

	if q != nil {
		e.Kind, e.Id, e.Since = gen.InboxEntryKindQuestion, string(gen.InboxEntryKindQuestion)+":"+string(it.ID), it.UpdatedAt
		e.Question = q

		return e
	}

	if ta != nil {
		e.Kind, e.Id, e.Since = gen.InboxEntryKindToolApproval, string(gen.InboxEntryKindToolApproval)+":"+string(it.ID), it.UpdatedAt
		e.ToolApproval = ta

		return e
	}

	switch it.State {
	case domain.StateWaitingHuman:
		e.Kind, e.Id, e.Since = gen.InboxEntryKindDecision, string(gen.InboxEntryKindDecision)+":"+string(it.ID), it.UpdatedAt
		e.Approval = briefOf(it.Brief(l))

		if st, ok := l.Station(it.Station); ok {
			e.Action = actionOf(string(st.Action))
		}
	case domain.StateBlocked:
		e.Kind, e.Id, e.Since = gen.InboxEntryKindBlocked, string(gen.InboxEntryKindBlocked)+":"+string(it.ID), it.UpdatedAt
		e.Blocked = &gen.InboxBlocked{Reason: optional(a.BlockedReason), Code: optional(a.BlockedCode), Paused: a.Paused}
	case domain.StateQueued, domain.StateReady, domain.StateInProgress, domain.StateDone, domain.StateCancelled, domain.StateAssembly:
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

// conversionEntries are the factories Goal could not convert to secrets and environment files
// (ADR 0037 decision 19) that still run on connections: one entry each, naming every problem. A
// factory that published a version on secrets since has none.
func (w workInbox) conversionEntries(ctx context.Context, factories []*config.Factory) ([]gen.InboxEntry, error) {
	if w.s.conversions == nil {
		return nil, nil
	}

	list, err := w.s.conversions.List(ctx)
	if err != nil {
		return nil, err
	}

	out := []gen.InboxEntry{}

	for _, c := range list {
		i := slices.IndexFunc(factories, func(f *config.Factory) bool { return domain.FactoryID(f.Metadata.ID) == c.Factory })
		if !c.Refused() || i < 0 || factories[i].Line().OnSecrets() {
			continue
		}

		e := entryOf(factories[i], gen.InboxEntryKindConversion, string(c.Factory), c.At)
		e.Conversion = &gen.InboxConversion{Reason: conversionReason(c.Problems), Problems: slices.Clone(c.Problems)}
		out = append(out, e)
	}

	return out, nil
}

// failingSyncs is how many syncs of a source fail in a row before it shows in Needs you (ADR 0038 decision 3).
const failingSyncs = 3

// knowledgeEntries are the knowledge sources whose last three syncs failed, one entry for each factory that
// lists the source: the people who run that factory are the ones who can fix what it reads.
func (w workInbox) knowledgeEntries(ctx context.Context, factories []*config.Factory) ([]gen.InboxEntry, error) {
	if w.s.knowledge == nil {
		return nil, nil
	}

	sources, err := w.s.knowledge.Sources(ctx)
	if err != nil {
		return nil, err
	}

	failing := map[string]domain.KnowledgeSource{}

	for _, src := range sources {
		if src.Status == domain.SyncFailed && src.SyncFailures >= failingSyncs {
			failing[src.Name] = src
		}
	}

	out := []gen.InboxEntry{}

	if len(failing) == 0 {
		return out, nil
	}

	for _, f := range factories {
		if f.Spec.Knowledge == nil {
			continue
		}

		for _, e := range f.Spec.Knowledge.Sources {
			src, ok := failing[e.Name]
			if !ok {
				continue
			}

			entry := entryOf(f, gen.InboxEntryKindKnowledgeSource, f.Metadata.ID+":"+src.Name, src.UpdatedAt)
			entry.KnowledgeSource = &gen.InboxKnowledgeSource{Source: src.Name, Error: src.LastError, FailedSyncs: src.SyncFailures}
			out = append(out, entry)
		}
	}

	return out, nil
}

// checkEntries are the check triggers whose checks need a person (ADR 0040 decision 19), derived
// on read from Intake.CheckAlerts: check_failing while a trigger's last three checks failed, and
// check_limit once a UTC day for a trigger whose check skipped something over the limit that day.
// Only triggers with a check in their factory's current definition show.
func (w workInbox) checkEntries(ctx context.Context, factories []*config.Factory) ([]gen.InboxEntry, error) {
	byID := map[domain.FactoryID]*config.Factory{}
	ids := []domain.FactoryID{}

	for _, f := range factories {
		if hasCheck(f) {
			byID[domain.FactoryID(f.Metadata.ID)] = f
			ids = append(ids, domain.FactoryID(f.Metadata.ID))
		}
	}

	out := []gen.InboxEntry{}

	if len(ids) == 0 || w.s.intake == nil {
		return out, nil
	}

	day := w.s.clock.Now().UTC().Truncate(24 * time.Hour)

	alerts, err := w.s.intake.CheckAlerts(ctx, ids, day)
	if err != nil {
		return nil, err
	}

	for _, a := range alerts {
		f, ok := byID[a.FactoryID]
		if !ok {
			continue
		}

		if t, ok := f.Line().Trigger(a.Trigger); !ok || t.Check == nil {
			continue
		}

		var entry gen.InboxEntry

		switch a.Kind {
		case domain.CheckFailing:
			entry = entryOf(f, gen.InboxEntryKindCheckFailing, f.Metadata.ID+":"+a.Trigger, a.At)
			entry.TriggerCheck = &gen.InboxTriggerCheck{Trigger: a.Trigger, Reason: a.Reason}
		case domain.CheckLimited:
			entry = entryOf(f, gen.InboxEntryKindCheckLimit, f.Metadata.ID+":"+a.Trigger+":"+day.Format(time.DateOnly), a.At)
			entry.TriggerCheck = &gen.InboxTriggerCheck{Trigger: a.Trigger, Reason: domain.CheckOverLimit}
		default:
			continue
		}

		out = append(out, entry)
	}

	return out, nil
}

// hasCheck reports whether f's current definition has a trigger with a check.
func hasCheck(f *config.Factory) bool {
	return slices.ContainsFunc(f.Line().Triggers, func(t domain.Trigger) bool { return t.Check != nil })
}

// conversionReason is a conversion's problems as one line: each a sentence.
func conversionReason(problems []string) string {
	parts := make([]string, 0, len(problems))
	for _, p := range problems {
		if p == "" {
			continue
		}

		parts = append(parts, strings.ToUpper(p[:1])+p[1:]+".")
	}

	return strings.Join(parts, " ")
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
