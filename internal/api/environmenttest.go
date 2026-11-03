package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strconv"

	"github.com/byFrederick/goal/internal/api/gen"
	"github.com/byFrederick/goal/internal/authoring"
	"github.com/byFrederick/goal/internal/config"
	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/environmentcheck"
)

// Environment tests (ADR 0037 decision 9): POST .../environments/{env}/test calls each remote MCP
// server's tools/list with its headers' {{secret}} references opened inside goald, or a
// template's MCP servers and HTTP test with the secrets the body puts in its slots, and keeps
// each server's result. Opened values reach the ToolLister and the test request only: every
// error is scrubbed of them before it is answered or kept.

// Errors of the environment test route, answered with their codes by environmentTestProblem.
var (
	errEnvironmentNotFound       = errors.New("environment not found")
	errEnvironmentServerNotFound = errors.New("environment server not found")
	errTemplateNotFound          = errors.New("template not found")
	errEnvironmentTestInvalid    = errors.New("environment test invalid")
)

// environmentName is the form of an environment's and an MCP server's name.
var environmentName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,38}[a-z0-9]$`)

// environmentTestProblem answers the environment test rules with their codes.
func environmentTestProblem(err error) (gen.Problem, bool) {
	for _, a := range []struct {
		err    error
		status int
		code   string
	}{
		{errEnvironmentNotFound, http.StatusNotFound, "environment-not-found"},
		{errEnvironmentServerNotFound, http.StatusNotFound, "environment-server-not-found"},
		{errTemplateNotFound, http.StatusNotFound, "template-not-found"},
		{errEnvironmentTestInvalid, http.StatusBadRequest, "environment-test-invalid"},
	} {
		if errors.Is(err, a.err) {
			p := detailed(a.status, err)
			p.Code = optional(a.code)

			return p, true
		}
	}

	return gen.Problem{}, false
}

func (s *server) TestEnvironment(ctx context.Context, req gen.TestEnvironmentRequestObject) (gen.TestEnvironmentResponseObject, error) {
	body := gen.EnvironmentTestRequest{}
	if req.Body != nil {
		body = *req.Body
	}

	rec, err := s.definition(ctx, req.Factory)
	if err != nil {
		return nil, err
	}

	// The draft by default, or the version that runs while none is open.
	sel := authoring.VersionSelector{Kind: authoring.SelectDraft}
	if rec.Draft == nil {
		sel.Kind = authoring.SelectCurrent
	}

	if body.Version != nil {
		if sel, err = authoring.ParseVersion(*body.Version); err != nil {
			return nil, err
		}
	}

	env, err := s.readEnvironment(ctx, rec.ID, sel, req.Environment)
	if err != nil {
		return nil, err
	}

	t := envTest{s: s, factory: rec.ID, environment: env.Name}
	out := gen.EnvironmentTestResult{Environment: env.Name, Version: versionLabel(sel, rec), Servers: []gen.ServerTest{}}

	if body.Template != nil {
		return t.template(ctx, *body.Template, deref(body.Server), out)
	}

	names := slices.Sorted(maps.Keys(env.Spec.MCP))
	if body.Server != nil {
		if _, ok := env.Spec.MCP[*body.Server]; !ok {
			return nil, fmt.Errorf("%w: environment %s has no MCP server %q", errEnvironmentServerNotFound, env.Name, *body.Server)
		}

		names = []string{*body.Server}
	}

	for _, name := range names {
		m := env.Spec.MCP[name]
		if m.URL == "" {
			out.Servers = append(out.Servers, gen.ServerTest{Server: name, Tools: []string{}})

			continue
		}

		r, err := t.server(ctx, name, m.URL, m.Headers, nil)
		if err != nil {
			return nil, err
		}

		out.Servers = append(out.Servers, r)
	}

	return gen.TestEnvironment200JSONResponse(out), nil
}

// readEnvironment reads environment name of factory's version sel: errEnvironmentNotFound when it
// has none, errEnvironmentTestInvalid when its file is broken.
func (s *server) readEnvironment(ctx context.Context, factory domain.FactoryID, sel authoring.VersionSelector, name string) (config.Environment, error) {
	if !environmentName.MatchString(name) {
		return config.Environment{}, fmt.Errorf("%w: %q is not an environment's name", errEnvironmentNotFound, name)
	}

	for _, ext := range []string{".yaml", ".yml"} {
		f, err := s.defs.VersionFile(ctx, factory, sel, config.EnvironmentsDir+"/"+name+ext)
		if errors.Is(err, authoring.ErrFileNotFound) {
			continue
		}

		if err != nil {
			return config.Environment{}, err
		}

		e, err := config.ParseEnvironment(name, f.Content)
		if err != nil {
			return config.Environment{}, fmt.Errorf("%w: %w", errEnvironmentTestInvalid, err)
		}

		return e, nil
	}

	return config.Environment{}, fmt.Errorf("%w: %s of factory %s has no environment %s", errEnvironmentNotFound, sel.Kind, factory, name)
}

// versionLabel names the version sel picked: draft, or its number.
func versionLabel(sel authoring.VersionSelector, rec ports.FactoryRecord) string {
	switch {
	case sel.Kind == authoring.SelectNumber:
		return strconv.Itoa(sel.Number)
	case sel.Kind == authoring.SelectCurrent && rec.Current != nil:
		return strconv.Itoa(rec.Current.Number)
	default:
		return string(sel.Kind)
	}
}

// envTest is one test request of factory's environment.
type envTest struct {
	s           *server
	factory     domain.FactoryID
	environment string
}

// server tests one remote MCP server: it opens the secrets its headers name (or takes slots'
// values for a template's, when slots is set), lists its tools and keeps the result.
func (t envTest) server(ctx context.Context, name, url string, headers, slots map[string]string) (gen.ServerTest, error) {
	if !environmentName.MatchString(name) {
		return gen.ServerTest{}, fmt.Errorf("%w: %q is not an MCP server's name", errEnvironmentTestInvalid, name)
	}

	at := t.s.clock.Now().UTC()
	r := domain.EnvironmentTest{Factory: t.factory, Environment: t.environment, Server: name, At: at}

	res, err := environmentcheck.Server(ctx, t.s.tools, t.open, url, headers, slots)
	if err != nil {
		return gen.ServerTest{}, secretError(err)
	}

	r.OK, r.Tools, r.Error = res.OK, res.Tools, res.Error

	if err := t.s.envTests.Record(ctx, r); err != nil {
		return gen.ServerTest{}, err
	}

	return serverTestOut(r), nil
}

// template tests template tpl of the catalog with the secrets its slots take: its remote MCP
// servers (only server, when set), kept as the environment's, and its HTTP test.
func (t envTest) template(ctx context.Context, tpl gen.EnvironmentTestTemplate, server string, out gen.EnvironmentTestResult) (gen.TestEnvironmentResponseObject, error) {
	i := slices.IndexFunc(t.s.catalog, func(c domain.ToolTemplate) bool { return c.Name == tpl.Name })
	if i < 0 {
		return nil, fmt.Errorf("%w: no template %q", errTemplateNotFound, tpl.Name)
	}

	c := t.s.catalog[i]

	servers := slices.Sorted(maps.Keys(c.MCP))
	if server != "" {
		if _, ok := c.MCP[server]; !ok {
			return nil, fmt.Errorf("%w: template %s has no MCP server %q", errEnvironmentServerNotFound, c.Name, server)
		}

		servers = []string{server}
	}

	slots, openErr, err := t.openSlots(ctx, c, tpl.Secrets)
	if err != nil {
		return nil, err
	}

	for _, name := range servers {
		r, err := t.templateServer(ctx, name, c.MCP[name], slots, openErr)
		if err != nil {
			return nil, err
		}

		out.Servers = append(out.Servers, r)
	}

	if c.Test != nil {
		r := gen.TemplateTestResult{Name: c.Name, At: t.s.clock.Now().UTC()}

		status, err := 0, openErr
		if err == nil {
			status, err = t.s.sendTemplateTest(ctx, c, slots, deref(tpl.Env))
		}

		r.Ok = err == nil
		if status > 0 {
			r.Status = &status
		}

		if err != nil {
			r.Error = new(environmentcheck.Cut(environmentcheck.Scrub(err.Error(), slots)))
		}

		out.Template = &r
	}

	return gen.TestEnvironment200JSONResponse(out), nil
}

// openSlots opens the secrets the request puts in each of c's slots: their values by slot. A secret
// that does not exist or is not open to the factory is openErr, which fails the tests that need
// it; err is any other refusal, such as a missing master key, or a slot without a secret.
func (t envTest) openSlots(ctx context.Context, c domain.ToolTemplate, secrets map[string]gen.SecretName) (slots map[string]string, openErr, err error) {
	var names []string

	for _, slot := range c.Secrets {
		name, ok := secrets[slot.Slot]
		if !ok {
			return nil, nil, fmt.Errorf("%w: template %s needs a secret for its slot %s", errEnvironmentTestInvalid, c.Name, slot.Slot)
		}

		names = append(names, name)
	}

	opened, openErr := t.open(ctx, names)
	if openErr != nil && !errors.Is(openErr, ports.ErrNotFound) && !errors.Is(openErr, ports.ErrSecretNotAllowed) {
		return nil, nil, secretError(openErr)
	}

	slots = map[string]string{}

	for slot, name := range secrets {
		if v, ok := opened[name]; ok {
			slots[slot] = v
		}
	}

	return slots, openErr, nil
}

// templateServer tests a template's MCP server m with slots: not a stdio one, and not when its
// secrets did not open (openErr).
func (t envTest) templateServer(ctx context.Context, name string, m domain.TemplateMCP, slots map[string]string, openErr error) (gen.ServerTest, error) {
	switch {
	case m.URL == "":
		return gen.ServerTest{Server: name, Tools: []string{}}, nil
	case openErr != nil:
		return gen.ServerTest{Server: name, Tested: true, Tools: []string{}, Error: new(environmentcheck.Cut(openErr.Error())), At: new(t.s.clock.Now().UTC())}, nil
	default:
		return t.server(ctx, name, m.URL, m.Headers, slots)
	}
}

// open opens the named secrets for the factory, without a run (audited as secret.used): their
// values by name. Its errors name a secret, never a value.
func (t envTest) open(ctx context.Context, names []string) (map[string]string, error) {
	return environmentcheck.Opens(t.s.secrets, domain.SecretUse{Factory: t.factory})(ctx, names)
}

// sendTemplateTest sends template c's test request with slots' values, and env's in place of its
// plain env values, through goald's connection client (public addresses only, no redirect), and
// returns the status it got; an error when there is none, or it is not the one expected.
func (s *server) sendTemplateTest(ctx context.Context, c domain.ToolTemplate, slots, env map[string]string) (int, error) {
	t, err := c.RenderTest(slots, env)
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

// serverTestOut is a kept test as the contract has it.
func serverTestOut(r domain.EnvironmentTest) gen.ServerTest {
	out := gen.ServerTest{Server: r.Server, Tested: true, Ok: r.OK, Tools: append([]string{}, r.Tools...), At: new(r.At)}
	if r.Error != "" {
		out.Error = new(r.Error)
	}

	return out
}
