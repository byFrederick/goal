package api

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/environmentcheck"
)

// The Access read model (ADR 0037 decision 16): what the organization's agents reach, computed
// on each request from every visible, unarchived factory's current published version, the
// environment tests and the secrets, with the organization-wide secret of its Slack
// notifications (decision 14). Nothing is stored for it.

// accessStatuses are the statuses from the worst; a row has the worst of its uses'.
var accessStatuses = []gen.AccessStatus{
	gen.AccessStatusMissing, gen.AccessStatusFailed, gen.AccessStatusExpired, gen.AccessStatusExpiring,
	gen.AccessStatusUntested, gen.AccessStatusTested, gen.AccessStatusOk,
}

// needsAttention are the statuses need_attention counts.
var needsAttention = []gen.AccessStatus{gen.AccessStatusMissing, gen.AccessStatusFailed, gen.AccessStatusExpired, gen.AccessStatusExpiring}

// The titles of the by=reach groups, by kind.
var reachTitles = map[gen.AccessRowKind]string{
	gen.AccessRowKindMcp: "MCP servers", gen.AccessRowKindEnv: "Variables", gen.AccessRowKindFile: "Files",
	gen.AccessRowKindSlack: "Notifications",
}

// slackRowName is the name of the organization-wide row of the Slack notifications' secret.
const slackRowName = "Slack notifications"

// accessUse is one thing an environment of a factory gives its agents.
type accessUse struct {
	kind        gen.AccessRowKind
	name        string
	factory     gen.AccessFactory
	environment string
	agents      []string
	secrets     []string
	// remote marks a remote MCP server, which a test covers; test is its latest.
	remote bool
	test   *domain.EnvironmentTest
}

func (s *server) GetAccess(ctx context.Context, req gen.GetAccessRequestObject) (gen.GetAccessResponseObject, error) {
	by := gen.AccessByReach
	if req.Params.By != nil {
		if by = gen.AccessBy(*req.Params.By); !by.Valid() {
			return nil, fmt.Errorf("%w: by is reach, factory or secret", errInvalid)
		}
	}

	var only domain.FactoryID

	if req.Params.Factory != nil {
		rec, err := s.definition(ctx, *req.Params.Factory)
		if err != nil {
			return nil, err
		}

		if !s.visible(ctx, rec.ID) {
			return nil, errFactoryNotFound
		}

		only = rec.ID
	}

	uses, secrets, err := s.accessUses(ctx, only)
	if err != nil {
		return nil, err
	}

	now := s.clock.Now()

	// The organization-wide rows belong to no factory, so one factory's Access has none.
	var org []gen.AccessRow

	if only == "" {
		if org, err = s.orgAccessRows(ctx, secrets, now); err != nil {
			return nil, err
		}
	}

	all := append(accessRows(uses, secrets, now), org...)
	out := gen.GetAccess200JSONResponse{By: by, Reached: len(all), Groups: []gen.AccessGroup{}}

	for _, r := range all {
		if slices.Contains(needsAttention, r.Status) {
			out.NeedAttention++
		}
	}

	out.Groups = accessGroups(by, uses, all, org, secrets, now)

	return out, nil
}

// orgAccessRows are the organization-wide rows, which have no factory uses: the secret of the
// organization's Slack notifications when the setting names one, its status from that secret
// alone.
func (s *server) orgAccessRows(ctx context.Context, secrets map[string]domain.Secret, now time.Time) ([]gen.AccessRow, error) {
	if s.notifySettings == nil {
		return nil, nil
	}

	set, err := s.notifySettings.NotificationSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("access: read the notification settings: %w", err)
	}

	if set.SlackSecret == "" {
		return nil, nil
	}

	use := accessUse{kind: gen.AccessRowKindSlack, name: slackRowName, secrets: []string{set.SlackSecret}}
	r := gen.AccessRow{Kind: use.kind, Name: use.name, Uses: []gen.AccessUse{}, Secrets: use.secrets, Status: useStatus(use, secrets, now)}
	secretDates(&r, use.secrets, secrets)

	return []gen.AccessRow{r}, nil
}

// accessGroups are the rows of uses, with the organization-wide rows org, grouped by kind (all,
// by reach), by factory (without org) or by secret.
func accessGroups(by gen.AccessBy, uses []accessUse, all, org []gen.AccessRow, secrets map[string]domain.Secret, now time.Time) []gen.AccessGroup {
	groups := []gen.AccessGroup{}

	switch by {
	case gen.AccessByFactory:
		factories := map[string]gen.AccessFactory{}
		for _, u := range uses {
			factories[u.factory.Id] = u.factory
		}

		for _, f := range slices.SortedFunc(maps.Values(factories), func(a, b gen.AccessFactory) int {
			return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Id, b.Id))
		}) {
			of := filter(uses, func(u accessUse) bool { return u.factory.Id == f.Id })
			groups = append(groups, gen.AccessGroup{Key: f.Id, Title: f.Name, Rows: accessRows(of, secrets, now)})
		}
	case gen.AccessBySecret:
		var names []string
		for _, u := range uses {
			names = append(names, u.secrets...)
		}

		for _, r := range org {
			names = append(names, r.Secrets...)
		}

		for _, name := range slices.Compact(slices.Sorted(slices.Values(names))) {
			of := filter(uses, func(u accessUse) bool { return slices.Contains(u.secrets, name) })
			rows := append(accessRows(of, secrets, now), filter(org, func(r gen.AccessRow) bool { return slices.Contains(r.Secrets, name) })...)
			groups = append(groups, gen.AccessGroup{Key: name, Title: name, Rows: rows})
		}
	case gen.AccessByReach:
		for _, kind := range accessKinds {
			if rows := filter(all, func(r gen.AccessRow) bool { return r.Kind == kind }); len(rows) > 0 {
				groups = append(groups, gen.AccessGroup{Key: string(kind), Title: reachTitles[kind], Rows: rows})
			}
		}
	}

	return groups
}

// accessUses are what the current versions of the factories the caller sees give their agents,
// only factory's when it is set, with the organization's secrets by name.
func (s *server) accessUses(ctx context.Context, only domain.FactoryID) ([]accessUse, map[string]domain.Secret, error) {
	recs, err := s.records(ctx)
	if err != nil {
		return nil, nil, err
	}

	list, err := s.secrets.List(ctx)
	if err != nil {
		return nil, nil, secretError(err)
	}

	secrets := make(map[string]domain.Secret, len(list))
	for _, sec := range list {
		secrets[sec.Name] = sec
	}

	kept, err := s.envTests.List(ctx, only)
	if err != nil {
		return nil, nil, err
	}

	tests := make(map[[3]string]domain.EnvironmentTest, len(kept))
	for _, t := range kept {
		tests[[3]string{string(t.Factory), t.Environment, t.Server}] = t
	}

	var uses []accessUse

	for _, f := range s.factories.Factories(ctx) {
		id := domain.FactoryID(f.Metadata.ID)
		if recs[id].Archived() || (only != "" && id != only) || !s.visible(ctx, id) {
			continue
		}

		agents := map[string][]string{}
		for _, a := range f.Spec.Agents {
			agents[a.EnvironmentName()] = append(agents[a.EnvironmentName()], a.Name)
		}

		for _, e := range f.Environments() {
			use := accessUse{factory: gen.AccessFactory{Id: string(id), Name: f.Metadata.Name}, environment: e.Name, agents: agents[e.Name]}
			uses = append(uses, environmentUses(use, e, tests)...)
		}
	}

	return uses, secrets, nil
}

// environmentUses are what environment e gives, each a copy of base: every MCP server, and each
// env variable and file that references a secret.
func environmentUses(base accessUse, e config.Environment, tests map[[3]string]domain.EnvironmentTest) []accessUse {
	var out []accessUse

	add := func(kind gen.AccessRowKind, name string, refs []string) accessUse {
		u := base
		u.kind, u.name, u.secrets = kind, name, refs

		u.agents = append([]string{}, base.agents...)

		return u
	}

	for _, name := range slices.Sorted(maps.Keys(e.Spec.MCP)) {
		m := e.Spec.MCP[name]
		u := add(gen.AccessRowKindMcp, name, environmentcheck.Referenced(mergeMaps(m.Headers, m.Env)))

		if m.URL != "" {
			u.remote = true
			if t, ok := tests[[3]string{base.factory.Id, e.Name, name}]; ok {
				u.test = &t
			}
		}

		out = append(out, u)
	}

	for _, name := range slices.Sorted(maps.Keys(e.Spec.Env)) {
		if refs := environmentcheck.Referenced(map[string]string{name: e.Spec.Env[name]}); len(refs) > 0 {
			out = append(out, add(gen.AccessRowKindEnv, name, refs))
		}
	}

	for _, p := range slices.Sorted(maps.Keys(e.Spec.Files)) {
		if refs := environmentcheck.Referenced(map[string]string{p: e.Spec.Files[p]}); len(refs) > 0 {
			out = append(out, add(gen.AccessRowKindFile, p, refs))
		}
	}

	return out
}

// accessRows are uses as rows, one per kind and name, by kind then name.
func accessRows(uses []accessUse, secrets map[string]domain.Secret, now time.Time) []gen.AccessRow {
	type key struct {
		kind gen.AccessRowKind
		name string
	}

	rows := map[key]*gen.AccessRow{}
	latest := map[key]*domain.EnvironmentTest{}

	for _, u := range uses {
		k := key{u.kind, u.name}

		r := rows[k]
		if r == nil {
			r = &gen.AccessRow{Kind: u.kind, Name: u.name, Uses: []gen.AccessUse{}, Secrets: []string{}, Status: gen.AccessStatusOk}
			rows[k] = r
		}

		addUse(r, u, useStatus(u, secrets, now), secrets)

		if t := u.test; t != nil && (latest[k] == nil || t.At.After(latest[k].At)) {
			latest[k] = t
		}
	}

	out := make([]gen.AccessRow, 0, len(rows))

	for _, k := range slices.SortedFunc(maps.Keys(rows), func(a, b key) int {
		return cmp.Or(cmp.Compare(slices.Index(accessKinds, a.kind), slices.Index(accessKinds, b.kind)), cmp.Compare(a.name, b.name))
	}) {
		r := rows[k]
		if t := latest[k]; t != nil {
			r.TestedAt = new(t.At)
			if !t.OK {
				r.TestError = new(t.Error)
			}
		}

		out = append(out, *r)
	}

	return out
}

// addUse adds u, of status, to row r: its status, secrets, latest use and soonest expiry.
func addUse(r *gen.AccessRow, u accessUse, status gen.AccessStatus, secrets map[string]domain.Secret) {
	out := gen.AccessUse{Factory: u.factory, Environment: u.environment, Agents: u.agents, Status: status}

	if t := u.test; t != nil {
		out.Test = &gen.AccessTest{At: t.At, Ok: t.OK, Tools: len(t.Tools)}
		if t.Error != "" {
			out.Test.Error = new(t.Error)
		}
	}

	r.Uses = append(r.Uses, out)
	r.Status = worse(r.Status, status)
	r.Secrets = nonNil(slices.Compact(slices.Sorted(slices.Values(append(r.Secrets, u.secrets...))))) // Sorted of none is nil
	secretDates(r, u.secrets, secrets)
}

// secretDates sets row r's latest use and soonest expiry from the secrets names.
func secretDates(r *gen.AccessRow, names []string, secrets map[string]domain.Secret) {
	for _, name := range names {
		sec, ok := secrets[name]
		if !ok {
			continue
		}

		if sec.LastUsedAt != nil && (r.LastUsedAt == nil || sec.LastUsedAt.After(*r.LastUsedAt)) {
			r.LastUsedAt = new(*sec.LastUsedAt)
		}

		if sec.ExpiresAt != nil && (r.ExpiresAt == nil || sec.ExpiresAt.Before(*r.ExpiresAt)) {
			r.ExpiresAt = new(*sec.ExpiresAt)
		}
	}
}

// accessKinds are the row kinds in the order rows list them.
var accessKinds = []gen.AccessRowKind{gen.AccessRowKindMcp, gen.AccessRowKindEnv, gen.AccessRowKindFile, gen.AccessRowKindSlack}

// useStatus is the status of one use: a secret it references is missing, its last test failed, a
// secret expired or is within its reminder days of its expiry, a remote server is untested or
// tested, or it is ok.
func useStatus(u accessUse, secrets map[string]domain.Secret, now time.Time) gen.AccessStatus {
	status := gen.AccessStatusOk

	switch {
	case u.remote && u.test == nil:
		status = gen.AccessStatusUntested
	case u.remote && !u.test.OK:
		status = gen.AccessStatusFailed
	case u.remote:
		status = gen.AccessStatusTested
	}

	for _, name := range u.secrets {
		sec, ok := secrets[name]

		switch {
		case !ok:
			status = worse(status, gen.AccessStatusMissing)
		case sec.ExpiresAt == nil:
		case !now.Before(*sec.ExpiresAt):
			status = worse(status, gen.AccessStatusExpired)
		case sec.RemindDays > 0 && !now.Before(sec.ExpiresAt.AddDate(0, 0, -sec.RemindDays)):
			status = worse(status, gen.AccessStatusExpiring)
		}
	}

	return status
}

// worse is the worse of two statuses.
func worse(a, b gen.AccessStatus) gen.AccessStatus {
	if slices.Index(accessStatuses, b) < slices.Index(accessStatuses, a) {
		return b
	}

	return a
}

// filter are the elements of s that keep holds for.
func filter[T any](s []T, keep func(T) bool) []T {
	var out []T

	for _, v := range s {
		if keep(v) {
			out = append(out, v)
		}
	}

	return out
}

// mergeMaps is a and b in one map.
func mergeMaps(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	maps.Copy(out, a)

	for k, v := range b {
		out["env:"+k] = v
	}

	return out
}
