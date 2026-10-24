package api

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/byFrederick/goal/internal/domain"
	"github.com/byFrederick/goal/internal/ports"
)

// rule is one permission of api/permissions.yaml as cmd/permgen generates it into
// permissionRules (permissions_gen.go); operationScopes names each operation's permission.
// Change the yaml or an operation's x-goal-scope, then run task api:gen.
type rule struct {
	// level is levelInstall (held outside an organization), levelOrg or levelFactory.
	level string
	// orgRoles hold an org permission. Owners and admins hold every factory permission.
	orgRoles []string
	// factoryRole is the lowest factory role holding a factory permission.
	factoryRole string
	// keys: an API key may carry it. fresh: a session signed in within domain.FreshFor.
	// platformAdmin: only platform admins hold this install permission.
	keys, fresh, platformAdmin bool
}

// The levels of api/permissions.yaml.
const (
	levelInstall = "install"
	levelOrg     = "org"
	levelFactory = "factory"
)

// Permissions every principal is asked about by name.
const (
	permPublic      domain.Permission = "public"
	permFactoryRead domain.Permission = "factory:read"
)

// errDenied is a principal that sees the resource but lacks the permission (403 forbidden).
var errDenied = errors.New("the principal does not hold the permission")

// errHidden is a resource the principal may not see: it answers as the unknown one (404).
var errHidden = fmt.Errorf("not visible to the principal: %w", ports.ErrNotFound)

// Permissions is the Authorizer of signed-in installs: it decides every request from the
// generated table (api/permissions.yaml, spec 3 §8.4) and the principal the request carries,
// never from a cache. Its clock dates sessions for the fresh rule.
type Permissions struct {
	clock ports.Clock
}

// NewPermissions returns the table's Authorizer.
func NewPermissions(clock ports.Clock) *Permissions { return &Permissions{clock: clock} }

// Authorize implements Authorizer, in the order of answers the API gives:
//  1. an operation or permission missing from the table is refused (fail closed);
//  2. public passes; anything else needs a principal (domain.ErrUnauthenticated);
//  3. install permissions: users, platform admins only where the table says, keys only where
//     keys may carry it;
//  4. org and factory permissions: a user who is not a member, or a key of another org, does
//     not see the organization (ErrNotFound); a factory the principal has no role on, or a key
//     restricted to other factories, is hidden too; then the org role, the factory role, or
//     the key's scopes and ceiling must hold it (403). In the middleware pass (Factory "") a
//     factory permission needs only membership;
//  5. fresh permissions need a session signed in within domain.FreshFor
//     (domain.ErrReauthRequired); keys never hold them.
func (p *Permissions) Authorize(_ context.Context, r AccessRequest) error {
	name := r.Permission
	if name == "" {
		name = domain.Permission(operationScopes[r.Operation])
	}

	ru, ok := permissionRules[string(name)]
	if !ok {
		return fmt.Errorf("operation %q: permission %q is not in the table: %w", r.Operation, name, errDenied)
	}

	if name == permPublic {
		return nil
	}

	pr := r.Principal
	if !authenticated(pr) {
		return domain.ErrUnauthenticated
	}

	var err error

	switch ru.level {
	case levelInstall:
		err = installGrant(pr, ru)
	case levelOrg, levelFactory:
		err = orgGrant(pr, name, ru, r)
	default:
		err = fmt.Errorf("permission %q: unknown level %q: %w", name, ru.level, errDenied)
	}

	if err != nil {
		return err
	}

	return p.fresh(pr, ru)
}

// authenticated reports whether the principal is a user or a key the database resolved.
func authenticated(p domain.Principal) bool {
	return (p.Kind == domain.PrincipalUser && p.User != "") || (p.Kind == domain.PrincipalService && p.Key != "")
}

func installGrant(p domain.Principal, ru rule) error {
	if ru.platformAdmin && (p.Kind != domain.PrincipalUser || !p.PlatformAdmin) {
		return fmt.Errorf("platform admins only: %w", errDenied)
	}

	if p.Kind == domain.PrincipalService && !ru.keys {
		return fmt.Errorf("not for API keys: %w", errDenied)
	}

	return nil
}

func orgGrant(p domain.Principal, name domain.Permission, ru rule, r AccessRequest) error {
	if r.Org == "" {
		return fmt.Errorf("permission %q outside an organization: %w", name, errDenied)
	}

	member := p.Org == r.Org && (p.Kind == domain.PrincipalService || p.OrgRole != "")
	if !member {
		return errHidden
	}

	if r.Factory != "" && p.RoleOn(r.Factory) == "" {
		return errHidden
	}

	if !holds(p, name, ru, r.Factory) {
		return fmt.Errorf("permission %q: %w", name, errDenied)
	}

	return nil
}

// holds reports whether a member of the organization, or one of its keys, holds an org or
// factory permission: by org role, by the role on factory f, or by a key's scopes and ceiling.
// In the middleware pass (f "") a factory permission needs only membership.
func holds(p domain.Principal, name domain.Permission, ru rule, f domain.FactoryID) bool {
	byRole := p.RoleOn(f).Includes(domain.FactoryRole(ru.factoryRole))

	switch {
	case ru.level == levelFactory && f == "":
		return true
	case p.Kind == domain.PrincipalService:
		return ru.keys && slices.Contains(p.Scopes, name) && (ru.level == levelOrg || byRole)
	case ru.level == levelOrg:
		return slices.Contains(ru.orgRoles, string(p.OrgRole))
	default:
		return byRole
	}
}

// fresh refuses a fresh permission to a session signed in longer ago than domain.FreshFor, and
// to a principal without a session.
func (p *Permissions) fresh(pr domain.Principal, ru rule) error {
	if !ru.fresh {
		return nil
	}

	if pr.Session == nil || p.clock.Now().Sub(pr.Session.CreatedAt) > domain.FreshFor {
		return domain.ErrReauthRequired
	}

	return nil
}

// ask asks the Authorizer whether the request's principal holds permission on factory f (the
// organization's own permissions when f is "").
func (s *server) ask(ctx context.Context, permission domain.Permission, f domain.FactoryID) error {
	r, err := access(ctx, f)
	if err != nil {
		return err
	}

	r.Permission = permission

	return s.authz.Authorize(ctx, r)
}

// visible reports whether the request's principal sees factory f: lists and aggregates show only
// the factories it sees, and every other route answers an invisible one as an unknown one.
func (s *server) visible(ctx context.Context, f domain.FactoryID) bool {
	err := s.ask(ctx, permFactoryRead, f)

	return !errors.Is(err, ports.ErrNotFound) && !errors.Is(err, domain.ErrUnauthenticated)
}

// can lists, sorted, the permissions the request's principal holds on factory f, or of the
// organization when f is "": what the UI may offer (the `can` of an Org or a Factory). A fresh
// permission held with an older session counts: the UI asks to sign in again when it is used.
func (s *server) can(ctx context.Context, f domain.FactoryID) []domain.Permission {
	level := levelFactory
	if f == "" {
		level = levelOrg
	}

	held := []domain.Permission{}

	for _, name := range slices.Sorted(maps.Keys(permissionRules)) {
		if permissionRules[name].level != level {
			continue
		}

		if err := s.ask(ctx, domain.Permission(name), f); err == nil || errors.Is(err, domain.ErrReauthRequired) {
			held = append(held, domain.Permission(name))
		}
	}

	return held
}
