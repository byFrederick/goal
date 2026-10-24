package main

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The levels a permission may have (api/permissions.yaml).
const (
	levelInstall = "install"
	levelOrg     = "org"
	levelFactory = "factory"
)

// table is api/permissions.yaml checked against itself and against the operations that name its
// permissions.
type table struct {
	OrgRoles     []string
	FactoryRoles []string // the ladder, lowest first
	Permissions  []permission
	// Scopes is every operationId's x-goal-scope.
	Scopes map[string]string
}

// permission is one entry of api/permissions.yaml.
type permission struct {
	Name          string              `yaml:"name"`
	Level         string              `yaml:"level"`
	OrgRoles      []string            `yaml:"org_roles"`
	FactoryRole   string              `yaml:"factory_role"`
	Keys          bool                `yaml:"keys"`
	PAT           bool                `yaml:"pat"`
	Fresh         bool                `yaml:"fresh"`
	PlatformAdmin bool                `yaml:"platform_admin"`
	BetterAuth    map[string][]string `yaml:"better_auth"`
}

type source struct {
	Version      int          `yaml:"version"`
	OrgRoles     []string     `yaml:"org_roles"`
	FactoryRoles []string     `yaml:"factory_roles"`
	Permissions  []permission `yaml:"permissions"`
}

// permissionName is a permission's form: a word, or resource:action.
var permissionName = regexp.MustCompile(`^[a-z][a-z_]*(:[a-z][a-z_]*)?$`)

// roleName is a role's form, also a TypeScript identifier.
var roleName = regexp.MustCompile(`^[a-z][a-z_]*$`)

// betterAuthName is the form of a Better Auth access-control resource or action.
var betterAuthName = regexp.MustCompile(`^[a-z][a-zA-Z]*$`)

// load reads and checks both sources. Permissions come out sorted by name.
func load(permissions, openapi []byte) (table, error) {
	var src source

	dec := yaml.NewDecoder(bytes.NewReader(permissions))
	dec.KnownFields(true)

	if err := dec.Decode(&src); err != nil {
		return table{}, fmt.Errorf("permissions: %w", err)
	}

	if src.Version != 1 {
		return table{}, fmt.Errorf("permissions: version %d, want 1", src.Version)
	}

	if err := distinct("org_roles", src.OrgRoles); err != nil {
		return table{}, err
	}

	if err := distinct("factory_roles", src.FactoryRoles); err != nil {
		return table{}, err
	}

	t := table{OrgRoles: src.OrgRoles, FactoryRoles: src.FactoryRoles}
	seen := map[string]bool{}

	for _, p := range src.Permissions {
		if seen[p.Name] {
			return table{}, fmt.Errorf("permission %q: listed twice", p.Name)
		}

		seen[p.Name] = true

		if err := t.check(p); err != nil {
			return table{}, fmt.Errorf("permission %q: %w", p.Name, err)
		}

		t.Permissions = append(t.Permissions, p)
	}

	slices.SortFunc(t.Permissions, func(a, b permission) int { return strings.Compare(a.Name, b.Name) })

	scopes, err := operationScopes(openapi, seen)
	if err != nil {
		return table{}, err
	}

	t.Scopes = scopes

	return t, nil
}

// check refuses a permission whose fields contradict its level or name unknown roles.
func (t table) check(p permission) error {
	if !permissionName.MatchString(p.Name) {
		return errors.New("a name is a word or resource:action in lowercase")
	}

	for _, r := range p.OrgRoles {
		if !slices.Contains(t.OrgRoles, r) {
			return fmt.Errorf("unknown org role %q", r)
		}
	}

	if err := t.checkLevel(p); err != nil {
		return err
	}

	if p.PlatformAdmin && p.Level != levelInstall {
		return errors.New("platform_admin is for install permissions")
	}

	if p.Fresh && p.Keys {
		return errors.New("keys never hold a fresh permission: an API key has no sign-in time")
	}

	if err := checkPAT(p); err != nil {
		return err
	}

	return checkBetterAuth(p)
}

// permAuthenticated is the one install permission personal tokens may carry: any valid principal.
const permAuthenticated = "authenticated"

// checkPAT refuses pat (personal tokens may carry it, spec 3 §4.5) where a token must never reach:
// a fresh permission (a token has no sign-in time), a platform admin's, or any install permission
// but authenticated (device approval, creating organizations).
func checkPAT(p permission) error {
	switch {
	case !p.PAT:
		return nil
	case p.Fresh:
		return errors.New("personal tokens never hold a fresh permission: a token has no sign-in time")
	case p.PlatformAdmin:
		return errors.New("personal tokens never hold a platform admin's permission")
	case p.Level == levelInstall && p.Name != permAuthenticated:
		return errors.New("personal tokens hold no install permission but authenticated")
	default:
		return nil
	}
}

// checkBetterAuth refuses better_auth statements outside an org permission (Better Auth's roles
// are the organization roles) and malformed resources or actions.
func checkBetterAuth(p permission) error {
	if len(p.BetterAuth) > 0 && p.Level != levelOrg {
		return errors.New("better_auth is for org permissions: Better Auth's roles are the organization roles")
	}

	for resource, actions := range p.BetterAuth {
		if !betterAuthName.MatchString(resource) || len(actions) == 0 {
			return fmt.Errorf("better_auth resource %q: a lowercase name with at least one action", resource)
		}

		for _, a := range actions {
			if !betterAuthName.MatchString(a) {
				return fmt.Errorf("better_auth %s action %q: a lowercase name", resource, a)
			}
		}
	}

	return nil
}

// checkLevel refuses roles that the permission's level does not take.
func (t table) checkLevel(p permission) error {
	orgRoles, factoryRole := len(p.OrgRoles) > 0, p.FactoryRole != ""

	switch p.Level {
	case levelInstall:
		if orgRoles || factoryRole {
			return errors.New("an install permission has no org_roles or factory_role")
		}
	case levelOrg:
		if !orgRoles || factoryRole {
			return errors.New("an org permission names its org_roles and no factory_role")
		}
	case levelFactory:
		if orgRoles || !slices.Contains(t.FactoryRoles, p.FactoryRole) {
			return errors.New("a factory permission names one of factory_roles and no org_roles (owners and admins hold it)")
		}
	default:
		return fmt.Errorf("unknown level %q (install, org or factory)", p.Level)
	}

	return nil
}

// httpMethods are the keys of an OpenAPI path item that are operations.
var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// operationScopes reads every operation's x-goal-scope and refuses one that has none or names a
// permission not in known.
func operationScopes(openapi []byte, known map[string]bool) (map[string]string, error) {
	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}

	if err := yaml.Unmarshal(openapi, &doc); err != nil {
		return nil, fmt.Errorf("openapi: %w", err)
	}

	scopes := map[string]string{}

	for _, path := range slices.Sorted(maps.Keys(doc.Paths)) {
		for _, method := range httpMethods {
			node, ok := doc.Paths[path][method]
			if !ok {
				continue
			}

			var op struct {
				ID    string `yaml:"operationId"`
				Scope string `yaml:"x-goal-scope"`
			}

			if err := node.Decode(&op); err != nil {
				return nil, fmt.Errorf("openapi %s %s: %w", method, path, err)
			}

			switch {
			case op.ID == "":
				return nil, fmt.Errorf("openapi %s %s: no operationId", method, path)
			case scopes[op.ID] != "":
				return nil, fmt.Errorf("openapi: operationId %s used twice", op.ID)
			case op.Scope == "":
				return nil, fmt.Errorf("operation %s has no x-goal-scope: give it one of api/permissions.yaml", op.ID)
			case !known[op.Scope]:
				return nil, fmt.Errorf("operation %s: x-goal-scope %q is not in api/permissions.yaml", op.ID, op.Scope)
			}

			scopes[op.ID] = op.Scope
		}
	}

	return scopes, nil
}

func distinct(field string, names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("permissions: %s is empty", field)
	}

	for i, n := range names {
		if !roleName.MatchString(n) || slices.Contains(names[:i], n) {
			return fmt.Errorf("permissions: %s: %q is not a lowercase name or is listed twice", field, n)
		}
	}

	return nil
}
