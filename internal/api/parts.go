package api

import (
	"context"
	"fmt"
	"slices"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
)

// Assemblies and parts on the item read (ADR 0012).

// assemblyOf fills out's assembly, when it is a part, or its parts, when it is an assembly: one
// read of the parts (and, for a part, of its assembly); getItem only.
func (s *server) assemblyOf(ctx context.Context, it domain.Item, out *gen.Item) error {
	if it.PartKey == "" {
		parts, err := s.items.Parts(ctx, it.ID)
		if err != nil {
			return fmt.Errorf("parts of %s: %w", it.ID, err)
		}

		if parts.Total > 0 {
			out.Parts = s.itemPartsOf(ctx, parts)
		}

		return nil
	}

	asm, err := s.items.Item(ctx, it.ParentID)
	if err != nil {
		return fmt.Errorf("assembly of part %s: %w", it.ID, err)
	}

	parts, err := s.items.Parts(ctx, asm.ID)
	if err != nil {
		return fmt.Errorf("parts of %s: %w", asm.ID, err)
	}

	out.Assembly = &gen.PartOf{
		Id: string(asm.ID), Ref: s.ref(ctx, asm.FactoryID, asm.Number), Title: asm.Title,
		Part:  slices.IndexFunc(parts.Items, func(p domain.Item) bool { return p.ID == it.ID }) + 1,
		Parts: parts.Total,
	}

	return nil
}

// itemPartsOf is an assembly's parts as its item read lists them, in plan order.
func (s *server) itemPartsOf(ctx context.Context, parts domain.Parts) *gen.ItemParts {
	out := &gen.ItemParts{Done: parts.Done, Total: parts.Total, List: make([]gen.ItemPart, 0, len(parts.Items))}

	for _, p := range parts.Items {
		out.List = append(out.List, gen.ItemPart{
			Id: string(p.ID), Ref: s.ref(ctx, p.FactoryID, p.Number), Number: p.Number, Title: p.Title,
			PartKey: p.PartKey, Station: p.Station, State: gen.ItemState(p.State),
		})
	}

	return out
}
