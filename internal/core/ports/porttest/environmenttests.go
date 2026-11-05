package porttest

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
)

var (
	_ ports.EnvironmentTests = (*EnvironmentTests)(nil)
	_ ports.ToolLister       = (*ToolLister)(nil)
)

// EnvironmentTests keeps the latest environment tests in memory with the database's rules: one
// per factory, environment and server, organizations kept apart. It knows only the factories
// AddFactory registered.
type EnvironmentTests struct {
	mu        sync.Mutex
	factories map[domain.FactoryID]domain.OrgID
	rows      map[domain.OrgID]map[[3]string]domain.EnvironmentTest
}

// NewEnvironmentTests returns no tests and no factories.
func NewEnvironmentTests() *EnvironmentTests {
	return &EnvironmentTests{factories: map[domain.FactoryID]domain.OrgID{}, rows: map[domain.OrgID]map[[3]string]domain.EnvironmentTest{}}
}

// AddFactory registers factory as org's.
func (e *EnvironmentTests) AddFactory(org domain.OrgID, factory domain.FactoryID) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.factories[factory] = org
}

// Record implements ports.EnvironmentTests.
func (e *EnvironmentTests) Record(ctx context.Context, t domain.EnvironmentTest) error {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return domain.ErrNoOrg
	}

	if err := t.Check(); err != nil {
		return fmt.Errorf("record environment test: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.factories[t.Factory] != org {
		return fmt.Errorf("record environment test of factory %s: %w", t.Factory, ports.ErrNotFound)
	}

	if e.rows[org] == nil {
		e.rows[org] = map[[3]string]domain.EnvironmentTest{}
	}

	t.At = t.At.UTC()
	e.rows[org][[3]string{string(t.Factory), t.Environment, t.Server}] = t.Clone()

	return nil
}

// List implements ports.EnvironmentTests.
func (e *EnvironmentTests) List(ctx context.Context, factory domain.FactoryID) ([]domain.EnvironmentTest, error) {
	org, ok := domain.OrgFrom(ctx)
	if !ok {
		return nil, domain.ErrNoOrg
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	out := []domain.EnvironmentTest{}

	for _, key := range slices.SortedFunc(maps.Keys(e.rows[org]), func(a, b [3]string) int {
		return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]), cmp.Compare(a[2], b[2]))
	}) {
		if factory == "" || key[0] == string(factory) {
			out = append(out, e.rows[org][key].Clone())
		}
	}

	return out, nil
}

// ToolLister answers tools/list from a table by URL, and records the endpoints it was asked
// about. A URL neither served nor failed answers an error.
type ToolLister struct {
	mu     sync.Mutex
	tools  map[string][]ports.Tool
	errs   map[string]error
	called []ports.MCPEndpoint
}

// NewToolLister returns a lister that knows no server.
func NewToolLister() *ToolLister {
	return &ToolLister{tools: map[string][]ports.Tool{}, errs: map[string]error{}}
}

// Serve makes url answer tools, none of them read-only, in place of what it answered before.
func (l *ToolLister) Serve(url string, tools ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.tools[url] = nil
	for _, name := range tools {
		l.tools[url] = append(l.tools[url], ports.Tool{Name: name})
	}
}

// ServeReadOnly adds tools to what url answers, each marked read-only.
func (l *ToolLister) ServeReadOnly(url string, tools ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, name := range tools {
		l.tools[url] = append(l.tools[url], ports.Tool{Name: name, ReadOnly: true})
	}
}

// Fail makes url answer err.
func (l *ToolLister) Fail(url string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.errs[url] = err
}

// Called are the endpoints asked so far, in order.
func (l *ToolLister) Called() []ports.MCPEndpoint {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.called)
}

// ListTools implements ports.ToolLister.
func (l *ToolLister) ListTools(_ context.Context, e ports.MCPEndpoint) ([]ports.Tool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.called = append(l.called, ports.MCPEndpoint{URL: e.URL, Headers: maps.Clone(e.Headers)})

	if err := l.errs[e.URL]; err != nil {
		return nil, err
	}

	tools, ok := l.tools[e.URL]
	if !ok {
		return nil, fmt.Errorf("%s: no MCP server answers", e.URL)
	}

	out := slices.Clone(tools)
	slices.SortFunc(out, func(a, b ports.Tool) int { return cmp.Compare(a.Name, b.Name) })

	return out, nil
}
