package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The organization's secrets (ADR 0037 decisions 4 and 16): members read their names, metadata
// and uses (secret:read), owners and admins create, change, replace, delete and import them
// (secret:manage, a fresh sign-in). A value is write-only: a request may carry it, no response,
// problem, log line or audit entry does. The store audits every change in its transaction, so
// these handlers write no audit of their own.
//
// What uses a secret is the factories whose current published version refers to it as {{name}}
// (decision 3: its repository token, model key, a trigger's model-agrees key or an environment
// file), the knowledge sources that read with it and the connections that refer to it, read
// through ports.Credentials.UsedBy (the same table). The parts that let webhooks and the Slack
// setting refer to secrets add their uses to secretUses.

// Errors of the secret routes, answered with their codes by secretProblem.
var (
	errSecretNotFound = errors.New("secret not found")
	errSecretTaken    = errors.New("secret name taken")
	errSecretInvalid  = errors.New("secret invalid")
	// errSecretNotAllowed is a secret opened without a factory, such as a repository test's or a
	// knowledge source's, that is open to some factories only.
	errSecretNotAllowed = errors.New("secret not open to all factories")
	// errSecretsUnavailable is a value to seal while goald has no master key.
	errSecretsUnavailable = errors.New("secrets unavailable")
)

// secretInUseError is a delete refused while something uses the secret; its problem names each use.
type secretInUseError struct {
	name string
	uses []gen.SecretUse
}

func (e *secretInUseError) Error() string {
	names := make([]string, 0, len(e.uses))
	for _, u := range e.uses {
		names = append(names, string(u.Kind)+" "+u.Name)
	}

	return fmt.Sprintf("secret %q is used by %s; change them to stop using it first", e.name, strings.Join(names, ", "))
}

// secretProblem answers the secret rules with their codes.
func secretProblem(err error) (gen.Problem, bool) {
	if errors.Is(err, errSecretsUnavailable) {
		detail := "secret values are unavailable: goald has no master key (set GOAL_MASTER_KEYS; .env.example shows how)"

		return gen.Problem{
			Title: http.StatusText(http.StatusServiceUnavailable), Status: http.StatusServiceUnavailable, Detail: &detail,
			Code: optional("secrets-unavailable"),
		}, true
	}

	if inUse, ok := errors.AsType[*secretInUseError](err); ok {
		p := detailed(http.StatusConflict, err)
		p.Code, p.Uses = optional("secret-in-use"), &inUse.uses

		return p, true
	}

	answers := []struct {
		err    error
		status int
		code   string
	}{
		{errSecretNotFound, http.StatusNotFound, "secret-not-found"},
		{errSecretTaken, http.StatusConflict, "secret-name-taken"},
		{errSecretInvalid, http.StatusBadRequest, "secret-invalid"},
		{errSecretNotAllowed, http.StatusBadRequest, "secret-not-allowed"},
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

// secretError is err of the store about a secret as the API answers it. The store's errors never
// hold a value.
func secretError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrNotFound):
		return fmt.Errorf("%w: %w", errSecretNotFound, err)
	case errors.Is(err, ports.ErrNameTaken):
		return fmt.Errorf("%w: %w", errSecretTaken, err)
	case errors.Is(err, ports.ErrSecretNotAllowed):
		return fmt.Errorf("%w: %w", errSecretNotAllowed, err)
	case errors.Is(err, ports.ErrNoMasterKey):
		return fmt.Errorf("%w: %w", errSecretsUnavailable, err)
	case errors.Is(err, domain.ErrInvalid):
		return fmt.Errorf("%w: %w", errSecretInvalid, err)
	default:
		return err
	}
}

func (s *server) ListSecrets(ctx context.Context, _ gen.ListSecretsRequestObject) (gen.ListSecretsResponseObject, error) {
	secrets, err := s.secrets.List(ctx)
	if err != nil {
		return nil, err
	}

	out := gen.ListSecrets200JSONResponse{Secrets: make([]gen.Secret, 0, len(secrets))}

	for _, sec := range secrets {
		o, err := s.secretOut(ctx, sec)
		if err != nil {
			return nil, err
		}

		out.Secrets = append(out.Secrets, o)
	}

	return out, nil
}

func (s *server) CreateSecret(ctx context.Context, req gen.CreateSecretRequestObject) (gen.CreateSecretResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("create secret: the request has no actor")
	}

	b := req.Body
	n := domain.NewSecret{
		Name: b.Name, Description: deref(b.Description), Scope: secretScopeIn(b.Scope), ExpiresAt: b.ExpiresAt,
		RemindDays: deref(b.RemindDays), Value: deref(b.Value),
	}

	sec, err := s.secrets.Create(ctx, n, by)
	if err != nil {
		return nil, secretError(err)
	}

	out, err := s.secretOut(ctx, sec)
	if err != nil {
		return nil, err
	}

	return gen.CreateSecret201JSONResponse(out), nil
}

func (s *server) GetSecret(ctx context.Context, req gen.GetSecretRequestObject) (gen.GetSecretResponseObject, error) {
	sec, err := s.secrets.Get(ctx, req.Secret)
	if err != nil {
		return nil, secretError(err)
	}

	out, err := s.secretOut(ctx, sec)
	if err != nil {
		return nil, err
	}

	return gen.GetSecret200JSONResponse(out), nil
}

// PatchSecret changes a secret's description, scope, expiry and reminder; never its value.
func (s *server) PatchSecret(ctx context.Context, req gen.PatchSecretRequestObject) (gen.PatchSecretResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("patch secret: the request has no actor")
	}

	b := req.Body
	p := domain.SecretPatch{Description: b.Description}

	if b.Scope != nil {
		scope := secretScopeIn(b.Scope)
		p.Scope = &scope
	}

	if b.Expiry != nil {
		p.Expiry = &domain.SecretExpiry{At: b.Expiry.ExpiresAt, RemindDays: deref(b.Expiry.RemindDays)}
	}

	if p.Empty() {
		return nil, fmt.Errorf("%w: the patch changes nothing", errSecretInvalid)
	}

	sec, err := s.secrets.Patch(ctx, req.Secret, p, by)
	if err != nil {
		return nil, secretError(err)
	}

	out, err := s.secretOut(ctx, sec)
	if err != nil {
		return nil, err
	}

	return gen.PatchSecret200JSONResponse(out), nil
}

// ReplaceSecretValue replaces a secret's value for the runs that start afterwards, then re-tests
// the remote MCP servers whose headers name it; a server that fails its test never fails the replace.
func (s *server) ReplaceSecretValue(ctx context.Context, req gen.ReplaceSecretValueRequestObject) (gen.ReplaceSecretValueResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("replace secret value: the request has no actor")
	}

	value := deref(req.Body.Value)
	p := domain.SecretPatch{Replace: &value}

	if err := p.CheckValue(); err != nil {
		return nil, fmt.Errorf("%w: %w", errSecretInvalid, err)
	}

	sec, err := s.secrets.Patch(ctx, req.Secret, p, by)
	if err != nil {
		return nil, secretError(err)
	}

	s.retestSecretUsers(ctx, sec.Name)

	out, err := s.secretOut(ctx, sec)
	if err != nil {
		return nil, err
	}

	return gen.ReplaceSecretValue200JSONResponse(out), nil
}

// DeleteSecret deletes a secret nothing uses; while something does, the answer names each use
// (ADR 0037 decision 4) and nothing is deleted.
func (s *server) DeleteSecret(ctx context.Context, req gen.DeleteSecretRequestObject) (gen.DeleteSecretResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("delete secret: the request has no actor")
	}

	if _, err := s.secrets.Get(ctx, req.Secret); err != nil {
		return nil, secretError(err)
	}

	uses, err := s.secretUses(ctx, req.Secret)
	if err != nil {
		return nil, err
	}

	if len(uses) > 0 {
		return nil, &secretInUseError{name: req.Secret, uses: uses}
	}

	err = s.secrets.Delete(ctx, req.Secret, by)
	if errors.Is(err, ports.ErrConflict) {
		// Something started using it after the check above.
		if uses, err = s.secretUses(ctx, req.Secret); err != nil {
			return nil, err
		}

		return nil, &secretInUseError{name: req.Secret, uses: uses}
	}

	if err != nil {
		return nil, secretError(err)
	}

	return gen.DeleteSecret204Response{}, nil
}

// ImportSecrets imports secrets from a .env or YAML text (ADR 0037 decision 3, ADR 0017 decision
// 9). Every name is checked before anything is written; a dry run answers each name as new or
// existing, with its uses, and stores nothing; a real run then writes one name at a time, so a
// write that fails leaves the names before it applied.
func (s *server) ImportSecrets(ctx context.Context, req gen.ImportSecretsRequestObject) (gen.ImportSecretsResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("import secrets: the request has no actor")
	}

	b := req.Body

	onConflict := gen.SecretImportOnConflictSkip
	if b.OnConflict != nil {
		onConflict = *b.OnConflict
	}

	if !onConflict.Valid() {
		return nil, fmt.Errorf("%w: on_conflict %q is neither replace nor skip", errSecretInvalid, onConflict)
	}

	entries, err := domain.ParseCredentialImport(string(b.Format), deref(b.Text))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errSecretInvalid, err)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: the import holds no secrets", errSecretInvalid)
	}

	dry := deref(b.DryRun)
	out := gen.ImportSecrets200JSONResponse{DryRun: dry, Secrets: make([]gen.SecretImportOutcome, 0, len(entries))}

	for _, e := range entries {
		o, err := s.importPreview(ctx, e.Name)
		if err != nil {
			return nil, err
		}

		out.Secrets = append(out.Secrets, o)
	}

	if dry {
		return out, nil
	}

	for i, e := range entries {
		outcome, err := s.importSecret(ctx, e, out.Secrets[i].Exists, onConflict, by)
		if err != nil {
			return nil, secretError(fmt.Errorf("import secret %q: %w", e.Name, err))
		}

		out.Secrets[i].Outcome = &outcome
	}

	return out, nil
}

// importPreview is what an import does with name: whether a secret has it, and what uses that one.
func (s *server) importPreview(ctx context.Context, name string) (gen.SecretImportOutcome, error) {
	_, err := s.secrets.Get(ctx, name)

	switch {
	case errors.Is(err, ports.ErrNotFound):
		return gen.SecretImportOutcome{Name: name, Uses: []gen.SecretUse{}}, nil
	case err != nil:
		return gen.SecretImportOutcome{}, secretError(err)
	}

	uses, err := s.secretUses(ctx, name)
	if err != nil {
		return gen.SecretImportOutcome{}, err
	}

	return gen.SecretImportOutcome{Name: name, Exists: true, Uses: uses}, nil
}

// importSecret writes one entry of a real import: a new name is created open to no factory, an
// existing one replaced or skipped.
func (s *server) importSecret(ctx context.Context, e domain.CredentialImportEntry, exists bool, onConflict gen.SecretImportOnConflict,
	by domain.Actor,
) (gen.SecretImportOutcomeOutcome, error) {
	switch {
	case !exists:
		_, err := s.secrets.Create(ctx, domain.NewSecret{Name: e.Name, Value: e.Value}, by)

		return gen.SecretImportOutcomeOutcomeCreated, err
	case onConflict == gen.SecretImportOnConflictReplace:
		value := e.Value
		_, err := s.secrets.Patch(ctx, e.Name, domain.SecretPatch{Replace: &value}, by)

		return gen.SecretImportOutcomeOutcomeReplaced, err
	default:
		return gen.SecretImportOutcomeOutcomeSkipped, nil
	}
}

// secretUses is what uses secret name, by kind then name: the connections that refer to it as
// {{name}}, the factories whose current published version does (archived ones left out, as for a
// connection's users) and the knowledge sources that read with it. It opens no secret.
func (s *server) secretUses(ctx context.Context, name string) ([]gen.SecretUse, error) {
	conns, err := s.credentials.UsedBy(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("the uses of secret %q: %w", name, err)
	}

	users, err := s.usersBy(ctx, secretRefNames)
	if err != nil {
		return nil, fmt.Errorf("the uses of secret %q: %w", name, err)
	}

	sources, err := s.knowledge.Sources(ctx)
	if err != nil {
		return nil, fmt.Errorf("the uses of secret %q: %w", name, err)
	}

	uses := make([]gen.SecretUse, 0, len(conns))

	for _, c := range conns {
		uses = append(uses, gen.SecretUse{Kind: gen.SecretUseConnection, Name: c})
	}

	defs := make([]string, 0, len(users[name]))
	for _, f := range users[name] {
		defs = append(defs, f.name)
	}

	slices.Sort(defs)

	for _, d := range defs {
		uses = append(uses, gen.SecretUse{Kind: gen.SecretUseDefinition, Name: d})
	}

	// Sources come by name.
	for _, src := range sources {
		if src.Secret == name {
			uses = append(uses, gen.SecretUse{Kind: gen.SecretUseKnowledgeSource, Name: src.Name})
		}
	}

	return uses, nil
}

// secretRefNames are the secrets f's {{name}} references name, each once.
func secretRefNames(f *config.Factory) []string {
	var out []string

	for _, r := range f.SecretRefs() {
		if !slices.Contains(out, r.Name) {
			out = append(out, r.Name)
		}
	}

	return out
}

// secretOut is a secret as the contract has it, never its value.
func (s *server) secretOut(ctx context.Context, sec domain.Secret) (gen.Secret, error) {
	uses, err := s.secretUses(ctx, sec.Name)
	if err != nil {
		return gen.Secret{}, err
	}

	out := gen.Secret{
		Id: string(sec.ID), Name: sec.Name, Source: gen.SecretSource(sec.Source), Description: sec.Description,
		Scope:     gen.SecretScope{AllFactories: sec.Scope.All, Factories: make([]string, 0, len(sec.Scope.Factories))},
		ExpiresAt: sec.ExpiresAt, RemindDays: sec.RemindDays, LastUsedAt: sec.LastUsedAt, Uses: uses,
		CreatedBy: s.actorOut(sec.CreatedBy), CreatedAt: sec.CreatedAt, UpdatedBy: s.actorOut(sec.UpdatedBy), UpdatedAt: sec.UpdatedAt,
	}

	for _, f := range sec.Scope.Factories {
		out.Scope.Factories = append(out.Scope.Factories, string(f))
	}

	return out, nil
}

// secretScopeIn is a request's scope; absent, none.
func secretScopeIn(sc *gen.SecretScope) domain.SecretScope {
	if sc == nil {
		return domain.SecretScope{}
	}

	out := domain.SecretScope{All: sc.AllFactories}
	for _, f := range sc.Factories {
		out.Factories = append(out.Factories, domain.FactoryID(f))
	}

	return out
}
