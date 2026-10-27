package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// Knowledge use and verdicts (ADR 0027) with 00071's rules: one use per run, document and kind,
// one verdict per actor, document and run, both kept on their document so its deletion, or its
// source's, takes them, and a source's window counted from now.

// RecordUse implements ports.Knowledge.
func (k *Knowledge) RecordUse(ctx context.Context, u domain.KnowledgeUse) error {
	if err := u.Check(); err != nil {
		return fmt.Errorf("record knowledge use: %w", err)
	}

	return k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		d, ok := o.docs[u.Document]
		if !ok || d.doc.Source != u.Source {
			return fmt.Errorf("record knowledge use of %s: %w", u.Document, ports.ErrNotFound)
		}

		if d.uses == nil {
			d.uses = map[useKey]domain.KnowledgeUse{}
		}

		u.At = u.At.UTC()
		key := useKey{run: u.Run, kind: u.Kind}

		if old, ok := d.uses[key]; ok {
			u.Chunks = append(slices.Clone(old.Chunks), u.Chunks...)
			u.At = earlier(old.At, u.At)
			u.Factory, u.Item = old.Factory, old.Item
		}

		u.Chunks = slices.Compact(slices.Sorted(slices.Values(u.Chunks)))
		if len(u.Chunks) > domain.MaxUseChunks {
			return fmt.Errorf("record knowledge use of %s: %d chunks: %w", u.Document, len(u.Chunks), domain.ErrInvalid)
		}

		d.uses[key] = u

		return nil
	})
}

func earlier(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}

	return a
}

// GiveVerdict implements ports.Knowledge.
func (k *Knowledge) GiveVerdict(ctx context.Context, v domain.KnowledgeVerdict) (domain.KnowledgeVerdict, error) {
	if err := v.Check(); err != nil {
		return domain.KnowledgeVerdict{}, fmt.Errorf("give knowledge verdict: %w", err)
	}

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		d, ok := o.docs[v.Document]
		if !ok {
			return fmt.Errorf("give knowledge verdict on %s: %w", v.Document, ports.ErrNotFound)
		}

		if d.verdicts == nil {
			d.verdicts = map[verdictKey]domain.KnowledgeVerdict{}
		}

		v.Source = d.doc.Source
		v.At = cmp.Or(v.At, time.Now()).UTC()
		d.verdicts[verdictKey{actor: v.By.Ref(), run: v.Run}] = v

		return nil
	})
	if err != nil {
		return domain.KnowledgeVerdict{}, err
	}

	return v, nil
}

// SourceUsage implements ports.Knowledge.
func (k *Knowledge) SourceUsage(ctx context.Context, source string, days int) ([]domain.DocumentUsage, error) {
	if err := domain.CheckUsageDays(days); err != nil {
		return nil, fmt.Errorf("knowledge usage: %w", err)
	}

	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)

	var out []domain.DocumentUsage

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		s, ok := o.sources[source]
		if !ok {
			return fmt.Errorf("knowledge source %q: %w", source, ports.ErrNotFound)
		}

		for _, d := range o.docs {
			if d.doc.Source != s.src.ID {
				continue
			}

			if u, ok := d.usage(since); ok {
				out = append(out, u)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(out, func(a, b domain.DocumentUsage) int {
		return cmp.Or(cmp.Compare(b.Runs, a.Runs), cmp.Compare(a.Document, b.Document))
	})

	return out[:min(len(out), domain.MaxUsageDocuments)], nil
}

// usage is d's use since since; false when it has neither a use nor a verdict then.
func (d *fakeDocument) usage(since time.Time) (domain.DocumentUsage, bool) {
	u := domain.DocumentUsage{Document: d.doc.ID, Title: d.doc.Title}
	runs := map[domain.RunID]bool{}
	seen := false

	for _, x := range d.uses {
		if !x.At.Before(since) {
			runs[x.Run], seen = true, true
		}
	}

	for _, v := range d.verdicts {
		if v.At.Before(since) {
			continue
		}

		seen = true

		switch {
		case v.By.Kind == domain.ActorAgent && v.Verdict == domain.KnowledgeHelped:
			u.AgentsHelped++
		case v.By.Kind == domain.ActorAgent:
			u.AgentsMisled++
		case v.Verdict == domain.KnowledgeHelped:
			u.PeopleHelped++
		default:
			u.PeopleMisled++
		}
	}

	u.Runs = len(runs)

	return u, seen
}

// DocumentUses implements ports.Knowledge.
func (k *Knowledge) DocumentUses(ctx context.Context, document domain.KnowledgeDocumentID, after domain.RunID, limit int) ([]domain.KnowledgeRunUse, error) {
	var runs []domain.KnowledgeRunUse

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		d, ok := o.docs[document]
		if !ok {
			return fmt.Errorf("knowledge document %s: %w", document, ports.ErrNotFound)
		}

		byRun := map[domain.RunID]*domain.KnowledgeRunUse{}

		for _, u := range d.uses {
			r, ok := byRun[u.Run]
			if !ok {
				r = &domain.KnowledgeRunUse{Run: u.Run, Item: u.Item, Factory: u.Factory, At: u.At}
				byRun[u.Run] = r
			}

			r.Kinds = append(r.Kinds, u.Kind)
			r.At = earlier(r.At, u.At)
		}

		for _, r := range byRun {
			slices.Sort(r.Kinds)
			runs = append(runs, *r)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	newest := func(a, b domain.KnowledgeRunUse) int { return cmp.Or(b.At.Compare(a.At), cmp.Compare(b.Run, a.Run)) }
	slices.SortFunc(runs, newest)

	if after != "" {
		i := slices.IndexFunc(runs, func(r domain.KnowledgeRunUse) bool { return r.Run == after })
		if i < 0 {
			return nil, nil
		}

		runs = runs[i+1:]
	}

	return runs[:min(len(runs), max(limit, 0), maxUsesPage)], nil
}

// maxUsesPage bounds one page of a document's runs, as the postgres adapter does.
const maxUsesPage = 1000

// RunUses implements ports.Knowledge.
func (k *Knowledge) RunUses(ctx context.Context, run domain.RunID) ([]domain.KnowledgeUse, error) {
	var out []domain.KnowledgeUse

	err := k.locked(ctx, func(_ domain.OrgID, o *knowledgeOrg) error {
		for _, id := range slices.Sorted(maps.Keys(o.docs)) {
			for key, u := range o.docs[id].uses {
				if key.run == run {
					out = append(out, u)
				}
			}
		}

		return nil
	})

	slices.SortFunc(out, func(a, b domain.KnowledgeUse) int {
		return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.Document, b.Document), cmp.Compare(a.Kind, b.Kind))
	})

	return out, err
}
