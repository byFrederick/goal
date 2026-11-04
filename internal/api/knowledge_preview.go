package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// GetDraftKnowledgePreview is "What agents see" in the editor (ADR 0038 decision 8): the
// Knowledge section a run of the agent gets from the draft, built by domain.IndexKnowledge as the
// gateway builds a run's, from each source's current snapshot rather than a claim's pin.
func (s *server) GetDraftKnowledgePreview(
	ctx context.Context, req gen.GetDraftKnowledgePreviewRequestObject,
) (gen.GetDraftKnowledgePreviewResponseObject, error) {
	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	line, err := s.defs.DraftLine(ctx, rec.ID)
	if err != nil {
		return nil, err
	}

	p := req.Params

	agent, ok := line.Agent(p.Agent)
	if !ok {
		return nil, fmt.Errorf("the draft of %s has no %q: %w", rec.ID, p.Agent, errAgentNotFound)
	}

	run := domain.RuleTarget{Agent: agent.Name, Station: deref(p.Station), Lane: deref(p.Lane), WorkType: deref(p.WorkType)}

	idx, err := domain.IndexKnowledge(line, agent, run, true, func(source string) (domain.KnowledgeSnapshot, []string, bool, error) {
		return s.previewSnapshot(ctx, source)
	})
	if err != nil {
		return nil, err
	}

	return gen.GetDraftKnowledgePreview200JSONResponse{Section: idx.Section()}, nil
}

// previewSnapshot is source's current snapshot with its top folders, read from its files; a
// source the organization does not have, or has not synced yet, has none.
func (s *server) previewSnapshot(ctx context.Context, source string) (domain.KnowledgeSnapshot, []string, bool, error) {
	snap, err := s.currentSnapshot(ctx, source)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return domain.KnowledgeSnapshot{}, nil, false, nil
	case err != nil:
		return domain.KnowledgeSnapshot{}, nil, false, err
	case snap == nil:
		return domain.KnowledgeSnapshot{}, nil, false, nil
	}

	paths, err := s.filePaths(ctx, source)
	if err != nil {
		return domain.KnowledgeSnapshot{}, nil, false, err
	}

	return *snap, domain.TopFolders(paths), true, nil
}

// filePaths are the paths of source's current files, read a page at a time: at most
// domain.MaxSourceFiles, so domain.MaxSourceFiles/domain.MaxFilePage pages.
func (s *server) filePaths(ctx context.Context, source string) ([]string, error) {
	var paths []string

	q := domain.KnowledgeFileQuery{Source: source, Limit: domain.MaxFilePage}

	for {
		page, err := s.knowledge.Files(ctx, q)
		if err != nil {
			return nil, sourceError(err, source)
		}

		for _, f := range page {
			paths = append(paths, f.Path)
		}

		if len(page) < q.Limit || len(paths) >= domain.MaxSourceFiles {
			return paths, nil
		}

		q.After = page[len(page)-1].Path
	}
}
