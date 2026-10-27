package api

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The organization's credentials (ADR 0017): members read their names and metadata
// (credential:read), owners and admins create, change, replace, delete and import them
// (credential:manage, a fresh sign-in). A value is write-only: a request may carry it, no
// response, log line or audit entry does. The store audits every change in its transaction, so
// these handlers write no audit of their own.

// Errors of the credential routes, answered with their codes by credentialProblem.
var (
	errCredentialNotFound = errors.New("credential not found")
	errCredentialTaken    = errors.New("credential name taken")
	errCredentialInUse    = errors.New("credential in use")
	errCredentialInvalid  = errors.New("credential invalid")
	// errCredentialsUnavailable is a value to seal while goald has no master key.
	errCredentialsUnavailable = errors.New("credentials unavailable")
)

// credentialProblem answers the credential rules with their codes.
func credentialProblem(err error) (gen.Problem, bool) {
	if errors.Is(err, errCredentialsUnavailable) {
		detail := "credential values are unavailable: goald has no master key (set GOAL_MASTER_KEYS; .env.example shows how)"

		return gen.Problem{
			Title: http.StatusText(http.StatusServiceUnavailable), Status: http.StatusServiceUnavailable, Detail: &detail,
			Code: optional("credentials-unavailable"),
		}, true
	}

	answers := []struct {
		err    error
		status int
		code   string
	}{
		{errCredentialNotFound, http.StatusNotFound, "credential-not-found"},
		{errCredentialTaken, http.StatusConflict, "credential-name-taken"},
		{errCredentialInUse, http.StatusConflict, "credential-in-use"},
		{errCredentialInvalid, http.StatusBadRequest, "credential-invalid"},
	}

	for _, a := range answers {
		if errors.Is(err, a.err) {
			p := detailed(a.status, err)
			p.Code = optional(a.code)

			return p, true
		}
	}

	return gen.Problem{}, false
}

// credentialError is err of the store about credential name as the API answers it. The store's
// errors never hold a value.
func credentialError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrNotFound):
		return fmt.Errorf("%w: %w", errCredentialNotFound, err)
	case errors.Is(err, ports.ErrNameTaken):
		return fmt.Errorf("%w: %w", errCredentialTaken, err)
	case errors.Is(err, ports.ErrNoMasterKey):
		return fmt.Errorf("%w: %w", errCredentialsUnavailable, err)
	case errors.Is(err, domain.ErrInvalid):
		return fmt.Errorf("%w: %w", errCredentialInvalid, err)
	default:
		return err
	}
}

func (s *server) ListCredentials(ctx context.Context, _ gen.ListCredentialsRequestObject) (gen.ListCredentialsResponseObject, error) {
	creds, err := s.credentials.List(ctx)
	if err != nil {
		return nil, err
	}

	out := gen.ListCredentials200JSONResponse{Credentials: make([]gen.Credential, 0, len(creds))}

	for _, c := range creds {
		o, err := s.credentialOut(ctx, c)
		if err != nil {
			return nil, err
		}

		out.Credentials = append(out.Credentials, o)
	}

	return out, nil
}

func (s *server) CreateCredential(ctx context.Context, req gen.CreateCredentialRequestObject) (gen.CreateCredentialResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("create credential: the request has no actor")
	}

	b := req.Body
	n := domain.NewCredential{
		Name: b.Name, Type: domain.CredentialType(b.Type), Description: deref(b.Description), Scope: scopeIn(b.Scope),
		Agents: agentsIn(b.Agents), ExpiresAt: b.ExpiresAt, RemindDays: deref(b.RemindDays), Value: deref(b.Value),
	}

	c, err := s.credentials.Create(ctx, n, by)
	if err != nil {
		return nil, credentialError(err)
	}

	out, err := s.credentialOut(ctx, c)
	if err != nil {
		return nil, err
	}

	return gen.CreateCredential201JSONResponse(out), nil
}

func (s *server) GetCredential(ctx context.Context, req gen.GetCredentialRequestObject) (gen.GetCredentialResponseObject, error) {
	c, err := s.credentials.Get(ctx, req.Credential)
	if err != nil {
		return nil, credentialError(err)
	}

	out, err := s.credentialOut(ctx, c)
	if err != nil {
		return nil, err
	}

	return gen.GetCredential200JSONResponse(out), nil
}

// PatchCredential changes a credential's metadata, and with value replaces its value.
func (s *server) PatchCredential(ctx context.Context, req gen.PatchCredentialRequestObject) (gen.PatchCredentialResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("patch credential: the request has no actor")
	}

	b := req.Body
	p := domain.CredentialPatch{Name: b.Name, Description: b.Description, Replace: b.Value}

	if b.Type != nil {
		t := domain.CredentialType(*b.Type)
		p.Type = &t
	}

	if b.Scope != nil {
		scope := scopeIn(b.Scope)
		p.Scope = &scope
	}

	if b.Agents != nil {
		p.Agents = agentsIn(b.Agents)
		if p.Agents == nil {
			p.Agents = map[domain.FactoryID][]string{}
		}
	}

	if b.Expiry != nil {
		p.Expiry = &domain.CredentialExpiry{At: b.Expiry.ExpiresAt, RemindDays: deref(b.Expiry.RemindDays)}
	}

	if p.Empty() {
		return nil, fmt.Errorf("%w: the patch changes nothing", errCredentialInvalid)
	}

	c, err := s.credentials.Patch(ctx, req.Credential, p, by)
	if err != nil {
		return nil, credentialError(err)
	}

	out, err := s.credentialOut(ctx, c)
	if err != nil {
		return nil, err
	}

	return gen.PatchCredential200JSONResponse(out), nil
}

// DeleteCredential deletes a credential no connection refers to; while one does, the answer names
// them (ADR 0017 decision 8).
func (s *server) DeleteCredential(ctx context.Context, req gen.DeleteCredentialRequestObject) (gen.DeleteCredentialResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("delete credential: the request has no actor")
	}

	err := s.credentials.Delete(ctx, req.Credential, by)
	if errors.Is(err, ports.ErrConflict) {
		users, uerr := s.credentials.UsedBy(ctx, req.Credential)
		if uerr != nil {
			return nil, uerr
		}

		return nil, fmt.Errorf("%w: credential %q is referred to by connections %s; change them first",
			errCredentialInUse, req.Credential, strings.Join(users, ", "))
	}

	if err != nil {
		return nil, credentialError(err)
	}

	return gen.DeleteCredential204Response{}, nil
}

// ImportCredentials imports credentials from a .env or YAML text (ADR 0017 decision 9). Every
// name is checked before anything is written; a real run then writes one name at a time, so a
// write that fails leaves the names before it applied.
func (s *server) ImportCredentials(ctx context.Context, req gen.ImportCredentialsRequestObject) (gen.ImportCredentialsResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("import credentials: the request has no actor")
	}

	b := req.Body

	onConflict := gen.CredentialImportOnConflictSkip
	if b.OnConflict != nil {
		onConflict = *b.OnConflict
	}

	if !onConflict.Valid() {
		return nil, fmt.Errorf("%w: on_conflict %q is neither replace nor skip", errCredentialInvalid, onConflict)
	}

	entries, err := domain.ParseCredentialImport(string(b.Format), deref(b.Text))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errCredentialInvalid, err)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: the import holds no credentials", errCredentialInvalid)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}

	preview, err := s.credentials.ImportPreview(ctx, names)
	if err != nil {
		return nil, credentialError(err)
	}

	dry := deref(b.DryRun)
	out := gen.ImportCredentials200JSONResponse{DryRun: dry, Credentials: make([]gen.CredentialImportOutcome, 0, len(preview))}

	for i, p := range preview {
		o := gen.CredentialImportOutcome{Name: p.Name, Exists: p.Exists, Connections: p.Connections}

		if !dry {
			outcome, err := s.importOne(ctx, entries[i], p.Exists, onConflict, by)
			if err != nil {
				return nil, credentialError(fmt.Errorf("import credential %q: %w", p.Name, err))
			}

			o.Outcome = &outcome
		}

		out.Credentials = append(out.Credentials, o)
	}

	return out, nil
}

// importOne writes one entry of a real import: a new name is created as an api_key with no
// factories, an existing one replaced or skipped.
func (s *server) importOne(ctx context.Context, e domain.CredentialImportEntry, exists bool, onConflict gen.CredentialImportOnConflict,
	by domain.Actor,
) (gen.CredentialImportOutcomeOutcome, error) {
	switch {
	case !exists:
		_, err := s.credentials.Create(ctx, domain.NewCredential{Name: e.Name, Type: domain.CredentialAPIKey, Value: e.Value}, by)

		return gen.CredentialImportOutcomeOutcomeCreated, err
	case onConflict == gen.CredentialImportOnConflictReplace:
		value := e.Value
		_, err := s.credentials.Patch(ctx, e.Name, domain.CredentialPatch{Replace: &value}, by)

		return gen.CredentialImportOutcomeOutcomeReplaced, err
	default:
		return gen.CredentialImportOutcomeOutcomeSkipped, nil
	}
}

// credentialOut is a credential as the contract has it, never its value: what uses it is the
// connections that refer to it and the agents it lists.
func (s *server) credentialOut(ctx context.Context, c domain.Credential) (gen.Credential, error) {
	conns, err := s.credentials.UsedBy(ctx, c.Name)
	if err != nil {
		return gen.Credential{}, err
	}

	out := gen.Credential{
		Id: string(c.ID), Name: c.Name, Type: gen.CredentialType(c.Type), Description: c.Description,
		Scope:     gen.CredentialScope{AllFactories: c.Scope.All, Factories: make([]string, 0, len(c.Scope.Factories))},
		Agents:    gen.CredentialAgents{},
		ExpiresAt: c.ExpiresAt, RemindDays: c.RemindDays, LastUsedAt: c.LastUsedAt,
		UsedBy:    gen.CredentialUsers{Connections: append([]string{}, conns...), Agents: []gen.CredentialAgent{}},
		CreatedBy: s.actorOut(c.CreatedBy), CreatedAt: c.CreatedAt, UpdatedBy: s.actorOut(c.UpdatedBy), UpdatedAt: c.UpdatedAt,
	}

	for _, f := range c.Scope.Factories {
		out.Scope.Factories = append(out.Scope.Factories, string(f))
	}

	for _, f := range slices.Sorted(maps.Keys(c.Agents)) {
		out.Agents[string(f)] = append([]string{}, c.Agents[f]...)

		for _, agent := range c.Agents[f] {
			out.UsedBy.Agents = append(out.UsedBy.Agents, gen.CredentialAgent{FactoryId: string(f), Agent: agent})
		}
	}

	return out, nil
}

// scopeIn is a request's scope; absent, none.
func scopeIn(s *gen.CredentialScope) domain.CredentialScope {
	if s == nil {
		return domain.CredentialScope{}
	}

	out := domain.CredentialScope{All: s.AllFactories}
	for _, f := range s.Factories {
		out.Factories = append(out.Factories, domain.FactoryID(f))
	}

	return out
}

// agentsIn is a request's agent list; absent, nil.
func agentsIn(a *gen.CredentialAgents) map[domain.FactoryID][]string {
	if a == nil {
		return nil
	}

	out := make(map[domain.FactoryID][]string, len(*a))
	for f, agents := range *a {
		out[domain.FactoryID(f)] = slices.Clone(agents)
	}

	return out
}
