package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// ErrNotAssembly means a part was asked for an open item that is not an assembly.
var ErrNotAssembly = errors.New("item is not an assembly")

const (
	// maxPartKey is the longest key AddPart derives: ^[a-z0-9][a-z0-9-]{0,40}$.
	maxPartKey = 41
	// partKeyFallback keys a part whose title has no letter or digit to slug.
	partKeyFallback = "part"
)

// AddPart adds one part, by by, to an open assembly (ADR 0012): trimmed, keyed by a slug of its
// title made unique among the assembly's parts (-2, -3…), queued at the assembly's station with
// a planned edge to each part it needs. A done or cancelled item is refused with ErrItemClosed,
// another item that is not an assembly with ErrNotAssembly, an empty title or brief or an
// unknown need with domain.ErrInvalid, and an assembly that changed meanwhile (closed, or the
// key taken by another add) with ErrItemChanged. A refused part writes nothing.
func (e *Engine) AddPart(ctx context.Context, assembly domain.ItemID, by domain.Actor, part domain.NewPart) (domain.Item, domain.Parts, error) {
	part = trimPart(part)
	if part.Title == "" || part.Brief == "" {
		return domain.Item{}, domain.Parts{}, fmt.Errorf("add part to %s: a part needs a title and a brief: %w", assembly, domain.ErrInvalid)
	}

	it, err := e.store.Item(ctx, assembly)
	if err != nil {
		return domain.Item{}, domain.Parts{}, fmt.Errorf("add part to %s: %w", assembly, err)
	}

	switch {
	case closed(it):
		return domain.Item{}, domain.Parts{}, fmt.Errorf("add part to %s: %w", assembly, ErrItemClosed)
	case it.State != domain.StateAssembly:
		return domain.Item{}, domain.Parts{}, fmt.Errorf("add part to %s: %w", assembly, ErrNotAssembly)
	}

	existing, err := e.store.Parts(ctx, assembly)
	if err != nil {
		return domain.Item{}, domain.Parts{}, fmt.Errorf("add part to %s: %w", assembly, err)
	}

	keys := make(map[string]bool, existing.Total)
	for _, p := range existing.Items {
		keys[p.PartKey] = true
	}

	part.Key = uniqueKey(partSlug(part.Title), keys)
	now := e.clock.Now()

	got, parts, err := e.store.AddPart(ctx, ports.NewAssemblyPart{
		Assembly: assembly, Part: part, By: by, At: now,
		Audit: itemAudit(it, now, by, domain.AuditItemPartAdded, map[string]any{"key": part.Key}),
	})
	if errors.Is(err, ports.ErrConflict) {
		return domain.Item{}, domain.Parts{}, fmt.Errorf("add part to %s: %w", assembly, errors.Join(ErrItemChanged, err))
	}

	if err != nil {
		return domain.Item{}, domain.Parts{}, fmt.Errorf("add part to %s: %w", assembly, err)
	}

	e.log.InfoContext(ctx, "part added", slog.String("item", string(assembly)), slog.String("part", string(got.ID)),
		slog.String("key", got.PartKey), slog.String("by", by.Ref()))

	return got, parts, nil
}

// trimPart is p with the space around its title, lane, brief and each need trimmed.
func trimPart(p domain.NewPart) domain.NewPart {
	p.Title, p.Lane, p.Brief = strings.TrimSpace(p.Title), strings.TrimSpace(p.Lane), strings.TrimSpace(p.Brief)

	needs := make([]string, len(p.Needs))
	for i, n := range p.Needs {
		needs[i] = strings.TrimSpace(n)
	}

	p.Needs = needs

	return p
}

// partSlug is title in lowercase with each run of characters other than a-z and 0-9 a hyphen,
// without hyphens at its ends, at most maxPartKey long; partKeyFallback when nothing is left.
func partSlug(title string) string {
	var b strings.Builder

	hyphen := false

	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}

			b.WriteRune(r)

			hyphen = false
		default:
			hyphen = true
		}
	}

	if b.Len() == 0 {
		return partKeyFallback
	}

	return clip(b.String(), maxPartKey)
}

// uniqueKey is slug when keys does not have it, else slug clipped to fit with the first suffix
// -2, -3… that makes it a key keys does not have.
func uniqueKey(slug string, keys map[string]bool) string {
	if !keys[slug] {
		return slug
	}

	for n := 2; ; n++ {
		suffix := "-" + strconv.Itoa(n)
		if k := clip(slug, maxPartKey-len(suffix)) + suffix; !keys[k] {
			return k
		}
	}
}

// clip is slug cut to at most n bytes, without a trailing hyphen.
func clip(slug string, n int) string {
	if len(slug) > n {
		slug = slug[:n]
	}

	return strings.TrimRight(slug, "-")
}

// assemblyNote tells a part's agent which assembly it serves (ADR 0012): the assembly's ref and
// title, the part's key, the other parts in plan order with their state, the parts it needs and
// that a part returns a spec, never a plan. It is empty for an item that is not a part.
func (e *Engine) assemblyNote(ctx context.Context, l domain.Line, it domain.Item) (string, error) {
	if it.ParentID == "" {
		return "", nil
	}

	a, err := e.store.Item(ctx, it.ParentID)
	if err != nil {
		return "", fmt.Errorf("read the assembly of part %s: %w", it.ID, err)
	}

	parts, err := e.store.Parts(ctx, it.ParentID)
	if err != nil {
		return "", fmt.Errorf("read the parts of assembly %s: %w", it.ParentID, err)
	}

	edges, err := e.store.Edges(ctx, it.ID)
	if err != nil {
		return "", fmt.Errorf("read the needs of part %s: %w", it.ID, err)
	}

	ref := func(n int64) string { return domain.ItemRef{Factory: l.Name, Number: n}.String() }
	keys := make(map[domain.ItemID]string, len(parts.Items))

	var b strings.Builder

	fmt.Fprintf(&b, "## Assembly\n\nThis item is part `%s` of assembly %s, %q: one of the parts it was split into, which ship together.",
		it.PartKey, ref(a.Number), a.Title)

	others := make([]string, 0, len(parts.Items))

	for _, p := range parts.Items {
		keys[p.ID] = p.PartKey

		if p.ID != it.ID {
			others = append(others, fmt.Sprintf("- `%s` %s %q: %s", p.PartKey, ref(p.Number), p.Title, p.State))
		}
	}

	if len(others) > 0 {
		fmt.Fprintf(&b, "\n\nThe other parts, in plan order:\n\n%s", strings.Join(others, "\n"))
	}

	var needs []string

	for _, n := range edges.Needs {
		if k, ok := keys[n.Needs]; ok && n.Kind == domain.EdgePlanned {
			needs = append(needs, "`"+k+"`")
		}
	}

	if len(needs) == 0 {
		b.WriteString("\n\nThis part needs no other part.")
	} else {
		fmt.Fprintf(&b, "\n\nThis part needs %s.", strings.Join(needs, ", "))
	}

	b.WriteString("\n\nA part returns a spec, never a plan: do not split this part into parts, and leave the plan out of your output.")

	return b.String(), nil
}
