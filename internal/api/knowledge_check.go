package api

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The Check panel of Add a source (ADR 0038 decision 12): a source's settings, not saved, are
// listed as a sync would list them, and checked against each factory picked to read the source.

// KnowledgeCheck lists a source whose settings are not saved yet (*knowledge.Ingestor): with its
// secret opened in goald, never answered, and refused when the secret is open to some factories
// only. domain.SourceCheck.Error says why it could not.
type KnowledgeCheck interface {
	Check(ctx context.Context, src domain.KnowledgeSource) (domain.SourceCheck, error)
}

// Codes of the Check's warnings besides authoring's: a name in use, a factory that lists the
// source already, and config's check of a factory listing too many.
const (
	checkNameTaken = "knowledge-source-name-taken"
	checkListed    = "knowledge-source-listed"
	checkTooMany   = "knowledge-too-many"
)

// CheckKnowledgeSource answers the Check panel for the request's settings, storing nothing.
func (s *server) CheckKnowledgeSource(ctx context.Context, req gen.CheckKnowledgeSourceRequestObject) (gen.CheckKnowledgeSourceResponseObject, error) {
	b := req.Body
	if b == nil {
		return nil, fmt.Errorf("%w: a check needs the source's settings", errInvalid)
	}

	n := domain.NewKnowledgeSource{
		Name: b.Name, Title: b.Name, Kind: domain.KnowledgeSourceKind(b.Kind), Secret: deref(b.Secret), Config: deref(b.Config), ReadWhen: deref(b.ReadWhen),
	}
	if err := n.Check(); err != nil {
		return nil, fmt.Errorf("%w: %w", errSourceInvalid, err)
	}

	// The secret's scope only, never its value: the check of each factory below.
	var secret *domain.Secret

	if n.Secret != "" {
		sec, err := s.secrets.Get(ctx, n.Secret)
		if err != nil {
			return nil, secretError(err)
		}

		secret = &sec
	}

	out := gen.KnowledgeCheck{
		Limits:    gen.KnowledgeLimits{Files: domain.MaxSourceFiles, Bytes: domain.MaxSourceBytes, FileBytes: domain.MaxFileBytes},
		OverLimit: []gen.KnowledgeCheckOverLimit{}, Warnings: []gen.KnowledgeCheckWarning{},
	}

	src := domain.KnowledgeSource{Name: n.Name, Kind: n.Kind, Secret: n.Secret, Config: n.Config, ReadWhen: n.ReadWhen}

	if err := s.checkExisting(ctx, &src, &out); err != nil {
		return nil, err
	}

	res, err := s.check.Check(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("check knowledge source %q: %w", n.Name, err)
	}

	out.Access = gen.KnowledgeCheckAccess{Ok: res.Error == "", Error: optional(res.Error)}
	out.Files = res.Files
	out.FirstFiles = append([]string{}, res.FirstFiles...)

	if out.Files > domain.MaxSourceFiles {
		out.OverLimit = append(out.OverLimit, "files")
	}

	if out.Bytes != nil && *out.Bytes > domain.MaxSourceBytes {
		out.OverLimit = append(out.OverLimit, "bytes")
	}

	for _, ref := range deref(b.Factories) {
		warnings, err := s.checkFactory(ctx, ref, src, secret)
		if err != nil {
			return nil, err
		}

		out.Warnings = append(out.Warnings, warnings...)
	}

	out.Line = domain.IndexedSource{Name: n.Name, ReadWhen: n.ReadWhen, Files: min(out.Files, domain.MaxSourceFiles)}.Line()

	return gen.CheckKnowledgeSource200JSONResponse(out), nil
}

// checkExisting warns when a source of src's name exists. One of the same kind lends the check
// its size, from its current snapshot, and an upload source its id, so its uploads are listed.
func (s *server) checkExisting(ctx context.Context, src *domain.KnowledgeSource, out *gen.KnowledgeCheck) error {
	have, err := s.knowledge.Source(ctx, src.Name)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return nil
	case err != nil:
		return err
	}

	out.Warnings = append(out.Warnings, gen.KnowledgeCheckWarning{
		Code: checkNameTaken, Message: fmt.Sprintf("the organization has a source named %s already; saving under this name fails", src.Name),
	})

	if have.Kind != src.Kind {
		return nil
	}

	src.ID = have.ID

	snap, err := s.currentSnapshot(ctx, src.Name)
	if err != nil || snap == nil {
		return err
	}

	out.Bytes = &snap.Bytes

	return nil
}

// checkFactory are the warnings of listing src in the factory ref's draft: the definition checks
// it would fail or warn with, and a factory that lists it already.
func (s *server) checkFactory(ctx context.Context, ref string, src domain.KnowledgeSource, secret *domain.Secret) ([]gen.KnowledgeCheckWarning, error) {
	f, err := s.factory(ctx, ref)
	if err != nil {
		return nil, err
	}

	id, name := domain.FactoryID(f.Metadata.ID), f.Metadata.Name

	var out []gen.KnowledgeCheckWarning

	warn := func(code, msg string) {
		out = append(out, gen.KnowledgeCheckWarning{Code: code, Factory: optional(name), Message: msg})
	}

	switch listed := f.Spec.Knowledge.Names(); {
	case slices.Contains(listed, src.Name):
		warn(checkListed, fmt.Sprintf("%s lists %s already", name, src.Name))
	case len(listed) >= domain.MaxFactorySources:
		warn(checkTooMany, fmt.Sprintf("%s lists %d sources, the most a factory may list", name, len(listed)))
	}

	if secret != nil && !secret.Scope.Allows(id) {
		warn(authoring.CodeKnowledgeSecretNotInScope, fmt.Sprintf(
			"%s is not in the scope of the secret %s, so listing the source would hand that token's reach to it", name, secret.Name))
	}

	if repo, ok := f.Spec.Runtime.OnlyRepository(); ok && src.CopiesCheckout(repo.URL) {
		warn(authoring.CodeKnowledgeCopiesCheckout, fmt.Sprintf(
			"%s checks out %s already: its runs have those files on the item's branch", name, repo.URL))
	}

	return out, nil
}
