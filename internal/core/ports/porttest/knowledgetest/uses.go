package knowledgetest

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Knowledge use and verdicts (ADR 0027): one use per run, document and kind, one verdict per
// actor, document and run, a source's usage over a window, and both kept to their organization
// and deleted with their document.

var investigator = domain.Actor{Kind: domain.ActorAgent, ID: "investigator", Name: "investigator"}

// aRun is a new run of a new item and factory.
type aRun struct {
	factory domain.FactoryID
	item    domain.ItemID
	run     domain.RunID
}

func newRun() aRun {
	return aRun{
		factory: domain.FactoryID(uuid.NewString()), item: domain.ItemID(uuid.NewString()),
		run: domain.RunID(uuid.Must(uuid.NewV7()).String()),
	}
}

func (r aRun) use(d domain.KnowledgeDocument, kind domain.KnowledgeUseKind, chunks []int64, at time.Time) domain.KnowledgeUse {
	return domain.KnowledgeUse{
		Factory: r.factory, Item: r.item, Run: r.run, Source: d.Source, Document: d.ID, Kind: kind, Chunks: chunks, At: at,
	}
}

// record records u in org.
func (f *fixture) record(org domain.OrgID, u domain.KnowledgeUse) {
	f.t.Helper()

	if err := f.Knowledge.RecordUse(f.in(org), u); err != nil {
		f.t.Fatalf("RecordUse %s %s: %v", u.Document, u.Kind, err)
	}
}

func (f *fixture) verdict(org domain.OrgID, v domain.KnowledgeVerdict) domain.KnowledgeVerdict {
	f.t.Helper()

	got, err := f.Knowledge.GiveVerdict(f.in(org), v)
	if err != nil {
		f.t.Fatalf("GiveVerdict %s by %s: %v", v.Document, v.By.Ref(), err)
	}

	return got
}

func (f *fixture) runUses(org domain.OrgID, run domain.RunID) []domain.KnowledgeUse {
	f.t.Helper()

	got, err := f.Knowledge.RunUses(f.in(org), run)
	f.must(err)

	return got
}

func (f *fixture) usage(org domain.OrgID, source string, days int) []domain.DocumentUsage {
	f.t.Helper()

	got, err := f.Knowledge.SourceUsage(f.in(org), source, days)
	f.must(err)

	return got
}

func (f *fixture) docUses(d domain.KnowledgeDocumentID, after domain.RunID, limit int) []domain.RunID {
	f.t.Helper()

	got, err := f.Knowledge.DocumentUses(f.ctx(), d, after, limit)
	f.must(err)

	out := make([]domain.RunID, 0, len(got))
	for _, u := range got {
		out = append(out, u.Run)
	}

	return out
}

// twoDocs gives runbooks (synced) docs a and b.
func (f *fixture) twoDocs(org domain.OrgID) (src domain.KnowledgeSource, a, b domain.KnowledgeDocument) {
	f.t.Helper()

	src = f.synced(org, runbooks)
	f.put(org, src.ID, docA, "v1", []string{"apple"}, nil)
	f.put(org, src.ID, docB, "v1", []string{"banana"}, nil)

	if org == f.org {
		return src, f.docNamed(runbooks, docA), f.docNamed(runbooks, docB)
	}

	return src, domain.KnowledgeDocument{}, domain.KnowledgeDocument{}
}

const docB = "docs/b.md"

// testUsesUpsert: recording the same run, document and kind again merges the chunk ids and keeps
// the first time; a document's runs are paged newest first.
func testUsesUpsert(f *fixture) {
	ctx := f.ctx()
	_, a, b := f.twoDocs(f.org)
	t0 := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	r := newRun()

	f.record(f.org, r.use(a, domain.KnowledgeSearched, []int64{3, 1}, t0))
	f.record(f.org, r.use(a, domain.KnowledgeSearched, []int64{2, 3}, t0.Add(time.Minute)))
	f.record(f.org, r.use(a, domain.KnowledgeSearched, []int64{2, 3}, t0.Add(time.Minute)))
	f.record(f.org, r.use(a, domain.KnowledgeRead, nil, t0.Add(2*time.Minute)))
	f.record(f.org, r.use(b, domain.KnowledgeSearched, []int64{7}, t0.Add(3*time.Minute)))

	got := f.runUses(f.org, r.run)
	f.want("RunUses (a search, a read, b search)", got, len(got) == 3)

	if len(got) == 3 {
		s := got[0]
		f.want("the merged search of a", s, s.Document == a.ID, s.Source == a.Source, s.Kind == domain.KnowledgeSearched,
			slices.Equal(s.Chunks, []int64{1, 2, 3}), s.At.Equal(t0), s.Factory == r.factory, s.Item == r.item, s.Run == r.run)
		f.want("the read of a", got[1], got[1].Document == a.ID, got[1].Kind == domain.KnowledgeRead, len(got[1].Chunks) == 0,
			got[1].At.Equal(t0.Add(2*time.Minute)))
		f.want("the search of b", got[2], got[2].Document == b.ID, slices.Equal(got[2].Chunks, []int64{7}))
	}

	none := f.runUses(f.org, newRun().run)
	f.want("RunUses of another run", none, len(none) == 0)

	r2, r3 := newRun(), newRun()
	f.record(f.org, r2.use(a, domain.KnowledgeRead, nil, t0.Add(5*time.Minute)))
	f.record(f.org, r3.use(a, domain.KnowledgeSearched, []int64{1}, t0.Add(4*time.Minute)))

	all, err := f.Knowledge.DocumentUses(ctx, a.ID, "", 10)
	f.must(err)
	f.want("DocumentUses, newest first", all, len(all) == 3)

	if len(all) == 3 {
		f.want("the newest run of a", all[0], all[0].Run == r2.run, all[0].Item == r2.item, all[0].Factory == r2.factory,
			all[0].At.Equal(t0.Add(5*time.Minute)), slices.Equal(all[0].Kinds, []domain.KnowledgeUseKind{domain.KnowledgeRead}))
		f.want("the first run of a, both kinds at its first use", all[2], all[2].Run == r.run, all[2].At.Equal(t0),
			slices.Equal(all[2].Kinds, []domain.KnowledgeUseKind{domain.KnowledgeRead, domain.KnowledgeSearched}))
	}

	page := f.docUses(a.ID, "", 2)
	f.want("the first page", page, slices.Equal(page, []domain.RunID{r2.run, r3.run}))
	page = f.docUses(a.ID, r3.run, 2)
	f.want("the next page", page, slices.Equal(page, []domain.RunID{r.run}))

	_, err = f.Knowledge.DocumentUses(ctx, "01900000-0000-7000-8000-000000000000", "", 10)
	f.refused("DocumentUses of an unknown document", err, ports.ErrNotFound)

	unknown := r.use(a, domain.KnowledgeRead, nil, t0)
	unknown.Document = "01900000-0000-7000-8000-000000000000"
	f.refused("RecordUse of an unknown document", f.Knowledge.RecordUse(ctx, unknown), ports.ErrNotFound)

	other := f.upload(handbook)
	wrong := r.use(a, domain.KnowledgeRead, nil, t0)
	wrong.Source = other.ID
	f.refused("RecordUse of a document under another source", f.Knowledge.RecordUse(ctx, wrong), ports.ErrNotFound)

	f.refused("RecordUse of an unknown kind", f.Knowledge.RecordUse(ctx, r.use(a, "browse", nil, t0)), domain.ErrInvalid)
}

// testVerdictsReplace: one verdict per actor, document and run (none counting as its own run),
// the latest's verdict, note and time.
func testVerdictsReplace(f *fixture) {
	ctx := f.ctx()
	src, a, _ := f.twoDocs(f.org)
	t0 := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	r, r2 := newRun(), newRun()

	first := f.verdict(f.org, domain.KnowledgeVerdict{
		Document: a.ID, Run: r.run, Item: r.item, Verdict: domain.KnowledgeHelped, By: investigator, At: t0,
	})
	f.want("the first verdict", first, first.Source == src.ID, first.At.Equal(t0), first.Verdict == domain.KnowledgeHelped,
		first.By.Ref() == investigator.Ref(), first.Run == r.run, first.Item == r.item)

	later := f.verdict(f.org, domain.KnowledgeVerdict{
		Document: a.ID, Run: r.run, Item: r.item, Verdict: domain.KnowledgeMisled, Note: "names the old port", By: investigator,
		At: t0.Add(time.Minute),
	})
	f.want("the later verdict", later, later.Verdict == domain.KnowledgeMisled, later.Note == "names the old port",
		later.At.Equal(t0.Add(time.Minute)), later.Source == src.ID)

	f.verdict(f.org, domain.KnowledgeVerdict{Document: a.ID, Run: r2.run, Item: r2.item, Verdict: domain.KnowledgeHelped, By: investigator, At: t0})
	f.verdict(f.org, domain.KnowledgeVerdict{Document: a.ID, Verdict: domain.KnowledgeHelped, By: admin, At: t0})

	now := f.verdict(f.org, domain.KnowledgeVerdict{Document: a.ID, Verdict: domain.KnowledgeMisled, By: admin})
	f.want("a verdict without a time is the store's now", now.At,
		now.At.After(time.Now().Add(-time.Minute)), now.At.Before(time.Now().Add(time.Minute)))

	got := f.usage(f.org, runbooks, domain.DefaultUsageDays)
	f.want("SourceUsage: one document, the agent's two runs and the person's one verdict", got, len(got) == 1)

	if len(got) == 1 {
		u := got[0]
		f.want("a's usage", u, u.Document == a.ID, u.Title == a.Title, u.Runs == 0, u.AgentsHelped == 1, u.AgentsMisled == 1,
			u.PeopleHelped == 0, u.PeopleMisled == 1)
	}

	_, err := f.Knowledge.GiveVerdict(ctx, domain.KnowledgeVerdict{
		Document: a.ID, Verdict: domain.KnowledgeHelped, Note: strings.Repeat("x", domain.MaxVerdictNote+1), By: admin,
	})
	f.refused("GiveVerdict with a note too long", err, domain.ErrInvalid)

	_, err = f.Knowledge.GiveVerdict(ctx, domain.KnowledgeVerdict{Document: a.ID, Verdict: "meh", By: admin})
	f.refused("GiveVerdict of an unknown verdict", err, domain.ErrInvalid)

	_, err = f.Knowledge.GiveVerdict(ctx, domain.KnowledgeVerdict{
		Document: "01900000-0000-7000-8000-000000000000", Verdict: domain.KnowledgeHelped, By: admin,
	})
	f.refused("GiveVerdict on an unknown document", err, ports.ErrNotFound)
}

// testUsageWindow: a source's usage counts the distinct runs with a use, and the verdicts given,
// inside the window only, of its own documents only.
func testUsageWindow(f *fixture) {
	ctx := f.ctx()
	_, a, b := f.twoDocs(f.org)
	hb := f.synced(f.org, handbook)
	f.put(f.org, hb.ID, docA, "v1", []string{"gamma"}, nil)
	c := f.docNamed(handbook, docA)

	now := time.Now().UTC()
	day := 24 * time.Hour
	r1, r2, r3 := newRun(), newRun(), newRun()

	f.record(f.org, r1.use(a, domain.KnowledgeSearched, []int64{1}, now.Add(-10*day)))
	f.record(f.org, r2.use(a, domain.KnowledgeSearched, []int64{1}, now.Add(-day)))
	f.record(f.org, r2.use(a, domain.KnowledgeRead, nil, now.Add(-day)))
	f.record(f.org, r3.use(a, domain.KnowledgeRead, nil, now.Add(-2*time.Hour)))
	f.record(f.org, r1.use(b, domain.KnowledgeRead, nil, now.Add(-2*day)))
	f.record(f.org, r1.use(c, domain.KnowledgeRead, nil, now.Add(-time.Hour)))

	f.verdict(f.org, domain.KnowledgeVerdict{Document: b.ID, Run: r1.run, Verdict: domain.KnowledgeHelped, By: investigator, At: now.Add(-10 * day)})
	f.verdict(f.org, domain.KnowledgeVerdict{Document: b.ID, Verdict: domain.KnowledgeHelped, By: admin, At: now.Add(-time.Hour)})

	week := f.usage(f.org, runbooks, 7)
	f.want("SourceUsage over 7 days (a in 2 runs, then b in 1)", week, len(week) == 2)

	if len(week) == 2 {
		f.want("a over 7 days", week[0], week[0].Document == a.ID, week[0].Runs == 2, week[0].AgentsHelped == 0)
		f.want("b over 7 days", week[1], week[1].Document == b.ID, week[1].Runs == 1, week[1].PeopleHelped == 1,
			week[1].AgentsHelped == 0)
	}

	month := f.usage(f.org, runbooks, 30)
	f.want("SourceUsage over 30 days", month, len(month) == 2)

	if len(month) == 2 {
		f.want("a over 30 days", month[0], month[0].Document == a.ID, month[0].Runs == 3)
		f.want("b over 30 days", month[1], month[1].Document == b.ID, month[1].Runs == 1, month[1].AgentsHelped == 1,
			month[1].PeopleHelped == 1)
	}

	other := f.usage(f.org, handbook, 7)
	f.want("SourceUsage of the other source (c only)", other, len(other) == 1, len(other) == 1 && other[0].Document == c.ID)

	for _, days := range []int{0, domain.MaxUsageDays + 1} {
		_, err := f.Knowledge.SourceUsage(ctx, runbooks, days)
		f.refused("SourceUsage over a window out of bounds", err, domain.ErrInvalid)
	}

	_, err := f.Knowledge.SourceUsage(ctx, "nope", 7)
	f.refused("SourceUsage of an unknown source", err, ports.ErrNotFound)
}

// testUsesIsolation: another organization sees none of the uses and verdicts and cannot write
// any on the first one's documents.
func testUsesIsolation(f *fixture) {
	_, a, _ := f.twoDocs(f.org)
	r := newRun()
	t0 := time.Now().UTC().Add(-time.Hour)

	f.record(f.org, r.use(a, domain.KnowledgeRead, nil, t0))
	f.verdict(f.org, domain.KnowledgeVerdict{Document: a.ID, Run: r.run, Verdict: domain.KnowledgeHelped, By: investigator})

	other := f.NewOrg(f.t)
	f.setUp(other)
	in := f.in(other)

	none := f.runUses(other, r.run)
	f.want("RunUses in another org", none, len(none) == 0)

	_, err := f.Knowledge.DocumentUses(in, a.ID, "", 10)
	f.refused("DocumentUses of another org's document", err, ports.ErrNotFound)

	_, err = f.Knowledge.SourceUsage(in, runbooks, 7)
	f.refused("SourceUsage of another org's source", err, ports.ErrNotFound)

	f.refused("RecordUse of another org's document", f.Knowledge.RecordUse(in, r.use(a, domain.KnowledgeSearched, []int64{1}, t0)),
		ports.ErrNotFound)

	_, err = f.Knowledge.GiveVerdict(in, domain.KnowledgeVerdict{Document: a.ID, Verdict: domain.KnowledgeMisled, By: admin})
	f.refused("GiveVerdict on another org's document", err, ports.ErrNotFound)

	f.twoDocs(other)

	mine := f.usage(other, runbooks, 7)
	f.want("SourceUsage of the other org's own source", mine, len(mine) == 0)

	got := f.usage(f.org, runbooks, 7)
	f.want("SourceUsage in the first org, unchanged", got, len(got) == 1,
		len(got) == 1 && got[0].Runs == 1 && got[0].AgentsHelped == 1 && got[0].PeopleMisled == 0)
	mineUses := f.runUses(f.org, r.run)
	f.want("RunUses in the first org, unchanged", mineUses, len(mineUses) == 1)
}

// testDeleteCascades: deleting a document, or its source, deletes its uses and verdicts.
func testDeleteCascades(f *fixture) {
	ctx := f.ctx()
	_, a, _ := f.twoDocs(f.org)
	f.upload(handbook)
	up := f.add(handbook, domain.BlobHash(strings.Repeat("c", 64)))
	r := newRun()
	t0 := time.Now().UTC().Add(-time.Hour)

	f.record(f.org, r.use(a, domain.KnowledgeRead, nil, t0))
	f.record(f.org, r.use(up, domain.KnowledgeRead, nil, t0))
	f.verdict(f.org, domain.KnowledgeVerdict{Document: up.ID, Run: r.run, Verdict: domain.KnowledgeMisled, By: investigator})
	f.verdict(f.org, domain.KnowledgeVerdict{Document: a.ID, Verdict: domain.KnowledgeHelped, By: admin})

	f.must(f.Knowledge.DeleteDocument(ctx, up.ID, admin, nil))

	got := f.runUses(f.org, r.run)
	f.want("RunUses after the upload's deletion (a's only)", got, len(got) == 1, len(got) == 1 && got[0].Document == a.ID)

	left := f.usage(f.org, handbook, 7)
	f.want("SourceUsage of the upload source after the deletion", left, len(left) == 0)

	f.must(f.Knowledge.DeleteSource(ctx, runbooks, admin, nil))
	got = f.runUses(f.org, r.run)
	f.want("RunUses after the source's deletion", got, len(got) == 0)

	// The document is gone, so a verdict on it now is refused, not stored.
	_, err := f.Knowledge.GiveVerdict(ctx, domain.KnowledgeVerdict{Document: a.ID, Verdict: domain.KnowledgeHelped, By: admin})
	f.refused("GiveVerdict on a deleted document", err, ports.ErrNotFound)
}
