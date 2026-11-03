package engine

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/byFrederick/goal/internal/core/domain"
	"github.com/byFrederick/goal/internal/core/ports"
	"github.com/byFrederick/goal/internal/core/ports/porttest"
)

// A header or a stdio server's env that names a secret renders on the resolved environment as
// ${GOAL_SECRET_<NAME>}, the value goes in that variable of the agent's env, and the rendered MCP
// config holds no value. Each file's path is kept with the variable its content goes in.
func TestRunEnv_MCPHeaderRendersSecretVariable(t *testing.T) {
	t.Parallel()

	const (
		ticketsValue = "tickets-key-for-tests-4142"
		ghValue      = "github-token-for-tests-4344"
		npmValue     = "npm-config-for-tests-4546"
		factory      = domain.FactoryID("00000000-0000-7000-8000-0000000000f1")
	)

	tenancy := porttest.NewTenancy(func() time.Time { return time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC) })

	o, err := tenancy.CreateOrg(t.Context(), domain.LocalUser, ports.NewOrg{Slug: "acme", Name: "acme"})
	if err != nil {
		t.Fatal(err)
	}

	ctx := domain.WithOrg(t.Context(), o.ID)
	secrets := porttest.NewOrgSecrets(porttest.NewMasterKeys().Key("k1"), tenancy, nil)

	for name, value := range map[string]string{"tickets-key": ticketsValue, "github-token": ghValue, "npm-config": npmValue} {
		if _, err := secrets.Create(ctx, domain.NewSecret{Name: name, Value: value, Scope: domain.SecretScope{All: true}}, domain.LocalUser); err != nil {
			t.Fatal(err)
		}
	}

	e := &Engine{orgSecrets: secrets, log: slog.New(slog.DiscardHandler)}
	env := domain.Environment{
		Name:  domain.DefaultEnvironment,
		Files: map[string]string{"~/.npmrc": "{{npm-config}}"},
		MCP: map[string]domain.MCPServer{
			"tickets": {URL: "https://tickets.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer {{tickets-key}}"}},
			"github":  {Command: []string{"github-mcp"}, Env: map[string]string{"GITHUB_TOKEN": "{{github-token}}"}}, //nolint:gosec // a reference
		},
	}

	got, err := e.secretEnv(ctx, domain.SecretUse{Factory: factory, Agent: "worker", Run: "run-1"}, "", env)
	if err != nil {
		t.Fatal(err)
	}

	if h := got.mcp["tickets"].Headers["Authorization"]; h != "Bearer ${GOAL_SECRET_TICKETS_KEY}" {
		t.Errorf("rendered header %q; want Bearer ${GOAL_SECRET_TICKETS_KEY}", h)
	}

	if v := got.mcp["github"].Env["GITHUB_TOKEN"]; v != "${GOAL_SECRET_GITHUB_TOKEN}" {
		t.Errorf("rendered stdio env %q; want ${GOAL_SECRET_GITHUB_TOKEN}", v)
	}

	if got.agent["GOAL_SECRET_TICKETS_KEY"] != ticketsValue || got.agent["GOAL_SECRET_GITHUB_TOKEN"] != ghValue {
		t.Errorf("the agent's GOAL_SECRET_* variables do not hold the secrets' values")
	}

	if got.files["~/.npmrc"] != "GOAL_FILE_1" || got.agent["GOAL_FILE_1"] != npmValue {
		t.Errorf("files %v; want ~/.npmrc in GOAL_FILE_1 with its secret's value", got.files)
	}

	rendered := fmt.Sprint(got.mcp)
	for _, v := range []string{ticketsValue, ghValue, npmValue} {
		if strings.Contains(rendered, v) {
			t.Errorf("the rendered MCP servers %s hold a secret's value", rendered)
		}
	}
}
