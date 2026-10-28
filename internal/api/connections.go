package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

// The organization's connections and their kinds (spec 4 §8.2): members read them, owners and
// admins change and test them (connection:manage, a fresh sign-in). Secret values are write-only:
// a request may carry them, no response, log line, event or audit entry does.

// KindParser reads a connection kind file, YAML or JSON (connkind.Parse); its error wraps
// domain.ErrInvalid and names every broken rule.
type KindParser func(data []byte) (domain.ConnectionKind, error)

// Limits of a connection's test (spec 4 §8.2): goald waits connectionTestTimeout for its answer
// and reads at most connectionTestBody of it.
const (
	connectionTestTimeout = 10 * time.Second
	connectionTestBody    = 64 << 10
	// repositoriesTimeout bounds one listing of a connection's repositories: up to 103 code host
	// requests, 8 at a time.
	repositoriesTimeout = time.Minute
)

// Errors of the connection routes, answered with their codes by connectionProblem.
var (
	errConnectionNotFound = errors.New("connection not found")
	errKindNotFound       = errors.New("connection kind not found")
	errConnectionTaken    = errors.New("connection name taken")
	errKindTaken          = errors.New("connection kind name taken")
	errConnectionInUse    = errors.New("connection in use")
	errKindInUse          = errors.New("connection kind in use")
	errNotTestable        = errors.New("connection not testable")
	errKindInvalid        = errors.New("connection kind invalid")
	// The refusals of a listing of a connection's repositories: a kind that is no code host goald
	// reaches, a code host that cannot list them, an owner it does not know, any other failure.
	errNotCodeHost      = errors.New("connection is not a code host")
	errReposUnsupported = errors.New("the code host cannot list repositories")
	errOwnerNotFound    = errors.New("owner not found")
	errCodeHost         = errors.New("the code host failed")
	// errConnectionInvalid is a connection the rules refuse (400): field-unknown and
	// field-required when a field rule says which.
	errConnectionInvalid = errors.New("connection invalid")
)

// connectionProblem answers the connection rules with their codes.
func connectionProblem(err error) (gen.Problem, bool) {
	answers := []struct {
		err    error
		status int
		code   string
	}{
		{errConnectionNotFound, http.StatusNotFound, "connection-not-found"},
		{errKindNotFound, http.StatusNotFound, "connection-kind-not-found"},
		{errConnectionTaken, http.StatusConflict, "connection-name-taken"},
		{errKindTaken, http.StatusConflict, "connection-kind-name-taken"},
		{errConnectionInUse, http.StatusConflict, "connection-in-use"},
		{errKindInUse, http.StatusConflict, "connection-kind-in-use"},
		{errNotTestable, http.StatusConflict, "connection-not-testable"},
		{errKindInvalid, http.StatusBadRequest, "connection-kind-invalid"},
		{errNotCodeHost, http.StatusUnprocessableEntity, "connection-not-code-host"},
		{errReposUnsupported, http.StatusUnprocessableEntity, "repositories-not-supported"},
		{errOwnerNotFound, http.StatusNotFound, "owner-not-found"},
		{errCodeHost, http.StatusBadGateway, "code-host-error"},
		{domain.ErrFieldUnknown, http.StatusBadRequest, "field-unknown"},
		{domain.ErrFieldRequired, http.StatusBadRequest, "field-required"},
		{errConnectionInvalid, http.StatusBadRequest, ""},
	}

	for _, a := range answers {
		if errors.Is(err, a.err) {
			p := detailed(a.status, err)
			p.Code = optional(a.code)

			return p, true
		}
	}

	if errors.Is(err, ports.ErrNoMasterKey) {
		detail := "connections with secrets are unavailable: goald has no master key (set GOAL_MASTER_KEYS; .env.example shows how)"

		return gen.Problem{
			Title: http.StatusText(http.StatusServiceUnavailable), Status: http.StatusServiceUnavailable, Detail: &detail,
			Code: optional("connections-unavailable"),
		}, true
	}

	return gen.Problem{}, false
}

// connectionError is err of a change of the connection name as the API answers it: an unknown
// connection, a taken name, a rule broken (ports.Connections' domain.ErrInvalid is a 400 here).
// An unknown kind is notFound's.
func connectionError(err error, name string, notFound error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrNotFound):
		return fmt.Errorf("%w: %w", notFound, err)
	case errors.Is(err, ports.ErrNameTaken):
		return fmt.Errorf("%w: %w", errConnectionTaken, err)
	case errors.Is(err, domain.ErrFieldUnknown), errors.Is(err, domain.ErrFieldRequired):
		return err
	case errors.Is(err, domain.ErrInvalid):
		return fmt.Errorf("%w: %w", errConnectionInvalid, err)
	case errors.Is(err, ports.ErrConflict):
		return fmt.Errorf("%w: connection %q: knowledge sources use it: %w", errConnectionInUse, name, err)
	default:
		return err
	}
}

func (s *server) ListConnectionKinds(ctx context.Context, _ gen.ListConnectionKindsRequestObject) (gen.ListConnectionKindsResponseObject, error) {
	kinds, err := s.connections.Kinds(ctx)
	if err != nil {
		return nil, err
	}

	out := gen.ListConnectionKinds200JSONResponse{Kinds: make([]gen.ConnectionKind, 0, len(kinds))}
	for _, k := range kinds {
		out.Kinds = append(out.Kinds, kindOut(k))
	}

	return out, nil
}

// PutConnectionKind creates or replaces an organization kind from its file, parsed as goald's own
// kinds are (connkind.Parse).
func (s *server) PutConnectionKind(ctx context.Context, req gen.PutConnectionKindRequestObject) (gen.PutConnectionKindResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("put connection kind: the request has no actor")
	}

	raw, err := json.Marshal(req.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalid, err)
	}

	k, err := s.parseKind(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errKindInvalid, err)
	}

	if k.Name != req.Kind {
		return nil, fmt.Errorf("%w: metadata.name %q is not the kind %q of the path", errKindInvalid, k.Name, req.Kind)
	}

	switch err := s.connections.PutKind(ctx, k, by); {
	case errors.Is(err, ports.ErrNameTaken):
		return nil, fmt.Errorf("%w: %w", errKindTaken, err)
	case errors.Is(err, domain.ErrInvalid):
		return nil, fmt.Errorf("%w: %w", errKindInvalid, err)
	case errors.Is(err, ports.ErrConflict):
		return nil, fmt.Errorf("%w: %w", errKindInUse, err)
	case err != nil:
		return nil, err
	}

	got, err := s.connections.Kind(ctx, k.Name)
	if err != nil {
		return nil, err
	}

	return gen.PutConnectionKind200JSONResponse(kindOut(got)), nil
}

func (s *server) DeleteConnectionKind(ctx context.Context, req gen.DeleteConnectionKindRequestObject) (gen.DeleteConnectionKindResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("delete connection kind: the request has no actor")
	}

	switch err := s.connections.DeleteKind(ctx, req.Kind, by); {
	case errors.Is(err, ports.ErrNotFound):
		return nil, fmt.Errorf("%w: the organization has no kind %q", errKindNotFound, req.Kind)
	case errors.Is(err, ports.ErrConflict):
		return nil, fmt.Errorf("%w: connections of kind %q exist: %w", errKindInUse, req.Kind, err)
	case err != nil:
		return nil, err
	}

	return gen.DeleteConnectionKind204Response{}, nil
}

func (s *server) ListConnections(ctx context.Context, req gen.ListConnectionsRequestObject) (gen.ListConnectionsResponseObject, error) {
	var named []string

	ref := deref(req.Params.Factory)
	if ref != "" {
		f, err := s.factory(ctx, ref)
		if err != nil {
			return nil, err
		}

		named = f.ConnectionNames()
	}

	conns, err := s.connections.List(ctx)
	if err != nil {
		return nil, err
	}

	kinds, err := s.kindsByName(ctx)
	if err != nil {
		return nil, err
	}

	sources, err := s.connectionSources(ctx)
	if err != nil {
		return nil, err
	}

	users, err := s.connectionUsers(ctx)
	if err != nil {
		return nil, err
	}

	waiting, err := s.waitingApprovals(ctx)
	if err != nil {
		return nil, err
	}

	kind := deref(req.Params.Kind)
	out := gen.ListConnections200JSONResponse{Connections: []gen.Connection{}}

	for _, c := range conns {
		if (kind != "" && c.Kind != kind) || (ref != "" && !slices.Contains(named, c.Name)) {
			continue
		}

		out.Connections = append(out.Connections, s.connectionOut(ctx, c, kinds[c.Kind], users[c.Name], sources[c.Name], waiting[c.Name]))
	}

	return out, nil
}

func (s *server) CreateConnection(ctx context.Context, req gen.CreateConnectionRequestObject) (gen.CreateConnectionResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("create connection: the request has no actor")
	}

	b := req.Body

	c, err := s.connections.Create(ctx, domain.NewConnection{
		Name: b.Name, Kind: b.Kind, Description: deref(b.Description), Fields: deref(b.Fields), Secrets: deref(b.Secrets),
	}, by)
	if err != nil {
		return nil, connectionError(err, b.Name, errKindNotFound)
	}

	kind, err := s.connections.Kind(ctx, c.Kind)
	if err != nil {
		return nil, err
	}

	waiting, err := s.waitingApprovals(ctx)
	if err != nil {
		return nil, err
	}

	return gen.CreateConnection201JSONResponse(s.connectionOut(ctx, c, kind, nil, nil, waiting[c.Name])), nil
}

// GetConnection is one connection with the env names a run gets from it and its kind's docs.
func (s *server) GetConnection(ctx context.Context, req gen.GetConnectionRequestObject) (gen.GetConnectionResponseObject, error) {
	c, err := s.connections.Get(ctx, req.Connection)
	if err != nil {
		return nil, connectionError(err, req.Connection, errConnectionNotFound)
	}

	kind, err := s.connections.Kind(ctx, c.Kind)
	if err != nil {
		return nil, err
	}

	sources, err := s.connectionSources(ctx)
	if err != nil {
		return nil, err
	}

	users, err := s.connectionUsers(ctx)
	if err != nil {
		return nil, err
	}

	waiting, err := s.waitingApprovals(ctx)
	if err != nil {
		return nil, err
	}

	out := s.connectionOut(ctx, c, kind, users[c.Name], sources[c.Name], waiting[c.Name])
	env := append([]string{}, kind.Outline(c).Env...)
	out.Env, out.Docs = &env, optional(kind.Spec.Docs)

	return gen.GetConnection200JSONResponse(out), nil
}

// UpdateConnection renames a connection or changes its description, fields, secrets or tool
// modes; a rename waits until no current version names it, and modes need a kind with an MCP
// server (ADR 0031 decision 2).
func (s *server) UpdateConnection(ctx context.Context, req gen.UpdateConnectionRequestObject) (gen.UpdateConnectionResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("update connection: the request has no actor")
	}

	b := req.Body

	users, err := s.connectionUsers(ctx)
	if err != nil {
		return nil, err
	}

	if b.Name != nil && *b.Name != req.Connection {
		if err := inUse(req.Connection, users[req.Connection]); err != nil {
			return nil, err
		}
	}

	p, err := s.patchIn(ctx, req.Connection, b)
	if err != nil {
		return nil, err
	}

	c, err := s.connections.Update(ctx, req.Connection, p, by)
	if err != nil {
		return nil, connectionError(err, req.Connection, errConnectionNotFound)
	}

	kind, err := s.connections.Kind(ctx, c.Kind)
	if err != nil {
		return nil, err
	}

	sources, err := s.connectionSources(ctx)
	if err != nil {
		return nil, err
	}

	waiting, err := s.waitingApprovals(ctx)
	if err != nil {
		return nil, err
	}

	return gen.UpdateConnection200JSONResponse(s.connectionOut(ctx, c, kind, users[c.Name], sources[c.Name], waiting[c.Name])), nil
}

// patchIn is the change b asks of the connection name: fields, when present, replace every
// non-secret value, and tools every tool's mode.
func (s *server) patchIn(ctx context.Context, name string, b *gen.ConnectionPatch) (domain.ConnectionPatch, error) {
	p := domain.ConnectionPatch{Name: b.Name, Description: b.Description, Secrets: deref(b.Secrets)}
	if b.Fields != nil {
		p.Fields = *b.Fields
		if p.Fields == nil {
			p.Fields = map[string]string{}
		}
	}

	if b.Tools != nil {
		modes, err := s.toolModesIn(ctx, name, *b.Tools)
		if err != nil {
			return domain.ConnectionPatch{}, err
		}

		p.ToolModes = modes
	}

	return p, nil
}

// toolModesIn are the modes of a patch's tools for the connection name, which every tool's mode
// replaces: refused (400) for a kind without an MCP server, a tool named twice, a malformed name
// or an unknown mode.
func (s *server) toolModesIn(ctx context.Context, name string, tools []gen.ConnectionTool) (map[string]domain.ConnectionToolMode, error) {
	c, err := s.connections.Get(ctx, name)
	if err != nil {
		return nil, connectionError(err, name, errConnectionNotFound)
	}

	kind, err := s.connections.Kind(ctx, c.Kind)
	if err != nil {
		return nil, err
	}

	if kind.Spec.MCP == nil {
		return nil, fmt.Errorf("%w: connection %s is of kind %s, which has no MCP server and so no tools", errConnectionInvalid, c.Name, kind.Name)
	}

	modes := make(map[string]domain.ConnectionToolMode, len(tools))

	for _, t := range tools {
		if _, twice := modes[t.Name]; twice {
			return nil, fmt.Errorf("%w: tool %q is named twice", errConnectionInvalid, t.Name)
		}

		modes[t.Name] = domain.ConnectionToolMode(t.Mode)
	}

	if err := domain.CheckToolModes(modes); err != nil {
		return nil, fmt.Errorf("%w: %w", errConnectionInvalid, err)
	}

	return modes, nil
}

// waitingApprovals are the organization's open tool approvals by connection name.
func (s *server) waitingApprovals(ctx context.Context) (map[string]int, error) {
	waiting, err := s.items.OpenToolApprovals(ctx)
	if err != nil {
		return nil, fmt.Errorf("count the open tool approvals: %w", err)
	}

	return waiting, nil
}

// DeleteConnection deletes a connection no current version names (and no knowledge source uses).
func (s *server) DeleteConnection(ctx context.Context, req gen.DeleteConnectionRequestObject) (gen.DeleteConnectionResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("delete connection: the request has no actor")
	}

	users, err := s.connectionUsers(ctx)
	if err != nil {
		return nil, err
	}

	if err := inUse(req.Connection, users[req.Connection]); err != nil {
		return nil, err
	}

	if err := s.connections.Delete(ctx, req.Connection, by); err != nil {
		return nil, connectionError(err, req.Connection, errConnectionNotFound)
	}

	return gen.DeleteConnection204Response{}, nil
}

// TestConnection sends the kind's test request with the connection's values through goald's
// connection transport (public addresses only), records the outcome and answers it. A test that
// fails is an answer, not an error.
func (s *server) TestConnection(ctx context.Context, req gen.TestConnectionRequestObject) (gen.TestConnectionResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("test connection: the request has no actor")
	}

	rc, err := s.connections.Open(ctx, req.Connection)
	if err != nil {
		return nil, connectionError(err, req.Connection, errConnectionNotFound)
	}

	if rc.Kind.Spec.Test == nil {
		return nil, fmt.Errorf("%w: kind %s has no test", errNotTestable, rc.Kind.Name)
	}

	status, err := s.probe(ctx, rc)
	r := domain.ConnectionTest{At: s.clock.Now().UTC(), OK: err == nil}

	if err != nil {
		r.Error = scrub(err.Error(), rc)
	}

	if err := s.connections.RecordTest(ctx, req.Connection, r, by); err != nil {
		return nil, connectionError(err, req.Connection, errConnectionNotFound)
	}

	out := testOut(r)
	if status > 0 {
		out.Status = &status
	}

	return gen.TestConnection200JSONResponse(out), nil
}

// ListConnectionRepositories lists what a code host connection reaches with its credential: the
// connection is resolved for the caller (audited as connection.used) only once its kind names a
// code host goald has. The code host's refusals are answered scrubbed of the connection's secrets.
func (s *server) ListConnectionRepositories(ctx context.Context, req gen.ListConnectionRepositoriesRequestObject) (gen.ListConnectionRepositoriesResponseObject, error) {
	by, ok := domain.ActorFrom(ctx)
	if !ok {
		return nil, errors.New("list connection repositories: the request has no actor")
	}

	c, err := s.connections.Get(ctx, req.Connection)
	if err != nil {
		return nil, connectionError(err, req.Connection, errConnectionNotFound)
	}

	kind, err := s.connections.Kind(ctx, c.Kind)
	if err != nil {
		return nil, err
	}

	ch, ok := s.codehosts[kind.Spec.CodeHost]
	if kind.Spec.CodeHost == "" || !ok {
		return nil, fmt.Errorf("%w: connection %s is of kind %s, which names no code host goald reaches", errNotCodeHost, c.Name, kind.Name)
	}

	resolved, err := s.connections.Resolve(ctx, []string{c.Name}, domain.ConnectionUse{By: by})
	if err != nil {
		return nil, connectionError(err, req.Connection, errConnectionNotFound)
	}

	rc := resolved[0]

	ctx, cancel := context.WithTimeout(ctx, repositoriesTimeout)
	defer cancel()

	list, err := ch.Repositories(ctx, rc, deref(req.Params.Owner))

	switch {
	case errors.Is(err, errors.ErrUnsupported):
		return nil, fmt.Errorf("%w: %s (kind %s)", errReposUnsupported, kind.Spec.CodeHost, kind.Name)
	case errors.Is(err, ports.ErrNotFound):
		return nil, fmt.Errorf("%w: %s", errOwnerNotFound, scrub(err.Error(), rc))
	case err != nil:
		return nil, fmt.Errorf("%w: %s", errCodeHost, scrub(err.Error(), rc))
	}

	return gen.ListConnectionRepositories200JSONResponse(repositoriesOut(list)), nil
}

// repositoriesOut is list as the contract has it.
func repositoriesOut(list ports.RepositoryList) gen.RepositoryList {
	out := gen.RepositoryList{
		Login: list.Login, Owner: list.Owner, Truncated: list.Truncated,
		Owners:       make([]gen.RepositoryOwner, 0, len(list.Owners)),
		Repositories: make([]gen.Repository, 0, len(list.Repositories)),
	}

	for _, o := range list.Owners {
		out.Owners = append(out.Owners, gen.RepositoryOwner{Login: o.Login, Kind: gen.RepositoryOwnerKind(o.Kind)})
	}

	for _, r := range list.Repositories {
		out.Repositories = append(out.Repositories, gen.Repository{
			FullName: r.FullName, CloneUrl: r.CloneURL, Languages: append([]string{}, r.Languages...), UpdatedAt: r.UpdatedAt,
		})
	}

	return out
}

// probe sends rc's test request and returns the status it got; an error when there is none, or
// it is not the one the kind expects.
func (s *server) probe(ctx context.Context, rc domain.ResolvedConnection) (int, error) {
	t, err := rc.Kind.RenderTest(rc.Values)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(ctx, connectionTestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, t.Method, t.URL, http.NoBody)
	if err != nil {
		return 0, err
	}

	for name, v := range t.Headers {
		req.Header.Set(name, v)
	}

	if t.Basic != nil {
		req.SetBasicAuth(t.Basic.Username, t.Basic.Password)
	}

	res, err := s.connectionClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = res.Body.Close() }()

	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, connectionTestBody))

	if res.StatusCode != t.Expect {
		return res.StatusCode, fmt.Errorf("%s %s answered %d, not %d", t.Method, req.URL.Redacted(), res.StatusCode, t.Expect)
	}

	return res.StatusCode, nil
}

// noRedirects makes the connection client answer a redirect as it came: a test follows none.
func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// scrub replaces rc's secret values in msg: a test's error reaches a response, the audit log and
// the database, which must never hold one (a kind puts no secret in a URL, but an answer might
// echo it).
func scrub(msg string, rc domain.ResolvedConnection) string {
	for _, v := range rc.SecretValues() {
		msg = strings.ReplaceAll(msg, v, "[secret]")
	}

	return msg
}

// connectionUsers are the factories goald runs for the organization, by the connections their
// current versions name; archived factories name none.
func (s *server) connectionUsers(ctx context.Context) (map[string][]factoryUse, error) {
	return s.usersBy(ctx, (*config.Factory).ConnectionNames)
}

// usersBy are the factories goald runs for the organization that are not archived, by the
// names each one's current version uses (names). A factory the store cannot be asked about
// fails the whole answer: an archived one is never assumed, nor a live one dropped.
func (s *server) usersBy(ctx context.Context, names func(*config.Factory) []string) (map[string][]factoryUse, error) {
	recs, err := s.records(ctx)
	if err != nil {
		return nil, err
	}

	out := map[string][]factoryUse{}

	for _, f := range s.factories.Factories(ctx) {
		if recs[domain.FactoryID(f.Metadata.ID)].Archived() {
			continue
		}

		for _, name := range names(f) {
			out[name] = append(out[name], factoryUse{id: domain.FactoryID(f.Metadata.ID), name: f.Metadata.Name})
		}
	}

	return out, nil
}

// factoryUse is a factory that names a connection.
type factoryUse struct {
	id   domain.FactoryID
	name string
}

// inUse refuses a change of the connection name while factories name it, naming them.
func inUse(name string, by []factoryUse) error {
	if len(by) == 0 {
		return nil
	}

	names := make([]string, 0, len(by))
	for _, f := range by {
		names = append(names, f.name)
	}

	return fmt.Errorf("%w: connection %q is named by the current version of %s; publish them without it first",
		errConnectionInUse, name, strings.Join(names, ", "))
}

// kindsByName are the kinds the organization sees, by name.
func (s *server) kindsByName(ctx context.Context) (map[string]domain.ConnectionKind, error) {
	kinds, err := s.connections.Kinds(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[string]domain.ConnectionKind, len(kinds))
	for _, k := range kinds {
		out[k.Name] = k
	}

	return out, nil
}

// connectionOut is a connection as the contract has it, never a secret value: what uses it (the
// factories the caller sees among by, the knowledge sources that read it), its status, its tools
// and the tool approvals waiting on it.
func (s *server) connectionOut(ctx context.Context, c domain.Connection, kind domain.ConnectionKind, by []factoryUse, sources []string, waiting int) gen.Connection {
	out := gen.Connection{
		Id: string(c.ID), Name: c.Name, Kind: c.Kind, Description: c.Description, Fields: map[string]string{},
		SecretsSet: slices.Clone(c.SecretFields), Status: connectionStatus(c, kind), LastUsedAt: c.LastUsedAt,
		Tools: toolsOut(c), WaitingApprovals: waiting,
		UsedBy:    gen.ConnectionUsers{Factories: []gen.ConnectionFactory{}, KnowledgeSources: append([]string{}, sources...)},
		CreatedBy: s.actorOut(c.CreatedBy), CreatedAt: c.CreatedAt, UpdatedBy: s.actorOut(c.UpdatedBy), UpdatedAt: c.UpdatedAt,
	}

	maps.Copy(out.Fields, c.Fields)

	if out.SecretsSet == nil {
		out.SecretsSet = []string{}
	}

	for _, f := range by {
		if s.visible(ctx, f.id) {
			out.UsedBy.Factories = append(out.UsedBy.Factories, gen.ConnectionFactory{Id: string(f.id), Name: f.name})
		}
	}

	if c.LastTest != nil {
		t := testOut(*c.LastTest)
		out.LastTest = &t
	}

	return out
}

// toolsOut are c's tools sorted by name: those its last test listed and those with a mode, a tool
// without one being allow (ADR 0031 decisions 1 and 2).
func toolsOut(c domain.Connection) []gen.ConnectionTool {
	var names []string
	if c.LastTest != nil {
		names = slices.Clone(c.LastTest.Tools)
	}

	names = slices.Compact(slices.Sorted(slices.Values(append(names, slices.Collect(maps.Keys(c.ToolModes))...))))
	out := make([]gen.ConnectionTool, 0, len(names))

	for _, name := range names {
		mode, ok := c.ToolModes[name]
		if !ok {
			mode = domain.ToolModeAllow
		}

		out = append(out, gen.ConnectionTool{Name: name, Mode: gen.ConnectionToolMode(mode)})
	}

	return out
}

// connectionStatus is missing-secret while a required secret field of the kind is not set, else
// the last test's outcome.
func connectionStatus(c domain.Connection, kind domain.ConnectionKind) gen.ConnectionStatus {
	missing := slices.ContainsFunc(kind.Spec.Fields, func(f domain.KindField) bool {
		return f.Secret && f.Required && !slices.Contains(c.SecretFields, f.Name)
	})

	switch {
	case missing:
		return gen.ConnectionStatusMissingSecret
	case c.LastTest == nil:
		return gen.ConnectionStatusUntested
	case c.LastTest.OK:
		return gen.ConnectionStatusOk
	default:
		return gen.ConnectionStatusFailing
	}
}

func testOut(t domain.ConnectionTest) gen.ConnectionTestResult {
	return gen.ConnectionTestResult{Ok: t.OK, Error: optional(t.Error), At: t.At}
}

// kindOut is a kind as the contract has it: its fields, modes, hooks and docs.
func kindOut(k domain.ConnectionKind) gen.ConnectionKind {
	s := k.Spec
	out := gen.ConnectionKind{
		Name: k.Name, Title: k.Title, Description: optional(k.Description), Platform: k.Platform, Fields: make([]gen.ConnectionKindField, 0, len(s.Fields)),
		Modes: []gen.ConnectionKindModes{}, Model: s.Model, CodeHost: optional(s.CodeHost), Mint: optional(s.Mint), Sync: optional(s.Sync),
		Testable: s.Test != nil, Env: []string{}, Docs: optional(s.Docs),
	}

	for _, f := range s.Fields {
		out.Fields = append(out.Fields, gen.ConnectionKindField{
			Name: f.Name, Title: f.Title, Description: optional(f.Description), Secret: f.Secret, Required: f.Required, Default: optional(f.Default),
		})
	}

	for name := range s.Env {
		out.Env = append(out.Env, name)
	}

	slices.Sort(out.Env)

	for _, m := range []struct {
		on   bool
		mode gen.ConnectionKindModes
	}{
		{len(s.Env) > 0, gen.ConnectionKindModesEnv},
		{s.MCP != nil, gen.ConnectionKindModesMcp},
		{s.CLI != nil, gen.ConnectionKindModesCli},
		{s.Git != nil, gen.ConnectionKindModesGit},
	} {
		if m.on {
			out.Modes = append(out.Modes, m.mode)
		}
	}

	return out
}
