package porttest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// FollowUps implements ports.Store.
func (s *Store) FollowUps(ctx context.Context, item domain.ItemID) ([]domain.FollowUp, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.visible(item, org); !ok {
		return nil, nil
	}

	var out []domain.FollowUp

	for _, f := range s.followUps {
		if f.ItemID == item {
			out = append(out, cloneFollowUp(f))
		}
	}

	return out, nil
}

// keepFollowUps keeps the follow-ups of a transition of it at at, skipping a text already kept
// for it, as the store's unique (item, text key) does. Callers hold s.mu.
func (s *Store) keepFollowUps(it *domain.Item, fs []domain.FollowUp, at time.Time) {
	for _, f := range fs {
		key := domain.FollowUpKey(f.Text)

		if slices.ContainsFunc(s.followUps, func(k domain.FollowUp) bool {
			return k.ItemID == it.ID && domain.FollowUpKey(k.Text) == key
		}) {
			continue
		}

		f = cloneFollowUp(f)
		f.ID, f.FactoryID, f.ItemID, f.CreatedAt = domain.FollowUpID(s.newID()), it.FactoryID, it.ID, at
		s.followUps = append(s.followUps, f)
	}
}

func cloneFollowUp(f domain.FollowUp) domain.FollowUp {
	if f.For != nil {
		ref := *f.For
		f.For = &ref
	}

	if f.Start != nil {
		p := *f.Start
		p.After, p.Repositories = slices.Clone(p.After), slices.Clone(p.Repositories)
		f.Start = &p
	}

	return f
}

// DeliverNotes implements ports.Store.
func (s *Store) DeliverNotes(ctx context.Context, factories []domain.NoteFactory, at time.Time, limit int) ([]domain.FollowUp, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	var out []domain.FollowUp

	for i := range s.followUps {
		if len(out) >= limit {
			break
		}

		f := &s.followUps[i]

		src, ok := s.visible(f.ItemID, org)
		if !ok || src.Untrusted || f.State != domain.FollowUpWaiting || f.Target != domain.FollowUpNote || f.For == nil {
			continue
		}

		s.deliverNote(org, f, src, factories, at)
		out = append(out, cloneFollowUp(*f))
	}

	return out, nil
}

// deliverNote routes one claimed note from src, as the store's transaction does. Callers hold s.mu.
func (s *Store) deliverNote(org domain.OrgID, f *domain.FollowUp, src *domain.Item, factories []domain.NoteFactory, at time.Time) {
	to, target, today := s.noteTarget(org, *f, factories, at)

	var state domain.ItemState
	if target != nil {
		state = target.State
	}

	if reason := domain.RouteNote(src.FactoryID, to, state, today); reason != "" {
		f.Reason, f.Target, f.For = domain.NoteTriageReason(*f.For, reason), domain.FollowUpTriage, nil

		return
	}

	name := string(src.FactoryID)
	if nf, ok := domain.NoteFactoryNamed(factories, name); ok && nf.Name != "" {
		name = nf.Name
	}

	s.append(domain.Event{
		FactoryID: target.FactoryID, ItemID: target.ID, At: at, Kind: domain.EventItemNote, Station: target.Station,
		Actor: domain.ActorEngine, Data: domain.NoteData(*f, domain.ItemRef{Factory: name, Number: src.Number}),
	})

	f.State = domain.FollowUpDelivered
}

// noteTarget is the factory f's for names (nil for none), its item with that number (nil for
// none) and the notes that factory took from other factories in the 24 hours before at.
// Callers hold s.mu.
func (s *Store) noteTarget(org domain.OrgID, f domain.FollowUp, factories []domain.NoteFactory, at time.Time) (*domain.NoteFactory, *domain.Item, int) {
	nf, ok := domain.NoteFactoryNamed(factories, f.For.Factory)
	if !ok {
		return nil, nil, 0
	}

	var (
		target *domain.Item
		today  int
	)

	for _, it := range s.items {
		if it.OrgID == org && it.FactoryID == nf.ID && it.Number == f.For.Number {
			target = it
		}
	}

	for _, e := range s.events {
		if e.FactoryID == nf.ID && e.Kind == domain.EventItemNote && e.At.After(at.Add(-24*time.Hour)) &&
			e.Data[domain.KeyNoteFromFactory] != string(nf.ID) {
			today++
		}
	}

	return &nf, target, today
}

// FileFollowUps implements ports.Store.
func (s *Store) FileFollowUps(ctx context.Context, lines []domain.Line, at time.Time, limit int) ([]domain.FollowUp, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()

	l := &fakeLander{s: s, org: org, lines: lines, at: at}

	var out []domain.FollowUp

	for len(out) < limit {
		root := l.nextRoot()
		if root == nil {
			break
		}

		out = append(out, l.land(root)...)
	}

	a := s.audit
	s.mu.Unlock()
	s.changes.signal()

	if err := appendAudits(ctx, a, l.audits...); err != nil {
		return out, fmt.Errorf("file follow-ups: %w", err)
	}

	return out, nil
}

// fakeLander files landed roots' follow-ups as the PostgreSQL store's lander does. Its methods
// run with s.mu held.
type fakeLander struct {
	s      *Store
	org    domain.OrgID
	lines  []domain.Line
	at     time.Time
	audits []domain.AuditEntry
}

func (l *fakeLander) line(id domain.FactoryID) (domain.Line, bool) {
	for _, ln := range l.lines {
		if ln.FactoryID == id {
			return ln, true
		}
	}

	return domain.Line{}, false
}

func (l *fakeLander) factories() []domain.NoteFactory {
	out := make([]domain.NoteFactory, 0, len(l.lines))
	for _, ln := range l.lines {
		out = append(out, domain.NoteFactoryOf(ln))
	}

	return out
}

func (l *fakeLander) ref(it *domain.Item) domain.ItemRef {
	name := string(it.FactoryID)
	if ln, ok := l.line(it.FactoryID); ok && ln.Name != "" {
		name = ln.Name
	}

	return domain.ItemRef{Factory: name, Number: it.Number}
}

// rootOf is f's root, its source or the source's assembly, when f is one the sweep takes
// (ClaimLandedFollowUp); nil otherwise.
func (l *fakeLander) rootOf(f domain.FollowUp) *domain.Item {
	if f.State != domain.FollowUpWaiting || (f.Target != domain.FollowUpStart && f.Target != domain.FollowUpTriage) {
		return nil
	}

	if _, ok := l.line(f.FactoryID); !ok {
		return nil
	}

	src, ok := l.s.visible(f.ItemID, l.org)
	if !ok {
		return nil
	}

	root := src
	if src.ParentID != "" {
		if root, ok = l.s.visible(src.ParentID, l.org); !ok {
			return nil
		}
	}

	if src.State == domain.StateCancelled || root.State == domain.StateCancelled || (root.State == domain.StateDone && !src.Untrusted) {
		return root
	}

	return nil
}

func (l *fakeLander) nextRoot() *domain.Item {
	for _, f := range l.s.followUps {
		if root := l.rootOf(f); root != nil {
			return root
		}
	}

	return nil
}

// land routes the follow-ups of root and its parts the sweep takes.
func (l *fakeLander) land(root *domain.Item) []domain.FollowUp {
	landed := root.DoneAt
	if landed.IsZero() {
		landed = l.at
	}

	var (
		out    []domain.FollowUp
		triage []int
	)

	for i := range l.s.followUps {
		f := &l.s.followUps[i]
		if r := l.rootOf(*f); r == nil || r.ID != root.ID {
			continue
		}

		src := l.s.items[f.ItemID]

		switch land := domain.LandFollowUp(*f, *src, root.State); {
		case land.Drop:
			f.State, f.Reason = domain.FollowUpDropped, land.Reason
		case land.File:
			if reason := l.fileStart(f, src, landed); reason != "" {
				f.Reason, f.Target, f.Start = domain.StartTriageReason(*f.Start, reason), domain.FollowUpTriage, nil

				triage = append(triage, i)

				continue
			}
		default:
			triage = append(triage, i)

			continue
		}

		out = append(out, cloneFollowUp(*f))
	}

	if len(triage) > 0 {
		out = append(out, l.joinTriage(root.FactoryID, triage)...)
	}

	return out
}

// fileStart files f, a start of src, or returns why it goes to triage.
func (l *fakeLander) fileStart(f *domain.FollowUp, src *domain.Item, landed time.Time) string {
	to := domain.StartTarget(l.factories(), src.FactoryID, *f.Start)
	if reason := domain.RouteStart(src.FactoryID, to); reason != "" {
		return reason
	}

	toLine, ok := l.line(to.ID)
	if !ok {
		return domain.ReasonNoteNoFactory
	}

	if dup := l.openFiledFrom(src.ID, domain.FollowUpKey(f.Start.Title)); dup != nil {
		f.State, f.FiledItem = domain.FollowUpFiled, dup.ID

		return ""
	}

	if l.filedToday(to.ID) >= domain.MaxFollowUpItemsADay {
		return domain.ReasonFollowUpCapped
	}

	n, err := domain.File(toLine, domain.FollowUpFileRequest(to.ID, *src, l.ref(src), *f, l.after(f.Start.After), landed))
	if err != nil {
		return domain.ReasonOf(err)
	}

	l.s.factories[n.FactoryID] = l.org
	it, audit := l.s.file(l.org, n, filedPart{})
	f.State, f.FiledItem = domain.FollowUpFiled, it.ID

	l.audits = append(l.audits, audit)

	return ""
}

// filedToday counts the follow-up items the engine filed into factory, triage items aside, in
// the 24 hours before l.at.
func (l *fakeLander) filedToday(factory domain.FactoryID) int {
	n := 0

	for _, it := range l.s.items {
		if it.FactoryID == factory && it.Origin == domain.OriginFollowUp && it.CreatedBy == "" && !it.Triage &&
			it.CreatedAt.After(l.at.Add(-24*time.Hour)) {
			n++
		}
	}

	return n
}

// after resolves after refs to the items they name, nil for none.
func (l *fakeLander) after(refs []string) []domain.After {
	out := make([]domain.After, 0, len(refs))

	for _, s := range refs {
		a := domain.After{Ref: s}
		ref, _ := domain.ParseItemRef(s)

		if nf, ok := domain.NoteFactoryNamed(l.factories(), ref.Factory); ok {
			for _, it := range l.s.items {
				if it.OrgID == l.org && it.FactoryID == nf.ID && it.Number == ref.Number {
					c := clone(it)
					a.Item = &c
				}
			}
		}

		out = append(out, a)
	}

	return out
}

// openFiledFrom is the oldest open item, not a triage item, already filed from src's follow-ups
// whose title has key; nil for none.
func (l *fakeLander) openFiledFrom(src domain.ItemID, key string) *domain.Item {
	var found *domain.Item

	for _, f := range l.s.followUps {
		it, ok := l.s.items[f.FiledItem]
		if f.ItemID != src || f.State != domain.FollowUpFiled || !ok || it.Triage ||
			it.State == domain.StateDone || it.State == domain.StateCancelled || domain.FollowUpKey(it.Title) != key {
			continue
		}

		if found == nil || it.CreatedAt.Before(found.CreatedAt) {
			found = it
		}
	}

	return found
}

// collecting is the factory's oldest triage item that is open and has no run; nil for none.
func (l *fakeLander) collecting(factory domain.FactoryID) *domain.Item {
	var found *domain.Item

	for _, it := range l.s.items {
		if it.OrgID != l.org || it.FactoryID != factory || !it.Triage || it.State == domain.StateDone || it.State == domain.StateCancelled {
			continue
		}

		if slices.ContainsFunc(slices.Collect(maps.Values(l.s.runs)), func(r *domain.Run) bool { return r.ItemID == it.ID }) {
			continue
		}

		if found == nil || it.CreatedAt.Before(found.CreatedAt) || (it.CreatedAt.Equal(found.CreatedAt) && it.Number < found.Number) {
			found = it
		}
	}

	return found
}

func (l *fakeLander) entry(f domain.FollowUp) domain.TriageEntry {
	src := l.s.items[f.ItemID]

	return domain.TriageEntry{
		FollowUp: cloneFollowUp(f), Source: l.ref(src), Title: src.Title, Type: src.Type, Priority: src.Priority, Generation: src.Generation,
	}
}

// joinTriage files the follow-ups at idx onto the factory's collecting triage item, filing one
// when there is none, and rewrites it.
func (l *fakeLander) joinTriage(factory domain.FactoryID, idx []int) []domain.FollowUp {
	ln, ok := l.line(factory)
	if !ok {
		return nil
	}

	tri := l.collecting(factory)
	if tri == nil {
		es := make([]domain.TriageEntry, 0, len(idx))
		for _, i := range idx {
			es = append(es, l.entry(l.s.followUps[i]))
		}

		n, err := domain.File(ln, domain.TriageFileRequest(factory, es, l.at))
		if err != nil {
			return nil
		}

		var audit domain.AuditEntry

		tri, audit = l.s.file(l.org, n, filedPart{})
		tri.Triage = true

		l.audits = append(l.audits, audit)
	}

	out := make([]domain.FollowUp, 0, len(idx))

	for _, i := range idx {
		f := &l.s.followUps[i]
		f.State, f.FiledItem = domain.FollowUpFiled, tri.ID
		l.s.joined = append(l.s.joined, f.ID)
		out = append(out, cloneFollowUp(*f))
	}

	var es []domain.TriageEntry

	for _, id := range l.s.joined {
		for _, f := range l.s.followUps {
			if f.ID == id && f.FiledItem == tri.ID && f.State == domain.FollowUpFiled && f.Target == domain.FollowUpTriage {
				es = append(es, l.entry(f))
			}
		}
	}

	tri.Title, tri.Body, tri.Priority, tri.Generation = domain.TriageTitle(es), domain.TriageBrief(es), domain.TriagePriority(es), domain.TriageGeneration(es)
	tri.UpdatedAt = l.at

	return out
}

// itemFollowUp is follow-up id of item, or ports.ErrNotFound. Callers hold s.mu.
func (s *Store) itemFollowUp(org domain.OrgID, item domain.ItemID, id domain.FollowUpID) (*domain.FollowUp, error) {
	if _, ok := s.visible(item, org); ok {
		for i := range s.followUps {
			if f := &s.followUps[i]; f.ID == id && f.ItemID == item {
				return f, nil
			}
		}
	}

	return nil, fmt.Errorf("follow-up %s of item %s: %w", id, item, ports.ErrNotFound)
}

// DropFollowUp implements ports.Store.
func (s *Store) DropFollowUp(ctx context.Context, item domain.ItemID, id domain.FollowUpID, by domain.Actor, _ time.Time) (domain.FollowUp, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.FollowUp{}, err
	}

	s.mu.Lock()
	defer s.changes.signal()
	defer s.mu.Unlock()

	f, err := s.itemFollowUp(org, item, id)
	if err != nil {
		return domain.FollowUp{}, fmt.Errorf("drop follow-up: %w", err)
	}

	if f.State != domain.FollowUpWaiting {
		return domain.FollowUp{}, fmt.Errorf("drop follow-up %s: it is %s, not waiting: %w", id, f.State, ports.ErrConflict)
	}

	f.State, f.Reason = domain.FollowUpDropped, domain.DropReason(by)

	return cloneFollowUp(*f), nil
}

// FileFollowUp implements ports.Store.
func (s *Store) FileFollowUp(ctx context.Context, item domain.ItemID, id domain.FollowUpID, n domain.NewItem) (domain.FollowUp, domain.Item, error) {
	org, err := orgOf(ctx)
	if err != nil {
		return domain.FollowUp{}, domain.Item{}, err
	}

	s.mu.Lock()

	f, err := s.itemFollowUp(org, item, id)
	if err != nil {
		s.mu.Unlock()

		return domain.FollowUp{}, domain.Item{}, fmt.Errorf("file follow-up: %w", err)
	}

	var (
		state  domain.ItemState
		triage bool
	)

	if it, ok := s.items[f.FiledItem]; ok {
		state, triage = it.State, it.Triage
	}

	if !domain.Fileable(*f, state, triage) {
		s.mu.Unlock()

		return domain.FollowUp{}, domain.Item{}, fmt.Errorf("file follow-up %s: it is %s and cannot be filed: %w", id, f.State, ports.ErrConflict)
	}

	s.factories[n.FactoryID] = org
	it, audit := s.file(org, n, filedPart{})
	f.State, f.FiledItem = domain.FollowUpFiled, it.ID
	out, filed, a := cloneFollowUp(*f), clone(it), s.audit

	s.mu.Unlock()
	s.changes.signal()

	if err := appendAudits(ctx, a, audit); err != nil {
		return domain.FollowUp{}, domain.Item{}, fmt.Errorf("file follow-up: %w", err)
	}

	return out, filed, nil
}
