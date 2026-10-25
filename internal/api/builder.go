package api

import (
	"bytes"
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/domain"
)

// Builder is the factory editor's backend beside authoring: the install's metadata and the
// skills catalog; *builder.Service satisfies it. Its refusals are domain.ErrSkillNotFound (404
// skill-not-found) and domain.ErrCatalogUnavailable (503 catalog-unavailable).
type Builder interface {
	Meta(ctx context.Context) domain.BuilderMeta
	SearchSkills(ctx context.Context, q string, limit int, after string) (domain.SkillPage, error)
	SkillFiles(ctx context.Context, key domain.SkillKey) (domain.CatalogSkill, map[string][]byte, error)
}

// Paging of the skills catalog (2.2-D §8).
const (
	defaultSkillLimit = 20
	maxSkillLimit     = 50
	maxSkillQuery     = 200
	maxSkillAfter     = 320
)

func (s *server) GetBuilderMeta(ctx context.Context, _ gen.GetBuilderMetaRequestObject) (gen.GetBuilderMetaResponseObject, error) {
	m := s.builder.Meta(ctx)

	out := gen.GetBuilderMeta200JSONResponse{
		Models:       make([]gen.BuilderModel, 0, len(m.Models)),
		Capabilities: gen.BuilderCapabilities{ToolApprovals: m.Capabilities.ToolApprovals},
		Limits:       gen.BuilderLimits{SkillBytes: m.MaxSkillBytes, SkillFiles: m.MaxSkillFiles},
	}

	for i, model := range m.Models {
		out.Models = append(out.Models, gen.BuilderModel{Id: model.ID, Label: model.Label, Default: i == 0})
	}

	return out, nil
}

func (s *server) SearchSkills(ctx context.Context, req gen.SearchSkillsRequestObject) (gen.SearchSkillsResponseObject, error) {
	p := req.Params

	limit := defaultSkillLimit
	if p.Limit != nil {
		limit = *p.Limit
	}

	if limit < 1 || limit > maxSkillLimit {
		return nil, fmt.Errorf("%w: limit must be in [1, %d]", errInvalid, maxSkillLimit)
	}

	if len(deref(p.Q)) > maxSkillQuery || len(deref(p.After)) > maxSkillAfter {
		return nil, fmt.Errorf("%w: q is at most %d characters and after at most %d", errInvalid, maxSkillQuery, maxSkillAfter)
	}

	page, err := s.builder.SearchSkills(ctx, deref(p.Q), limit, deref(p.After))
	if err != nil {
		return nil, err
	}

	out := gen.SearchSkills200JSONResponse{Skills: make([]gen.SkillListing, 0, len(page.Skills)), NextAfter: optional(page.NextAfter)}
	for _, sk := range page.Skills {
		out.Skills = append(out.Skills, skillListingOf(sk))
	}

	return out, nil
}

func (s *server) GetSkillFiles(ctx context.Context, req gen.GetSkillFilesRequestObject) (gen.GetSkillFilesResponseObject, error) {
	sk, files, err := s.builder.SkillFiles(ctx, domain.SkillKey{Owner: req.Owner, Repo: req.Repo, Skill: req.Skill})
	if err != nil {
		return nil, err
	}

	out := gen.GetSkillFiles200JSONResponse{Skill: skillListingOf(sk), Files: make(gen.FileMap, len(files))}

	for p, body := range files {
		c, err := fileContentOf(body)
		if err != nil {
			return nil, err
		}

		out.Files[p] = c
	}

	return out, nil
}

// fileContentOf is a file as text when it is UTF-8 without NUL bytes, else in base64.
func fileContentOf(body []byte) (gen.FileContent, error) {
	var c gen.FileContent

	if utf8.Valid(body) && !bytes.Contains(body, []byte{0}) {
		return c, c.FromTextFile(gen.TextFile{Text: string(body)})
	}

	return c, c.FromBase64File(gen.Base64File{Base64: body})
}

func skillListingOf(sk domain.CatalogSkill) gen.SkillListing {
	return gen.SkillListing{
		Key: sk.String(), Owner: sk.Owner, Repo: sk.Repo, Skill: sk.Skill, Commit: sk.Commit, Description: sk.Description,
		Installs: sk.Installs, Files: sk.Files, Bytes: sk.Bytes,
	}
}
