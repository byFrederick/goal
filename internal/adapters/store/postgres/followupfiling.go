package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/adapters/store/postgres/db"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// FileFollowUps implements ports.Store: one transaction per landed root, which locks the root's
// waiting follow-ups (ClaimLandedFollowUp, LandedFollowUps), then a receiving factory's cap lock
// (LockFollowUpCap), then the items it files or rewrites; it never locks a source item, so it
// cannot deadlock with a transition or a note's delivery.
func (s *Store) FileFollowUps(ctx context.Context, lines []domain.Line, at time.Time, limit int) ([]domain.FollowUp, error) {
	factories := make([]uuid.UUID, 0, len(lines))

	for _, l := range lines {
		if isUUID(string(l.FactoryID)) {
			factories = append(factories, uuid.MustParse(string(l.FactoryID)))
		}
	}

	var out []domain.FollowUp

	for len(out) < limit {
		var got []domain.FollowUp

		err := s.inTx(ctx, func(q *db.Queries) error {
			row, err := q.ClaimLandedFollowUp(ctx, factories)
			if notFound(err) {
				return nil
			}

			if err != nil {
				return fmt.Errorf("claim landed follow-up: %w", err)
			}

			rows, err := q.LandedFollowUps(ctx, row.RootID)
			if err != nil {
				return fmt.Errorf("follow-ups of root %s: %w", row.RootID, err)
			}

			l := &lander{q: q, lines: lines, at: at, items: map[uuid.UUID]domain.Item{}}
			got, err = l.land(ctx, row.RootID, rows)

			return err
		})
		if err != nil {
			return out, fmt.Errorf("file follow-ups: %w", err)
		}

		if len(got) == 0 {
			break
		}

		out = append(out, got...)
	}

	return out, nil
}

// lander files one landed root's follow-ups in q's transaction.
type lander struct {
	q     *db.Queries
	lines []domain.Line
	at    time.Time
	items map[uuid.UUID]domain.Item
}

// item is the item id, read once per transaction and never locked.
func (l *lander) item(ctx context.Context, id uuid.UUID) (domain.Item, error) {
	if it, ok := l.items[id]; ok {
		return it, nil
	}

	row, err := l.q.GetItem(ctx, id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("item %s: %w", id, err)
	}

	it, err := itemFrom(row)
	if err != nil {
		return domain.Item{}, err
	}

	l.items[id] = it

	return it, nil
}

func (l *lander) line(id domain.FactoryID) (domain.Line, bool) {
	for _, ln := range l.lines {
		if ln.FactoryID == id {
			return ln, true
		}
	}

	return domain.Line{}, false
}

func (l *lander) factories() []domain.NoteFactory {
	out := make([]domain.NoteFactory, 0, len(l.lines))
	for _, ln := range l.lines {
		out = append(out, domain.NoteFactoryOf(ln))
	}

	return out
}

// ref is it's item ref, by its factory's metadata.name when goald has it loaded.
func (l *lander) ref(it domain.Item) domain.ItemRef {
	name := string(it.FactoryID)
	if ln, ok := l.line(it.FactoryID); ok && ln.Name != "" {
		name = ln.Name
	}

	return domain.ItemRef{Factory: name, Number: it.Number}
}

// land routes rows, the waiting follow-ups of root and its parts (domain.LandFollowUp): it drops,
// files or sends each to triage, then adds those for triage to the root factory's triage item.
func (l *lander) land(ctx context.Context, rootID uuid.UUID, rows []db.FollowUp) ([]domain.FollowUp, error) {
	root, err := l.item(ctx, rootID)
	if err != nil {
		return nil, err
	}

	landed := root.DoneAt
	if landed.IsZero() {
		landed = l.at
	}

	var (
		out       []domain.FollowUp
		triage    []db.FollowUp
		untrusted []db.FollowUp
	)

	for _, r := range rows {
		if row, ok, err := l.untrusted(ctx, r, root); err != nil || ok {
			if err != nil {
				return nil, fmt.Errorf("follow-up %s: %w", r.ID, err)
			}

			untrusted = append(untrusted, row)

			continue
		}

		row, toTriage, err := l.route(ctx, r, root, landed)
		if err != nil {
			return nil, fmt.Errorf("follow-up %s: %w", r.ID, err)
		}

		if toTriage {
			triage = append(triage, row)

			continue
		}

		f, err := followUpFrom(row)
		if err != nil {
			return nil, err
		}

		out = append(out, f)
	}

	if len(untrusted) > 0 {
		own, err := l.untrustedTriage(ctx, root.FactoryID, untrusted)
		if err != nil {
			return nil, err
		}

		out = append(out, own...)
	}

	if len(triage) == 0 {
		return out, nil
	}

	joined, err := l.joinTriage(ctx, root.FactoryID, triage)

	return append(out, joined...), err
}

// untrusted turns r, when its source is untrusted (domain.LandFollowUp), into a follow-up for
// the triage item of its own, with its ref in the reason, and reports that it did.
func (l *lander) untrusted(ctx context.Context, r db.FollowUp, root domain.Item) (db.FollowUp, bool, error) {
	f, err := followUpFrom(r)
	if err != nil {
		return db.FollowUp{}, false, err
	}

	src, err := l.item(ctx, r.ItemID)
	if err != nil {
		return db.FollowUp{}, false, err
	}

	if !domain.LandFollowUp(f, src, root.State).Untrusted {
		return db.FollowUp{}, false, nil
	}

	row, err := l.q.TriageFollowUp(ctx, db.TriageFollowUpParams{ID: r.ID, Reason: optional(domain.UntrustedTriageReason(f)), At: l.at})
	if err != nil {
		return db.FollowUp{}, false, fmt.Errorf("to its own triage: %w", err)
	}

	return row, true, nil
}

// untrustedTriage files rows, the follow-ups of untrusted work that landed, onto a triage item of
// their own (domain.UntrustedTriageFileRequest), filed paused: blocked, with the engine's paused
// event whose reason is domain.ReasonUntrustedFollowUps, so a person checks it and resumes it.
func (l *lander) untrustedTriage(ctx context.Context, factory domain.FactoryID, rows []db.FollowUp) ([]domain.FollowUp, error) {
	ln, ok := l.line(factory)
	if !ok || !isUUID(string(factory)) {
		return nil, fmt.Errorf("untrusted triage of factory %s: no line: %w", factory, ports.ErrNotFound)
	}

	es, err := l.entries(ctx, rows)
	if err != nil {
		return nil, err
	}

	n, err := domain.File(ln, domain.UntrustedTriageFileRequest(factory, es, l.at))
	if err != nil {
		return nil, fmt.Errorf("file untrusted triage item: %w", err)
	}

	row, err := fileItem(ctx, l.q, filingOf(n, uuid.MustParse(string(factory))))
	if err != nil {
		return nil, err
	}

	if err := l.q.MarkTriageItem(ctx, row.ID); err != nil {
		return nil, fmt.Errorf("mark triage item: %w", err)
	}

	if row, err = l.q.PauseUntrustedTriageItem(ctx, db.PauseUntrustedTriageItemParams{ID: row.ID, At: l.at}); err != nil {
		return nil, fmt.Errorf("pause untrusted triage item: %w", err)
	}

	it, err := itemFrom(row)
	if err != nil {
		return nil, err
	}

	if err := appendEvents(ctx, l.q, []domain.Event{domain.UntrustedTriagePaused(it, l.at)}); err != nil {
		return nil, err
	}

	return l.fileOnto(ctx, rows, row.ID)
}

// entries are rows' triage entries, with their source items.
func (l *lander) entries(ctx context.Context, rows []db.FollowUp) ([]domain.TriageEntry, error) {
	es := make([]domain.TriageEntry, 0, len(rows))

	for _, r := range rows {
		f, err := followUpFrom(r)
		if err != nil {
			return nil, err
		}

		src, err := l.item(ctx, r.ItemID)
		if err != nil {
			return nil, err
		}

		es = append(es, domain.TriageEntry{
			FollowUp: f, Source: l.ref(src), Title: src.Title, Type: src.Type, Priority: src.Priority, Generation: src.Generation,
		})
	}

	return es, nil
}

// fileOnto files rows onto the item it.
func (l *lander) fileOnto(ctx context.Context, rows []db.FollowUp, it uuid.UUID) ([]domain.FollowUp, error) {
	out := make([]domain.FollowUp, 0, len(rows))

	for _, r := range rows {
		filed, err := l.q.FileFollowUpOnto(ctx, db.FileFollowUpOntoParams{ID: r.ID, FiledItemID: &it, At: l.at})
		if err != nil {
			return nil, fmt.Errorf("follow-up %s to its triage item: %w", r.ID, err)
		}

		f, err := followUpFrom(filed)
		if err != nil {
			return nil, err
		}

		out = append(out, f)
	}

	return out, nil
}

// route drops r, files it, or reports it is for triage, as it now is.
func (l *lander) route(ctx context.Context, r db.FollowUp, root domain.Item, landed time.Time) (db.FollowUp, bool, error) {
	f, err := followUpFrom(r)
	if err != nil {
		return db.FollowUp{}, false, err
	}

	src, err := l.item(ctx, r.ItemID)
	if err != nil {
		return db.FollowUp{}, false, err
	}

	land := domain.LandFollowUp(f, src, root.State)

	switch {
	case land.Drop:
		row, err := l.q.DropFollowUp(ctx, db.DropFollowUpParams{ID: r.ID, Reason: &land.Reason, At: l.at})

		return row, false, err
	case !land.File:
		return r, true, nil
	}

	row, reason, err := l.fileStart(ctx, f, src, landed)
	if err != nil || reason == "" {
		return row, false, err
	}

	row, err = l.q.TriageFollowUp(ctx, db.TriageFollowUpParams{
		ID: r.ID, Reason: optional(domain.StartTriageReason(*f.Start, reason)), At: l.at,
	})
	if err != nil {
		return db.FollowUp{}, false, fmt.Errorf("to triage: %w", err)
	}

	return row, true, nil
}

// fileStart files start follow-up f of src, which landed at landed, and returns its row filed; or
// the reason it goes to triage instead.
func (l *lander) fileStart(ctx context.Context, f domain.FollowUp, src domain.Item, landed time.Time) (db.FollowUp, string, error) {
	id := uuid.MustParse(string(f.ID))

	to := domain.StartTarget(l.factories(), src.FactoryID, *f.Start)
	if reason := domain.RouteStart(src.FactoryID, to); reason != "" {
		return db.FollowUp{}, reason, nil
	}

	toLine, ok := l.line(to.ID)
	if !ok || !isUUID(string(to.ID)) {
		return db.FollowUp{}, domain.ReasonNoteNoFactory, nil
	}

	if row, ok, err := l.dedupe(ctx, id, src, f.Start.Title); ok || err != nil {
		return row, "", err
	}

	factory := uuid.MustParse(string(to.ID))

	if capped, err := l.capped(ctx, factory); err != nil || capped {
		return db.FollowUp{}, domain.ReasonFollowUpCapped, err
	}

	after, err := l.after(ctx, f.Start.After)
	if err != nil {
		return db.FollowUp{}, "", err
	}

	n, err := domain.File(toLine, domain.FollowUpFileRequest(to.ID, src, l.ref(src), f, after, landed))
	if err != nil {
		return db.FollowUp{}, domain.ReasonOf(err), nil
	}

	row, err := fileItem(ctx, l.q, filingOf(n, factory))
	if err != nil {
		return db.FollowUp{}, "", err
	}

	filed, err := l.q.FileFollowUpOnto(ctx, db.FileFollowUpOntoParams{ID: id, FiledItemID: &row.ID, At: l.at})

	return filed, "", err
}

// dedupe files follow-up id onto an open item already filed from src with title, and reports
// whether there was one.
func (l *lander) dedupe(ctx context.Context, id uuid.UUID, src domain.Item, title string) (db.FollowUp, bool, error) {
	dup, err := l.q.OpenItemFiledFrom(ctx, db.OpenItemFiledFromParams{ItemID: uuid.MustParse(string(src.ID)), Key: domain.FollowUpKey(title)})
	if notFound(err) {
		return db.FollowUp{}, false, nil
	}

	if err != nil {
		return db.FollowUp{}, false, fmt.Errorf("open item filed from %s: %w", src.ID, err)
	}

	row, err := l.q.FileFollowUpOnto(ctx, db.FileFollowUpOntoParams{ID: id, FiledItemID: &dup.ID, At: l.at})

	return row, true, err
}

// capped takes factory's cap lock and reports whether it already took MaxFollowUpItemsADay
// follow-up items in the 24 hours before l.at.
func (l *lander) capped(ctx context.Context, factory uuid.UUID) (bool, error) {
	if err := l.q.LockFollowUpCap(ctx, factory); err != nil {
		return false, fmt.Errorf("lock follow-up cap: %w", err)
	}

	today, err := l.q.FollowUpItemsSince(ctx, db.FollowUpItemsSinceParams{FactoryID: factory, Since: l.at.Add(-24 * time.Hour)})
	if err != nil {
		return false, fmt.Errorf("count follow-up items: %w", err)
	}

	return today >= domain.MaxFollowUpItemsADay, nil
}

// after resolves a start's after refs to the items they name, nil for none, which domain.File
// refuses with the ref.
func (l *lander) after(ctx context.Context, refs []string) ([]domain.After, error) {
	out := make([]domain.After, 0, len(refs))

	for _, s := range refs {
		a := domain.After{Ref: s}

		ref, _ := domain.ParseItemRef(s) // checked when it was kept
		if nf, ok := domain.NoteFactoryNamed(l.factories(), ref.Factory); ok && isUUID(string(nf.ID)) {
			row, err := l.q.GetItemByNumber(ctx, db.GetItemByNumberParams{FactoryID: uuid.MustParse(string(nf.ID)), Number: ref.Number})
			if err != nil && !notFound(err) {
				return nil, fmt.Errorf("after %s: %w", s, err)
			}

			if err == nil {
				it, err := itemFrom(row)
				if err != nil {
					return nil, err
				}

				a.Item = &it
			}
		}

		out = append(out, a)
	}

	return out, nil
}

// joinTriage files rows, follow-ups for triage, onto the collecting triage item of factory, filing
// one when there is none, and rewrites its title, brief, priority and generation from all of its
// follow-ups, under the factory's lock.
func (l *lander) joinTriage(ctx context.Context, factory domain.FactoryID, rows []db.FollowUp) ([]domain.FollowUp, error) {
	ln, ok := l.line(factory)
	if !ok || !isUUID(string(factory)) {
		return nil, fmt.Errorf("triage of factory %s: no line: %w", factory, ports.ErrNotFound)
	}

	fid := uuid.MustParse(string(factory))
	if err := l.q.LockFollowUpCap(ctx, fid); err != nil {
		return nil, fmt.Errorf("lock follow-up cap: %w", err)
	}

	tri, err := l.q.CollectingTriageItem(ctx, fid)
	if notFound(err) {
		tri, err = l.fileTriage(ctx, ln, fid, rows)
	}

	if err != nil {
		return nil, fmt.Errorf("triage item of factory %s: %w", factory, err)
	}

	out := make([]domain.FollowUp, 0, len(rows))

	for _, r := range rows {
		row, err := l.q.FileFollowUpOnto(ctx, db.FileFollowUpOntoParams{ID: r.ID, FiledItemID: &tri.ID, At: l.at})
		if err != nil {
			return nil, fmt.Errorf("follow-up %s to triage item: %w", r.ID, err)
		}

		f, err := followUpFrom(row)
		if err != nil {
			return nil, err
		}

		out = append(out, f)
	}

	joined, err := l.q.TriageFollowUps(ctx, &tri.ID)
	if err != nil {
		return nil, fmt.Errorf("follow-ups of triage item %s: %w", tri.ID, err)
	}

	es := make([]domain.TriageEntry, 0, len(joined))

	for _, j := range joined {
		f, err := followUpFrom(j.FollowUp)
		if err != nil {
			return nil, err
		}

		es = append(es, domain.TriageEntry{
			FollowUp: f, Source: domain.ItemRef{Factory: cmpName(ln), Number: j.SourceNumber}, Title: j.SourceTitle,
			Type: j.SourceType, Priority: int(j.SourcePriority), Generation: int(j.SourceGeneration),
		})
	}

	if _, err := l.q.RewriteTriageItem(ctx, db.RewriteTriageItemParams{
		ID: tri.ID, Title: domain.TriageTitle(es), Body: domain.TriageBrief(es), Priority: int64(domain.TriagePriority(es)),
		Generation: int32(min(max(domain.TriageGeneration(es), 0), maxInt32)), At: l.at,
	}); err != nil {
		return nil, fmt.Errorf("rewrite triage item %s: %w", tri.ID, err)
	}

	return out, nil
}

// fileTriage files a new triage item on ln for rows (domain.TriageFileRequest) and marks it.
func (l *lander) fileTriage(ctx context.Context, ln domain.Line, factory uuid.UUID, rows []db.FollowUp) (db.WorkItem, error) {
	es := make([]domain.TriageEntry, 0, len(rows))

	for _, r := range rows {
		f, err := followUpFrom(r)
		if err != nil {
			return db.WorkItem{}, err
		}

		src, err := l.item(ctx, r.ItemID)
		if err != nil {
			return db.WorkItem{}, err
		}

		es = append(es, domain.TriageEntry{
			FollowUp: f, Source: l.ref(src), Title: src.Title, Type: src.Type, Priority: src.Priority, Generation: src.Generation,
		})
	}

	n, err := domain.File(ln, domain.TriageFileRequest(ln.FactoryID, es, l.at))
	if err != nil {
		return db.WorkItem{}, fmt.Errorf("file triage item: %w", err)
	}

	row, err := fileItem(ctx, l.q, filingOf(n, factory))
	if err != nil {
		return db.WorkItem{}, err
	}

	if err := l.q.MarkTriageItem(ctx, row.ID); err != nil {
		return db.WorkItem{}, fmt.Errorf("mark triage item: %w", err)
	}

	row.Triage = true

	return row, nil
}

// cmpName is ln's metadata.name, or its id when it has none.
func cmpName(ln domain.Line) string {
	if ln.Name != "" {
		return ln.Name
	}

	return string(ln.FactoryID)
}

// filingOf is n, checked by domain.File, for fileItem into factory, with its after items.
func filingOf(n domain.NewItem, factory uuid.UUID) filing {
	f := filing{n: n, factory: factory, afterBy: n.FiledBy()}

	for _, a := range n.After {
		if isUUID(string(a)) {
			f.after = append(f.after, uuid.MustParse(string(a)))
		}
	}

	return f
}

// DropFollowUp implements ports.Store.
func (s *Store) DropFollowUp(ctx context.Context, item domain.ItemID, id domain.FollowUpID, by domain.Actor, at time.Time) (domain.FollowUp, error) {
	var out domain.FollowUp

	err := s.inTx(ctx, func(q *db.Queries) error {
		row, err := lockItemFollowUp(ctx, q, item, id)
		if err != nil {
			return err
		}

		if row.FollowUp.State != string(domain.FollowUpWaiting) {
			return fmt.Errorf("follow-up %s is %s, not waiting: %w", id, row.FollowUp.State, ports.ErrConflict)
		}

		reason := domain.DropReason(by)

		r, err := q.DropFollowUp(ctx, db.DropFollowUpParams{ID: row.FollowUp.ID, Reason: &reason, At: at})
		if err != nil {
			return fmt.Errorf("drop: %w", err)
		}

		out, err = followUpFrom(r)

		return err
	})
	if err != nil {
		return domain.FollowUp{}, fmt.Errorf("drop follow-up %s: %w", id, err)
	}

	return out, nil
}

// FileFollowUp implements ports.Store.
func (s *Store) FileFollowUp(ctx context.Context, item domain.ItemID, id domain.FollowUpID, n domain.NewItem) (domain.FollowUp, domain.Item, error) {
	factory, err := parseID("factory", string(n.FactoryID))
	if err != nil {
		return domain.FollowUp{}, domain.Item{}, err
	}

	var (
		f  domain.FollowUp
		it domain.Item
	)

	err = s.inTx(ctx, func(q *db.Queries) error {
		row, err := lockItemFollowUp(ctx, q, item, id)
		if err != nil {
			return err
		}

		if f, err = followUpFrom(row.FollowUp); err != nil {
			return err
		}

		if !domain.Fileable(f, domain.ItemState(row.FiledState), row.FiledTriage) {
			return fmt.Errorf("follow-up %s is %s and cannot be filed: %w", id, f.State, ports.ErrConflict)
		}

		filed, err := fileItem(ctx, q, filingOf(n, factory))
		if err != nil {
			return err
		}

		r, err := q.FileFollowUpOnto(ctx, db.FileFollowUpOntoParams{ID: row.FollowUp.ID, FiledItemID: &filed.ID, At: n.CreatedAt})
		if err != nil {
			return fmt.Errorf("filed: %w", err)
		}

		if f, err = followUpFrom(r); err != nil {
			return err
		}

		it, err = itemFrom(filed)

		return err
	})
	if err != nil {
		return domain.FollowUp{}, domain.Item{}, fmt.Errorf("file follow-up %s: %w", id, err)
	}

	return f, it, nil
}

// lockItemFollowUp locks follow-up id of item; one that is not item's is ports.ErrNotFound.
func lockItemFollowUp(ctx context.Context, q *db.Queries, item domain.ItemID, id domain.FollowUpID) (db.LockItemFollowUpRow, error) {
	if !isUUID(string(item)) || !isUUID(string(id)) {
		return db.LockItemFollowUpRow{}, fmt.Errorf("follow-up %s of item %s: %w", id, item, ports.ErrNotFound)
	}

	row, err := q.LockItemFollowUp(ctx, db.LockItemFollowUpParams{ID: uuid.MustParse(string(id)), ItemID: uuid.MustParse(string(item))})
	if notFound(err) {
		return db.LockItemFollowUpRow{}, fmt.Errorf("follow-up %s of item %s: %w", id, item, ports.ErrNotFound)
	}

	if err != nil {
		return db.LockItemFollowUpRow{}, fmt.Errorf("lock follow-up %s: %w", id, err)
	}

	return row, nil
}
