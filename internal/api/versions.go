package api

import (
	"bytes"
	"context"
	"fmt"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
)

// The version routes (B §8.3): history, one version, its model and files, compare and restore.

func (s *server) ListVersions(ctx context.Context, req gen.ListVersionsRequestObject) (gen.ListVersionsResponseObject, error) {
	p := req.Params

	badBefore := p.Before != nil && *p.Before < 1
	badLimit := p.Limit != nil && (*p.Limit < 1 || *p.Limit > authoring.MaxVersionPage)

	if badBefore || badLimit {
		return nil, fmt.Errorf("%w: before must be ≥ 1 and limit in [1, %d]", errInvalid, authoring.MaxVersionPage)
	}

	before, limit := deref(p.Before), deref(p.Limit)

	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	page, err := s.defs.Versions(ctx, rec.ID, before, limit)
	if err != nil {
		return nil, err
	}

	out := gen.ListVersions200JSONResponse{Versions: make([]gen.Version, 0, len(page.Versions))}
	for _, v := range page.Versions {
		out.Versions = append(out.Versions, s.versionOut(v))
	}

	if page.NextBefore > 0 {
		out.NextBefore = &page.NextBefore
	}

	return out, nil
}

func (s *server) GetVersion(ctx context.Context, req gen.GetVersionRequestObject) (gen.GetVersionResponseObject, error) {
	rec, sel, err := s.version(ctx, req.Factory, req.Version)
	if err != nil {
		return nil, err
	}

	v, err := s.defs.Version(ctx, rec, sel)
	if err != nil {
		return nil, err
	}

	return gen.GetVersion200JSONResponse(s.versionDetailOut(v)), nil
}

func (s *server) GetVersionModel(ctx context.Context, req gen.GetVersionModelRequestObject) (gen.GetVersionModelResponseObject, error) {
	rec, sel, err := s.version(ctx, req.Factory, req.Version)
	if err != nil {
		return nil, err
	}

	m, r, err := s.defs.VersionModel(ctx, rec, sel)
	if err != nil {
		return nil, err
	}

	model, err := modelOut(m)
	if err != nil {
		return nil, err
	}

	return gen.GetVersionModel200JSONResponse{Model: model, Ranges: rangesOut(r)}, nil
}

func (s *server) GetVersionFile(ctx context.Context, req gen.GetVersionFileRequestObject) (gen.GetVersionFileResponseObject, error) {
	rec, sel, err := s.version(ctx, req.Factory, req.Version)
	if err != nil {
		return nil, err
	}

	f, err := s.defs.VersionFile(ctx, rec, sel, req.Path)
	if err != nil {
		return nil, err
	}

	cache := "no-cache"
	if f.Immutable {
		cache = immutableCache
	}

	return gen.GetVersionFile200ApplicationoctetStreamResponse{
		Body: bytes.NewReader(f.Content), ContentLength: int64(len(f.Content)),
		Headers: gen.GetVersionFile200ResponseHeaders{ETag: quote(string(f.SHA256)), CacheControl: cache},
	}, nil
}

func (s *server) RestoreVersion(ctx context.Context, req gen.RestoreVersionRequestObject) (gen.RestoreVersionResponseObject, error) {
	rec, sel, err := s.version(ctx, req.Factory, req.Version)
	if err != nil {
		return nil, err
	}

	b := req.Body

	restored, err := s.defs.Restore(ctx, rec, authoring.RestoreRequest{
		Version: sel, ExpectedCurrent: domain.DefinitionVersionID(b.ExpectedCurrentVersionId), StationMoves: deref(b.StationMoves),
	})
	if err != nil {
		return nil, err
	}

	return gen.RestoreVersion200JSONResponse(s.factoryOut(ctx, restored)), nil
}

func (s *server) CompareVersions(ctx context.Context, req gen.CompareVersionsRequestObject) (gen.CompareVersionsResponseObject, error) {
	from, to := deref(req.Params.From), deref(req.Params.To)
	if from == "" {
		from = string(authoring.SelectBase)
	}

	if to == "" {
		to = string(authoring.SelectDraft)
	}

	fromSel, err := authoring.ParseVersion(from)
	if err != nil {
		return nil, err
	}

	toSel, err := authoring.ParseVersion(to)
	if err != nil {
		return nil, err
	}

	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	c, err := s.defs.Compare(ctx, rec.ID, fromSel, toSel)
	if err != nil {
		return nil, err
	}

	return gen.CompareVersions200JSONResponse(comparisonOut(c, from, to)), nil
}

// version resolves a version route's factory and reads its {version} selector.
func (s *server) version(ctx context.Context, factory, version string) (domain.FactoryID, authoring.VersionSelector, error) {
	sel, err := authoring.ParseVersion(version)
	if err != nil {
		return "", authoring.VersionSelector{}, err
	}

	rec, err := s.definition(ctx, factory)
	if err != nil {
		return "", authoring.VersionSelector{}, err
	}

	return rec.ID, sel, nil
}
